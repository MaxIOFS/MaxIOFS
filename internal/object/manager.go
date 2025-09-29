package object

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"crypto/rand"
	"encoding/hex"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/storage"
)

// Manager defines the interface for object management
type Manager interface {
	// Basic object operations
	GetObject(ctx context.Context, bucket, key string) (*Object, io.ReadCloser, error)
	PutObject(ctx context.Context, bucket, key string, data io.Reader, headers http.Header) (*Object, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	ListObjects(ctx context.Context, bucket, prefix, delimiter, marker string, maxKeys int) ([]Object, bool, error)

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
	Retention  *RetentionConfig  `json:"retention,omitempty"`
	LegalHold  *LegalHoldConfig  `json:"legal_hold,omitempty"`

	// Tagging
	Tags *TagSet `json:"tags,omitempty"`

	// ACL
	ACL *ACL `json:"acl,omitempty"`
}

// objectManager implements the Manager interface
type objectManager struct {
	storage storage.Backend
	config  config.StorageConfig
}

// NewManager creates a new object manager
func NewManager(storage storage.Backend, config config.StorageConfig) Manager {
	return &objectManager{
		storage: storage,
		config:  config,
	}
}

// GetObject retrieves an object
func (om *objectManager) GetObject(ctx context.Context, bucket, key string) (*Object, io.ReadCloser, error) {
	if err := om.validateObjectName(key); err != nil {
		return nil, nil, err
	}

	objectPath := om.getObjectPath(bucket, key)

	// Get object data
	reader, metadata, err := om.storage.Get(ctx, objectPath)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			return nil, nil, ErrObjectNotFound
		}
		return nil, nil, fmt.Errorf("failed to get object: %w", err)
	}

	// Load object metadata
	object, err := om.loadObjectMetadata(ctx, bucket, key)
	if err != nil {
		// If metadata doesn't exist, create basic object info from storage metadata
		size, _ := strconv.ParseInt(metadata["size"], 10, 64)
		lastModified, _ := strconv.ParseInt(metadata["last_modified"], 10, 64)

		object = &Object{
			Key:          key,
			Bucket:       bucket,
			Size:         size,
			LastModified: time.Unix(lastModified, 0),
			ETag:         metadata["etag"],
			ContentType:  metadata["content-type"],
			Metadata:     metadata,
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

	// Extract metadata from headers
	metadata := make(map[string]string)
	for headerKey, values := range headers {
		if len(values) > 0 {
			// Convert header keys to lowercase for consistency
			key := strings.ToLower(headerKey)
			metadata[key] = values[0]
		}
	}

	// Set default content type if not provided
	if _, exists := metadata["content-type"]; !exists {
		metadata["content-type"] = "application/octet-stream"
	}

	// Store object in storage backend
	if err := om.storage.Put(ctx, objectPath, data, metadata); err != nil {
		return nil, fmt.Errorf("failed to store object: %w", err)
	}

	// Get object metadata from storage to get size and etag
	storageMetadata, err := om.storage.GetMetadata(ctx, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get object metadata: %w", err)
	}

	// Create object info
	size, _ := strconv.ParseInt(storageMetadata["size"], 10, 64)
	lastModified, _ := strconv.ParseInt(storageMetadata["last_modified"], 10, 64)

	object := &Object{
		Key:          key,
		Bucket:       bucket,
		Size:         size,
		LastModified: time.Unix(lastModified, 0),
		ETag:         storageMetadata["etag"],
		ContentType:  metadata["content-type"],
		Metadata:     metadata,
		StorageClass: StorageClassStandard,
	}

	// Save object metadata
	if err := om.saveObjectMetadata(ctx, object); err != nil {
		// Log error but don't fail the operation
		// Object is still stored in storage backend
	}

	return object, nil
}

// DeleteObject deletes an object
func (om *objectManager) DeleteObject(ctx context.Context, bucket, key string) error {
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

	// Delete object from storage
	if err := om.storage.Delete(ctx, objectPath); err != nil {
		if err == storage.ErrObjectNotFound {
			return ErrObjectNotFound
		}
		return fmt.Errorf("failed to delete object: %w", err)
	}

	// Delete object metadata
	metadataPath := om.getObjectMetadataPath(bucket, key)
	om.storage.Delete(ctx, metadataPath) // Ignore errors for metadata deletion

	return nil
}

// ListObjects lists objects in a bucket
func (om *objectManager) ListObjects(ctx context.Context, bucket, prefix, delimiter, marker string, maxKeys int) ([]Object, bool, error) {
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
		return nil, false, fmt.Errorf("failed to list objects: %w", err)
	}

	var objects []Object
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

		// Apply prefix filter
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}

		// Apply marker filter
		if marker != "" && key <= marker {
			continue
		}

		// Handle delimiter (common prefixes)
		if delimiter != "" {
			delimiterIndex := strings.Index(key[len(prefix):], delimiter)
			if delimiterIndex >= 0 {
				// This is a common prefix, skip for now
				// TODO: Implement common prefixes handling
				continue
			}
		}

		// Create object info
		object := Object{
			Key:          key,
			Bucket:       bucket,
			Size:         storageObj.Size,
			LastModified: time.Unix(storageObj.LastModified, 0),
			ETag:         storageObj.ETag,
			StorageClass: StorageClassStandard,
			Metadata:     make(map[string]string),
		}

		// Try to load extended metadata
		if objectMeta, err := om.loadObjectMetadata(ctx, bucket, key); err == nil {
			object.ContentType = objectMeta.ContentType
			object.Metadata = objectMeta.Metadata
			object.StorageClass = objectMeta.StorageClass
		}

		objects = append(objects, object)
	}

	// Sort objects by key
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].Key < objects[j].Key
	})

	// Apply maxKeys limit
	isTruncated := false
	if len(objects) > maxKeys {
		objects = objects[:maxKeys]
		isTruncated = true
	}

	return objects, isTruncated, nil
}

