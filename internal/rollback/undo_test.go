package rollback_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

const (
	undoTestKey = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	oldBody     = "the object as it stood before the interrupted write"
	newBody     = "the bytes the interrupted write published"
)

// droppingStore accepts object writes without recording them: the shape of a
// commit that reached the write-ahead log and was lost with the process.
type droppingStore struct {
	metadata.Store
	dropObjects bool
	dropParts   bool
}

func (s *droppingStore) PutObject(ctx context.Context, obj *metadata.ObjectMetadata) error {
	if s.dropObjects {
		return nil
	}
	return s.Store.PutObject(ctx, obj)
}

func (s *droppingStore) PutPart(ctx context.Context, part *metadata.PartMetadata) error {
	if s.dropParts {
		return nil
	}
	return s.Store.PutPart(ctx, part)
}

type undoFixture struct {
	root    string
	backend storage.Backend
	store   metadata.Store
	manager object.Manager
	// crashed writes through a store that never records what it is told,
	// leaving the bytes published and the index behind.
	crashed object.Manager
}

func setupUndo(t *testing.T) *undoFixture {
	t.Helper()
	root := t.TempDir()
	backend, err := storage.NewFilesystemBackend(storage.Config{Root: root})
	require.NoError(t, err)
	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: filepath.Join(root, "metadata"),
		Logger:  logrus.StandardLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	cfg := config.StorageConfig{Backend: "filesystem", Root: root, EncryptionKey: undoTestKey}
	fixture := &undoFixture{
		root:    root,
		backend: backend,
		store:   store,
		manager: object.NewManager(backend, store, cfg),
		crashed: object.NewManager(backend, &droppingStore{Store: store, dropObjects: true, dropParts: true}, cfg),
	}
	require.NoError(t, store.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "undo", OwnerID: "owner"}))
	return fixture
}

// retainObjectCopy reproduces what an overwrite keeps on disk before it
// publishes: a copy of the stored bytes plus a manifest naming the index entry
// the copy was taken from.
func (f *undoFixture) retainObjectCopy(t *testing.T, key string) (dataPath, manifestPath string) {
	t.Helper()
	ref := storage.ObjectRef{Bucket: "undo", Key: key}
	reader, meta, err := f.backend.Get(t.Context(), ref)
	require.NoError(t, err)
	defer reader.Close()

	entry, err := f.store.GetObject(t.Context(), "undo", key)
	require.NoError(t, err)

	copyFile, err := os.CreateTemp(f.root, rollback.ObjectPrefix+"*")
	require.NoError(t, err)
	_, err = io.Copy(copyFile, reader)
	require.NoError(t, err)
	require.NoError(t, copyFile.Close())

	manifestPath = copyFile.Name() + rollback.ManifestSuffix
	require.NoError(t, rollback.WriteObjectManifest(manifestPath, ref, meta,
		&rollback.Committed{Size: entry.Size, ETag: entry.ETag}))
	return copyFile.Name(), manifestPath
}

func (f *undoFixture) retainPartCopy(t *testing.T, uploadID string, partNumber int) (dataPath, manifestPath string) {
	t.Helper()
	reader, meta, err := f.backend.GetPart(t.Context(), uploadID, partNumber)
	require.NoError(t, err)
	defer reader.Close()

	row, err := f.store.GetPart(t.Context(), uploadID, partNumber)
	require.NoError(t, err)

	copyFile, err := os.CreateTemp(f.root, rollback.PartPrefix+"*")
	require.NoError(t, err)
	_, err = io.Copy(copyFile, reader)
	require.NoError(t, err)
	require.NoError(t, copyFile.Close())

	manifestPath = copyFile.Name() + rollback.ManifestSuffix
	require.NoError(t, rollback.WritePartManifest(manifestPath, uploadID, partNumber, meta,
		&rollback.Committed{Size: row.Size, ETag: row.ETag}))
	return copyFile.Name(), manifestPath
}

func (f *undoFixture) undo(t *testing.T) *rollback.Report {
	t.Helper()
	report, err := rollback.Undo(t.Context(), f.root, f.backend, f.store, logrus.StandardLogger())
	require.NoError(t, err)
	require.Empty(t, report.Failures)
	return report
}

func (f *undoFixture) readObject(t *testing.T, key string) (*object.Object, string) {
	t.Helper()
	obj, reader, err := f.manager.GetObject(t.Context(), "undo", key)
	require.NoError(t, err)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return obj, string(data)
}

func (f *undoFixture) requireNoRetainedFiles(t *testing.T) {
	t.Helper()
	for _, prefix := range []string{rollback.ObjectPrefix, rollback.PartPrefix} {
		files, err := filepath.Glob(filepath.Join(f.root, prefix+"*"))
		require.NoError(t, err)
		require.Empty(t, files, "resolved copies must not stay on disk")
	}
}

