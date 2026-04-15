package pool

import (
	"fmt"
	"math/rand"
	"prequal/controller"
	"prequal/observability"
	"slices"
	"sync"
	"time"
)

type ProbeEntry struct {
	Backend   string
	Endpoint  *controller.Endpoint
	RIF       int64
	Latency   time.Duration
	Timestamp time.Time
	UsesLeft  int
}

// PoolConfig holds the tunables that NewProbePool accepts. It is a subset of
// loadbalancer.ProbeConfig and lives here to avoid an import cycle.
type PoolConfig struct {
	MaxSize    int
	MaxAge     time.Duration
	ReuseLimit int
	QRIF       float64
	MaxProbeAge time.Duration
}

type ProbePool struct {
	mu             sync.Mutex
	entries        []*ProbeEntry
	maxSize        int
	maxAge         time.Duration
	reuseLimit     int
	removeWorstAlt bool // alternates between oldest and highest-load removal
	QRIF           float64
	maxProbeAge    time.Duration
}

func NewProbePool(cfg PoolConfig) *ProbePool {
	return &ProbePool{
		entries:     make([]*ProbeEntry, 0, cfg.MaxSize),
		maxSize:     cfg.MaxSize,
		maxAge:      cfg.MaxAge,
		reuseLimit:  cfg.ReuseLimit,
		QRIF:        cfg.QRIF,
		maxProbeAge: cfg.MaxProbeAge,
	}
}

// cleanup removes entries older than maxAge.
func (pool *ProbePool) cleanup() {
	i := 0
	size := len(pool.entries)
	for i < size {
		if time.Since(pool.entries[i].Timestamp) > pool.maxAge {
			pool.entries[i] = pool.entries[size-1]
			pool.entries[size-1] = nil
			size--
		} else {
			i++
		}
	}
	pool.entries = pool.entries[:size]
}

// removeAt removes the entry at index by swapping with the last element.
func (pool *ProbePool) removeAt(index int) {
	last := len(pool.entries) - 1
	pool.entries[index] = pool.entries[last]
	pool.entries[last] = nil
	pool.entries = pool.entries[:last]
}

// findOldestIndex returns the index of the entry with the smallest Timestamp.
func (pool *ProbePool) findOldestIndex() int {
	oldest := 0
	for i := 1; i < len(pool.entries); i++ {
		if pool.entries[i].Timestamp.Before(pool.entries[oldest].Timestamp) {
			oldest = i
		}
	}
	return oldest
}

// rifThreshold computes the RIF value at the QRIF quantile.
func (pool *ProbePool) rifThreshold() int64 {
	rifs := make([]int64, len(pool.entries))
	for i, e := range pool.entries {
		rifs[i] = e.RIF
	}
	slices.Sort(rifs)
	idx := int(pool.QRIF * float64(len(rifs)))
	if idx >= len(rifs) {
		idx = len(rifs) - 1
	}
	return rifs[idx]
}

// Add inserts a new probe entry. Evicts oldest if pool is full.
func (pool *ProbePool) Add(entry *ProbeEntry) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pool.cleanup()

	if len(pool.entries) >= pool.maxSize {
		pool.removeAt(pool.findOldestIndex())
	}

	entry.UsesLeft = pool.reuseLimit
	pool.entries = append(pool.entries, entry)
	observability.RecordPoolOccupancy(len(pool.entries))
}

// Size returns current pool occupancy.
func (pool *ProbePool) Size() int {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return len(pool.entries)
}

// IncrementRIF bumps the RIF on the entry matching this backend.
func (pool *ProbePool) IncrementRIF(backend string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for _, probe := range pool.entries {
		if probe.Backend == backend {
			probe.RIF++
			return
		}
	}
}

