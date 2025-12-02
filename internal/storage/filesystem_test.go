package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestBackend(t *testing.T) (*FilesystemBackend, string) {
	tmpDir, err := os.MkdirTemp("", "storage-test-*")
	require.NoError(t, err)

	cfg := config.StorageConfig{
		Root: tmpDir,
	}

	backend, err := NewFilesystemBackend(cfg)
	require.NoError(t, err)
	require.NotNil(t, backend)

	return backend, tmpDir
}

func cleanup(tmpDir string) {
	os.RemoveAll(tmpDir)
}

// TestNewFilesystemBackend tests backend creation
func TestNewFilesystemBackend(t *testing.T) {
	t.Run("Create backend with valid config", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "storage-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		cfg := config.StorageConfig{
			Root: tmpDir,
		}

		backend, err := NewFilesystemBackend(cfg)
		assert.NoError(t, err)
		assert.NotNil(t, backend)
		assert.Equal(t, tmpDir, backend.GetRootPath())
	})

	t.Run("Create backend creates root directory", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "storage-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		rootPath := filepath.Join(tmpDir, "new-storage-root")
		cfg := config.StorageConfig{
			Root: rootPath,
		}

		backend, err := NewFilesystemBackend(cfg)
		assert.NoError(t, err)
		assert.NotNil(t, backend)

		// Verify directory was created
		info, err := os.Stat(rootPath)
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	})
}

// TestPutAndGet tests basic Put and Get operations
func TestPutAndGet(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)
	ctx := context.Background()

	t.Run("Put and get simple object", func(t *testing.T) {
		data := []byte("Hello, World!")
		reader := bytes.NewReader(data)
		metadata := map[string]string{
			"content-type": "text/plain",
			"custom-meta":  "test-value",
		}

		err := backend.Put(ctx, "test-file.txt", reader, metadata)
		assert.NoError(t, err)

		// Get the object
		rc, meta, err := backend.Get(ctx, "test-file.txt")
		assert.NoError(t, err)
		assert.NotNil(t, rc)
		defer rc.Close()

		// Verify data
		gotData, err := io.ReadAll(rc)
		assert.NoError(t, err)
		assert.Equal(t, data, gotData)

		// Verify metadata
		assert.Equal(t, "text/plain", meta["content-type"])
		assert.Equal(t, "test-value", meta["custom-meta"])
		assert.Equal(t, "13", meta["size"])
		assert.NotEmpty(t, meta["etag"])
		assert.NotEmpty(t, meta["last_modified"])
	})

	t.Run("Put object in nested path", func(t *testing.T) {
		data := []byte("nested content")
		reader := bytes.NewReader(data)

		err := backend.Put(ctx, "folder1/folder2/nested.txt", reader, nil)
		assert.NoError(t, err)

		// Verify file exists
		exists, err := backend.Exists(ctx, "folder1/folder2/nested.txt")
		assert.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("Get non-existent object", func(t *testing.T) {
		rc, meta, err := backend.Get(ctx, "does-not-exist.txt")
		assert.Error(t, err)
		assert.Equal(t, ErrObjectNotFound, err)
		assert.Nil(t, rc)
		assert.Nil(t, meta)
	})

	t.Run("Put with invalid path", func(t *testing.T) {
		data := []byte("test")
		reader := bytes.NewReader(data)

		// Path traversal attempt
		err := backend.Put(ctx, "../escape.txt", reader, nil)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidPath, err)

		// Absolute path
		err = backend.Put(ctx, "/absolute.txt", reader, nil)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidPath, err)

		// Empty path
		err = backend.Put(ctx, "", reader, nil)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidPath, err)
	})
}

// TestPutDirectoryMarker tests directory marker creation
func TestPutDirectoryMarker(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)
	ctx := context.Background()

	t.Run("Create directory marker", func(t *testing.T) {
		err := backend.Put(ctx, "my-folder/", nil, nil)
		assert.NoError(t, err)

		// Verify directory exists
		fullPath := filepath.Join(tmpDir, "my-folder")
		info, err := os.Stat(fullPath)
		assert.NoError(t, err)
		assert.True(t, info.IsDir())

		// Verify marker file exists
		markerPath := filepath.Join(fullPath, ".maxiofs-folder")
		_, err = os.Stat(markerPath)
		assert.NoError(t, err)

		// Verify metadata exists
		meta, err := backend.GetMetadata(ctx, "my-folder/")
		assert.NoError(t, err)
		assert.Equal(t, "0", meta["size"])
		assert.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", meta["etag"])
		assert.Equal(t, "application/x-directory", meta["content-type"])
	})

	t.Run("Create nested directory markers", func(t *testing.T) {
		err := backend.Put(ctx, "level1/level2/level3/", nil, nil)
		assert.NoError(t, err)

		// Verify all levels exist
		for _, path := range []string{"level1", "level1/level2", "level1/level2/level3"} {
			fullPath := filepath.Join(tmpDir, path)
			info, err := os.Stat(fullPath)
			assert.NoError(t, err)
			assert.True(t, info.IsDir())
		}
	})
}

