package object

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// Manager defines the interface for object management
type Manager interface {
	// Basic object operations
	GetObject(ctx context.Context, bucket, key string) (*Object, io.ReadCloser, error)
	PutObject(ctx context.Context, bucket, key string, data io.Reader, headers http.Header) (*Object, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	ListObjects(ctx context.Context, bucket, prefix, delimiter, marker string, maxKeys int) (*ListObjectsResult, error)

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

	// Copy operations
	CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, headers http.Header) (*Object, error)

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
	storage       storage.Backend
	config        config.StorageConfig
	metadataStore metadata.Store
	bucketManager interface {
		IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
		DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
	}
}

// NewManager creates a new object manager
func NewManager(storage storage.Backend, metadataStore metadata.Store, config config.StorageConfig) Manager {
	return &objectManager{
		storage:       storage,
		config:        config,
		metadataStore: metadataStore,
		bucketManager: nil, // Will be set later via SetBucketManager
	}
}

// SetBucketManager sets the bucket manager for metrics updates
func (om *objectManager) SetBucketManager(bm interface {
	IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
	DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
}) {
	om.bucketManager = bm
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

// GetObject retrieves an object
func (om *objectManager) GetObject(ctx context.Context, bucket, key string) (*Object, io.ReadCloser, error) {
	if err := om.validateObjectName(key); err != nil {
		return nil, nil, err
	}

	objectPath := om.getObjectPath(bucket, key)

	// Get object data
	reader, storageMetadata, err := om.storage.Get(ctx, objectPath)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			return nil, nil, ErrObjectNotFound
		}
		return nil, nil, fmt.Errorf("failed to get object: %w", err)
	}

	// Load object metadata from BadgerDB
	metaObj, err := om.metadataStore.GetObject(ctx, bucket, key)
	if err != nil && err != metadata.ErrObjectNotFound {
		// Log error but continue with basic metadata
		logrus.WithError(err).Debug("Failed to load object metadata from BadgerDB")
		metaObj = nil // Ensure metaObj is nil on error
	}

	var object *Object
	if metaObj != nil {
		object = fromMetadataObject(metaObj)
	} else {
		// If metadata doesn't exist, create basic object info from storage metadata
		size, _ := strconv.ParseInt(storageMetadata["size"], 10, 64)
		lastModified, _ := strconv.ParseInt(storageMetadata["last_modified"], 10, 64)

		object = &Object{
			Key:          key,
			Bucket:       bucket,
			Size:         size,
			LastModified: time.Unix(lastModified, 0),
			ETag:         storageMetadata["etag"],
			ContentType:  storageMetadata["content-type"],
			Metadata:     storageMetadata,
			StorageClass: StorageClassStandard,
		}
	}

	return object, reader, nil
}

// PutObject stores an object
func (om *objectManager) PutObject(ctx context.Context, bucket, key string, data io.Reader, headers http.Header) (*Object, error) {
	if err := om.validateObjectName(key); err != nil {
		return nil, err
	}

	objectPath := om.getObjectPath(bucket, key)

	// Extract ONLY relevant S3/storage metadata from headers (not auth, cookies, etc.)
	storageMetadata := make(map[string]string)
	userMetadata := make(map[string]string)

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

	// Store object in storage backend (ONLY storage metadata, not HTTP headers)
	if err := om.storage.Put(ctx, objectPath, data, storageMetadata); err != nil {
		return nil, fmt.Errorf("failed to store object: %w", err)
	}

	// Get object metadata from storage to get size and etag
	finalStorageMetadata, err := om.storage.GetMetadata(ctx, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get object metadata: %w", err)
	}

	// Create object info
	size, _ := strconv.ParseInt(finalStorageMetadata["size"], 10, 64)
	lastModified, _ := strconv.ParseInt(finalStorageMetadata["last_modified"], 10, 64)

	object := &Object{
		Key:          key,
		Bucket:       bucket,
		Size:         size,
		LastModified: time.Unix(lastModified, 0),
		ETag:         finalStorageMetadata["etag"],
		ContentType:  finalStorageMetadata["content-type"],
		Metadata:     userMetadata, // User metadata from x-amz-meta-* headers
		StorageClass: StorageClassStandard,
	}

	// Apply default Object Lock retention if bucket has it configured
	if err := om.applyDefaultRetention(ctx, object); err != nil {
		logrus.WithError(err).Debug("Failed to apply default retention")
	}

	// Save object metadata to BadgerDB
	metaObj := toMetadataObject(object)
	if err := om.metadataStore.PutObject(ctx, metaObj); err != nil {
		logrus.WithError(err).Warn("Failed to save object metadata to BadgerDB")
	}

	// Update bucket metrics (increment object count)
	if om.bucketManager != nil {
		// Parse bucket path to extract tenantID and bucket name
		tenantID, bucketName := om.parseBucketPath(bucket)
		if err := om.bucketManager.IncrementObjectCount(ctx, tenantID, bucketName, size); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"bucket_path": bucket,
				"tenant_id":   tenantID,
				"bucket_name": bucketName,
				"key":         key,
				"size":        size,
			}).Warn("Failed to increment bucket object count")
		}
	}

	return object, nil
}

