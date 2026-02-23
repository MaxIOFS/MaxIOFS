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

func TestGroupMappingSyncManager_New(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewGroupMappingSyncManager(db, clusterManager)

	assert.NotNil(t, syncManager)
	assert.NotNil(t, syncManager.db)
	assert.NotNil(t, syncManager.clusterManager)
	assert.NotNil(t, syncManager.proxyClient)
	assert.NotNil(t, syncManager.stopChan)
	assert.NotNil(t, syncManager.log)
}

func TestGroupMappingSyncManager_ListLocalMappings(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create required tables
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

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS idp_group_mappings (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL,
			external_group TEXT NOT NULL,
			external_group_name TEXT,
			role TEXT NOT NULL,
			tenant_id TEXT,
			auto_sync BOOLEAN DEFAULT 0,
			last_synced_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(provider_id, external_group)
		)
	`)
	require.NoError(t, err)

	now := time.Now().Unix()

	// Insert test provider
	_, err = db.ExecContext(ctx, `
		INSERT INTO identity_providers (id, name, type, status, config, created_by, created_at, updated_at)
		VALUES ('idp-1', 'Test LDAP', 'ldap', 'active', '{}', 'admin', ?, ?)
	`, now, now)
	require.NoError(t, err)

	// Insert test group mappings
	_, err = db.ExecContext(ctx, `
		INSERT INTO idp_group_mappings (id, provider_id, external_group, external_group_name, role, tenant_id, auto_sync, created_at, updated_at)
		VALUES ('gm-1', 'idp-1', 'cn=admins,dc=example', 'Admins', 'admin', '', 1, ?, ?)
	`, now, now)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO idp_group_mappings (id, provider_id, external_group, external_group_name, role, tenant_id, auto_sync, created_at, updated_at)
		VALUES ('gm-2', 'idp-1', 'cn=users,dc=example', 'Users', 'user', 'tenant-1', 0, ?, ?)
	`, now, now)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewGroupMappingSyncManager(db, clusterManager)

	mappings, err := syncManager.listLocalMappings(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, len(mappings))

	found := false
	for _, m := range mappings {
		if m.ID == "gm-1" {
			found = true
			assert.Equal(t, "idp-1", m.ProviderID)
			assert.Equal(t, "cn=admins,dc=example", m.ExternalGroup)
			assert.Equal(t, "Admins", m.ExternalGroupName)
			assert.Equal(t, "admin", m.Role)
			assert.True(t, m.AutoSync)
		}
	}
	assert.True(t, found, "Should find gm-1")
}

func TestGroupMappingSyncManager_ComputeChecksum(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewGroupMappingSyncManager(db, clusterManager)

	now := time.Now().Unix()
	mapping := &GroupMappingData{
		ID:                "gm-1",
		ProviderID:        "idp-1",
		ExternalGroup:     "cn=admins,dc=example",
		ExternalGroupName: "Admins",
		Role:              "admin",
		TenantID:          "",
		AutoSync:          true,
		UpdatedAt:         now,
	}

	checksum1 := syncManager.computeMappingChecksum(mapping)
	checksum2 := syncManager.computeMappingChecksum(mapping)
	assert.Equal(t, checksum1, checksum2, "Same data should produce same checksum")

	mapping.Role = "user"
	checksum3 := syncManager.computeMappingChecksum(mapping)
	assert.NotEqual(t, checksum1, checksum3, "Different data should produce different checksum")
}

func TestGroupMappingSyncManager_NeedsSynchronization(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitReplicationSchema(db))

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewGroupMappingSyncManager(db, clusterManager)

	// Never synced - should need sync
	needsSync, err := syncManager.needsSynchronization(ctx, "gm-1", "node-1", "checksum123")
	require.NoError(t, err)
	assert.True(t, needsSync)

	// Update sync status
	err = syncManager.updateSyncStatus(ctx, "gm-1", "source-node", "node-1", "checksum123")
	require.NoError(t, err)

	// Same checksum - should not need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "gm-1", "node-1", "checksum123")
	require.NoError(t, err)
	assert.False(t, needsSync)

	// Different checksum - should need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "gm-1", "node-1", "checksum456")
	require.NoError(t, err)
	assert.True(t, needsSync)
}

