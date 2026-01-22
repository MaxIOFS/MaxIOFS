package inventory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestNewWorker tests worker initialization
func TestNewWorker(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	// Create a minimal manager for this test
	db := setupTestDB(t)
	defer db.Close()
	manager := NewManager(db)

	// Create worker
	worker := NewWorker(manager, mockBucketMgr, mockMetadata, mockStorage)

	// Verify
	assert.NotNil(t, worker)
	assert.NotNil(t, worker.manager)
	assert.NotNil(t, worker.generator)
	assert.NotNil(t, worker.bucketManager)
	assert.NotNil(t, worker.stopChan)
}

// TestWorker_ProcessInventoryConfig_Success tests successful inventory processing
func TestWorker_ProcessInventoryConfig_Success(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	// Create mock manager with in-memory DB
	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)

	worker := NewWorker(manager, mockBucketMgr, mockMetadata, mockStorage)

	ctx := context.Background()
	now := time.Now()

	config := &InventoryConfig{
		ID:                "test-config-1",
		BucketName:        "source-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key"},
		ScheduleTime:      "02:00",
	}

	// Mock source bucket exists
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "source-bucket").Return(&bucket.Bucket{
		Name:     "source-bucket",
		TenantID: "tenant1",
	}, nil).Once()

	// Mock destination bucket exists
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "dest-bucket").Return(&bucket.Bucket{
		Name:     "dest-bucket",
		TenantID: "tenant1",
	}, nil).Twice()

	// Mock objects in source bucket
	mockMetadata.On("ListObjects", ctx, "source-bucket", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{
				TenantID:     "tenant1",
				Bucket:       "source-bucket",
				Key:          "test-file.txt",
				Size:         1024,
				LastModified: now,
				ETag:         "abc123",
			},
		},
		"",
		nil,
	).Once()

	// Mock successful storage operations
	mockMetadata.On("PutObject", ctx, mock.AnythingOfType("*metadata.ObjectMetadata")).Return(nil).Once()
	mockStorage.On("Put", ctx, mock.AnythingOfType("string"), mock.Anything, mock.AnythingOfType("map[string]string")).Return(nil).Once()

	// Execute
	err := worker.processInventoryConfig(ctx, config)

	// Verify
	assert.NoError(t, err)
	mockBucketMgr.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	mockStorage.AssertExpectations(t)

	// Verify config was updated with next run time
	assert.NotNil(t, config.LastRunAt)
	assert.NotNil(t, config.NextRunAt)
}

// TestWorker_ProcessInventoryConfig_SourceBucketNotFound tests error when source bucket doesn't exist
func TestWorker_ProcessInventoryConfig_SourceBucketNotFound(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)

	worker := NewWorker(manager, mockBucketMgr, mockMetadata, mockStorage)

	ctx := context.Background()

	config := &InventoryConfig{
		ID:                "test-config-2",
		BucketName:        "nonexistent-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key"},
		ScheduleTime:      "02:00",
	}

	// Mock source bucket does NOT exist
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "nonexistent-bucket").Return(
		nil, errors.New("bucket not found"),
	).Once()

	// Execute
	err := worker.processInventoryConfig(ctx, config)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Source bucket not found")
	mockBucketMgr.AssertExpectations(t)

	// Verify next run time was still calculated (for retry)
	assert.NotNil(t, config.NextRunAt)
}

// TestWorker_ProcessInventoryConfig_DestinationBucketNotFound tests error when destination bucket doesn't exist
func TestWorker_ProcessInventoryConfig_DestinationBucketNotFound(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)

	worker := NewWorker(manager, mockBucketMgr, mockMetadata, mockStorage)

	ctx := context.Background()

	config := &InventoryConfig{
		ID:                "test-config-3",
		BucketName:        "source-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "nonexistent-dest",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key"},
		ScheduleTime:      "02:00",
	}

	// Mock source bucket exists
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "source-bucket").Return(&bucket.Bucket{
		Name:     "source-bucket",
		TenantID: "tenant1",
	}, nil).Once()

	// Mock destination bucket does NOT exist
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "nonexistent-dest").Return(
		nil, errors.New("bucket not found"),
	).Once()

	// Execute
	err := worker.processInventoryConfig(ctx, config)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Destination bucket not found")
	mockBucketMgr.AssertExpectations(t)

	// Verify next run time was still calculated (for retry)
	assert.NotNil(t, config.NextRunAt)
}

