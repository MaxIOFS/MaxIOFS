package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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
	var err error
	require.NoError(t, createClusterDeletionLogTable(ctx, db))

	// Test recording a deletion
	err = RecordDeletion(ctx, db, EntityTypeUser, "user-1", "node-1", time.Now().Unix())
	require.NoError(t, err)

	// Verify it was recorded
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_deletion_log WHERE entity_type = ? AND entity_id = ?`, EntityTypeUser, "user-1").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Re-recording the same entity keeps one record, with the later deletion.
	record := func(node string, at int64) {
		t.Helper()
		require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-1", node, at))
	}
	stored := func() (string, int64, int64) {
		t.Helper()
		var node string
		var at, seq int64
		require.NoError(t, db.QueryRowContext(ctx, `SELECT deleted_by_node_id, deleted_at, seq FROM cluster_deletion_log WHERE entity_type = ? AND entity_id = ?`,
			EntityTypeUser, "user-1").Scan(&node, &at, &seq))
		return node, at, seq
	}
	record("node-2", 2_000_000_000)
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_deletion_log WHERE entity_type = ? AND entity_id = ?`, EntityTypeUser, "user-1").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Should still be 1 record after re-recording")
	node, at, seq := stored()
	assert.Equal(t, "node-2", node)
	assert.EqualValues(t, 2_000_000_000, at)
	record("node-3", 1_000_000_000)
	node2, at2, seq2 := stored()
	assert.Equal(t, [3]any{"node-2", int64(2_000_000_000), seq}, [3]any{node2, at2, seq2}, "an earlier deletion changes nothing")
	record("node-3", 2_000_000_001)
	_, _, seq3 := stored()
	assert.Greater(t, seq3, seq, "a later deletion is sent again")
}

func TestObjectTombstoneIDRoundTrip(t *testing.T) {
	bucket := "tenant-a/bucket-a"
	key := "nested/key with spaces?and&chars\nline"
	id := ObjectTombstoneID(bucket, key)

	gotBucket, gotKey, ok := DecodeObjectTombstoneID(id)
	require.True(t, ok)
	assert.Equal(t, bucket, gotBucket)
	assert.Equal(t, key, gotKey)

	versionID := "v-123"
	versionIDEncoded := ObjectVersionTombstoneID(bucket, key, versionID)
	gotBucket, gotKey, gotVersionID, ok := DecodeObjectVersionTombstoneID(versionIDEncoded)
	require.True(t, ok)
	assert.Equal(t, bucket, gotBucket)
	assert.Equal(t, key, gotKey)
	assert.Equal(t, versionID, gotVersionID)
}

func TestRecordDeletion_MultipleEntityTypes(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	var err error
	require.NoError(t, createClusterDeletionLogTable(ctx, db))

	// Record deletions for different entity types
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-1", "node-1", time.Now().Unix()))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeTenant, "tenant-1", "node-1", time.Now().Unix()))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeAccessKey, "key-1", "node-1", time.Now().Unix()))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeBucketPermission, "perm-1", "node-1", time.Now().Unix()))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeIDPProvider, "idp-1", "node-1", time.Now().Unix()))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeGroupMapping, "gm-1", "node-1", time.Now().Unix()))

	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_deletion_log`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 6, count)
}

func TestListDeletions(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	var err error
	require.NoError(t, createClusterDeletionLogTable(ctx, db))

	// Record some deletions
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-1", "node-1", time.Now().Unix()))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-2", "node-2", time.Now().Unix()))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeTenant, "tenant-1", "node-1", time.Now().Unix()))

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

	var err error
	require.NoError(t, createClusterDeletionLogTable(ctx, db))

	// No deletion should exist yet
	has, err := HasDeletion(ctx, db, EntityTypeUser, "user-1")
	require.NoError(t, err)
	assert.False(t, has)

	// Record a deletion
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-1", "node-1", time.Now().Unix()))

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

	var err error
	require.NoError(t, createClusterDeletionLogTable(ctx, db))

	// Insert an old tombstone (8 days ago)
	oldTime := time.Now().Add(-8 * 24 * time.Hour).Unix()
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_deletion_log (id, entity_type, entity_id, deleted_by_node_id, deleted_at)
		VALUES ('old-1', ?, 'user-old', 'node-1', ?)
	`, EntityTypeUser, oldTime)
	require.NoError(t, err)

	// Insert a recent tombstone
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-new", "node-1", time.Now().Unix()))

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

	var err error
	require.NoError(t, createClusterDeletionLogTable(ctx, db))

	// Record a recent deletion
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "user-1", "node-1", time.Now().Unix()))

	count, err := CleanupOldDeletions(ctx, db, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestDeletionLogSyncManager_New(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	syncManager := NewDeletionLogSyncManager(db, clusterManager)

	assert.NotNil(t, syncManager)
	assert.NotNil(t, syncManager.db)
	assert.NotNil(t, syncManager.clusterManager)
	assert.NotNil(t, syncManager.proxyClient)
	assert.NotNil(t, syncManager.Stopped())
	assert.NotNil(t, syncManager.log)
}

func TestDeletionLogSyncManager_Stop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	syncManager := NewDeletionLogSyncManager(db, clusterManager)

	syncManager.Stop()

	select {
	case <-syncManager.Stopped():
		// Expected - channel is closed
	default:
		t.Error("Expected stop channel to be closed")
	}
}

