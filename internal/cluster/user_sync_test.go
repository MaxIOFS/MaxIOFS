package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserSyncManager_NewUserSyncManager(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewUserSyncManager(db, clusterManager)

	assert.NotNil(t, syncManager)
	assert.NotNil(t, syncManager.db)
	assert.NotNil(t, syncManager.clusterManager)
	assert.NotNil(t, syncManager.proxyClient)
	assert.NotNil(t, syncManager.stopChan)
	assert.NotNil(t, syncManager.log)
}

func TestUserSyncManager_ListLocalUsers(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create users table
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			display_name TEXT,
			email TEXT,
			status TEXT DEFAULT 'active',
			tenant_id TEXT,
			roles TEXT DEFAULT '',
			policies TEXT DEFAULT '',
			metadata TEXT DEFAULT '{}',
			failed_login_attempts INTEGER DEFAULT 0,
			locked_until INTEGER DEFAULT 0,
			last_failed_login INTEGER DEFAULT 0,
			theme_preference TEXT DEFAULT 'light',
			language_preference TEXT DEFAULT 'en',
			auth_provider TEXT NOT NULL DEFAULT 'local',
			external_id TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	require.NoError(t, err)

	// Insert test users
	now := time.Now().Unix()
	testUsers := []struct {
		id       string
		username string
		status   string
	}{
		{"user-1", "testuser1", "active"},
		{"user-2", "testuser2", "active"},
		{"user-3", "testuser3", "deleted"}, // should be excluded
	}

	for _, user := range testUsers {
		_, err = db.ExecContext(ctx, `
			INSERT INTO users (id, username, password_hash, display_name, email, status, tenant_id, roles, policies, metadata, failed_login_attempts, locked_until, last_failed_login, theme_preference, language_preference, created_at, updated_at)
			VALUES (?, ?, 'hash', '', '', ?, '', '', '', '{}', 0, 0, 0, 'light', 'en', ?, ?)
		`, user.id, user.username, user.status, now, now)
		require.NoError(t, err)
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewUserSyncManager(db, clusterManager)

	users, err := syncManager.listLocalUsers(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, len(users), "Should only list non-deleted users")

	// Verify user data
	found := false
	for _, user := range users {
		if user.ID == "user-1" {
			found = true
			assert.Equal(t, "testuser1", user.Username)
			assert.Equal(t, "active", user.Status)
		}
	}
	assert.True(t, found, "Should find user-1")
}

func TestUserSyncManager_ComputeChecksum(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewUserSyncManager(db, clusterManager)

	now := time.Now().Unix()
	user := &UserData{
		ID:           "user-1",
		Username:     "testuser",
		PasswordHash: "hash123",
		Email:        "test@example.com",
		Status:       "active",
		CreatedAt:    now,
	}

	checksum1 := syncManager.computeUserChecksum(user)
	checksum2 := syncManager.computeUserChecksum(user)
	assert.Equal(t, checksum1, checksum2, "Same data should produce same checksum")

	// Different data should produce different checksum
	user.Email = "different@example.com"
	checksum3 := syncManager.computeUserChecksum(user)
	assert.NotEqual(t, checksum1, checksum3, "Different data should produce different checksum")
}

func TestUserSyncManager_NeedsSynchronization(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	require.NoError(t, InitReplicationSchema(db))

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewUserSyncManager(db, clusterManager)

	// Test 1: Never synced before - should need sync
	needsSync, err := syncManager.needsSynchronization(ctx, "user-1", "node-1", "checksum123")
	require.NoError(t, err)
	assert.True(t, needsSync)

	// Test 2: Update sync status
	err = syncManager.updateSyncStatus(ctx, "user-1", "source-node", "node-1", "checksum123")
	require.NoError(t, err)

	// Test 3: Same checksum - should not need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "user-1", "node-1", "checksum123")
	require.NoError(t, err)
	assert.False(t, needsSync)

	// Test 4: Different checksum - should need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "user-1", "node-1", "checksum456")
	require.NoError(t, err)
	assert.True(t, needsSync)
}

func TestUserSyncManager_UpdateSyncStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	require.NoError(t, InitReplicationSchema(db))

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewUserSyncManager(db, clusterManager)

	// Insert new sync status
	err := syncManager.updateSyncStatus(ctx, "user-1", "source-node", "dest-node", "checksum1")
	require.NoError(t, err)

	// Verify insertion
	var checksum string
	err = db.QueryRowContext(ctx, `
		SELECT user_checksum FROM cluster_user_sync
		WHERE user_id = ? AND destination_node_id = ?
	`, "user-1", "dest-node").Scan(&checksum)
	require.NoError(t, err)
	assert.Equal(t, "checksum1", checksum)

	// Update existing sync status
	err = syncManager.updateSyncStatus(ctx, "user-1", "source-node", "dest-node", "checksum2")
	require.NoError(t, err)

	// Verify update
	err = db.QueryRowContext(ctx, `
		SELECT user_checksum FROM cluster_user_sync
		WHERE user_id = ? AND destination_node_id = ?
	`, "user-1", "dest-node").Scan(&checksum)
	require.NoError(t, err)
	assert.Equal(t, "checksum2", checksum)

	// Should have only 1 record
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_user_sync
		WHERE user_id = ? AND destination_node_id = ?
	`, "user-1", "dest-node").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestUserSyncManager_Stop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewUserSyncManager(db, clusterManager)

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

func TestUserSyncManager_SendUserToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create mock HTTP server
	receivedData := make(chan *UserData, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/internal/cluster/user-sync", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var userData UserData
		err := json.NewDecoder(r.Body).Decode(&userData)
		require.NoError(t, err)
		receivedData <- &userData

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	node := &Node{
		ID:           "test-node-1",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	now := time.Now().Unix()
	user := &UserData{
		ID:           "user-123",
		Username:     "testuser",
		PasswordHash: "hash123",
		Email:        "test@example.com",
		Status:       "active",
		CreatedAt:    now,
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewUserSyncManager(db, clusterManager)

	err := syncManager.sendUserToNode(ctx, user, node, "source-node", "test-token")
	require.NoError(t, err)

	// Verify data was received
	select {
	case received := <-receivedData:
		assert.Equal(t, user.ID, received.ID)
		assert.Equal(t, user.Username, received.Username)
		assert.Equal(t, user.Email, received.Email)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for user data")
	}
}

func TestUserSyncManager_SendUserToNode_ServerError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	node := &Node{ID: "test-node-1", Endpoint: server.URL}
	user := &UserData{ID: "user-123", Username: "test", Status: "active", CreatedAt: time.Now().Unix()}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewUserSyncManager(db, clusterManager)

	err := syncManager.sendUserToNode(ctx, user, node, "source-node", "test-token")
	assert.Error(t, err)
}

func TestUserSyncManager_SyncUserToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	require.NoError(t, InitSchema(db))
	require.NoError(t, InitReplicationSchema(db))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
		VALUES ('local-node', 'Local', 'local-token', 1)
	`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status)
		VALUES ('local-node', 'Local', 'http://localhost:8080', 'local-token', 'healthy')
	`)
	require.NoError(t, err)

	node := &Node{ID: "test-node-1", Endpoint: server.URL, HealthStatus: "healthy"}
	user := &UserData{ID: "user-123", Username: "test", Status: "active", CreatedAt: time.Now().Unix()}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewUserSyncManager(db, clusterManager)

	err = syncManager.syncUserToNode(ctx, user, node, "local-node")
	require.NoError(t, err)

	// Verify sync status was recorded
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_user_sync WHERE user_id = ? AND destination_node_id = ?
	`, user.ID, node.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestUserSyncManager_SyncLoop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, InitSchema(db))
	require.NoError(t, InitReplicationSchema(db))

	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
		VALUES ('local-node', 'Local', 'local-token', 1)
	`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status)
		VALUES ('local-node', 'Local', 'http://localhost:8080', 'local-token', 'healthy')
	`)
	require.NoError(t, err)

	syncCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		syncCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status)
		VALUES ('remote-node', 'Remote', ?, 'remote-token', 'healthy')
	`, server.URL)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT,
			password_hash TEXT,
			display_name TEXT DEFAULT '',
			email TEXT DEFAULT '',
			status TEXT DEFAULT 'active',
			tenant_id TEXT DEFAULT '',
			roles TEXT DEFAULT '',
			policies TEXT DEFAULT '',
			metadata TEXT DEFAULT '{}',
			failed_login_attempts INTEGER DEFAULT 0,
			locked_until INTEGER DEFAULT 0,
			last_failed_login INTEGER DEFAULT 0,
			theme_preference TEXT DEFAULT 'light',
			language_preference TEXT DEFAULT 'en',
			auth_provider TEXT NOT NULL DEFAULT 'local',
			external_id TEXT,
			created_at INTEGER,
			updated_at INTEGER
		)
	`)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, status, created_at, updated_at)
		VALUES ('user-1', 'test', 'hash', 'active', ?, ?)
	`, now, now)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewUserSyncManager(db, clusterManager)

	go syncManager.syncLoop(ctx, 100*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	assert.GreaterOrEqual(t, syncCount, 1, "Expected at least 1 sync call")
}

func TestUserSyncManager_Start(t *testing.T) {
	t.Run("disabled auto sync", func(t *testing.T) {
		db, cleanup := setupTestDB(t)
		defer cleanup()

		ctx := context.Background()

		require.NoError(t, InitSchema(db))
		require.NoError(t, InitReplicationSchema(db))

		_, err := db.ExecContext(ctx, `
			INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
			VALUES ('local-node', 'Local', 'local-token', 0)
		`)
		require.NoError(t, err)

		clusterManager := NewManager(db, "http://localhost:8080")
		syncManager := NewUserSyncManager(db, clusterManager)

		syncManager.Start(ctx) // Should return immediately
	})

	t.Run("enabled auto sync", func(t *testing.T) {
		db, cleanup := setupTestDB(t)
		defer cleanup()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		require.NoError(t, InitSchema(db))
		require.NoError(t, InitReplicationSchema(db))

		_, err := db.ExecContext(ctx, `
			INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
			VALUES ('local-node', 'Local', 'local-token', 0)
		`)
		require.NoError(t, err)

		_, err = db.ExecContext(ctx, `
			INSERT OR REPLACE INTO cluster_global_config (key, value, created_at, updated_at)
			VALUES ('auto_user_sync_enabled', 'true', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		require.NoError(t, err)

		_, err = db.ExecContext(ctx, `
			INSERT OR REPLACE INTO cluster_global_config (key, value, created_at, updated_at)
			VALUES ('user_sync_interval_seconds', '1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		require.NoError(t, err)

		clusterManager := NewManager(db, "http://localhost:8080")
		syncManager := NewUserSyncManager(db, clusterManager)

		syncManager.Start(ctx)
		time.Sleep(100 * time.Millisecond)
		syncManager.Stop()
	})
}
