package object

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// Helper function to convert int to *int
func intPtr(i int) *int {
	return &i
}

func setupObjectIntegrationTest(t *testing.T) (Manager, bucket.Manager, func()) {
	// Create temporary directory for test data
	tempDir, err := os.MkdirTemp("", "maxiofs-object-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Initialize storage backend
	storageBackend, err := storage.NewFilesystemBackend(storage.Config{Root: tempDir})
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create storage backend: %v", err)
	}

	// Initialize BadgerDB metadata store
	dbPath := filepath.Join(tempDir, "metadata")
	metadataStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           dbPath,
		SyncWrites:        true,
		CompactionEnabled: false,
		Logger:            logrus.StandardLogger(),
	})
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create metadata store: %v", err)
	}

	// Create bucket manager
	bucketManager := bucket.NewManager(storageBackend, metadataStore)

	// Create object manager
	cfg := config.StorageConfig{
		Backend: "filesystem",
		Root:    tempDir,
	}
	objectManager := NewManager(storageBackend, metadataStore, cfg)

	// Connect object manager to bucket manager
	if om, ok := objectManager.(interface {
		SetBucketManager(interface {
			IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
			DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
		})
	}); ok {
		om.SetBucketManager(bucketManager)
	}

	// Cleanup function
	cleanup := func() {
		metadataStore.Close()
		os.RemoveAll(tempDir)
	}

	return objectManager, bucketManager, cleanup
}

