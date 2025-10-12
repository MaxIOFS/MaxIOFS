package bucket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/storage"
)

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

	// Object Lock
	GetObjectLockConfig(ctx context.Context, tenantID, name string) (*ObjectLockConfig, error)
	SetObjectLockConfig(ctx context.Context, tenantID, name string, config *ObjectLockConfig) error

	// Metrics management (for incremental updates)
	IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
	DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
	RecalculateMetrics(ctx context.Context, tenantID, name string) error

	// Health check
	IsReady() bool
}

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
func (bm *bucketManager) CreateBucket(ctx context.Context, tenantID, name string) error {
	// Validate bucket name
	if err := ValidateBucketName(name); err != nil {
		return err
	}

	// Check if bucket already exists for this tenant
	exists, err := bm.bucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if exists {
		return ErrBucketAlreadyExists
	}

	// Create bucket metadata
	bucket := &Bucket{
		Name:      name,
		TenantID:  tenantID,
		CreatedAt: time.Now(),
		Region:    "us-east-1", // Default region
		Metadata:  make(map[string]string),
	}

	// Store bucket metadata
	if err := bm.saveBucketMetadata(ctx, bucket); err != nil {
		return err
	}

	// Create bucket directory in storage with tenant prefix
	bucketPath := bm.getTenantBucketPath(tenantID, name) + "/"
	return bm.storage.Put(ctx, bucketPath+".maxiofs-bucket",
		strings.NewReader(""), map[string]string{
			"bucket-created": bucket.CreatedAt.Format(time.RFC3339),
			"tenant-id":      tenantID,
		})
}

// UpdateBucket updates an existing bucket's metadata
func (bm *bucketManager) UpdateBucket(ctx context.Context, tenantID, name string, bucket *Bucket) error {
	// Check if bucket exists
	exists, err := bm.bucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	// Validate bucket name and tenant match
	if bucket.Name != name {
		return fmt.Errorf("bucket name mismatch")
	}
	if bucket.TenantID != tenantID {
		return fmt.Errorf("tenant ID mismatch")
	}

	// Save updated metadata
	return bm.saveBucketMetadata(ctx, bucket)
}

