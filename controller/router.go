package controller

import (
	"log"
	"sync"

	radix "github.com/armon/go-radix"
	networkingv1 "k8s.io/api/networking/v1"
)

type Router struct {
	mu     sync.RWMutex
	routes map[string]*HostConfig
}
type HostConfig struct {
	Host  string
	Paths []*PathConfig
	tree  *radix.Tree
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
			tree:  radix.New(),
		}
		r.routes[host] = hostconfig
	}
	// checking if path already exists - update it
	for _, p := range hostconfig.Paths {
		if p.Path == path {
			p.PathType = pathTypeStr
			p.Key = key
			p.Port = port
			p.Algorithm = algo
			hostconfig.tree.Insert(path, p)
			return nil
		}
	}
	// New path
	pathConfig := &PathConfig{
		Path:      path,
		PathType:  pathTypeStr,
		Key:       key,
		Port:      port,
		Algorithm: algo,
	}
	hostconfig.Paths = append(hostconfig.Paths, pathConfig)
	hostconfig.tree.Insert(path, pathConfig)
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
			continue
		}

		pathsToRemove := make(map[string]bool)
		for _, path := range rule.HTTP.Paths {
			pathsToRemove[path.Path] = true
			hostconfig.tree.Delete(path.Path)
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
func (r *Router) Match(host, path string) *PathConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hostconfig, exists := r.routes[host]
	if !exists {
		hostconfig, exists = r.routes[""]
		if !exists {
			return nil
		}
	}

	_, val, found := hostconfig.tree.LongestPrefix(path)
	if !found {
		return nil
	}
	pathConfig := val.(*PathConfig)

	if pathConfig.PathType == "Exact" && pathConfig.Path != path {
		return nil
	}
	return pathConfig
}
func (r *Router) GetAllRoutes() map[string]*HostConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	copy := make(map[string]*HostConfig)
	for k, v := range r.routes {
		copy[k] = v
	}
	log.Printf("[DEBUG] Making a copy for Debug server")
	return copy
}