// DeleteObject deletes an object
func (om *objectManager) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := om.validateObjectName(key); err != nil {
		return err
	}

	objectPath := om.getObjectPath(bucket, key)

	// Check if object exists and get its size before deleting
	storageMetadata, err := om.storage.GetMetadata(ctx, objectPath)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			return ErrObjectNotFound
		}
		return fmt.Errorf("failed to get object metadata: %w", err)
	}
	objectSize, _ := strconv.ParseInt(storageMetadata["size"], 10, 64)

	// Check Object Lock - Legal Hold
	legalHold, err := om.GetObjectLegalHold(ctx, bucket, key)
	if err == nil && legalHold != nil && legalHold.Status == LegalHoldStatusOn {
		return ErrObjectUnderLegalHold
	}

	// Check Object Lock - Retention
	// Checking Object Lock retention
	retention, err := om.GetObjectRetention(ctx, bucket, key)
	if err != nil {
		// GetObjectRetention error - object might not have retention
	} else if retention != nil {
		// Found retention configuration
		// Check if retention is still active
		if time.Now().Before(retention.RetainUntilDate) {
			// COMPLIANCE mode cannot be bypassed
			if retention.Mode == RetentionModeCompliance {
				return NewComplianceRetentionError(retention.RetainUntilDate)
			}
			// GOVERNANCE mode requires bypass permission (handled at API layer)
			if retention.Mode == RetentionModeGovernance {
				return NewGovernanceRetentionError(retention.RetainUntilDate)
			}
		}
	}

	// Delete object from storage
	if err := om.storage.Delete(ctx, objectPath); err != nil {
		if err == storage.ErrObjectNotFound {
			return ErrObjectNotFound
		}
		return fmt.Errorf("failed to delete object: %w", err)
	}

	// Delete object metadata from BadgerDB
	if err := om.metadataStore.DeleteObject(ctx, bucket, key); err != nil && err != metadata.ErrObjectNotFound {
		// Log the error but don't fail the operation since the object itself was deleted
		logrus.WithError(err).WithFields(logrus.Fields{
			"bucket": bucket,
			"key":    key,
		}).Warn("Failed to delete object metadata from BadgerDB")
	}

	// Update bucket metrics (decrement object count)
	if om.bucketManager != nil {
		// Parse bucket path to extract tenantID and bucket name
		tenantID, bucketName := om.parseBucketPath(bucket)
		if err := om.bucketManager.DecrementObjectCount(ctx, tenantID, bucketName, objectSize); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"bucket_path": bucket,
				"tenant_id":   tenantID,
				"bucket_name": bucketName,
				"key":         key,
				"size":        objectSize,
			}).Warn("Failed to decrement bucket object count")
		}
	}

	return nil
}

// ListObjects lists objects in a bucket
func (om *objectManager) ListObjects(ctx context.Context, bucket, prefix, delimiter, marker string, maxKeys int) (*ListObjectsResult, error) {
	if maxKeys <= 0 {
		maxKeys = 1000 // Default max keys
	}

	// List objects from storage
	bucketPrefix := bucket + "/"
	if prefix != "" {
		bucketPrefix = bucket + "/" + prefix
	}

	storageObjects, err := om.storage.List(ctx, bucketPrefix, true)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	var objects []Object
	commonPrefixesMap := make(map[string]bool) // Use map to avoid duplicates

	for _, storageObj := range storageObjects {
		// Extract object key from path
		if !strings.HasPrefix(storageObj.Path, bucket+"/") {
			continue
		}

		key := strings.TrimPrefix(storageObj.Path, bucket+"/")
		if key == "" {
			continue
		}

		// Skip bucket marker files
		if strings.HasSuffix(key, ".maxiofs-bucket") {
			continue
		}

		// Skip metadata files
		if strings.Contains(key, ".maxiofs/objects/") {
			continue
		}

		// Skip bucket configuration files and internal marker files
		// Check both if the key starts with .maxiofs- OR if the filename (last part) starts with .maxiofs-
		if strings.HasPrefix(key, ".maxiofs-") {
			continue
		}

		// Check if the filename (last segment) starts with .maxiofs-
		lastSlashIndex := strings.LastIndex(key, "/")
		if lastSlashIndex >= 0 {
			filename := key[lastSlashIndex+1:]
			if strings.HasPrefix(filename, ".maxiofs-") {
				continue
			}
		}

		// Apply prefix filter
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}

		// Apply marker filter
		if marker != "" && key <= marker {
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

		// Try to load extended metadata from BadgerDB
		metaObj, err := om.metadataStore.GetObject(ctx, bucket, key)
		if err == nil && metaObj != nil {
			// Use metadata from BadgerDB
			objects = append(objects, *fromMetadataObject(metaObj))
		} else {
			// Fallback: Create basic object info from storage metadata
			object := Object{
				Key:          key,
				Bucket:       bucket,
				Size:         storageObj.Size,
				LastModified: time.Unix(storageObj.LastModified, 0),
				ETag:         storageObj.ETag,
				StorageClass: StorageClassStandard,
				Metadata:     make(map[string]string),
			}
			objects = append(objects, object)
		}
	}

	// Convert commonPrefixesMap to slice
	var commonPrefixes []CommonPrefix
	for prefix := range commonPrefixesMap {
		commonPrefixes = append(commonPrefixes, CommonPrefix{Prefix: prefix})
	}

	// Sort common prefixes
	sort.Slice(commonPrefixes, func(i, j int) bool {
		return commonPrefixes[i].Prefix < commonPrefixes[j].Prefix
	})

	// Sort objects by key
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].Key < objects[j].Key
	})

	// Calculate total items (objects + prefixes) for truncation
	totalItems := len(objects) + len(commonPrefixes)
	isTruncated := false
	nextMarker := ""

	// Apply maxKeys limit
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
	// TODO: Implement versioning in Fase 7.2
	return []ObjectVersion{}, nil
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

