package bucket

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// badgerBucketManager implements Manager using BadgerDB for metadata
type badgerBucketManager struct {
	storage       storage.Backend
	metadataStore metadata.Store
	aclManager    acl.Manager
}

// NewBadgerManager creates a new bucket manager using BadgerDB for metadata
func NewBadgerManager(storage storage.Backend, metadataStore metadata.Store) Manager {
	// Extract BadgerDB instance for ACL manager
	var aclMgr acl.Manager
	if badgerStore, ok := metadataStore.(interface{ DB() *badger.DB }); ok {
		db := badgerStore.DB()
		if db != nil {
			aclMgr = acl.NewManager(db)
		}
	}

	return &badgerBucketManager{
		storage:       storage,
		metadataStore: metadataStore,
		aclManager:    aclMgr,
	}
}

// CreateBucket creates a new bucket
func (bm *badgerBucketManager) CreateBucket(ctx context.Context, tenantID, name string) error {
	// Validate bucket name
	if err := ValidateBucketName(name); err != nil {
		return err
	}

	// Create bucket metadata
	bucket := &Bucket{
		Name:      name,
		TenantID:  tenantID,
		CreatedAt: time.Now(),
		Region:    "us-east-1", // Default region
		Metadata:  make(map[string]string),
	}

	// Store in BadgerDB
	metaBucket := toMetadataBucket(bucket)
	if err := bm.metadataStore.CreateBucket(ctx, metaBucket); err != nil {
		if err == metadata.ErrBucketAlreadyExists {
			return ErrBucketAlreadyExists
		}
		return err
	}

	// Solo crear ACL por defecto si no existe uno explícito
	if bm.aclManager != nil {
		aclActual, err := bm.aclManager.GetBucketACL(ctx, tenantID, name)
		defaultACL := acl.CreateDefaultACL("maxiofs", "MaxIOFS")
		if err != nil {
			// Si hay error inesperado, loguear pero no fallar bucket creation
			fmt.Printf("Warning: Error al consultar ACL para bucket %s: %v\n", name, err)
		} else {
			// Compara owner y cannedACL para saber si es el default
			esDefault := reflect.DeepEqual(aclActual.Owner, defaultACL.Owner) && aclActual.CannedACL == defaultACL.CannedACL && len(aclActual.Grants) == len(defaultACL.Grants)
			if esDefault {
				if err := bm.aclManager.SetBucketACL(ctx, tenantID, name, defaultACL); err != nil {
					fmt.Printf("Warning: Failed to set default ACL for bucket %s: %v\n", name, err)
				}
			}
		}
	} else {
		fmt.Printf("Warning: ACL manager not initialized for bucket %s\n", name)
	}

	// Create bucket directory in storage
	bucketPath := bm.getTenantBucketPath(tenantID, name) + "/"
	return bm.storage.Put(ctx, bucketPath+".maxiofs-bucket",
		strings.NewReader(""), map[string]string{
			"bucket-created": bucket.CreatedAt.Format(time.RFC3339),
			"tenant-id":      tenantID,
		})
}

// UpdateBucket updates an existing bucket's metadata
func (bm *badgerBucketManager) UpdateBucket(ctx context.Context, tenantID, name string, bucket *Bucket) error {
	// Validate
	if bucket.Name != name {
		return fmt.Errorf("bucket name mismatch")
	}
	if bucket.TenantID != tenantID {
		return fmt.Errorf("tenant ID mismatch")
	}

	// Update in BadgerDB
	metaBucket := toMetadataBucket(bucket)
	if err := bm.metadataStore.UpdateBucket(ctx, metaBucket); err != nil {
		if err == metadata.ErrBucketNotFound {
			return ErrBucketNotFound
		}
		return err
	}

	return nil
}

// DeleteBucket deletes a bucket
func (bm *badgerBucketManager) DeleteBucket(ctx context.Context, tenantID, name string) error {
	// Check if bucket is empty
	isEmpty, err := bm.isBucketEmpty(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !isEmpty {
		return ErrBucketNotEmpty
	}

	// Delete from BadgerDB
	if err := bm.metadataStore.DeleteBucket(ctx, tenantID, name); err != nil {
		if err == metadata.ErrBucketNotFound {
			return ErrBucketNotFound
		}
		return err
	}

	// Delete bucket marker from storage
	bucketPath := bm.getTenantBucketPath(tenantID, name) + "/"
	if err := bm.storage.Delete(ctx, bucketPath+".maxiofs-bucket"); err != nil {
		if err != storage.ErrObjectNotFound {
			return err
		}
	}

	// Remove physical directory if using filesystem backend
	if fsBackend, ok := bm.storage.(interface{ RemoveDirectory(string) error }); ok {
		tenantBucketPath := bm.getTenantBucketPath(tenantID, name)
		_ = fsBackend.RemoveDirectory(tenantBucketPath) // Ignore errors
	}

	return nil
}

// ListBuckets lists all buckets for a tenant
func (bm *badgerBucketManager) ListBuckets(ctx context.Context, tenantID string) ([]Bucket, error) {
	// Get from BadgerDB
	metaBuckets, err := bm.metadataStore.ListBuckets(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Convert to bucket.Bucket
	buckets := make([]Bucket, len(metaBuckets))
	for i, mb := range metaBuckets {
		buckets[i] = *fromMetadataBucket(mb)
	}

	return buckets, nil
}

// BucketExists checks if a bucket exists
func (bm *badgerBucketManager) BucketExists(ctx context.Context, tenantID, name string) (bool, error) {
	return bm.metadataStore.BucketExists(ctx, tenantID, name)
}

// GetBucketInfo retrieves bucket information
func (bm *badgerBucketManager) GetBucketInfo(ctx context.Context, tenantID, name string) (*Bucket, error) {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return nil, ErrBucketNotFound
		}
		return nil, err
	}

	return fromMetadataBucket(metaBucket), nil
}