// GetObjectMetadata retrieves object metadata
func (om *objectManager) GetObjectMetadata(ctx context.Context, bucket, key string) (*Object, error) {
	if err := om.validateObjectName(key); err != nil {
		return nil, err
	}

	objectPath := om.getObjectPath(bucket, key)

	// Check if object exists
	exists, err := om.storage.Exists(ctx, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check object existence: %w", err)
	}
	if !exists {
		return nil, ErrObjectNotFound
	}

	// Get storage metadata
	storageMetadata, err := om.storage.GetMetadata(ctx, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage metadata: %w", err)
	}

	// Try to load extended metadata
	object, err := om.loadObjectMetadata(ctx, bucket, key)
	if err != nil {
		// Create basic object info from storage metadata
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

	// Load current object metadata
	object, err := om.loadObjectMetadata(ctx, bucket, key)
	if err != nil {
		// Create new object metadata if it doesn't exist
		storageMetadata, err := om.storage.GetMetadata(ctx, objectPath)
		if err != nil {
			return fmt.Errorf("failed to get storage metadata: %w", err)
		}

		size, _ := strconv.ParseInt(storageMetadata["size"], 10, 64)
		lastModified, _ := strconv.ParseInt(storageMetadata["last_modified"], 10, 64)

		object = &Object{
			Key:          key,
			Bucket:       bucket,
			Size:         size,
			LastModified: time.Unix(lastModified, 0),
			ETag:         storageMetadata["etag"],
			ContentType:  metadata["content-type"],
			StorageClass: StorageClassStandard,
		}
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

	// Save updated metadata
	return om.saveObjectMetadata(ctx, object)
}

// Placeholder implementations for Object Lock operations
func (om *objectManager) GetObjectRetention(ctx context.Context, bucket, key string) (*RetentionConfig, error) {
	panic("not implemented - Fase 2.1")
}

func (om *objectManager) SetObjectRetention(ctx context.Context, bucket, key string, config *RetentionConfig) error {
	panic("not implemented - Fase 2.1")
}

func (om *objectManager) GetObjectLegalHold(ctx context.Context, bucket, key string) (*LegalHoldConfig, error) {
	panic("not implemented - Fase 2.1")
}

func (om *objectManager) SetObjectLegalHold(ctx context.Context, bucket, key string, config *LegalHoldConfig) error {
	panic("not implemented - Fase 2.1")
}

// Placeholder implementations for versioning operations
func (om *objectManager) GetObjectVersions(ctx context.Context, bucket, key string) ([]ObjectVersion, error) {
	panic("not implemented - Fase 7.2")
}

func (om *objectManager) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	panic("not implemented - Fase 7.2")
}

// Placeholder implementations for tagging operations
func (om *objectManager) GetObjectTagging(ctx context.Context, bucket, key string) (*TagSet, error) {
	panic("not implemented - Fase 4.1")
}

func (om *objectManager) SetObjectTagging(ctx context.Context, bucket, key string, tags *TagSet) error {
	panic("not implemented - Fase 4.1")
}

func (om *objectManager) DeleteObjectTagging(ctx context.Context, bucket, key string) error {
	panic("not implemented - Fase 4.1")
}

// Placeholder implementations for ACL operations
func (om *objectManager) GetObjectACL(ctx context.Context, bucket, key string) (*ACL, error) {
	panic("not implemented - Fase 4.1")
}

func (om *objectManager) SetObjectACL(ctx context.Context, bucket, key string, acl *ACL) error {
	panic("not implemented - Fase 4.1")
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

	// Save multipart upload metadata
	if err := om.saveMultipartUpload(ctx, multipart); err != nil {
		return nil, fmt.Errorf("failed to save multipart upload: %w", err)
	}

	return multipart, nil
}

func (om *objectManager) UploadPart(ctx context.Context, uploadID string, partNumber int, data io.Reader) (*Part, error) {
	if partNumber < 1 || partNumber > 10000 {
		return nil, fmt.Errorf("part number must be between 1 and 10000")
	}

	// Load multipart upload metadata
	multipart, err := om.loadMultipartUpload(ctx, uploadID)
	if err != nil {
		return nil, err
	}

	// Create part path
	partPath := om.getMultipartPartPath(uploadID, partNumber)

	// Store part data
	partMetadata := map[string]string{
		"upload-id":    uploadID,
		"part-number": strconv.Itoa(partNumber),
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

	part := &Part{
		PartNumber:   partNumber,
		ETag:         storageMetadata["etag"],
		Size:         size,
		LastModified: time.Unix(lastModified, 0),
	}

	// Update multipart upload parts list
	multipart.Parts = om.updatePartsList(multipart.Parts, *part)
	if err := om.saveMultipartUpload(ctx, multipart); err != nil {
		return nil, fmt.Errorf("failed to update multipart upload: %w", err)
	}

	return part, nil
}

func (om *objectManager) ListParts(ctx context.Context, uploadID string) ([]Part, error) {
	// Load multipart upload metadata
	multipart, err := om.loadMultipartUpload(ctx, uploadID)
	if err != nil {
		return nil, err
	}

	// Sort parts by part number
	sort.Slice(multipart.Parts, func(i, j int) bool {
		return multipart.Parts[i].PartNumber < multipart.Parts[j].PartNumber
	})

	return multipart.Parts, nil
}

func (om *objectManager) CompleteMultipartUpload(ctx context.Context, uploadID string, parts []Part) (*Object, error) {
	// Load multipart upload metadata
	multipart, err := om.loadMultipartUpload(ctx, uploadID)
	if err != nil {
		return nil, err
	}

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

	// Save object metadata
	if err := om.saveObjectMetadata(ctx, object); err != nil {
		// Log error but don't fail the operation
	}

	// Clean up multipart upload
	om.abortMultipartUpload(ctx, uploadID, false) // Don't return error

	return object, nil
}

func (om *objectManager) AbortMultipartUpload(ctx context.Context, uploadID string) error {
	return om.abortMultipartUpload(ctx, uploadID, true)
}

func (om *objectManager) ListMultipartUploads(ctx context.Context, bucket string) ([]MultipartUpload, error) {
	// List all multipart upload metadata files
	prefix := ".maxiofs/multipart/uploads/"
	objects, err := om.storage.List(ctx, prefix, true)
	if err != nil {
		return nil, fmt.Errorf("failed to list multipart uploads: %w", err)
	}

	var uploads []MultipartUpload
	for _, obj := range objects {
		if strings.HasSuffix(obj.Path, ".json") {
			// Extract upload ID from path
			parts := strings.Split(obj.Path, "/")
			if len(parts) >= 1 {
				uploadID := strings.TrimSuffix(parts[len(parts)-1], ".json")

				// Load multipart upload
				multipart, err := om.loadMultipartUpload(ctx, uploadID)
				if err == nil && multipart.Bucket == bucket {
					uploads = append(uploads, *multipart)
				}
			}
		}
	}

	// Sort by initiated time
	sort.Slice(uploads, func(i, j int) bool {
		return uploads[i].Initiated.Before(uploads[j].Initiated)
	})

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

// getObjectMetadataPath returns the path for object metadata
func (om *objectManager) getObjectMetadataPath(bucket, key string) string {
	// Create a hash of the key to avoid filesystem issues with special characters
	hash := fmt.Sprintf("%x", md5.Sum([]byte(key)))
	return fmt.Sprintf(".maxiofs/objects/%s/%s.json", bucket, hash)
}

// saveObjectMetadata saves object metadata to storage
func (om *objectManager) saveObjectMetadata(ctx context.Context, object *Object) error {
	data, err := json.Marshal(object)
	if err != nil {
		return fmt.Errorf("failed to marshal object metadata: %w", err)
	}

	metadataPath := om.getObjectMetadataPath(object.Bucket, object.Key)
	return om.storage.Put(ctx, metadataPath, strings.NewReader(string(data)), map[string]string{
		"content-type": "application/json",
	})
}

// loadObjectMetadata loads object metadata from storage
func (om *objectManager) loadObjectMetadata(ctx context.Context, bucket, key string) (*Object, error) {
	metadataPath := om.getObjectMetadataPath(bucket, key)

	reader, _, err := om.storage.Get(ctx, metadataPath)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			return nil, ErrObjectNotFound
		}
		return nil, fmt.Errorf("failed to load object metadata: %w", err)
	}
	defer reader.Close()

	var object Object
	if err := json.NewDecoder(reader).Decode(&object); err != nil {
		return nil, fmt.Errorf("failed to decode object metadata: %w", err)
	}

	return &object, nil
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

// getMultipartUploadPath returns the path for multipart upload metadata
func (om *objectManager) getMultipartUploadPath(uploadID string) string {
	return fmt.Sprintf(".maxiofs/multipart/uploads/%s.json", uploadID)
}

// getMultipartPartPath returns the path for a multipart part
func (om *objectManager) getMultipartPartPath(uploadID string, partNumber int) string {
	return fmt.Sprintf(".maxiofs/multipart/parts/%s/%05d", uploadID, partNumber)
}

// saveMultipartUpload saves multipart upload metadata to storage
func (om *objectManager) saveMultipartUpload(ctx context.Context, multipart *MultipartUpload) error {
	data, err := json.Marshal(multipart)
	if err != nil {
		return fmt.Errorf("failed to marshal multipart upload metadata: %w", err)
	}

	metadataPath := om.getMultipartUploadPath(multipart.UploadID)
	return om.storage.Put(ctx, metadataPath, strings.NewReader(string(data)), map[string]string{
		"content-type": "application/json",
	})
}

// loadMultipartUpload loads multipart upload metadata from storage
func (om *objectManager) loadMultipartUpload(ctx context.Context, uploadID string) (*MultipartUpload, error) {
	metadataPath := om.getMultipartUploadPath(uploadID)

	reader, _, err := om.storage.Get(ctx, metadataPath)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			return nil, ErrInvalidUploadID
		}
		return nil, fmt.Errorf("failed to load multipart upload metadata: %w", err)
	}
	defer reader.Close()

	var multipart MultipartUpload
	if err := json.NewDecoder(reader).Decode(&multipart); err != nil {
		return nil, fmt.Errorf("failed to decode multipart upload metadata: %w", err)
	}

	return &multipart, nil
}