// DeleteBucket deletes a bucket
func (bm *bucketManager) DeleteBucket(ctx context.Context, tenantID, name string) error {
	// Check if bucket exists
	exists, err := bm.bucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	// Check if bucket is empty
	isEmpty, err := bm.isBucketEmpty(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !isEmpty {
		return ErrBucketNotEmpty
	}

	// Delete bucket marker
	bucketPath := bm.getTenantBucketPath(tenantID, name) + "/"
	if err := bm.storage.Delete(ctx, bucketPath+".maxiofs-bucket"); err != nil {
		// Ignore not found errors, bucket might not have marker
		if err != storage.ErrObjectNotFound {
			return err
		}
	}

	// Delete bucket metadata
	if err := bm.deleteBucketMetadata(ctx, tenantID, name); err != nil {
		return err
	}

	// If using filesystem backend, remove the physical directory
	if fsBackend, ok := bm.storage.(interface{ RemoveDirectory(string) error }); ok {
		tenantBucketPath := bm.getTenantBucketPath(tenantID, name)
		if err := fsBackend.RemoveDirectory(tenantBucketPath); err != nil {
			// Log error but don't fail the operation since metadata is already deleted
			// TODO: Add proper logging
			_ = err
		}
	}

	return nil
}

// ListBuckets lists all buckets for a tenant
// If tenantID is empty, lists all buckets across all tenants (global admin view)
func (bm *bucketManager) ListBuckets(ctx context.Context, tenantID string) ([]Bucket, error) {
	var searchPrefix string
	if tenantID != "" {
		// List buckets for specific tenant only
		searchPrefix = tenantID + "/"
	}

	// List all .maxiofs-bucket files
	objects, err := bm.storage.List(ctx, searchPrefix, true)
	if err != nil {
		return nil, err
	}

	var buckets []Bucket
	for _, obj := range objects {
		if strings.HasSuffix(obj.Path, ".maxiofs-bucket") {
			// Extract tenant ID and bucket name from path
			// Path format: {tenant_id}/{bucket_name}/.maxiofs-bucket or {bucket_name}/.maxiofs-bucket
			pathParts := strings.Split(strings.TrimSuffix(obj.Path, "/.maxiofs-bucket"), "/")

			var extractedTenantID, bucketName string
			if len(pathParts) == 2 {
				// Tenant-scoped bucket: {tenant_id}/{bucket_name}
				extractedTenantID = pathParts[0]
				bucketName = pathParts[1]
			} else if len(pathParts) == 1 {
				// Legacy global bucket: {bucket_name}
				extractedTenantID = ""
				bucketName = pathParts[0]
			} else {
				// Unexpected format, skip
				continue
			}

			// If filtering by tenant, skip buckets from other tenants
			if tenantID != "" && extractedTenantID != tenantID {
				continue
			}

			// Load bucket metadata
			bucket, err := bm.loadBucketMetadata(ctx, extractedTenantID, bucketName)
			if err != nil {
				// If metadata not found, create basic bucket info
				bucket = &Bucket{
					Name:      bucketName,
					TenantID:  extractedTenantID,
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
func (bm *bucketManager) BucketExists(ctx context.Context, tenantID, name string) (bool, error) {
	return bm.bucketExists(ctx, tenantID, name)
}

// GetBucketInfo retrieves bucket information
func (bm *bucketManager) GetBucketInfo(ctx context.Context, tenantID, name string) (*Bucket, error) {
	exists, err := bm.bucketExists(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrBucketNotFound
	}

	bucket, err := bm.loadBucketMetadata(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}

	// Ensure metadata is always initialized
	if bucket.Metadata == nil {
		bucket.Metadata = make(map[string]string)
	}

	return bucket, nil
}

// Configuration implementations - store as JSON metadata files

// GetBucketPolicy retrieves the bucket policy
func (bm *bucketManager) GetBucketPolicy(ctx context.Context, tenantID, name string) (*Policy, error) {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrBucketNotFound
	}

	// Try to read policy file
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	policyPath := bucketPath + "/.maxiofs-policy"
	exists, err = bm.storage.Exists(ctx, policyPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrPolicyNotFound
	}

	// Read and unmarshal policy
	reader, _, err := bm.storage.Get(ctx, policyPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var policy Policy
	if err := json.NewDecoder(reader).Decode(&policy); err != nil {
		return nil, fmt.Errorf("failed to decode policy: %w", err)
	}

	return &policy, nil
}

func (bm *bucketManager) SetBucketPolicy(ctx context.Context, tenantID, name string, policy *Policy) error {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	if policy == nil {
		// If policy is nil, delete it
		return bm.DeleteBucketPolicy(ctx, tenantID, name)
	}

	// Marshal policy to JSON
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("failed to marshal policy: %w", err)
	}

	// Write policy file
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	policyPath := bucketPath + "/.maxiofs-policy"
	return bm.storage.Put(ctx, policyPath, strings.NewReader(string(policyJSON)), nil)
}

func (bm *bucketManager) DeleteBucketPolicy(ctx context.Context, tenantID, name string) error {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	// Delete policy file if it exists
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	policyPath := bucketPath + "/.maxiofs-policy"
	exists, err = bm.storage.Exists(ctx, policyPath)
	if err != nil {
		return err
	}
	if exists {
		return bm.storage.Delete(ctx, policyPath)
	}

	return nil
}

// GetVersioning retrieves the bucket versioning configuration
func (bm *bucketManager) GetVersioning(ctx context.Context, tenantID, name string) (*VersioningConfig, error) {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrBucketNotFound
	}

	// Try to read versioning file
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	versioningPath := bucketPath + "/.maxiofs-versioning"
	exists, err = bm.storage.Exists(ctx, versioningPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		// Return default (disabled)
		return &VersioningConfig{Status: "Suspended"}, nil
	}

	// Read and unmarshal versioning config
	reader, _, err := bm.storage.Get(ctx, versioningPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var config VersioningConfig
	if err := json.NewDecoder(reader).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode versioning config: %w", err)
	}

	return &config, nil
}

func (bm *bucketManager) SetVersioning(ctx context.Context, tenantID, name string, config *VersioningConfig) error {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	if config == nil {
		// If config is nil, set to suspended
		config = &VersioningConfig{Status: "Suspended"}
	}

	// Marshal config to JSON
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal versioning config: %w", err)
	}

	// Write versioning file
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	versioningPath := bucketPath + "/.maxiofs-versioning"
	return bm.storage.Put(ctx, versioningPath, strings.NewReader(string(configJSON)), nil)
}

// GetLifecycle retrieves the bucket lifecycle configuration
func (bm *bucketManager) GetLifecycle(ctx context.Context, tenantID, name string) (*LifecycleConfig, error) {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrBucketNotFound
	}

	// Try to read lifecycle file
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	lifecyclePath := bucketPath + "/.maxiofs-lifecycle"
	exists, err = bm.storage.Exists(ctx, lifecyclePath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrLifecycleNotFound
	}

	// Read and unmarshal lifecycle config
	reader, _, err := bm.storage.Get(ctx, lifecyclePath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var config LifecycleConfig
	if err := json.NewDecoder(reader).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode lifecycle config: %w", err)
	}

	return &config, nil
}

func (bm *bucketManager) SetLifecycle(ctx context.Context, tenantID, name string, config *LifecycleConfig) error {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	if config == nil {
		// If config is nil, delete it
		return bm.DeleteLifecycle(ctx, tenantID, name)
	}

	// Marshal config to JSON
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal lifecycle config: %w", err)
	}

	// Write lifecycle file
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	lifecyclePath := bucketPath + "/.maxiofs-lifecycle"
	return bm.storage.Put(ctx, lifecyclePath, strings.NewReader(string(configJSON)), nil)
}

func (bm *bucketManager) DeleteLifecycle(ctx context.Context, tenantID, name string) error {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	// Delete lifecycle file if it exists
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	lifecyclePath := bucketPath + "/.maxiofs-lifecycle"
	exists, err = bm.storage.Exists(ctx, lifecyclePath)
	if err != nil {
		return err
	}
	if exists {
		return bm.storage.Delete(ctx, lifecyclePath)
	}

	return nil
}

// GetCORS retrieves the bucket CORS configuration
func (bm *bucketManager) GetCORS(ctx context.Context, tenantID, name string) (*CORSConfig, error) {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrBucketNotFound
	}

	// Try to read CORS file
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	corsPath := bucketPath + "/.maxiofs-cors"
	exists, err = bm.storage.Exists(ctx, corsPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCORSNotFound
	}

	// Read and unmarshal CORS config
	reader, _, err := bm.storage.Get(ctx, corsPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var config CORSConfig
	if err := json.NewDecoder(reader).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode CORS config: %w", err)
	}

	return &config, nil
}