// The interrupted write published its bytes and never reached the index: the
// object has to come back, and the entry has to describe what a GET serves.
func TestUndoRestoresOverwriteThatNeverCommitted(t *testing.T) {
	f := setupUndo(t)
	old, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(oldBody), http.Header{})
	require.NoError(t, err)
	f.retainObjectCopy(t, "key")
	_, err = f.crashed.PutObject(t.Context(), "undo", "key", strings.NewReader(newBody), http.Header{})
	require.NoError(t, err)

	report := f.undo(t)
	require.Equal(t, 1, report.ObjectsRestored)

	obj, data := f.readObject(t, "key")
	require.Equal(t, oldBody, data)
	require.Equal(t, old.ETag, obj.ETag)
	require.Equal(t, int64(len(data)), obj.Size)
	f.requireNoRetainedFiles(t)
}

// The same retained copy, but this time the write did commit: rolling back
// would throw away an acknowledged object.
func TestUndoKeepsOverwriteThatCommitted(t *testing.T) {
	f := setupUndo(t)
	_, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(oldBody), http.Header{})
	require.NoError(t, err)
	f.retainObjectCopy(t, "key")
	replacement, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(newBody), http.Header{})
	require.NoError(t, err)

	report := f.undo(t)
	require.Equal(t, 0, report.ObjectsRestored)
	require.Equal(t, 1, report.Committed)

	obj, data := f.readObject(t, "key")
	require.Equal(t, newBody, data)
	require.Equal(t, replacement.ETag, obj.ETag)
	f.requireNoRetainedFiles(t)
}

// A committed overwrite whose replacement happens to be the same length: only
// the identity recorded in the manifest tells this apart from an interrupted
// write, which is why the manifest records it.
func TestUndoKeepsCommittedOverwriteOfEqualSize(t *testing.T) {
	f := setupUndo(t)
	const before = "identical length content AAA"
	const after = "identical length content BBB"
	require.Equal(t, len(before), len(after))

	_, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(before), http.Header{})
	require.NoError(t, err)
	f.retainObjectCopy(t, "key")
	replacement, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(after), http.Header{})
	require.NoError(t, err)

	report := f.undo(t)
	require.Equal(t, 0, report.ObjectsRestored, "an acknowledged same-size overwrite must survive")
	require.Equal(t, 1, report.Committed)

	obj, data := f.readObject(t, "key")
	require.Equal(t, after, data)
	require.Equal(t, replacement.ETag, obj.ETag)
}

// A multipart overwrite that committed: its ETag has a shape of its own, which
// must not read as a disagreement.
func TestUndoKeepsCommittedMultipartOverwrite(t *testing.T) {
	f := setupUndo(t)
	_, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(oldBody), http.Header{})
	require.NoError(t, err)
	f.retainObjectCopy(t, "key")

	upload, err := f.manager.CreateMultipartUpload(t.Context(), "undo", "key", http.Header{})
	require.NoError(t, err)
	part, err := f.manager.UploadPart(t.Context(), upload.UploadID, 1, strings.NewReader(newBody))
	require.NoError(t, err)
	completed, err := f.manager.CompleteMultipartUpload(t.Context(), upload.UploadID, []object.Part{*part})
	require.NoError(t, err)
	require.Contains(t, completed.ETag, "-1")

	report := f.undo(t)
	require.Equal(t, 0, report.ObjectsRestored)

	obj, data := f.readObject(t, "key")
	require.Equal(t, newBody, data)
	require.Equal(t, completed.ETag, obj.ETag)
}

func TestUndoRetainsCopyWhenIndexEntryIsAbsent(t *testing.T) {
	f := setupUndo(t)
	_, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(oldBody), http.Header{})
	require.NoError(t, err)
	dataPath, manifestPath := f.retainObjectCopy(t, "key")
	_, err = f.crashed.PutObject(t.Context(), "undo", "key", strings.NewReader(newBody), http.Header{})
	require.NoError(t, err)
	require.NoError(t, f.store.DeleteObject(t.Context(), "undo", "key"))

	report := f.undo(t)
	require.Equal(t, 0, report.ObjectsRestored)
	require.Equal(t, 1, report.Retained)
	require.FileExists(t, dataPath)
	require.FileExists(t, manifestPath)
	_, data := f.readObject(t, "key")
	require.Equal(t, newBody, data)
}

// Nothing identifies this copy: it must be kept, not guessed at.
func TestUndoLeavesUnreadableManifestInPlace(t *testing.T) {
	f := setupUndo(t)
	_, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(oldBody), http.Header{})
	require.NoError(t, err)
	dataPath, manifestPath := f.retainObjectCopy(t, "key")
	require.NoError(t, os.WriteFile(manifestPath, []byte("{not json"), 0600))
	_, err = f.crashed.PutObject(t.Context(), "undo", "key", strings.NewReader(newBody), http.Header{})
	require.NoError(t, err)

	report, err := rollback.Undo(t.Context(), f.root, f.backend, f.store, logrus.StandardLogger())
	require.NoError(t, err)
	require.Len(t, report.Failures, 1)
	require.Equal(t, 0, report.ObjectsRestored)
	require.FileExists(t, dataPath, "an unidentifiable copy must not be deleted")
	require.FileExists(t, manifestPath)
}

