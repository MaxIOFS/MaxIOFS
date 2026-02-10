package object

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/maxiofs/maxiofs/pkg/encryption"
	"github.com/sirupsen/logrus"
)

// Manager defines the interface for object management
type Manager interface {
	// Basic object operations
	GetObject(ctx context.Context, bucket, key string, versionID ...string) (*Object, io.ReadCloser, error)
	PutObject(ctx context.Context, bucket, key string, data io.Reader, headers http.Header) (*Object, error)
	DeleteObject(ctx context.Context, bucket, key string, bypassGovernance bool, versionID ...string) (deleteMarkerVersionID string, err error)
	ListObjects(ctx context.Context, bucket, prefix, delimiter, marker string, maxKeys int) (*ListObjectsResult, error)
	SearchObjects(ctx context.Context, bucket, prefix, delimiter, marker string, maxKeys int, filter *metadata.ObjectFilter) (*ListObjectsResult, error)

	// Metadata operations
	GetObjectMetadata(ctx context.Context, bucket, key string) (*Object, error)
	UpdateObjectMetadata(ctx context.Context, bucket, key string, metadata map[string]string) error

	// Object Lock operations
	GetObjectRetention(ctx context.Context, bucket, key string) (*RetentionConfig, error)
	SetObjectRetention(ctx context.Context, bucket, key string, config *RetentionConfig) error
	GetObjectLegalHold(ctx context.Context, bucket, key string) (*LegalHoldConfig, error)
	SetObjectLegalHold(ctx context.Context, bucket, key string, config *LegalHoldConfig) error

	// Versioning operations
	GetObjectVersions(ctx context.Context, bucket, key string) ([]ObjectVersion, error)
	DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error

	// Tagging operations
	GetObjectTagging(ctx context.Context, bucket, key string) (*TagSet, error)
	SetObjectTagging(ctx context.Context, bucket, key string, tags *TagSet) error
	DeleteObjectTagging(ctx context.Context, bucket, key string) error

	// ACL operations
	GetObjectACL(ctx context.Context, bucket, key string) (*ACL, error)
	SetObjectACL(ctx context.Context, bucket, key string, acl *ACL) error

	// Multipart upload operations
	CreateMultipartUpload(ctx context.Context, bucket, key string, headers http.Header) (*MultipartUpload, error)
	UploadPart(ctx context.Context, uploadID string, partNumber int, data io.Reader) (*Part, error)
	ListParts(ctx context.Context, uploadID string) ([]Part, error)
	CompleteMultipartUpload(ctx context.Context, uploadID string, parts []Part) (*Object, error)
	AbortMultipartUpload(ctx context.Context, uploadID string) error
	ListMultipartUploads(ctx context.Context, bucket string) ([]MultipartUpload, error)

	// Health check
	IsReady() bool
}

// Object represents a stored object
type Object struct {
	Key          string            `json:"key"`
	Bucket       string            `json:"bucket"`
	Size         int64             `json:"size"`
	LastModified time.Time         `json:"last_modified"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"content_type"`
	Metadata     map[string]string `json:"metadata"`
	StorageClass string            `json:"storage_class"`
	VersionID    string            `json:"version_id,omitempty"`

	// Object Lock
	Retention *RetentionConfig `json:"retention,omitempty"`
	LegalHold *LegalHoldConfig `json:"legal_hold,omitempty"`

	// Tagging
	Tags *TagSet `json:"tags,omitempty"`

	// ACL
	ACL *ACL `json:"acl,omitempty"`
}

// objectManager implements the Manager interface
type objectManager struct {
	storage           storage.Backend
	config            config.StorageConfig
	metadataStore     metadata.Store
	aclManager        acl.Manager
	encryptionService *encryption.EncryptionService
	bucketManager     interface {
		IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
		DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
	}
	authManager interface {
		IncrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error
		DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error
		CheckTenantStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) error
	}
}

// NewManager creates a new object manager
func NewManager(storage storage.Backend, metadataStore metadata.Store, config config.StorageConfig) Manager {
	// Extract BadgerDB instance for ACL manager
	var aclMgr acl.Manager
	if badgerStore, ok := metadataStore.(interface{ DB() *badger.DB }); ok {
		db := badgerStore.DB()
		if db != nil {
			aclMgr = acl.NewManager(db)
		}
	}

	// Initialize encryption service with AES-256-GCM
	encryptionConfig := encryption.DefaultEncryptionConfig()
	encryptionService := encryption.NewEncryptionService(encryptionConfig)

	// Load master key from config if provided
	// This key is needed for:
	// 1. Encrypting new objects (if enable_encryption = true)
	// 2. Decrypting existing encrypted objects (always, regardless of enable_encryption)
	if config.EncryptionKey != "" {
		// Validate key length (must be 32 bytes = 64 hex characters)
		if len(config.EncryptionKey) != 64 {
			logrus.Fatalf("Invalid encryption_key length: got %d characters, expected 64 (32 bytes in hex). "+
				"Generate a secure key with: openssl rand -hex 32", len(config.EncryptionKey))
		}

		// Convert hex string to bytes (32 bytes for AES-256)
		keyBytes := make([]byte, 32)
		_, err := fmt.Sscanf(config.EncryptionKey, "%64x", &keyBytes)
		if err != nil {
			logrus.WithError(err).Fatal("Invalid encryption_key format: must be 64 hex characters. " +
				"Generate a secure key with: openssl rand -hex 32")
		}

		// Store the master key as the default encryption key
		if err := encryptionService.GetKeyManager().StoreKey("default", keyBytes); err != nil {
			logrus.WithError(err).Fatal("Failed to store master encryption key")
		}

		if config.EnableEncryption {
			logrus.Info("✅ Encryption enabled: New objects will be encrypted with AES-256-CTR")
		} else {
			logrus.Info("⚠️  Encryption disabled for new objects (existing encrypted objects remain accessible)")
		}
	} else {
		// No encryption key configured
		if config.EnableEncryption {
			logrus.Fatal("Encryption is enabled but encryption_key is not set in config. " +
				"Generate a secure key with: openssl rand -hex 32")
		}
		logrus.Info("⚠️  No encryption key configured: All objects will be stored unencrypted")
	}

	return &objectManager{
		storage:           storage,
		config:            config,
		metadataStore:     metadataStore,
		aclManager:        aclMgr,
		encryptionService: encryptionService,
		bucketManager:     nil, // Will be set later via SetBucketManager
	}
}

// SetBucketManager sets the bucket manager for metrics updates
func (om *objectManager) SetBucketManager(bm interface {
	IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
	DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
}) {
	om.bucketManager = bm
}

// SetAuthManager sets the auth manager for tenant quota updates
func (om *objectManager) SetAuthManager(am interface {
	IncrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error
	DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error
	CheckTenantStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) error
}) {
	om.authManager = am
}

// parseBucketPath extracts tenantID and bucketName from a bucket path
// Formats: "tenantID/bucketName" or "bucketName" (for global buckets)
func (om *objectManager) parseBucketPath(bucketPath string) (tenantID, bucketName string) {
	parts := strings.SplitN(bucketPath, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1] // "tenant-123", "backups"
	}
	return "", parts[0] // "", "backups" (global bucket)
}

// generateVersionID generates a unique version ID for object versioning
// Format: timestamp (nanoseconds) + random hex (8 chars)
func generateVersionID() string {
	timestamp := time.Now().UnixNano()
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to timestamp-only version ID if crypto/rand fails
		return fmt.Sprintf("%d", timestamp)
	}
	randomHex := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("%d.%s", timestamp, randomHex)
}

// GetObject retrieves an object (optionally a specific version)
func (om *objectManager) GetObject(ctx context.Context, bucket, key string, versionID ...string) (*Object, io.ReadCloser, error) {
	if err := om.validateObjectName(key); err != nil {
		return nil, nil, err
	}

	// Load object metadata from BadgerDB first to determine if versioning is enabled
	var metaObj *metadata.ObjectMetadata
	var requestedVersionID string
	var err error

	if len(versionID) > 0 && versionID[0] != "" {
		requestedVersionID = versionID[0]
		// Get specific version metadata
		metaObj, err = om.metadataStore.GetObject(ctx, bucket, key, requestedVersionID)
		if err != nil {
			if err == metadata.ErrObjectNotFound {
				return nil, nil, ErrObjectNotFound
			}
			return nil, nil, fmt.Errorf("failed to get object version metadata: %w", err)
		}
	} else {
		// Get latest version metadata
		metaObj, err = om.metadataStore.GetObject(ctx, bucket, key)
		if err != nil && err != metadata.ErrObjectNotFound {
			logrus.WithError(err).Debug("Failed to load object metadata from BadgerDB")
		}
		// If metadata exists and has VersionID, use it
		if metaObj != nil && metaObj.VersionID != "" {
			requestedVersionID = metaObj.VersionID
		}
	}

	// Determine the correct object path
	var objectPath string
	if requestedVersionID != "" {
		// Use versioned path
		objectPath = om.getVersionedObjectPath(bucket, key, requestedVersionID)
	} else {
		// Use regular path (for non-versioned objects)
		objectPath = om.getObjectPath(bucket, key)
	}

	// Get encrypted object data from storage
	encryptedReader, storageMetadata, err := om.storage.Get(ctx, objectPath)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			return nil, nil, ErrObjectNotFound
		}
		return nil, nil, fmt.Errorf("failed to get object: %w", err)
	}

	var object *Object
	if metaObj != nil {
		object = fromMetadataObject(metaObj)

		// Check if this is a delete marker (Size==0, ETag=="")
		// Delete markers should return 404
		if object.Size == 0 && object.ETag == "" && requestedVersionID == "" {
			// Latest version is a delete marker
			encryptedReader.Close()
			return nil, nil, ErrObjectNotFound
		}
	} else {
		// If metadata doesn't exist in BadgerDB, use storage metadata
		// Check if file is encrypted
		var size int64
		var etag string

		if storageMetadata["encrypted"] == "true" {
			// Use original metadata (before encryption)
			size, _ = strconv.ParseInt(storageMetadata["original-size"], 10, 64)
			etag = storageMetadata["original-etag"]
		} else {
			// Unencrypted file (legacy or multipart)
			size, _ = strconv.ParseInt(storageMetadata["size"], 10, 64)
			etag = storageMetadata["etag"]
		}

		lastModified, _ := strconv.ParseInt(storageMetadata["last_modified"], 10, 64)

		object = &Object{
			Key:          key,
			Bucket:       bucket,
			Size:         size,
			LastModified: time.Unix(lastModified, 0),
			ETag:         etag,
			ContentType:  storageMetadata["content-type"],
			Metadata:     storageMetadata,
			StorageClass: StorageClassStandard,
		}
	}

	// Check if object is encrypted
	isEncrypted := storageMetadata["encrypted"] == "true"

	if isEncrypted {
		// Object is encrypted - decrypt stream
		pipeReader, pipeWriter := io.Pipe()

		// Create encryption metadata for decryption
		encryptionMeta := &encryption.EncryptionMetadata{
			Algorithm: "AES-256-GCM",
		}

		// Decrypt in a goroutine — monitor context to prevent goroutine leak
		go func() {
			defer encryptedReader.Close()

			// If the caller abandons the reader (e.g., client disconnects),
			// the context will be cancelled, unblocking pipeWriter.Write()
			done := make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					pipeWriter.CloseWithError(ctx.Err())
				case <-done:
				}
			}()

			err := om.encryptionService.DecryptStream(encryptedReader, pipeWriter, encryptionMeta)
			close(done)

			if err != nil {
				// Ignore "closed pipe" errors - these occur during range requests when client
				// closes connection after receiving requested bytes, which is expected behavior
				if err.Error() != "io: read/write on closed pipe" && !strings.Contains(err.Error(), "closed pipe") {
					logrus.WithError(err).Error("Failed to decrypt object data")
					pipeWriter.CloseWithError(fmt.Errorf("decryption failed: %w", err))
					return
				}
			}
			pipeWriter.Close()
		}()

		return object, pipeReader, nil
	} else {
		// Object is NOT encrypted - return as-is
		return object, encryptedReader, nil
	}
}

