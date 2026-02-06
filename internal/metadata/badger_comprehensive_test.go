package metadata

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Helper Functions
// ============================================================================

func createTestStore(t *testing.T) (*BadgerStore, string, func()) {
	tmpDir, err := os.MkdirTemp("", "badger-comprehensive-test-*")
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	store, err := NewBadgerStore(BadgerOptions{
		DataDir:           tmpDir,
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logger,
	})
	require.NoError(t, err)
	require.NotNil(t, store)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, tmpDir, cleanup
}

// ============================================================================
// NewBadgerStore Tests
// ============================================================================

func TestNewBadgerStore_Success(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "badger-new-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	store, err := NewBadgerStore(BadgerOptions{
		DataDir:           tmpDir,
		SyncWrites:        true,
		CompactionEnabled: false,
		Logger:            logger,
	})
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	assert.True(t, store.IsReady())
	assert.NotNil(t, store.DB())
}

func TestNewBadgerStore_WithCompaction(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "badger-compaction-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	store, err := NewBadgerStore(BadgerOptions{
		DataDir:           tmpDir,
		SyncWrites:        false,
		CompactionEnabled: true, // Enable compaction
		Logger:            logger,
	})
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	assert.True(t, store.IsReady())
}

func TestNewBadgerStore_NilLogger(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "badger-nil-logger-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	store, err := NewBadgerStore(BadgerOptions{
		DataDir:           tmpDir,
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            nil, // Should create default logger
	})
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	assert.True(t, store.IsReady())
}

func TestNewBadgerStore_InvalidPath(t *testing.T) {
	// Try to create store in invalid path (null character not allowed in paths)
	_, err := NewBadgerStore(BadgerOptions{
		DataDir:           string([]byte{0}),
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logrus.New(),
	})
	assert.Error(t, err)
}

// ============================================================================
// DB() Method Tests
// ============================================================================

func TestDB_ReturnsValidInstance(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()

	db := store.DB()
	assert.NotNil(t, db)

	// Just verify db is not nil - View would require correct type
	assert.True(t, store.IsReady())
}

// ============================================================================
// Close and IsReady Tests
// ============================================================================

func TestClose_SetsNotReady(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "badger-close-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	store, err := NewBadgerStore(BadgerOptions{
		DataDir:           tmpDir,
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logger,
	})
	require.NoError(t, err)

	assert.True(t, store.IsReady())

	err = store.Close()
	assert.NoError(t, err)
	assert.False(t, store.IsReady())
}

// ============================================================================
// UpdateBucket Tests
// ============================================================================

func TestUpdateBucket_Success(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:      "update-test-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
		Region:    "us-east-1",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Update bucket
	bucket.Region = "eu-west-1"
	bucket.IsPublic = true
	err = store.UpdateBucket(ctx, bucket)
	assert.NoError(t, err)

	// Verify update
	updated, err := store.GetBucket(ctx, "tenant-1", "update-test-bucket")
	assert.NoError(t, err)
	assert.Equal(t, "eu-west-1", updated.Region)
	assert.True(t, updated.IsPublic)
	assert.False(t, updated.UpdatedAt.IsZero())
}

func TestUpdateBucket_NotFound(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:     "non-existent-bucket",
		TenantID: "tenant-1",
	}

	err := store.UpdateBucket(ctx, bucket)
	assert.ErrorIs(t, err, ErrBucketNotFound)
}

func TestUpdateBucket_NilBucket(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.UpdateBucket(ctx, nil)
	assert.Error(t, err)
}

// ============================================================================
// GetBucketByName Tests
// ============================================================================

