package storage

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilesystemBackend(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "maxiofs-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create backend
	config := config.StorageConfig{
		Root: tempDir,
	}

	backend, err := storage.NewFilesystemBackend(config)
	require.NoError(t, err)
	require.NotNil(t, backend)

	// Test basic operations
	t.Run("PutAndGet", func(t *testing.T) {
		testPutAndGet(t, backend)
	})

	t.Run("Delete", func(t *testing.T) {
		testDelete(t, backend)
	})

	t.Run("Exists", func(t *testing.T) {
		testExists(t, backend)
	})

	t.Run("List", func(t *testing.T) {
		testList(t, backend)
	})

	t.Run("Metadata", func(t *testing.T) {
		testMetadata(t, backend)
	})

	t.Run("PathValidation", func(t *testing.T) {
		testPathValidation(t, backend)
	})
}

func testPutAndGet(t *testing.T, backend storage.Backend) {
	ctx := context.Background()
	testData := "Hello MaxIOFS!"
	testPath := "test/object.txt"
	metadata := map[string]string{
		"content-type": "text/plain",
		"user-meta":    "test-value",
	}

	// Put object
	err := backend.Put(ctx, testPath, strings.NewReader(testData), metadata)
	assert.NoError(t, err)

	// Get object
	reader, retrievedMetadata, err := backend.Get(ctx, testPath)
	require.NoError(t, err)
	require.NotNil(t, reader)
	defer reader.Close()

	// Read data
	buf := make([]byte, len(testData))
	n, err := reader.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, len(testData), n)
	assert.Equal(t, testData, string(buf[:n]))

	// Check metadata
	assert.Equal(t, "text/plain", retrievedMetadata["content-type"])
	assert.Equal(t, "test-value", retrievedMetadata["user-meta"])
	assert.Contains(t, retrievedMetadata, "etag")
	assert.Contains(t, retrievedMetadata, "size")
	assert.Contains(t, retrievedMetadata, "last_modified")
}

func testDelete(t *testing.T, backend storage.Backend) {
	ctx := context.Background()
	testData := "Delete me!"
	testPath := "test/delete-me.txt"

	// Put object
	err := backend.Put(ctx, testPath, strings.NewReader(testData), nil)
	require.NoError(t, err)

	// Verify exists
	exists, err := backend.Exists(ctx, testPath)
	require.NoError(t, err)
	assert.True(t, exists)

	// Delete object
	err = backend.Delete(ctx, testPath)
	assert.NoError(t, err)

	// Verify doesn't exist
	exists, err = backend.Exists(ctx, testPath)
	require.NoError(t, err)
	assert.False(t, exists)

	// Try to delete non-existent object
	err = backend.Delete(ctx, "non-existent.txt")
	assert.Equal(t, storage.ErrObjectNotFound, err)
}

func testExists(t *testing.T, backend storage.Backend) {
	ctx := context.Background()
	testPath := "test/exists-test.txt"

	// Should not exist initially
	exists, err := backend.Exists(ctx, testPath)
	require.NoError(t, err)
	assert.False(t, exists)

	// Put object
	err = backend.Put(ctx, testPath, strings.NewReader("exists"), nil)
	require.NoError(t, err)

	// Should exist now
	exists, err = backend.Exists(ctx, testPath)
	require.NoError(t, err)
	assert.True(t, exists)
}

func testList(t *testing.T, backend storage.Backend) {
	ctx := context.Background()

	// Put multiple objects
	objects := map[string]string{
		"list/file1.txt":     "content1",
		"list/file2.txt":     "content2",
		"list/sub/file3.txt": "content3",
		"other/file4.txt":    "content4",
	}

	for path, content := range objects {
		err := backend.Put(ctx, path, strings.NewReader(content), nil)
		require.NoError(t, err)
	}

	// List with prefix "list/" recursively
	result, err := backend.List(ctx, "list/", true)
	require.NoError(t, err)
	assert.Len(t, result, 3) // file1.txt, file2.txt, sub/file3.txt

	// List with prefix "list/" non-recursively
	result, err = backend.List(ctx, "list/", false)
	require.NoError(t, err)
	assert.Len(t, result, 2) // file1.txt, file2.txt (sub/file3.txt should be excluded)

	// Verify object info
	for _, obj := range result {
		assert.True(t, strings.HasPrefix(obj.Path, "list/"))
		assert.Greater(t, obj.Size, int64(0))
		assert.Greater(t, obj.LastModified, int64(0))
		assert.NotEmpty(t, obj.ETag)
	}
}

func testMetadata(t *testing.T, backend storage.Backend) {
	ctx := context.Background()
	testPath := "test/metadata-test.txt"

	// Put object with metadata
	initialMetadata := map[string]string{
		"content-type": "application/json",
		"cache-control": "no-cache",
		"custom-header": "custom-value",
	}

	err := backend.Put(ctx, testPath, strings.NewReader(`{"test": true}`), initialMetadata)
	require.NoError(t, err)

	// Get metadata
	metadata, err := backend.GetMetadata(ctx, testPath)
	require.NoError(t, err)
	assert.Equal(t, "application/json", metadata["content-type"])
	assert.Equal(t, "no-cache", metadata["cache-control"])
	assert.Equal(t, "custom-value", metadata["custom-header"])

	// Update metadata
	updatedMetadata := map[string]string{
		"content-type": "text/plain",
		"new-header":   "new-value",
	}

	err = backend.SetMetadata(ctx, testPath, updatedMetadata)
	require.NoError(t, err)

	// Get updated metadata
	metadata, err = backend.GetMetadata(ctx, testPath)
	require.NoError(t, err)
	assert.Equal(t, "text/plain", metadata["content-type"])
	assert.Equal(t, "new-value", metadata["new-header"])
}

func testPathValidation(t *testing.T, backend storage.Backend) {
	ctx := context.Background()

	// Test invalid paths
	invalidPaths := []string{
		"",
		"../etc/passwd",
		"test/../passwd",
		"/absolute/path",
	}

	for _, invalidPath := range invalidPaths {
		err := backend.Put(ctx, invalidPath, strings.NewReader("test"), nil)
		assert.Equal(t, storage.ErrInvalidPath, err, "Path %s should be invalid", invalidPath)

		_, _, err = backend.Get(ctx, invalidPath)
		assert.Equal(t, storage.ErrInvalidPath, err, "Path %s should be invalid", invalidPath)

		err = backend.Delete(ctx, invalidPath)
		assert.Equal(t, storage.ErrInvalidPath, err, "Path %s should be invalid", invalidPath)

		_, err = backend.Exists(ctx, invalidPath)
		assert.Equal(t, storage.ErrInvalidPath, err, "Path %s should be invalid", invalidPath)
	}

	// Test valid paths
	validPaths := []string{
		"simple.txt",
		"path/to/file.txt",
		"very/deep/path/to/file.txt",
		"file-with-dashes.txt",
		"file_with_underscores.txt",
	}

	for _, validPath := range validPaths {
		err := backend.Put(ctx, validPath, strings.NewReader("test"), nil)
		assert.NoError(t, err, "Path %s should be valid", validPath)

		exists, err := backend.Exists(ctx, validPath)
		assert.NoError(t, err, "Path %s should be valid", validPath)
		assert.True(t, exists, "Object should exist at %s", validPath)
	}
}