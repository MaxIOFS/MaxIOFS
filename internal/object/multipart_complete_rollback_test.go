package object

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

// publishThenFailBackend completes the underlying write and only then reports
// the error: the shape of a storage failure that leaves the new bytes already
// in place (ENOSPC on sync, a device error after the rename).
type publishThenFailBackend struct {
	storage.Backend
	failOn int // 1-based Put to fail after publishing; <= 0 fails every Put
	calls  int
	err    error
}

func (b *publishThenFailBackend) Put(ctx context.Context, ref storage.ObjectRef, data io.Reader, meta map[string]string) error {
	b.calls++
	if err := b.Backend.Put(ctx, ref, data, meta); err != nil {
		return err
	}
	if b.failOn <= 0 || b.calls == b.failOn {
		return b.err
	}
	return nil
}

func stageOnePartUpload(t *testing.T, om *objectManager, bucket, key, body string) (string, []Part) {
	t.Helper()
	upload, err := om.CreateMultipartUpload(t.Context(), bucket, key, http.Header{})
	require.NoError(t, err)
	part, err := om.UploadPart(t.Context(), upload.UploadID, 1, strings.NewReader(body))
	require.NoError(t, err)
	return upload.UploadID, []Part{*part}
}

func readWholeObject(t *testing.T, om *objectManager, bucket, key string) (*Object, string) {
	t.Helper()
	obj, reader, err := om.GetObject(t.Context(), bucket, key)
	require.NoError(t, err)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return obj, string(data)
}

// bucketFiles lists every file under one bucket directory. Parts and backups
// live outside it, so the set changes only when an object file appears or goes.
func bucketFiles(t *testing.T, om *objectManager, bucket string) []string {
	t.Helper()
	root := filepath.Join(om.config.Root, storage.BucketDirName(bucket))
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(found)
	return found
}

func requireNoRetainedBackup(t *testing.T, om *objectManager) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(om.config.Root, "maxiofs-mpu-backup-*"))
	require.NoError(t, err)
	require.Empty(t, files, "a completed rollback must not leave backup files behind")
}

// A storage error raised after the combined bytes are already published must
// still bring the previous object back: its bytes and its index entry.
func TestCompleteMultipartRestoresPreviousObjectAfterPublishFailure(t *testing.T) {
	const previous = "previous object bytes"
	const replacement = "replacement object bytes, a different length"

	for name, failOn := range map[string]int{"combine": 1, "encrypt": 2} {
		t.Run(name, func(t *testing.T) {
			om, backend, meta := setupManagerWithConfigKey(t)
			ctx := t.Context()
			require.NoError(t, meta.CreateBucket(ctx, &metadata.BucketMetadata{Name: "rollback", OwnerID: "owner"}))
			old, err := om.PutObject(ctx, "rollback", "key", strings.NewReader(previous), http.Header{})
			require.NoError(t, err)
			uploadID, parts := stageOnePartUpload(t, om, "rollback", "key", replacement)

			publishErr := errors.New("device full after publish")
			om.storage = &publishThenFailBackend{Backend: backend, failOn: failOn, err: publishErr}
			_, err = om.CompleteMultipartUpload(ctx, uploadID, parts)
			require.ErrorIs(t, err, publishErr)
			om.storage = backend

			obj, data := readWholeObject(t, om, "rollback", "key")
			require.Equal(t, previous, data)
			require.Equal(t, old.ETag, obj.ETag)
			require.Equal(t, int64(len(previous)), obj.Size)

			entry, err := meta.GetObject(ctx, "rollback", "key")
			require.NoError(t, err)
			require.Equal(t, int64(len(data)), entry.Size, "index entry and stored bytes must agree")
			require.Equal(t, old.ETag, entry.ETag)
			requireNoRetainedBackup(t, om)
		})
	}
}

