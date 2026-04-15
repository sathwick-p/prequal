package loadbalancer

import (
	"slices"
	"time"
)

type CircularBuffer struct {
	data  []time.Duration
	pos   int
	count int
	size  int
}

func NewCircularBuffer(size int) *CircularBuffer {
	return &CircularBuffer{
		data: make([]time.Duration, size),
		size: size,
	}
}

func (c *CircularBuffer) Add(d time.Duration) {
	c.data[c.pos] = d
	c.pos = (c.pos + 1) % c.size
	if c.count < c.size {
		c.count++
	}
}

func (c *CircularBuffer) Median() time.Duration {
	if c.count == 0 {
		return 0
	}
	sorted := make([]time.Duration, c.count)
	copy(sorted, c.data[:c.count])
	slices.Sort(sorted)
	return sorted[c.count/2]
}
