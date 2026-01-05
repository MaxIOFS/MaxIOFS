package cluster

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestAccessKeySyncManager_ListLocalAccessKeys tests listing access keys from database
func TestAccessKeySyncManager_ListLocalAccessKeys(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create users table for foreign key constraint
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create users table: %v", err)
	}

	// Create access_keys table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS access_keys (
			access_key_id TEXT PRIMARY KEY,
			secret_access_key TEXT NOT NULL,
			user_id TEXT NOT NULL,
			status TEXT DEFAULT 'active',
			created_at INTEGER NOT NULL,
			last_used INTEGER,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create access_keys table: %v", err)
	}

	// Insert test user
	now := time.Now().Unix()
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, created_at, updated_at)
		VALUES ('user-1', 'testuser', 'hash123', ?, ?)
	`, now, now)
	if err != nil {
		t.Fatalf("Failed to insert test user: %v", err)
	}

	// Insert test access keys
	testKeys := []struct {
		accessKeyID     string
		secretAccessKey string
		userID          string
		status          string
	}{
		{"AKIA1234567890ABCDEF", "secret1", "user-1", "active"},
		{"AKIA0987654321FEDCBA", "secret2", "user-1", "active"},
		{"AKIA1111111111111111", "secret3", "user-1", "inactive"}, // inactive, should not be listed
	}

	for _, key := range testKeys {
		_, err = db.ExecContext(ctx, `
			INSERT INTO access_keys (access_key_id, secret_access_key, user_id, status, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, key.accessKeyID, key.secretAccessKey, key.userID, key.status, now)
		if err != nil {
			t.Fatalf("Failed to insert test access key: %v", err)
		}
	}

	// Create cluster manager and access key sync manager
	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewAccessKeySyncManager(db, clusterManager)

	// Test listing access keys
	keys, err := syncManager.listLocalAccessKeys(ctx)
	if err != nil {
		t.Fatalf("Failed to list access keys: %v", err)
	}

	// Should only list active keys
	if len(keys) != 2 {
		t.Errorf("Expected 2 active access keys, got %d", len(keys))
	}

	// Verify key data
	found := false
	for _, key := range keys {
		if key.AccessKeyID == "AKIA1234567890ABCDEF" {
			found = true
			if key.SecretAccessKey != "secret1" {
				t.Errorf("Expected secret 'secret1', got '%s'", key.SecretAccessKey)
			}
			if key.UserID != "user-1" {
				t.Errorf("Expected user_id 'user-1', got '%s'", key.UserID)
			}
			if key.Status != "active" {
				t.Errorf("Expected status 'active', got '%s'", key.Status)
			}
		}
	}

	if !found {
		t.Error("Expected to find access key AKIA1234567890ABCDEF")
	}
}

// TestAccessKeySyncManager_ComputeChecksum tests checksum calculation
func TestAccessKeySyncManager_ComputeChecksum(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewAccessKeySyncManager(db, clusterManager)

	now := time.Now().Unix()
	key := &AccessKeyData{
		AccessKeyID:     "AKIA1234567890ABCDEF",
		SecretAccessKey: "secretkey123",
		UserID:          "user-1",
		Status:          "active",
		CreatedAt:       now,
	}

	checksum1 := syncManager.computeAccessKeyChecksum(key)

	// Same data should produce same checksum
	checksum2 := syncManager.computeAccessKeyChecksum(key)
	if checksum1 != checksum2 {
		t.Errorf("Expected same checksum for same data, got %s and %s", checksum1, checksum2)
	}

	// Different data should produce different checksum
	key.SecretAccessKey = "differentsecret"
	checksum3 := syncManager.computeAccessKeyChecksum(key)
	if checksum1 == checksum3 {
		t.Error("Expected different checksum for different data")
	}
}