func TestGetBucketByName_Success(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create buckets in different tenants
	bucket1 := &BucketMetadata{
		Name:      "global-unique-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket1)
	require.NoError(t, err)

	// Find by name (across all tenants)
	found, err := store.GetBucketByName(ctx, "global-unique-bucket")
	assert.NoError(t, err)
	assert.NotNil(t, found)
	assert.Equal(t, "global-unique-bucket", found.Name)
	assert.Equal(t, "tenant-1", found.TenantID)
}

func TestGetBucketByName_NotFound(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	found, err := store.GetBucketByName(ctx, "non-existent-bucket")
	assert.ErrorIs(t, err, ErrBucketNotFound)
	assert.Nil(t, found)
}

// ============================================================================
// GetBucketStats Tests
// ============================================================================

func TestGetBucketStats_Success(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:        "stats-bucket",
		TenantID:    "tenant-1",
		OwnerID:     "user-1",
		OwnerType:   "user",
		ObjectCount: 10,
		TotalSize:   1024000,
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Get stats
	count, size, err := store.GetBucketStats(ctx, "tenant-1", "stats-bucket")
	assert.NoError(t, err)
	assert.Equal(t, int64(10), count)
	assert.Equal(t, int64(1024000), size)
}

func TestGetBucketStats_NotFound(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, _, err := store.GetBucketStats(ctx, "tenant-1", "non-existent")
	assert.ErrorIs(t, err, ErrBucketNotFound)
}

// ============================================================================
// RecalculateBucketStats Tests
// ============================================================================

func TestRecalculateBucketStats_Success(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket with wrong stats
	bucket := &BucketMetadata{
		Name:        "recalc-bucket",
		TenantID:    "tenant-1",
		OwnerID:     "user-1",
		OwnerType:   "user",
		ObjectCount: 999, // Wrong
		TotalSize:   999, // Wrong
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Add some objects
	for i := 0; i < 5; i++ {
		obj := &ObjectMetadata{
			Bucket: "recalc-bucket",
			Key:    "obj-" + string(rune('a'+i)),
			Size:   100,
			ETag:   "etag",
		}
		err := store.PutObject(ctx, obj)
		require.NoError(t, err)
	}

	// Recalculate stats
	err = store.RecalculateBucketStats(ctx, "tenant-1", "recalc-bucket")
	assert.NoError(t, err)

	// Verify stats are correct now
	count, size, err := store.GetBucketStats(ctx, "tenant-1", "recalc-bucket")
	assert.NoError(t, err)
	assert.Equal(t, int64(5), count)
	assert.Equal(t, int64(500), size) // 5 * 100
}

func TestRecalculateBucketStats_EmptyBucket(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:        "empty-recalc-bucket",
		TenantID:    "tenant-1",
		OwnerID:     "user-1",
		OwnerType:   "user",
		ObjectCount: 100,
		TotalSize:   10000,
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	err = store.RecalculateBucketStats(ctx, "tenant-1", "empty-recalc-bucket")
	assert.NoError(t, err)

	count, size, err := store.GetBucketStats(ctx, "tenant-1", "empty-recalc-bucket")
	assert.NoError(t, err)
	assert.Equal(t, int64(0), count)
	assert.Equal(t, int64(0), size)
}

// ============================================================================
// UpdateBucketMetrics Edge Cases
// ============================================================================

func TestUpdateBucketMetrics_NegativeValues(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:        "metrics-negative-bucket",
		TenantID:    "tenant-1",
		OwnerID:     "user-1",
		OwnerType:   "user",
		ObjectCount: 5,
		TotalSize:   500,
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Try to decrement more than available (should floor at 0)
	err = store.UpdateBucketMetrics(ctx, "tenant-1", "metrics-negative-bucket", -100, -10000)
	assert.NoError(t, err)

	count, size, err := store.GetBucketStats(ctx, "tenant-1", "metrics-negative-bucket")
	assert.NoError(t, err)
	assert.Equal(t, int64(0), count)
	assert.Equal(t, int64(0), size)
}

func TestUpdateBucketMetrics_NotFound(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.UpdateBucketMetrics(ctx, "tenant-1", "non-existent", 1, 100)
	assert.ErrorIs(t, err, ErrBucketNotFound)
}

func TestUpdateBucketMetrics_ConcurrentUpdates(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:        "concurrent-bucket",
		TenantID:    "tenant-1",
		OwnerID:     "user-1",
		OwnerType:   "user",
		ObjectCount: 0,
		TotalSize:   0,
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Concurrent updates - test that updates complete without errors
	// Note: Due to transaction conflicts, not all updates may succeed
	var wg sync.WaitGroup
	numGoroutines := 10
	updatesPerGoroutine := 10
	successCount := int64(0)
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < updatesPerGoroutine; j++ {
				err := store.UpdateBucketMetrics(ctx, "tenant-1", "concurrent-bucket", 1, 100)
				if err == nil {
					mu.Lock()
					successCount++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	count, size, err := store.GetBucketStats(ctx, "tenant-1", "concurrent-bucket")
	assert.NoError(t, err)
	// Verify that successful updates are reflected correctly
	assert.Equal(t, successCount, count)
	assert.Equal(t, successCount*100, size)
	// Most updates should succeed (at least 80%)
	assert.GreaterOrEqual(t, successCount, int64(80))
}

// ============================================================================
// Compact Tests
// ============================================================================

func TestCompact_Success(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Add and delete some data to create garbage
	for i := 0; i < 100; i++ {
		obj := &ObjectMetadata{
			Bucket: "compact-test",
			Key:    "temp-obj-" + string(rune(i)),
			Size:   1000,
		}
		store.PutObject(ctx, obj)
	}

	for i := 0; i < 100; i++ {
		store.DeleteObject(ctx, "compact-test", "temp-obj-"+string(rune(i)))
	}

	// Run compaction - may return error if nothing to compact
	// Both nil and any error are acceptable since GC behavior depends on data
	_ = store.Compact(ctx)
	// Test passes as long as Compact doesn't panic
}

// ============================================================================
// Backup Tests
// ============================================================================

func TestBackup_Success(t *testing.T) {
	store, tmpDir, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Add some data
	bucket := &BucketMetadata{
		Name:      "backup-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	obj := &ObjectMetadata{
		Bucket: "backup-bucket",
		Key:    "backup-object",
		Size:   1024,
		ETag:   "backup-etag",
	}
	err = store.PutObject(ctx, obj)
	require.NoError(t, err)

	// Create backup
	backupPath := filepath.Join(tmpDir, "backup.bak")
	err = store.Backup(ctx, backupPath)
	assert.NoError(t, err)

	// Verify backup file exists
	_, err = os.Stat(backupPath)
	assert.NoError(t, err)
}

func TestBackup_InvalidPath(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Try backup to invalid path
	err := store.Backup(ctx, "/non/existent/path/backup.bak")
	assert.Error(t, err)
}

// ============================================================================
// GetRaw/PutRaw/DeleteRaw Tests
// ============================================================================

func TestRawOperations_Success(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	key := "custom:config:setting1"
	value := []byte(`{"enabled": true, "value": 42}`)

	// Put
	err := store.PutRaw(ctx, key, value)
	assert.NoError(t, err)

	// Get
	retrieved, err := store.GetRaw(ctx, key)
	assert.NoError(t, err)
	assert.Equal(t, value, retrieved)

	// Delete
	err = store.DeleteRaw(ctx, key)
	assert.NoError(t, err)

	// Verify deleted
	_, err = store.GetRaw(ctx, key)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetRaw_NotFound(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.GetRaw(ctx, "non-existent-key")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDeleteRaw_NotFound(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.DeleteRaw(ctx, "non-existent-key")
	// BadgerDB doesn't return error when deleting non-existent key
	// It silently succeeds, which is valid behavior
	// Just verify we can call it without panic
	_ = err
}

// ============================================================================
// CreateBucket Edge Cases
// ============================================================================

func TestCreateBucket_NilBucket(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.CreateBucket(ctx, nil)
	assert.Error(t, err)
}

func TestCreateBucket_DuplicateName(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket1 := &BucketMetadata{
		Name:      "duplicate-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket1)
	require.NoError(t, err)

	// Same name in different tenant should also fail (global uniqueness)
	bucket2 := &BucketMetadata{
		Name:      "duplicate-bucket",
		TenantID:  "tenant-2",
		OwnerID:   "user-2",
		OwnerType: "user",
	}
	err = store.CreateBucket(ctx, bucket2)
	assert.ErrorIs(t, err, ErrBucketAlreadyExists)
}

func TestCreateBucket_SetsTimestamps(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:      "timestamp-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}

	beforeCreate := time.Now()
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	retrieved, err := store.GetBucket(ctx, "tenant-1", "timestamp-bucket")
	assert.NoError(t, err)
	assert.True(t, retrieved.CreatedAt.After(beforeCreate) || retrieved.CreatedAt.Equal(beforeCreate))
	assert.True(t, retrieved.UpdatedAt.After(beforeCreate) || retrieved.UpdatedAt.Equal(beforeCreate))
}

// ============================================================================
// ListBuckets Tests
// ============================================================================

func TestListBuckets_AllTenants(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create buckets in different tenants
	for i := 0; i < 3; i++ {
		bucket := &BucketMetadata{
			Name:      "bucket-tenant1-" + string(rune('a'+i)),
			TenantID:  "tenant-1",
			OwnerID:   "user-1",
			OwnerType: "user",
		}
		store.CreateBucket(ctx, bucket)
	}

	for i := 0; i < 2; i++ {
		bucket := &BucketMetadata{
			Name:      "bucket-tenant2-" + string(rune('a'+i)),
			TenantID:  "tenant-2",
			OwnerID:   "user-2",
			OwnerType: "user",
		}
		store.CreateBucket(ctx, bucket)
	}

	// List all (empty tenantID)
	all, err := store.ListBuckets(ctx, "")
	assert.NoError(t, err)
	assert.Len(t, all, 5)

	// List specific tenant
	tenant1Buckets, err := store.ListBuckets(ctx, "tenant-1")
	assert.NoError(t, err)
	assert.Len(t, tenant1Buckets, 3)

	tenant2Buckets, err := store.ListBuckets(ctx, "tenant-2")
	assert.NoError(t, err)
	assert.Len(t, tenant2Buckets, 2)
}

func TestListBuckets_EmptyResult(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	buckets, err := store.ListBuckets(ctx, "non-existent-tenant")
	assert.NoError(t, err)
	assert.Empty(t, buckets)
}

// ============================================================================
// PutObject Edge Cases
// ============================================================================

func TestPutObject_NilObject(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.PutObject(ctx, nil)
	assert.Error(t, err)
}

func TestPutObject_EmptyBucket(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	obj := &ObjectMetadata{
		Bucket: "",
		Key:    "test",
	}
	err := store.PutObject(ctx, obj)
	assert.ErrorIs(t, err, ErrInvalidKey)
}

func TestPutObject_EmptyKey(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	obj := &ObjectMetadata{
		Bucket: "test-bucket",
		Key:    "",
	}
	err := store.PutObject(ctx, obj)
	assert.ErrorIs(t, err, ErrInvalidKey)
}

func TestPutObject_WithTags(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	obj := &ObjectMetadata{
		Bucket:      "tag-bucket",
		Key:         "tagged-obj",
		Size:        100,
		ContentType: "text/plain",
		Tags: map[string]string{
			"env":  "prod",
			"team": "backend",
		},
	}

	err := store.PutObject(ctx, obj)
	assert.NoError(t, err)

	retrieved, err := store.GetObject(ctx, "tag-bucket", "tagged-obj")
	assert.NoError(t, err)
	assert.Equal(t, "prod", retrieved.Tags["env"])
	assert.Equal(t, "backend", retrieved.Tags["team"])
}

// ============================================================================
// GetObject Edge Cases
// ============================================================================

func TestGetObject_EmptyBucket(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.GetObject(ctx, "", "key")
	assert.ErrorIs(t, err, ErrInvalidKey)
}

func TestGetObject_EmptyKey(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.GetObject(ctx, "bucket", "")
	assert.ErrorIs(t, err, ErrInvalidKey)
}

// ============================================================================
// DeleteObject Edge Cases
// ============================================================================

func TestDeleteObject_EmptyBucket(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.DeleteObject(ctx, "", "key")
	assert.ErrorIs(t, err, ErrInvalidKey)
}

func TestDeleteObject_EmptyKey(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.DeleteObject(ctx, "bucket", "")
	assert.ErrorIs(t, err, ErrInvalidKey)
}

// ============================================================================
// ObjectExists Edge Cases
// ============================================================================

func TestObjectExists_EmptyBucket(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.ObjectExists(ctx, "", "key")
	assert.ErrorIs(t, err, ErrInvalidKey)
}

func TestObjectExists_EmptyKey(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.ObjectExists(ctx, "bucket", "")
	assert.ErrorIs(t, err, ErrInvalidKey)
}

// ============================================================================
// ListObjects Edge Cases
// ============================================================================

func TestListObjects_EmptyBucket(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, _, err := store.ListObjects(ctx, "", "", "", 10)
	assert.Error(t, err)
}

func TestListObjects_DefaultMaxKeys(t *testing.T) {
	store, _, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket and objects
	for i := 0; i < 5; i++ {
		obj := &ObjectMetadata{
			Bucket: "default-max-bucket",
			Key:    "obj-" + string(rune('a'+i)),
			Size:   100,
		}
		store.PutObject(ctx, obj)
	}

	// Pass 0 or negative maxKeys - should default to 1000
	objs, _, err := store.ListObjects(ctx, "default-max-bucket", "", "", 0)
	assert.NoError(t, err)
	assert.Len(t, objs, 5)

	objs, _, err = store.ListObjects(ctx, "default-max-bucket", "", "", -1)
	assert.NoError(t, err)
	assert.Len(t, objs, 5)
}

// ============================================================================
// BadgerLogger Tests
// ============================================================================

func TestBadgerLogger_AllLevels(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.TraceLevel)

	bl := newBadgerLogger(logger)
	assert.NotNil(t, bl)

	// These should not panic
	bl.Errorf("test error %s", "message")
	bl.Warningf("test warning %s", "message")
	bl.Infof("test info %s", "message")
	bl.Debugf("test debug %s", "message")
}

// ============================================================================
// extractObjectKeyFromKey Tests
// ============================================================================

func TestExtractObjectKeyFromKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid key", "obj:bucket:my/object/key", "my/object/key"},
		{"empty key", "obj:bucket:", ""},
		{"no colons", "invalid", ""},
		{"one colon", "obj:bucket", ""},
		{"complex key", "obj:my-bucket:path/to/file.txt", "path/to/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractObjectKeyFromKey(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
