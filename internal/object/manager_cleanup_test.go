package object

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nonFSBackend implements storage.Backend but is NOT a *FilesystemBackend,
// so cleanupEmptyDirectories should return immediately without touching the FS.
type nonFSBackend struct{}

func (n *nonFSBackend) Put(_ context.Context, _ string, _ io.Reader, _ map[string]string) error {
	return nil
}
func (n *nonFSBackend) Get(_ context.Context, _ string) (io.ReadCloser, map[string]string, error) {
	return nil, nil, nil
}
func (n *nonFSBackend) Delete(_ context.Context, _ string) error        { return nil }
func (n *nonFSBackend) Exists(_ context.Context, _ string) (bool, error) { return false, nil }
func (n *nonFSBackend) List(_ context.Context, _ string, _ bool) ([]storage.ObjectInfo, error) {
	return nil, nil
}
func (n *nonFSBackend) GetMetadata(_ context.Context, _ string) (map[string]string, error) {
	return nil, nil
}
func (n *nonFSBackend) SetMetadata(_ context.Context, _ string, _ map[string]string) error {
	return nil
}
func (n *nonFSBackend) Close() error { return nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeDir creates a directory (and all parents) inside the given root.
func makeDir(t *testing.T, root string, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{root}, parts...)...)
	require.NoError(t, os.MkdirAll(path, 0755))
	return path
}

// makeFile creates a file with minimal content at root/parts...
func makeFile(t *testing.T, root string, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{root}, parts...)...)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte("data"), 0644))
	return path
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ---------------------------------------------------------------------------
// cleanupEmptyDirectories — non-filesystem backend returns immediately
// ---------------------------------------------------------------------------

func TestCleanupEmptyDirectories_NonFilesystemBackend(t *testing.T) {
	// Manager with a mock (non-filesystem) backend should be a no-op
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	// Replace the real FilesystemBackend with a non-filesystem backend
	om.storage = &nonFSBackend{}

	// Should not panic or fail
	om.cleanupEmptyDirectories("bucket", "subdir/file.txt")
}

// ---------------------------------------------------------------------------
// cleanupEmptyDirectories — single-level empty directory is removed
// ---------------------------------------------------------------------------

func TestCleanupEmptyDirectories_RemovesSingleEmptyDir(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	root := om.storage.(interface{ GetRootPath() string }).GetRootPath()

	// Create bucket/subdir/ (empty)
	subdir := makeDir(t, root, "bucket", "subdir")
	assert.True(t, dirExists(subdir))

	om.cleanupEmptyDirectories("bucket", "subdir/file.txt")

	assert.False(t, dirExists(subdir), "empty subdir should be removed")
}

// ---------------------------------------------------------------------------
// cleanupEmptyDirectories — nested empty directories all cleaned up
// ---------------------------------------------------------------------------

func TestCleanupEmptyDirectories_RemovesNestedEmptyDirs(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	root := om.storage.(interface{ GetRootPath() string }).GetRootPath()

	// Create bucket/a/b/c/ (all empty)
	deepDir := makeDir(t, root, "bucket", "a", "b", "c")
	assert.True(t, dirExists(deepDir))

	om.cleanupEmptyDirectories("bucket", "a/b/c/file.txt")

	assert.False(t, dirExists(filepath.Join(root, "bucket", "a", "b", "c")))
	assert.False(t, dirExists(filepath.Join(root, "bucket", "a", "b")))
	assert.False(t, dirExists(filepath.Join(root, "bucket", "a")))
}

// ---------------------------------------------------------------------------
// cleanupEmptyDirectories — stops when a sibling file exists
// ---------------------------------------------------------------------------

func TestCleanupEmptyDirectories_StopsAtNonEmptyParent(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	root := om.storage.(interface{ GetRootPath() string }).GetRootPath()

	// bucket/parent/
	//   sibling.txt  ← keeps parent alive
	//   subdir/      ← should be removed (empty)
	makeFile(t, root, "bucket", "parent", "sibling.txt")
	subdir := makeDir(t, root, "bucket", "parent", "subdir")
	parentDir := filepath.Join(root, "bucket", "parent")

	om.cleanupEmptyDirectories("bucket", "parent/subdir/file.txt")

	assert.False(t, dirExists(subdir), "empty subdir should be removed")
	assert.True(t, dirExists(parentDir), "parent with sibling should be kept")
}

// ---------------------------------------------------------------------------
// cleanupEmptyDirectories — .maxiofs-folder marker is treated as system file
// ---------------------------------------------------------------------------

func TestCleanupEmptyDirectories_FolderMarkerIsSystemFile(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	root := om.storage.(interface{ GetRootPath() string }).GetRootPath()

	// Create subdir with only the folder marker inside
	subdir := makeDir(t, root, "bucket", "subdir")
	makeFile(t, root, "bucket", "subdir", ".maxiofs-folder")

	om.cleanupEmptyDirectories("bucket", "subdir/file.txt")

	assert.False(t, dirExists(subdir), "dir with only .maxiofs-folder should be removed")
}

// ---------------------------------------------------------------------------
// cleanupEmptyDirectories — .metadata files are treated as system files
// ---------------------------------------------------------------------------

func TestCleanupEmptyDirectories_MetadataFilesAreSystemFiles(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	root := om.storage.(interface{ GetRootPath() string }).GetRootPath()

	subdir := makeDir(t, root, "bucket", "subdir")
	makeFile(t, root, "bucket", "subdir", "object.metadata")

	om.cleanupEmptyDirectories("bucket", "subdir/file.txt")

	assert.False(t, dirExists(subdir), "dir with only .metadata files should be removed")
}

// ---------------------------------------------------------------------------
// cleanupEmptyDirectories — does not remove root path itself
// ---------------------------------------------------------------------------

func TestCleanupEmptyDirectories_DoesNotRemoveRoot(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	root := om.storage.(interface{ GetRootPath() string }).GetRootPath()

	// Call with a top-level key (no subdirectory) — dirPath == root/bucket,
	// not root itself, so root must survive.
	makeDir(t, root, "bucket")

	om.cleanupEmptyDirectories("bucket", "file.txt")

	assert.True(t, dirExists(root), "root path must never be removed")
}
