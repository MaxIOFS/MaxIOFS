package object

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

const (
	firstPartBody  = "bytes of the part the client already acknowledged"
	secondPartBody = "shorter replacement"
)

// partPublishThenFailBackend writes the part and only then reports the error,
// the shape of a storage failure that leaves the replacement bytes in place.
type partPublishThenFailBackend struct {
	storage.Backend
	failOn int // 1-based PutPart to fail after writing; <= 0 fails every call
	calls  int
	err    error
}

func (b *partPublishThenFailBackend) PutPart(ctx context.Context, uploadID string, partNumber int, data io.Reader, meta map[string]string) error {
	b.calls++
	if err := b.Backend.PutPart(ctx, uploadID, partNumber, data, meta); err != nil {
		return err
	}
	if b.failOn <= 0 || b.calls == b.failOn {
		return b.err
	}
	return nil
}

// partMetadataFailBackend stores the part but cannot describe it afterwards.
type partMetadataFailBackend struct {
	storage.Backend
	err error
}

func (b *partMetadataFailBackend) PartMetadata(context.Context, string, int) (map[string]string, error) {
	return nil, b.err
}

// partPutFailStore fails every part-metadata commit, leaving the row that is
// already committed as the only description of the part.
type partPutFailStore struct {
	metadata.Store
	err error
}

func (s *partPutFailStore) PutPart(context.Context, *metadata.PartMetadata) error {
	return s.err
}

func setupPartReplacement(t *testing.T) (*objectManager, storage.Backend, metadata.Store, string, *Part) {
	t.Helper()
	om, backend, meta := setupManagerWithConfigKey(t)
	require.NoError(t, meta.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "parts", OwnerID: "owner"}))
	upload, err := om.CreateMultipartUpload(t.Context(), "parts", "key", http.Header{})
	require.NoError(t, err)
	first, err := om.UploadPart(t.Context(), upload.UploadID, 1, strings.NewReader(firstPartBody))
	require.NoError(t, err)
	return om, backend, meta, upload.UploadID, first
}

func requireStoredPart(t *testing.T, om *objectManager, backend storage.Backend, uploadID string, partNumber int, want string) {
	t.Helper()
	reader, _, err := backend.GetPart(t.Context(), uploadID, partNumber)
	require.NoError(t, err, "a part advertised by ListParts must exist on disk")
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, want, string(data))
}

func requirePartList(t *testing.T, om *objectManager, uploadID string, want *Part) {
	t.Helper()
	parts, err := om.ListParts(t.Context(), uploadID)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.Equal(t, want.ETag, parts[0].ETag)
	require.Equal(t, want.Size, parts[0].Size)
}

func requireNoRetainedPartBackup(t *testing.T, om *objectManager) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(om.config.Root, rollback.PartPrefix+"*"))
	require.NoError(t, err)
	require.Empty(t, files, "a completed part rollback must not leave backup files behind")
}

// A failed part-metadata commit must not destroy the part the client already
// holds an ETag for: the upload has to stay completable.
func TestUploadPartMetadataFailureKeepsPreviousPartCompletable(t *testing.T) {
	om, backend, meta, uploadID, first := setupPartReplacement(t)
	ctx := t.Context()

	commitErr := errors.New("part metadata commit failed")
	om.metadataStore = &partPutFailStore{Store: meta, err: commitErr}
	_, err := om.UploadPart(ctx, uploadID, 1, strings.NewReader(secondPartBody))
	require.ErrorIs(t, err, commitErr)
	om.metadataStore = meta

	requirePartList(t, om, uploadID, first)
	requireStoredPart(t, om, backend, uploadID, 1, firstPartBody)
	requireNoRetainedPartBackup(t, om)

	_, err = om.CompleteMultipartUpload(ctx, uploadID, []Part{*first})
	require.NoError(t, err)
	_, data := readWholeObject(t, om, "parts", "key")
	require.Equal(t, firstPartBody, data)
}

// Same invariant when the storage write is what fails after publishing.
func TestUploadPartStorageFailureAfterPublishKeepsPreviousPart(t *testing.T) {
	om, backend, meta, uploadID, first := setupPartReplacement(t)
	ctx := t.Context()

	publishErr := errors.New("device full after publish")
	om.storage = &partPublishThenFailBackend{Backend: backend, failOn: 1, err: publishErr}
	_, err := om.UploadPart(ctx, uploadID, 1, strings.NewReader(secondPartBody))
	require.ErrorIs(t, err, publishErr)
	om.storage = backend

	requirePartList(t, om, uploadID, first)
	requireStoredPart(t, om, backend, uploadID, 1, firstPartBody)
	requireNoRetainedPartBackup(t, om)

	_, err = om.CompleteMultipartUpload(ctx, uploadID, []Part{*first})
	require.NoError(t, err)
	_, data := readWholeObject(t, om, "parts", "key")
	require.Equal(t, firstPartBody, data)
	_ = meta
}

