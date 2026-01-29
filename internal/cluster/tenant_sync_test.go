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

func TestTenantSyncManager_NewTenantSyncManager(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewTenantSyncManager(db, clusterManager)

	assert.NotNil(t, syncManager)
	assert.NotNil(t, syncManager.db)
	assert.NotNil(t, syncManager.clusterManager)
	assert.NotNil(t, syncManager.proxyClient)
	assert.NotNil(t, syncManager.stopChan)
	assert.NotNil(t, syncManager.log)
}

func TestTenantSyncManager_ListLocalTenants(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create tenants table
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS tenants (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			display_name TEXT DEFAULT '',
			description TEXT DEFAULT '',
			status TEXT DEFAULT 'active',
			max_access_keys INTEGER DEFAULT 5,
			max_storage_bytes INTEGER DEFAULT 0,
			current_storage_bytes INTEGER DEFAULT 0,
			max_buckets INTEGER DEFAULT 10,
			current_buckets INTEGER DEFAULT 0,
			metadata TEXT DEFAULT '{}',
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)
	`)
	require.NoError(t, err)

	// Insert test tenants
	now := time.Now()
	testTenants := []struct {
		id     string
		name   string
		status string
	}{
		{"tenant-1", "tenant1", "active"},
		{"tenant-2", "tenant2", "active"},
		{"tenant-3", "tenant3", "deleted"}, // should be excluded
	}

	for _, tenant := range testTenants {
		_, err = db.ExecContext(ctx, `
			INSERT INTO tenants (id, name, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
		`, tenant.id, tenant.name, tenant.status, now, now)
		require.NoError(t, err)
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewTenantSyncManager(db, clusterManager)

	tenants, err := syncManager.listLocalTenants(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, len(tenants), "Should only list non-deleted tenants")

	// Verify tenant data
	found := false
	for _, tenant := range tenants {
		if tenant.ID == "tenant-1" {
			found = true
			assert.Equal(t, "tenant1", tenant.Name)
			assert.Equal(t, "active", tenant.Status)
		}
	}
	assert.True(t, found, "Should find tenant-1")
}

func TestTenantSyncManager_ComputeChecksum(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewTenantSyncManager(db, clusterManager)

	now := time.Now()
	tenant := &TenantData{
		ID:              "tenant-1",
		Name:            "testtenant",
		DisplayName:     "Test Tenant",
		Status:          "active",
		MaxAccessKeys:   5,
		MaxStorageBytes: 1000000,
		MaxBuckets:      10,
		Metadata:        map[string]string{"key": "value"},
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	checksum1 := syncManager.computeTenantChecksum(tenant)
	checksum2 := syncManager.computeTenantChecksum(tenant)
	assert.Equal(t, checksum1, checksum2, "Same data should produce same checksum")

	// Different data should produce different checksum
	tenant.DisplayName = "Different Name"
	checksum3 := syncManager.computeTenantChecksum(tenant)
	assert.NotEqual(t, checksum1, checksum3, "Different data should produce different checksum")
}

func TestTenantSyncManager_FormatMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		expected string
	}{
		{
			name:     "empty metadata",
			metadata: map[string]string{},
			expected: "",
		},
		{
			name:     "single key",
			metadata: map[string]string{"key1": "value1"},
			expected: `{"key1":"value1"}`,
		},
		{
			name:     "multiple keys",
			metadata: map[string]string{"key1": "value1", "key2": "value2"},
			expected: "", // Will be JSON, but order may vary
		},
		{
			name:     "nil metadata",
			metadata: nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMetadata(tt.metadata)

			if tt.name == "multiple keys" {
				// For multiple keys, just verify it's valid JSON
				var m map[string]string
				err := json.Unmarshal([]byte(result), &m)
				assert.NoError(t, err)
				assert.Equal(t, len(tt.metadata), len(m))
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestTenantSyncManager_NeedsSynchronization(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	require.NoError(t, InitReplicationSchema(db))

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewTenantSyncManager(db, clusterManager)

	// Test 1: Never synced before - should need sync
	needsSync, err := syncManager.needsSynchronization(ctx, "tenant-1", "node-1", "checksum123")
	require.NoError(t, err)
	assert.True(t, needsSync)

	// Test 2: Update sync status
	err = syncManager.updateSyncStatus(ctx, "tenant-1", "source-node", "node-1", "checksum123")
	require.NoError(t, err)

	// Test 3: Same checksum - should not need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "tenant-1", "node-1", "checksum123")
	require.NoError(t, err)
	assert.False(t, needsSync)

	// Test 4: Different checksum - should need sync
	needsSync, err = syncManager.needsSynchronization(ctx, "tenant-1", "node-1", "checksum456")
	require.NoError(t, err)
	assert.True(t, needsSync)
}

func TestTenantSyncManager_UpdateSyncStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	require.NoError(t, InitReplicationSchema(db))

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewTenantSyncManager(db, clusterManager)

	// Insert new sync status
	err := syncManager.updateSyncStatus(ctx, "tenant-1", "source-node", "dest-node", "checksum1")
	require.NoError(t, err)

	// Verify insertion
	var checksum string
	err = db.QueryRowContext(ctx, `
		SELECT tenant_checksum FROM cluster_tenant_sync
		WHERE tenant_id = ? AND destination_node_id = ?
	`, "tenant-1", "dest-node").Scan(&checksum)
	require.NoError(t, err)
	assert.Equal(t, "checksum1", checksum)

	// Update existing sync status
	err = syncManager.updateSyncStatus(ctx, "tenant-1", "source-node", "dest-node", "checksum2")
	require.NoError(t, err)

	// Verify update
	err = db.QueryRowContext(ctx, `
		SELECT tenant_checksum FROM cluster_tenant_sync
		WHERE tenant_id = ? AND destination_node_id = ?
	`, "tenant-1", "dest-node").Scan(&checksum)
	require.NoError(t, err)
	assert.Equal(t, "checksum2", checksum)

	// Should have only 1 record
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_tenant_sync
		WHERE tenant_id = ? AND destination_node_id = ?
	`, "tenant-1", "dest-node").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestTenantSyncManager_Stop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewTenantSyncManager(db, clusterManager)

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

func TestTenantSyncManager_SendTenantToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create mock HTTP server
	receivedData := make(chan *TenantData, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/internal/cluster/tenant-sync", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var tenantData TenantData
		err := json.NewDecoder(r.Body).Decode(&tenantData)
		require.NoError(t, err)
		receivedData <- &tenantData

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	node := &Node{
		ID:           "test-node-1",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	now := time.Now()
	tenant := &TenantData{
		ID:              "tenant-123",
		Name:            "testtenant",
		DisplayName:     "Test Tenant",
		Status:          "active",
		MaxAccessKeys:   5,
		MaxStorageBytes: 1000000,
		MaxBuckets:      10,
		Metadata:        map[string]string{"key": "value"},
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewTenantSyncManager(db, clusterManager)

	err := syncManager.sendTenantToNode(ctx, tenant, node, "source-node", "test-token")
	require.NoError(t, err)

	// Verify data was received
	select {
	case received := <-receivedData:
		assert.Equal(t, tenant.ID, received.ID)
		assert.Equal(t, tenant.Name, received.Name)
		assert.Equal(t, tenant.DisplayName, received.DisplayName)
		assert.Equal(t, tenant.MaxAccessKeys, received.MaxAccessKeys)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for tenant data")
	}
}

func TestTenantSyncManager_SendTenantToNode_ServerError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	node := &Node{ID: "test-node-1", Endpoint: server.URL}
	tenant := &TenantData{
		ID:        "tenant-123",
		Name:      "test",
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewTenantSyncManager(db, clusterManager)

	err := syncManager.sendTenantToNode(ctx, tenant, node, "source-node", "test-token")
	assert.Error(t, err)
}

func TestTenantSyncManager_SyncTenantToNode(t *testing.T) {
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
	tenant := &TenantData{
		ID:        "tenant-123",
		Name:      "test",
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewTenantSyncManager(db, clusterManager)

	err = syncManager.syncTenantToNode(ctx, tenant, node, "local-node")
	require.NoError(t, err)

	// Verify sync status was recorded
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cluster_tenant_sync WHERE tenant_id = ? AND destination_node_id = ?
	`, tenant.ID, node.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestTenantSyncManager_SyncLoop(t *testing.T) {
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
		CREATE TABLE IF NOT EXISTS tenants (
			id TEXT PRIMARY KEY,
			name TEXT,
			status TEXT,
			created_at TIMESTAMP,
			updated_at TIMESTAMP,
			display_name TEXT DEFAULT '',
			description TEXT DEFAULT '',
			max_access_keys INTEGER DEFAULT 5,
			max_storage_bytes INTEGER DEFAULT 0,
			current_storage_bytes INTEGER DEFAULT 0,
			max_buckets INTEGER DEFAULT 10,
			current_buckets INTEGER DEFAULT 0,
			metadata TEXT DEFAULT '{}'
		)
	`)
	require.NoError(t, err)

	now := time.Now()
	_, err = db.ExecContext(ctx, `INSERT INTO tenants (id, name, status, created_at, updated_at) VALUES ('tenant-1', 'test', 'active', ?, ?)`, now, now)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewTenantSyncManager(db, clusterManager)

	go syncManager.syncLoop(ctx, 100*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	assert.GreaterOrEqual(t, syncCount, 1, "Expected at least 1 sync call")
}

func TestTenantSyncManager_Start(t *testing.T) {
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
		syncManager := NewTenantSyncManager(db, clusterManager)

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
			VALUES ('auto_tenant_sync_enabled', 'true', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		require.NoError(t, err)

		_, err = db.ExecContext(ctx, `
			INSERT OR REPLACE INTO cluster_global_config (key, value, created_at, updated_at)
			VALUES ('tenant_sync_interval_seconds', '1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`)
		require.NoError(t, err)

		clusterManager := NewManager(db, "http://localhost:8080")
		syncManager := NewTenantSyncManager(db, clusterManager)

		syncManager.Start(ctx)
		time.Sleep(100 * time.Millisecond)
		syncManager.Stop()
	})
}