func (bm *bucketManager) SetCORS(ctx context.Context, tenantID, name string, config *CORSConfig) error {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	if config == nil {
		// If config is nil, delete it
		return bm.DeleteCORS(ctx, tenantID, name)
	}

	// Marshal config to JSON
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal CORS config: %w", err)
	}

	// Write CORS file
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	corsPath := bucketPath + "/.maxiofs-cors"
	return bm.storage.Put(ctx, corsPath, strings.NewReader(string(configJSON)), nil)
}

func (bm *bucketManager) DeleteCORS(ctx context.Context, tenantID, name string) error {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	// Delete CORS file if it exists
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	corsPath := bucketPath + "/.maxiofs-cors"
	exists, err = bm.storage.Exists(ctx, corsPath)
	if err != nil {
		return err
	}
	if exists {
		return bm.storage.Delete(ctx, corsPath)
	}

	return nil
}

// GetObjectLockConfig retrieves the bucket object lock configuration
func (bm *bucketManager) GetObjectLockConfig(ctx context.Context, tenantID, name string) (*ObjectLockConfig, error) {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrBucketNotFound
	}

	// Try to read object lock file
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	lockPath := bucketPath + "/.maxiofs-objectlock"
	exists, err = bm.storage.Exists(ctx, lockPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		// Return default (disabled)
		return &ObjectLockConfig{ObjectLockEnabled: false}, nil
	}

	// Read and unmarshal object lock config
	reader, _, err := bm.storage.Get(ctx, lockPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var config ObjectLockConfig
	if err := json.NewDecoder(reader).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode object lock config: %w", err)
	}

	return &config, nil
}

