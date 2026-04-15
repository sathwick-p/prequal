package loadbalancer

import (
	"encoding/json"
	"fmt"
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
	backgroundInterval time.Duration
	maxProbeAge        time.Duration
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
		backgroundInterval: cfg.BackgroundInterval,
		maxProbeAge:        cfg.MaxProbeAge,
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

// TriggerProbes is called once per incoming request. It fires probesPerQuery
// background probes for the given route key without blocking the request path.
func (pr *Prober) TriggerProbes(routeKey string) {
	backends := pr.ips.Get(routeKey)
	if len(backends) == 0 {
		return
	}
	n := int(pr.probesPerQuery)
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		go pr.ProbeRandom(backends)
	}
}

// Run is a background loop that periodically probes random backends to keep the
// pool fresh during idle periods. It stops when stopCh is closed.
func (pr *Prober) Run() {
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
			backends := pr.ips.Get(routeKey)
			go pr.ProbeRandom(backends)
		}
	}
}
