package cluster

import (
	"context"
	"errors"
	"sync"
)

// ErrBucketFrozen: the bucket is being migrated and takes no writes until the
// migration ends.
var ErrBucketFrozen = errors.New("the bucket is being migrated to another node and takes no writes until it finishes")

// BucketWriteGate holds the writes to a bucket while it is migrated. Every
// write passes through the gate for as long as it runs. Freezing a bucket
// refuses new writes and waits for the running ones to end, so a migration
// copies a bucket nothing changes under it.
type BucketWriteGate struct {
	mu       sync.Mutex
	frozen   map[string]bool
	inflight map[string]int
	idle     map[string]chan struct{} // closed when a frozen bucket's last running write ends
}

// NewBucketWriteGate returns a gate with no bucket frozen.
func NewBucketWriteGate() *BucketWriteGate {
	return &BucketWriteGate{
		frozen:   make(map[string]bool),
		inflight: make(map[string]int),
		idle:     make(map[string]chan struct{}),
	}
}

// Enter admits a write to bucket. It returns false while the bucket is frozen;
// otherwise the caller calls leave when the write ends.
func (g *BucketWriteGate) Enter(bucket string) (leave func(), ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.frozen[bucket] {
		return nil, false
	}
	g.inflight[bucket]++
	var once sync.Once
	return func() { once.Do(func() { g.leave(bucket) }) }, true
}

func (g *BucketWriteGate) leave(bucket string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inflight[bucket]--; g.inflight[bucket] > 0 {
		return
	}
	delete(g.inflight, bucket)
	if idle, ok := g.idle[bucket]; ok {
		close(idle)
		delete(g.idle, bucket)
	}
}

// Freeze refuses new writes to bucket and waits for the running ones to end.
// It fails with ErrBucketFrozen when the bucket is frozen already, and with
// the context's error when ctx ends first, leaving the bucket as it was.
func (g *BucketWriteGate) Freeze(ctx context.Context, bucket string) error {
	g.mu.Lock()
	if g.frozen[bucket] {
		g.mu.Unlock()
		return ErrBucketFrozen
	}
	g.frozen[bucket] = true
	if g.inflight[bucket] == 0 {
		g.mu.Unlock()
		return nil
	}
	idle := make(chan struct{})
	g.idle[bucket] = idle
	g.mu.Unlock()

	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		g.mu.Lock()
		if g.idle[bucket] == idle {
			delete(g.idle, bucket)
		}
		delete(g.frozen, bucket)
		g.mu.Unlock()
		return ctx.Err()
	}
}

// Thaw lets writes to bucket in again.
func (g *BucketWriteGate) Thaw(bucket string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.frozen, bucket)
}

// Frozen reports whether bucket takes no writes.
func (g *BucketWriteGate) Frozen(bucket string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.frozen[bucket]
}
