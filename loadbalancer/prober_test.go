package loadbalancer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"prequal/controller"
	"prequal/loadbalancer/pool"
	"strconv"
	"strings"
	"testing"
	"time"
)

func hostPortFromURL(url string) (host string, port int, err error) {
	addr := strings.TrimPrefix(url, "http://")
	parts := strings.SplitN(addr, ":", 2)
	host = parts[0]
	port, err = strconv.Atoi(parts[1])
	return
}

func newTestProbePool() *pool.ProbePool {
	return pool.NewProbePool(pool.PoolConfig{
		MaxSize:    10,
		MaxAge:     time.Hour,
		ReuseLimit: 5,
		QRIF:       0.5,
	})
}

func newTestProber(p *pool.ProbePool, probePort int) *Prober {
	stop := make(chan struct{})
	ips := controller.NewBackendIPStore()
	return NewProber(p, ips, ProbeConfig{
		ProbePort:          probePort,
		ProbeTimeout:       2 * time.Second,
		ProbesPerQuery:     1.0,
		BackgroundInterval: time.Second,
	}, stop)
}

func TestProber_ProbeBackend_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ProbeResponse{
			RIF:             3,
			LatencyMedianMs: 12.5,
			TimestampMs:     uint64(time.Now().UnixMilli()),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	host, port, err := hostPortFromURL(ts.URL)
	if err != nil {
		t.Fatalf("could not parse test server URL: %v", err)
	}

	pr := newTestProber(newTestProbePool(), port)
	ep := controller.NewEndpoint(host, int32(port))

	entry, err := pr.ProbeBackend(ep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.RIF != 3 {
		t.Errorf("expected RIF=3, got %d", entry.RIF)
	}
	want := time.Duration(12.5 * float64(time.Millisecond))
	if entry.Latency != want {
		t.Errorf("expected latency=%v, got %v", want, entry.Latency)
	}
}

func TestProber_ProbeBackend_Non200Status(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	host, port, err := hostPortFromURL(ts.URL)
	if err != nil {
		t.Fatalf("could not parse test server URL: %v", err)
	}

	pr := newTestProber(newTestProbePool(), port)
	ep := controller.NewEndpoint(host, int32(port))

	_, err = pr.ProbeBackend(ep)
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestProber_ProbeBackend_MalformedJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{not valid json"))
	}))
	defer ts.Close()

	host, port, err := hostPortFromURL(ts.URL)
	if err != nil {
		t.Fatalf("could not parse test server URL: %v", err)
	}

	pr := newTestProber(newTestProbePool(), port)
	ep := controller.NewEndpoint(host, int32(port))

	_, err = pr.ProbeBackend(ep)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestProber_ProbeBackend_ZeroTimestampUsesLocalTime(t *testing.T) {
	before := time.Now()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ProbeResponse{
			RIF:             1,
			LatencyMedianMs: 5.0,
			TimestampMs:     0, // zero => fallback to local time
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	host, port, err := hostPortFromURL(ts.URL)
	if err != nil {
		t.Fatalf("could not parse test server URL: %v", err)
	}

	// MaxProbeAge=0 disables staleness check so we can test timestamp fallback.
	p := pool.NewProbePool(pool.PoolConfig{MaxSize: 10, MaxAge: time.Hour, ReuseLimit: 5, QRIF: 0.5})
	stop := make(chan struct{})
	ips := controller.NewBackendIPStore()
	pr := NewProber(p, ips, ProbeConfig{
		ProbePort:          port,
		ProbeTimeout:       2 * time.Second,
		ProbesPerQuery:     1.0,
		BackgroundInterval: time.Second,
		MaxProbeAge:        0, // disabled
	}, stop)

	ep := controller.NewEndpoint(host, int32(port))
	entry, err := pr.ProbeBackend(ep)
	after := time.Now()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Timestamp.Before(before) || entry.Timestamp.After(after) {
		t.Errorf("expected timestamp between %v and %v, got %v", before, after, entry.Timestamp)
	}
}

func TestProber_TriggerProbes_ReturnsQuickly(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // simulate slow backend
		resp := ProbeResponse{RIF: 0, LatencyMedianMs: 1.0, TimestampMs: uint64(time.Now().UnixMilli())}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	host, port, err := hostPortFromURL(ts.URL)
	if err != nil {
		t.Fatalf("could not parse test server URL: %v", err)
	}

	ips := controller.NewBackendIPStore()
	ep := controller.NewEndpoint(host, int32(port))
	ips.Set("route1", []*controller.Endpoint{ep})

	stop := make(chan struct{})
	defer close(stop)

	p := newTestProbePool()
	pr := NewProber(p, ips, ProbeConfig{
		ProbePort:          port,
		ProbeTimeout:       2 * time.Second,
		ProbesPerQuery:     2.0,
		BackgroundInterval: time.Second,
	}, stop)

	start := time.Now()
	pr.TriggerProbes("route1")
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("TriggerProbes blocked for %v, expected near-instant return", elapsed)
	}
}
