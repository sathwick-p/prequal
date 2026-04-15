package loadbalancer

import (
	"testing"
	"time"
)

func TestLatencyTracker_RecordAndMedian(t *testing.T) {
	lt := NewLatencyTracker()
	lt.Record("backend1", 10*time.Millisecond)
	lt.Record("backend1", 30*time.Millisecond)
	lt.Record("backend1", 20*time.Millisecond)
	// sorted: [10, 20, 30], count=3, median index=1 => 20ms
	got := lt.Median("backend1")
	if got != 20*time.Millisecond {
		t.Errorf("expected 20ms, got %v", got)
	}
}

func TestLatencyTracker_UnknownBackendReturnsZero(t *testing.T) {
	lt := NewLatencyTracker()
	if got := lt.Median("unknown"); got != 0 {
		t.Errorf("expected 0 for unknown backend, got %v", got)
	}
}

func TestLatencyTracker_MultipleBackendsTrackedIndependently(t *testing.T) {
	lt := NewLatencyTracker()
	lt.Record("backend1", 10*time.Millisecond)
	lt.Record("backend1", 20*time.Millisecond)
	lt.Record("backend1", 30*time.Millisecond)

	lt.Record("backend2", 100*time.Millisecond)
	lt.Record("backend2", 200*time.Millisecond)
	lt.Record("backend2", 300*time.Millisecond)

	// backend1: sorted [10,20,30], median index=1 => 20ms
	got1 := lt.Median("backend1")
	if got1 != 20*time.Millisecond {
		t.Errorf("backend1: expected 20ms, got %v", got1)
	}

	// backend2: sorted [100,200,300], median index=1 => 200ms
	got2 := lt.Median("backend2")
	if got2 != 200*time.Millisecond {
		t.Errorf("backend2: expected 200ms, got %v", got2)
	}
}