// GetBucketPolicy retrieves the bucket policy
func (bm *badgerBucketManager) GetBucketPolicy(ctx context.Context, tenantID, name string) (*Policy, error) {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return nil, ErrBucketNotFound
		}
		return nil, err
	}

	if metaBucket.Policy == nil {
		return nil, ErrPolicyNotFound
	}

	return fromMetadataPolicy(metaBucket.Policy), nil
}

// SetBucketPolicy sets the bucket policy
func (bm *badgerBucketManager) SetBucketPolicy(ctx context.Context, tenantID, name string, policy *Policy) error {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return ErrBucketNotFound
		}
		return err
	}

	// Update policy
	metaBucket.Policy = toMetadataPolicy(policy)

	return bm.metadataStore.UpdateBucket(ctx, metaBucket)
}

// DeleteBucketPolicy deletes the bucket policy
func (bm *badgerBucketManager) DeleteBucketPolicy(ctx context.Context, tenantID, name string) error {
	return bm.SetBucketPolicy(ctx, tenantID, name, nil)
}

// GetVersioning retrieves the bucket versioning configuration
func (bm *badgerBucketManager) GetVersioning(ctx context.Context, tenantID, name string) (*VersioningConfig, error) {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return nil, ErrBucketNotFound
		}
		return nil, err
	}

	if metaBucket.Versioning == nil {
		return &VersioningConfig{Status: "Suspended"}, nil
	}

	return fromMetadataVersioning(metaBucket.Versioning), nil
}

// SetVersioning sets the bucket versioning configuration
func (bm *badgerBucketManager) SetVersioning(ctx context.Context, tenantID, name string, config *VersioningConfig) error {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return ErrBucketNotFound
		}
		return err
	}

	metaBucket.Versioning = toMetadataVersioning(config)

	return bm.metadataStore.UpdateBucket(ctx, metaBucket)
}

// GetLifecycle retrieves the bucket lifecycle configuration
func (bm *badgerBucketManager) GetLifecycle(ctx context.Context, tenantID, name string) (*LifecycleConfig, error) {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return nil, ErrBucketNotFound
		}
		return nil, err
	}

	if metaBucket.Lifecycle == nil {
		return nil, ErrLifecycleNotFound
	}

	return fromMetadataLifecycle(metaBucket.Lifecycle), nil
}

// SetLifecycle sets the bucket lifecycle configuration
func (bm *badgerBucketManager) SetLifecycle(ctx context.Context, tenantID, name string, config *LifecycleConfig) error {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return ErrBucketNotFound
		}
		return err
	}

	metaBucket.Lifecycle = toMetadataLifecycle(config)

	return bm.metadataStore.UpdateBucket(ctx, metaBucket)
}

// DeleteLifecycle deletes the bucket lifecycle configuration
func (bm *badgerBucketManager) DeleteLifecycle(ctx context.Context, tenantID, name string) error {
	return bm.SetLifecycle(ctx, tenantID, name, nil)
}

// GetCORS retrieves the bucket CORS configuration
func (bm *badgerBucketManager) GetCORS(ctx context.Context, tenantID, name string) (*CORSConfig, error) {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return nil, ErrBucketNotFound
		}
		return nil, err
	}

	if metaBucket.CORS == nil {
		return nil, ErrCORSNotFound
	}

	return fromMetadataCORS(metaBucket.CORS), nil
}

// SetCORS sets the bucket CORS configuration
func (bm *badgerBucketManager) SetCORS(ctx context.Context, tenantID, name string, config *CORSConfig) error {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return ErrBucketNotFound
		}
		return err
	}

	metaBucket.CORS = toMetadataCORS(config)

	return bm.metadataStore.UpdateBucket(ctx, metaBucket)
}

// DeleteCORS deletes the bucket CORS configuration
func (bm *badgerBucketManager) DeleteCORS(ctx context.Context, tenantID, name string) error {
	return bm.SetCORS(ctx, tenantID, name, nil)
}

// SetBucketTags sets the bucket tags
func (bm *badgerBucketManager) SetBucketTags(ctx context.Context, tenantID, name string, tags map[string]string) error {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return ErrBucketNotFound
		}
		return err
	}

	metaBucket.Tags = tags

	return bm.metadataStore.UpdateBucket(ctx, metaBucket)
}