func TestGroupMappingSyncManager_UpdateSyncStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitReplicationSchema(db))

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewGroupMappingSyncManager(db, clusterManager)

	// Insert sync status
	err := syncManager.updateSyncStatus(ctx, "gm-1", "source-node", "dest-node", "checksum1")
	require.NoError(t, err)

	var checksum string
	err = db.QueryRowContext(ctx, `
		SELECT mapping_checksum FROM cluster_group_mapping_sync
		WHERE mapping_id = ? AND destination_node_id = ?
	`, "gm-1", "dest-node").Scan(&checksum)
	require.NoError(t, err)
	assert.Equal(t, "checksum1", checksum)

	// Update existing
	err = syncManager.updateSyncStatus(ctx, "gm-1", "source-node", "dest-node", "checksum2")
	require.NoError(t, err)

	err = db.QueryRowContext(ctx, `
		SELECT mapping_checksum FROM cluster_group_mapping_sync
		WHERE mapping_id = ? AND destination_node_id = ?
	`, "gm-1", "dest-node").Scan(&checksum)
	require.NoError(t, err)
	assert.Equal(t, "checksum2", checksum)

	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_group_mapping_sync
		WHERE mapping_id = ? AND destination_node_id = ?
	`, "gm-1", "dest-node").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestGroupMappingSyncManager_Stop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewGroupMappingSyncManager(db, clusterManager)

	syncManager.Stop()

	select {
	case <-syncManager.stopChan:
		// Expected
	default:
		t.Error("Expected stop channel to be closed")
	}
}

func TestGroupMappingSyncManager_SendMappingToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	receivedData := make(chan *GroupMappingData, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/internal/cluster/group-mapping-sync", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var data GroupMappingData
		err := json.NewDecoder(r.Body).Decode(&data)
		require.NoError(t, err)
		receivedData <- &data

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	node := &Node{ID: "test-node-1", Endpoint: server.URL, HealthStatus: "healthy"}
	mapping := &GroupMappingData{
		ID:                "gm-1",
		ProviderID:        "idp-1",
		ExternalGroup:     "cn=admins,dc=example",
		ExternalGroupName: "Admins",
		Role:              "admin",
		AutoSync:          true,
		CreatedAt:         time.Now().Unix(),
		UpdatedAt:         time.Now().Unix(),
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewGroupMappingSyncManager(db, clusterManager)

	err := syncManager.sendMappingToNode(ctx, mapping, node, "source-node", "test-token")
	require.NoError(t, err)

	select {
	case received := <-receivedData:
		assert.Equal(t, mapping.ID, received.ID)
		assert.Equal(t, mapping.ProviderID, received.ProviderID)
		assert.Equal(t, mapping.ExternalGroup, received.ExternalGroup)
		assert.Equal(t, mapping.Role, received.Role)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for mapping data")
	}
}

func TestGroupMappingSyncManager_SendMappingToNode_ServerError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	node := &Node{ID: "test-node-1", Endpoint: server.URL}
	mapping := &GroupMappingData{ID: "gm-1", ProviderID: "idp-1", ExternalGroup: "group", Role: "admin", CreatedAt: time.Now().Unix()}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewGroupMappingSyncManager(db, clusterManager)

	err := syncManager.sendMappingToNode(ctx, mapping, node, "source-node", "test-token")
	assert.Error(t, err)
}

func TestGroupMappingSyncManager_SyncMappingToNode(t *testing.T) {
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
	mapping := &GroupMappingData{
		ID:            "gm-1",
		ProviderID:    "idp-1",
		ExternalGroup: "group",
		Role:          "admin",
		CreatedAt:     time.Now().Unix(),
		UpdatedAt:     time.Now().Unix(),
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewGroupMappingSyncManager(db, clusterManager)

	err = syncManager.syncMappingToNode(ctx, mapping, node, "local-node")
	require.NoError(t, err)

	// Verify sync status was recorded
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_group_mapping_sync WHERE mapping_id = ? AND destination_node_id = ?
	`, mapping.ID, node.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestGroupMappingSyncManager_SyncLoop(t *testing.T) {
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

	// Create idp_group_mappings table and insert test data
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS idp_group_mappings (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL,
			external_group TEXT NOT NULL,
			external_group_name TEXT,
			role TEXT NOT NULL,
			tenant_id TEXT,
			auto_sync BOOLEAN DEFAULT 0,
			last_synced_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(provider_id, external_group)
		)
	`)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = db.ExecContext(ctx, `
		INSERT INTO idp_group_mappings (id, provider_id, external_group, external_group_name, role, auto_sync, created_at, updated_at)
		VALUES ('gm-1', 'idp-1', 'cn=admins', 'Admins', 'admin', 1, ?, ?)
	`, now, now)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewGroupMappingSyncManager(db, clusterManager)

	go syncManager.syncLoop(ctx, 100*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	assert.GreaterOrEqual(t, syncCount, 1, "Expected at least 1 sync call")
}

func TestGroupMappingSyncManager_Start(t *testing.T) {
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
		syncManager := NewGroupMappingSyncManager(db, clusterManager)

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
			VALUES ('auto_group_mapping_sync_enabled', 'true', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		require.NoError(t, err)

		_, err = db.ExecContext(ctx, `
			INSERT OR REPLACE INTO cluster_global_config (key, value, created_at, updated_at)
			VALUES ('group_mapping_sync_interval_seconds', '1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		require.NoError(t, err)

		clusterManager := NewManager(db, "http://localhost:8080")
		syncManager := NewGroupMappingSyncManager(db, clusterManager)

		syncManager.Start(ctx)
		time.Sleep(100 * time.Millisecond)
		syncManager.Stop()
	})
}

func TestGroupMappingSyncManager_SendDeletionToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	receivedID := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/internal/cluster/group-mapping-delete-sync", r.URL.Path)

		var data map[string]string
		json.NewDecoder(r.Body).Decode(&data)
		receivedID <- data["id"]

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	node := &Node{ID: "remote-node", Endpoint: server.URL, HealthStatus: "healthy"}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewGroupMappingSyncManager(db, clusterManager)

	err := syncManager.sendDeletionToNode(ctx, "gm-1", time.Now().Unix(), node, "local-node", "test-token")
	require.NoError(t, err)

	select {
	case id := <-receivedID:
		assert.Equal(t, "gm-1", id)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for deletion data")
	}
}
