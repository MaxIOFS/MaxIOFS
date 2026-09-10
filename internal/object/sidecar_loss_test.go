package object

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// objectFilePath resolves where an object's bytes sit under the storage root.
func objectFilePath(t *testing.T, om *objectManager, bucket, key string) string {
	t.Helper()
	fs, ok := om.storage.(*storage.FilesystemBackend)
	require.True(t, ok, "these tests manipulate files, so they need the filesystem backend")
	rel := fs.ObjectPath(storage.ObjectRef{Bucket: bucket, Key: key})
	return filepath.Join(fs.GetRootPath(), filepath.FromSlash(rel))
}

// removeSidecar deletes an object's `.metadata` file, leaving its bytes intact.
func removeSidecar(t *testing.T, om *objectManager, bucket, key string) {
	t.Helper()
	require.NoError(t, os.Remove(objectFilePath(t, om, bucket, key)+".metadata"),
		"the sidecar must exist to be removed")
}

func TestSidecarLoss_CiphertextIsNotServedAsTheObject(t *testing.T) {
	manager, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket = "integrity"
	const key = "report.bin"
	payload := bytes.Repeat([]byte("A"), 1280)

	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: bucket, TenantID: "tenant-1", OwnerID: "user-1"}))

	_, err := manager.PutObject(ctx, bucket, key, bytes.NewReader(payload), nil)
	require.NoError(t, err)

	// Confirm it reads back correctly first, so the test fails for the right
	// reason if the write path ever changes.
	obj, reader, err := manager.GetObject(ctx, bucket, key)
	require.NoError(t, err)
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()
	require.Equal(t, payload, got)
	recordedSize := obj.Size

	// Now lose the sidecar, exactly as a failed roll-forward would.
	removeSidecar(t, manager, bucket, key)

	_, reader, err = manager.GetObject(ctx, bucket, key)
	if err == nil {
		served, _ := io.ReadAll(reader)
		reader.Close()
		t.Fatalf("served %d bytes with no error for an object recorded as %d bytes; "+
			"the ciphertext was returned as though it were the object",
			len(served), recordedSize)
	}
	assert.Contains(t, strings.ToLower(err.Error()), "unreadable",
		"the refusal must say the object cannot be read, not that it is missing")
}

// TestSidecarLoss_PlaintextLegacyObjectsStillRead is the other half: objects
func TestSidecarLoss_PlaintextLegacyObjectsStillRead(t *testing.T) {
	manager, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket = "legacyread"
	const key = "old.txt"
	payload := []byte("written before encryption existed")

	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: bucket, TenantID: "tenant-1", OwnerID: "user-1"}))

	_, err := manager.PutObject(ctx, bucket, key, bytes.NewReader(payload), nil)
	require.NoError(t, err)

	// Replace the stored bytes with the plaintext and drop the sidecar, which
	// is what an object from before encryption looks like on disk.
	require.NoError(t, os.WriteFile(objectFilePath(t, manager, bucket, key), payload, 0o644))
	removeSidecar(t, manager, bucket, key)

	// The metadata store still records the plaintext size and ETag, so the
	// bytes match and the object is served.
	_, reader, err := manager.GetObject(ctx, bucket, key)
	require.NoError(t, err, "an intact plaintext object must still be readable")
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()
	assert.Equal(t, payload, got)
}