// PutObject stores an object
func (om *objectManager) PutObject(ctx context.Context, bucket, key string, data io.Reader, headers http.Header) (*Object, error) {
	if err := om.validateObjectName(key); err != nil {
		return nil, err
	}

	// Extract metadata from headers using helper function
	storageMetadata, userMetadata := om.extractMetadataFromHeaders(headers)

	// Check if versioning is enabled for this bucket
	tenantID, bucketName := om.parseBucketPath(bucket)
	bucketMeta, err := om.metadataStore.GetBucket(ctx, tenantID, bucketName)
	versioningEnabled := false
	if err == nil && bucketMeta != nil && bucketMeta.Versioning != nil {
		versioningEnabled = bucketMeta.Versioning.Status == "Enabled"
	}

	// Generate versionID if versioning is enabled
	var versionID string
	var objectPath string
	if versioningEnabled {
		versionID = generateVersionID()
		objectPath = om.getVersionedObjectPath(bucket, key, versionID)
	} else {
		objectPath = om.getObjectPath(bucket, key)
	}

	// Step 1: Stream data to temporary file while calculating hash and size
	// This avoids loading entire file into memory
	tempFile, err := os.CreateTemp("", "maxiofs-upload-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)       // Clean up temp file when done
	defer tempFile.Close()          // Ensure handle is closed on panic

	// Write to temp file while calculating MD5 hash
	hasher := md5.New()
	multiWriter := io.MultiWriter(tempFile, hasher)
	originalSize, err := io.Copy(multiWriter, data)
	if err != nil {
		return nil, fmt.Errorf("failed to write to temp file: %w", err)
	}
	tempFile.Close()

	// Calculate original ETag (MD5 hash)
	originalETag := hex.EncodeToString(hasher.Sum(nil))

	logrus.WithFields(logrus.Fields{
		"bucket":       bucket,
		"key":          key,
		"originalSize": originalSize,
		"originalETag": originalETag,
	}).Debug("Calculated metadata from streaming upload")

	// Store object data (encrypted or unencrypted) using helper functions
	shouldEncrypt := om.shouldEncryptObject(ctx, tenantID, bucketName)
	if shouldEncrypt {
		if err := om.storeEncryptedObject(ctx, objectPath, tempPath, storageMetadata, originalSize, originalETag); err != nil {
			return nil, err
		}
	} else {
		if err := om.storeUnencryptedObject(ctx, objectPath, tempPath, storageMetadata, originalSize, originalETag); err != nil {
			return nil, err
		}
	}

	// Get final storage metadata (timestamps, etc)
	finalStorageMetadata, err := om.storage.GetMetadata(ctx, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get object metadata: %w", err)
	}

	// Use ORIGINAL size and ETag (not encrypted ones)
	size := originalSize
	lastModified, _ := strconv.ParseInt(finalStorageMetadata["last_modified"], 10, 64)

	// Validate tenant storage quota before committing
	// If quota is exceeded, delete the stored object and return error
	if om.authManager != nil && tenantID != "" && !versioningEnabled {
		// Check if overwriting existing object
		existingObj, _ := om.metadataStore.GetObject(ctx, bucket, key)
		var sizeIncrement int64
		if existingObj == nil {
			sizeIncrement = size
		} else {
			sizeIncrement = size - existingObj.Size
		}

		// Only validate if adding storage
		if sizeIncrement > 0 {
			if err := om.authManager.CheckTenantStorageQuota(ctx, tenantID, sizeIncrement); err != nil {
				// Quota exceeded - delete the stored object file
				if delErr := om.storage.Delete(ctx, objectPath); delErr != nil {
					logrus.WithError(delErr).WithField("path", objectPath).Error("Failed to delete object after quota exceeded — orphaned file may remain")
				}
				return nil, fmt.Errorf("storage quota exceeded: %w", err)
			}
		}
	}

	object := &Object{
		Key:          key,
		Bucket:       bucket,
		Size:         size, // Original size (unencrypted)
		LastModified: time.Unix(lastModified, 0),
		ETag:         originalETag, // Original ETag (MD5 of unencrypted data)
		ContentType:  finalStorageMetadata["content-type"],
		Metadata:     userMetadata, // User metadata from x-amz-meta-* headers
		StorageClass: StorageClassStandard,
		VersionID:    versionID, // Set versionID (empty string if versioning disabled)
	}

	// Apply default Object Lock retention if bucket has it configured
	if err := om.applyDefaultRetention(ctx, object); err != nil {
		logrus.WithError(err).Debug("Failed to apply default retention")
	}

	// CRITICAL: Get existing object BEFORE overwriting in metadata store
	// This is needed for correct size calculations in metrics and quotas
	var existingObjBeforeSave *metadata.ObjectMetadata
	if !versioningEnabled {
		existingObjBeforeSave, _ = om.metadataStore.GetObject(ctx, bucket, key)
	}

	// If versioning is enabled, store as version
	if versioningEnabled {

		// Create version entry
		version := &metadata.ObjectVersion{
			VersionID:    versionID,
			IsLatest:     true,
			Key:          key,
			Size:         size,
			ETag:         object.ETag,
			LastModified: object.LastModified,
			StorageClass: StorageClassStandard,
		}

		// Store version (this also updates the main object if IsLatest=true)
		metaObj := toMetadataObject(object)
		if err := om.metadataStore.PutObjectVersion(ctx, metaObj, version); err != nil {
			logrus.WithError(err).Warn("Failed to save object version to BadgerDB")
		}
	} else {
		// No versioning - use regular PutObject
		metaObj := toMetadataObject(object)
		if err := om.metadataStore.PutObject(ctx, metaObj); err != nil {
			logrus.WithError(err).Warn("Failed to save object metadata to BadgerDB")
		}
	}

	// Create implicit parent folders in BadgerDB
	// This ensures folders are listable even when created implicitly by S3 clients
	om.ensureImplicitFolders(ctx, bucket, key)

	// Update bucket metrics using helper function
	om.updateBucketMetricsAfterPut(ctx, tenantID, bucketName, bucket, key, size, versioningEnabled, existingObjBeforeSave)

	// Update tenant storage quota using helper function
	om.updateTenantQuotaAfterPut(ctx, tenantID, key, size, versioningEnabled, existingObjBeforeSave)

	return object, nil
}

// DeleteObject deletes an object or creates a delete marker
// Returns deleteMarkerVersionID if a delete marker was created, empty string otherwise
// bypassGovernance allows admins to delete objects under GOVERNANCE retention
func (om *objectManager) DeleteObject(ctx context.Context, bucket, key string, bypassGovernance bool, versionID ...string) (string, error) {
	if err := om.validateObjectName(key); err != nil {
		return "", err
	}

	// Check if versioning is enabled
	tenantID, bucketName := om.parseBucketPath(bucket)
	bucketMeta, err := om.metadataStore.GetBucket(ctx, tenantID, bucketName)
	versioningEnabled := false
	if err == nil && bucketMeta != nil && bucketMeta.Versioning != nil {
		versioningEnabled = bucketMeta.Versioning.Status == "Enabled"
	}

	// Determine if we're deleting a specific version or creating a delete marker
	var specificVersionID string
	if len(versionID) > 0 && versionID[0] != "" {
		specificVersionID = versionID[0]
	}

	if specificVersionID != "" {
		// DELETE with versionId → Permanent deletion of specific version
		return "", om.deleteSpecificVersion(ctx, bucket, key, specificVersionID)
	} else if versioningEnabled {
		// DELETE without versionId + versioning enabled → Create delete marker
		return om.createDeleteMarker(ctx, bucket, key)
	} else {
		// DELETE without versioning → Legacy behavior (permanent delete)
		return "", om.deletePermanently(ctx, bucket, key, bypassGovernance)
	}
}

// createDeleteMarker creates a delete marker for a versioned object
func (om *objectManager) createDeleteMarker(ctx context.Context, bucket, key string) (string, error) {
	// Generate delete marker versionID
	deleteMarkerVersionID := generateVersionID()

	// Create delete marker version entry
	deleteMarker := &metadata.ObjectVersion{
		VersionID:    deleteMarkerVersionID,
		IsLatest:     true,
		Key:          key,
		Size:         0,
		ETag:         "",
		LastModified: time.Now(),
		StorageClass: StorageClassStandard,
	}

	// Create minimal object metadata for delete marker
	deleteMarkerObj := &metadata.ObjectMetadata{
		Bucket:       bucket,
		Key:          key,
		VersionID:    deleteMarkerVersionID,
		Size:         0,
		LastModified: time.Now(),
		ETag:         "",
		ContentType:  "",
		StorageClass: StorageClassStandard,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Store delete marker version
	if err := om.metadataStore.PutObjectVersion(ctx, deleteMarkerObj, deleteMarker); err != nil {
		return "", fmt.Errorf("failed to create delete marker: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"bucket":    bucket,
		"key":       key,
		"versionID": deleteMarkerVersionID,
	}).Info("Created delete marker")

	return deleteMarkerVersionID, nil
}

// deleteSpecificVersion permanently deletes a specific version
func (om *objectManager) deleteSpecificVersion(ctx context.Context, bucket, key, versionID string) error {
	// Get all versions first to check if we're deleting the latest
	allVersions, err := om.metadataStore.GetObjectVersions(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("failed to get object versions: %w", err)
	}

	// Find the version we're deleting and check if it's latest
	var deletingLatest bool
	var versionToDelete *metadata.ObjectVersion
	for _, ver := range allVersions {
		if ver.VersionID == versionID {
			versionToDelete = ver
			deletingLatest = ver.IsLatest
			break
		}
	}

	if versionToDelete == nil {
		return ErrObjectNotFound
	}

	// Get full metadata for Object Lock checks
	metaObj, err := om.metadataStore.GetObject(ctx, bucket, key, versionID)
	if err != nil {
		if err == metadata.ErrObjectNotFound {
			return ErrObjectNotFound
		}
		return fmt.Errorf("failed to get version metadata: %w", err)
	}

	objMetadata := fromMetadataObject(metaObj)

	// Check Object Lock - Legal Hold
	if objMetadata.LegalHold != nil && objMetadata.LegalHold.Status == LegalHoldStatusOn {
		return ErrObjectUnderLegalHold
	}

	// Check Object Lock - Retention
	if objMetadata.Retention != nil {
		if time.Now().Before(objMetadata.Retention.RetainUntilDate) {
			if objMetadata.Retention.Mode == RetentionModeCompliance {
				return NewComplianceRetentionError(objMetadata.Retention.RetainUntilDate)
			}
			if objMetadata.Retention.Mode == RetentionModeGovernance {
				return NewGovernanceRetentionError(objMetadata.Retention.RetainUntilDate)
			}
		}
	}

	// Delete version metadata from BadgerDB
	if err := om.metadataStore.DeleteObjectVersion(ctx, bucket, key, versionID); err != nil {
		return fmt.Errorf("failed to delete version metadata: %w", err)
	}

	// Delete physical file (if not a delete marker)
	if objMetadata.Size > 0 {
		objectPath := om.getVersionedObjectPath(bucket, key, versionID)
		if err := om.storage.Delete(ctx, objectPath); err != nil && err != storage.ErrObjectNotFound {
			logrus.WithError(err).Warn("Failed to delete physical versioned file")
		}
	}

	// If we deleted the latest version, handle next version or delete object entry
	if deletingLatest {
		if len(allVersions) > 1 {
			// Find the next most recent version (excluding the one we just deleted)
			var nextLatest *metadata.ObjectVersion
			for _, ver := range allVersions {
				if ver.VersionID != versionID {
					if nextLatest == nil || ver.LastModified.After(nextLatest.LastModified) {
						nextLatest = ver
					}
				}
			}

			if nextLatest != nil {
				// Mark as latest
				nextLatest.IsLatest = true

				// Get full object metadata and update
				nextMetaObj, err := om.metadataStore.GetObject(ctx, bucket, key, nextLatest.VersionID)
				if err != nil {
					logrus.WithError(err).Warn("Failed to get object metadata for next latest")
				} else {
					// Ensure bucket and key are set correctly (they might be empty from version metadata)
					if nextMetaObj.Bucket == "" {
						nextMetaObj.Bucket = bucket
					}
					if nextMetaObj.Key == "" {
						nextMetaObj.Key = key
					}

					err = om.metadataStore.PutObjectVersion(ctx, nextMetaObj, nextLatest)
					if err != nil {
						logrus.WithError(err).Warn("Failed to mark next version as latest")
					}
				}
			}
		} else {
			// This was the last version - delete the main object entry
			if err := om.metadataStore.DeleteObject(ctx, bucket, key); err != nil {
				logrus.WithError(err).Warn("Failed to delete main object entry")
			}
			logrus.WithFields(logrus.Fields{
				"bucket": bucket,
				"key":    key,
			}).Info("Deleted main object entry - no versions remaining")
		}
	}

	logrus.WithFields(logrus.Fields{
		"bucket":    bucket,
		"key":       key,
		"versionID": versionID,
	}).Info("Permanently deleted version")

	return nil
}

// deletePermanently permanently deletes an object (legacy behavior without versioning)
func (om *objectManager) deletePermanently(ctx context.Context, bucket, key string, bypassGovernance bool) error {
	objectPath := om.getObjectPath(bucket, key)

	// Get metadata
	metaObj, err := om.metadataStore.GetObject(ctx, bucket, key)
	var objectSize int64

	if err != nil {
		if err == metadata.ErrObjectNotFound {
			// Metadata doesn't exist, but physical file might - clean it up
			logrus.WithFields(logrus.Fields{
				"bucket": bucket,
				"key":    key,
			}).Debug("Metadata not found, cleaning up orphaned physical file if exists")

			if err := om.storage.Delete(ctx, objectPath); err != nil && err != storage.ErrObjectNotFound {
				logrus.WithError(err).Warn("Failed to delete orphaned physical file")
			}

			// Clean up empty directories
			om.cleanupEmptyDirectories(bucket, key)

			// Return success - object is gone (idempotent delete per S3 spec)
			return nil
		}
		return fmt.Errorf("failed to get object metadata: %w", err)
	}

	objMetadata := fromMetadataObject(metaObj)
	objectSize = objMetadata.Size

	// Check Object Lock
	if objMetadata.LegalHold != nil && objMetadata.LegalHold.Status == LegalHoldStatusOn {
		return ErrObjectUnderLegalHold
	}

	if objMetadata.Retention != nil {
		if time.Now().Before(objMetadata.Retention.RetainUntilDate) {
			if objMetadata.Retention.Mode == RetentionModeCompliance {
				return NewComplianceRetentionError(objMetadata.Retention.RetainUntilDate)
			}
			if objMetadata.Retention.Mode == RetentionModeGovernance {
				// Allow bypass if flag is set (caller must validate admin permissions)
				if !bypassGovernance {
					return NewGovernanceRetentionError(objMetadata.Retention.RetainUntilDate)
				}
				logrus.WithFields(logrus.Fields{
					"bucket": bucket,
					"key":    key,
				}).Info("Bypassing GOVERNANCE retention for delete")
			}
		}
	}

	// Delete object: physical file AND metadata together
	// Step 1: Delete physical file
	physicalDeleted := false
	if err := om.storage.Delete(ctx, objectPath); err != nil {
		if err != storage.ErrObjectNotFound {
			return fmt.Errorf("failed to delete physical file: %w", err)
		}
	} else {
		physicalDeleted = true
	}

	// Step 2: Delete metadata from BadgerDB
	if err := om.metadataStore.DeleteObject(ctx, bucket, key); err != nil {
		if err != metadata.ErrObjectNotFound {
			// Metadata delete failed but physical file was deleted
			// This creates inconsistency - need to update bucket counters
			if physicalDeleted {
				logrus.WithFields(logrus.Fields{
					"bucket": bucket,
					"key":    key,
				}).Error("Physical file deleted but metadata deletion failed - bucket counters may be incorrect")

				// Decrement bucket counter since physical file is gone
				if om.bucketManager != nil {
					tenantID, bucketName := om.parseBucketPath(bucket)
					if err := om.bucketManager.DecrementObjectCount(ctx, tenantID, bucketName, objectSize); err != nil {
						logrus.WithError(err).Error("Failed to update bucket counters after inconsistent delete")
					}
				}
			}
			return fmt.Errorf("failed to delete metadata: %w", err)
		}
	}

	// Clean up empty parent directories
	om.cleanupEmptyDirectories(bucket, key)

	// Update bucket metrics (best effort - don't fail if this errors)
	if om.bucketManager != nil {
		tenantID, bucketName := om.parseBucketPath(bucket)
		logrus.WithFields(logrus.Fields{
			"bucket":     bucket,
			"tenantID":   tenantID,
			"bucketName": bucketName,
			"key":        key,
			"objectSize": objectSize,
		}).Debug("Decrementing bucket metrics after delete")

		if err := om.bucketManager.DecrementObjectCount(ctx, tenantID, bucketName, objectSize); err != nil {
			logrus.WithFields(logrus.Fields{
				"bucket":     bucket,
				"tenantID":   tenantID,
				"bucketName": bucketName,
				"objectSize": objectSize,
			}).WithError(err).Error("Failed to update bucket metrics after delete")
		} else {
			logrus.WithFields(logrus.Fields{
				"bucket":     bucket,
				"tenantID":   tenantID,
				"bucketName": bucketName,
				"objectSize": objectSize,
			}).Info("Successfully decremented bucket metrics")
		}
	} else {
		logrus.Warn("BucketManager is nil, cannot update metrics")
	}

	// Update tenant storage quota
	if om.authManager != nil {
		tenantID, _ := om.parseBucketPath(bucket)
		if tenantID != "" {
			if err := om.authManager.DecrementTenantStorage(ctx, tenantID, objectSize); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"tenant_id":  tenantID,
					"objectSize": objectSize,
				}).Warn("Failed to decrement tenant storage quota")
			}
		}
	}

	return nil
}