// TestAccessKeySyncManager_NeedsSynchronization tests sync status checking
func TestAccessKeySyncManager_NeedsSynchronization(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Initialize cluster schema (includes sync tables)
	if err := InitReplicationSchema(db); err != nil {
		t.Fatalf("Failed to initialize replication schema: %v", err)
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewAccessKeySyncManager(db, clusterManager)

	// Test 1: Never synced before - should need sync
	needsSync, err := syncManager.needsSynchronization(ctx, "key-1", "node-1", "checksum123")
	if err != nil {
		t.Fatalf("Failed to check sync status: %v", err)
	}
	if !needsSync {
		t.Error("Expected to need synchronization for new key")
	}

	// Test 2: Update sync status
	err = syncManager.updateSyncStatus(ctx, "key-1", "source-node", "node-1", "checksum123")
	if err != nil {
		t.Fatalf("Failed to update sync status: %v", err)
	}

	// Test 3: Same checksum - should not need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "key-1", "node-1", "checksum123")
	if err != nil {
		t.Fatalf("Failed to check sync status: %v", err)
	}
	if needsSync {
		t.Error("Expected not to need synchronization with same checksum")
	}

	// Test 4: Different checksum - should need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "key-1", "node-1", "checksum456")
	if err != nil {
		t.Fatalf("Failed to check sync status: %v", err)
	}
	if !needsSync {
		t.Error("Expected to need synchronization with different checksum")
	}
}

// TestAccessKeySyncManager_UpdateSyncStatus tests updating sync status
func TestAccessKeySyncManager_UpdateSyncStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Initialize cluster schema
	if err := InitReplicationSchema(db); err != nil {
		t.Fatalf("Failed to initialize replication schema: %v", err)
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewAccessKeySyncManager(db, clusterManager)

	// Test 1: Insert new sync status
	err := syncManager.updateSyncStatus(ctx, "key-1", "source-node", "dest-node", "checksum1")
	if err != nil {
		t.Fatalf("Failed to insert sync status: %v", err)
	}

	// Verify it was inserted
	var checksum string
	err = db.QueryRowContext(ctx, `
		SELECT key_checksum FROM cluster_access_key_sync
		WHERE access_key_id = ? AND destination_node_id = ?
	`, "key-1", "dest-node").Scan(&checksum)
	if err != nil {
		t.Fatalf("Failed to query sync status: %v", err)
	}
	if checksum != "checksum1" {
		t.Errorf("Expected checksum 'checksum1', got '%s'", checksum)
	}

	// Test 2: Update existing sync status (ON CONFLICT DO UPDATE)
	err = syncManager.updateSyncStatus(ctx, "key-1", "source-node", "dest-node", "checksum2")
	if err != nil {
		t.Fatalf("Failed to update sync status: %v", err)
	}

	// Verify it was updated
	err = db.QueryRowContext(ctx, `
		SELECT key_checksum FROM cluster_access_key_sync
		WHERE access_key_id = ? AND destination_node_id = ?
	`, "key-1", "dest-node").Scan(&checksum)
	if err != nil {
		t.Fatalf("Failed to query updated sync status: %v", err)
	}
	if checksum != "checksum2" {
		t.Errorf("Expected checksum 'checksum2', got '%s'", checksum)
	}

	// Should still have only 1 record (not 2)
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_access_key_sync
		WHERE access_key_id = ? AND destination_node_id = ?
	`, "key-1", "dest-node").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count sync records: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 sync record, got %d", count)
	}
}

// TestAccessKeySyncManager_Stop tests stopping the sync manager
func TestAccessKeySyncManager_Stop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewAccessKeySyncManager(db, clusterManager)

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

// TestAccessKeySyncManagerSchema verifies the sync table schema
func TestAccessKeySyncManagerSchema(t *testing.T) {
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
		WHERE type='table' AND name='cluster_access_key_sync'
	`).Scan(&tableName)
	if err == sql.ErrNoRows {
		t.Fatal("cluster_access_key_sync table not created")
	}
	if err != nil {
		t.Fatalf("Failed to query table: %v", err)
	}

	// Verify required columns exist
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(cluster_access_key_sync)")
	if err != nil {
		t.Fatalf("Failed to get table info: %v", err)
	}
	defer rows.Close()

	requiredColumns := map[string]bool{
		"id":                   false,
		"access_key_id":        false,
		"source_node_id":       false,
		"destination_node_id":  false,
		"key_checksum":         false,
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
			t.Errorf("Required column '%s' not found in cluster_access_key_sync table", col)
		}
	}
}
