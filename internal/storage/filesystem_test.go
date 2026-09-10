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

		err := backend.putAt(ctx, "test-file.txt", reader, metadata)
		assert.NoError(t, err)

		// Get the object
		rc, meta, err := backend.getAt(ctx, "test-file.txt")
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

		err := backend.putAt(ctx, "folder1/folder2/nested.txt", reader, nil)
		assert.NoError(t, err)

		// Verify file exists
		exists, err := backend.existsAt(ctx, "folder1/folder2/nested.txt")
		assert.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("Get non-existent object", func(t *testing.T) {
		rc, meta, err := backend.getAt(ctx, "does-not-exist.txt")
		assert.Error(t, err)
		assert.Equal(t, ErrObjectNotFound, err)
		assert.Nil(t, rc)
		assert.Nil(t, meta)
	})

	t.Run("Put with invalid path", func(t *testing.T) {
		data := []byte("test")
		reader := bytes.NewReader(data)

		// Path traversal attempt
		err := backend.putAt(ctx, "../escape.txt", reader, nil)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidPath, err)

		// Absolute path
		err = backend.putAt(ctx, "/absolute.txt", reader, nil)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidPath, err)

		// Empty path
		err = backend.putAt(ctx, "", reader, nil)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidPath, err)
	})

	t.Run("Put cleans up prepared metadata on rename failure", func(t *testing.T) {
		conflictPath := filepath.Join(tmpDir, "conflict")
		require.NoError(t, os.MkdirAll(conflictPath, 0755))

		err := backend.putAt(ctx, "conflict", bytes.NewReader([]byte("data")), map[string]string{"content-type": "text/plain"})
		require.Error(t, err)

		_, statErr := os.Stat(filepath.Join(tmpDir, "conflict.metadata"))
		assert.True(t, os.IsNotExist(statErr), "metadata file should not remain after failed put")
	})
}

// TestPutDirectoryMarker tests directory marker creation
// A key ending in "/" is an ordinary object: no directory is created for it.
func TestPutFolderMarkerKey(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)
	ctx := context.Background()

	ref := ObjectRef{Bucket: "bucket", Key: "my-folder/"}
	require.NoError(t, backend.Put(ctx, ref, strings.NewReader(""), nil))

	meta, err := backend.GetMetadata(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "0", meta["size"])
	assert.Equal(t, "my-folder/", meta[MetadataKeyField])

	_, err = os.Stat(filepath.Join(tmpDir, "bucket", "my-folder"))
	assert.True(t, os.IsNotExist(err), "no directory is created for a folder-marker key")

	nested := ObjectRef{Bucket: "bucket", Key: "level1/level2/level3/"}
	require.NoError(t, backend.Put(ctx, nested, strings.NewReader(""), nil))

	for _, name := range []string{"level1", filepath.Join("level1", "level2")} {
		_, err := os.Stat(filepath.Join(tmpDir, "bucket", name))
		assert.True(t, os.IsNotExist(err), "no directory is created for %s", name)
	}
}