// ListObjects lists objects in a bucket
func (om *objectManager) ListObjects(ctx context.Context, bucket, prefix, delimiter, marker string, maxKeys int) (*ListObjectsResult, error) {
	if maxKeys <= 0 {
		maxKeys = 1000 // Default max keys
	}

	// Check if bucket exists first
	tenantID, bucketName := om.parseBucketPath(bucket)
	exists, err := om.metadataStore.BucketExists(ctx, tenantID, bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %w", err)
	}
	if !exists {
		return nil, ErrBucketNotFound
	}

	// When using delimiter, we need to scan more objects to find all unique folders
	// Since folders are derived from object keys, we must iterate through enough objects
	// to discover all common prefixes (folders)
	scanLimit := maxKeys
	if delimiter != "" {
		// Scan up to 100k objects to find all folders
		// This is necessary because folders are derived from file paths
		scanLimit = 100000
	}

	// OPTIMIZATION: Use BadgerDB metadata store for fast listing
	// This avoids expensive filesystem operations
	metadataObjects, nextMarker, err := om.metadataStore.ListObjects(ctx, bucket, prefix, marker, scanLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects from metadata store: %w", err)
	}

	var objects []Object
	commonPrefixesMap := make(map[string]bool) // Use map to avoid duplicates

	for _, metaObj := range metadataObjects {
		key := metaObj.Key

		// Skip internal MaxIOFS files
		// Check if the filename (not just prefix) contains .maxiofs-
		if strings.HasPrefix(key, ".maxiofs-") || strings.Contains(key, "/.maxiofs-") {
			continue
		}

		// Skip implicit folders (created automatically when uploading files)
		// In S3, only explicitly created folders should appear in listings
		if metaObj.Metadata != nil {
			if implicit, ok := metaObj.Metadata["x-maxiofs-implicit-folder"]; ok && implicit == "true" {
				continue
			}
		}

		// Skip Delete Markers (objects with Size=0 and empty ETag)
		// These are "deleted" objects and should not appear in ListObjects (AWS S3 behavior)
		if metaObj.Size == 0 && metaObj.ETag == "" {
			continue
		}

		// Handle delimiter (common prefixes / folders)
		if delimiter != "" {
			// Find the delimiter after the prefix
			remainingKey := key[len(prefix):]
			delimiterIndex := strings.Index(remainingKey, delimiter)

			if delimiterIndex >= 0 {
				// This object is inside a "folder"
				// Extract the common prefix (folder name)
				commonPrefix := prefix + remainingKey[:delimiterIndex+len(delimiter)]
				commonPrefixesMap[commonPrefix] = true
				continue // Don't include this object in the objects list
			}
		}

		// Convert metadata object to API object
		objects = append(objects, *fromMetadataObject(metaObj))
	}

	// Convert commonPrefixesMap to slice and sort
	var commonPrefixes []CommonPrefix
	for prefix := range commonPrefixesMap {
		commonPrefixes = append(commonPrefixes, CommonPrefix{Prefix: prefix})
	}
	sort.Slice(commonPrefixes, func(i, j int) bool {
		return commonPrefixes[i].Prefix < commonPrefixes[j].Prefix
	})

	// Objects are already sorted by BadgerDB iterator
	// Check if truncated based on nextMarker from metadata store
	isTruncated := nextMarker != ""

	// Apply maxKeys limit considering both objects and common prefixes
	totalItems := len(objects) + len(commonPrefixes)
	if totalItems > maxKeys {
		isTruncated = true

		// Prioritize showing common prefixes first, then objects
		if len(commonPrefixes) > maxKeys {
			commonPrefixes = commonPrefixes[:maxKeys]
			objects = []Object{}
			if len(commonPrefixes) > 0 {
				nextMarker = commonPrefixes[len(commonPrefixes)-1].Prefix
			}
		} else {
			remainingSlots := maxKeys - len(commonPrefixes)
			if len(objects) > remainingSlots {
				objects = objects[:remainingSlots]
			}
			if len(objects) > 0 {
				nextMarker = objects[len(objects)-1].Key
			}
		}
	}

	result := &ListObjectsResult{
		Objects:        objects,
		CommonPrefixes: commonPrefixes,
		IsTruncated:    isTruncated,
		NextMarker:     nextMarker,
		MaxKeys:        maxKeys,
		Prefix:         prefix,
		Delimiter:      delimiter,
		Marker:         marker,
	}

	return result, nil
}

