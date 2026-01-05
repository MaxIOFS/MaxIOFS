package cluster

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestBucketPermissionSyncManager_ListLocalBucketPermissions tests listing permissions from database
func TestBucketPermissionSyncManager_ListLocalBucketPermissions(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create required tables
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create users table: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS tenants (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create tenants table: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS bucket_permissions (
			id TEXT PRIMARY KEY,
			bucket_name TEXT NOT NULL,
			user_id TEXT,
			tenant_id TEXT,
			permission_level TEXT NOT NULL,
			granted_by TEXT NOT NULL,
			granted_at INTEGER NOT NULL,
			expires_at INTEGER,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create bucket_permissions table: %v", err)
	}

	// Insert test user and tenant
	now := time.Now().Unix()
	_, err = db.ExecContext(ctx, `INSERT INTO users (id, username, created_at) VALUES ('user-1', 'testuser', ?)`, now)
	if err != nil {
		t.Fatalf("Failed to insert test user: %v", err)
	}

	_, err = db.ExecContext(ctx, `INSERT INTO tenants (id, name, created_at) VALUES ('tenant-1', 'testtenant', ?)`, now)
	if err != nil {
		t.Fatalf("Failed to insert test tenant: %v", err)
	}

	// Insert test permissions
	testPermissions := []struct {
		id              string
		bucketName      string
		userID          *string
		tenantID        *string
		permissionLevel string
		grantedBy       string
	}{
		{"perm-1", "bucket-1", stringPtr("user-1"), nil, "read", "admin"},
		{"perm-2", "bucket-2", nil, stringPtr("tenant-1"), "write", "admin"},
		{"perm-3", "bucket-3", stringPtr("user-1"), nil, "admin", "admin"},
	}

	for _, perm := range testPermissions {
		_, err = db.ExecContext(ctx, `
			INSERT INTO bucket_permissions (id, bucket_name, user_id, tenant_id, permission_level, granted_by, granted_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, perm.id, perm.bucketName, perm.userID, perm.tenantID, perm.permissionLevel, perm.grantedBy, now)
		if err != nil {
			t.Fatalf("Failed to insert test permission: %v", err)
		}
	}

	// Create managers
	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewBucketPermissionSyncManager(db, clusterManager)

	// Test listing permissions
	permissions, err := syncManager.listLocalBucketPermissions(ctx)
	if err != nil {
		t.Fatalf("Failed to list bucket permissions: %v", err)
	}

	if len(permissions) != 3 {
		t.Errorf("Expected 3 bucket permissions, got %d", len(permissions))
	}

	// Verify permission data
	found := false
	for _, perm := range permissions {
		if perm.ID == "perm-1" {
			found = true
			if perm.BucketName != "bucket-1" {
				t.Errorf("Expected bucket 'bucket-1', got '%s'", perm.BucketName)
			}
			if perm.UserID == nil || *perm.UserID != "user-1" {
				t.Error("Expected user_id 'user-1'")
			}
			if perm.TenantID != nil {
				t.Error("Expected tenant_id to be nil")
			}
			if perm.PermissionLevel != "read" {
				t.Errorf("Expected permission level 'read', got '%s'", perm.PermissionLevel)
			}
		}
	}

	if !found {
		t.Error("Expected to find permission perm-1")
	}
}

// TestBucketPermissionSyncManager_ComputeChecksum tests checksum calculation
func TestBucketPermissionSyncManager_ComputeChecksum(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewBucketPermissionSyncManager(db, clusterManager)

	now := time.Now().Unix()
	userID := "user-1"
	perm := &BucketPermissionData{
		ID:              "perm-1",
		BucketName:      "bucket-1",
		UserID:          &userID,
		TenantID:        nil,
		PermissionLevel: "read",
		GrantedBy:       "admin",
		GrantedAt:       now,
		ExpiresAt:       nil,
	}

	checksum1 := syncManager.computePermissionChecksum(perm)

	// Same data should produce same checksum
	checksum2 := syncManager.computePermissionChecksum(perm)
	if checksum1 != checksum2 {
		t.Errorf("Expected same checksum for same data, got %s and %s", checksum1, checksum2)
	}

	// Different permission level should produce different checksum
	perm.PermissionLevel = "write"
	checksum3 := syncManager.computePermissionChecksum(perm)
	if checksum1 == checksum3 {
		t.Error("Expected different checksum for different permission level")
	}

	// Different bucket should produce different checksum
	perm.PermissionLevel = "read" // reset
	perm.BucketName = "bucket-2"
	checksum4 := syncManager.computePermissionChecksum(perm)
	if checksum1 == checksum4 {
		t.Error("Expected different checksum for different bucket")
	}
}

// TestBucketPermissionSyncManager_NeedsSynchronization tests sync status checking
func TestBucketPermissionSyncManager_NeedsSynchronization(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Initialize cluster schema
	if err := InitReplicationSchema(db); err != nil {
		t.Fatalf("Failed to initialize replication schema: %v", err)
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewBucketPermissionSyncManager(db, clusterManager)

	// Test 1: Never synced before - should need sync
	needsSync, err := syncManager.needsSynchronization(ctx, "perm-1", "node-1", "checksum123")
	if err != nil {
		t.Fatalf("Failed to check sync status: %v", err)
	}
	if !needsSync {
		t.Error("Expected to need synchronization for new permission")
	}

	// Test 2: Update sync status
	err = syncManager.updateSyncStatus(ctx, "perm-1", "source-node", "node-1", "checksum123")
	if err != nil {
		t.Fatalf("Failed to update sync status: %v", err)
	}

	// Test 3: Same checksum - should not need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "perm-1", "node-1", "checksum123")
	if err != nil {
		t.Fatalf("Failed to check sync status: %v", err)
	}
	if needsSync {
		t.Error("Expected not to need synchronization with same checksum")
	}

	// Test 4: Different checksum - should need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "perm-1", "node-1", "checksum456")
	if err != nil {
		t.Fatalf("Failed to check sync status: %v", err)
	}
	if !needsSync {
		t.Error("Expected to need synchronization with different checksum")
	}
}

// TestBucketPermissionSyncManager_UpdateSyncStatus tests updating sync status
func TestBucketPermissionSyncManager_UpdateSyncStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Initialize cluster schema
	if err := InitReplicationSchema(db); err != nil {
		t.Fatalf("Failed to initialize replication schema: %v", err)
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewBucketPermissionSyncManager(db, clusterManager)

	// Test 1: Insert new sync status
	err := syncManager.updateSyncStatus(ctx, "perm-1", "source-node", "dest-node", "checksum1")
	if err != nil {
		t.Fatalf("Failed to insert sync status: %v", err)
	}

	// Verify it was inserted
	var checksum string
	err = db.QueryRowContext(ctx, `
		SELECT permission_checksum FROM cluster_bucket_permission_sync
		WHERE permission_id = ? AND destination_node_id = ?
	`, "perm-1", "dest-node").Scan(&checksum)
	if err != nil {
		t.Fatalf("Failed to query sync status: %v", err)
	}
	if checksum != "checksum1" {
		t.Errorf("Expected checksum 'checksum1', got '%s'", checksum)
	}

	// Test 2: Update existing sync status
	err = syncManager.updateSyncStatus(ctx, "perm-1", "source-node", "dest-node", "checksum2")
	if err != nil {
		t.Fatalf("Failed to update sync status: %v", err)
	}

	// Verify it was updated
	err = db.QueryRowContext(ctx, `
		SELECT permission_checksum FROM cluster_bucket_permission_sync
		WHERE permission_id = ? AND destination_node_id = ?
	`, "perm-1", "dest-node").Scan(&checksum)
	if err != nil {
		t.Fatalf("Failed to query updated sync status: %v", err)
	}
	if checksum != "checksum2" {
		t.Errorf("Expected checksum 'checksum2', got '%s'", checksum)
	}

	// Should still have only 1 record
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_bucket_permission_sync
		WHERE permission_id = ? AND destination_node_id = ?
	`, "perm-1", "dest-node").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count sync records: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 sync record, got %d", count)
	}
}

// TestBucketPermissionSyncManager_Stop tests stopping the sync manager
func TestBucketPermissionSyncManager_Stop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewBucketPermissionSyncManager(db, clusterManager)

	// Stop should not panic
	syncManager.Stop()

	// Stop channel should be closed
	select {
	case <-syncManager.stopChan:
		// Expected - channel is closed
	default:
		t.Error("Expected stop channel to be closed")
	}
}

