package metadata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func newDeleteBucketStore(t *testing.T) (*PebbleStore, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "maxiofs-delete-bucket-empty-*")
	require.NoError(t, err)

	store, err := NewPebbleStore(PebbleOptions{
		DataDir: dir,
		Logger:  logrus.StandardLogger(),
	})
	require.NoError(t, err)

	return store, func() {
		_ = store.Close()
		_ = os.RemoveAll(dir)
	}
}

func TestDeleteBucketIfEmptyIgnoresImplicitFolderMarkers(t *testing.T) {
	store, cleanup := newDeleteBucketStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	require.NoError(t, store.CreateBucket(ctx, &BucketMetadata{
		Name:      "implicit-only",
		TenantID:  "tenant-a",
		OwnerID:   "owner",
		OwnerType: "tenant",
		CreatedAt: now,
		UpdatedAt: now,
	}))

	bucketPath := "tenant-a/implicit-only"
	for _, key := range []string{"copied/", "nested/", "nested/deep/"} {
		require.NoError(t, store.PutObject(ctx, &ObjectMetadata{
			Bucket:       bucketPath,
			Key:          key,
			Size:         0,
			LastModified: now,
			ETag:         "d41d8cd98f00b204e9800998ecf8427e",
			ContentType:  "application/x-directory",
			Metadata:     map[string]string{"x-maxiofs-implicit-folder": "true"},
			CreatedAt:    now,
			UpdatedAt:    now,
		}))
	}

	require.NoError(t, store.DeleteBucketIfEmpty(ctx, "tenant-a", "implicit-only"))

	_, err := store.GetBucket(ctx, "tenant-a", "implicit-only")
	require.ErrorIs(t, err, ErrBucketNotFound)
	objects, _, err := store.ListObjects(ctx, bucketPath, "", "", 100)
	require.NoError(t, err)
	require.Empty(t, objects)
}

func TestDeleteBucketIfEmptyKeepsExplicitFolderObjectsNonEmpty(t *testing.T) {
	store, cleanup := newDeleteBucketStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	require.NoError(t, store.CreateBucket(ctx, &BucketMetadata{
		Name:      "explicit-folder",
		TenantID:  "tenant-a",
		OwnerID:   "owner",
		OwnerType: "tenant",
		CreatedAt: now,
		UpdatedAt: now,
	}))

	bucketPath := "tenant-a/explicit-folder"
	require.NoError(t, store.PutObject(ctx, &ObjectMetadata{
		Bucket:       bucketPath,
		Key:          "prefix/",
		Size:         0,
		LastModified: now,
		ETag:         "d41d8cd98f00b204e9800998ecf8427e",
		ContentType:  "application/x-directory",
		CreatedAt:    now,
		UpdatedAt:    now,
	}))

	require.ErrorIs(t, store.DeleteBucketIfEmpty(ctx, "tenant-a", "explicit-folder"), ErrBucketNotEmpty)

	_, err := store.GetBucket(ctx, "tenant-a", "explicit-folder")
	require.NoError(t, err)
}