func TestObjectManagerBasicOperations(t *testing.T) {
	om, bm, cleanup := setupObjectIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()
	tenantID := "test-tenant"
	bucketName := "test-bucket"
	bucketPath := tenantID + "/" + bucketName // ObjectManager expects tenant/bucket format
	objectKey := "test-object.txt"

	// Create bucket first
	err := bm.CreateBucket(ctx, tenantID, bucketName, "")
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}

	t.Run("PutObject", func(t *testing.T) {
		content := []byte("Hello, MaxIOFS!")
		headers := http.Header{}
		headers.Set("Content-Type", "text/plain")
		headers.Set("X-Amz-Meta-Author", "test-user")
		headers.Set("X-Amz-Meta-Department", "engineering")

		obj, err := om.PutObject(ctx, bucketPath, objectKey, bytes.NewReader(content), headers)
		if err != nil {
			t.Fatalf("Failed to put object: %v", err)
		}

		if obj.Key != objectKey {
			t.Errorf("Expected key %s, got %s", objectKey, obj.Key)
		}
		if obj.Size != int64(len(content)) {
			t.Errorf("Expected size %d, got %d", len(content), obj.Size)
		}
		if obj.ContentType != "text/plain" {
			t.Errorf("Expected content type text/plain, got %s", obj.ContentType)
		}
		if obj.Metadata["author"] != "test-user" {
			t.Errorf("Expected author metadata test-user, got %s", obj.Metadata["author"])
		}
	})

	t.Run("GetObject", func(t *testing.T) {
		obj, reader, err := om.GetObject(ctx, bucketPath, objectKey)
		if err != nil {
			t.Fatalf("Failed to get object: %v", err)
		}
		defer reader.Close()

		// Read content
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("Failed to read object content: %v", err)
		}

		expectedContent := "Hello, MaxIOFS!"
		if string(content) != expectedContent {
			t.Errorf("Expected content %s, got %s", expectedContent, string(content))
		}

		if obj.ContentType != "text/plain" {
			t.Errorf("Expected content type text/plain, got %s", obj.ContentType)
		}
	})

	t.Run("GetObjectMetadata", func(t *testing.T) {
		obj, err := om.GetObjectMetadata(ctx, bucketPath, objectKey)
		if err != nil {
			t.Fatalf("Failed to get object metadata: %v", err)
		}

		if obj.Key != objectKey {
			t.Errorf("Expected key %s, got %s", objectKey, obj.Key)
		}
		if obj.Metadata["author"] != "test-user" {
			t.Errorf("Expected author metadata, got %v", obj.Metadata)
		}
	})

	t.Run("UpdateObjectMetadata", func(t *testing.T) {
		newMetadata := map[string]string{
			"version": "2.0",
			"status":  "updated",
		}

		err := om.UpdateObjectMetadata(ctx, bucketPath, objectKey, newMetadata)
		if err != nil {
			t.Fatalf("Failed to update object metadata: %v", err)
		}

		// Verify update
		obj, err := om.GetObjectMetadata(ctx, bucketPath, objectKey)
		if err != nil {
			t.Fatalf("Failed to get updated metadata: %v", err)
		}

		if obj.Metadata["version"] != "2.0" {
			t.Errorf("Expected updated version metadata, got %v", obj.Metadata)
		}
	})

	t.Run("ListObjects", func(t *testing.T) {
		// Put additional objects
		for i := 1; i <= 5; i++ {
			key := "file-" + string(rune('0'+i)) + ".txt"
			content := []byte("Content " + string(rune('0'+i)))
			headers := http.Header{}
			headers.Set("Content-Type", "text/plain")

			_, err := om.PutObject(ctx, bucketPath, key, bytes.NewReader(content), headers)
			if err != nil {
				t.Fatalf("Failed to put object %s: %v", key, err)
			}
		}

		// List all objects
		result, err := om.ListObjects(ctx, bucketPath, "", "", "", 1000)
		if err != nil {
			t.Fatalf("Failed to list objects: %v", err)
		}

		// Should have 6 objects total (1 original + 5 new)
		if len(result.Objects) < 6 {
			t.Errorf("Expected at least 6 objects, got %d", len(result.Objects))
		}
	})

	t.Run("ListObjectsWithPrefix", func(t *testing.T) {
		// Create objects with specific prefix
		for i := 1; i <= 3; i++ {
			key := fmt.Sprintf("prefix-test-%d.txt", i)
			content := []byte(fmt.Sprintf("Content %d", i))
			headers := http.Header{}
			headers.Set("Content-Type", "text/plain")
			om.PutObject(ctx, bucketPath, key, bytes.NewReader(content), headers)
		}

		// List all objects first to debug
		allResult, _ := om.ListObjects(ctx, bucketPath, "", "", "", 1000)
		t.Logf("Total objects in bucket: %d", len(allResult.Objects))
		for _, obj := range allResult.Objects {
			t.Logf("  - %s", obj.Key)
		}

		result, err := om.ListObjects(ctx, bucketPath, "prefix-test-", "", "", 1000)
		if err != nil {
			t.Fatalf("Failed to list objects with prefix: %v", err)
		}

		t.Logf("Objects with prefix 'prefix-test-': %d", len(result.Objects))
		if len(result.Objects) < 3 {
			t.Errorf("Expected at least 3 objects with prefix 'prefix-test-', got %d", len(result.Objects))
		}
	})

	t.Run("DeleteObject", func(t *testing.T) {
		_, err := om.DeleteObject(ctx, bucketPath, objectKey, false)
		if err != nil {
			t.Fatalf("Failed to delete object: %v", err)
		}

		// Verify object is deleted
		_, _, err = om.GetObject(ctx, bucketPath, objectKey)
		if err == nil {
			t.Error("Expected error when getting deleted object")
		}
		if err != ErrObjectNotFound {
			t.Errorf("Expected ErrObjectNotFound, got %v", err)
		}
	})
}

