package cluster

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrationJobCRUD tests basic CRUD operations for migration jobs
func TestMigrationJobCRUD(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster
	_, err := manager.InitializeCluster(ctx, "node-1", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	config, err := manager.GetConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	// Test 1: List migrations when empty
	jobs, err := manager.ListMigrationJobs(ctx)
	if err != nil {
		t.Fatalf("Failed to list migrations: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("Expected 0 migrations, got %d", len(jobs))
	}

	// Test 2: Create a migration job via CreateMigrationJob
	job := &MigrationJob{
		BucketName:      "test-bucket",
		SourceNodeID:    config.NodeID,
		TargetNodeID:    "target-node",
		Status:          MigrationStatusPending,
		ObjectsTotal:    100,
		ObjectsMigrated: 0,
		BytesTotal:      102400,
		BytesMigrated:   0,
		DeleteSource:    false,
		VerifyData:      true,
	}

	err = manager.CreateMigrationJob(ctx, job)
	if err != nil {
		t.Fatalf("Failed to create migration job: %v", err)
	}

	if job.ID == 0 {
		t.Error("Expected non-zero migration ID after creation")
	}

	// Test 3: Get the migration job
	retrieved, err := manager.GetMigrationJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("Failed to get migration job: %v", err)
	}

	if retrieved.BucketName != "test-bucket" {
		t.Errorf("Expected bucket 'test-bucket', got '%s'", retrieved.BucketName)
	}
	if retrieved.Status != MigrationStatusPending {
		t.Errorf("Expected status 'pending', got '%s'", retrieved.Status)
	}

	// Test 4: Update migration status and progress
	retrieved.Status = MigrationStatusInProgress
	retrieved.ObjectsMigrated = 50
	retrieved.BytesMigrated = 51200

	err = manager.UpdateMigrationJob(ctx, retrieved)
	if err != nil {
		t.Fatalf("Failed to update migration job: %v", err)
	}

	updated, err := manager.GetMigrationJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("Failed to get updated migration: %v", err)
	}
	if updated.Status != MigrationStatusInProgress {
		t.Errorf("Expected status 'in_progress', got '%s'", updated.Status)
	}
	if updated.ObjectsMigrated != 50 {
		t.Errorf("Expected 50 objects migrated, got %d", updated.ObjectsMigrated)
	}
	if updated.BytesMigrated != 51200 {
		t.Errorf("Expected 51200 bytes migrated, got %d", updated.BytesMigrated)
	}

	// Test 6: List migrations (should have 1)
	jobs, err = manager.ListMigrationJobs(ctx)
	if err != nil {
		t.Fatalf("Failed to list migrations: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("Expected 1 migration, got %d", len(jobs))
	}

	// Test 7: Get migrations by bucket
	bucketJobs, err := manager.GetMigrationJobsByBucket(ctx, "test-bucket")
	if err != nil {
		t.Fatalf("Failed to get migrations by bucket: %v", err)
	}
	if len(bucketJobs) != 1 {
		t.Errorf("Expected 1 migration for test-bucket, got %d", len(bucketJobs))
	}

	// Test 5: Mark as failed with error
	updated.Status = MigrationStatusFailed
	updated.ErrorMessage = "Test error"

	err = manager.UpdateMigrationJob(ctx, updated)
	if err != nil {
		t.Fatalf("Failed to update status with error: %v", err)
	}

	failed, err := manager.GetMigrationJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("Failed to get failed migration: %v", err)
	}
	if failed.Status != MigrationStatusFailed {
		t.Errorf("Expected status 'failed', got '%s'", failed.Status)
	}
	if failed.ErrorMessage != "Test error" {
		t.Errorf("Expected error 'Test error', got '%s'", failed.ErrorMessage)
	}
}

// TestGetMigrationJob_NotFound tests error handling for non-existent migration
func TestGetMigrationJob_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	_, err := manager.GetMigrationJob(ctx, 999)
	if err == nil {
		t.Error("Expected error when getting non-existent migration")
	}
}

func TestManager_SendBucketPermission(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create mock target server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/api/internal/cluster/bucket-permissions")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	manager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()

	err := manager.sendBucketPermission(ctx, proxyClient, server.URL, "local-node", "test-token",
		"perm-1", "test-bucket", "user-1", "tenant-1", "read,write", "admin", time.Now().Unix(), sql.NullInt64{})
	require.NoError(t, err)
}