// SearchObjects searches objects with filters applied server-side
func (om *objectManager) SearchObjects(ctx context.Context, bucket, prefix, delimiter, marker string, maxKeys int, filter *metadata.ObjectFilter) (*ListObjectsResult, error) {
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	// Check if bucket exists first
	tenantID, bucketName := om.parseBucketPath(bucket)
	exists, err := om.metadataStore.BucketExists(ctx, tenantID, bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %w", err)
	}
	if !exists {
		return nil, ErrBucketNotFound
	}

	// When using delimiter, scan more to find all unique folders
	scanLimit := maxKeys
	if delimiter != "" {
		scanLimit = 100000
	}

	metadataObjects, nextMarker, err := om.metadataStore.SearchObjects(ctx, bucket, prefix, marker, scanLimit, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to search objects: %w", err)
	}

	var objects []Object
	commonPrefixesMap := make(map[string]bool)

	for _, metaObj := range metadataObjects {
		key := metaObj.Key

		// Skip internal MaxIOFS files
		if strings.HasPrefix(key, ".maxiofs-") || strings.Contains(key, "/.maxiofs-") {
			continue
		}

		// Skip implicit folders
		if metaObj.Metadata != nil {
			if implicit, ok := metaObj.Metadata["x-maxiofs-implicit-folder"]; ok && implicit == "true" {
				continue
			}
		}

		// Skip Delete Markers
		if metaObj.Size == 0 && metaObj.ETag == "" {
			continue
		}

		// Handle delimiter (common prefixes / folders)
		if delimiter != "" {
			remainingKey := key[len(prefix):]
			delimiterIndex := strings.Index(remainingKey, delimiter)

			if delimiterIndex >= 0 {
				commonPrefix := prefix + remainingKey[:delimiterIndex+len(delimiter)]
				commonPrefixesMap[commonPrefix] = true
				continue
			}
		}

		objects = append(objects, *fromMetadataObject(metaObj))
	}

	// Convert commonPrefixesMap to slice and sort
	var commonPrefixes []CommonPrefix
	for pfx := range commonPrefixesMap {
		commonPrefixes = append(commonPrefixes, CommonPrefix{Prefix: pfx})
	}
	sort.Slice(commonPrefixes, func(i, j int) bool {
		return commonPrefixes[i].Prefix < commonPrefixes[j].Prefix
	})

	isTruncated := nextMarker != ""

	// Apply maxKeys limit
	totalItems := len(objects) + len(commonPrefixes)
	if totalItems > maxKeys {
		isTruncated = true
		if len(commonPrefixes) > maxKeys {
			commonPrefixes = commonPrefixes[:maxKeys]
			objects = []Object{}
			if len(commonPrefixes) > 0 {
				nextMarker = commonPrefixes[len(commonPrefixes)-1].Prefix
			}
		} else {
			remainingSlots := maxKeys - len(commonPrefixes)
			if len(objects) > remainingSlots {
				objects = objects[:remainingSlots]
			}
			if len(objects) > 0 {
				nextMarker = objects[len(objects)-1].Key
			}
		}
	}

	result := &ListObjectsResult{
		Objects:        objects,
		CommonPrefixes: commonPrefixes,
		IsTruncated:    isTruncated,
		NextMarker:     nextMarker,
		MaxKeys:        maxKeys,
		Prefix:         prefix,
		Delimiter:      delimiter,
		Marker:         marker,
	}

	return result, nil
}

// GetObjectMetadata retrieves object metadata
func (om *objectManager) GetObjectMetadata(ctx context.Context, bucket, key string) (*Object, error) {
	if err := om.validateObjectName(key); err != nil {
		return nil, err
	}

	objectPath := om.getObjectPath(bucket, key)

	// Check if object exists in storage
	exists, err := om.storage.Exists(ctx, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check object existence: %w", err)
	}
	if !exists {
		return nil, ErrObjectNotFound
	}

	// Try to load metadata from BadgerDB
	metaObj, err := om.metadataStore.GetObject(ctx, bucket, key)
	if err == nil && metaObj != nil {
		return fromMetadataObject(metaObj), nil
	}

	// Fallback: create basic object info from storage metadata
	storageMetadata, err := om.storage.GetMetadata(ctx, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage metadata: %w", err)
	}

	size, _ := strconv.ParseInt(storageMetadata["size"], 10, 64)
	lastModified, _ := strconv.ParseInt(storageMetadata["last_modified"], 10, 64)

	object := &Object{
		Key:          key,
		Bucket:       bucket,
		Size:         size,
		LastModified: time.Unix(lastModified, 0),
		ETag:         storageMetadata["etag"],
		ContentType:  storageMetadata["content-type"],
		Metadata:     storageMetadata,
		StorageClass: StorageClassStandard,
	}

	return object, nil
}

// UpdateObjectMetadata updates object metadata
func (om *objectManager) UpdateObjectMetadata(ctx context.Context, bucket, key string, metadata map[string]string) error {
	if err := om.validateObjectName(key); err != nil {
		return err
	}

	objectPath := om.getObjectPath(bucket, key)

	// Check if object exists
	exists, err := om.storage.Exists(ctx, objectPath)
	if err != nil {
		return fmt.Errorf("failed to check object existence: %w", err)
	}
	if !exists {
		return ErrObjectNotFound
	}

	// Update storage metadata
	if err := om.storage.SetMetadata(ctx, objectPath, metadata); err != nil {
		return fmt.Errorf("failed to update storage metadata: %w", err)
	}

	// Load current object metadata from BadgerDB
	object, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("failed to get object metadata: %w", err)
	}

	// Update object metadata
	if object.Metadata == nil {
		object.Metadata = make(map[string]string)
	}
	for k, v := range metadata {
		object.Metadata[k] = v
	}

	// Update content type if provided
	if contentType, exists := metadata["content-type"]; exists {
		object.ContentType = contentType
	}

	// Save updated metadata to BadgerDB
	metaObj := toMetadataObject(object)
	return om.metadataStore.PutObject(ctx, metaObj)
}

// Object Lock operations implementations

func (om *objectManager) GetObjectRetention(ctx context.Context, bucket, key string) (*RetentionConfig, error) {
	// Get object metadata
	obj, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	if obj.Retention == nil {
		return nil, ErrNoRetentionConfiguration
	}

	return obj.Retention, nil
}

func (om *objectManager) SetObjectRetention(ctx context.Context, bucket, key string, config *RetentionConfig) error {
	// Get object metadata
	obj, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return err
	}

	// Check if object is locked and retention is being shortened
	if obj.Retention != nil {
		if config == nil || config.RetainUntilDate.Before(obj.Retention.RetainUntilDate) {
			// Cannot shorten retention
			if obj.Retention.Mode == "COMPLIANCE" {
				return ErrCannotShortenCompliance
			}
			// For GOVERNANCE, would need bypass permission (not implemented yet)
			return ErrCannotShortenGovernance
		}
	}

	// Update retention
	obj.Retention = config

	// Save updated metadata to BadgerDB
	metaObj := toMetadataObject(obj)
	return om.metadataStore.PutObject(ctx, metaObj)
}

