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

// TestAccessKeySyncManager_SendAccessKeyToNode tests sending access key to a node via HTTP
func TestAccessKeySyncManager_SendAccessKeyToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create mock HTTP server to simulate remote node
	receivedData := make(chan *AccessKeyData, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Verify URL path
		if r.URL.Path != "/api/internal/cluster/access-key-sync" {
			t.Errorf("Expected path /api/internal/cluster/access-key-sync, got %s", r.URL.Path)
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
		var keyData AccessKeyData
		if err := json.NewDecoder(r.Body).Decode(&keyData); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Send received data to channel for verification
		receivedData <- &keyData

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

	// Create test access key
	now := time.Now().Unix()
	accessKey := &AccessKeyData{
		AccessKeyID:     "AKIA1234567890ABCDEF",
		SecretAccessKey: "testsecretkey123",
		UserID:          "user-123",
		Status:          "active",
		CreatedAt:       now,
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewAccessKeySyncManager(db, clusterManager)

	// Send access key to node
	err := syncManager.sendAccessKeyToNode(ctx, accessKey, node, "source-node", "test-token")
	if err != nil {
		t.Fatalf("Failed to send access key to node: %v", err)
	}

	// Verify data was received correctly
	select {
	case received := <-receivedData:
		if received.AccessKeyID != accessKey.AccessKeyID {
			t.Errorf("Expected AccessKeyID %s, got %s", accessKey.AccessKeyID, received.AccessKeyID)
		}
		if received.SecretAccessKey != accessKey.SecretAccessKey {
			t.Errorf("Expected SecretAccessKey %s, got %s", accessKey.SecretAccessKey, received.SecretAccessKey)
		}
		if received.UserID != accessKey.UserID {
			t.Errorf("Expected UserID %s, got %s", accessKey.UserID, received.UserID)
		}
		if received.Status != accessKey.Status {
			t.Errorf("Expected Status %s, got %s", accessKey.Status, received.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for access key data")
	}
}

// TestAccessKeySyncManager_SendAccessKeyToNode_ServerError tests handling of server errors
func TestAccessKeySyncManager_SendAccessKeyToNode_ServerError(t *testing.T) {
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

	accessKey := &AccessKeyData{
		AccessKeyID:     "AKIA1234567890ABCDEF",
		SecretAccessKey: "testsecretkey123",
		UserID:          "user-123",
		Status:          "active",
		CreatedAt:       time.Now().Unix(),
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewAccessKeySyncManager(db, clusterManager)

	// Should return error
	err := syncManager.sendAccessKeyToNode(ctx, accessKey, node, "source-node", "test-token")
	if err == nil {
		t.Fatal("Expected error from server, got nil")
	}
}

// TestAccessKeySyncManager_SyncAccessKeyToNode tests syncing a single key to a node
func TestAccessKeySyncManager_SyncAccessKeyToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Initialize both cluster schema and replication schema
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

	accessKey := &AccessKeyData{
		AccessKeyID:     "AKIA1234567890ABCDEF",
		SecretAccessKey: "testsecretkey123",
		UserID:          "user-123",
		Status:          "active",
		CreatedAt:       time.Now().Unix(),
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewAccessKeySyncManager(db, clusterManager)

	// First sync - should succeed
	err = syncManager.syncAccessKeyToNode(ctx, accessKey, node, "local-node")
	if err != nil {
		t.Fatalf("Failed to sync access key: %v", err)
	}

	// Verify sync status was recorded
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_access_key_sync
		WHERE access_key_id = ? AND destination_node_id = ?
	`, accessKey.AccessKeyID, node.ID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query sync status: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 sync record, got %d", count)
	}

	// Second sync with same checksum - should skip (not call HTTP server again)
	err = syncManager.syncAccessKeyToNode(ctx, accessKey, node, "local-node")
	if err != nil {
		t.Fatalf("Failed on second sync: %v", err)
	}
}

// TestAccessKeySyncManager_SyncLoop tests the background sync loop
func TestAccessKeySyncManager_SyncLoop(t *testing.T) {
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

	// Create users and access_keys tables
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT,
			password_hash TEXT,
			created_at INTEGER,
			updated_at INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create users table: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS access_keys (
			access_key_id TEXT PRIMARY KEY,
			secret_access_key TEXT,
			user_id TEXT,
			status TEXT DEFAULT 'active',
			created_at INTEGER,
			last_used INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create access_keys table: %v", err)
	}

	// Insert test user and access key
	now := time.Now().Unix()
	_, err = db.ExecContext(ctx, `INSERT INTO users VALUES ('user-1', 'test', 'hash', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("Failed to insert user: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO access_keys VALUES ('AKIA1234567890ABCDEF', 'secret', 'user-1', 'active', ?, NULL)
	`, now)
	if err != nil {
		t.Fatalf("Failed to insert access key: %v", err)
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewAccessKeySyncManager(db, clusterManager)

	// Run sync loop with very short interval
	go syncManager.syncLoop(ctx, 100*time.Millisecond)

	// Wait for at least 2 sync iterations
	time.Sleep(300 * time.Millisecond)

	// Stop the loop
	cancel()

	// Give it time to stop
	time.Sleep(100 * time.Millisecond)

	// Verify sync was called (at least once from immediate run, possibly more from ticker)
	if syncCount < 1 {
		t.Errorf("Expected at least 1 sync call, got %d", syncCount)
	}
}

// TestAccessKeySyncManager_Start tests starting the sync manager
func TestAccessKeySyncManager_Start(t *testing.T) {
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

		// Insert local node config for cluster operations
		_, err := db.ExecContext(ctx, `
			INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
			VALUES ('local-node', 'Local', 'local-token', 0)
		`)
		if err != nil {
			t.Fatalf("Failed to insert cluster config: %v", err)
		}

		clusterManager := NewManager(db, "http://localhost:8080")
		syncManager := NewAccessKeySyncManager(db, clusterManager)

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
			VALUES ('auto_access_key_sync_enabled', 'true', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		if err != nil {
			t.Fatalf("Failed to enable auto sync: %v", err)
		}

		_, err = db.ExecContext(ctx, `
			INSERT OR REPLACE INTO cluster_global_config (key, value, created_at, updated_at)
			VALUES ('access_key_sync_interval_seconds', '1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		if err != nil {
			t.Fatalf("Failed to set sync interval: %v", err)
		}

		clusterManager := NewManager(db, "http://localhost:8080")
		syncManager := NewAccessKeySyncManager(db, clusterManager)

		// Start should launch goroutine and return immediately
		syncManager.Start(ctx)

		// Give it a moment to start
		time.Sleep(100 * time.Millisecond)

		// Stop it
		syncManager.Stop()
	})
}
