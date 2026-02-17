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

func TestRecordDeletion(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create the cluster_deletion_log table
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cluster_deletion_log (
			id TEXT PRIMARY KEY,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			deleted_by_node_id TEXT NOT NULL,
			deleted_at INTEGER NOT NULL,
			UNIQUE(entity_type, entity_id)
		)
	`)
	require.NoError(t, err)

	// Test recording a deletion
	err = RecordDeletion(ctx, db, EntityTypeUser, "user-1", "node-1")
	require.NoError(t, err)

	// Verify it was recorded
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_deletion_log WHERE entity_type = ? AND entity_id = ?`, EntityTypeUser, "user-1").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Test idempotency - re-recording same entity should update, not duplicate
	err = RecordDeletion(ctx, db, EntityTypeUser, "user-1", "node-2")
	require.NoError(t, err)

	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_deletion_log WHERE entity_type = ? AND entity_id = ?`, EntityTypeUser, "user-1").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Should still be 1 record after re-recording")

	// Verify the node was updated
	var nodeID string
	err = db.QueryRowContext(ctx, `SELECT deleted_by_node_id FROM cluster_deletion_log WHERE entity_type = ? AND entity_id = ?`, EntityTypeUser, "user-1").Scan(&nodeID)
	require.NoError(t, err)
	assert.Equal(t, "node-2", nodeID)
}

func TestRecordDeletion_MultipleEntityTypes(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cluster_deletion_log (
			id TEXT PRIMARY KEY,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			deleted_by_node_id TEXT NOT NULL,
			deleted_at INTEGER NOT NULL,
			UNIQUE(entity_type, entity_id)
		)
	`)
	require.NoError(t, err)

	// Record deletions for different entity types
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-1", "node-1"))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeTenant, "tenant-1", "node-1"))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeAccessKey, "key-1", "node-1"))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeBucketPermission, "perm-1", "node-1"))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeIDPProvider, "idp-1", "node-1"))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeGroupMapping, "gm-1", "node-1"))

	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_deletion_log`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 6, count)
}

func TestListDeletions(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cluster_deletion_log (
			id TEXT PRIMARY KEY,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			deleted_by_node_id TEXT NOT NULL,
			deleted_at INTEGER NOT NULL,
			UNIQUE(entity_type, entity_id)
		)
	`)
	require.NoError(t, err)

	// Record some deletions
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-1", "node-1"))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-2", "node-2"))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeTenant, "tenant-1", "node-1"))

	// List user deletions
	entries, err := ListDeletions(ctx, db, EntityTypeUser)
	require.NoError(t, err)
	assert.Equal(t, 2, len(entries))

	// List tenant deletions
	entries, err = ListDeletions(ctx, db, EntityTypeTenant)
	require.NoError(t, err)
	assert.Equal(t, 1, len(entries))

	// List for entity type with no deletions
	entries, err = ListDeletions(ctx, db, EntityTypeAccessKey)
	require.NoError(t, err)
	assert.Equal(t, 0, len(entries))
}

func TestHasDeletion(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cluster_deletion_log (
			id TEXT PRIMARY KEY,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			deleted_by_node_id TEXT NOT NULL,
			deleted_at INTEGER NOT NULL,
			UNIQUE(entity_type, entity_id)
		)
	`)
	require.NoError(t, err)

	// No deletion should exist yet
	has, err := HasDeletion(ctx, db, EntityTypeUser, "user-1")
	require.NoError(t, err)
	assert.False(t, has)

	// Record a deletion
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-1", "node-1"))

	// Now it should exist
	has, err = HasDeletion(ctx, db, EntityTypeUser, "user-1")
	require.NoError(t, err)
	assert.True(t, has)

	// Different entity ID should not exist
	has, err = HasDeletion(ctx, db, EntityTypeUser, "user-2")
	require.NoError(t, err)
	assert.False(t, has)

	// Same ID but different entity type should not exist
	has, err = HasDeletion(ctx, db, EntityTypeTenant, "user-1")
	require.NoError(t, err)
	assert.False(t, has)
}

func TestCleanupOldDeletions(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cluster_deletion_log (
			id TEXT PRIMARY KEY,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			deleted_by_node_id TEXT NOT NULL,
			deleted_at INTEGER NOT NULL,
			UNIQUE(entity_type, entity_id)
		)
	`)
	require.NoError(t, err)

	// Insert an old tombstone (8 days ago)
	oldTime := time.Now().Add(-8 * 24 * time.Hour).Unix()
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_deletion_log (id, entity_type, entity_id, deleted_by_node_id, deleted_at)
		VALUES ('old-1', ?, 'user-old', 'node-1', ?)
	`, EntityTypeUser, oldTime)
	require.NoError(t, err)

	// Insert a recent tombstone
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-new", "node-1"))

	// Cleanup with 7 day max age
	count, err := CleanupOldDeletions(ctx, db, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "Should have cleaned up 1 old entry")

	// Verify old one is gone
	has, err := HasDeletion(ctx, db, EntityTypeUser, "user-old")
	require.NoError(t, err)
	assert.False(t, has)

	// Verify new one is still there
	has, err = HasDeletion(ctx, db, EntityTypeUser, "user-new")
	require.NoError(t, err)
	assert.True(t, has)
}

func TestCleanupOldDeletions_NothingToClean(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cluster_deletion_log (
			id TEXT PRIMARY KEY,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			deleted_by_node_id TEXT NOT NULL,
			deleted_at INTEGER NOT NULL,
			UNIQUE(entity_type, entity_id)
		)
	`)
	require.NoError(t, err)

	// Record a recent deletion
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-1", "node-1"))

	count, err := CleanupOldDeletions(ctx, db, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestDeletionLogSyncManager_New(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewDeletionLogSyncManager(db, clusterManager)

	assert.NotNil(t, syncManager)
	assert.NotNil(t, syncManager.db)
	assert.NotNil(t, syncManager.clusterManager)
	assert.NotNil(t, syncManager.proxyClient)
	assert.NotNil(t, syncManager.stopChan)
	assert.NotNil(t, syncManager.log)
}

func TestDeletionLogSyncManager_Stop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewDeletionLogSyncManager(db, clusterManager)

	syncManager.Stop()

	select {
	case <-syncManager.stopChan:
		// Expected - channel is closed
	default:
		t.Error("Expected stop channel to be closed")
	}
}

