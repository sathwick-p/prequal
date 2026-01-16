package controller

import (
	networkingv1 "k8s.io/api/networking/v1"
	"sync"
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
	return nil
}

func (r *Router) RemoveRoute(host string, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	hostConfig, exists := r.routes[host]
	if !exists {
		return
	}

	filtered := make([]*PathConfig, 0, len(hostConfig.Paths))
	for _, p := range hostConfig.Paths {
		if p.Path != path {
			filtered = append(filtered, p)
		}
	}

	if len(filtered) == 0 {
		delete(r.routes, host)
	} else {
		hostConfig.Paths = filtered
	}
}
