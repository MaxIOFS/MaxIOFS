package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStagedTestBackend(t *testing.T) (*FilesystemBackend, string) {
	t.Helper()
	tmpDir := t.TempDir()
	backend, err := NewFilesystemBackend(Config{Root: tmpDir})
	require.NoError(t, err)
	return backend, tmpDir
}

func writeStagedSidecar(t *testing.T, backend *FilesystemBackend, path string, meta map[string]string) {
	t.Helper()
	data, err := json.Marshal(meta)
	require.NoError(t, err)
	stagingPath := backend.getStagingMetadataPath(path)
	require.NoError(t, os.MkdirAll(filepath.Dir(stagingPath), 0o750))
	require.NoError(t, os.WriteFile(stagingPath, data, 0o640))
}

// A normal Put must leave no staging file behind and read back consistently.
func TestStagedCommit_NormalPutAndOverwrite(t *testing.T) {
	backend, _ := newStagedTestBackend(t)
	ctx := context.Background()
	path := "bucket/obj.txt"

	require.NoError(t, backend.Put(ctx, path, bytes.NewReader([]byte("v1")), map[string]string{"content-type": "text/plain"}))
	_, err := os.Stat(backend.getStagingMetadataPath(path))
	assert.True(t, os.IsNotExist(err), "staging file must be consumed by the commit")

	// Overwrite.
	require.NoError(t, backend.Put(ctx, path, bytes.NewReader([]byte("version-two")), map[string]string{"content-type": "text/plain"}))
	reader, meta, err := backend.Get(ctx, path)
	require.NoError(t, err)
	defer reader.Close()
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, "version-two", string(got))
	assert.Equal(t, fmt.Sprintf("%d", len("version-two")), meta["size"])
	_, err = os.Stat(backend.getStagingMetadataPath(path))
	assert.True(t, os.IsNotExist(err))
}

// Crash BETWEEN the data commit and the metadata commit: the staged sidecar
// matches the stored bytes, so the next access must roll FORWARD (finish the
// metadata commit).
func TestStagedCommit_RollForwardAfterDataCommit(t *testing.T) {
	backend, _ := newStagedTestBackend(t)
	ctx := context.Background()
	path := "bucket/obj.txt"

	// Old pair on disk.
	require.NoError(t, backend.Put(ctx, path, bytes.NewReader([]byte("old data")), map[string]string{"x-old": "yes"}))

	// Simulate the crashed Put: NEW data already renamed into place, staged
	// sidecar present, final sidecar still the old one.
	newData := []byte("new data after crash")
	sum := md5.Sum(newData)
	require.NoError(t, os.WriteFile(backend.getFullPath(path), newData, 0o640))
	writeStagedSidecar(t, backend, path, map[string]string{
		"etag": hex.EncodeToString(sum[:]),
		"size": fmt.Sprintf("%d", len(newData)),
		"x-new": "yes",
	})

	// Any read triggers the repair.
	meta, err := backend.GetMetadata(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, "yes", meta["x-new"], "repair must roll the staged sidecar forward")
	assert.Empty(t, meta["x-old"])

	_, err = os.Stat(backend.getStagingMetadataPath(path))
	assert.True(t, os.IsNotExist(err), "stage must be consumed by the roll-forward")

	reader, meta2, err := backend.Get(ctx, path)
	require.NoError(t, err)
	defer reader.Close()
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, newData, got)
	assert.Equal(t, "yes", meta2["x-new"])
}

// Crash BEFORE the data commit: the stored bytes do not match the staged
// sidecar, so the next access must roll BACK (discard the stage) and keep the
// old pair fully intact.
func TestStagedCommit_RollBackWhenDataCommitNeverHappened(t *testing.T) {
	backend, _ := newStagedTestBackend(t)
	ctx := context.Background()
	path := "bucket/obj.txt"

	oldData := []byte("old data")
	require.NoError(t, backend.Put(ctx, path, bytes.NewReader(oldData), map[string]string{"x-old": "yes"}))

	// Simulate the crashed Put: staged sidecar written, data commit never ran.
	writeStagedSidecar(t, backend, path, map[string]string{
		"etag": "00000000000000000000000000000000", // matches nothing
		"size": "999",
		"x-new": "yes",
	})

	reader, meta, err := backend.Get(ctx, path)
	require.NoError(t, err)
	defer reader.Close()
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, oldData, got, "old data must be intact")
	assert.Equal(t, "yes", meta["x-old"], "old sidecar must be intact")
	assert.Empty(t, meta["x-new"])

	_, err = os.Stat(backend.getStagingMetadataPath(path))
	assert.True(t, os.IsNotExist(err), "stage must be discarded by the roll-back")
}

// A stage for a brand-new object (no data file at all) is dead and must be
// discarded without creating anything.
func TestStagedCommit_StageWithoutDataIsDiscarded(t *testing.T) {
	backend, _ := newStagedTestBackend(t)
	ctx := context.Background()
	path := "bucket/never-committed.txt"

	writeStagedSidecar(t, backend, path, map[string]string{"etag": "abc", "size": "3"})

	_, _, err := backend.Get(ctx, path)
	assert.ErrorIs(t, err, ErrObjectNotFound)

	_, err = os.Stat(backend.getStagingMetadataPath(path))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(backend.getMetadataPath(path))
	assert.True(t, os.IsNotExist(err), "no final sidecar may appear out of a dead stage")
}

// A corrupt (non-JSON) stage is foreign and must be discarded.
func TestStagedCommit_CorruptStageIsDiscarded(t *testing.T) {
	backend, _ := newStagedTestBackend(t)
	ctx := context.Background()
	path := "bucket/obj.txt"

	require.NoError(t, backend.Put(ctx, path, bytes.NewReader([]byte("data")), nil))
	stagingPath := backend.getStagingMetadataPath(path)
	require.NoError(t, os.WriteFile(stagingPath, []byte("not json"), 0o640))

	_, err := backend.GetMetadata(ctx, path)
	require.NoError(t, err)
	_, err = os.Stat(stagingPath)
	assert.True(t, os.IsNotExist(err))
}

// Delete must clean up a leftover stage along with the pair.
func TestStagedCommit_DeleteRemovesStage(t *testing.T) {
	backend, _ := newStagedTestBackend(t)
	ctx := context.Background()
	path := "bucket/obj.txt"

	require.NoError(t, backend.Put(ctx, path, bytes.NewReader([]byte("data")), nil))
	writeStagedSidecar(t, backend, path, map[string]string{"etag": "zz", "size": "1"})

	require.NoError(t, backend.Delete(ctx, path))
	_, err := os.Stat(backend.getStagingMetadataPath(path))
	assert.True(t, os.IsNotExist(err))
}

// List must never surface staged sidecars as objects.
func TestStagedCommit_ListSkipsStagedSidecars(t *testing.T) {
	backend, _ := newStagedTestBackend(t)
	ctx := context.Background()

	require.NoError(t, backend.Put(ctx, "bucket/a.txt", bytes.NewReader([]byte("a")), nil))
	writeStagedSidecar(t, backend, "bucket/b.txt", map[string]string{"etag": "zz", "size": "1"})

	objects, err := backend.List(ctx, "bucket/", true)
	require.NoError(t, err)
	for _, o := range objects {
		assert.NotContains(t, o.Path, ".metadata", "sidecar/staging files must not be listed: %s", o.Path)
	}
}