// updatePartsList updates the parts list with a new part
func (om *objectManager) updatePartsList(parts []Part, newPart Part) []Part {
	// Find existing part with same number and replace, or add new
	for i, part := range parts {
		if part.PartNumber == newPart.PartNumber {
			parts[i] = newPart
			return parts
		}
	}
	// Add new part
	return append(parts, newPart)
}

// combineMultipartParts combines all parts into the final object
func (om *objectManager) combineMultipartParts(ctx context.Context, uploadID string, parts []Part, finalPath string) error {
	// For simplicity, we'll store a reference to the parts and combine them on read
	// In a production implementation, you might want to actually concatenate the files

	// Create a combined metadata that references all parts
	combinedMetadata := map[string]string{
		"content-type": "application/octet-stream",
		"multipart-upload-id": uploadID,
		"parts-count": strconv.Itoa(len(parts)),
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
	// Load multipart upload to get parts list
	multipart, err := om.loadMultipartUpload(ctx, uploadID)
	if err != nil {
		if returnError {
			return err
		}
		return nil
	}

	// Delete all parts
	for _, part := range multipart.Parts {
		partPath := om.getMultipartPartPath(uploadID, part.PartNumber)
		om.storage.Delete(ctx, partPath) // Ignore errors
	}

	// Delete multipart upload metadata
	metadataPath := om.getMultipartUploadPath(uploadID)
	err = om.storage.Delete(ctx, metadataPath)
	if err != nil && err != storage.ErrObjectNotFound && returnError {
		return fmt.Errorf("failed to delete multipart upload metadata: %w", err)
	}

	return nil
}