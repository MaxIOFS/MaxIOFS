package object

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flipByte changes one byte in the middle of the file of a version of key, as a
// failing disk does.
func flipByte(t *testing.T, om *objectManager, bucket, key, versionID string) {
	t.Helper()
	fs, ok := om.storage.(*storage.FilesystemBackend)
	require.True(t, ok)
	rel := fs.ObjectPath(storage.ObjectRef{Bucket: bucket, Key: key, VersionID: versionID})
	path := filepath.Join(fs.GetRootPath(), filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)/2] ^= 0x40
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

// writeInParts stores key as a multipart upload of two parts, as a client does.
func writeInParts(t *testing.T, om *objectManager, bucket, key string) *Object {
	t.Helper()
	ctx := context.Background()
	upload, err := om.CreateMultipartUpload(ctx, bucket, key, http.Header{})
	require.NoError(t, err)
	var parts []Part
	for n, size := range []int{5 << 20, 1 << 20} {
		part, err := om.UploadPart(ctx, upload.UploadID, n+1, bytes.NewReader(bytes.Repeat([]byte{byte('a' + n)}, size)))
		require.NoError(t, err)
		parts = append(parts, *part)
	}
	written, err := om.CompleteMultipartUpload(ctx, upload.UploadID, parts)
	require.NoError(t, err)
	return written
}

// A byte changed on disk is corruption: the scrubber alerts of that status, and
// data at rest is encrypted, so what shows it is the read failing, not the MD5.
func TestABitFlippedOnDiskIsCorruption(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	putTestObject(t, om, store, "rot", "whole", bytes.Repeat([]byte("x"), 200<<10))
	flipByte(t, om, "rot", "whole", "")

	result, err := om.VerifyObjectIntegrity(context.Background(), "rot", "whole")
	require.NoError(t, err)
	assert.Equal(t, IntegrityCorrupted, result.Status, "%+v", result)
	assert.Contains(t, result.Reason, "does not read back")
}

// sidecarPath is the path of the sidecar of key's current write.
func sidecarPath(t *testing.T, om *objectManager, bucket, key string) string {
	t.Helper()
	fs, ok := om.storage.(*storage.FilesystemBackend)
	require.True(t, ok)
	rel := fs.ObjectPath(storage.ObjectRef{Bucket: bucket, Key: key})
	return filepath.Join(fs.GetRootPath(), filepath.FromSlash(rel)) + ".metadata"
}

// The wrapped DEK is in the sidecar: an object that lost it is lost, and the
// scrubber only alerts of corruption.
func TestAnObjectThatLostItsSidecarIsCorruption(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	putTestObject(t, om, store, "rot", "whole", bytes.Repeat([]byte("x"), 200<<10))
	require.NoError(t, os.Remove(sidecarPath(t, om, "rot", "whole")))

	result, err := om.VerifyObjectIntegrity(context.Background(), "rot", "whole")
	require.NoError(t, err)
	assert.Equal(t, IntegrityCorrupted, result.Status, "%+v", result)
	assert.Contains(t, result.Reason, "sidecar is missing")
}

// A wrapped DEK that does not open is the same loss.
func TestAnObjectWhoseKeyDoesNotOpenIsCorruption(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	putTestObject(t, om, store, "rot", "whole", bytes.Repeat([]byte("x"), 200<<10))
	path := sidecarPath(t, om, "rot", "whole")
	sidecar, err := os.ReadFile(path)
	require.NoError(t, err)
	field := []byte(`"wrapped-dek":"`)
	at := bytes.Index(sidecar, field)
	require.Positive(t, at, "%s", sidecar)
	at += len(field)
	if sidecar[at] == '0' {
		sidecar[at] = '1'
	} else {
		sidecar[at] = '0'
	}
	require.NoError(t, os.WriteFile(path, sidecar, 0o644))

	result, err := om.VerifyObjectIntegrity(context.Background(), "rot", "whole")
	require.NoError(t, err)
	assert.Equal(t, IntegrityCorrupted, result.Status, "%+v", result)
	assert.Contains(t, result.Reason, "decryption key")
}

