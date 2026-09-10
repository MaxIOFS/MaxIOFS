package server

import (
	"context"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

func TestPendingBucketRemovalIsFinishedAtStartup(t *testing.T) {
	backend, store, cleanup := setupSweepTest(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "doomed", OwnerID: "u"}))
	require.NoError(t, backend.CreateBucket(ctx, "doomed"))
	require.NoError(t, backend.Put(ctx, refOf("doomed", "leftover.bin"), strings.NewReader("bytes"), nil))

	// The deletion is ordered and recorded, but the directory survives it.
	require.NoError(t, store.DeleteBucketIfEmpty(ctx, "", "doomed"))

	pending, err := store.PendingBucketRemovals(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"doomed"}, pending)

	finishPendingBucketRemovals(ctx, backend, store)

	exists, err := backend.Exists(ctx, refOf("doomed", "leftover.bin"))
	require.NoError(t, err)
	require.False(t, exists, "the objects of a bucket whose removal was ordered should be gone")

	pending, err = store.PendingBucketRemovals(ctx)
	require.NoError(t, err)
	require.Empty(t, pending, "the removal record should be cleared once the directory is gone")
}

func TestBucketRemovalRecordIsClearedOnANormalDelete(t *testing.T) {
	backend, store, cleanup := setupSweepTest(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "plain", OwnerID: "u"}))
	require.NoError(t, backend.CreateBucket(ctx, "plain"))
	require.NoError(t, store.DeleteBucketIfEmpty(ctx, "", "plain"))
	require.NoError(t, backend.DeleteBucket(ctx, "plain"))
	require.NoError(t, store.ClearBucketRemoval(ctx, "plain"))

	pending, err := store.PendingBucketRemovals(ctx)
	require.NoError(t, err)
	require.Empty(t, pending)
}
