package storage

import (
	"context"
	"fmt"
	"io"
)

// Backend defines the interface for all storage backends
type Backend interface {
	// Basic operations
	Put(ctx context.Context, path string, data io.Reader, metadata map[string]string) error
	Get(ctx context.Context, path string) (io.ReadCloser, map[string]string, error)
	Delete(ctx context.Context, path string) error
	Exists(ctx context.Context, path string) (bool, error)

	// Listing
	List(ctx context.Context, prefix string, recursive bool) ([]ObjectInfo, error)

	// Metadata
	GetMetadata(ctx context.Context, path string) (map[string]string, error)
	SetMetadata(ctx context.Context, path string, metadata map[string]string) error

	// Lifecycle
	Close() error
}

// ObjectInfo represents information about a stored object
type ObjectInfo struct {
	Path         string
	Size         int64
	LastModified int64
	ETag         string
	Metadata     map[string]string
}

// NewBackend creates a new storage backend based on configuration
func NewBackend(config Config) (Backend, error) {
	switch config.Backend {
	case "filesystem", "":
		// Empty string defaults to filesystem
		return NewFilesystemBackend(config)
	default:
		return nil, fmt.Errorf("unsupported storage backend: %s (only 'filesystem' is currently supported)", config.Backend)
	}
}