// A cancelled scan says nothing of the object it was reading.
func TestACancelledScanIsNoCorruption(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	putTestObject(t, om, store, "rot", "whole", bytes.Repeat([]byte("x"), 200<<10))
	flipByte(t, om, "rot", "whole", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := om.VerifyObjectIntegrity(ctx, "rot", "whole")
	require.NoError(t, err)
	assert.Equal(t, IntegrityError, result.Status, "%+v", result)
}

// An object written in parts is verified too: its composite ETag is recomputed
// from the sizes of its parts.
func TestAnObjectWrittenInPartsIsVerified(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "parts", OwnerID: "u"}))
	writeInParts(t, om, "parts", "big")

	result, err := om.VerifyObjectIntegrity(ctx, "parts", "big")
	require.NoError(t, err)
	assert.Equal(t, IntegrityOK, result.Status, "%+v", result)
	assert.Equal(t, result.StoredETag, result.ComputedETag)

	flipByte(t, om, "parts", "big", "")
	result, err = om.VerifyObjectIntegrity(ctx, "parts", "big")
	require.NoError(t, err)
	assert.Equal(t, IntegrityCorrupted, result.Status, "%+v", result)
}

// Part sizes that do not lead to the ETag stored are a mismatch, not a pass.
func TestPartsThatDoNotMatchTheETagAreCorruption(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "parts", OwnerID: "u"}))
	writeInParts(t, om, "parts", "big")
	entry, err := store.GetObject(ctx, "parts", "big")
	require.NoError(t, err)
	entry.ETag = fmt.Sprintf("%032x-2", 7)
	require.NoError(t, store.PutObject(ctx, entry))

	result, err := om.VerifyObjectIntegrity(ctx, "parts", "big")
	require.NoError(t, err)
	assert.Equal(t, IntegrityCorrupted, result.Status, "%+v", result)
	assert.Contains(t, result.Reason, "mismatch")
}

// An object written in parts before their sizes were kept reads back
// authenticated and with its size: nothing else can be asked of it.
func TestAnObjectWrittenInPartsWithoutItsSizesReadsBack(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "parts", OwnerID: "u"}))
	writeInParts(t, om, "parts", "old")
	entry, err := store.GetObject(ctx, "parts", "old")
	require.NoError(t, err)
	entry.Parts = nil
	require.NoError(t, store.PutObject(ctx, entry))

	result, err := om.VerifyObjectIntegrity(ctx, "parts", "old")
	require.NoError(t, err)
	assert.Equal(t, IntegrityOK, result.Status, "%+v", result)

	flipByte(t, om, "parts", "old", "")
	result, err = om.VerifyObjectIntegrity(ctx, "parts", "old")
	require.NoError(t, err)
	assert.Equal(t, IntegrityCorrupted, result.Status, "%+v", result)
}

// A scan covers every version, not the latest alone: an older one is what a
// restore may need, and a key whose latest is a delete marker still holds data.
func TestAScanCoversTheVersionsThatAreNotTheLatest(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "kept")
	defer cleanup()
	ctx := context.Background()
	old := put(t, om, "kept", "doc", "first")
	put(t, om, "kept", "doc", "second")
	hidden := put(t, om, "kept", "gone", "hidden by a marker")
	_, err := om.DeleteObject(ctx, "kept", "gone", false)
	require.NoError(t, err)

	report, err := om.VerifyBucketIntegrity(ctx, "kept", "", "", 100)
	require.NoError(t, err)
	assert.Equal(t, 3, report.OK, "two versions of doc and the one under the marker")
	assert.Equal(t, 1, report.Skipped, "the marker")

	flipByte(t, om, "kept", "doc", old)
	flipByte(t, om, "kept", "gone", hidden)
	report, err = om.VerifyBucketIntegrity(ctx, "kept", "", "", 100)
	require.NoError(t, err)
	assert.Equal(t, 2, report.Corrupted)
	require.Len(t, report.Issues, 2)
	versions := []string{report.Issues[0].VersionID, report.Issues[1].VersionID}
	assert.ElementsMatch(t, []string{old, hidden}, versions, "each issue names its version")
}

// A scan stops with the server: the daily scan of millions of versions held
// a stop until it ended.
func TestAScanStopsWhenItIsCancelled(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "stopped")
	defer cleanup()
	put(t, om, "stopped", "doc", "data")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report, err := om.VerifyBucketIntegrity(ctx, "stopped", "", "", 100)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, report)
}

// A scan page by page reads each version once, keys with slashes in them too.
func TestAScanPagesThroughEveryVersion(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "paged")
	defer cleanup()
	ctx := context.Background()
	for i := 0; i < 7; i++ {
		key := fmt.Sprintf("blocks/%d/data.blk", i)
		put(t, om, "paged", key, "one")
		put(t, om, "paged", key, "two")
	}

	checked, marker := 0, ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 20, "the pages never end")
		report, err := om.VerifyBucketIntegrity(ctx, "paged", "", marker, 3)
		require.NoError(t, err)
		checked += report.OK
		assert.Zero(t, report.Corrupted)
		if report.NextMarker == "" {
			break
		}
		marker = report.NextMarker
	}
	assert.Equal(t, 14, checked)
}
