package object

// An object whose metadata sidecar is gone.
//
// The sidecar carries the wrapped DEK and the `encrypted` flag. Without it the
// storage layer derives what it can from the bytes on disk, and that derived
// map has no `encrypted` key — so decryption was skipped and the raw ciphertext
// was streamed to the client with a nil error, under the plaintext
// Content-Length that the metadata store still reported. Nothing anywhere said
// anything was wrong; a backup tool would have written those bytes to disk as
// the restored file.
//
// The condition is reachable: `repairStagedCommit`'s roll-forward rename can
// fail persistently (a sharing violation on Windows, EACCES), and its own log
// line says the object "stays unreadable until repair succeeds" — but it was
// not unreadable, it was readable as garbage.

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

// removeSidecar deletes an object's `.metadata` file, leaving its bytes intact.
func removeSidecar(t *testing.T, root, bucket, key string) {
	t.Helper()
	path := filepath.Join(root, bucket, key+".metadata")
	require.NoError(t, os.Remove(path), "the sidecar must exist to be removed")
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
	removeSidecar(t, storageRootOf(t, manager), bucket, key)

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
// written before encryption existed have no sidecar either, and refusing those
// would take away data that is perfectly intact. What separates the two cases
// is whether the bytes on disk still match what the metadata store recorded.
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

	root := storageRootOf(t, manager)

	// Replace the stored bytes with the plaintext and drop the sidecar, which
	// is what an object from before encryption looks like on disk.
	require.NoError(t, os.WriteFile(filepath.Join(root, bucket, key), payload, 0o644))
	removeSidecar(t, root, bucket, key)

	// The metadata store still records the plaintext size and ETag, so the
	// bytes match and the object is served.
	_, reader, err := manager.GetObject(ctx, bucket, key)
	require.NoError(t, err, "an intact plaintext object must still be readable")
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()
	assert.Equal(t, payload, got)
}

// storageRootOf reaches the directory the filesystem backend writes into.
func storageRootOf(t *testing.T, om *objectManager) string {
	t.Helper()
	fs, ok := om.storage.(*storage.FilesystemBackend)
	require.True(t, ok, "these tests manipulate files, so they need the filesystem backend")
	return fs.GetRootPath()
}