// RemoveWorst alternates between removing the oldest entry and the highest-load entry.
func (pool *ProbePool) RemoveWorst() {
	if len(pool.entries) == 0 {
		return
	}

	if pool.removeWorstAlt {
		// Strategy A: remove oldest
		pool.removeAt(pool.findOldestIndex())
	} else {
		// Strategy B: remove highest-load (reverse HCL)
		threshold := pool.rifThreshold()
		worstIndex := -1

		// Check if any entry is hot
		hasHot := false
		for _, e := range pool.entries {
			if e.RIF > threshold {
				hasHot = true
				break
			}
		}

		if hasHot {
			// Remove the hot entry with highest RIF
			var highestRIF int64 = -1
			for i, e := range pool.entries {
				if e.RIF > threshold && e.RIF > highestRIF {
					highestRIF = e.RIF
					worstIndex = i
				}
			}
		} else {
			// All cold: remove the cold entry with highest latency
			var highestLatency time.Duration = -1
			for i, e := range pool.entries {
				if e.Latency > highestLatency {
					highestLatency = e.Latency
					worstIndex = i
				}
			}
		}

		if worstIndex >= 0 {
			pool.removeAt(worstIndex)
		}
	}

	pool.removeWorstAlt = !pool.removeWorstAlt
}

// Select picks a backend from the pool using the HCL rule.
// Falls back to a random pick from allBackends if pool is too small.
func (pool *ProbePool) Select(allBackends []*controller.Endpoint) (*ProbeEntry, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pool.cleanup()
	pool.RemoveWorst()

	observability.RecordPoolOccupancy(len(pool.entries))

	// Eagerly purge stale entries whose probe data is older than maxProbeAge.
	// This keeps occupancy accurate and avoids stale entries consuming pool slots.
	if pool.maxProbeAge > 0 {
		i := 0
		size := len(pool.entries)
		for i < size {
			if time.Since(pool.entries[i].Timestamp) > pool.maxProbeAge {
				pool.entries[i] = pool.entries[size-1]
				pool.entries[size-1] = nil
				size--
			} else {
				i++
			}
		}
		pool.entries = pool.entries[:size]
	}

	observability.RecordPoolOccupancy(len(pool.entries))

	// Fallback: pool too small
	if len(pool.entries) < 2 {
		if len(allBackends) == 0 {
			return nil, fmt.Errorf("no backends available")
		}
		ep := allBackends[rand.Intn(len(allBackends))]
		return &ProbeEntry{
			Backend:   ep.String(),
			Endpoint:  ep,
			RIF:       0,
			Latency:   0,
			Timestamp: time.Now(),
			UsesLeft:  0, // synthetic, not reusable
		}, nil
	}

	// HCL selection over pool.entries entries only.
	// Compute threshold from pool.entries slice.
	rifs := make([]int64, len(pool.entries))
	for i, e := range pool.entries {
		rifs[i] = e.RIF
	}
	slices.Sort(rifs)
	idx := int(pool.QRIF * float64(len(rifs)))
	if idx >= len(rifs) {
		idx = len(rifs) - 1
	}
	threshold := rifs[idx]

	// Classify and find best
	var bestCold *ProbeEntry
	var bestColdIndex int = -1
	var bestHot *ProbeEntry
	var bestHotIndex int = -1
	allHot := true

	for i, e := range pool.entries {
		if e.RIF <= threshold {
			// Cold entry
			allHot = false
			if bestCold == nil || e.Latency < bestCold.Latency {
				bestCold = e
				bestColdIndex = i
			}
		} else {
			// Hot entry
			if bestHot == nil || e.RIF < bestHot.RIF {
				bestHot = e
				bestHotIndex = i
			}
		}
	}

	var selected *ProbeEntry
	var selectedIndex int

	if allHot {
		selected = bestHot
		selectedIndex = bestHotIndex
	} else {
		selected = bestCold
		selectedIndex = bestColdIndex
	}

	selected.UsesLeft--
	if selected.UsesLeft <= 0 {
		pool.removeAt(selectedIndex)
	}

	return selected, nil
}
