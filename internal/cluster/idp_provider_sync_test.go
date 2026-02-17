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

func TestIDPProviderSyncManager_New(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewIDPProviderSyncManager(db, clusterManager)

	assert.NotNil(t, syncManager)
	assert.NotNil(t, syncManager.db)
	assert.NotNil(t, syncManager.clusterManager)
	assert.NotNil(t, syncManager.proxyClient)
	assert.NotNil(t, syncManager.stopChan)
	assert.NotNil(t, syncManager.log)
}

func TestIDPProviderSyncManager_ListLocalProviders(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create identity_providers table
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS identity_providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			tenant_id TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			config TEXT NOT NULL,
			created_by TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	require.NoError(t, err)

	now := time.Now().Unix()

	// Insert test providers
	_, err = db.ExecContext(ctx, `
		INSERT INTO identity_providers (id, name, type, tenant_id, status, config, created_by, created_at, updated_at)
		VALUES ('idp-1', 'LDAP Corp', 'ldap', '', 'active', '{"host":"ldap.example.com"}', 'admin', ?, ?)
	`, now, now)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO identity_providers (id, name, type, tenant_id, status, config, created_by, created_at, updated_at)
		VALUES ('idp-2', 'Google OAuth', 'oauth', 'tenant-1', 'active', '{"client_id":"abc"}', 'admin', ?, ?)
	`, now, now)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewIDPProviderSyncManager(db, clusterManager)

	providers, err := syncManager.listLocalProviders(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, len(providers))

	// Verify provider data
	found := false
	for _, p := range providers {
		if p.ID == "idp-1" {
			found = true
			assert.Equal(t, "LDAP Corp", p.Name)
			assert.Equal(t, "ldap", p.Type)
			assert.Equal(t, "active", p.Status)
		}
	}
	assert.True(t, found, "Should find idp-1")
}

func TestIDPProviderSyncManager_ComputeChecksum(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewIDPProviderSyncManager(db, clusterManager)

	now := time.Now().Unix()
	provider := &IDPProviderData{
		ID:        "idp-1",
		Name:      "LDAP Corp",
		Type:      "ldap",
		Status:    "active",
		Config:    `{"host":"ldap.example.com"}`,
		CreatedBy: "admin",
		UpdatedAt: now,
	}

	checksum1 := syncManager.computeProviderChecksum(provider)
	checksum2 := syncManager.computeProviderChecksum(provider)
	assert.Equal(t, checksum1, checksum2, "Same data should produce same checksum")

	provider.Name = "Updated LDAP"
	checksum3 := syncManager.computeProviderChecksum(provider)
	assert.NotEqual(t, checksum1, checksum3, "Different data should produce different checksum")
}

func TestIDPProviderSyncManager_NeedsSynchronization(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitReplicationSchema(db))

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewIDPProviderSyncManager(db, clusterManager)

	// Never synced - should need sync
	needsSync, err := syncManager.needsSynchronization(ctx, "idp-1", "node-1", "checksum123")
	require.NoError(t, err)
	assert.True(t, needsSync)

	// Update sync status
	err = syncManager.updateSyncStatus(ctx, "idp-1", "source-node", "node-1", "checksum123")
	require.NoError(t, err)

	// Same checksum - should not need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "idp-1", "node-1", "checksum123")
	require.NoError(t, err)
	assert.False(t, needsSync)

	// Different checksum - should need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "idp-1", "node-1", "checksum456")
	require.NoError(t, err)
	assert.True(t, needsSync)
}

func TestIDPProviderSyncManager_UpdateSyncStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitReplicationSchema(db))

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewIDPProviderSyncManager(db, clusterManager)

	// Insert sync status
	err := syncManager.updateSyncStatus(ctx, "idp-1", "source-node", "dest-node", "checksum1")
	require.NoError(t, err)

	// Verify insertion
	var checksum string
	err = db.QueryRowContext(ctx, `
		SELECT provider_checksum FROM cluster_idp_provider_sync
		WHERE provider_id = ? AND destination_node_id = ?
	`, "idp-1", "dest-node").Scan(&checksum)
	require.NoError(t, err)
	assert.Equal(t, "checksum1", checksum)

	// Update existing
	err = syncManager.updateSyncStatus(ctx, "idp-1", "source-node", "dest-node", "checksum2")
	require.NoError(t, err)

	err = db.QueryRowContext(ctx, `
		SELECT provider_checksum FROM cluster_idp_provider_sync
		WHERE provider_id = ? AND destination_node_id = ?
	`, "idp-1", "dest-node").Scan(&checksum)
	require.NoError(t, err)
	assert.Equal(t, "checksum2", checksum)

	// Should have only 1 record
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_idp_provider_sync
		WHERE provider_id = ? AND destination_node_id = ?
	`, "idp-1", "dest-node").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestIDPProviderSyncManager_Stop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewIDPProviderSyncManager(db, clusterManager)

	syncManager.Stop()

	select {
	case <-syncManager.stopChan:
		// Expected
	default:
		t.Error("Expected stop channel to be closed")
	}
}

