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

// A force delete removes the entry with DeleteBucket rather than
// DeleteBucketIfEmpty; it has to leave the same record behind.
func TestForceDeleteAlsoRecordsThePendingRemoval(t *testing.T) {
	backend, store, cleanup := setupSweepTest(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "forced", OwnerID: "u"}))
	require.NoError(t, backend.CreateBucket(ctx, "forced"))
	require.NoError(t, backend.Put(ctx, refOf("forced", "leftover.bin"), strings.NewReader("bytes"), nil))

	// What a force delete does to the index: drop the entry outright.
	require.NoError(t, store.DeleteBucket(ctx, "", "forced"))

	pending, err := store.PendingBucketRemovals(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"forced"}, pending,
		"a kill between the index write and the directory removal must leave something to finish")

	finishPendingBucketRemovals(ctx, backend, store)

	exists, err := backend.Exists(ctx, refOf("forced", "leftover.bin"))
	require.NoError(t, err)
	require.False(t, exists)

	pending, err = store.PendingBucketRemovals(ctx)
	require.NoError(t, err)
	require.Empty(t, pending)
}

// A bucket created where a deleted one left its directory, before creation
// removed such leftovers, owns the directory: the sweep keeps it and clears
// only the record.
func TestSweepKeepsTheDirectoryOfABucketCreatedSince(t *testing.T) {
	backend, store, cleanup := setupSweepTest(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "reborn", OwnerID: "u"}))
	require.NoError(t, backend.CreateBucket(ctx, "reborn"))
	require.NoError(t, store.DeleteBucket(ctx, "", "reborn"))
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "reborn", OwnerID: "u"}))
	require.NoError(t, backend.Put(ctx, refOf("reborn", "new.bin"), strings.NewReader("new bytes"), nil))

	finishPendingBucketRemovals(ctx, backend, store)

	exists, err := backend.Exists(ctx, refOf("reborn", "new.bin"))
	require.NoError(t, err)
	require.True(t, exists, "the bucket that lives there keeps its objects")
	pending, err := store.PendingBucketRemovals(ctx)
	require.NoError(t, err)
	require.Empty(t, pending)
}