func (om *objectManager) GetObjectLegalHold(ctx context.Context, bucket, key string) (*LegalHoldConfig, error) {
	// Get object metadata
	obj, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	if obj.LegalHold == nil {
		// Return default (OFF)
		return &LegalHoldConfig{Status: "OFF"}, nil
	}

	return obj.LegalHold, nil
}

func (om *objectManager) SetObjectLegalHold(ctx context.Context, bucket, key string, config *LegalHoldConfig) error {
	// Get object metadata
	obj, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return err
	}

	// Update legal hold
	obj.LegalHold = config

	// Save updated metadata to BadgerDB
	metaObj := toMetadataObject(obj)
	return om.metadataStore.PutObject(ctx, metaObj)
}

// Versioning operations (Fase 7.2 - future)
func (om *objectManager) GetObjectVersions(ctx context.Context, bucket, key string) ([]ObjectVersion, error) {
	// Get versions from metadata store
	metaVersions, err := om.metadataStore.GetObjectVersions(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	// Convert metadata versions to object versions
	versions := make([]ObjectVersion, 0, len(metaVersions))
	for _, metaVer := range metaVersions {
		// Get full object metadata for this version
		objMeta, err := om.metadataStore.GetObject(ctx, bucket, key, metaVer.VersionID)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"bucket":    bucket,
				"key":       key,
				"versionID": metaVer.VersionID,
			}).Warn("Failed to get object metadata for version")
			continue
		}

		// Convert to Object
		obj := fromMetadataObject(objMeta)

		// Detect if this is a Delete Marker (Size == 0 and ETag is empty)
		isDeleteMarker := objMeta.Size == 0 && objMeta.ETag == ""

		// Create ObjectVersion
		version := ObjectVersion{
			Object:         *obj,
			IsLatest:       metaVer.IsLatest,
			IsDeleteMarker: isDeleteMarker,
		}

		versions = append(versions, version)
	}

	return versions, nil
}

func (om *objectManager) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	// TODO: Implement versioning in Fase 7.2
	return fmt.Errorf("versioning not yet implemented")
}

// Tagging operations implementations

func (om *objectManager) GetObjectTagging(ctx context.Context, bucket, key string) (*TagSet, error) {
	// Get object metadata
	obj, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	if obj.Tags == nil {
		// Return empty tagset
		return &TagSet{Tags: []Tag{}}, nil
	}

	return obj.Tags, nil
}

func (om *objectManager) SetObjectTagging(ctx context.Context, bucket, key string, tags *TagSet) error {
	// Get object metadata
	obj, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return err
	}

	// Validate tags
	if tags != nil && len(tags.Tags) > 10 {
		return ErrTooManyTags
	}

	// Update tags
	obj.Tags = tags

	// Save updated metadata to BadgerDB
	metaObj := toMetadataObject(obj)
	return om.metadataStore.PutObject(ctx, metaObj)
}

func (om *objectManager) DeleteObjectTagging(ctx context.Context, bucket, key string) error {
	// Get object metadata
	obj, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return err
	}

	// Clear tags
	obj.Tags = &TagSet{Tags: []Tag{}}

	// Save updated metadata to BadgerDB
	metaObj := toMetadataObject(obj)
	return om.metadataStore.PutObject(ctx, metaObj)
}

// ACL operations implementations

func (om *objectManager) GetObjectACL(ctx context.Context, bucket, key string) (*ACL, error) {
	// Get object metadata to ensure it exists
	_, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	// Parse bucket path to extract tenantID and bucketName
	tenantID, bucketName := om.parseBucketPath(bucket)

	// Get ACL from ACL manager
	aclData, err := om.aclManager.GetObjectACL(ctx, tenantID, bucketName, key)
	if err != nil {
		return nil, err
	}

	// Convert from acl.ACL to object.ACL
	return om.convertFromACLManagerType(aclData), nil
}

func (om *objectManager) SetObjectACL(ctx context.Context, bucket, key string, acl *ACL) error {
	// Get object metadata to ensure it exists
	_, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return err
	}

	// Parse bucket path to extract tenantID and bucketName
	tenantID, bucketName := om.parseBucketPath(bucket)

	// Convert from object.ACL to acl.ACL
	aclData := om.convertToACLManagerType(acl)

	// Set ACL using ACL manager
	return om.aclManager.SetObjectACL(ctx, tenantID, bucketName, key, aclData)
}

// convertFromACLManagerType converts acl.ACL to object.ACL
func (om *objectManager) convertFromACLManagerType(aclData *acl.ACL) *ACL {
	if aclData == nil {
		return nil
	}

	grants := make([]Grant, len(aclData.Grants))
	for i, g := range aclData.Grants {
		grants[i] = Grant{
			Grantee: Grantee{
				Type:         string(g.Grantee.Type),
				ID:           g.Grantee.ID,
				DisplayName:  g.Grantee.DisplayName,
				EmailAddress: g.Grantee.EmailAddress,
				URI:          g.Grantee.URI,
			},
			Permission: string(g.Permission),
		}
	}

	return &ACL{
		Owner: Owner{
			ID:          aclData.Owner.ID,
			DisplayName: aclData.Owner.DisplayName,
		},
		Grants: grants,
	}
}

// convertToACLManagerType converts object.ACL to acl.ACL
func (om *objectManager) convertToACLManagerType(objACL *ACL) *acl.ACL {
	if objACL == nil {
		return nil
	}

	grants := make([]acl.Grant, len(objACL.Grants))
	for i, g := range objACL.Grants {
		grants[i] = acl.Grant{
			Grantee: acl.Grantee{
				Type:         acl.GranteeType(g.Grantee.Type),
				ID:           g.Grantee.ID,
				DisplayName:  g.Grantee.DisplayName,
				EmailAddress: g.Grantee.EmailAddress,
				URI:          g.Grantee.URI,
			},
			Permission: acl.Permission(g.Permission),
		}
	}

	return &acl.ACL{
		Owner: acl.Owner{
			ID:          objACL.Owner.ID,
			DisplayName: objACL.Owner.DisplayName,
		},
		Grants: grants,
	}
}

// CreateMultipartUpload creates a new multipart upload session
func (om *objectManager) CreateMultipartUpload(ctx context.Context, bucket, key string, headers http.Header) (*MultipartUpload, error) {
	if err := om.validateObjectName(key); err != nil {
		return nil, err
	}

	// Generate unique upload ID
	uploadID, err := om.generateUploadID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate upload ID: %w", err)
	}

	// Extract metadata from headers
	metadata := make(map[string]string)
	for headerKey, values := range headers {
		if len(values) > 0 {
			key := strings.ToLower(headerKey)
			metadata[key] = values[0]
		}
	}

	// Set default content type if not provided
	if _, exists := metadata["content-type"]; !exists {
		metadata["content-type"] = "application/octet-stream"
	}

	// Create multipart upload metadata
	multipart := &MultipartUpload{
		UploadID:     uploadID,
		Bucket:       bucket,
		Key:          key,
		Initiated:    time.Now(),
		StorageClass: StorageClassStandard,
		Metadata:     metadata,
		Parts:        []Part{},
	}

	// Save multipart upload metadata to BadgerDB
	metaMU := toMetadataMultipartUpload(multipart)
	if err := om.metadataStore.CreateMultipartUpload(ctx, metaMU); err != nil {
		return nil, fmt.Errorf("failed to save multipart upload: %w", err)
	}

	return multipart, nil
}

func (om *objectManager) UploadPart(ctx context.Context, uploadID string, partNumber int, data io.Reader) (*Part, error) {
	if partNumber < 1 || partNumber > 10000 {
		return nil, fmt.Errorf("part number must be between 1 and 10000")
	}

	// Create part path
	partPath := om.getMultipartPartPath(uploadID, partNumber)

	// Store part data
	partMetadata := map[string]string{
		"upload-id":    uploadID,
		"part-number":  strconv.Itoa(partNumber),
		"content-type": "application/octet-stream",
	}

	if err := om.storage.Put(ctx, partPath, data, partMetadata); err != nil {
		return nil, fmt.Errorf("failed to store part: %w", err)
	}

	// Get part metadata to get size and etag
	storageMetadata, err := om.storage.GetMetadata(ctx, partPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get part metadata: %w", err)
	}

	size, _ := strconv.ParseInt(storageMetadata["size"], 10, 64)
	lastModified, _ := strconv.ParseInt(storageMetadata["last_modified"], 10, 64)

	partMeta := &metadata.PartMetadata{
		UploadID:     uploadID,
		PartNumber:   partNumber,
		ETag:         storageMetadata["etag"],
		Size:         size,
		LastModified: time.Unix(lastModified, 0),
	}

	// Store part metadata in BadgerDB
	if err := om.metadataStore.PutPart(ctx, partMeta); err != nil {
		return nil, fmt.Errorf("failed to save part metadata: %w", err)
	}

	part := &Part{
		PartNumber:   partNumber,
		ETag:         storageMetadata["etag"],
		Size:         size,
		LastModified: time.Unix(lastModified, 0),
	}

	return part, nil
}

func (om *objectManager) ListParts(ctx context.Context, uploadID string) ([]Part, error) {
	// List parts from BadgerDB
	metaParts, err := om.metadataStore.ListParts(ctx, uploadID)
	if err != nil {
		if err == metadata.ErrUploadNotFound {
			return nil, ErrInvalidUploadID
		}
		return nil, err
	}

	// Convert to object.Part
	parts := make([]Part, len(metaParts))
	for i, mp := range metaParts {
		parts[i] = Part{
			PartNumber:   mp.PartNumber,
			ETag:         mp.ETag,
			Size:         mp.Size,
			LastModified: mp.LastModified,
		}
	}

	return parts, nil
}

