package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"prequal/controller"
	"prequal/loadbalancer"
	"prequal/loadbalancer/pool"
	"prequal/observability"
	"strconv"
	"strings"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

type ProxyServer struct {
	router         *controller.Router
	ips            *controller.BackendIPStore
	Transport      *http.Transport
	tracker        *loadbalancer.RIFTracker
	latencyTracker *loadbalancer.LatencyTracker
	pool           *pool.ProbePool
}

func NewProxyServer(router *controller.Router, ips *controller.BackendIPStore, tracker *loadbalancer.RIFTracker, latencyTracker *loadbalancer.LatencyTracker, pool *pool.ProbePool) *ProxyServer {
	return &ProxyServer{
		router: router,
		ips:    ips,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
		tracker:        tracker,
		latencyTracker: latencyTracker,
		pool:           pool,
	}
}

func (p *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}
	path := r.URL.Path
	log.Printf("[PROXY] %s %s Host: %s", r.Method, path, host)

	rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

	// match routes
	pathConfig := p.router.Match(host, path)
	start := time.Now()
	if pathConfig == nil {
		log.Printf("[PROXY] No route found for %s%s", host, path)
		observability.RecordNoRoute()
		http.Error(rec, "no route found", http.StatusNotFound)
		observability.RecordRequest(host, path, observability.StatusCode(http.StatusNotFound), time.Since(start), "")
		return
	}
	log.Printf("[PROXY] Matched: %s → %s", pathConfig.Path, pathConfig.Key)

	backends := p.ips.Get(pathConfig.Key)
	if len(backends) == 0 {
		log.Printf("[PROXY] No backends for %s", pathConfig.Key)
		observability.RecordNoBackends()
		http.Error(rec, "0 backends available", http.StatusServiceUnavailable)
		observability.RecordRequest(host, path, observability.StatusCode(http.StatusServiceUnavailable), time.Since(start), "")
		return
	}

	// Select backend via probe pool (HCL) with fallback to random
	entry, err := p.pool.Select(backends)
	if err != nil {
		observability.RecordNoBackends()
		http.Error(rec, "0 backends available", http.StatusServiceUnavailable)
		observability.RecordRequest(host, path, observability.StatusCode(http.StatusServiceUnavailable), time.Since(start), "")
		return
	}

	backend := entry.Endpoint
	backendAddr := backend.String()
	observability.RecordBackendSelection(backendAddr, pathConfig.Algorithm)

	// Increment RIF in both the pool entry and the tracker
	p.pool.IncrementRIF(backendAddr)
	p.tracker.Increase(backendAddr)
	defer p.tracker.Decrease(backendAddr)

	target := fmt.Sprintf(
		"http://%s",
		net.JoinHostPort(backend.Addr(), strconv.Itoa(int(backend.Port()))),
	)

	log.Printf("[PROXY] Forwarding to %s", target)

	targetURL, err := url.Parse(target)
	if err != nil {
		http.Error(rec, "invalid backend", http.StatusInternalServerError)
		observability.RecordRequest(host, path, observability.StatusCode(http.StatusInternalServerError), time.Since(start), backendAddr)
		return
	}

	// Creating Reverse proxy
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			pr.Out.Host = r.Host
			pr.SetXForwarded()
		},
		Transport: p.Transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[PROXY ERROR] %v", err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}

	proxyStart := time.Now()
	proxy.ServeHTTP(rec, r)
	proxyDuration := time.Since(proxyStart)

	// Record latency
	p.latencyTracker.Record(backendAddr, proxyDuration)

	// Feed virtual probe back into the pool
	p.pool.Add(&pool.ProbeEntry{
		Backend:   backendAddr,
		Endpoint:  backend,
		RIF:       p.tracker.Get(backendAddr),
		Latency:   p.latencyTracker.Median(backendAddr),
		Timestamp: time.Now(),
	})

	observability.RecordRequest(host, path, observability.StatusCode(rec.statusCode), time.Since(start), backendAddr)
}