// TestWorker_ProcessInventoryConfig_CircularReference tests detection of circular reference
func TestWorker_ProcessInventoryConfig_CircularReference(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)

	worker := NewWorker(manager, mockBucketMgr, mockMetadata, mockStorage)

	ctx := context.Background()

	config := &InventoryConfig{
		ID:                "test-config-4",
		BucketName:        "same-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "same-bucket", // Same as source!
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key"},
		ScheduleTime:      "02:00",
	}

	// Mock source bucket exists
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "same-bucket").Return(&bucket.Bucket{
		Name:     "same-bucket",
		TenantID: "tenant1",
	}, nil).Twice() // Called twice: once for source, once for destination

	// Execute
	err := worker.processInventoryConfig(ctx, config)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Circular reference")
	mockBucketMgr.AssertExpectations(t)

	// Verify next run time was still calculated (for retry)
	assert.NotNil(t, config.NextRunAt)
}

// TestWorker_ProcessInventoryConfig_GenerationFailure tests handling of report generation failure
func TestWorker_ProcessInventoryConfig_GenerationFailure(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)

	worker := NewWorker(manager, mockBucketMgr, mockMetadata, mockStorage)

	ctx := context.Background()

	config := &InventoryConfig{
		ID:                "test-config-5",
		BucketName:        "source-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key"},
		ScheduleTime:      "02:00",
	}

	// Mock source bucket exists
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "source-bucket").Return(&bucket.Bucket{
		Name:     "source-bucket",
		TenantID: "tenant1",
	}, nil).Once()

	// Mock destination bucket exists (called twice: once in worker, once in generator)
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "dest-bucket").Return(&bucket.Bucket{
		Name:     "dest-bucket",
		TenantID: "tenant1",
	}, nil).Twice()

	// Mock objects in source bucket
	mockMetadata.On("ListObjects", ctx, "source-bucket", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{
				TenantID: "tenant1",
				Bucket:   "source-bucket",
				Key:      "test.txt",
				Size:     100,
			},
		},
		"",
		nil,
	).Once()

	// Mock metadata write succeeds
	mockMetadata.On("PutObject", ctx, mock.AnythingOfType("*metadata.ObjectMetadata")).Return(nil).Once()

	// Mock storage failure
	mockStorage.On("Put", ctx, mock.AnythingOfType("string"), mock.Anything, mock.AnythingOfType("map[string]string")).Return(
		errors.New("storage write failed"),
	).Once()

	// Execute
	err := worker.processInventoryConfig(ctx, config)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate report")
	mockBucketMgr.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
}

// TestWorker_ProcessInventories_NoConfigs tests processing when no configs are ready
func TestWorker_ProcessInventories_NoConfigs(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)

	worker := NewWorker(manager, mockBucketMgr, mockMetadata, mockStorage)

	ctx := context.Background()

	// Execute - no configs exist
	worker.processInventories(ctx)

	// Should complete without error (logs "No inventory configurations ready to run")
	// No assertions needed - just verifying it doesn't panic
}

