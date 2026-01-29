package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// TestBucketPermissionSyncManager_SendPermissionToNode tests sending permission to a node via HTTP
func TestBucketPermissionSyncManager_SendPermissionToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create mock HTTP server to simulate remote node
	receivedData := make(chan *BucketPermissionData, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Verify URL path
		if r.URL.Path != "/api/internal/cluster/bucket-permission-sync" {
			t.Errorf("Expected path /api/internal/cluster/bucket-permission-sync, got %s", r.URL.Path)
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		// Verify Content-Type
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
			http.Error(w, "Invalid content type", http.StatusBadRequest)
			return
		}

		// Parse request body
		var permData BucketPermissionData
		if err := json.NewDecoder(r.Body).Decode(&permData); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Send received data to channel for verification
		receivedData <- &permData

		// Respond with success
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	// Create node pointing to mock server
	node := &Node{
		ID:           "test-node-1",
		Name:         "Test Node 1",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	// Create test permission
	now := time.Now().Unix()
	userID := "user-123"
	permission := &BucketPermissionData{
		ID:              "perm-123",
		BucketName:      "test-bucket",
		UserID:          &userID,
		TenantID:        nil,
		PermissionLevel: "read",
		GrantedBy:       "admin",
		GrantedAt:       now,
		ExpiresAt:       nil,
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewBucketPermissionSyncManager(db, clusterManager)

	// Send permission to node
	err := syncManager.sendPermissionToNode(ctx, permission, node, "source-node", "test-token")
	if err != nil {
		t.Fatalf("Failed to send permission to node: %v", err)
	}

	// Verify data was received correctly
	select {
	case received := <-receivedData:
		if received.ID != permission.ID {
			t.Errorf("Expected ID %s, got %s", permission.ID, received.ID)
		}
		if received.BucketName != permission.BucketName {
			t.Errorf("Expected BucketName %s, got %s", permission.BucketName, received.BucketName)
		}
		if received.UserID == nil || *received.UserID != *permission.UserID {
			t.Error("UserID mismatch")
		}
		if received.PermissionLevel != permission.PermissionLevel {
			t.Errorf("Expected PermissionLevel %s, got %s", permission.PermissionLevel, received.PermissionLevel)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for permission data")
	}
}

// TestBucketPermissionSyncManager_SendPermissionToNode_ServerError tests handling of server errors
func TestBucketPermissionSyncManager_SendPermissionToNode_ServerError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create mock HTTP server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	node := &Node{
		ID:       "test-node-1",
		Endpoint: server.URL,
	}

	userID := "user-123"
	permission := &BucketPermissionData{
		ID:              "perm-123",
		BucketName:      "test-bucket",
		UserID:          &userID,
		PermissionLevel: "read",
		GrantedBy:       "admin",
		GrantedAt:       time.Now().Unix(),
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewBucketPermissionSyncManager(db, clusterManager)

	// Should return error
	err := syncManager.sendPermissionToNode(ctx, permission, node, "source-node", "test-token")
	if err == nil {
		t.Fatal("Expected error from server, got nil")
	}
}

// TestBucketPermissionSyncManager_SyncPermissionToNode tests syncing a single permission to a node
func TestBucketPermissionSyncManager_SyncPermissionToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Initialize both schemas
	if err := InitSchema(db); err != nil {
		t.Fatalf("Failed to initialize cluster schema: %v", err)
	}
	if err := InitReplicationSchema(db); err != nil {
		t.Fatalf("Failed to initialize replication schema: %v", err)
	}

	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	// Insert cluster config for local node
	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
		VALUES ('local-node', 'Local Node', 'local-token', 1)
	`)
	if err != nil {
		t.Fatalf("Failed to insert cluster config: %v", err)
	}

	// Also insert local node into cluster_nodes table for GetNodeToken
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status)
		VALUES ('local-node', 'Local Node', 'http://localhost:8080', 'local-token', 'healthy')
	`)
	if err != nil {
		t.Fatalf("Failed to insert local node: %v", err)
	}

	node := &Node{
		ID:           "test-node-1",
		Name:         "Test Node 1",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	userID := "user-123"
	permission := &BucketPermissionData{
		ID:              "perm-123",
		BucketName:      "test-bucket",
		UserID:          &userID,
		PermissionLevel: "read",
		GrantedBy:       "admin",
		GrantedAt:       time.Now().Unix(),
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewBucketPermissionSyncManager(db, clusterManager)

	// First sync - should succeed
	err = syncManager.syncPermissionToNode(ctx, permission, node, "local-node")
	if err != nil {
		t.Fatalf("Failed to sync permission: %v", err)
	}

	// Verify sync status was recorded
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_bucket_permission_sync
		WHERE permission_id = ? AND destination_node_id = ?
	`, permission.ID, node.ID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query sync status: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 sync record, got %d", count)
	}

	// Second sync with same checksum - should skip
	err = syncManager.syncPermissionToNode(ctx, permission, node, "local-node")
	if err != nil {
		t.Fatalf("Failed on second sync: %v", err)
	}
}

// TestBucketPermissionSyncManager_SyncLoop tests the background sync loop
func TestBucketPermissionSyncManager_SyncLoop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize both schemas
	if err := InitSchema(db); err != nil {
		t.Fatalf("Failed to initialize cluster schema: %v", err)
	}
	if err := InitReplicationSchema(db); err != nil {
		t.Fatalf("Failed to initialize replication schema: %v", err)
	}

	// Insert cluster config with cluster enabled
	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
		VALUES ('local-node', 'Local', 'local-token', 1)
	`)
	if err != nil {
		t.Fatalf("Failed to insert cluster config: %v", err)
	}

	// Insert local node into cluster_nodes
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status)
		VALUES ('local-node', 'Local', 'http://localhost:8080', 'local-token', 'healthy')
	`)
	if err != nil {
		t.Fatalf("Failed to insert local node into cluster_nodes: %v", err)
	}

	// Create mock HTTP server
	syncCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		syncCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Insert remote node
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status)
		VALUES ('remote-node', 'Remote', ?, 'remote-token', 'healthy')
	`, server.URL)
	if err != nil {
		t.Fatalf("Failed to insert remote node: %v", err)
	}

	// Create required tables for permissions
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT,
			created_at INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create users table: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS tenants (
			id TEXT PRIMARY KEY,
			name TEXT,
			created_at INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create tenants table: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS bucket_permissions (
			id TEXT PRIMARY KEY,
			bucket_name TEXT,
			user_id TEXT,
			tenant_id TEXT,
			permission_level TEXT,
			granted_by TEXT,
			granted_at INTEGER,
			expires_at INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create bucket_permissions table: %v", err)
	}

	// Insert test user and permission
	now := time.Now().Unix()
	_, err = db.ExecContext(ctx, `INSERT INTO users VALUES ('user-1', 'test', ?)`, now)
	if err != nil {
		t.Fatalf("Failed to insert user: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO bucket_permissions VALUES ('perm-1', 'bucket-1', 'user-1', NULL, 'read', 'admin', ?, NULL)
	`, now)
	if err != nil {
		t.Fatalf("Failed to insert permission: %v", err)
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewBucketPermissionSyncManager(db, clusterManager)

	// Run sync loop with very short interval
	go syncManager.syncLoop(ctx, 100*time.Millisecond)

	// Wait for at least 2 sync iterations
	time.Sleep(300 * time.Millisecond)

	// Stop the loop
	cancel()

	// Give it time to stop
	time.Sleep(100 * time.Millisecond)

	// Verify sync was called
	if syncCount < 1 {
		t.Errorf("Expected at least 1 sync call, got %d", syncCount)
	}
}

// TestBucketPermissionSyncManager_Start tests starting the sync manager
func TestBucketPermissionSyncManager_Start(t *testing.T) {
	t.Run("disabled auto sync", func(t *testing.T) {
		db, cleanup := setupTestDB(t)
		defer cleanup()

		ctx := context.Background()

		// Initialize schemas
		if err := InitSchema(db); err != nil {
			t.Fatalf("Failed to initialize cluster schema: %v", err)
		}
		if err := InitReplicationSchema(db); err != nil {
			t.Fatalf("Failed to initialize replication schema: %v", err)
		}

		// Insert local node config
		_, err := db.ExecContext(ctx, `
			INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
			VALUES ('local-node', 'Local', 'local-token', 0)
		`)
		if err != nil {
			t.Fatalf("Failed to insert cluster config: %v", err)
		}

		clusterManager := NewManager(db, "http://localhost:8080")
		syncManager := NewBucketPermissionSyncManager(db, clusterManager)

		// Don't enable auto sync - Start() should return immediately
		syncManager.Start(ctx)

		// If we get here, it means Start() returned (didn't block forever)
		// This is the expected behavior when auto sync is disabled
	})

	t.Run("enabled auto sync", func(t *testing.T) {
		db, cleanup := setupTestDB(t)
		defer cleanup()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Initialize schemas
		if err := InitSchema(db); err != nil {
			t.Fatalf("Failed to initialize cluster schema: %v", err)
		}
		if err := InitReplicationSchema(db); err != nil {
			t.Fatalf("Failed to initialize replication schema: %v", err)
		}

		// Insert local node config
		_, err := db.ExecContext(ctx, `
			INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
			VALUES ('local-node', 'Local', 'local-token', 0)
		`)
		if err != nil {
			t.Fatalf("Failed to insert cluster config: %v", err)
		}

		// Enable auto sync
		_, err = db.ExecContext(ctx, `
			INSERT OR REPLACE INTO cluster_global_config (key, value, created_at, updated_at)
			VALUES ('auto_bucket_permission_sync_enabled', 'true', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		if err != nil {
			t.Fatalf("Failed to enable auto sync: %v", err)
		}

		_, err = db.ExecContext(ctx, `
			INSERT OR REPLACE INTO cluster_global_config (key, value, created_at, updated_at)
			VALUES ('bucket_permission_sync_interval_seconds', '1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		if err != nil {
			t.Fatalf("Failed to set sync interval: %v", err)
		}

		clusterManager := NewManager(db, "http://localhost:8080")
		syncManager := NewBucketPermissionSyncManager(db, clusterManager)

		// Start should launch goroutine and return immediately
		syncManager.Start(ctx)

		// Give it a moment to start
		time.Sleep(100 * time.Millisecond)

		// Stop it
		syncManager.Stop()
	})
}
