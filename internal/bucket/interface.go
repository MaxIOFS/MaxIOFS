package bucket

import (
	"context"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
)

// Bucket represents a storage bucket
type Bucket struct {
	Name              string             `json:"name"`
	TenantID          string             `json:"tenant_id"`  // Tenant ID for multi-tenancy isolation
	OwnerID           string             `json:"owner_id"`   // Owner user ID
	OwnerType         string             `json:"owner_type"` // "user" or "tenant"
	IsPublic          bool               `json:"is_public"`  // Public access flag
	CreatedAt         time.Time          `json:"created_at"`
	Region            string             `json:"region"`
	Versioning        *VersioningConfig  `json:"versioning,omitempty"`
	ObjectLock        *ObjectLockConfig  `json:"object_lock,omitempty"`
	Policy            *Policy            `json:"policy,omitempty"`
	Lifecycle         *LifecycleConfig   `json:"lifecycle,omitempty"`
	CORS              *CORSConfig        `json:"cors,omitempty"`
	Encryption        *EncryptionConfig  `json:"encryption,omitempty"`
	PublicAccessBlock *PublicAccessBlock `json:"public_access_block,omitempty"`
	Tags              map[string]string  `json:"tags,omitempty"`
	Metadata          map[string]string  `json:"metadata,omitempty"`

	// Cached metrics for performance (updated incrementally)
	ObjectCount int64 `json:"object_count"` // Cached object count
	TotalSize   int64 `json:"total_size"`   // Cached total size in bytes
}

// Manager defines the interface for bucket management
type Manager interface {
	// Basic bucket operations
	CreateBucket(ctx context.Context, tenantID, name string) error
	DeleteBucket(ctx context.Context, tenantID, name string) error
	ListBuckets(ctx context.Context, tenantID string) ([]Bucket, error)
	BucketExists(ctx context.Context, tenantID, name string) (bool, error)
	GetBucketInfo(ctx context.Context, tenantID, name string) (*Bucket, error)
	UpdateBucket(ctx context.Context, tenantID, name string, bucket *Bucket) error

	// Configuration operations
	GetBucketPolicy(ctx context.Context, tenantID, name string) (*Policy, error)
	SetBucketPolicy(ctx context.Context, tenantID, name string, policy *Policy) error
	DeleteBucketPolicy(ctx context.Context, tenantID, name string) error

	// Versioning
	GetVersioning(ctx context.Context, tenantID, name string) (*VersioningConfig, error)
	SetVersioning(ctx context.Context, tenantID, name string, config *VersioningConfig) error

	// Lifecycle
	GetLifecycle(ctx context.Context, tenantID, name string) (*LifecycleConfig, error)
	SetLifecycle(ctx context.Context, tenantID, name string, config *LifecycleConfig) error
	DeleteLifecycle(ctx context.Context, tenantID, name string) error

	// CORS
	GetCORS(ctx context.Context, tenantID, name string) (*CORSConfig, error)
	SetCORS(ctx context.Context, tenantID, name string, config *CORSConfig) error
	DeleteCORS(ctx context.Context, tenantID, name string) error

	// Bucket Tagging
	SetBucketTags(ctx context.Context, tenantID, name string, tags map[string]string) error

	// Object Lock
	GetObjectLockConfig(ctx context.Context, tenantID, name string) (*ObjectLockConfig, error)
	SetObjectLockConfig(ctx context.Context, tenantID, name string, config *ObjectLockConfig) error

	// ACL operations
	GetBucketACL(ctx context.Context, tenantID, name string) (interface{}, error)
	SetBucketACL(ctx context.Context, tenantID, name string, acl interface{}) error
	GetACLManager() interface{} // Returns acl.Manager but uses interface{} to avoid circular dependency

	// Metrics management (for incremental updates)
	IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
	DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
	RecalculateMetrics(ctx context.Context, tenantID, name string) error

	// Health check
	IsReady() bool
}

// NewManager creates a new bucket manager using BadgerDB for metadata
func NewManager(storage storage.Backend, metadataStore metadata.Store) Manager {
	return NewBadgerManager(storage, metadataStore)
}
