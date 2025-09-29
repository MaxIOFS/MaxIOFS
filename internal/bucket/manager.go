package bucket

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/storage"
)

// Manager defines the interface for bucket management
type Manager interface {
	// Basic bucket operations
	CreateBucket(ctx context.Context, name string) error
	DeleteBucket(ctx context.Context, name string) error
	ListBuckets(ctx context.Context) ([]Bucket, error)
	BucketExists(ctx context.Context, name string) (bool, error)
	GetBucketInfo(ctx context.Context, name string) (*Bucket, error)

	// Configuration operations
	GetBucketPolicy(ctx context.Context, name string) (*Policy, error)
	SetBucketPolicy(ctx context.Context, name string, policy *Policy) error
	DeleteBucketPolicy(ctx context.Context, name string) error

	// Versioning
	GetVersioning(ctx context.Context, name string) (*VersioningConfig, error)
	SetVersioning(ctx context.Context, name string, config *VersioningConfig) error

	// Lifecycle
	GetLifecycle(ctx context.Context, name string) (*LifecycleConfig, error)
	SetLifecycle(ctx context.Context, name string, config *LifecycleConfig) error
	DeleteLifecycle(ctx context.Context, name string) error

	// CORS
	GetCORS(ctx context.Context, name string) (*CORSConfig, error)
	SetCORS(ctx context.Context, name string, config *CORSConfig) error
	DeleteCORS(ctx context.Context, name string) error

	// Object Lock
	GetObjectLockConfig(ctx context.Context, name string) (*ObjectLockConfig, error)
	SetObjectLockConfig(ctx context.Context, name string, config *ObjectLockConfig) error

	// Health check
	IsReady() bool
}

