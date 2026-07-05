package bandwidth

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// TestLimiter_NilCases: no tenant or unlimited => nil limiter (no throttling).
func TestLimiter_NilCases(t *testing.T) {
	m := NewManager()
	if m.Limiter("", 1000) != nil {
		t.Fatal("empty tenant should yield nil limiter")
	}
	if m.Limiter("t1", 0) != nil {
		t.Fatal("zero cap (unlimited) should yield nil limiter")
	}
	if m.Limiter("t1", -5) != nil {
		t.Fatal("negative cap should yield nil limiter")
	}
}

// TestLimiter_SharedAndHotUpdate: same tenant shares one limiter; changing the
// cap updates that limiter's rate in place (hot update).
func TestLimiter_SharedAndHotUpdate(t *testing.T) {
	m := NewManager()
	a := m.Limiter("t1", 1<<20) // 1 MB/s
	b := m.Limiter("t1", 1<<20)
	if a != b {
		t.Fatal("same tenant must share the same limiter instance")
	}
	// Hot update to a new rate should mutate the same limiter, not replace it.
	c := m.Limiter("t1", 2<<20) // 2 MB/s
	if c != a {
		t.Fatal("changing the cap must reuse the same limiter instance")
	}
	if got := float64(a.Limit()); got != float64(2<<20) {
		t.Fatalf("limiter rate not hot-updated: got %v want %v", got, float64(2<<20))
	}
}

// TestThrottleReader_Rate: reading 2 MiB through a 1 MiB/s limiter must take
// ~2 seconds (real throttling of the core mechanism).
func TestThrottleReader_Rate(t *testing.T) {
	const rate = 1 << 20     // 1 MiB/s
	const total = 2 << 20    // 2 MiB
	m := NewManager()
	lim := m.Limiter("t1", rate)

	src := bytes.NewReader(make([]byte, total))
	tr := ThrottleReader(context.Background(), src, lim)

	start := time.Now()
	n, err := io.Copy(io.Discard, tr)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("copy failed: %v", err)
	}
	if n != total {
		t.Fatalf("short read: got %d want %d", n, total)
	}
	// Expect ~2s. Allow generous bounds (burst lets the first ~1s of data through
	// quickly, so >=1s is the meaningful floor; cap at 4s for CI slack).
	if elapsed < 900*time.Millisecond {
		t.Fatalf("throttle too fast: %v (expected ~2s for 2MiB @ 1MiB/s)", elapsed)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("throttle too slow: %v", elapsed)
	}
	t.Logf("2 MiB @ 1 MiB/s took %v", elapsed)
}

// TestThrottleReader_NilPassthrough: nil limiter returns the reader unchanged.
func TestThrottleReader_NilPassthrough(t *testing.T) {
	src := bytes.NewReader([]byte("hello"))
	if got := ThrottleReader(context.Background(), src, nil); got != io.Reader(src) {
		t.Fatal("nil limiter must return the original reader unchanged")
	}
}
