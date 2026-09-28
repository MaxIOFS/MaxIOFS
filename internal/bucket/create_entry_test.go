package bucket

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// stuckDirectoryBackend cannot remove a bucket directory, as when a file in it
// is held open.
type stuckDirectoryBackend struct {
	storage.Backend
}

func (stuckDirectoryBackend) DeleteBucket(context.Context, string) error {
	return errors.New("directory in use")
}

func setupCreateEntryTest(t *testing.T) (storage.Backend, metadata.Store) {
	t.Helper()
	dir, err := os.MkdirTemp("", "maxiofs-create-entry-*")
	require.NoError(t, err)
	backend, err := storage.NewBackend(config.StorageConfig{Backend: "filesystem", Root: filepath.Join(dir, "storage")})
	require.NoError(t, err)
	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: filepath.Join(dir, "metadata"), Logger: logrus.StandardLogger()})
	require.NoError(t, err)
	t.Cleanup(func() {
		store.Close()
		os.RemoveAll(dir)
	})
	return backend, store
}

// A bucket deleted while its directory could not be removed leaves the
// directory and a record of the removal. A bucket created at the same path
// starts empty: the directory is removed first, and when it cannot be, the
// bucket is not created.
func TestCreatingABucketRemovesWhatAnEarlierOneLeft(t *testing.T) {
	backend, store := setupCreateEntryTest(t)
	ctx := context.Background()
	leftover := storage.ObjectRef{Bucket: "tenant-1/reused", Key: "leftover.bin"}
	require.NoError(t, NewManager(backend, store).CreateBucket(ctx, "tenant-1", "reused", "u"))
	require.NoError(t, backend.Put(ctx, leftover, strings.NewReader("old bytes"), nil))
	require.NoError(t, store.DeleteBucket(ctx, "tenant-1", "reused"))

	err := NewManager(stuckDirectoryBackend{backend}, store).CreateBucket(ctx, "tenant-1", "reused", "u")
	require.Error(t, err)
	_, err = store.GetBucketByName(ctx, "reused")
	require.ErrorIs(t, err, metadata.ErrBucketNotFound, "not created over what it could not remove")

	require.NoError(t, NewManager(backend, store).CreateBucket(ctx, "tenant-1", "reused", "u"))
	exists, err := backend.Exists(ctx, leftover)
	require.NoError(t, err)
	require.False(t, exists, "the new bucket starts empty")
	pending, err := store.PendingBucketRemovals(ctx)
	require.NoError(t, err)
	require.Empty(t, pending, "nothing is left for the removal sweep to take from the new bucket")
}

// A bucket that lives at a path with a removal still recorded, as one created
// before creation removed leftovers, is not touched by another creation.
func TestCreatingABucketThatExistsTouchesNothing(t *testing.T) {
	backend, store := setupCreateEntryTest(t)
	ctx := context.Background()
	live := storage.ObjectRef{Bucket: "alive", Key: "kept.bin"}
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "alive", OwnerID: "u"}))
	require.NoError(t, store.DeleteBucket(ctx, "", "alive"))
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "alive", OwnerID: "u"}))
	require.NoError(t, backend.Put(ctx, live, strings.NewReader("live bytes"), nil))

	err := NewManager(backend, store).CreateBucket(ctx, "", "alive", "u")
	require.ErrorIs(t, err, ErrBucketAlreadyExists)
	exists, err := backend.Exists(ctx, live)
	require.NoError(t, err)
	require.True(t, exists)
}