// TestWorker_ProcessInventories_MultipleConfigs tests processing multiple configurations
func TestWorker_ProcessInventories_MultipleConfigs(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)

	ctx := context.Background()
	now := time.Now()

	// Create two test configs with NextRunAt set to the past (ready to run)
	pastTime := time.Now().Add(-1 * time.Hour).Unix()

	config1 := &InventoryConfig{
		ID:                "config-1",
		BucketName:        "bucket-1",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-1",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key"},
		ScheduleTime:      "02:00",
		NextRunAt:         &pastTime, // Ready to run!
	}

	config2 := &InventoryConfig{
		ID:                "config-2",
		BucketName:        "bucket-2",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "json",
		DestinationBucket: "dest-2",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key"},
		ScheduleTime:      "03:00",
		NextRunAt:         &pastTime, // Ready to run!
	}

	// Save configs to DB
	err := manager.CreateConfig(ctx, config1)
	assert.NoError(t, err)
	err = manager.CreateConfig(ctx, config2)
	assert.NoError(t, err)

	// Update NextRunAt to make them ready to run (CreateConfig overwrites NextRunAt)
	config1.NextRunAt = &pastTime
	config2.NextRunAt = &pastTime
	err = manager.UpdateConfig(ctx, config1)
	assert.NoError(t, err)
	err = manager.UpdateConfig(ctx, config2)
	assert.NoError(t, err)

	worker := NewWorker(manager, mockBucketMgr, mockMetadata, mockStorage)

	// Mock for config1
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "bucket-1").Return(&bucket.Bucket{
		Name:     "bucket-1",
		TenantID: "tenant1",
	}, nil).Once()
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "dest-1").Return(&bucket.Bucket{
		Name:     "dest-1",
		TenantID: "tenant1",
	}, nil).Twice()
	mockMetadata.On("ListObjects", ctx, "bucket-1", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{TenantID: "tenant1", Bucket: "bucket-1", Key: "file1.txt", Size: 100, LastModified: now},
		},
		"", nil,
	).Once()
	mockMetadata.On("PutObject", ctx, mock.MatchedBy(func(obj *metadata.ObjectMetadata) bool {
		return obj.Bucket == "dest-1"
	})).Return(nil).Once()
	mockStorage.On("Put", ctx, mock.MatchedBy(func(path string) bool {
		return true
	}), mock.Anything, mock.AnythingOfType("map[string]string")).Return(nil).Once()

	// Mock for config2
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "bucket-2").Return(&bucket.Bucket{
		Name:     "bucket-2",
		TenantID: "tenant1",
	}, nil).Once()
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "dest-2").Return(&bucket.Bucket{
		Name:     "dest-2",
		TenantID: "tenant1",
	}, nil).Twice()
	mockMetadata.On("ListObjects", ctx, "bucket-2", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{TenantID: "tenant1", Bucket: "bucket-2", Key: "file2.txt", Size: 200, LastModified: now},
		},
		"", nil,
	).Once()
	mockMetadata.On("PutObject", ctx, mock.MatchedBy(func(obj *metadata.ObjectMetadata) bool {
		return obj.Bucket == "dest-2"
	})).Return(nil).Once()
	mockStorage.On("Put", ctx, mock.MatchedBy(func(path string) bool {
		return true
	}), mock.Anything, mock.AnythingOfType("map[string]string")).Return(nil).Once()

	// Execute
	worker.processInventories(ctx)

	// Verify both were processed
	mockBucketMgr.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
}

// TestWorker_StartStop tests worker lifecycle
func TestWorker_StartStop(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)

	worker := NewWorker(manager, mockBucketMgr, mockMetadata, mockStorage)

	ctx := context.Background()

	// Start worker with very short interval
	worker.Start(ctx, 100*time.Millisecond)

	// Let it run briefly
	time.Sleep(150 * time.Millisecond)

	// Stop worker
	worker.Stop()

	// Give it time to stop
	time.Sleep(50 * time.Millisecond)

	// Verify ticker was stopped (no panic)
	assert.NotNil(t, worker)
}

// TestWorker_StartStop_WithContextCancellation tests worker stops on context cancellation
func TestWorker_StartStop_WithContextCancellation(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)

	worker := NewWorker(manager, mockBucketMgr, mockMetadata, mockStorage)

	ctx, cancel := context.WithCancel(context.Background())

	// Start worker
	worker.Start(ctx, 100*time.Millisecond)

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Give it time to stop
	time.Sleep(100 * time.Millisecond)

	// Verify worker stopped gracefully
	assert.NotNil(t, worker)
}

// TestWorker_RecordFailure tests the recordFailure method
func TestWorker_RecordFailure(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)

	worker := NewWorker(manager, mockBucketMgr, mockMetadata, mockStorage)

	ctx := context.Background()

	config := &InventoryConfig{
		ID:                "test-config-fail",
		BucketName:        "test-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key"},
		ScheduleTime:      "02:00",
	}

	// Save config to DB first
	err := manager.CreateConfig(ctx, config)
	assert.NoError(t, err)

	// Execute
	err = worker.recordFailure(ctx, config, "Test error message")

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Test error message")

	// Verify next run time was calculated
	assert.NotNil(t, config.NextRunAt)

	// Verify failed report was created in DB
	reports, err := manager.ListReports(ctx, config.BucketName, config.TenantID, 10, 0)
	assert.NoError(t, err)
	assert.Len(t, reports, 1)
	assert.Equal(t, "failed", reports[0].Status)
	assert.NotNil(t, reports[0].ErrorMessage)
	assert.Contains(t, *reports[0].ErrorMessage, "Test error message")
}