// A node is sent each deletion once, in the order they were recorded, and again
// when a later deletion of the same entity is recorded; a delivery that fails
// is sent again.
func TestDeletionLogIsSentOnceAndInOrder(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, InitReplicationSchema(db))

	var mu sync.Mutex
	var batches [][]*DeletionEntry
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var entries []*DeletionEntry
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&entries))
		mu.Lock()
		defer mu.Unlock()
		if fail {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		batches = append(batches, entries)
	}))
	defer server.Close()
	node := &Node{ID: "peer", Endpoint: server.URL}
	m := NewDeletionLogSyncManager(db, NewManager(db, "", ""))
	sent := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var out []string
		for _, b := range batches {
			for _, e := range b {
				out = append(out, fmt.Sprintf("%s@%d", e.EntityID, e.DeletedAt))
			}
		}
		batches = nil
		return out
	}

	for i := 0; i < deletionLogBatch+2; i++ {
		require.NoError(t, RecordDeletion(ctx, db, EntityTypeAccessKey, fmt.Sprintf("k%04d", i), "local", int64(1000+i)))
	}
	require.NoError(t, m.deliverTo(ctx, node, "local", "token"))
	got := sent()
	require.Len(t, got, deletionLogBatch+2)
	assert.Equal(t, "k0000@1000", got[0])
	assert.Equal(t, fmt.Sprintf("k%04d@%d", deletionLogBatch+1, 1000+deletionLogBatch+1), got[len(got)-1])

	require.NoError(t, m.deliverTo(ctx, node, "local", "token"))
	assert.Empty(t, sent(), "nothing new")

	require.NoError(t, RecordDeletion(ctx, db, EntityTypeAccessKey, "k0000", "local", 999))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeAccessKey, "k0001", "local", 5000))
	require.NoError(t, m.deliverTo(ctx, node, "local", "token"))
	assert.Equal(t, []string{"k0001@5000"}, sent(), "only the deletion that changed")

	require.NoError(t, RecordDeletion(ctx, db, EntityTypeAccessKey, "late", "local", 6000))
	mu.Lock()
	fail = true
	mu.Unlock()
	assert.Error(t, m.deliverTo(ctx, node, "local", "token"))
	mu.Lock()
	fail = false
	mu.Unlock()
	require.NoError(t, m.deliverTo(ctx, node, "local", "token"))
	assert.Equal(t, []string{"late@6000"}, sent(), "a failed delivery is sent again")
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

	clusterManager := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	syncManager := NewDeletionLogSyncManager(db, clusterManager)

	err = syncManager.syncToNode(ctx, entries, node, "local-node", "local-token")
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

	clusterManager := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	syncManager := NewDeletionLogSyncManager(db, clusterManager)

	err := syncManager.syncToNode(ctx, entries, node, "local-node", "local-token")
	assert.Error(t, err)
}

func TestStartDeletionLogCleanup(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())

	var err error
	require.NoError(t, createClusterDeletionLogTable(ctx, db))

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