func (bm *bucketManager) SetObjectLockConfig(ctx context.Context, tenantID, name string, config *ObjectLockConfig) error {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	if config == nil {
		// If config is nil, set to disabled
		config = &ObjectLockConfig{ObjectLockEnabled: false}
	}

	// Marshal config to JSON
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal object lock config: %w", err)
	}

	// Write object lock file
	bucketPath := bm.getTenantBucketPath(tenantID, name)
	lockPath := bucketPath + "/.maxiofs-objectlock"
	return bm.storage.Put(ctx, lockPath, strings.NewReader(string(configJSON)), nil)
}

// IsReady checks if the bucket manager is ready
func (bm *bucketManager) IsReady() bool {
	// TODO: Implement readiness check
	return true
}

// Helper methods

// bucketExists checks if a bucket exists by looking for its marker file
func (bm *bucketManager) bucketExists(ctx context.Context, tenantID, name string) (bool, error) {
	bucketPath := bm.getTenantBucketPath(tenantID, name) + "/.maxiofs-bucket"
	return bm.storage.Exists(ctx, bucketPath)
}

// isBucketEmpty checks if a bucket contains no objects
func (bm *bucketManager) isBucketEmpty(ctx context.Context, tenantID, name string) (bool, error) {
	prefix := bm.getTenantBucketPath(tenantID, name) + "/"
	objects, err := bm.storage.List(ctx, prefix, false)
	if err != nil {
		return false, err
	}

	// Filter out the bucket marker file and configuration files
	for _, obj := range objects {
		if !strings.HasSuffix(obj.Path, ".maxiofs-bucket") &&
			!strings.Contains(obj.Path, "/.maxiofs-") {
			return false, nil
		}
	}

	return true, nil
}

// getBucketMetadataPath returns the path for bucket metadata
func (bm *bucketManager) getBucketMetadataPath(tenantID, name string) string {
	if tenantID == "" {
		// Global buckets (for backward compatibility or global admin)
		return fmt.Sprintf(".maxiofs/buckets/global/%s.json", name)
	}
	return fmt.Sprintf(".maxiofs/buckets/%s/%s.json", tenantID, name)
}

// getTenantBucketPath returns the storage path for a tenant's bucket
func (bm *bucketManager) getTenantBucketPath(tenantID, bucketName string) string {
	if tenantID == "" {
		// Global buckets
		return bucketName
	}
	return fmt.Sprintf("%s/%s", tenantID, bucketName)
}

// saveBucketMetadata saves bucket metadata to storage
func (bm *bucketManager) saveBucketMetadata(ctx context.Context, bucket *Bucket) error {
	data, err := json.Marshal(bucket)
	if err != nil {
		return fmt.Errorf("failed to marshal bucket metadata: %w", err)
	}

	metadataPath := bm.getBucketMetadataPath(bucket.TenantID, bucket.Name)
	return bm.storage.Put(ctx, metadataPath, strings.NewReader(string(data)), map[string]string{
		"content-type": "application/json",
	})
}

