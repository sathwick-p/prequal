package loadbalancer

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"prequal/controller"
	"prequal/loadbalancer/pool"
	"prequal/observability"
	"time"
)

// ProbeResponse is the JSON payload returned by the backend's /prequal/probe endpoint.
type ProbeResponse struct {
	RIF             int64   `json:"rif"`
	LatencyMedianMs float64 `json:"latency_median_ms"`
	TimestampMs     uint64  `json:"timestamp_ms"`
}

// Prober sends async HTTP probes to backends and feeds results into the ProbePool.
type Prober struct {
	pool               *pool.ProbePool
	ips                *controller.BackendIPStore
	probePort          int
	probeTimeout       time.Duration
	probesPerQuery     float64
	probeWorkers       int
	backgroundInterval time.Duration
	maxProbeAge        time.Duration
	workCh             chan string
	client             *http.Client
	stopCh             <-chan struct{}
}

// NewProber creates a Prober from a ProbeConfig.
func NewProber(
	p *pool.ProbePool,
	ips *controller.BackendIPStore,
	cfg ProbeConfig,
	stopCh <-chan struct{},
) *Prober {
	return &Prober{
		pool:               p,
		ips:                ips,
		probePort:          cfg.ProbePort,
		probeTimeout:       cfg.ProbeTimeout,
		probesPerQuery:     cfg.ProbesPerQuery,
		probeWorkers:       max(cfg.ProbeWorkers, 1),
		backgroundInterval: cfg.BackgroundInterval,
		maxProbeAge:        cfg.MaxProbeAge,
		workCh:             make(chan string, max(cfg.TriggerQueueSize, 1)),
		client: &http.Client{
			Timeout: cfg.ProbeTimeout,
		},
		stopCh: stopCh,
	}
}

// ProbeBackend sends a GET to http://<endpoint-addr>:<probePort>/prequal/probe and
// returns a ProbeEntry on success.
func (pr *Prober) ProbeBackend(endpoint *controller.Endpoint) (*pool.ProbeEntry, error) {
	observability.RecordProbeSent()

	url := fmt.Sprintf("http://%s:%d/prequal/probe", endpoint.Addr(), pr.probePort)
	resp, err := pr.client.Get(url)
	if err != nil {
		observability.RecordProbeFailed("timeout")
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		observability.RecordProbeFailed("non_200")
		return nil, fmt.Errorf("probe returned status %d", resp.StatusCode)
	}

	var probeResp ProbeResponse
	if err := json.NewDecoder(resp.Body).Decode(&probeResp); err != nil {
		observability.RecordProbeFailed("decode_error")
		return nil, err
	}

	// Use backend-reported timestamp if available, otherwise fall back to local time.
	var ts time.Time
	if probeResp.TimestampMs > 0 {
		ts = time.UnixMilli(int64(probeResp.TimestampMs))
	} else {
		ts = time.Now()
	}

	// Staleness check: reject probe responses whose timestamp is too old.
	if pr.maxProbeAge > 0 && time.Since(ts) > pr.maxProbeAge {
		observability.RecordProbeFailed("stale")
		return nil, fmt.Errorf("probe response too stale: age %v", time.Since(ts))
	}

	latency := time.Duration(probeResp.LatencyMedianMs * float64(time.Millisecond))
	observability.RecordProbeSuccess()
	return &pool.ProbeEntry{
		Backend:   endpoint.String(),
		Endpoint:  endpoint,
		RIF:       probeResp.RIF,
		Latency:   latency,
		Timestamp: ts,
	}, nil
}

// ProbeRandom picks a random backend from the list, probes it, and adds the result
// to the pool. Errors are silently ignored.
func (pr *Prober) ProbeRandom(backends []*controller.Endpoint) {
	if len(backends) == 0 {
		return
	}
	ep := backends[rand.Intn(len(backends))]
	entry, err := pr.ProbeBackend(ep)
	if err != nil {
		return
	}
	pr.pool.Add(entry)
}

func (pr *Prober) runWorker() {
	for {
		select {
		case <-pr.stopCh:
			return
		case routeKey := <-pr.workCh:
			observability.RecordProbeQueueDepth(len(pr.workCh))
			backends := pr.ips.Get(routeKey)
			pr.ProbeRandom(backends)
		}
	}
}

func (pr *Prober) enqueueProbe(routeKey string) {
	select {
	case <-pr.stopCh:
		return
	case pr.workCh <- routeKey:
		observability.RecordProbeQueueDepth(len(pr.workCh))
	default:
		observability.RecordProbeDropped("queue_full")
	}
}

func (pr *Prober) probesForQuery() int {
	if pr.probesPerQuery <= 0 {
		return 0
	}
	base := int(math.Floor(pr.probesPerQuery))
	fractional := pr.probesPerQuery - float64(base)
	if rand.Float64() < fractional {
		base++
	}
	return base
}

// TriggerProbes is called once per incoming request. It fires probesPerQuery
// background probes for the given route key without blocking the request path.
func (pr *Prober) TriggerProbes(routeKey string) {
	if routeKey == "" {
		return
	}
	n := pr.probesForQuery()
	for i := 0; i < n; i++ {
		pr.enqueueProbe(routeKey)
	}
}

// Run is a background loop that periodically probes random backends to keep the
// pool fresh during idle periods. It stops when stopCh is closed.
func (pr *Prober) Run() {
	for i := 0; i < pr.probeWorkers; i++ {
		go pr.runWorker()
	}
	if pr.backgroundInterval <= 0 {
		<-pr.stopCh
		return
	}
	ticker := time.NewTicker(pr.backgroundInterval)
	defer ticker.Stop()
	for {
		select {
		case <-pr.stopCh:
			return
		case <-ticker.C:
			keys := pr.ips.Keys()
			if len(keys) == 0 {
				continue
			}
			routeKey := keys[rand.Intn(len(keys))]
			pr.enqueueProbe(routeKey)
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
