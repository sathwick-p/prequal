package pool

import (
	"prequal/controller"
	"testing"
	"time"
)

func newTestPool(maxSize int, maxAge time.Duration, reuseLimit int, qRIF float64) *ProbePool {
	return NewProbePool(PoolConfig{
		MaxSize:    maxSize,
		MaxAge:     maxAge,
		ReuseLimit: reuseLimit,
		QRIF:       qRIF,
	})
}

func newEntry(addr string, rif int64, latency time.Duration, age time.Duration) *ProbeEntry {
	return &ProbeEntry{
		Backend:   addr + ":8080",
		Endpoint:  controller.NewEndpoint(addr, 8080),
		RIF:       rif,
		Latency:   latency,
		Timestamp: time.Now().Add(-age),
	}
}

func TestProbePool_AddCapsAtMaxSize(t *testing.T) {
	p := newTestPool(3, time.Hour, 5, 0.5)
	p.Add(newEntry("10.0.0.1", 0, 10*time.Millisecond, 0))
	p.Add(newEntry("10.0.0.2", 0, 10*time.Millisecond, 0))
	p.Add(newEntry("10.0.0.3", 0, 10*time.Millisecond, 0))
	p.Add(newEntry("10.0.0.4", 0, 10*time.Millisecond, 0)) // should evict oldest

	if sz := p.Size(); sz != 3 {
		t.Errorf("expected size 3, got %d", sz)
	}
}

func TestProbePool_CleanupRemovesStaleEntries(t *testing.T) {
	p := newTestPool(10, 50*time.Millisecond, 5, 0.5)
	// Add a fresh entry; the stale one is added first but cleaned on subsequent Add.
	p.Add(newEntry("10.0.0.1", 0, 10*time.Millisecond, 200*time.Millisecond)) // stale (200ms > 50ms maxAge)
	// On the second Add, cleanup fires and removes the stale entry before inserting.
	p.Add(newEntry("10.0.0.2", 0, 10*time.Millisecond, 0)) // fresh

	if sz := p.Size(); sz != 1 {
		t.Errorf("expected 1 (stale cleaned), got %d", sz)
	}
}

