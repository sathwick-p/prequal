package loadbalancer

import (
	"fmt"
	"math/rand"
	"prequal/controller"
)

type LeastConnections struct {
	Tracker *RIFTracker
}

func (lc *LeastConnections) Select(endpoints []*controller.Endpoint) (*controller.Endpoint, error) {
	if len(endpoints) == 1 {
		return endpoints[0], nil
	}
	if len(endpoints) > 1 {
		bestEndpoint := endpoints[0]
		bestRIF := lc.Tracker.Get(bestEndpoint.String())
		for i := 1; i < len(endpoints); i++ {
			rif := lc.Tracker.Get(endpoints[i].String())
			if rif < bestRIF {
				bestEndpoint = endpoints[i]
				bestRIF = rif
			} else if rif == bestRIF {
				n := rand.Intn(2)
				if n == 0 {
					bestEndpoint = endpoints[i]
				}
			}
		}
		return bestEndpoint, nil
	}
	return nil, fmt.Errorf("[PROXY] No endpoints available")
}
