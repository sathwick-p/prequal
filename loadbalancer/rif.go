package loadbalancer

import (
	"sync"
	"sync/atomic"
)

type RIFTracker struct {
	counters sync.Map
}

func (tracker *RIFTracker) Increase(backend string) {
	newCounter := &atomic.Int64{}
	actual, _ := tracker.counters.LoadOrStore(backend, newCounter)
	counter := actual.(*atomic.Int64)
	counter.Add(1)
}

func (tracker *RIFTracker) Decrease(backend string) {
	val, ok := tracker.counters.Load(backend)
	if !ok {
		return
	}
	actual := val.(*atomic.Int64)
	actual.Add(-1)
}

func (tracker *RIFTracker) Get(backend string) int64 {
	val, ok := tracker.counters.Load(backend)
	if !ok {
		return 0
	}
	counter := val.(*atomic.Int64)
	return counter.Load()
}
