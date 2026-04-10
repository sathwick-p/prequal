package server_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"prequal/controller"
	"prequal/loadbalancer/roundrobin"
	"prequal/server"
)

// backendEndpoint extracts host and port from a test server's listener address.
func backendEndpoint(t *testing.T, ts *httptest.Server) *controller.Endpoint {
	t.Helper()
	addr := ts.Listener.Addr().String()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("failed to parse test server addr %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("failed to parse port from %q: %v", portStr, err)
	}
	return controller.NewEndpoint(host, int32(port))
}

func newProxy(router *controller.Router, store *controller.BackendIPStore) *server.ProxyServer {
	rr := &roundrobin.RoundRobin{}
	return server.NewProxyServer(router, store, rr)
}

// Test 1: Request forwarded to matched backend
func TestRequestForwardedToMatchedBackend(t *testing.T) {
	reached := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	router := controller.NewRouter()
	_ = router.AddRoute("example.com", "/", nil, "svc-key", 80, "")

	store := controller.NewBackendIPStore()
	store.Set("svc-key", []*controller.Endpoint{backendEndpoint(t, backend)})

	proxy := newProxy(router, store)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if !reached {
		t.Error("expected request to reach the backend, but it did not")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// Test 2: No route returns 404
func TestNoRouteReturns404(t *testing.T) {
	router := controller.NewRouter()
	store := controller.NewBackendIPStore()
	proxy := newProxy(router, store)

	req := httptest.NewRequest(http.MethodGet, "http://unknown.host/path", nil)
	req.Host = "unknown.host"
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// Test 3: No backends returns 503
func TestNoBackendsReturns503(t *testing.T) {
	router := controller.NewRouter()
	_ = router.AddRoute("example.com", "/", nil, "svc-key", 80, "")

	store := controller.NewBackendIPStore()
	// do not add any endpoints for "svc-key"

	proxy := newProxy(router, store)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

// Test 4: Backend error returns 502
func TestBackendErrorReturns502(t *testing.T) {
	router := controller.NewRouter()
	_ = router.AddRoute("example.com", "/", nil, "svc-key", 80, "")

	store := controller.NewBackendIPStore()
	// Point to a port where nothing is listening
	store.Set("svc-key", []*controller.Endpoint{
		controller.NewEndpoint("127.0.0.1", 19999),
	})

	proxy := newProxy(router, store)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

// Test 5: Round-robin distributes requests across backends
func TestRoundRobinDistributes(t *testing.T) {
	const numBackends = 3
	const totalRequests = 6

	counts := make([]atomic.Int32, numBackends)
	backends := make([]*httptest.Server, numBackends)
	endpoints := make([]*controller.Endpoint, numBackends)

	for i := 0; i < numBackends; i++ {
		idx := i
		backends[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			counts[idx].Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer backends[i].Close()
		endpoints[i] = backendEndpoint(t, backends[i])
	}

	router := controller.NewRouter()
	_ = router.AddRoute("example.com", "/", nil, "svc-key", 80, "")

	store := controller.NewBackendIPStore()
	store.Set("svc-key", endpoints)

	proxy := newProxy(router, store)

	for i := 0; i < totalRequests; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
		req.Host = "example.com"
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rec.Code)
		}
	}

	expected := totalRequests / numBackends
	for i, c := range counts {
		if got := int(c.Load()); got != expected {
			t.Errorf("backend %d: expected %d requests, got %d", i, expected, got)
		}
	}
}

// Test 6: Host header forwarded correctly
func TestHostHeaderForwarded(t *testing.T) {
	var receivedHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	router := controller.NewRouter()
	_ = router.AddRoute("example.com", "/", nil, "svc-key", 80, "")

	store := controller.NewBackendIPStore()
	store.Set("svc-key", []*controller.Endpoint{backendEndpoint(t, backend)})

	proxy := newProxy(router, store)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if receivedHost != "example.com" {
		t.Errorf("expected Host header 'example.com', got %q", receivedHost)
	}
}

// Test 7: X-Forwarded headers set
func TestXForwardedHeadersSet(t *testing.T) {
	var (
		xForwardedFor   string
		xForwardedHost  string
		xForwardedProto string
	)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		xForwardedFor = r.Header.Get("X-Forwarded-For")
		xForwardedHost = r.Header.Get("X-Forwarded-Host")
		xForwardedProto = r.Header.Get("X-Forwarded-Proto")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	router := controller.NewRouter()
	_ = router.AddRoute("example.com", "/", nil, "svc-key", 80, "")

	store := controller.NewBackendIPStore()
	store.Set("svc-key", []*controller.Endpoint{backendEndpoint(t, backend)})

	proxy := newProxy(router, store)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Host = "example.com"
	// Simulate a client IP by setting RemoteAddr
	req.RemoteAddr = "203.0.113.5:4321"
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if xForwardedFor == "" {
		t.Error("expected X-Forwarded-For to be set, got empty")
	}
	if xForwardedHost != "example.com" {
		t.Errorf("expected X-Forwarded-Host 'example.com', got %q", xForwardedHost)
	}
	if xForwardedProto == "" {
		t.Error("expected X-Forwarded-Proto to be set, got empty")
	}
}
