package storage

import (
	"context"
	"fmt"
	"io"
)

// Backend defines the interface for all storage backends
type Backend interface {
	Put(ctx context.Context, ref ObjectRef, data io.Reader, metadata map[string]string) error
	Get(ctx context.Context, ref ObjectRef) (io.ReadCloser, map[string]string, error)
	Delete(ctx context.Context, ref ObjectRef) error
	Exists(ctx context.Context, ref ObjectRef) (bool, error)
	GetMetadata(ctx context.Context, ref ObjectRef) (map[string]string, error)
	SetMetadata(ctx context.Context, ref ObjectRef, metadata map[string]string) error

	// List returns every object stored under one bucket, versions included.
	List(ctx context.Context, bucket string) ([]ObjectInfo, error)

	CreateBucket(ctx context.Context, bucket string) error
	DeleteBucket(ctx context.Context, bucket string) error

	// Multipart parts are not objects: they live outside any bucket and are
	// addressed by upload rather than by key.
	PutPart(ctx context.Context, uploadID string, partNumber int, data io.Reader, metadata map[string]string) error
	GetPart(ctx context.Context, uploadID string, partNumber int) (io.ReadCloser, map[string]string, error)
	PartMetadata(ctx context.Context, uploadID string, partNumber int) (map[string]string, error)
	PartExists(ctx context.Context, uploadID string, partNumber int) (bool, error)
	DeletePart(ctx context.Context, uploadID string, partNumber int) error

	Close() error
}

// ObjectInfo represents information about a stored object
type ObjectInfo struct {
	Ref          ObjectRef
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
