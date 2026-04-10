package roundrobin

import (
	"fmt"
	"prequal/controller"
	"sync/atomic"
)

type RoundRobin struct {
	counter uint64
}

func (rr *RoundRobin) Select(endpoints []*controller.Endpoint) (*controller.Endpoint, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("[PROXY] No endpoints available")
	}

	idx := atomic.AddUint64(&rr.counter, 1) - 1
	pick := idx % uint64(len(endpoints))
	return endpoints[pick], nil
}
