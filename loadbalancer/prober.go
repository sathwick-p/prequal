package loadbalancer

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"prequal/controller"
	"prequal/loadbalancer/pool"
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
	pool           *pool.ProbePool
	ips            *controller.BackendIPStore
	probePort      int
	probeTimeout   time.Duration
	probesPerQuery float64
	client         *http.Client
	stopCh         <-chan struct{}
}

// NewProber creates a Prober. probePort is the port backends expose /prequal/probe on.
func NewProber(
	p *pool.ProbePool,
	ips *controller.BackendIPStore,
	probePort int,
	probeTimeout time.Duration,
	probesPerQuery float64,
	stopCh <-chan struct{},
) *Prober {
	return &Prober{
		pool:           p,
		ips:            ips,
		probePort:      probePort,
		probeTimeout:   probeTimeout,
		probesPerQuery: probesPerQuery,
		client: &http.Client{
			Timeout: probeTimeout,
		},
		stopCh: stopCh,
	}
}

// ProbeBackend sends a GET to http://<endpoint-addr>:<probePort>/prequal/probe and
// returns a ProbeEntry on success.
func (pr *Prober) ProbeBackend(endpoint *controller.Endpoint) (*pool.ProbeEntry, error) {
	url := fmt.Sprintf("http://%s:%d/prequal/probe", endpoint.Addr(), pr.probePort)
	resp, err := pr.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var probeResp ProbeResponse
	if err := json.NewDecoder(resp.Body).Decode(&probeResp); err != nil {
		return nil, err
	}

	latency := time.Duration(probeResp.LatencyMedianMs * float64(time.Millisecond))
	return &pool.ProbeEntry{
		Backend:   endpoint.String(),
		Endpoint:  endpoint,
		RIF:       probeResp.RIF,
		Latency:   latency,
		Timestamp: time.Now(),
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
	ticker := time.NewTicker(100 * time.Millisecond)
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