// GetObjectLockConfig retrieves the bucket object lock configuration
func (bm *badgerBucketManager) GetObjectLockConfig(ctx context.Context, tenantID, name string) (*ObjectLockConfig, error) {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return nil, ErrBucketNotFound
		}
		return nil, err
	}

	if metaBucket.ObjectLock == nil {
		return &ObjectLockConfig{ObjectLockEnabled: false}, nil
	}

	return fromMetadataObjectLock(metaBucket.ObjectLock), nil
}

// SetObjectLockConfig sets the bucket object lock configuration
func (bm *badgerBucketManager) SetObjectLockConfig(ctx context.Context, tenantID, name string, config *ObjectLockConfig) error {
	metaBucket, err := bm.metadataStore.GetBucket(ctx, tenantID, name)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return ErrBucketNotFound
		}
		return err
	}

	metaBucket.ObjectLock = toMetadataObjectLock(config)

	return bm.metadataStore.UpdateBucket(ctx, metaBucket)
}

// IncrementObjectCount increments the cached object count for a bucket
func (bm *badgerBucketManager) IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error {
	return bm.metadataStore.UpdateBucketMetrics(ctx, tenantID, name, 1, sizeBytes)
}

// DecrementObjectCount decrements the cached object count for a bucket
func (bm *badgerBucketManager) DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error {
	return bm.metadataStore.UpdateBucketMetrics(ctx, tenantID, name, -1, -sizeBytes)
}

// RecalculateMetrics recalculates the object count and total size for a bucket
func (bm *badgerBucketManager) RecalculateMetrics(ctx context.Context, tenantID, name string) error {
	return bm.metadataStore.RecalculateBucketStats(ctx, tenantID, name)
}

// IsReady checks if the bucket manager is ready
func (bm *badgerBucketManager) IsReady() bool {
	return bm.metadataStore.IsReady()
}

// Helper methods

// isBucketEmpty checks if a bucket contains no objects
func (bm *badgerBucketManager) isBucketEmpty(ctx context.Context, tenantID, name string) (bool, error) {
	prefix := bm.getTenantBucketPath(tenantID, name) + "/"
	objects, err := bm.storage.List(ctx, prefix, false)
	if err != nil {
		return false, err
	}

	bucketPath := bm.getTenantBucketPath(tenantID, name)
	hasValidObjects := false

	// Check each physical file
	for _, obj := range objects {
		// Skip bucket marker and internal files
		if strings.HasSuffix(obj.Path, ".maxiofs-bucket") || strings.Contains(obj.Path, "/.maxiofs-") {
			continue
		}

		// Extract object key from path
		objectKey := strings.TrimPrefix(obj.Path, prefix)
		if objectKey == "" {
			continue
		}

		// Check if metadata exists in BadgerDB
		_, err := bm.metadataStore.GetObject(ctx, bucketPath, objectKey)
		if err != nil {
			if err == metadata.ErrObjectNotFound {
				// Orphaned physical file - delete it
				logrus.WithFields(logrus.Fields{
					"bucket": bucketPath,
					"key":    objectKey,
					"path":   obj.Path,
				}).Warn("Found orphaned physical file without metadata - deleting")

				if delErr := bm.storage.Delete(ctx, obj.Path); delErr != nil {
					logrus.WithError(delErr).Error("Failed to delete orphaned file")
				}
				continue
			}
			// Other error - assume file is valid to be safe
			return false, err
		}

		// Valid object with metadata
		hasValidObjects = true
	}

	return !hasValidObjects, nil
}

// getTenantBucketPath returns the storage path for a tenant's bucket
func (bm *badgerBucketManager) getTenantBucketPath(tenantID, bucketName string) string {
	if tenantID == "" {
		return bucketName
	}
	return fmt.Sprintf("%s/%s", tenantID, bucketName)
}

// GetBucketACL retrieves the bucket ACL
func (bm *badgerBucketManager) GetBucketACL(ctx context.Context, tenantID, name string) (interface{}, error) {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrBucketNotFound
	}

	// Check if ACL manager is available
	if bm.aclManager == nil {
		return nil, fmt.Errorf("ACL manager not initialized")
	}

	// Get ACL from ACL manager
	return bm.aclManager.GetBucketACL(ctx, tenantID, name)
}

// SetBucketACL sets the bucket ACL
func (bm *badgerBucketManager) SetBucketACL(ctx context.Context, tenantID, name string, aclInterface interface{}) error {
	// Check if bucket exists
	exists, err := bm.BucketExists(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}

	// Check if ACL manager is available
	if bm.aclManager == nil {
		return fmt.Errorf("ACL manager not initialized")
	}

	// Type assertion to convert interface{} to *acl.ACL
	aclData, ok := aclInterface.(*acl.ACL)
	if !ok {
		return fmt.Errorf("invalid ACL type")
	}

	// Set ACL using ACL manager
	return bm.aclManager.SetBucketACL(ctx, tenantID, name, aclData)
}

// GetACLManager returns the ACL manager
func (bm *badgerBucketManager) GetACLManager() interface{} {
	return bm.aclManager
}
