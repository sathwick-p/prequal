package loadbalancer

import (
	"sync"
	"time"
)

const defaultBufferSize = 64

type LatencyTracker struct {
	mu      sync.RWMutex
	buffers map[string]*CircularBuffer
}

func NewLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		buffers: make(map[string]*CircularBuffer),
	}
}

func (lt *LatencyTracker) Record(backend string, duration time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	buf, exists := lt.buffers[backend]
	if !exists {
		buf = NewCircularBuffer(defaultBufferSize)
		lt.buffers[backend] = buf
	}
	buf.Add(duration)
}

func (lt *LatencyTracker) Median(backend string) time.Duration {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	buf, exists := lt.buffers[backend]
	if !exists {
		return 0
	}
	return buf.Median()
}