// Nothing to roll back to: the combined file must not survive as an orphan,
// and its absence must not be reported as a second failure.
func TestCompleteMultipartOnNewKeyRemovesCombinedFileAfterPublishFailure(t *testing.T) {
	om, backend, meta := setupManagerWithConfigKey(t)
	ctx := t.Context()
	require.NoError(t, meta.CreateBucket(ctx, &metadata.BucketMetadata{Name: "rollback", OwnerID: "owner"}))
	uploadID, parts := stageOnePartUpload(t, om, "rollback", "fresh", "bytes for a key that never existed")

	publishErr := errors.New("device full after publish")
	om.storage = &publishThenFailBackend{Backend: backend, failOn: 1, err: publishErr}
	_, err := om.CompleteMultipartUpload(ctx, uploadID, parts)
	require.ErrorIs(t, err, publishErr)
	om.storage = backend

	_, _, err = om.GetObject(ctx, "rollback", "fresh")
	require.Error(t, err)
	exists, err := backend.Exists(ctx, om.objectRef("rollback", "fresh"))
	require.NoError(t, err)
	require.False(t, exists, "the combined file must be removed when there is no previous object")
	requireNoRetainedBackup(t, om)
}

// With versioning the combine writes to a fresh version path: the failure must
// drop that path and leave the live object and its version list untouched.
func TestCompleteMultipartVersionedPublishFailureKeepsLiveObject(t *testing.T) {
	om, backend, meta := setupManagerWithConfigKey(t)
	ctx := t.Context()
	require.NoError(t, meta.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:       "versioned",
		OwnerID:    "owner",
		Versioning: &metadata.VersioningMetadata{Enabled: true, Status: "Enabled"},
	}))
	const previous = "first version bytes"
	old, err := om.PutObject(ctx, "versioned", "key", strings.NewReader(previous), http.Header{})
	require.NoError(t, err)
	uploadID, parts := stageOnePartUpload(t, om, "versioned", "key", "second version bytes, longer")

	before := bucketFiles(t, om, "versioned")
	versionsBefore, err := meta.GetObjectVersions(ctx, "versioned", "key")
	require.NoError(t, err)

	publishErr := errors.New("device full after publish")
	om.storage = &publishThenFailBackend{Backend: backend, failOn: 2, err: publishErr}
	_, err = om.CompleteMultipartUpload(ctx, uploadID, parts)
	require.ErrorIs(t, err, publishErr)
	om.storage = backend

	obj, data := readWholeObject(t, om, "versioned", "key")
	require.Equal(t, previous, data)
	require.Equal(t, old.ETag, obj.ETag)
	versionsAfter, err := meta.GetObjectVersions(ctx, "versioned", "key")
	require.NoError(t, err)
	require.Len(t, versionsAfter, len(versionsBefore))
	require.Equal(t, before, bucketFiles(t, om, "versioned"), "the failed version must leave no file behind")
}

// The staging copy the encryption step writes must not survive a failure either.
func TestCompleteMultipartPublishFailureLeavesNoStagingFile(t *testing.T) {
	om, backend, meta := setupManagerWithConfigKey(t)
	ctx := t.Context()
	require.NoError(t, meta.CreateBucket(ctx, &metadata.BucketMetadata{Name: "rollback", OwnerID: "owner"}))
	_, err := om.PutObject(ctx, "rollback", "key", strings.NewReader("previous"), http.Header{})
	require.NoError(t, err)
	uploadID, parts := stageOnePartUpload(t, om, "rollback", "key", "replacement")

	om.storage = &publishThenFailBackend{Backend: backend, failOn: 2, err: errors.New("device full after publish")}
	_, err = om.CompleteMultipartUpload(ctx, uploadID, parts)
	require.Error(t, err)
	om.storage = backend

	staging, err := filepath.Glob(filepath.Join(om.config.Root, "maxiofs-multipart-*"))
	require.NoError(t, err)
	for _, path := range staging {
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		t.Fatalf("staging file left behind: %s (%d bytes)", path, info.Size())
	}
}
