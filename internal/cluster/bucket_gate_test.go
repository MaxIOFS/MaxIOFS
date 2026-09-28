package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Freezing a bucket refuses new writes at once and returns only when the
// writes under way have ended. Other buckets are not held.
func TestFreezeWaitsForTheWritesUnderWay(t *testing.T) {
	g := NewBucketWriteGate()
	first, ok := g.Enter("b")
	require.True(t, ok)
	second, ok := g.Enter("b")
	require.True(t, ok)

	frozen := make(chan error, 1)
	go func() { frozen <- g.Freeze(context.Background(), "b") }()
	require.Eventually(t, func() bool { return g.Frozen("b") }, time.Second, time.Millisecond)

	_, ok = g.Enter("b")
	require.False(t, ok, "a new write waits for the migration")
	other, ok := g.Enter("other")
	require.True(t, ok)
	other()

	first()
	first()
	select {
	case <-frozen:
		t.Fatal("the freeze returned with a write still running")
	case <-time.After(50 * time.Millisecond):
	}
	second()
	require.NoError(t, <-frozen)

	require.ErrorIs(t, g.Freeze(context.Background(), "b"), ErrBucketFrozen)
	g.Thaw("b")
	leave, ok := g.Enter("b")
	require.True(t, ok)
	leave()
}

// A freeze given up while it waits leaves the bucket taking writes, and the
// write it waited for can still end.
func TestFreezeGivenUpLeavesTheBucketWritable(t *testing.T) {
	g := NewBucketWriteGate()
	leave, ok := g.Enter("b")
	require.True(t, ok)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, g.Freeze(ctx, "b"), context.DeadlineExceeded)
	require.False(t, g.Frozen("b"))
	again, ok := g.Enter("b")
	require.True(t, ok)
	again()
	leave()

	require.NoError(t, g.Freeze(context.Background(), "b"), "nothing runs: the freeze is immediate")
}
