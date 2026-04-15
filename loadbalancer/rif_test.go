package loadbalancer

import (
	"sync"
	"testing"
)

func TestRIFTracker_IncrementDecrement(t *testing.T) {
	tracker := &RIFTracker{}
	tracker.Increase("backend1")
	tracker.Increase("backend1")
	if got := tracker.Get("backend1"); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
	tracker.Decrease("backend1")
	if got := tracker.Get("backend1"); got != 1 {
		t.Errorf("expected 1 after decrement, got %d", got)
	}
}

func TestRIFTracker_GetUnknownBackendReturnsZero(t *testing.T) {
	tracker := &RIFTracker{}
	if got := tracker.Get("unknown"); got != 0 {
		t.Errorf("expected 0 for unknown backend, got %d", got)
	}
}

func TestRIFTracker_DecreaseUnknownBackendNoOp(t *testing.T) {
	tracker := &RIFTracker{}
	// Should not panic
	tracker.Decrease("nonexistent")
	if got := tracker.Get("nonexistent"); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestRIFTracker_ConcurrentIncrementDecrement(t *testing.T) {
	tracker := &RIFTracker{}
	const goroutines = 100
	var wg sync.WaitGroup

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			tracker.Increase("backend")
		}()
	}
	wg.Wait()

	if got := tracker.Get("backend"); got != goroutines {
		t.Errorf("expected %d after concurrent increments, got %d", goroutines, got)
	}

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			tracker.Decrease("backend")
		}()
	}
	wg.Wait()

	if got := tracker.Get("backend"); got != 0 {
		t.Errorf("expected 0 after balanced inc/dec, got %d", got)
	}
}

func TestRIFTracker_CounterNeverNegativeAfterBalanced(t *testing.T) {
	tracker := &RIFTracker{}
	const n = 50
	var wg sync.WaitGroup

	// Increment n times first, then decrement n times
	for i := 0; i < n; i++ {
		tracker.Increase("backend")
	}
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			tracker.Decrease("backend")
		}()
	}
	wg.Wait()

	if got := tracker.Get("backend"); got != 0 {
		t.Errorf("expected 0 after balanced operations, got %d", got)
	}
}
