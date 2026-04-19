package pool

import (
	"fmt"
	"prequal/controller"
	"prequal/observability"
	"sync"
	"time"
)

// RoutePools keeps independent probe pools per route key so observations never
// bleed across ingress routes.
type RoutePools struct {
	mu                sync.RWMutex
	pools             map[string]*ProbePool
	cfg               PoolConfig
	maintenanceTicker time.Duration
}

func NewRoutePools(cfg PoolConfig, maintenanceTicker time.Duration) *RoutePools {
	return &RoutePools{
		pools:             make(map[string]*ProbePool),
		cfg:               cfg,
		maintenanceTicker: maintenanceTicker,
	}
}

func (rp *RoutePools) getOrCreate(routeKey string) *ProbePool {
	rp.mu.RLock()
	existing := rp.pools[routeKey]
	rp.mu.RUnlock()
	if existing != nil {
		return existing
	}

	rp.mu.Lock()
	defer rp.mu.Unlock()
	if existing = rp.pools[routeKey]; existing != nil {
		return existing
	}
	existing = NewProbePool(rp.cfg)
	rp.pools[routeKey] = existing
	return existing
}

func (rp *RoutePools) Add(routeKey string, entry *ProbeEntry) {
	if routeKey == "" || entry == nil {
		return
	}
	size := rp.getOrCreate(routeKey).Add(entry)
	observability.RecordPoolOccupancy(routeKey, size)
}

func (rp *RoutePools) IncrementRIF(routeKey, backend string) {
	if routeKey == "" {
		return
	}
	pool := rp.getOrCreate(routeKey)
	pool.IncrementRIF(backend)
}

func (rp *RoutePools) Select(routeKey string, allBackends []*controller.Endpoint) (*ProbeEntry, error) {
	if routeKey == "" {
		return nil, fmt.Errorf("missing route key")
	}
	pool := rp.getOrCreate(routeKey)
	entry, size, err := pool.Select(allBackends)
	observability.RecordPoolOccupancy(routeKey, size)
	return entry, err
}

func (rp *RoutePools) DeleteRoute(routeKey string) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	delete(rp.pools, routeKey)
	observability.RecordPoolOccupancy(routeKey, 0)
}

func (rp *RoutePools) Run(stopCh <-chan struct{}) {
	if rp.maintenanceTicker <= 0 {
		<-stopCh
		return
	}

	ticker := time.NewTicker(rp.maintenanceTicker)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			rp.mu.RLock()
			snapshot := make(map[string]*ProbePool, len(rp.pools))
			for routeKey, pool := range rp.pools {
				snapshot[routeKey] = pool
			}
			rp.mu.RUnlock()

			emptyKeys := make([]string, 0)
			for routeKey, pool := range snapshot {
				size := pool.Maintain()
				observability.RecordPoolOccupancy(routeKey, size)
				if size == 0 {
					emptyKeys = append(emptyKeys, routeKey)
				}
			}

			if len(emptyKeys) == 0 {
				continue
			}
			rp.mu.Lock()
			for _, routeKey := range emptyKeys {
				if current, ok := rp.pools[routeKey]; ok && current.Size() == 0 {
					delete(rp.pools, routeKey)
				}
			}
			rp.mu.Unlock()
		}
	}
}