// Bucket represents a storage bucket
type Bucket struct {
	Name         string            `json:"name"`
	CreatedAt    time.Time         `json:"created_at"`
	Region       string            `json:"region"`
	Versioning   *VersioningConfig `json:"versioning,omitempty"`
	ObjectLock   *ObjectLockConfig `json:"object_lock,omitempty"`
	Policy       *Policy           `json:"policy,omitempty"`
	Lifecycle    *LifecycleConfig  `json:"lifecycle,omitempty"`
	CORS         *CORSConfig       `json:"cors,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// bucketManager implements the Manager interface
type bucketManager struct {
	storage storage.Backend
}

// NewManager creates a new bucket manager
func NewManager(storage storage.Backend) Manager {
	return &bucketManager{
		storage: storage,
	}
}

// CreateBucket creates a new bucket
func (bm *bucketManager) CreateBucket(ctx context.Context, name string) error {
	// Validate bucket name
	if err := ValidateBucketName(name); err != nil {
		return err
	}

	// Check if bucket already exists
	exists, err := bm.bucketExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return ErrBucketAlreadyExists
	}

	// Create bucket metadata
	bucket := &Bucket{
		Name:      name,
		CreatedAt: time.Now(),
		Region:    "us-east-1", // Default region
		Metadata:  make(map[string]string),
	}

	// Store bucket metadata
	if err := bm.saveBucketMetadata(ctx, bucket); err != nil {
		return err
	}

	// Create bucket directory in storage
	bucketPath := name + "/"
	return bm.storage.Put(ctx, bucketPath+".maxiofs-bucket",
		strings.NewReader(""), map[string]string{
			"bucket-created": bucket.CreatedAt.Format(time.RFC3339),
		})
}

// DeleteBucket deletes a bucket
func (bm *bucketManager) DeleteBucket(ctx context.Context, name string) error {
	// Check if bucket exists
	exists, err := bm.bucketExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	// Check if bucket is empty
	isEmpty, err := bm.isBucketEmpty(ctx, name)
	if err != nil {
		return err
	}
	if !isEmpty {
		return ErrBucketNotEmpty
	}

	// Delete bucket marker
	bucketPath := name + "/"
	if err := bm.storage.Delete(ctx, bucketPath+".maxiofs-bucket"); err != nil {
		// Ignore not found errors, bucket might not have marker
		if err != storage.ErrObjectNotFound {
			return err
		}
	}

	// Delete bucket metadata
	return bm.deleteBucketMetadata(ctx, name)
}

// ListBuckets lists all buckets
func (bm *bucketManager) ListBuckets(ctx context.Context) ([]Bucket, error) {
	// List all .maxiofs-bucket files
	objects, err := bm.storage.List(ctx, "", true)
	if err != nil {
		return nil, err
	}

	var buckets []Bucket
	for _, obj := range objects {
		if strings.HasSuffix(obj.Path, ".maxiofs-bucket") {
			// Extract bucket name
			bucketName := strings.TrimSuffix(obj.Path, "/.maxiofs-bucket")

			// Load bucket metadata
			bucket, err := bm.loadBucketMetadata(ctx, bucketName)
			if err != nil {
				// If metadata not found, create basic bucket info
				bucket = &Bucket{
					Name:      bucketName,
					CreatedAt: time.Unix(obj.LastModified, 0),
					Region:    "us-east-1",
					Metadata:  make(map[string]string),
				}
			}

			// Ensure metadata is always initialized
			if bucket.Metadata == nil {
				bucket.Metadata = make(map[string]string)
			}

			buckets = append(buckets, *bucket)
		}
	}

	return buckets, nil
}

// BucketExists checks if a bucket exists
func (bm *bucketManager) BucketExists(ctx context.Context, name string) (bool, error) {
	return bm.bucketExists(ctx, name)
}

// GetBucketInfo retrieves bucket information
func (bm *bucketManager) GetBucketInfo(ctx context.Context, name string) (*Bucket, error) {
	exists, err := bm.bucketExists(ctx, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrBucketNotFound
	}

	bucket, err := bm.loadBucketMetadata(ctx, name)
	if err != nil {
		return nil, err
	}

	// Ensure metadata is always initialized
	if bucket.Metadata == nil {
		bucket.Metadata = make(map[string]string)
	}

	return bucket, nil
}

// Placeholder implementations for configuration methods
func (bm *bucketManager) GetBucketPolicy(ctx context.Context, name string) (*Policy, error) {
	panic("not implemented")
}

func (bm *bucketManager) SetBucketPolicy(ctx context.Context, name string, policy *Policy) error {
	panic("not implemented")
}

func (bm *bucketManager) DeleteBucketPolicy(ctx context.Context, name string) error {
	panic("not implemented")
}

func (bm *bucketManager) GetVersioning(ctx context.Context, name string) (*VersioningConfig, error) {
	panic("not implemented")
}

func (bm *bucketManager) SetVersioning(ctx context.Context, name string, config *VersioningConfig) error {
	panic("not implemented")
}

func (bm *bucketManager) GetLifecycle(ctx context.Context, name string) (*LifecycleConfig, error) {
	panic("not implemented")
}

func (bm *bucketManager) SetLifecycle(ctx context.Context, name string, config *LifecycleConfig) error {
	panic("not implemented")
}

func (bm *bucketManager) DeleteLifecycle(ctx context.Context, name string) error {
	panic("not implemented")
}

func (bm *bucketManager) GetCORS(ctx context.Context, name string) (*CORSConfig, error) {
	panic("not implemented")
}

func (bm *bucketManager) SetCORS(ctx context.Context, name string, config *CORSConfig) error {
	panic("not implemented")
}

func (bm *bucketManager) DeleteCORS(ctx context.Context, name string) error {
	panic("not implemented")
}

func (bm *bucketManager) GetObjectLockConfig(ctx context.Context, name string) (*ObjectLockConfig, error) {
	panic("not implemented")
}

func (bm *bucketManager) SetObjectLockConfig(ctx context.Context, name string, config *ObjectLockConfig) error {
	panic("not implemented")
}

// IsReady checks if the bucket manager is ready
func (bm *bucketManager) IsReady() bool {
	// TODO: Implement readiness check
	return true
}

// Helper methods

// bucketExists checks if a bucket exists by looking for its marker file
func (bm *bucketManager) bucketExists(ctx context.Context, name string) (bool, error) {
	bucketPath := name + "/.maxiofs-bucket"
	return bm.storage.Exists(ctx, bucketPath)
}

// isBucketEmpty checks if a bucket contains no objects
func (bm *bucketManager) isBucketEmpty(ctx context.Context, name string) (bool, error) {
	prefix := name + "/"
	objects, err := bm.storage.List(ctx, prefix, false)
	if err != nil {
		return false, err
	}

	// Filter out the bucket marker file
	for _, obj := range objects {
		if !strings.HasSuffix(obj.Path, ".maxiofs-bucket") {
			return false, nil
		}
	}

	return true, nil
}

// getBucketMetadataPath returns the path for bucket metadata
func (bm *bucketManager) getBucketMetadataPath(name string) string {
	return fmt.Sprintf(".maxiofs/buckets/%s.json", name)
}

// saveBucketMetadata saves bucket metadata to storage
func (bm *bucketManager) saveBucketMetadata(ctx context.Context, bucket *Bucket) error {
	data, err := json.Marshal(bucket)
	if err != nil {
		return fmt.Errorf("failed to marshal bucket metadata: %w", err)
	}

	metadataPath := bm.getBucketMetadataPath(bucket.Name)
	return bm.storage.Put(ctx, metadataPath, strings.NewReader(string(data)), map[string]string{
		"content-type": "application/json",
	})
}

// loadBucketMetadata loads bucket metadata from storage
func (bm *bucketManager) loadBucketMetadata(ctx context.Context, name string) (*Bucket, error) {
	metadataPath := bm.getBucketMetadataPath(name)

	reader, _, err := bm.storage.Get(ctx, metadataPath)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			return nil, ErrBucketNotFound
		}
		return nil, fmt.Errorf("failed to load bucket metadata: %w", err)
	}
	defer reader.Close()

	var bucket Bucket
	if err := json.NewDecoder(reader).Decode(&bucket); err != nil {
		return nil, fmt.Errorf("failed to decode bucket metadata: %w", err)
	}

	return &bucket, nil
}

// deleteBucketMetadata deletes bucket metadata from storage
func (bm *bucketManager) deleteBucketMetadata(ctx context.Context, name string) error {
	metadataPath := bm.getBucketMetadataPath(name)
	err := bm.storage.Delete(ctx, metadataPath)
	if err != nil && err != storage.ErrObjectNotFound {
		return fmt.Errorf("failed to delete bucket metadata: %w", err)
	}
	return nil
}