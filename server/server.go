package server

import (
	"main/controller"
	"net/http"
	"net/http/httputil"
	"strings"
)


type ProxyServer struct{
	router *controller.Router
	ips *controller.BackendIPStore
	Transport *http.Transport
}


func(p *ProxyServer) NewServer(w http.ResponseWriter, r *http.Request){
	host := r.Host
	if colonIndex := strings.Index(host,":"); colonIndex != -1{
		host = host[:colonIndex]
	}

	path := r.URL.Path

	// longest prefix matching
	pathConfig := p.router.Match(host, path)
	
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL()
		},
	}
}