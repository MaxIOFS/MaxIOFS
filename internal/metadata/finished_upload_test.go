package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Finishing an upload removes it and its parts and keeps what answers it
// again, in one commit; what is kept goes once it is a day old.
func TestAFinishedUploadIsKeptForADay(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	start := func(id string) {
		require.NoError(t, store.CreateMultipartUpload(ctx, &MultipartUploadMetadata{UploadID: id, Bucket: "b", Key: id}))
		require.NoError(t, store.PutPart(ctx, &PartMetadata{UploadID: id, PartNumber: 1, Size: 1, ETag: "e"}))
	}
	start("old")
	start("recent")
	require.NoError(t, store.FinishMultipartUpload(ctx, "old", &FinishedUpload{Bucket: "b", Key: "old", ETag: "x",
		FinishedAt: time.Now().Add(-finishedUploadRetention - time.Minute)}))
	require.NoError(t, store.FinishMultipartUpload(ctx, "recent", &FinishedUpload{Bucket: "b", Key: "recent", Aborted: true}))

	for _, id := range []string{"old", "recent"} {
		_, err := store.GetMultipartUpload(ctx, id)
		assert.ErrorIs(t, err, ErrUploadNotFound, id)
		parts, err := store.ListParts(ctx, id)
		require.NoError(t, err)
		assert.Empty(t, parts, id)
	}
	recent, err := store.GetFinishedUpload(ctx, "recent")
	require.NoError(t, err)
	assert.Equal(t, "recent", recent.UploadID)
	assert.True(t, recent.Aborted)
	assert.False(t, recent.FinishedAt.IsZero())

	assert.ErrorIs(t, store.FinishMultipartUpload(ctx, "recent", &FinishedUpload{}), ErrUploadNotFound,
		"an upload is finished once")

	store.cleanupStaleMultipartUploads()
	_, err = store.GetFinishedUpload(ctx, "old")
	assert.ErrorIs(t, err, ErrUploadNotFound)
	_, err = store.GetFinishedUpload(ctx, "recent")
	assert.NoError(t, err)
}
