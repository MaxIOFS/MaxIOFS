package storage

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FilesystemBackend implements the Backend interface for local filesystem storage
type FilesystemBackend struct {
	rootPath string
	config   Config
}

// NewFilesystemBackend creates a new filesystem storage backend
func NewFilesystemBackend(config Config) (*FilesystemBackend, error) {
	// Ensure root path exists
	if err := os.MkdirAll(config.Root, 0755); err != nil {
		return nil, NewErrorWithCause("CreateRootDir", "Failed to create root directory", err)
	}

	backend := &FilesystemBackend{
		rootPath: config.Root,
		config:   config,
	}

	return backend, nil
}

// Put stores an object in the filesystem
func (fs *FilesystemBackend) Put(ctx context.Context, path string, data io.Reader, metadata map[string]string) error {
	if err := fs.validatePath(path); err != nil {
		return err
	}

	fullPath := fs.getFullPath(path)

	// Create directory if it doesn't exist
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return NewErrorWithCause("CreateDirectory", "Failed to create directory", err)
	}

	// Create temporary file
	tempFile, err := os.CreateTemp(dir, ".tmp_")
	if err != nil {
		return NewErrorWithCause("CreateTempFile", "Failed to create temporary file", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Copy data and calculate hash
	hasher := md5.New()
	multiWriter := io.MultiWriter(tempFile, hasher)

	size, err := io.Copy(multiWriter, data)
	if err != nil {
		return NewErrorWithCause("WriteData", "Failed to write data", err)
	}

	tempFile.Close()

	// Add calculated metadata
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata["size"] = fmt.Sprintf("%d", size)
	metadata["etag"] = hex.EncodeToString(hasher.Sum(nil))
	metadata["last_modified"] = fmt.Sprintf("%d", time.Now().Unix())

	// Save metadata
	if err := fs.saveMetadata(path, metadata); err != nil {
		return err
	}

	// Atomic move
	if err := os.Rename(tempFile.Name(), fullPath); err != nil {
		return NewErrorWithCause("AtomicMove", "Failed to move file to final location", err)
	}

	return nil
}

// Get retrieves an object from the filesystem
func (fs *FilesystemBackend) Get(ctx context.Context, path string) (io.ReadCloser, map[string]string, error) {
	if err := fs.validatePath(path); err != nil {
		return nil, nil, err
	}

	fullPath := fs.getFullPath(path)

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return nil, nil, ErrObjectNotFound
	} else if err != nil {
		return nil, nil, NewErrorWithCause("StatFile", "Failed to stat file", err)
	}

	// Open file
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, nil, NewErrorWithCause("OpenFile", "Failed to open file", err)
	}

	// Get metadata
	metadata, err := fs.GetMetadata(ctx, path)
	if err != nil {
		file.Close()
		return nil, nil, err
	}

	return file, metadata, nil
}

// Delete removes an object from the filesystem
func (fs *FilesystemBackend) Delete(ctx context.Context, path string) error {
	if err := fs.validatePath(path); err != nil {
		return err
	}

	fullPath := fs.getFullPath(path)

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return ErrObjectNotFound
	}

	// Delete file or directory
	// Check if it's a directory (ends with /)
	if strings.HasSuffix(path, "/") {
		// For directories, use RemoveAll to remove directory and all contents
		if err := os.RemoveAll(fullPath); err != nil {
			return NewErrorWithCause("DeleteDirectory", "Failed to delete directory", err)
		}
	} else {
		// For files, use Remove
		if err := os.Remove(fullPath); err != nil {
			return NewErrorWithCause("DeleteFile", "Failed to delete file", err)
		}
	}

	// Delete metadata
	metadataPath := fs.getMetadataPath(path)
	if _, err := os.Stat(metadataPath); err == nil {
		os.Remove(metadataPath) // Ignore errors for metadata cleanup
	}

	return nil
}

// Exists checks if an object exists in the filesystem
func (fs *FilesystemBackend) Exists(ctx context.Context, path string) (bool, error) {
	if err := fs.validatePath(path); err != nil {
		return false, err
	}

	fullPath := fs.getFullPath(path)
	_, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, NewErrorWithCause("StatFile", "Failed to stat file", err)
	}

	return true, nil
}