func (om *objectManager) CompleteMultipartUpload(ctx context.Context, uploadID string, parts []Part) (*Object, error) {
	// Load multipart upload metadata from BadgerDB
	metaMU, err := om.metadataStore.GetMultipartUpload(ctx, uploadID)
	if err != nil {
		if err == metadata.ErrUploadNotFound {
			return nil, ErrInvalidUploadID
		}
		return nil, err
	}
	multipart := fromMetadataMultipartUpload(metaMU)

	// Validate parts list and calculate total size
	totalSize, err := om.validateAndCalculatePartsSize(ctx, uploadID, parts)
	if err != nil {
		return nil, err
	}

	// Check if this overwrites an existing object (before combining parts)
	existingObj, _ := om.metadataStore.GetObject(ctx, multipart.Bucket, multipart.Key)
	isNewObject := existingObj == nil

	// Validate tenant storage quota BEFORE combining parts (early rejection to avoid wasted work)
	if err := om.checkMultipartQuotaBeforeComplete(ctx, multipart.Bucket, uploadID, totalSize, existingObj); err != nil {
		return nil, err
	}

	// Combine parts into final object
	objectPath := om.getObjectPath(multipart.Bucket, multipart.Key)
	if err := om.combineMultipartParts(ctx, uploadID, parts, objectPath); err != nil {
		return nil, fmt.Errorf("failed to combine parts: %w", err)
	}

	// Stream combined file to temp while calculating MD5 hash
	originalSize, originalETag, tempPath, err := om.calculateMultipartHash(ctx, objectPath)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tempPath) // Clean up temp file when done

	// Get timestamp from storage before replacing file
	storageMetadata, err := om.storage.GetMetadata(ctx, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get object metadata: %w", err)
	}
	lastModified, _ := strconv.ParseInt(storageMetadata["last_modified"], 10, 64)

	// Create and save object metadata to BadgerDB with ORIGINAL values
	object := &Object{
		Key:          multipart.Key,
		Bucket:       multipart.Bucket,
		Size:         originalSize,
		LastModified: time.Unix(lastModified, 0),
		ETag:         originalETag,
		ContentType:  multipart.Metadata["content-type"],
		Metadata:     multipart.Metadata,
		StorageClass: multipart.StorageClass,
	}

	metaObj := toMetadataObject(object)
	if err := om.metadataStore.PutObject(ctx, metaObj); err != nil {
		logrus.WithError(err).Warn("Failed to save final object metadata to BadgerDB")
	}

	logrus.WithFields(logrus.Fields{
		"uploadID":     uploadID,
		"originalSize": originalSize,
		"originalETag": originalETag,
	}).Info("Saved original metadata to BadgerDB before encryption")

	// Apply encryption if enabled
	if om.shouldEncryptMultipartObject(ctx, multipart.Bucket) {
		if err := om.storeEncryptedMultipartObject(ctx, objectPath, tempPath, uploadID, multipart, originalSize, originalETag); err != nil {
			return nil, err
		}
	} else {
		if err := om.storeUnencryptedMultipartObject(ctx, objectPath, tempPath, uploadID, multipart, originalSize, originalETag); err != nil {
			return nil, err
		}
	}

	// Update bucket metrics and clean up multipart data
	om.updateMetricsAndCleanupMultipart(ctx, multipart.Bucket, uploadID, originalSize, isNewObject, existingObj, parts)

	return object, nil
}

func (om *objectManager) AbortMultipartUpload(ctx context.Context, uploadID string) error {
	return om.abortMultipartUpload(ctx, uploadID, true)
}

func (om *objectManager) ListMultipartUploads(ctx context.Context, bucket string) ([]MultipartUpload, error) {
	// List multipart uploads from BadgerDB (with prefix matching on bucket)
	metaUploads, err := om.metadataStore.ListMultipartUploads(ctx, bucket, "", 1000)
	if err != nil {
		return nil, fmt.Errorf("failed to list multipart uploads: %w", err)
	}

	// Convert to object.MultipartUpload
	uploads := make([]MultipartUpload, len(metaUploads))
	for i, mu := range metaUploads {
		uploads[i] = *fromMetadataMultipartUpload(mu)
	}

	return uploads, nil
}

// Placeholder implementation for copy operations
// IsReady checks if the object manager is ready
func (om *objectManager) IsReady() bool {
	// TODO: Implement readiness check
	return true
}

// Helper methods

// validateObjectName validates object key according to S3 rules
func (om *objectManager) validateObjectName(key string) error {
	if key == "" {
		return ErrInvalidObjectName
	}

	// Check for invalid characters
	if strings.Contains(key, "../") || strings.Contains(key, "/..") {
		return ErrInvalidObjectName
	}

	// Check for absolute paths
	if strings.HasPrefix(key, "/") {
		return ErrInvalidObjectName
	}

	// Check maximum length (1024 characters for S3)
	if len(key) > 1024 {
		return ErrInvalidObjectName
	}

	return nil
}

// getObjectPath returns the storage path for an object
func (om *objectManager) getObjectPath(bucket, key string) string {
	return fmt.Sprintf("%s/%s", bucket, key)
}

// getVersionedObjectPath returns the storage path for a versioned object
// Format: bucket/.versions/key/versionID
func (om *objectManager) getVersionedObjectPath(bucket, key, versionID string) string {
	return fmt.Sprintf("%s/.versions/%s/%s", bucket, key, versionID)
}

// Removed: getObjectMetadataPath, saveObjectMetadata, loadObjectMetadata
// These functions are now replaced with BadgerDB operations via metadataStore

// loadBucketMetadata loads bucket metadata from BadgerDB to check Object Lock configuration
func (om *objectManager) loadBucketMetadata(ctx context.Context, bucketName string) (*metadata.BucketMetadata, error) {
	// Parse bucket path to extract tenantID and bucket name
	tenantID, actualBucketName := om.parseBucketPath(bucketName)

	// Get bucket metadata from BadgerDB
	bucketMeta, err := om.metadataStore.GetBucket(ctx, tenantID, actualBucketName)
	if err != nil {
		if err == metadata.ErrBucketNotFound {
			return nil, fmt.Errorf("bucket metadata not found")
		}
		return nil, fmt.Errorf("failed to load bucket metadata: %w", err)
	}

	return bucketMeta, nil
}

// applyDefaultRetention applies bucket's default Object Lock retention to a new object
func (om *objectManager) applyDefaultRetention(ctx context.Context, object *Object) error {
	// Load bucket metadata to check for Object Lock configuration
	bucketMeta, err := om.loadBucketMetadata(ctx, object.Bucket)
	if err != nil {
		// Bucket metadata not found or no Object Lock - not an error
		return nil
	}

	// Check if Object Lock is enabled
	if bucketMeta.ObjectLock == nil || !bucketMeta.ObjectLock.Enabled {
		return nil
	}

	// Check for default retention rule
	if bucketMeta.ObjectLock.Rule == nil || bucketMeta.ObjectLock.Rule.DefaultRetention == nil {
		return nil
	}

	retention := bucketMeta.ObjectLock.Rule.DefaultRetention

	// Apply retention to object
	object.Retention = &RetentionConfig{
		Mode:            retention.Mode,
		RetainUntilDate: retention.RetainUntilDate,
	}

	return nil
}

// Multipart upload helper methods

// generateUploadID generates a unique upload ID
func (om *objectManager) generateUploadID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// getMultipartPartPath returns the path for a multipart part in storage
func (om *objectManager) getMultipartPartPath(uploadID string, partNumber int) string {
	return fmt.Sprintf(".maxiofs/multipart/parts/%s/%05d", uploadID, partNumber)
}

// Removed: getMultipartUploadPath, saveMultipartUpload, loadMultipartUpload, updatePartsList
// These functions are now replaced with BadgerDB operations via metadataStore

// combineMultipartParts combines all parts into the final object
func (om *objectManager) combineMultipartParts(ctx context.Context, uploadID string, parts []Part, finalPath string) error {
	// Create a combined metadata
	combinedMetadata := map[string]string{
		"content-type": "application/octet-stream",
	}

	if len(parts) == 0 {
		return fmt.Errorf("no parts to combine")
	}

	// Get content type from first part if available
	if len(parts) > 0 {
		firstPartPath := om.getMultipartPartPath(uploadID, parts[0].PartNumber)
		metadata, err := om.storage.GetMetadata(ctx, firstPartPath)
		if err == nil {
			if contentType, exists := metadata["content-type"]; exists {
				combinedMetadata["content-type"] = contentType
			}
		}
	}

	// Create a MultiReader that concatenates all parts in order
	readers := make([]io.Reader, len(parts))
	for i, part := range parts {
		partPath := om.getMultipartPartPath(uploadID, part.PartNumber)
		reader, _, err := om.storage.Get(ctx, partPath)
		if err != nil {
			// Close all previously opened readers
			for j := 0; j < i; j++ {
				if closer, ok := readers[j].(io.Closer); ok {
					closer.Close()
				}
			}
			return fmt.Errorf("failed to read part %d: %w", part.PartNumber, err)
		}
		readers[i] = reader
	}

	// Create a combined reader that reads all parts sequentially
	combinedReader := io.MultiReader(readers...)

	// Store the combined object
	err := om.storage.Put(ctx, finalPath, combinedReader, combinedMetadata)

	// Close all readers after Put completes
	for _, reader := range readers {
		if closer, ok := reader.(io.Closer); ok {
			closer.Close()
		}
	}

	return err
}

// abortMultipartUpload cleans up a multipart upload
func (om *objectManager) abortMultipartUpload(ctx context.Context, uploadID string, returnError bool) error {
	// Get parts list from BadgerDB before deleting
	metaParts, err := om.metadataStore.ListParts(ctx, uploadID)
	if err != nil {
		if returnError && err != metadata.ErrUploadNotFound {
			return err
		}
		return nil
	}

	// Delete all part files from storage
	for _, part := range metaParts {
		partPath := om.getMultipartPartPath(uploadID, part.PartNumber)
		om.storage.Delete(ctx, partPath) // Ignore errors
	}

	// Delete multipart upload metadata from BadgerDB
	err = om.metadataStore.AbortMultipartUpload(ctx, uploadID)
	if err != nil && err != metadata.ErrUploadNotFound && returnError {
		return fmt.Errorf("failed to delete multipart upload metadata: %w", err)
	}

	return nil
}