// TestDelete tests object deletion
func TestDelete(t *testing.T) {
	backend, tmpDir := createTestBackend(t)
	defer cleanup(tmpDir)
	ctx := context.Background()

	t.Run("Delete existing file", func(t *testing.T) {
		// Create file
		data := []byte("delete me")
		err := backend.putAt(ctx, "to-delete.txt", bytes.NewReader(data), nil)
		require.NoError(t, err)

		// Delete file
		err = backend.deleteAt(ctx, "to-delete.txt")
		assert.NoError(t, err)

		// Verify file is gone
		exists, err := backend.existsAt(ctx, "to-delete.txt")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("Delete a folder-marker key", func(t *testing.T) {
		ref := ObjectRef{Bucket: "bucket", Key: "delete-folder/"}
		require.NoError(t, backend.Put(ctx, ref, strings.NewReader(""), nil))
		require.NoError(t, backend.Delete(ctx, ref))

		exists, err := backend.Exists(ctx, ref)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("A folder-marker key and the same name without the slash are different objects", func(t *testing.T) {
		marker := ObjectRef{Bucket: "bucket", Key: "both/"}
		plain := ObjectRef{Bucket: "bucket", Key: "both"}
		require.NoError(t, backend.Put(ctx, marker, strings.NewReader(""), nil))
		require.NoError(t, backend.Put(ctx, plain, strings.NewReader("data"), nil))

		require.NoError(t, backend.Delete(ctx, marker))

		exists, err := backend.Exists(ctx, plain)
		require.NoError(t, err)
		assert.True(t, exists, "deleting the marker must not touch the object")
	})

	t.Run("Delete non-existent object", func(t *testing.T) {
		err := backend.deleteAt(ctx, "does-not-exist.txt")
		assert.Error(t, err)
		assert.Equal(t, ErrObjectNotFound, err)
	})

	t.Run("Delete with invalid path", func(t *testing.T) {
		err := backend.deleteAt(ctx, "../escape.txt")
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
		err := backend.putAt(ctx, "exists.txt", bytes.NewReader(data), nil)
		require.NoError(t, err)

		exists, err := backend.existsAt(ctx, "exists.txt")
		assert.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("Check non-existent file", func(t *testing.T) {
		exists, err := backend.existsAt(ctx, "not-exists.txt")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("Check with invalid path", func(t *testing.T) {
		exists, err := backend.existsAt(ctx, "../escape.txt")
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

	keys := []string{
		"file1.txt",
		"file2.txt",
		"folder/file3.txt",
		"folder/subfolder/file4.txt",
	}

	require.NoError(t, backend.CreateBucket(ctx, "listing"))
	for _, key := range keys {
		data := []byte("content of " + key)
		require.NoError(t, backend.Put(ctx, ObjectRef{Bucket: "listing", Key: key}, bytes.NewReader(data), nil))
	}
	require.NoError(t, backend.Put(ctx, ObjectRef{Bucket: "elsewhere", Key: "other.txt"}, bytes.NewReader([]byte("x")), nil))

	t.Run("Lists every object in the bucket", func(t *testing.T) {
		objects, err := backend.List(ctx, "listing")
		assert.NoError(t, err)
		assert.Equal(t, keys, listedRefs(objects))
	})

	t.Run("Does not reach into other buckets", func(t *testing.T) {
		objects, err := backend.List(ctx, "elsewhere")
		assert.NoError(t, err)
		assert.Equal(t, []string{"other.txt"}, listedRefs(objects))
	})

	t.Run("Absent bucket lists empty", func(t *testing.T) {
		objects, err := backend.List(ctx, "nonexistent")
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

		err := backend.putAt(ctx, "meta-test.txt", bytes.NewReader(data), metadata)
		require.NoError(t, err)

		meta, err := backend.metadataAt(ctx, "meta-test.txt")
		assert.NoError(t, err)
		assert.Equal(t, "custom-value", meta["custom-key"])
		assert.NotEmpty(t, meta["size"])
		assert.NotEmpty(t, meta["etag"])
	})

	t.Run("Set metadata for existing object", func(t *testing.T) {
		data := []byte("set meta test")
		err := backend.putAt(ctx, "set-meta.txt", bytes.NewReader(data), nil)
		require.NoError(t, err)

		newMeta := map[string]string{
			"new-key": "new-value",
		}
		err = backend.setMetadataAt(ctx, "set-meta.txt", newMeta)
		assert.NoError(t, err)

		meta, err := backend.metadataAt(ctx, "set-meta.txt")
		assert.NoError(t, err)
		assert.Equal(t, "new-value", meta["new-key"])
	})

	t.Run("Get metadata for non-existent object", func(t *testing.T) {
		meta, err := backend.metadataAt(ctx, "no-meta.txt")
		assert.Error(t, err)
		assert.Equal(t, ErrObjectNotFound, err)
		assert.Nil(t, meta)
	})

	t.Run("Generate basic metadata when metadata file missing", func(t *testing.T) {
		// Create file directly in filesystem without metadata
		fullPath := filepath.Join(tmpDir, "no-metadata.txt")
		err := os.WriteFile(fullPath, []byte("direct write"), 0644)
		require.NoError(t, err)

		meta, err := backend.metadataAt(ctx, "no-metadata.txt")
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
		{"Valid dots in filename", "folder/file..txt", false},
		{"Valid triple-dot segment", "folder/.../file.txt", false},
		{"Empty path", "", true},
		{"Path traversal dots", "../file.txt", true},
		{"Path traversal in middle", "folder/../file.txt", true},
		{"Absolute path", "/file.txt", true},
		{"Windows separator", "folder\\file.txt", true},
		{"Windows traversal", "folder\\..\\file.txt", true},
		{"Windows drive absolute", "C:/tmp/file.txt", true},
		{"Drive qualified relative", "C:tmp/file.txt", true},
		{"Colon outside drive prefix", "folder/file:name.txt", false},
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
			err := backend.putAt(ctx, file, bytes.NewReader(data), nil)
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
		err := backend.putAt(ctx, "just-file.txt", bytes.NewReader(data), nil)
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
				ref := ObjectRef{Bucket: "concurrent", Key: string(rune('a'+n)) + ".txt"}
				data := []byte(strings.Repeat("x", n*100))
				err := backend.Put(ctx, ref, bytes.NewReader(data), nil)
				assert.NoError(t, err)
				done <- true
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}

		// Verify all files exist
		objects, err := backend.List(ctx, "concurrent")
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
