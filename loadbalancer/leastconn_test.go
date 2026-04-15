package loadbalancer

import (
	"prequal/controller"
	"testing"
)

func TestLeastConnections_SelectsLowestRIF(t *testing.T) {
	tracker := &RIFTracker{}
	lc := &LeastConnections{Tracker: tracker}

	ep1 := controller.NewEndpoint("10.0.0.1", 8080)
	ep2 := controller.NewEndpoint("10.0.0.2", 8080)
	ep3 := controller.NewEndpoint("10.0.0.3", 8080)

	tracker.Increase(ep1.String())
	tracker.Increase(ep1.String())
	tracker.Increase(ep2.String())
	// ep1: 2, ep2: 1, ep3: 0

	got, err := lc.Select([]*controller.Endpoint{ep1, ep2, ep3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ep3 {
		t.Errorf("expected ep3 (RIF=0), got %s", got.String())
	}
}

func TestLeastConnections_SingleEndpoint(t *testing.T) {
	tracker := &RIFTracker{}
	lc := &LeastConnections{Tracker: tracker}

	ep := controller.NewEndpoint("10.0.0.1", 8080)
	got, err := lc.Select([]*controller.Endpoint{ep})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ep {
		t.Errorf("expected the only endpoint, got %v", got)
	}
}

func TestLeastConnections_EmptyEndpointsReturnsError(t *testing.T) {
	tracker := &RIFTracker{}
	lc := &LeastConnections{Tracker: tracker}

	got, err := lc.Select([]*controller.Endpoint{})
	if err == nil {
		t.Fatalf("expected error for empty endpoints, got nil (result: %v)", got)
	}
}

func TestLeastConnections_TieBreakingIsNotAlwaysFirst(t *testing.T) {
	tracker := &RIFTracker{}
	lc := &LeastConnections{Tracker: tracker}

	ep1 := controller.NewEndpoint("10.0.0.1", 8080)
	ep2 := controller.NewEndpoint("10.0.0.2", 8080)
	// Both have RIF=0, so they are tied.

	const iterations = 1000
	firstCount := 0
	for i := 0; i < iterations; i++ {
		got, err := lc.Select([]*controller.Endpoint{ep1, ep2})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == ep1 {
			firstCount++
		}
	}

	// With random tie-breaking, it should not always pick ep1.
	// Allow a very wide margin: if ep1 is chosen 100% of the time, something is wrong.
	if firstCount == iterations {
		t.Errorf("tie-breaking always picked first endpoint over %d iterations", iterations)
	}
}