// ensureImplicitFolders creates folder objects in BadgerDB for all parent directories
// of the given key. This is necessary because S3 clients often upload files to nested
// paths without explicitly creating parent folders first.
// For example, uploading "folder1/folder2/file.txt" should create:
// - "folder1/" (folder object in BadgerDB)
// - "folder1/folder2/" (folder object in BadgerDB)
// - "folder1/folder2/file.txt" (actual file object)
func (om *objectManager) ensureImplicitFolders(ctx context.Context, bucket, key string) {
	// Skip if key ends with / (it's already a folder)
	if strings.HasSuffix(key, "/") {
		return
	}

	// Extract all parent directories from the key
	parts := strings.Split(key, "/")
	if len(parts) <= 1 {
		return // No parent directories
	}

	logrus.WithFields(logrus.Fields{
		"bucket": bucket,
		"key":    key,
		"parts":  len(parts) - 1,
	}).Debug("ensureImplicitFolders called")

	// Create folder objects for each parent directory
	currentPath := ""
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "" {
			continue
		}

		if currentPath != "" {
			currentPath += "/"
		}
		currentPath += parts[i]
		folderKey := currentPath + "/"

		// Check if folder object already exists in BadgerDB
		_, err := om.metadataStore.GetObject(ctx, bucket, folderKey)
		if err == nil {
			// Folder already exists, skip
			logrus.WithField("folder_key", folderKey).Debug("Folder already exists, skipping")
			continue
		}

		// Create folder object in BadgerDB
		now := time.Now()
		folderMetadata := make(map[string]string)
		folderMetadata["x-maxiofs-implicit-folder"] = "true" // Mark as implicit
		folderObj := &metadata.ObjectMetadata{
			Bucket:       bucket,
			Key:          folderKey,
			Size:         0,
			LastModified: now,
			ETag:         "d41d8cd98f00b204e9800998ecf8427e", // MD5 of empty string
			ContentType:  "application/x-directory",
			Metadata:     folderMetadata,
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		if err := om.metadataStore.PutObject(ctx, folderObj); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"bucket":     bucket,
				"folder_key": folderKey,
			}).Debug("Failed to create implicit folder object in BadgerDB")
		} else {
			logrus.WithFields(logrus.Fields{
				"bucket":     bucket,
				"folder_key": folderKey,
			}).Debug("Created implicit folder object in BadgerDB")
		}
	}
}

// cleanupEmptyDirectories removes empty parent directories after object deletion
func (om *objectManager) cleanupEmptyDirectories(bucket, key string) {
	// Get the filesystem backend to work with directories
	fsBackend, ok := om.storage.(*storage.FilesystemBackend)
	if !ok {
		return
	}

	// Get the object path
	objectPath := om.getObjectPath(bucket, key)
	dirPath := filepath.Dir(objectPath)

	// Get the root path from filesystem backend
	rootPath := fsBackend.GetRootPath()

	// Walk up the directory tree and remove empty directories
	for {
		// Don't go above the root path
		if !strings.HasPrefix(dirPath, rootPath) || dirPath == rootPath {
			break
		}

		// Check if directory is empty (only .maxiofs-folder marker or completely empty)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			break
		}

		// Count non-system files
		nonSystemFiles := 0
		for _, entry := range entries {
			if entry.Name() != ".maxiofs-folder" && !strings.HasSuffix(entry.Name(), ".metadata") {
				nonSystemFiles++
			}
		}

		// If directory only has system files or is empty, remove it
		if nonSystemFiles == 0 {
			if err := os.RemoveAll(dirPath); err != nil {
				logrus.WithError(err).WithField("path", dirPath).Debug("Failed to remove empty directory")
				break
			}
			logrus.WithField("path", dirPath).Debug("Removed empty directory")

			// Move to parent directory
			parentDir := filepath.Dir(dirPath)
			if parentDir == dirPath {
				break
			}
			dirPath = parentDir
		} else {
			// Directory has files, stop cleanup
			break
		}
	}
}

// ========== PutObject Helper Functions (Refactoring for Complexity Reduction) ==========

// extractMetadataFromHeaders extracts storage and user metadata from HTTP headers
func (om *objectManager) extractMetadataFromHeaders(headers http.Header) (storageMetadata, userMetadata map[string]string) {
	storageMetadata = make(map[string]string)
	userMetadata = make(map[string]string)

	// Extract Content-Type
	if contentType := headers.Get("Content-Type"); contentType != "" {
		storageMetadata["content-type"] = contentType
	} else {
		storageMetadata["content-type"] = "application/octet-stream"
	}

	// Extract user-defined metadata (x-amz-meta-* headers)
	for headerKey, values := range headers {
		if len(values) > 0 {
			lowerKey := strings.ToLower(headerKey)
			// Only store x-amz-meta-* headers as user metadata
			if strings.HasPrefix(lowerKey, "x-amz-meta-") {
				metaKey := strings.TrimPrefix(lowerKey, "x-amz-meta-")
				userMetadata[metaKey] = values[0]
			}
		}
	}

	return storageMetadata, userMetadata
}

// shouldEncryptObject determines if an object should be encrypted based on server and bucket configuration
func (om *objectManager) shouldEncryptObject(ctx context.Context, tenantID, bucketName string) bool {
	if !om.config.EnableEncryption {
		return false
	}

	// Server encryption is enabled, check bucket preference
	bucketInfo, err := om.metadataStore.GetBucket(ctx, tenantID, bucketName)
	if err == nil && bucketInfo != nil && bucketInfo.Encryption != nil {
		// Check if bucket has encryption rules configured
		if len(bucketInfo.Encryption.Rules) > 0 && bucketInfo.Encryption.Rules[0].ApplyServerSideEncryptionByDefault != nil {
			sseConfig := bucketInfo.Encryption.Rules[0].ApplyServerSideEncryptionByDefault
			if sseConfig.SSEAlgorithm != "" {
				// Bucket has encryption configured
				return true
			}
		}
	}

	return false
}

// storeEncryptedObject encrypts and stores an object
func (om *objectManager) storeEncryptedObject(ctx context.Context, objectPath, tempPath string, storageMetadata map[string]string, originalSize int64, originalETag string) error {
	// Save original metadata (size and etag are from UNENCRYPTED data)
	storageMetadata["original-size"] = fmt.Sprintf("%d", originalSize)
	storageMetadata["original-etag"] = originalETag
	storageMetadata["encrypted"] = "true"
	storageMetadata["x-amz-server-side-encryption"] = "AES256"
	storageMetadata["x-amz-server-side-encryption-algorithm"] = "AES-256-CTR"

	// Open temp file for reading and encrypt while streaming to storage
	tempFileRead, err := os.Open(tempPath)
	if err != nil {
		return fmt.Errorf("failed to open temp file for encryption: %w", err)
	}
	defer tempFileRead.Close()

	// Create a pipe for streaming encryption
	pipeReader, pipeWriter := io.Pipe()

	// Encrypt in background goroutine
	go func() {
		defer pipeWriter.Close()
		if _, err := om.encryptionService.EncryptStream(tempFileRead, pipeWriter); err != nil {
			logrus.WithError(err).Error("Failed to encrypt object during upload")
			pipeWriter.CloseWithError(fmt.Errorf("encryption failed: %w", err))
		}
	}()

	// Store encrypted data (streaming from pipe)
	if err := om.storage.Put(ctx, objectPath, pipeReader, storageMetadata); err != nil {
		return fmt.Errorf("failed to store object: %w", err)
	}

	return nil
}

// storeUnencryptedObject stores an object without encryption
func (om *objectManager) storeUnencryptedObject(ctx context.Context, objectPath, tempPath string, storageMetadata map[string]string, originalSize int64, originalETag string) error {
	// Use original size and ETag directly
	storageMetadata["size"] = fmt.Sprintf("%d", originalSize)
	storageMetadata["etag"] = originalETag
	// Do NOT set "encrypted" = "true"

	// Open temp file for reading
	tempFileRead, err := os.Open(tempPath)
	if err != nil {
		return fmt.Errorf("failed to open temp file: %w", err)
	}
	defer tempFileRead.Close()

	// Store unencrypted data directly
	if err := om.storage.Put(ctx, objectPath, tempFileRead, storageMetadata); err != nil {
		return fmt.Errorf("failed to store object: %w", err)
	}

	return nil
}

// updateBucketMetricsAfterPut updates bucket metrics after a PutObject operation
func (om *objectManager) updateBucketMetricsAfterPut(ctx context.Context, tenantID, bucketName, bucket, key string, size int64, versioningEnabled bool, existingObjBeforeSave *metadata.ObjectMetadata) {
	if om.bucketManager == nil {
		return
	}

	// Check if this is a new object (not an overwrite)
	// For non-versioned buckets: check if object existed before
	// For versioned buckets: only count the first version
	if !versioningEnabled {
		// Use the existing object we captured BEFORE saving
		isNewObject := existingObjBeforeSave == nil

		if isNewObject {
			// New object - increment count and size
			if err := om.bucketManager.IncrementObjectCount(ctx, tenantID, bucketName, size); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"bucket_path": bucket,
					"tenant_id":   tenantID,
					"bucket_name": bucketName,
					"key":         key,
					"size":        size,
				}).Warn("Failed to increment bucket object count")
			}
		} else {
			// Overwrite - adjust size difference only
			sizeDiff := size - existingObjBeforeSave.Size
			if sizeDiff != 0 {
				if err := om.bucketManager.IncrementObjectCount(ctx, tenantID, bucketName, sizeDiff); err != nil {
					logrus.WithError(err).WithFields(logrus.Fields{
						"bucket_path": bucket,
						"key":         key,
						"old_size":    existingObjBeforeSave.Size,
						"new_size":    size,
						"size_diff":   sizeDiff,
					}).Warn("Failed to adjust bucket size on overwrite")
				}
			}
		}
	} else {
		// Versioned bucket - only increment count on first version
		existingVersions, err := om.metadataStore.GetObjectVersions(ctx, bucket, key)
		if err != nil || len(existingVersions) == 1 {
			// First version - increment count
			if err := om.bucketManager.IncrementObjectCount(ctx, tenantID, bucketName, size); err != nil {
				logrus.WithError(err).Warn("Failed to increment bucket object count for first version")
			}
		} else {
			// Additional version - only increment size, not count
			if err := om.bucketManager.IncrementObjectCount(ctx, tenantID, bucketName, size); err != nil {
				logrus.WithError(err).Warn("Failed to increment bucket size for additional version")
			}
		}
	}
}