// loadBucketMetadata loads bucket metadata from storage
func (bm *bucketManager) loadBucketMetadata(ctx context.Context, tenantID, name string) (*Bucket, error) {
	metadataPath := bm.getBucketMetadataPath(tenantID, name)

	reader, _, err := bm.storage.Get(ctx, metadataPath)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			return nil, ErrBucketNotFound
		}
		return nil, fmt.Errorf("failed to load bucket metadata: %w", err)
	}
	defer reader.Close()

	// First decode to map to handle legacy format
	var rawData map[string]interface{}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read bucket metadata: %w", err)
	}

	if err := json.Unmarshal(data, &rawData); err != nil {
		return nil, fmt.Errorf("failed to decode bucket metadata: %w", err)
	}

	// Migrate legacy object_lock format (PascalCase) to new format (camelCase)
	if objectLock, ok := rawData["object_lock"].(map[string]interface{}); ok {
		// Check if using old format with "ObjectLockEnabled" string
		if enabled, ok := objectLock["ObjectLockEnabled"].(string); ok {
			objectLock["objectLockEnabled"] = (enabled == "Enabled")
			delete(objectLock, "ObjectLockEnabled")
		}
		// Migrate Rule -> rule
		if rule, ok := objectLock["Rule"].(map[string]interface{}); ok {
			objectLock["rule"] = rule
			delete(objectLock, "Rule")

			// Migrate DefaultRetention -> defaultRetention
			if defaultRetention, ok := rule["DefaultRetention"].(map[string]interface{}); ok {
				rule["defaultRetention"] = defaultRetention
				delete(rule, "DefaultRetention")

				// Migrate Mode, Days, Years to lowercase
				if mode, ok := defaultRetention["Mode"].(string); ok {
					defaultRetention["mode"] = mode
					delete(defaultRetention, "Mode")
				}
				if days, ok := defaultRetention["Days"]; ok {
					defaultRetention["days"] = days
					delete(defaultRetention, "Days")
				}
				if years, ok := defaultRetention["Years"]; ok {
					defaultRetention["years"] = years
					delete(defaultRetention, "Years")
				}
			}
		}
	}

	// Now decode to struct with migrated data
	migratedData, err := json.Marshal(rawData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal migrated data: %w", err)
	}

	var bucket Bucket
	if err := json.Unmarshal(migratedData, &bucket); err != nil {
		return nil, fmt.Errorf("failed to decode bucket metadata: %w", err)
	}

	return &bucket, nil
}

// deleteBucketMetadata deletes bucket metadata from storage
func (bm *bucketManager) deleteBucketMetadata(ctx context.Context, tenantID, name string) error {
	metadataPath := bm.getBucketMetadataPath(tenantID, name)
	err := bm.storage.Delete(ctx, metadataPath)
	if err != nil && err != storage.ErrObjectNotFound {
		return fmt.Errorf("failed to delete bucket metadata: %w", err)
	}
	return nil
}

// IncrementObjectCount increments the cached object count for a bucket
func (bm *bucketManager) IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error {
	bucket, err := bm.GetBucketInfo(ctx, tenantID, name)
	if err != nil {
		return err
	}

	bucket.ObjectCount++
	bucket.TotalSize += sizeBytes

	return bm.saveBucketMetadata(ctx, bucket)
}

// DecrementObjectCount decrements the cached object count for a bucket
func (bm *bucketManager) DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error {
	bucket, err := bm.GetBucketInfo(ctx, tenantID, name)
	if err != nil {
		return err
	}

	if bucket.ObjectCount > 0 {
		bucket.ObjectCount--
	}
	if bucket.TotalSize >= sizeBytes {
		bucket.TotalSize -= sizeBytes
	} else {
		bucket.TotalSize = 0
	}

	return bm.saveBucketMetadata(ctx, bucket)
}

// RecalculateMetrics recalculates the object count and total size for a bucket
// This is useful for fixing drift or initializing metrics for existing buckets
func (bm *bucketManager) RecalculateMetrics(ctx context.Context, tenantID, name string) error {
	// This would require the object manager, which we don't have access to here
	// This method should be called from a higher level (server) that has both managers
	return fmt.Errorf("RecalculateMetrics must be called from server level")
}
