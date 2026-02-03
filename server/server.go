package server

import (
	"fmt"
	"log"
	"main/controller"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ProxyServer struct {
	router    *controller.Router
	ips       *controller.BackendIPStore
	Transport *http.Transport
}

func NewProxyServer(router *controller.Router, ips *controller.BackendIPStore) *ProxyServer {
	return &ProxyServer{
		router: router,
		ips:    ips,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

func (p *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}
	path := r.URL.Path
	log.Printf("[PROXY] %s %s Host: %s", r.Method, path, host)

	// match routes

	pathConfig := p.router.Match(host, path)
	if pathConfig == nil {
		log.Printf("[PROXY] No route found for %s%s", host, path)
		http.Error(w, "no route found", http.StatusNotFound)
		return
	}
	log.Printf("[PROXY] Matched: %s → %s", pathConfig.Path, pathConfig.Key)

	backends := p.ips.Get(pathConfig.Key)
	if len(backends) == 0 {
		log.Printf("[PROXY] No backends for %s", pathConfig.Key)
		http.Error(w, "no backends available", http.StatusServiceUnavailable)
		return
	}

	// 4. Select backend (simple: first one for now)
	backend := backends[0]
	target := fmt.Sprintf(
		"http://%s",
		net.JoinHostPort(backend.Addr(), strconv.Itoa(int(backend.Port()))),
	)

	log.Printf("[PROXY] Forwarding to %s", target)

	// 5. Parse target URL
	targetURL, err := url.Parse(target)
	if err != nil {
		http.Error(w, "invalid backend", http.StatusInternalServerError)
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

	proxy.ServeHTTP(w, r)
}