func TestManager_SendBucketACL(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create mock target server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/api/internal/cluster/bucket-acl")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	manager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()

	acl := map[string]interface{}{
		"grantee_id": "user-1",
		"permission": "READ",
	}

	err := manager.sendBucketACL(ctx, proxyClient, server.URL, "local-node", "test-token",
		"tenant-1", "test-bucket", acl)
	require.NoError(t, err)
}

func TestManager_SendBucketConfiguration(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create mock target server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/api/internal/cluster/bucket-config")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	manager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()

	err := manager.sendBucketConfiguration(ctx, proxyClient, server.URL, "local-node", "test-token",
		"tenant-1", "test-bucket",
		sql.NullString{String: "Enabled", Valid: true},
		sql.NullString{String: "false", Valid: true},
		sql.NullString{},
		sql.NullString{},
		sql.NullString{String: `{"env":"test"}`, Valid: true},
		sql.NullString{},
		sql.NullString{},
		sql.NullString{})
	require.NoError(t, err)
}

func TestManager_SendBucketInventory(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create mock target server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/api/internal/cluster/bucket-inventory")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	manager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()

	err := manager.sendBucketInventory(ctx, proxyClient, server.URL, "local-node", "test-token",
		"tenant-1", "test-bucket", true, "daily", "CSV", "dest-bucket", "prefix/",
		[]string{"Size", "ETag"}, "00:00", sql.NullInt64{}, sql.NullInt64{})
	require.NoError(t, err)
}

func TestManager_CountBucketObjects(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create objects table
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS objects (
			bucket TEXT NOT NULL,
			key TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			size INTEGER NOT NULL,
			created_at TIMESTAMP NOT NULL,
			deleted_at TIMESTAMP,
			PRIMARY KEY (bucket, key, tenant_id)
		)
	`)
	require.NoError(t, err)

	// Insert test objects
	now := time.Now()
	testObjects := []struct {
		bucket   string
		key      string
		tenantID string
		size     int64
		deleted  bool
	}{
		{"test-bucket", "file1.txt", "tenant-1", 100, false},
		{"test-bucket", "file2.txt", "tenant-1", 200, false},
		{"test-bucket", "file3.txt", "tenant-1", 300, false},
		{"test-bucket", "deleted.txt", "tenant-1", 400, true},
		{"other-bucket", "file4.txt", "tenant-1", 500, false},
	}

	for _, obj := range testObjects {
		var deletedAt sql.NullTime
		if obj.deleted {
			deletedAt = sql.NullTime{Time: now, Valid: true}
		}
		_, err = db.ExecContext(ctx, `
			INSERT INTO objects (bucket, key, tenant_id, size, created_at, deleted_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, obj.bucket, obj.key, obj.tenantID, obj.size, now, deletedAt)
		require.NoError(t, err)
	}

	manager := NewManager(db, "http://localhost:8080")

	// Count objects in test-bucket
	count, totalSize, err := manager.countBucketObjects(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)

	assert.Equal(t, int64(3), count, "Should count only non-deleted objects")
	assert.Equal(t, int64(600), totalSize, "Should sum only non-deleted object sizes")
}

func TestManager_CountBucketObjects_EmptyBucket(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create objects table
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS objects (
			bucket TEXT NOT NULL,
			key TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			size INTEGER NOT NULL,
			created_at TIMESTAMP NOT NULL,
			deleted_at TIMESTAMP,
			PRIMARY KEY (bucket, key, tenant_id)
		)
	`)
	require.NoError(t, err)

	manager := NewManager(db, "http://localhost:8080")

	// Count objects in empty bucket
	count, totalSize, err := manager.countBucketObjects(ctx, "tenant-1", "empty-bucket")
	require.NoError(t, err)

	assert.Equal(t, int64(0), count)
	assert.Equal(t, int64(0), totalSize)
}

func TestManager_VerifyObjectOnTarget(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create mock target server that returns object metadata
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "HEAD", r.Method)
		assert.Contains(t, r.URL.Path, "/api/internal/cluster/objects/tenant-1/test-bucket/file.txt")

		w.Header().Set("X-Object-ETag", "etag123")
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	manager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()

	// Verify object exists with correct metadata
	err := manager.verifyObjectOnTarget(ctx, proxyClient, server.URL, "local-node", "test-token",
		"tenant-1", "test-bucket", "file.txt", "etag123")
	require.NoError(t, err)
}

func TestManager_VerifyObjectOnTarget_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create mock target server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	manager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()

	// Verify object - should fail with 404
	err := manager.verifyObjectOnTarget(ctx, proxyClient, server.URL, "local-node", "test-token",
		"tenant-1", "test-bucket", "missing.txt", "etag123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
