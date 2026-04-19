package pool

import (
	"prequal/controller"
	"testing"
	"time"
)

func TestRoutePools_IsolatesRoutes(t *testing.T) {
	pools := NewRoutePools(PoolConfig{
		MaxSize:     8,
		MaxAge:      time.Hour,
		ReuseLimit:  3,
		QRIF:        0.75,
		MaxProbeAge: time.Hour,
	}, time.Second)

	routeAEndpoint := controller.NewEndpoint("10.0.0.1", 8080)
	routeBEndpoint := controller.NewEndpoint("10.0.0.2", 8080)

	pools.Add("route-a", &ProbeEntry{
		Backend:   routeAEndpoint.String(),
		Endpoint:  routeAEndpoint,
		RIF:       1,
		Latency:   10 * time.Millisecond,
		Timestamp: time.Now(),
	})
	pools.Add("route-a", &ProbeEntry{
		Backend:   routeAEndpoint.String(),
		Endpoint:  routeAEndpoint,
		RIF:       1,
		Latency:   11 * time.Millisecond,
		Timestamp: time.Now(),
	})
	pools.Add("route-b", &ProbeEntry{
		Backend:   routeBEndpoint.String(),
		Endpoint:  routeBEndpoint,
		RIF:       1,
		Latency:   10 * time.Millisecond,
		Timestamp: time.Now(),
	})
	pools.Add("route-b", &ProbeEntry{
		Backend:   routeBEndpoint.String(),
		Endpoint:  routeBEndpoint,
		RIF:       1,
		Latency:   11 * time.Millisecond,
		Timestamp: time.Now(),
	})

	selectedA, err := pools.Select("route-a", []*controller.Endpoint{routeAEndpoint})
	if err != nil {
		t.Fatalf("route-a select failed: %v", err)
	}
	if selectedA.Endpoint.String() != routeAEndpoint.String() {
		t.Fatalf("route-a selected wrong backend: got %s want %s", selectedA.Endpoint.String(), routeAEndpoint.String())
	}

	selectedB, err := pools.Select("route-b", []*controller.Endpoint{routeBEndpoint})
	if err != nil {
		t.Fatalf("route-b select failed: %v", err)
	}
	if selectedB.Endpoint.String() != routeBEndpoint.String() {
		t.Fatalf("route-b selected wrong backend: got %s want %s", selectedB.Endpoint.String(), routeBEndpoint.String())
	}
}