// Nothing was acknowledged for this part number yet, so the failed upload must
// leave neither a row nor a file behind.
func TestUploadPartFirstUploadMetadataFailureLeavesNothing(t *testing.T) {
	om, backend, meta := setupManagerWithConfigKey(t)
	ctx := t.Context()
	require.NoError(t, meta.CreateBucket(ctx, &metadata.BucketMetadata{Name: "parts", OwnerID: "owner"}))
	upload, err := om.CreateMultipartUpload(ctx, "parts", "key", http.Header{})
	require.NoError(t, err)

	om.metadataStore = &partPutFailStore{Store: meta, err: errors.New("part metadata commit failed")}
	_, err = om.UploadPart(ctx, upload.UploadID, 1, strings.NewReader(firstPartBody))
	require.Error(t, err)
	om.metadataStore = meta

	parts, err := om.ListParts(ctx, upload.UploadID)
	require.NoError(t, err)
	require.Empty(t, parts)
	exists, err := backend.PartExists(ctx, upload.UploadID, 1)
	require.NoError(t, err)
	require.False(t, exists, "an unacknowledged part must not survive as an orphan file")
	requireNoRetainedPartBackup(t, om)
}

// A row whose bytes were already lost must not block the replacement that would
// repair it.
func TestUploadPartReplacesRowWithoutStoredBytes(t *testing.T) {
	om, backend, _, uploadID, _ := setupPartReplacement(t)
	ctx := t.Context()
	require.NoError(t, backend.DeletePart(ctx, uploadID, 1))

	replacement, err := om.UploadPart(ctx, uploadID, 1, strings.NewReader(secondPartBody))
	require.NoError(t, err)
	requirePartList(t, om, uploadID, replacement)
	requireStoredPart(t, om, backend, uploadID, 1, secondPartBody)
	requireNoRetainedPartBackup(t, om)

	_, err = om.CompleteMultipartUpload(ctx, uploadID, []Part{*replacement})
	require.NoError(t, err)
	_, data := readWholeObject(t, om, "parts", "key")
	require.Equal(t, secondPartBody, data)
}

// When the rollback itself cannot complete, the retained copy must stay on disk
// instead of being cleaned up behind a failure.
func TestUploadPartRetainsBackupWhenRestoreFails(t *testing.T) {
	om, backend, meta, uploadID, _ := setupPartReplacement(t)
	ctx := t.Context()

	restoreErr := errors.New("restore write failed")
	om.storage = &partPublishThenFailBackend{Backend: backend, failOn: 0, err: restoreErr}
	om.metadataStore = &partPutFailStore{Store: meta, err: errors.New("part metadata commit failed")}
	_, err := om.UploadPart(ctx, uploadID, 1, strings.NewReader(secondPartBody))
	require.ErrorIs(t, err, restoreErr)
	om.storage = backend
	om.metadataStore = meta

	backups, err := filepath.Glob(filepath.Join(om.config.Root, rollback.PartPrefix+"*"))
	require.NoError(t, err)
	require.Len(t, backups, 2, "the retained part copy and its manifest must both survive")

	manifests, err := rollback.Manifests(om.config.Root, rollback.PartPrefix)
	require.NoError(t, err)
	require.Len(t, manifests, 1)
	manifest, err := rollback.ReadPartManifest(manifests[0])
	require.NoError(t, err)
	require.Equal(t, uploadID, manifest.UploadID)
	require.Equal(t, 1, manifest.PartNumber)
}

// The replacement is stored but cannot be described: the previous part still
// has to be the one ListParts promises.
func TestUploadPartDescribeFailureKeepsPreviousPart(t *testing.T) {
	om, backend, _, uploadID, first := setupPartReplacement(t)
	ctx := t.Context()

	describeErr := errors.New("sidecar unreadable")
	om.storage = &partMetadataFailBackend{Backend: backend, err: describeErr}
	_, err := om.UploadPart(ctx, uploadID, 1, strings.NewReader(secondPartBody))
	require.ErrorIs(t, err, describeErr)
	om.storage = backend

	requirePartList(t, om, uploadID, first)
	requireStoredPart(t, om, backend, uploadID, 1, firstPartBody)
	requireNoRetainedPartBackup(t, om)
}

// A successful replacement must leave no retained copy behind.
func TestUploadPartSuccessfulReplacementLeavesNoBackup(t *testing.T) {
	om, backend, _, uploadID, _ := setupPartReplacement(t)
	ctx := t.Context()

	replacement, err := om.UploadPart(ctx, uploadID, 1, strings.NewReader(secondPartBody))
	require.NoError(t, err)
	requirePartList(t, om, uploadID, replacement)
	requireStoredPart(t, om, backend, uploadID, 1, secondPartBody)
	requireNoRetainedPartBackup(t, om)
}