func TestIDPProviderSyncManager_SendProviderToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	receivedData := make(chan *IDPProviderData, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/internal/cluster/idp-provider-sync", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var data IDPProviderData
		err := json.NewDecoder(r.Body).Decode(&data)
		require.NoError(t, err)
		receivedData <- &data

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	node := &Node{ID: "test-node-1", Endpoint: server.URL, HealthStatus: "healthy"}
	provider := &IDPProviderData{
		ID:        "idp-1",
		Name:      "Test LDAP",
		Type:      "ldap",
		Status:    "active",
		Config:    `{"host":"ldap.example.com"}`,
		CreatedBy: "admin",
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewIDPProviderSyncManager(db, clusterManager)

	err := syncManager.sendProviderToNode(ctx, provider, node, "source-node", "test-token")
	require.NoError(t, err)

	select {
	case received := <-receivedData:
		assert.Equal(t, provider.ID, received.ID)
		assert.Equal(t, provider.Name, received.Name)
		assert.Equal(t, provider.Type, received.Type)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for provider data")
	}
}

func TestIDPProviderSyncManager_SendProviderToNode_ServerError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	node := &Node{ID: "test-node-1", Endpoint: server.URL}
	provider := &IDPProviderData{ID: "idp-1", Name: "Test", Type: "ldap", Status: "active", Config: "{}", CreatedBy: "admin", CreatedAt: time.Now().Unix()}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewIDPProviderSyncManager(db, clusterManager)

	err := syncManager.sendProviderToNode(ctx, provider, node, "source-node", "test-token")
	assert.Error(t, err)
}

func TestIDPProviderSyncManager_SyncProviderToNode(t *testing.T) {
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
	provider := &IDPProviderData{
		ID:        "idp-1",
		Name:      "Test",
		Type:      "ldap",
		Status:    "active",
		Config:    "{}",
		CreatedBy: "admin",
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewIDPProviderSyncManager(db, clusterManager)

	err = syncManager.syncProviderToNode(ctx, provider, node, "local-node")
	require.NoError(t, err)

	// Verify sync status was recorded
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_idp_provider_sync WHERE provider_id = ? AND destination_node_id = ?
	`, provider.ID, node.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestIDPProviderSyncManager_SyncLoop(t *testing.T) {
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

	// Create identity_providers table and insert test data
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS identity_providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			tenant_id TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			config TEXT NOT NULL,
			created_by TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = db.ExecContext(ctx, `
		INSERT INTO identity_providers (id, name, type, status, config, created_by, created_at, updated_at)
		VALUES ('idp-1', 'Test', 'ldap', 'active', '{}', 'admin', ?, ?)
	`, now, now)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewIDPProviderSyncManager(db, clusterManager)

	go syncManager.syncLoop(ctx, 100*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	assert.GreaterOrEqual(t, syncCount, 1, "Expected at least 1 sync call")
}

func TestIDPProviderSyncManager_Start(t *testing.T) {
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
		syncManager := NewIDPProviderSyncManager(db, clusterManager)

		syncManager.Start(ctx) // Should return immediately (auto sync disabled)
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
			VALUES ('auto_idp_provider_sync_enabled', 'true', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		require.NoError(t, err)

		_, err = db.ExecContext(ctx, `
			INSERT OR REPLACE INTO cluster_global_config (key, value, created_at, updated_at)
			VALUES ('idp_provider_sync_interval_seconds', '1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		require.NoError(t, err)

		clusterManager := NewManager(db, "http://localhost:8080")
		syncManager := NewIDPProviderSyncManager(db, clusterManager)

		syncManager.Start(ctx)
		time.Sleep(100 * time.Millisecond)
		syncManager.Stop()
	})
}

func TestIDPProviderSyncManager_SendDeletionToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	receivedID := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/internal/cluster/idp-provider-delete-sync", r.URL.Path)

		var data map[string]string
		json.NewDecoder(r.Body).Decode(&data)
		receivedID <- data["id"]

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	node := &Node{ID: "remote-node", Endpoint: server.URL, HealthStatus: "healthy"}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewIDPProviderSyncManager(db, clusterManager)

	err := syncManager.sendDeletionToNode(ctx, "idp-1", node, "local-node", "test-token")
	require.NoError(t, err)

	select {
	case id := <-receivedID:
		assert.Equal(t, "idp-1", id)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for deletion data")
	}
}
