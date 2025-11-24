package metadata

import (
	"context"
	"errors"
)

// Common errors
var (
	ErrBucketNotFound      = errors.New("bucket not found")
	ErrBucketAlreadyExists = errors.New("bucket already exists")
	ErrObjectNotFound      = errors.New("object not found")
	ErrObjectAlreadyExists = errors.New("object already exists")
	ErrInvalidKey          = errors.New("invalid key")
	ErrInvalidBucketName   = errors.New("invalid bucket name")
	ErrUploadNotFound      = errors.New("multipart upload not found")
	ErrPartNotFound        = errors.New("part not found")
	ErrVersionNotFound     = errors.New("version not found")
)

// Store defines the interface for metadata storage operations
type Store interface {
	// ==================== Bucket Operations ====================

	// CreateBucket creates a new bucket with the given metadata
	CreateBucket(ctx context.Context, bucket *BucketMetadata) error

	// GetBucket retrieves metadata for a specific bucket
	GetBucket(ctx context.Context, tenantID, name string) (*BucketMetadata, error)

	// UpdateBucket updates an existing bucket's metadata
	UpdateBucket(ctx context.Context, bucket *BucketMetadata) error

	// DeleteBucket deletes a bucket and all its metadata
	DeleteBucket(ctx context.Context, tenantID, name string) error

	// ListBuckets lists all buckets for a tenant (empty tenantID = global)
	ListBuckets(ctx context.Context, tenantID string) ([]*BucketMetadata, error)

	// GetBucketByName finds a bucket by name across all tenants (for globally unique buckets)
	GetBucketByName(ctx context.Context, name string) (*BucketMetadata, error)

	// BucketExists checks if a bucket exists
	BucketExists(ctx context.Context, tenantID, name string) (bool, error)

	// UpdateBucketMetrics atomically updates bucket metrics (object count, total size)
	UpdateBucketMetrics(ctx context.Context, tenantID, bucketName string, objectCountDelta, sizeDelta int64) error

	// ==================== Object Operations ====================

	// PutObject stores metadata for an object (creates or updates)
	PutObject(ctx context.Context, obj *ObjectMetadata) error

	// GetObject retrieves metadata for a specific object
	GetObject(ctx context.Context, bucket, key string, versionID ...string) (*ObjectMetadata, error)

	// DeleteObject deletes an object's metadata
	DeleteObject(ctx context.Context, bucket, key string, versionID ...string) error

	// ListObjects lists objects in a bucket with optional prefix and pagination
	ListObjects(ctx context.Context, bucket, prefix, marker string, maxKeys int) ([]*ObjectMetadata, string, error)

	// ObjectExists checks if an object exists
	ObjectExists(ctx context.Context, bucket, key string) (bool, error)

	// ==================== Object Versioning ====================

	// PutObjectVersion stores a new version of an object
	PutObjectVersion(ctx context.Context, obj *ObjectMetadata, version *ObjectVersion) error

	// GetObjectVersions retrieves all versions of an object
	GetObjectVersions(ctx context.Context, bucket, key string) ([]*ObjectVersion, error)

	// ListAllObjectVersions lists all versions of all objects in a bucket (for versioning support)
	ListAllObjectVersions(ctx context.Context, bucket, prefix string, maxKeys int) ([]*ObjectVersion, error)

	// DeleteObjectVersion deletes a specific version of an object
	DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error

	// ==================== Multipart Uploads ====================

	// CreateMultipartUpload initiates a new multipart upload
	CreateMultipartUpload(ctx context.Context, upload *MultipartUploadMetadata) error

	// GetMultipartUpload retrieves metadata for a multipart upload
	GetMultipartUpload(ctx context.Context, uploadID string) (*MultipartUploadMetadata, error)

	// ListMultipartUploads lists all in-progress multipart uploads for a bucket
	ListMultipartUploads(ctx context.Context, bucket, prefix string, maxUploads int) ([]*MultipartUploadMetadata, error)

	// AbortMultipartUpload cancels a multipart upload and cleans up parts
	AbortMultipartUpload(ctx context.Context, uploadID string) error

	// CompleteMultipartUpload marks a multipart upload as complete
	CompleteMultipartUpload(ctx context.Context, uploadID string, obj *ObjectMetadata) error

	// PutPart stores metadata for a multipart upload part
	PutPart(ctx context.Context, part *PartMetadata) error

	// GetPart retrieves metadata for a specific part
	GetPart(ctx context.Context, uploadID string, partNumber int) (*PartMetadata, error)

	// ListParts lists all parts for a multipart upload
	ListParts(ctx context.Context, uploadID string) ([]*PartMetadata, error)

	// ==================== Tags ====================

	// PutObjectTags sets tags for an object
	PutObjectTags(ctx context.Context, bucket, key string, tags map[string]string) error

	// GetObjectTags retrieves tags for an object
	GetObjectTags(ctx context.Context, bucket, key string) (map[string]string, error)

	// DeleteObjectTags removes all tags from an object
	DeleteObjectTags(ctx context.Context, bucket, key string) error

	// ListObjectsByTags finds objects matching specific tags
	ListObjectsByTags(ctx context.Context, bucket string, tags map[string]string) ([]*ObjectMetadata, error)

	// ==================== Statistics & Maintenance ====================

	// GetBucketStats retrieves statistics for a bucket (object count, total size)
	GetBucketStats(ctx context.Context, tenantID, bucket string) (objectCount, totalSize int64, err error)

	// RecalculateBucketStats recalculates bucket statistics by scanning all objects
	RecalculateBucketStats(ctx context.Context, tenantID, bucket string) error

	// Compact runs garbage collection and compaction on the underlying storage
	Compact(ctx context.Context) error

	// Backup creates a backup of the metadata store
	Backup(ctx context.Context, path string) error

	// ==================== Lifecycle ====================

	// Close closes the metadata store and releases resources
	Close() error

	// IsReady returns true if the store is ready to serve requests
	IsReady() bool
}

// ListObjectsOptions provides options for listing objects
type ListObjectsOptions struct {
	Bucket      string
	Prefix      string
	Delimiter   string
	Marker      string
	MaxKeys     int
	VersionID   string
	StartAfter  string
	FetchOwner  bool
	EncodingType string
}

// ListObjectsResult contains the result of a list objects operation
type ListObjectsResult struct {
	Objects        []*ObjectMetadata
	CommonPrefixes []string
	NextMarker     string
	IsTruncated    bool
	MaxKeys        int
}