// List lists objects with the given prefix
func (fs *FilesystemBackend) List(ctx context.Context, prefix string, recursive bool) ([]ObjectInfo, error) {
	var objects []ObjectInfo

	searchPath := fs.getFullPath(prefix)

	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip directories and metadata files
		if info.IsDir() || strings.HasSuffix(path, ".metadata") {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(fs.rootPath, path)
		if err != nil {
			return nil
		}

		// Convert to forward slashes for consistency
		relPath = filepath.ToSlash(relPath)

		// Check if it matches prefix
		if !strings.HasPrefix(relPath, prefix) {
			return nil
		}

		// For non-recursive, skip if path contains additional slashes after prefix
		if !recursive {
			remaining := strings.TrimPrefix(relPath, prefix)
			if strings.Contains(remaining, "/") {
				return nil
			}
		}

		// Create object info
		obj := ObjectInfo{
			Path:         relPath,
			Size:         info.Size(),
			LastModified: info.ModTime().Unix(),
		}

		// Try to get ETag from metadata
		if metadata, err := fs.GetMetadata(context.Background(), relPath); err == nil {
			if etag, ok := metadata["etag"]; ok {
				obj.ETag = etag
			}
			obj.Metadata = metadata
		}

		objects = append(objects, obj)
		return nil
	})

	if err != nil {
		return nil, NewErrorWithCause("WalkDirectory", "Failed to walk directory", err)
	}

	return objects, nil
}

// GetMetadata retrieves object metadata
func (fs *FilesystemBackend) GetMetadata(ctx context.Context, path string) (map[string]string, error) {
	if err := fs.validatePath(path); err != nil {
		return nil, err
	}

	metadataPath := fs.getMetadataPath(path)

	// Check if metadata file exists
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		// Return basic metadata from file stats if metadata file doesn't exist
		return fs.generateBasicMetadata(path)
	}

	// Read metadata file
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, NewErrorWithCause("ReadMetadata", "Failed to read metadata file", err)
	}

	var metadata map[string]string
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, NewErrorWithCause("ParseMetadata", "Failed to parse metadata", err)
	}

	return metadata, nil
}

// SetMetadata sets object metadata
func (fs *FilesystemBackend) SetMetadata(ctx context.Context, path string, metadata map[string]string) error {
	if err := fs.validatePath(path); err != nil {
		return err
	}

	return fs.saveMetadata(path, metadata)
}

// Close closes the filesystem backend
func (fs *FilesystemBackend) Close() error {
	// Filesystem backend doesn't need explicit cleanup
	return nil
}

// Helper methods

// validatePath validates that the path is safe for filesystem operations
func (fs *FilesystemBackend) validatePath(path string) error {
	if path == "" {
		return ErrInvalidPath
	}

	// Prevent directory traversal attacks
	if strings.Contains(path, "..") {
		return ErrInvalidPath
	}

	// Ensure path doesn't start with /
	if strings.HasPrefix(path, "/") {
		return ErrInvalidPath
	}

	return nil
}

// getFullPath returns the full filesystem path for a given object path
func (fs *FilesystemBackend) getFullPath(path string) string {
	return filepath.Join(fs.rootPath, filepath.FromSlash(path))
}

// getMetadataPath returns the path for the metadata file
func (fs *FilesystemBackend) getMetadataPath(path string) string {
	return fs.getFullPath(path) + ".metadata"
}

// saveMetadata saves metadata to a file
func (fs *FilesystemBackend) saveMetadata(path string, metadata map[string]string) error {
	metadataPath := fs.getMetadataPath(path)

	// Create directory for metadata file
	dir := filepath.Dir(metadataPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return NewErrorWithCause("CreateMetadataDirectory", "Failed to create metadata directory", err)
	}

	// Marshal metadata
	data, err := json.Marshal(metadata)
	if err != nil {
		return NewErrorWithCause("MarshalMetadata", "Failed to marshal metadata", err)
	}

	// Write metadata file
	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return NewErrorWithCause("WriteMetadata", "Failed to write metadata file", err)
	}

	return nil
}

// generateBasicMetadata generates basic metadata from file stats
func (fs *FilesystemBackend) generateBasicMetadata(path string) (map[string]string, error) {
	fullPath := fs.getFullPath(path)

	stat, err := os.Stat(fullPath)
	if err != nil {
		return nil, NewErrorWithCause("StatFile", "Failed to stat file", err)
	}

	metadata := make(map[string]string)
	metadata["size"] = fmt.Sprintf("%d", stat.Size())
	metadata["last_modified"] = fmt.Sprintf("%d", stat.ModTime().Unix())

	// Try to calculate ETag by reading file
	file, err := os.Open(fullPath)
	if err == nil {
		defer file.Close()
		hasher := md5.New()
		if _, err := io.Copy(hasher, file); err == nil {
			metadata["etag"] = hex.EncodeToString(hasher.Sum(nil))
		}
	}

	return metadata, nil
}

// RemoveDirectory removes a directory and all its contents
// This is a special method for the FilesystemBackend to support bucket deletion
func (fs *FilesystemBackend) RemoveDirectory(path string) error {
	if err := fs.validatePath(path); err != nil {
		return err
	}

	fullPath := fs.getFullPath(path)

	// Check if directory exists
	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return nil // Already deleted, nothing to do
	}
	if err != nil {
		return NewErrorWithCause("StatDirectory", "Failed to stat directory", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}

	// Remove directory and all contents
	if err := os.RemoveAll(fullPath); err != nil {
		return NewErrorWithCause("RemoveDirectory", "Failed to remove directory", err)
	}

	return nil
}