func TestObjectManagerTagging(t *testing.T) {
	om, bm, cleanup := setupObjectIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()
	bucketName := "tagging-bucket"
	objectKey := "tagged-object.txt"

	// Create bucket
	err := bm.CreateBucket(ctx, "", bucketName, "")
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}

	// Put object
	content := []byte("Tagged content")
	headers := http.Header{}
	_, err = om.PutObject(ctx, bucketName, objectKey, bytes.NewReader(content), headers)
	if err != nil {
		t.Fatalf("Failed to put object: %v", err)
	}

	t.Run("SetObjectTagging", func(t *testing.T) {
		tags := &TagSet{
			Tags: []Tag{
				{Key: "Environment", Value: "Production"},
				{Key: "Application", Value: "MaxIOFS"},
				{Key: "Owner", Value: "DevOps"},
			},
		}

		err := om.SetObjectTagging(ctx, bucketName, objectKey, tags)
		if err != nil {
			t.Fatalf("Failed to set object tags: %v", err)
		}
	})

	t.Run("GetObjectTagging", func(t *testing.T) {
		tags, err := om.GetObjectTagging(ctx, bucketName, objectKey)
		if err != nil {
			t.Fatalf("Failed to get object tags: %v", err)
		}

		if len(tags.Tags) != 3 {
			t.Fatalf("Expected 3 tags, got %d", len(tags.Tags))
		}

		// Verify specific tag
		found := false
		for _, tag := range tags.Tags {
			if tag.Key == "Environment" && tag.Value == "Production" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected Environment=Production tag not found")
		}
	})

	t.Run("DeleteObjectTagging", func(t *testing.T) {
		err := om.DeleteObjectTagging(ctx, bucketName, objectKey)
		if err != nil {
			t.Fatalf("Failed to delete object tags: %v", err)
		}

		// Verify tags are deleted
		tags, err := om.GetObjectTagging(ctx, bucketName, objectKey)
		if err != nil {
			t.Fatalf("Failed to get tags after deletion: %v", err)
		}

		if len(tags.Tags) != 0 {
			t.Errorf("Expected 0 tags after deletion, got %d", len(tags.Tags))
		}
	})
}

func TestObjectManagerObjectLock(t *testing.T) {
	om, bm, cleanup := setupObjectIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()
	bucketName := "lock-bucket"
	objectKey := "locked-object.txt"

	// Create bucket with Object Lock enabled
	err := bm.CreateBucket(ctx, "", bucketName, "")
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}

	// Enable Object Lock on bucket
	objectLockConfig := &bucket.ObjectLockConfig{
		ObjectLockEnabled: true,
		Rule: &bucket.ObjectLockRule{
			DefaultRetention: &bucket.DefaultRetention{
				Mode: "GOVERNANCE",
				Days: intPtr(7),
			},
		},
	}
	err = bm.SetObjectLockConfig(ctx, "", bucketName, objectLockConfig)
	if err != nil {
		t.Fatalf("Failed to set object lock config: %v", err)
	}

	// Put object
	content := []byte("Locked content")
	headers := http.Header{}
	obj, err := om.PutObject(ctx, bucketName, objectKey, bytes.NewReader(content), headers)
	if err != nil {
		t.Fatalf("Failed to put object: %v", err)
	}

	// Verify default retention was applied
	if obj.Retention == nil {
		t.Error("Expected default retention to be applied")
	} else {
		if obj.Retention.Mode != "GOVERNANCE" {
			t.Errorf("Expected retention mode GOVERNANCE, got %s", obj.Retention.Mode)
		}
	}

	t.Run("SetObjectRetention", func(t *testing.T) {
		retainUntil := time.Now().Add(30 * 24 * time.Hour) // 30 days
		retention := &RetentionConfig{
			Mode:            "COMPLIANCE",
			RetainUntilDate: retainUntil,
		}

		err := om.SetObjectRetention(ctx, bucketName, objectKey, retention)
		if err != nil {
			t.Fatalf("Failed to set object retention: %v", err)
		}

		// Verify retention
		retrievedRetention, err := om.GetObjectRetention(ctx, bucketName, objectKey)
		if err != nil {
			t.Fatalf("Failed to get object retention: %v", err)
		}

		if retrievedRetention.Mode != "COMPLIANCE" {
			t.Errorf("Expected retention mode COMPLIANCE, got %s", retrievedRetention.Mode)
		}
	})

	t.Run("SetObjectLegalHold", func(t *testing.T) {
		legalHold := &LegalHoldConfig{
			Status: LegalHoldStatusOn,
		}

		err := om.SetObjectLegalHold(ctx, bucketName, objectKey, legalHold)
		if err != nil {
			t.Fatalf("Failed to set legal hold: %v", err)
		}

		// Verify legal hold
		retrievedLegalHold, err := om.GetObjectLegalHold(ctx, bucketName, objectKey)
		if err != nil {
			t.Fatalf("Failed to get legal hold: %v", err)
		}

		if retrievedLegalHold.Status != LegalHoldStatusOn {
			t.Errorf("Expected legal hold status ON, got %s", retrievedLegalHold.Status)
		}
	})

	t.Run("DeleteObjectWithLegalHold", func(t *testing.T) {
		// Try to delete object with legal hold - should fail
		_, err := om.DeleteObject(ctx, bucketName, objectKey, false)
		if err == nil {
			t.Error("Expected error when deleting object with legal hold")
		}
		if err != ErrObjectUnderLegalHold {
			t.Errorf("Expected ErrObjectUnderLegalHold, got %v", err)
		}

		// Remove legal hold
		legalHold := &LegalHoldConfig{
			Status: LegalHoldStatusOff,
		}
		err = om.SetObjectLegalHold(ctx, bucketName, objectKey, legalHold)
		if err != nil {
			t.Fatalf("Failed to remove legal hold: %v", err)
		}
	})
}