// TestDelete tests object deletion
func TestDelete(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)
	ctx := context.Background()

	t.Run("Delete existing file", func(t *testing.T) {
		// Create file
		data := []byte("delete me")
		err := backend.Put(ctx, "to-delete.txt", bytes.NewReader(data), nil)
		require.NoError(t, err)

		// Delete file
		err = backend.Delete(ctx, "to-delete.txt")
		assert.NoError(t, err)

		// Verify file is gone
		exists, err := backend.Exists(ctx, "to-delete.txt")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("Delete directory marker", func(t *testing.T) {
		// Create directory
		err := backend.Put(ctx, "delete-folder/", nil, nil)
		require.NoError(t, err)

		// Delete directory
		err = backend.Delete(ctx, "delete-folder/")
		assert.NoError(t, err)

		// Verify directory is gone
		fullPath := filepath.Join(tmpDir, "delete-folder")
		_, err = os.Stat(fullPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("Delete non-existent object", func(t *testing.T) {
		err := backend.Delete(ctx, "does-not-exist.txt")
		assert.Error(t, err)
		assert.Equal(t, ErrObjectNotFound, err)
	})

	t.Run("Delete with invalid path", func(t *testing.T) {
		err := backend.Delete(ctx, "../escape.txt")
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidPath, err)
	})
}

// TestExists tests existence checks
func TestExists(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)
	ctx := context.Background()

	t.Run("Check existing file", func(t *testing.T) {
		// Create file
		data := []byte("exists")
		err := backend.Put(ctx, "exists.txt", bytes.NewReader(data), nil)
		require.NoError(t, err)

		exists, err := backend.Exists(ctx, "exists.txt")
		assert.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("Check non-existent file", func(t *testing.T) {
		exists, err := backend.Exists(ctx, "not-exists.txt")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("Check with invalid path", func(t *testing.T) {
		exists, err := backend.Exists(ctx, "../escape.txt")
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidPath, err)
		assert.False(t, exists)
	})
}

// TestList tests object listing
func TestList(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)
	ctx := context.Background()

	// Setup test files
	files := []string{
		"file1.txt",
		"file2.txt",
		"folder/file3.txt",
		"folder/subfolder/file4.txt",
	}

	for _, file := range files {
		data := []byte("content of " + file)
		err := backend.Put(ctx, file, bytes.NewReader(data), nil)
		require.NoError(t, err)
	}

	t.Run("List all objects recursively", func(t *testing.T) {
		objects, err := backend.List(ctx, "", true)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(objects), 4)

		// Verify all files are listed
		paths := make(map[string]bool)
		for _, obj := range objects {
			paths[obj.Path] = true
		}
		for _, file := range files {
			assert.True(t, paths[file], "File %s should be in list", file)
		}
	})

	t.Run("List with prefix non-recursive", func(t *testing.T) {
		objects, err := backend.List(ctx, "folder/", false)
		assert.NoError(t, err)

		// Should only contain file3.txt, not file4.txt (which is in subfolder)
		assert.Len(t, objects, 1)
		assert.Equal(t, "folder/file3.txt", objects[0].Path)
	})

	t.Run("List with prefix recursive", func(t *testing.T) {
		objects, err := backend.List(ctx, "folder/", true)
		assert.NoError(t, err)

		// Should contain both file3.txt and file4.txt
		assert.GreaterOrEqual(t, len(objects), 2)

		paths := make(map[string]bool)
		for _, obj := range objects {
			paths[obj.Path] = true
		}
		assert.True(t, paths["folder/file3.txt"])
		assert.True(t, paths["folder/subfolder/file4.txt"])
	})

	t.Run("List empty prefix", func(t *testing.T) {
		objects, err := backend.List(ctx, "nonexistent/", true)
		assert.NoError(t, err)
		assert.Empty(t, objects)
	})
}

// TestMetadata tests metadata operations
func TestMetadata(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)
	ctx := context.Background()

	t.Run("Get metadata for existing object", func(t *testing.T) {
		data := []byte("metadata test")
		metadata := map[string]string{
			"custom-key": "custom-value",
		}

		err := backend.Put(ctx, "meta-test.txt", bytes.NewReader(data), metadata)
		require.NoError(t, err)

		meta, err := backend.GetMetadata(ctx, "meta-test.txt")
		assert.NoError(t, err)
		assert.Equal(t, "custom-value", meta["custom-key"])
		assert.NotEmpty(t, meta["size"])
		assert.NotEmpty(t, meta["etag"])
	})

	t.Run("Set metadata for existing object", func(t *testing.T) {
		data := []byte("set meta test")
		err := backend.Put(ctx, "set-meta.txt", bytes.NewReader(data), nil)
		require.NoError(t, err)

		newMeta := map[string]string{
			"new-key": "new-value",
		}
		err = backend.SetMetadata(ctx, "set-meta.txt", newMeta)
		assert.NoError(t, err)

		meta, err := backend.GetMetadata(ctx, "set-meta.txt")
		assert.NoError(t, err)
		assert.Equal(t, "new-value", meta["new-key"])
	})

	t.Run("Get metadata for non-existent object", func(t *testing.T) {
		meta, err := backend.GetMetadata(ctx, "no-meta.txt")
		assert.Error(t, err)
		assert.Equal(t, ErrObjectNotFound, err)
		assert.Nil(t, meta)
	})

	t.Run("Generate basic metadata when metadata file missing", func(t *testing.T) {
		// Create file directly in filesystem without metadata
		fullPath := filepath.Join(tmpDir, "no-metadata.txt")
		err := os.WriteFile(fullPath, []byte("direct write"), 0644)
		require.NoError(t, err)

		meta, err := backend.GetMetadata(ctx, "no-metadata.txt")
		assert.NoError(t, err)
		assert.NotEmpty(t, meta["size"])
		assert.NotEmpty(t, meta["last_modified"])
		assert.NotEmpty(t, meta["etag"])
	})
}

// TestValidatePath tests path validation
func TestValidatePath(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"Valid simple path", "file.txt", false},
		{"Valid nested path", "folder/file.txt", false},
		{"Valid deep nested", "a/b/c/d/file.txt", false},
		{"Empty path", "", true},
		{"Path traversal dots", "../file.txt", true},
		{"Path traversal in middle", "folder/../file.txt", true},
		{"Absolute path", "/file.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := backend.validatePath(tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRemoveDirectory tests directory removal
func TestRemoveDirectory(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)
	ctx := context.Background()

	t.Run("Remove directory with contents", func(t *testing.T) {
		// Create directory with files
		files := []string{
			"bucket/file1.txt",
			"bucket/folder/file2.txt",
		}
		for _, file := range files {
			data := []byte("content")
			err := backend.Put(ctx, file, bytes.NewReader(data), nil)
			require.NoError(t, err)
		}

		// Remove directory
		err := backend.RemoveDirectory("bucket")
		assert.NoError(t, err)

		// Verify directory is gone
		fullPath := filepath.Join(tmpDir, "bucket")
		_, err = os.Stat(fullPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("Remove non-existent directory", func(t *testing.T) {
		err := backend.RemoveDirectory("non-existent")
		assert.NoError(t, err) // Should not error for non-existent
	})

	t.Run("Remove file instead of directory", func(t *testing.T) {
		// Create a file
		data := []byte("not a dir")
		err := backend.Put(ctx, "just-file.txt", bytes.NewReader(data), nil)
		require.NoError(t, err)

		// Try to remove as directory
		err = backend.RemoveDirectory("just-file.txt")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a directory")
	})
}

// TestConcurrentOperations tests concurrent access
func TestConcurrentOperations(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)
	ctx := context.Background()

	t.Run("Concurrent writes to different files", func(t *testing.T) {
		done := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			go func(n int) {
				path := filepath.Join("concurrent", string(rune('a'+n))+".txt")
				data := []byte(strings.Repeat("x", n*100))
				err := backend.Put(ctx, path, bytes.NewReader(data), nil)
				assert.NoError(t, err)
				done <- true
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}

		// Verify all files exist
		objects, err := backend.List(ctx, "concurrent/", true)
		assert.NoError(t, err)
		assert.Equal(t, 10, len(objects))
	})
}

// TestClose tests backend cleanup
func TestClose(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)

	err := backend.Close()
	assert.NoError(t, err)
}