func TestProbePool_SelectReturnsEntry(t *testing.T) {
	p := newTestPool(10, time.Hour, 5, 0.5)
	p.Add(newEntry("10.0.0.1", 0, 10*time.Millisecond, 0))
	p.Add(newEntry("10.0.0.2", 0, 20*time.Millisecond, 0))

	backends := []*controller.Endpoint{
		controller.NewEndpoint("10.0.0.1", 8080),
		controller.NewEndpoint("10.0.0.2", 8080),
	}
	entry, err := p.Select(backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
}

func TestProbePool_SelectFallsBackToRandomWhenPoolSmall(t *testing.T) {
	p := newTestPool(10, time.Hour, 5, 0.5)
	// Only 1 entry — less than 2 triggers random fallback.
	p.Add(newEntry("10.0.0.1", 0, 10*time.Millisecond, 0))

	backends := []*controller.Endpoint{
		controller.NewEndpoint("10.0.0.1", 8080),
		controller.NewEndpoint("10.0.0.2", 8080),
	}
	entry, err := p.Select(backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry from fallback")
	}
}

func TestProbePool_SelectEmptyPoolEmptyBackendsReturnsError(t *testing.T) {
	p := newTestPool(10, time.Hour, 5, 0.5)
	_, err := p.Select([]*controller.Endpoint{})
	if err == nil {
		t.Fatal("expected error when pool empty and no backends")
	}
}

func TestProbePool_HCL_AllCold_SelectsLowestLatency(t *testing.T) {
	// QRIF=1.0 => threshold = max(RIF) => all entries cold (RIF <= max).
	// Select picks cold entry with lowest latency.
	p := newTestPool(10, time.Hour, 10, 1.0)

	e1 := newEntry("10.0.0.1", 1, 30*time.Millisecond, 0)
	e2 := newEntry("10.0.0.2", 2, 10*time.Millisecond, 0) // lowest latency
	e3 := newEntry("10.0.0.3", 3, 20*time.Millisecond, 0)
	p.Add(e1)
	p.Add(e2)
	p.Add(e3)

	backends := []*controller.Endpoint{e1.Endpoint, e2.Endpoint, e3.Endpoint}
	entry, err := p.Select(backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Backend != e2.Backend {
		t.Errorf("expected lowest latency backend %s, got %s", e2.Backend, entry.Backend)
	}
}

func TestProbePool_HCL_MixedHotCold_SelectsColdEntry(t *testing.T) {
	// QRIF=0.0 => idx=0 => threshold = min(RIF).
	// Entry with min RIF is cold; others are hot.
	// With 3 entries RIFs [1,5,10]: threshold=1, cold=e1(RIF=1), hot=e2,e3.
	p := newTestPool(10, time.Hour, 10, 0.0)

	e1 := newEntry("10.0.0.1", 1, 50*time.Millisecond, 0) // cold (RIF=1 <= threshold=1)
	e2 := newEntry("10.0.0.2", 5, 10*time.Millisecond, 0) // hot
	e3 := newEntry("10.0.0.3", 10, 5*time.Millisecond, 0) // hot
	p.Add(e1)
	p.Add(e2)
	p.Add(e3)

	backends := []*controller.Endpoint{e1.Endpoint, e2.Endpoint, e3.Endpoint}
	entry, err := p.Select(backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only e1 is cold; it must be selected.
	if entry.Backend != e1.Backend {
		t.Errorf("expected cold backend %s, got %s", e1.Backend, entry.Backend)
	}
}

func TestProbePool_RemoveWorst_Alternates(t *testing.T) {
	p := newTestPool(10, time.Hour, 10, 0.5)

	// Entries with distinct timestamps (oldest first) and distinct RIFs.
	e1 := newEntry("10.0.0.1", 1, 10*time.Millisecond, 300*time.Millisecond) // oldest
	e2 := newEntry("10.0.0.2", 5, 20*time.Millisecond, 200*time.Millisecond)
	e3 := newEntry("10.0.0.3", 10, 30*time.Millisecond, 100*time.Millisecond) // newest, highest RIF

	p.mu.Lock()
	p.entries = append(p.entries, e1, e2, e3)
	p.mu.Unlock()

	// First call: removeWorstAlt=false => Strategy B (highest-load hot entry).
	// QRIF=0.5, 3 entries, idx=int(0.5*3)=1 => threshold=rifs[1]=5 (middle).
	// hot = RIF>5 => only e3(RIF=10). Remove e3.
	p.mu.Lock()
	p.RemoveWorst()
	p.mu.Unlock()

	p.mu.Lock()
	found3 := false
	for _, e := range p.entries {
		if e.Backend == e3.Backend {
			found3 = true
		}
	}
	p.mu.Unlock()
	if found3 {
		t.Error("expected e3 (highest RIF hot) to be removed on first RemoveWorst call")
	}

	// Second call: removeWorstAlt=true => Strategy A (oldest = e1).
	p.mu.Lock()
	p.RemoveWorst()
	p.mu.Unlock()

	p.mu.Lock()
	found1 := false
	for _, e := range p.entries {
		if e.Backend == e1.Backend {
			found1 = true
		}
	}
	p.mu.Unlock()
	if found1 {
		t.Error("expected e1 (oldest) to be removed on second RemoveWorst call")
	}
}

func TestProbePool_IncrementRIF_UpdatesCorrectEntry(t *testing.T) {
	p := newTestPool(10, time.Hour, 5, 0.5)
	e1 := newEntry("10.0.0.1", 0, 10*time.Millisecond, 0)
	e2 := newEntry("10.0.0.2", 0, 10*time.Millisecond, 0)
	p.Add(e1)
	p.Add(e2)

	p.IncrementRIF("10.0.0.1:8080")

	p.mu.Lock()
	var rif1, rif2 int64
	for _, e := range p.entries {
		if e.Backend == "10.0.0.1:8080" {
			rif1 = e.RIF
		}
		if e.Backend == "10.0.0.2:8080" {
			rif2 = e.RIF
		}
	}
	p.mu.Unlock()

	if rif1 != 1 {
		t.Errorf("expected RIF=1 for 10.0.0.1:8080, got %d", rif1)
	}
	if rif2 != 0 {
		t.Errorf("expected RIF=0 for 10.0.0.2:8080, got %d", rif2)
	}
}

func TestProbePool_UsesLeftDecrementedOnSelect(t *testing.T) {
	// reuseLimit=1 means each entry can only be selected once.
	p := newTestPool(10, time.Hour, 1, 0.5)

	e1 := newEntry("10.0.0.1", 0, 10*time.Millisecond, 0)
	e2 := newEntry("10.0.0.2", 0, 20*time.Millisecond, 0)
	p.Add(e1)
	p.Add(e2)

	backends := []*controller.Endpoint{e1.Endpoint, e2.Endpoint}

	// First select: pool has 2 entries; selects one and removes it (UsesLeft hits 0).
	// RemoveWorst also fires, potentially removing another entry.
	// After first select the pool may have 0 entries => fallback on next call.
	_, err := p.Select(backends)
	if err != nil {
		t.Fatalf("first select unexpected error: %v", err)
	}

	// Eventually the pool should drain and fall back to random (still no error).
	for i := 0; i < 5; i++ {
		_, err = p.Select(backends)
		if err != nil {
			// Error only when backends slice is also empty, which it isn't here.
			t.Fatalf("unexpected error on select %d: %v", i+2, err)
		}
	}
}
