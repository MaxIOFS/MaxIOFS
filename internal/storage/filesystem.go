package storage

import (
	"context"
	"io"
)

// FilesystemBackend implements the Backend interface for local filesystem storage
type FilesystemBackend struct {
	rootPath string
	config   Config
}

// NewFilesystemBackend creates a new filesystem storage backend
func NewFilesystemBackend(config Config) (*FilesystemBackend, error) {
	// TODO: Implement in Fase 1.1 - Storage Backend Implementation
	return &FilesystemBackend{
		rootPath: config.Root,
		config:   config,
	}, nil
}

// Put stores an object in the filesystem
func (fs *FilesystemBackend) Put(ctx context.Context, path string, data io.Reader, metadata map[string]string) error {
	// TODO: Implement filesystem put operation
	panic("not implemented")
}

// Get retrieves an object from the filesystem
func (fs *FilesystemBackend) Get(ctx context.Context, path string) (io.ReadCloser, map[string]string, error) {
	// TODO: Implement filesystem get operation
	panic("not implemented")
}

// Delete removes an object from the filesystem
func (fs *FilesystemBackend) Delete(ctx context.Context, path string) error {
	// TODO: Implement filesystem delete operation
	panic("not implemented")
}

// Exists checks if an object exists in the filesystem
func (fs *FilesystemBackend) Exists(ctx context.Context, path string) (bool, error) {
	// TODO: Implement filesystem exists check
	panic("not implemented")
}

// List lists objects with the given prefix
func (fs *FilesystemBackend) List(ctx context.Context, prefix string, recursive bool) ([]ObjectInfo, error) {
	// TODO: Implement filesystem list operation
	panic("not implemented")
}

// GetMetadata retrieves object metadata
func (fs *FilesystemBackend) GetMetadata(ctx context.Context, path string) (map[string]string, error) {
	// TODO: Implement filesystem metadata retrieval
	panic("not implemented")
}

// SetMetadata sets object metadata
func (fs *FilesystemBackend) SetMetadata(ctx context.Context, path string, metadata map[string]string) error {
	// TODO: Implement filesystem metadata setting
	panic("not implemented")
}

// Close closes the filesystem backend
func (fs *FilesystemBackend) Close() error {
	// TODO: Implement filesystem cleanup if needed
	return nil
}