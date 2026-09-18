package rollback_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

func TestReviewUndoDoesNotResurrectDeletedObject(t *testing.T) {
	f := setupUndo(t)
	_, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(oldBody), http.Header{})
	require.NoError(t, err)
	dataPath, manifestPath := f.retainObjectCopy(t, "key")
	_, err = f.manager.DeleteObject(t.Context(), "undo", "key", false)
	require.NoError(t, err)
	exists, err := f.backend.Exists(t.Context(), storage.ObjectRef{Bucket: "undo", Key: "key"})
	require.NoError(t, err)
	require.False(t, exists)
	f.undo(t)
	exists, err = f.backend.Exists(t.Context(), storage.ObjectRef{Bucket: "undo", Key: "key"})
	require.NoError(t, err)
	require.False(t, exists, "an acknowledged DELETE must not be undone by a retained overwrite copy")
	require.FileExists(t, dataPath)
	require.FileExists(t, manifestPath)
}

type failingUndoBackend struct {
	storage.Backend
	calls int
}

func (b *failingUndoBackend) Put(context.Context, storage.ObjectRef, io.Reader, map[string]string) error {
	b.calls++
	return errors.New("restore unavailable")
}

func TestUndoKeepsLaterCopiesWhenOldestRestoreFails(t *testing.T) {
	f := setupUndo(t)
	_, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(oldBody), http.Header{})
	require.NoError(t, err)
	first, firstManifest := f.retainObjectCopy(t, "key")
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(firstManifest, past, past))
	_, err = f.crashed.PutObject(t.Context(), "undo", "key", strings.NewReader(newBody), http.Header{})
	require.NoError(t, err)
	second, secondManifest := f.retainObjectCopy(t, "key")
	b := &failingUndoBackend{Backend: f.backend}
	report, err := rollback.Undo(t.Context(), f.root, b, f.store, nil)
	require.NoError(t, err)
	require.Len(t, report.Failures, 1)
	require.Equal(t, 1, b.calls)
	for _, path := range []string{first, firstManifest, second, secondManifest} {
		require.FileExists(t, path)
	}
	f.undo(t)
	_, data := f.readObject(t, "key")
	require.Equal(t, oldBody, data)
}

func TestUndoChoosesPartCopyForCurrentCommit(t *testing.T) {
	f := setupUndo(t)
	u, err := f.manager.CreateMultipartUpload(t.Context(), "undo", "key", http.Header{})
	require.NoError(t, err)
	_, err = f.manager.UploadPart(t.Context(), u.UploadID, 1, strings.NewReader(oldBody))
	require.NoError(t, err)
	_, stale := f.retainPartCopy(t, u.UploadID, 1)
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(stale, past, past))
	const committed = "the last acknowledged part"
	_, err = f.manager.UploadPart(t.Context(), u.UploadID, 1, strings.NewReader(committed))
	require.NoError(t, err)
	f.retainPartCopy(t, u.UploadID, 1)
	_, err = f.crashed.UploadPart(t.Context(), u.UploadID, 1, strings.NewReader(newBody))
	require.NoError(t, err)
	report := f.undo(t)
	require.Equal(t, 1, report.PartsRestored)
	r, _, err := f.backend.GetPart(t.Context(), u.UploadID, 1)
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, committed, string(data))
}

func TestReviewUndoChoosesBackupForCurrentCommit(t *testing.T) {
	f := setupUndo(t)
	_, err := f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(oldBody), http.Header{})
	require.NoError(t, err)
	_, stale := f.retainObjectCopy(t, "key")
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(stale, past, past))
	const acknowledged = "a subsequent acknowledged value"
	_, err = f.manager.PutObject(t.Context(), "undo", "key", strings.NewReader(acknowledged), http.Header{})
	require.NoError(t, err)
	f.retainObjectCopy(t, "key")
	_, err = f.crashed.PutObject(t.Context(), "undo", "key", strings.NewReader(newBody), http.Header{})
	require.NoError(t, err)
	report := f.undo(t)
	t.Logf("rollback: %+v", report)
	_, data := f.readObject(t, "key")
	require.Equal(t, acknowledged, data)
}

func TestReviewDurablePartAfterBackupCleanup(t *testing.T) {
	f := setupUndo(t)
	u, err := f.manager.CreateMultipartUpload(t.Context(), "undo", "key", http.Header{})
	require.NoError(t, err)
	_, err = f.manager.UploadPart(t.Context(), u.UploadID, 1, strings.NewReader(oldBody))
	require.NoError(t, err)
	_, err = f.manager.UploadPart(t.Context(), u.UploadID, 1, strings.NewReader(newBody))
	require.NoError(t, err)
	f.undo(t)
	row, err := f.store.GetPart(t.Context(), u.UploadID, 1)
	require.NoError(t, err)
	r, meta, err := f.backend.GetPart(t.Context(), u.UploadID, 1)
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, int64(len(data)), row.Size)
	require.Equal(t, meta["etag"], row.ETag)
}
