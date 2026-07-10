package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PutObject must preserve a caller-provided LastModified (HA replicas and the
// disaster-recovery rebuild depend on it) and only stamp the current time when
// the caller left it unset.
func TestPutObjectLastModifiedHandling(t *testing.T) {
	store, cleanup := setupObjectTestStore(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, store.CreateBucket(ctx, &BucketMetadata{
		Name:      "lm-bucket",
		CreatedAt: time.Now(),
	}))

	t.Run("preserves provided LastModified", func(t *testing.T) {
		original := time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)
		obj := &ObjectMetadata{
			Bucket:       "lm-bucket",
			Key:          "replicated.txt",
			Size:         42,
			ETag:         "etag-1",
			LastModified: original,
		}
		require.NoError(t, store.PutObject(ctx, obj))

		got, err := store.GetObject(ctx, "lm-bucket", "replicated.txt")
		require.NoError(t, err)
		assert.True(t, got.LastModified.Equal(original),
			"expected preserved LastModified %v, got %v", original, got.LastModified)
	})

	t.Run("stamps now when unset", func(t *testing.T) {
		before := time.Now().Add(-time.Second)
		obj := &ObjectMetadata{
			Bucket: "lm-bucket",
			Key:    "fresh.txt",
			Size:   1,
			ETag:   "etag-2",
		}
		require.NoError(t, store.PutObject(ctx, obj))

		got, err := store.GetObject(ctx, "lm-bucket", "fresh.txt")
		require.NoError(t, err)
		assert.True(t, got.LastModified.After(before),
			"expected LastModified stamped near now, got %v", got.LastModified)
	})

	t.Run("stamps now on non-positive epoch", func(t *testing.T) {
		before := time.Now().Add(-time.Second)
		obj := &ObjectMetadata{
			Bucket:       "lm-bucket",
			Key:          "epoch.txt",
			Size:         1,
			ETag:         "etag-3",
			LastModified: time.Unix(0, 0), // failed-parse sentinel, not a real timestamp
		}
		require.NoError(t, store.PutObject(ctx, obj))

		got, err := store.GetObject(ctx, "lm-bucket", "epoch.txt")
		require.NoError(t, err)
		assert.True(t, got.LastModified.After(before),
			"expected LastModified stamped near now, got %v", got.LastModified)
	})
}