// A copy whose manifest never landed belongs to a write that had not touched
// the object yet: it restores nothing and must not accumulate.
func TestUndoRemovesCopyWithoutManifest(t *testing.T) {
	f := setupUndo(t)
	_, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(oldBody), http.Header{})
	require.NoError(t, err)
	dataPath, manifestPath := f.retainObjectCopy(t, "key")
	require.NoError(t, os.Remove(manifestPath))

	report := f.undo(t)
	require.Equal(t, 1, report.Discarded)
	require.NoFileExists(t, dataPath)
	_, data := f.readObject(t, "key")
	require.Equal(t, oldBody, data)
}

// A part replacement caught before its row was committed: ListParts still
// promises the previous ETag, so the previous bytes must come back.
func TestUndoRestoresPartReplacementThatNeverCommitted(t *testing.T) {
	f := setupUndo(t)
	upload, err := f.manager.CreateMultipartUpload(t.Context(), "undo", "key", http.Header{})
	require.NoError(t, err)
	first, err := f.manager.UploadPart(t.Context(), upload.UploadID, 1, strings.NewReader(oldBody))
	require.NoError(t, err)
	f.retainPartCopy(t, upload.UploadID, 1)
	_, err = f.crashed.UploadPart(t.Context(), upload.UploadID, 1, strings.NewReader(newBody))
	require.NoError(t, err)

	report := f.undo(t)
	require.Equal(t, 1, report.PartsRestored)

	_, err = f.manager.CompleteMultipartUpload(t.Context(), upload.UploadID, []object.Part{*first})
	require.NoError(t, err)
	_, data := f.readObject(t, "key")
	require.Equal(t, oldBody, data)
	f.requireNoRetainedFiles(t)
}

// The upload is over: a restored part would be a file nothing points at.
func TestUndoDiscardsPartCopyWhenUploadIsGone(t *testing.T) {
	f := setupUndo(t)
	upload, err := f.manager.CreateMultipartUpload(t.Context(), "undo", "key", http.Header{})
	require.NoError(t, err)
	part, err := f.manager.UploadPart(t.Context(), upload.UploadID, 1, strings.NewReader(oldBody))
	require.NoError(t, err)
	dataPath, _ := f.retainPartCopy(t, upload.UploadID, 1)
	_, err = f.manager.CompleteMultipartUpload(t.Context(), upload.UploadID, []object.Part{*part})
	require.NoError(t, err)

	report := f.undo(t)
	require.Equal(t, 0, report.PartsRestored)
	require.Equal(t, 1, report.Committed)
	require.NoFileExists(t, dataPath)
	exists, err := f.backend.PartExists(t.Context(), upload.UploadID, 1)
	require.NoError(t, err)
	require.False(t, exists, "a finished upload must not have parts restored under it")
}

// Running the pass twice must not undo anything a second time.
func TestUndoIsIdempotent(t *testing.T) {
	f := setupUndo(t)
	_, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(oldBody), http.Header{})
	require.NoError(t, err)
	f.retainObjectCopy(t, "key")
	_, err = f.crashed.PutObject(t.Context(), "undo", "key", strings.NewReader(newBody), http.Header{})
	require.NoError(t, err)

	require.Equal(t, 1, f.undo(t).ObjectsRestored)

	second := f.undo(t)
	require.Equal(t, 0, second.ObjectsRestored)
	require.Equal(t, 0, second.Discarded)
	require.Equal(t, 0, second.Committed)
	_, data := f.readObject(t, "key")
	require.Equal(t, oldBody, data)
}

// Two interruptions on the same object: the oldest copy is the one that was
// consistent with the index, and it is the one that must win.
func TestUndoAppliesOldestCopyFirst(t *testing.T) {
	f := setupUndo(t)
	original, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(oldBody), http.Header{})
	require.NoError(t, err)
	f.retainObjectCopy(t, "key")
	_, err = f.crashed.PutObject(t.Context(), "undo", "key", strings.NewReader(newBody), http.Header{})
	require.NoError(t, err)
	// A second interrupted attempt keeps a copy of the bytes the first one left.
	f.retainObjectCopy(t, "key")
	_, err = f.crashed.PutObject(t.Context(), "undo", "key", strings.NewReader("a third set of bytes"), http.Header{})
	require.NoError(t, err)

	report := f.undo(t)
	require.Equal(t, 1, report.ObjectsRestored)

	obj, data := f.readObject(t, "key")
	require.Equal(t, oldBody, data, "the object the index still describes is the one to come back")
	require.Equal(t, original.ETag, obj.ETag)
	f.requireNoRetainedFiles(t)
}
