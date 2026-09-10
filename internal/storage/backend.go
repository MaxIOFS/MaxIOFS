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

	// ListUploads returns every upload with something still stored for it, and
	// DeleteUpload discards an upload's stored parts wholesale.
	ListUploads(ctx context.Context) ([]string, error)
	DeleteUpload(ctx context.Context, uploadID string) error

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

// ValidateBackend reports whether the configuration names a backend this build
// implements, without opening anything.
func ValidateBackend(config Config) error {
	switch config.Backend {
	case "filesystem", "":
		return nil
	default:
		return fmt.Errorf("unsupported storage backend: %s (only 'filesystem' is currently supported)", config.Backend)
	}
}

// NewBackend creates a new storage backend based on configuration
func NewBackend(config Config) (Backend, error) {
	if err := ValidateBackend(config); err != nil {
		return nil, err
	}
	return NewFilesystemBackend(config)
}
