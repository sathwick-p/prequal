package loadbalancer

import (
	"testing"
	"time"
)

func TestCircularBuffer_EmptyReturnsZero(t *testing.T) {
	buf := NewCircularBuffer(4)
	if got := buf.Median(); got != 0 {
		t.Errorf("expected 0 for empty buffer, got %v", got)
	}
}

func TestCircularBuffer_SingleEntry(t *testing.T) {
	buf := NewCircularBuffer(4)
	buf.Add(10 * time.Millisecond)
	if got := buf.Median(); got != 10*time.Millisecond {
		t.Errorf("expected 10ms, got %v", got)
	}
}

func TestCircularBuffer_AddAndMedian(t *testing.T) {
	buf := NewCircularBuffer(4)
	buf.Add(1 * time.Millisecond)
	buf.Add(3 * time.Millisecond)
	buf.Add(2 * time.Millisecond)
	// sorted: [1, 2, 3], count=3, median index = 3/2 = 1 => 2ms
	got := buf.Median()
	if got != 2*time.Millisecond {
		t.Errorf("expected 2ms, got %v", got)
	}
}

func TestCircularBuffer_FillCompletely(t *testing.T) {
	buf := NewCircularBuffer(4)
	buf.Add(4 * time.Millisecond)
	buf.Add(2 * time.Millisecond)
	buf.Add(1 * time.Millisecond)
	buf.Add(3 * time.Millisecond)
	// sorted: [1, 2, 3, 4], count=4, median index = 4/2 = 2 => 3ms
	got := buf.Median()
	if got != 3*time.Millisecond {
		t.Errorf("expected 3ms, got %v", got)
	}
}

func TestCircularBuffer_OverflowReplacesOldest(t *testing.T) {
	buf := NewCircularBuffer(3)
	buf.Add(10 * time.Millisecond)
	buf.Add(20 * time.Millisecond)
	buf.Add(30 * time.Millisecond)
	// Now overflow: replace slot 0 (the oldest)
	buf.Add(5 * time.Millisecond)
	// Buffer now contains: [5, 20, 30], count stays at 3
	// sorted: [5, 20, 30], median index = 3/2 = 1 => 20ms
	got := buf.Median()
	if got != 20*time.Millisecond {
		t.Errorf("expected 20ms after overflow, got %v", got)
	}
}
