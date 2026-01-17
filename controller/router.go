package controller

import (
	"log"
	"sync"

	networkingv1 "k8s.io/api/networking/v1"
)

type Router struct {
	mu     sync.RWMutex
	routes map[string]*HostConfig
}
type HostConfig struct {
	Host  string
	Paths []*PathConfig
}

type PathConfig struct {
	Path      string
	PathType  string
	Key       string
	Port      int32
	Algorithm string
}

func NewRouter() *Router {
	return &Router{
		routes: make(map[string]*HostConfig),
	}
}

func (r *Router) AddRoute(host string, path string, pathType *networkingv1.PathType, key string, port int32, algo string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	pathTypeStr := "Prefix"
	if pathType != nil {
		pathTypeStr = string(*pathType)
	}

	hostconfig, exists := r.routes[host]
	if !exists {
		hostconfig = &HostConfig{
			Host:  host,
			Paths: make([]*PathConfig, 0),
		}
		r.routes[host] = hostconfig
	}

	for _, p := range hostconfig.Paths {
		if p.Path == path {
			p.PathType = pathTypeStr
			p.Key = key
			p.Port = port
			p.Algorithm = algo
			return nil
		}
	}

	hostconfig.Paths = append(hostconfig.Paths, &PathConfig{
		Path:      path,
		PathType:  pathTypeStr,
		Key:       key,
		Port:      port,
		Algorithm: algo,
	})
	log.Printf("[ROUTER] Added host: %s\n", host)
	return nil
}

func (r *Router) RemoveRoute(ingress *networkingv1.Ingress) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}

		host := rule.Host
		hostconfig, exists := r.routes[host]
		if !exists {
			return
		}

		pathsToRemove := make(map[string]bool)
		for _, path := range rule.HTTP.Paths {
			pathsToRemove[path.Path] = true
		}

		filtered := make([]*PathConfig, 0, len(hostconfig.Paths))
		for _, p := range hostconfig.Paths {
			if !pathsToRemove[p.Path] {
				filtered = append(filtered, p)
			}
		}

		if len(filtered) == 0 {
			delete(r.routes, host)
			log.Printf("[ROUTER] Removed host: %s\n", host)
		} else {
			hostconfig.Paths = filtered
		}
	}
}