// ACL operations implementations (basic placeholders)

func (om *objectManager) GetObjectACL(ctx context.Context, bucket, key string) (*ACL, error) {
	// Get object metadata to ensure it exists
	_, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	// Return default ACL (owner has full control)
	// TODO: Implement actual ACL storage and retrieval
	return &ACL{
		Owner: Owner{
			ID:          "maxiofs",
			DisplayName: "MaxIOFS",
		},
		Grants: []Grant{
			{
				Grantee: Grantee{
					Type:        "CanonicalUser",
					ID:          "maxiofs",
					DisplayName: "MaxIOFS",
				},
				Permission: "FULL_CONTROL",
			},
		},
	}, nil
}

func (om *objectManager) SetObjectACL(ctx context.Context, bucket, key string, acl *ACL) error {
	// Get object metadata to ensure it exists
	_, err := om.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return err
	}

	// TODO: Implement actual ACL storage
	// For now, just validate that object exists (done above)
	return nil
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

	// Validate parts list
	if len(parts) == 0 {
		return nil, fmt.Errorf("no parts provided")
	}

	// Sort parts by part number
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	// Validate part numbers are consecutive starting from 1
	for i, part := range parts {
		if part.PartNumber != i+1 {
			return nil, fmt.Errorf("part numbers must be consecutive starting from 1")
		}

		// Validate part exists in storage
		partPath := om.getMultipartPartPath(uploadID, part.PartNumber)
		exists, err := om.storage.Exists(ctx, partPath)
		if err != nil {
			return nil, fmt.Errorf("failed to check part %d existence: %w", part.PartNumber, err)
		}
		if !exists {
			return nil, fmt.Errorf("part %d not found", part.PartNumber)
		}
	}

	// Combine parts into final object
	objectPath := om.getObjectPath(multipart.Bucket, multipart.Key)
	if err := om.combineMultipartParts(ctx, uploadID, parts, objectPath); err != nil {
		return nil, fmt.Errorf("failed to combine parts: %w", err)
	}

	// Get final object metadata
	storageMetadata, err := om.storage.GetMetadata(ctx, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get final object metadata: %w", err)
	}

	size, _ := strconv.ParseInt(storageMetadata["size"], 10, 64)
	lastModified, _ := strconv.ParseInt(storageMetadata["last_modified"], 10, 64)

	// Create final object
	object := &Object{
		Key:          multipart.Key,
		Bucket:       multipart.Bucket,
		Size:         size,
		LastModified: time.Unix(lastModified, 0),
		ETag:         storageMetadata["etag"],
		ContentType:  multipart.Metadata["content-type"],
		Metadata:     multipart.Metadata,
		StorageClass: multipart.StorageClass,
	}

	// Save object metadata to BadgerDB
	metaObj := toMetadataObject(object)
	if err := om.metadataStore.PutObject(ctx, metaObj); err != nil {
		logrus.WithError(err).Warn("Failed to save final object metadata to BadgerDB")
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
func (om *objectManager) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, headers http.Header) (*Object, error) {
	panic("not implemented - Fase 4.1")
}

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
	// For simplicity, we'll store a reference to the parts and combine them on read
	// In a production implementation, you might want to actually concatenate the files

	// Create a combined metadata that references all parts
	combinedMetadata := map[string]string{
		"content-type":        "application/octet-stream",
		"multipart-upload-id": uploadID,
		"parts-count":         strconv.Itoa(len(parts)),
	}

	// For now, we'll use the first part as the base and create a reference
	if len(parts) > 0 {
		firstPartPath := om.getMultipartPartPath(uploadID, parts[0].PartNumber)
		reader, metadata, err := om.storage.Get(ctx, firstPartPath)
		if err != nil {
			return err
		}
		defer reader.Close()

		// Copy first part content type if available
		if contentType, exists := metadata["content-type"]; exists {
			combinedMetadata["content-type"] = contentType
		}

		// For MVP, just copy the first part as the final object
		// TODO: Implement proper part concatenation
		return om.storage.Put(ctx, finalPath, reader, combinedMetadata)
	}

	return fmt.Errorf("no parts to combine")
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