func TestDeletionLogSyncManager_ComputeChecksum(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewDeletionLogSyncManager(db, clusterManager)

	entries := []*DeletionEntry{
		{EntityType: EntityTypeUser, EntityID: "user-1", DeletedByNodeID: "node-1", DeletedAt: 1000},
		{EntityType: EntityTypeTenant, EntityID: "tenant-1", DeletedByNodeID: "node-2", DeletedAt: 2000},
	}

	checksum1 := syncManager.computeChecksum(entries)
	checksum2 := syncManager.computeChecksum(entries)
	assert.Equal(t, checksum1, checksum2, "Same data should produce same checksum")

	// Different data should produce different checksum
	entries2 := []*DeletionEntry{
		{EntityType: EntityTypeUser, EntityID: "user-1", DeletedByNodeID: "node-1", DeletedAt: 1000},
		{EntityType: EntityTypeTenant, EntityID: "tenant-2", DeletedByNodeID: "node-2", DeletedAt: 2000},
	}
	checksum3 := syncManager.computeChecksum(entries2)
	assert.NotEqual(t, checksum1, checksum3, "Different data should produce different checksum")
}

func TestDeletionLogSyncManager_SyncToNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	require.NoError(t, InitSchema(db))
	require.NoError(t, InitReplicationSchema(db))

	// Create cluster config
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

	// Create mock server to receive deletion entries
	receivedEntries := make(chan []*DeletionEntry, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/internal/cluster/deletion-log-sync", r.URL.Path)

		var entries []*DeletionEntry
		err := json.NewDecoder(r.Body).Decode(&entries)
		require.NoError(t, err)
		receivedEntries <- entries

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	node := &Node{
		ID:           "remote-node",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	entries := []*DeletionEntry{
		{ID: "del-1", EntityType: EntityTypeUser, EntityID: "user-1", DeletedByNodeID: "local-node", DeletedAt: time.Now().Unix()},
		{ID: "del-2", EntityType: EntityTypeTenant, EntityID: "tenant-1", DeletedByNodeID: "local-node", DeletedAt: time.Now().Unix()},
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewDeletionLogSyncManager(db, clusterManager)

	err = syncManager.syncToNode(ctx, entries, node, "local-node", "local-token", "checksum")
	require.NoError(t, err)

	select {
	case received := <-receivedEntries:
		assert.Equal(t, 2, len(received))
		assert.Equal(t, "user-1", received[0].EntityID)
		assert.Equal(t, "tenant-1", received[1].EntityID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for deletion entries")
	}
}

func TestDeletionLogSyncManager_SyncToNode_ServerError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	node := &Node{ID: "remote-node", Endpoint: server.URL}
	entries := []*DeletionEntry{
		{ID: "del-1", EntityType: EntityTypeUser, EntityID: "user-1", DeletedByNodeID: "local-node", DeletedAt: time.Now().Unix()},
	}

	clusterManager := NewManager(db, "http://localhost:8080")
	syncManager := NewDeletionLogSyncManager(db, clusterManager)

	err := syncManager.syncToNode(ctx, entries, node, "local-node", "local-token", "checksum")
	assert.Error(t, err)
}

func TestStartDeletionLogCleanup(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())

	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cluster_deletion_log (
			id TEXT PRIMARY KEY,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			deleted_by_node_id TEXT NOT NULL,
			deleted_at INTEGER NOT NULL,
			UNIQUE(entity_type, entity_id)
		)
	`)
	require.NoError(t, err)

	// Insert an old tombstone
	oldTime := time.Now().Add(-2 * time.Hour).Unix()
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_deletion_log (id, entity_type, entity_id, deleted_by_node_id, deleted_at)
		VALUES ('old-1', ?, 'user-old', 'node-1', ?)
	`, EntityTypeUser, oldTime)
	require.NoError(t, err)

	// Start cleanup with short interval and 1-hour max age
	StartDeletionLogCleanup(ctx, db, 100*time.Millisecond, 1*time.Hour)

	// Wait for cleanup to run
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Verify old entry was cleaned up (use fresh context since we cancelled the other)
	verifyCtx := context.Background()
	has, err := HasDeletion(verifyCtx, db, EntityTypeUser, "user-old")
	require.NoError(t, err)
	assert.False(t, has, "Old tombstone should have been cleaned up")
}
