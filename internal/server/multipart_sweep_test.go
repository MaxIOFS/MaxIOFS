package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

func setupSweepTest(t *testing.T) (storage.Backend, metadata.Store, func()) {
	t.Helper()
	dataDir, err := os.MkdirTemp("", "multipart-sweep-*")
	require.NoError(t, err)

	backend, err := storage.NewBackend(storage.Config{Root: filepath.Join(dataDir, "objects")})
	require.NoError(t, err)

	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: dataDir, WALSyncInterval: -1})
	require.NoError(t, err)

	return backend, store, func() {
		store.Close()         //nolint:errcheck
		os.RemoveAll(dataDir) //nolint:errcheck
	}
}

func TestSweepDiscardsPartsWhoseUploadIsGone(t *testing.T) {
	backend, store, cleanup := setupSweepTest(t)
	defer cleanup()
	ctx := context.Background()

	// One upload the index knows about, one whose record is gone.
	require.NoError(t, store.CreateMultipartUpload(ctx, &metadata.MultipartUploadMetadata{
		UploadID: "live", Bucket: "b", Key: "k", Initiated: time.Now(),
	}))
	require.NoError(t, backend.PutPart(ctx, "live", 1, strings.NewReader("data"), nil))
	require.NoError(t, backend.PutPart(ctx, "orphan", 1, strings.NewReader("unreachable"), nil))

	sweepOrphanedUploads(ctx, backend, store)

	ids, err := backend.ListUploads(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"live"}, ids, "only the upload with no record should go")

	exists, err := backend.PartExists(ctx, "live", 1)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestSweepIsQuietWithNothingStored(t *testing.T) {
	backend, store, cleanup := setupSweepTest(t)
	defer cleanup()

	sweepOrphanedUploads(context.Background(), backend, store)

	ids, err := backend.ListUploads(context.Background())
	require.NoError(t, err)
	require.Empty(t, ids)
}

func refOf(bucket, key string) storage.ObjectRef {
	return storage.ObjectRef{Bucket: bucket, Key: key}
}