// updateTenantQuotaAfterPut updates tenant storage quota after a PutObject operation
func (om *objectManager) updateTenantQuotaAfterPut(ctx context.Context, tenantID, key string, size int64, versioningEnabled bool, existingObjBeforeSave *metadata.ObjectMetadata) {
	if om.authManager == nil || tenantID == "" {
		return
	}

	var sizeToAdd int64
	if !versioningEnabled {
		// Use the existing object we captured BEFORE saving
		if existingObjBeforeSave == nil {
			sizeToAdd = size // New object
		} else {
			sizeToAdd = size - existingObjBeforeSave.Size // Size difference
		}
	} else {
		sizeToAdd = size // Versioned: always add new version size
	}

	logrus.WithFields(logrus.Fields{
		"tenantID": tenantID,
		"key":      key,
		"newSize":  size,
		"existingSize": func() int64 {
			if existingObjBeforeSave != nil {
				return existingObjBeforeSave.Size
			}
			return 0
		}(),
		"sizeToAdd":   sizeToAdd,
		"isNewObject": existingObjBeforeSave == nil,
	}).Info("Updating tenant storage quota after PutObject")

	if sizeToAdd != 0 {
		if err := om.authManager.IncrementTenantStorage(ctx, tenantID, sizeToAdd); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"tenant_id": tenantID,
				"size":      sizeToAdd,
			}).Warn("Failed to increment tenant storage quota")
		} else {
			logrus.WithFields(logrus.Fields{
				"tenantID":  tenantID,
				"sizeAdded": sizeToAdd,
			}).Info("Successfully incremented tenant storage")
		}
	} else {
		logrus.WithField("tenantID", tenantID).Debug("No storage change needed (sizeToAdd = 0)")
	}
}

// ========== CompleteMultipartUpload Helper Functions (Refactoring for Complexity Reduction) ==========

// validateAndCalculatePartsSize validates parts list and calculates total size
func (om *objectManager) validateAndCalculatePartsSize(ctx context.Context, uploadID string, parts []Part) (int64, error) {
	if len(parts) == 0 {
		return 0, fmt.Errorf("no parts provided")
	}

	// Sort parts by part number
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	// Validate parts exist in storage and calculate total size
	var totalSize int64
	for _, part := range parts {
		partPath := om.getMultipartPartPath(uploadID, part.PartNumber)
		exists, err := om.storage.Exists(ctx, partPath)
		if err != nil {
			return 0, fmt.Errorf("failed to check part %d existence: %w", part.PartNumber, err)
		}
		if !exists {
			return 0, fmt.Errorf("part %d not found", part.PartNumber)
		}

		// Get part metadata to get the actual size
		partMeta, err := om.metadataStore.GetPart(ctx, uploadID, part.PartNumber)
		if err != nil {
			return 0, fmt.Errorf("failed to get part %d metadata: %w", part.PartNumber, err)
		}
		totalSize += partMeta.Size
	}

	return totalSize, nil
}

// checkMultipartQuotaBeforeComplete validates tenant quota before combining parts
func (om *objectManager) checkMultipartQuotaBeforeComplete(ctx context.Context, bucket, uploadID string, totalSize int64, existingObj *metadata.ObjectMetadata) error {
	if om.authManager == nil {
		return nil
	}

	tenantID, _ := om.parseBucketPath(bucket)
	if tenantID == "" {
		return nil
	}

	isNewObject := existingObj == nil
	var sizeIncrement int64
	if isNewObject {
		sizeIncrement = totalSize
	} else {
		sizeIncrement = totalSize - existingObj.Size
	}

	// Only check quota if adding storage
	if sizeIncrement <= 0 {
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"tenantID":  tenantID,
		"uploadID":  uploadID,
		"totalSize": totalSize,
		"existingSize": func() int64 {
			if existingObj != nil {
				return existingObj.Size
			}
			return 0
		}(),
		"sizeIncrement": sizeIncrement,
	}).Info("Validating quota before completing multipart upload")

	if err := om.authManager.CheckTenantStorageQuota(ctx, tenantID, sizeIncrement); err != nil {
		logrus.WithFields(logrus.Fields{
			"tenantID":      tenantID,
			"uploadID":      uploadID,
			"sizeIncrement": sizeIncrement,
			"error":         err,
		}).Warn("Multipart upload quota validation failed")
		return fmt.Errorf("storage quota exceeded: %w", err)
	}

	return nil
}

// calculateMultipartHash streams combined file to temp while calculating MD5 hash
func (om *objectManager) calculateMultipartHash(ctx context.Context, objectPath string) (int64, string, string, error) {
	combinedReader, _, err := om.storage.Get(ctx, objectPath)
	if err != nil {
		return 0, "", "", fmt.Errorf("failed to read combined object: %w", err)
	}

	// Create temp file for calculating metadata
	tempFile, err := os.CreateTemp("", "maxiofs-multipart-*")
	if err != nil {
		combinedReader.Close()
		return 0, "", "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Stream to temp file while calculating MD5 hash
	hasher := md5.New()
	multiWriter := io.MultiWriter(tempFile, hasher)
	originalSize, err := io.Copy(multiWriter, combinedReader)
	combinedReader.Close()
	if err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return 0, "", "", fmt.Errorf("failed to write to temp file: %w", err)
	}
	tempFile.Close()

	originalETag := hex.EncodeToString(hasher.Sum(nil))
	return originalSize, originalETag, tempPath, nil
}

// shouldEncryptMultipartObject determines if multipart object should be encrypted
func (om *objectManager) shouldEncryptMultipartObject(ctx context.Context, bucket string) bool {
	if !om.config.EnableEncryption {
		return false
	}

	tenantID, bucketName := om.parseBucketPath(bucket)
	bucketInfo, err := om.metadataStore.GetBucket(ctx, tenantID, bucketName)
	if err != nil || bucketInfo == nil || bucketInfo.Encryption == nil {
		return false
	}

	if len(bucketInfo.Encryption.Rules) > 0 && bucketInfo.Encryption.Rules[0].ApplyServerSideEncryptionByDefault != nil {
		sseConfig := bucketInfo.Encryption.Rules[0].ApplyServerSideEncryptionByDefault
		return sseConfig.SSEAlgorithm != ""
	}

	return false
}

// storeEncryptedMultipartObject encrypts and stores multipart object
func (om *objectManager) storeEncryptedMultipartObject(ctx context.Context, objectPath, tempPath string, uploadID string, multipart *MultipartUpload, originalSize int64, originalETag string) error {
	tempFileRead, err := os.Open(tempPath)
	if err != nil {
		return fmt.Errorf("failed to open temp file for encryption: %w", err)
	}
	defer tempFileRead.Close()

	// Create a pipe for streaming encryption
	pipeReader, pipeWriter := io.Pipe()

	// Encrypt in background goroutine
	go func() {
		defer pipeWriter.Close()
		if _, err := om.encryptionService.EncryptStream(tempFileRead, pipeWriter); err != nil {
			logrus.WithError(err).Error("Failed to encrypt multipart object")
			pipeWriter.CloseWithError(fmt.Errorf("encryption failed: %w", err))
		}
	}()

	// Store encryption markers in storage metadata
	encryptionMetadata := map[string]string{
		"original-size":                          fmt.Sprintf("%d", originalSize),
		"original-etag":                          originalETag,
		"encrypted":                              "true",
		"x-amz-server-side-encryption":           "AES256",
		"x-amz-server-side-encryption-algorithm": "AES-256-CTR",
		"content-type":                           multipart.Metadata["content-type"],
	}

	// Copy any user metadata from multipart upload
	for k, v := range multipart.Metadata {
		if k != "content-type" {
			encryptionMetadata[k] = v
		}
	}

	if err := om.storage.Put(ctx, objectPath, pipeReader, encryptionMetadata); err != nil {
		return fmt.Errorf("failed to store encrypted multipart object: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"uploadID": uploadID,
		"bucket":   multipart.Bucket,
		"key":      multipart.Key,
	}).Info("Multipart object encrypted and stored successfully (streaming)")

	return nil
}

// storeUnencryptedMultipartObject stores multipart object without encryption
func (om *objectManager) storeUnencryptedMultipartObject(ctx context.Context, objectPath, tempPath string, uploadID string, multipart *MultipartUpload, originalSize int64, originalETag string) error {
	unencryptedMetadata := map[string]string{
		"size":         fmt.Sprintf("%d", originalSize),
		"etag":         originalETag,
		"content-type": multipart.Metadata["content-type"],
	}

	// Copy any user metadata from multipart upload
	for k, v := range multipart.Metadata {
		if k != "content-type" {
			unencryptedMetadata[k] = v
		}
	}

	// Open temp file to replace combined file with proper metadata
	tempFileRead, err := os.Open(tempPath)
	if err != nil {
		return fmt.Errorf("failed to open temp file: %w", err)
	}
	defer tempFileRead.Close()

	// Replace with unencrypted version (streaming)
	if err := om.storage.Put(ctx, objectPath, tempFileRead, unencryptedMetadata); err != nil {
		return fmt.Errorf("failed to store multipart object: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"uploadID": uploadID,
		"bucket":   multipart.Bucket,
		"key":      multipart.Key,
	}).Info("Multipart object stored successfully (unencrypted)")

	return nil
}

// updateMetricsAndCleanupMultipart updates bucket/tenant metrics and cleans up multipart data
func (om *objectManager) updateMetricsAndCleanupMultipart(ctx context.Context, bucket, uploadID string, originalSize int64, isNewObject bool, existingObj *metadata.ObjectMetadata, parts []Part) {
	tenantID, bucketName := om.parseBucketPath(bucket)

	// Update bucket metrics
	if om.bucketManager != nil {
		if isNewObject {
			if err := om.bucketManager.IncrementObjectCount(ctx, tenantID, bucketName, originalSize); err != nil {
				logrus.WithError(err).Warn("Failed to increment bucket metrics after multipart upload")
			}
		} else {
			sizeDiff := originalSize - existingObj.Size
			if sizeDiff != 0 {
				if err := om.bucketManager.IncrementObjectCount(ctx, tenantID, bucketName, sizeDiff); err != nil {
					logrus.WithError(err).Warn("Failed to adjust bucket size after multipart overwrite")
				}
			}
		}

		// Update tenant storage quota
		if om.authManager != nil && tenantID != "" {
			var sizeToAdd int64
			if isNewObject {
				sizeToAdd = originalSize
			} else {
				sizeToAdd = originalSize - existingObj.Size
			}
			if sizeToAdd != 0 {
				if err := om.authManager.IncrementTenantStorage(ctx, tenantID, sizeToAdd); err != nil {
					logrus.WithError(err).Warn("Failed to increment tenant storage after multipart upload")
				}
			}
		}
	}

	// Clean up multipart upload from BadgerDB
	if err := om.metadataStore.AbortMultipartUpload(ctx, uploadID); err != nil {
		logrus.WithError(err).Warn("Failed to delete multipart upload from BadgerDB")
	}

	// Clean up part files from storage
	for _, part := range parts {
		partPath := om.getMultipartPartPath(uploadID, part.PartNumber)
		om.storage.Delete(ctx, partPath) // Ignore errors
	}
}