func TestObjectManagerMultipartUpload(t *testing.T) {
	om, bm, cleanup := setupObjectIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()
	bucketName := "multipart-bucket"
	objectKey := "large-file.bin"

	// Create bucket
	err := bm.CreateBucket(ctx, "", bucketName, "")
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}

	var uploadID string
	var parts []Part

	t.Run("CreateMultipartUpload", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("Content-Type", "application/octet-stream")

		upload, err := om.CreateMultipartUpload(ctx, bucketName, objectKey, headers)
		if err != nil {
			t.Fatalf("Failed to create multipart upload: %v", err)
		}

		if upload.UploadID == "" {
			t.Error("Expected non-empty upload ID")
		}

		uploadID = upload.UploadID
	})

	t.Run("UploadParts", func(t *testing.T) {
		// Upload 3 parts
		partSizes := []int{1024, 2048, 512}
		for i, size := range partSizes {
			partNumber := i + 1
			data := make([]byte, size)
			for j := range data {
				data[j] = byte(partNumber)
			}

			part, err := om.UploadPart(ctx, uploadID, partNumber, bytes.NewReader(data))
			if err != nil {
				t.Fatalf("Failed to upload part %d: %v", partNumber, err)
			}

			if part.PartNumber != partNumber {
				t.Errorf("Expected part number %d, got %d", partNumber, part.PartNumber)
			}
			if part.Size != int64(size) {
				t.Errorf("Expected part size %d, got %d", size, part.Size)
			}

			parts = append(parts, *part)
		}
	})

	t.Run("ListParts", func(t *testing.T) {
		listedParts, err := om.ListParts(ctx, uploadID)
		if err != nil {
			t.Fatalf("Failed to list parts: %v", err)
		}

		if len(listedParts) != 3 {
			t.Errorf("Expected 3 parts, got %d", len(listedParts))
		}

		// Verify parts are sorted by part number
		for i, part := range listedParts {
			if part.PartNumber != i+1 {
				t.Errorf("Expected part number %d at index %d, got %d", i+1, i, part.PartNumber)
			}
		}
	})

	t.Run("CompleteMultipartUpload", func(t *testing.T) {
		obj, err := om.CompleteMultipartUpload(ctx, uploadID, parts)
		if err != nil {
			t.Fatalf("Failed to complete multipart upload: %v", err)
		}

		if obj.Key != objectKey {
			t.Errorf("Expected key %s, got %s", objectKey, obj.Key)
		}

		// Verify object exists
		_, err = om.GetObjectMetadata(ctx, bucketName, objectKey)
		if err != nil {
			t.Fatalf("Failed to get completed object: %v", err)
		}
	})

	t.Run("AbortMultipartUpload", func(t *testing.T) {
		// Create new upload to abort
		headers := http.Header{}
		upload, err := om.CreateMultipartUpload(ctx, bucketName, "aborted-file.bin", headers)
		if err != nil {
			t.Fatalf("Failed to create multipart upload: %v", err)
		}

		// Upload a part
		data := []byte("test data")
		_, err = om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(data))
		if err != nil {
			t.Fatalf("Failed to upload part: %v", err)
		}

		// Abort upload
		err = om.AbortMultipartUpload(ctx, upload.UploadID)
		if err != nil {
			t.Fatalf("Failed to abort multipart upload: %v", err)
		}

		// Verify parts are cleaned up (either error or empty list is acceptable)
		parts, err := om.ListParts(ctx, upload.UploadID)
		if err == nil && len(parts) > 0 {
			t.Error("Expected error or empty parts list after aborting upload")
		}
	})

	t.Run("ListMultipartUploads", func(t *testing.T) {
		// Create multiple uploads
		for i := 1; i <= 3; i++ {
			key := "upload-" + string(rune('0'+i)) + ".bin"
			headers := http.Header{}
			_, err := om.CreateMultipartUpload(ctx, bucketName, key, headers)
			if err != nil {
				t.Fatalf("Failed to create upload %d: %v", i, err)
			}
		}

		// List uploads
		uploads, err := om.ListMultipartUploads(ctx, bucketName)
		if err != nil {
			t.Fatalf("Failed to list multipart uploads: %v", err)
		}

		if len(uploads) < 3 {
			t.Errorf("Expected at least 3 uploads, got %d", len(uploads))
		}
	})
}