// TestBucketPermissionSyncManagerSchema verifies the sync table schema
func TestBucketPermissionSyncManagerSchema(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Initialize replication schema
	if err := InitReplicationSchema(db); err != nil {
		t.Fatalf("Failed to initialize replication schema: %v", err)
	}

	// Verify table exists
	var tableName string
	err := db.QueryRowContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type='table' AND name='cluster_bucket_permission_sync'
	`).Scan(&tableName)
	if err == sql.ErrNoRows {
		t.Fatal("cluster_bucket_permission_sync table not created")
	}
	if err != nil {
		t.Fatalf("Failed to query table: %v", err)
	}

	// Verify required columns exist
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(cluster_bucket_permission_sync)")
	if err != nil {
		t.Fatalf("Failed to get table info: %v", err)
	}
	defer rows.Close()

	requiredColumns := map[string]bool{
		"id":                   false,
		"permission_id":        false,
		"source_node_id":       false,
		"destination_node_id":  false,
		"permission_checksum":  false,
		"status":               false,
		"last_sync_at":         false,
		"created_at":           false,
		"updated_at":           false,
	}

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue sql.NullString

		err = rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk)
		if err != nil {
			t.Fatalf("Failed to scan column info: %v", err)
		}

		if _, exists := requiredColumns[name]; exists {
			requiredColumns[name] = true
		}
	}

	// Check all required columns were found
	for col, found := range requiredColumns {
		if !found {
			t.Errorf("Required column '%s' not found in cluster_bucket_permission_sync table", col)
		}
	}
}

// TestBucketPermissionChecksumWithExpiry tests checksum with expiry dates
func TestBucketPermissionChecksumWithExpiry(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewBucketPermissionSyncManager(db, clusterManager)

	now := time.Now().Unix()
	expiresAt := now + 86400 // 1 day from now
	userID := "user-1"

	perm1 := &BucketPermissionData{
		ID:              "perm-1",
		BucketName:      "bucket-1",
		UserID:          &userID,
		PermissionLevel: "read",
		GrantedBy:       "admin",
		GrantedAt:       now,
		ExpiresAt:       nil,
	}

	perm2 := &BucketPermissionData{
		ID:              "perm-1",
		BucketName:      "bucket-1",
		UserID:          &userID,
		PermissionLevel: "read",
		GrantedBy:       "admin",
		GrantedAt:       now,
		ExpiresAt:       &expiresAt,
	}

	checksum1 := syncManager.computePermissionChecksum(perm1)
	checksum2 := syncManager.computePermissionChecksum(perm2)

	// Different expiry should produce different checksum
	if checksum1 == checksum2 {
		t.Error("Expected different checksum when expiry is different")
	}
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
