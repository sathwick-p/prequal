package controller

import (
	"log"
	"prequal/tree"
	"sync"

	networkingv1 "k8s.io/api/networking/v1"
)

type Router struct {
	mu           sync.RWMutex
	routes       map[string]*HostConfig
	ingressPaths map[string][]routeRef
}

type HostConfig struct {
	Host  string
	Paths []*tree.PathConfig
	trie  *tree.SegmentNode
}

type RouteSpec struct {
	Host      string
	Path      string
	PathType  string
	Key       string
	Port      int32
	Algorithm string
}

type routeRef struct {
	host string
	path string
}

func NewRouter() *Router {
	return &Router{
		routes:       make(map[string]*HostConfig),
		ingressPaths: make(map[string][]routeRef),
	}
}

func (r *Router) AddRoute(host string, path string, pathType *networkingv1.PathType, key string, port int32, algo string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.addRouteLocked(RouteSpec{
		Host:      host,
		Path:      path,
		PathType:  pathTypeString(pathType),
		Key:       key,
		Port:      port,
		Algorithm: algo,
	})
	return nil
}

func (r *Router) ReplaceIngressRoutes(ingressKey string, specs []RouteSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.deleteIngressRoutesLocked(ingressKey)

	if len(specs) == 0 {
		delete(r.ingressPaths, ingressKey)
		return
	}

	refs := make([]routeRef, 0, len(specs))
	for _, spec := range specs {
		r.addRouteLocked(spec)
		refs = append(refs, routeRef{host: spec.Host, path: spec.Path})
	}
	r.ingressPaths[ingressKey] = refs
}

func (r *Router) DeleteIngressRoutes(ingressKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteIngressRoutesLocked(ingressKey)
}

func (r *Router) RemoveRoute(ingress *networkingv1.Ingress) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			r.removeRouteLocked(rule.Host, path.Path)
		}
	}
}

func (r *Router) Match(host, path string) *tree.PathConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hostconfig, exists := r.routes[host]
	if !exists {
		hostconfig, exists = r.routes[""]
		if !exists {
			return nil
		}
	}

	return hostconfig.trie.Match(path)
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

func (r *Router) addRouteLocked(spec RouteSpec) {
	hostconfig, exists := r.routes[spec.Host]
	if !exists {
		hostconfig = &HostConfig{
			Host:  spec.Host,
			Paths: make([]*tree.PathConfig, 0),
			trie:  tree.NewSegmentNode(),
		}
		r.routes[spec.Host] = hostconfig
	}

	for _, p := range hostconfig.Paths {
		if p.Path == spec.Path {
			p.PathType = spec.PathType
			p.Key = spec.Key
			p.Port = spec.Port
			p.Algorithm = spec.Algorithm
			hostconfig.trie.Insert(spec.Path, p)
			return
		}
	}

	pathConfig := &tree.PathConfig{
		Path:      spec.Path,
		PathType:  spec.PathType,
		Key:       spec.Key,
		Port:      spec.Port,
		Algorithm: spec.Algorithm,
	}
	hostconfig.Paths = append(hostconfig.Paths, pathConfig)
	hostconfig.trie.Insert(spec.Path, pathConfig)
	log.Printf("[ROUTER] Added host: %s\n", spec.Host)
}

func (r *Router) deleteIngressRoutesLocked(ingressKey string) {
	refs := r.ingressPaths[ingressKey]
	for _, ref := range refs {
		r.removeRouteLocked(ref.host, ref.path)
	}
	delete(r.ingressPaths, ingressKey)
}

func (r *Router) removeRouteLocked(host, path string) {
	hostconfig, exists := r.routes[host]
	if !exists {
		return
	}

	hostconfig.trie.Delete(path)

	filtered := make([]*tree.PathConfig, 0, len(hostconfig.Paths))
	for _, p := range hostconfig.Paths {
		if p.Path != path {
			filtered = append(filtered, p)
		}
	}

	if len(filtered) == 0 {
		delete(r.routes, host)
		log.Printf("[ROUTER] Removed host: %s\n", host)
		return
	}

	hostconfig.Paths = filtered
}

func pathTypeString(pathType *networkingv1.PathType) string {
	if pathType == nil {
		return string(networkingv1.PathTypePrefix)
	}
	return string(*pathType)
}