func TestObjectManagerBucketMetricsIntegration(t *testing.T) {
	om, bm, cleanup := setupObjectIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()
	tenantID := "metrics-tenant"
	bucketName := "metrics-bucket"

	// Create bucket
	err := bm.CreateBucket(ctx, tenantID, bucketName, "")
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}

	// Put multiple objects
	for i := 1; i <= 5; i++ {
		key := "file-" + string(rune('0'+i)) + ".txt"
		content := strings.Repeat("A", 1000*i) // Different sizes
		headers := http.Header{}

		_, err := om.PutObject(ctx, tenantID+"/"+bucketName, key, strings.NewReader(content), headers)
		if err != nil {
			t.Fatalf("Failed to put object %s: %v", key, err)
		}
	}

	// Verify bucket metrics
	bucketInfo, err := bm.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		t.Fatalf("Failed to get bucket info: %v", err)
	}

	if bucketInfo.ObjectCount != 5 {
		t.Errorf("Expected object count 5, got %d", bucketInfo.ObjectCount)
	}

	// Expected total size: 1000 + 2000 + 3000 + 4000 + 5000 = 15000
	expectedSize := int64(15000)
	if bucketInfo.TotalSize != expectedSize {
		t.Errorf("Expected total size %d, got %d", expectedSize, bucketInfo.TotalSize)
	}

	// Delete an object
	_, err = om.DeleteObject(ctx, tenantID+"/"+bucketName, "file-3.txt", false)
	if err != nil {
		t.Fatalf("Failed to delete object: %v", err)
	}

	// Verify metrics updated
	bucketInfo, err = bm.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		t.Fatalf("Failed to get bucket info after deletion: %v", err)
	}

	if bucketInfo.ObjectCount != 4 {
		t.Errorf("Expected object count 4 after deletion, got %d", bucketInfo.ObjectCount)
	}

	// Size should be 15000 - 3000 = 12000
	expectedSize = int64(12000)
	if bucketInfo.TotalSize != expectedSize {
		t.Errorf("Expected total size %d after deletion, got %d", expectedSize, bucketInfo.TotalSize)
	}
}

func TestObjectManagerPersistence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "maxiofs-object-persistence-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	ctx := context.Background()
	bucketName := "persistent-bucket"
	objectKey := "persistent-object.txt"
	objectContent := "This should persist"

	// First session - create objects
	{
		storageBackend, _ := storage.NewFilesystemBackend(storage.Config{
			Root: tempDir,
		})
		dbPath := filepath.Join(tempDir, "metadata")
		metadataStore, _ := metadata.NewBadgerStore(metadata.BadgerOptions{
			DataDir:           dbPath,
			SyncWrites:        true,
			CompactionEnabled: false,
			Logger:            logrus.StandardLogger(),
		})

		bm := bucket.NewManager(storageBackend, metadataStore)
		cfg := config.StorageConfig{Backend: "filesystem", Root: tempDir}
		om := NewManager(storageBackend, metadataStore, cfg)

		// Create bucket
		bm.CreateBucket(ctx, "", bucketName, "")

		// Put object with tags
		headers := http.Header{}
		headers.Set("Content-Type", "text/plain")
		headers.Set("X-Amz-Meta-Version", "1.0")

		obj, err := om.PutObject(ctx, bucketName, objectKey, strings.NewReader(objectContent), headers)
		if err != nil {
			t.Fatalf("Failed to put object: %v", err)
		}

		// Set tags
		tags := &TagSet{
			Tags: []Tag{
				{Key: "Persistence", Value: "Test"},
			},
		}
		om.SetObjectTagging(ctx, bucketName, objectKey, tags)

		// Set retention
		retainUntil := time.Now().Add(24 * time.Hour)
		retention := &RetentionConfig{
			Mode:            "GOVERNANCE",
			RetainUntilDate: retainUntil,
		}
		om.SetObjectRetention(ctx, bucketName, objectKey, retention)

		t.Logf("First session - Object ETag: %s", obj.ETag)

		metadataStore.Close()
	}

	// Give BadgerDB time to flush
	time.Sleep(100 * time.Millisecond)

	// Second session - verify persistence
	{
		storageBackend, _ := storage.NewFilesystemBackend(storage.Config{
			Root: tempDir,
		})
		dbPath := filepath.Join(tempDir, "metadata")
		metadataStore, _ := metadata.NewBadgerStore(metadata.BadgerOptions{
			DataDir:           dbPath,
			SyncWrites:        true,
			CompactionEnabled: false,
			Logger:            logrus.StandardLogger(),
		})
		defer metadataStore.Close()

		cfg := config.StorageConfig{Backend: "filesystem", Root: tempDir}
		om := NewManager(storageBackend, metadataStore, cfg)

		// Get object
		obj, reader, err := om.GetObject(ctx, bucketName, objectKey)
		if err != nil {
			t.Fatalf("Failed to get object after restart: %v", err)
		}
		defer reader.Close()

		// Verify content
		content, _ := io.ReadAll(reader)
		if string(content) != objectContent {
			t.Errorf("Expected content %s, got %s", objectContent, string(content))
		}

		// Verify metadata
		if obj.Metadata["version"] != "1.0" {
			t.Errorf("Expected version metadata to persist, got %v", obj.Metadata)
		}

		// Verify tags
		tags, err := om.GetObjectTagging(ctx, bucketName, objectKey)
		if err != nil {
			t.Fatalf("Failed to get tags after restart: %v", err)
		}
		if len(tags.Tags) == 0 {
			t.Error("Expected tags to persist")
		}

		// Verify retention
		retention, err := om.GetObjectRetention(ctx, bucketName, objectKey)
		if err != nil {
			t.Fatalf("Failed to get retention after restart: %v", err)
		}
		if retention.Mode != "GOVERNANCE" {
			t.Errorf("Expected retention mode GOVERNANCE to persist, got %s", retention.Mode)
		}

		t.Logf("Second session - Object ETag: %s", obj.ETag)
	}
}
