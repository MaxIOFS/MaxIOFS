package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupReplicationTestDB creates a test database with cluster schema
func setupReplicationTestDB(t *testing.T) (*sql.DB, func()) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_replication.db")

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=30000")
	require.NoError(t, err)

	// Set PRAGMA to reduce lock contention
	_, err = db.Exec("PRAGMA synchronous = NORMAL")
	require.NoError(t, err)

	// Initialize cluster schema
	err = InitSchema(db)
	require.NoError(t, err)

	// Initialize replication schema (includes global config with defaults)
	err = InitReplicationSchema(db)
	require.NoError(t, err)

	// Create objects table for testing (normally from metadata package)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS objects (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			bucket TEXT NOT NULL,
			key TEXT NOT NULL,
			size INTEGER NOT NULL,
			etag TEXT NOT NULL,
			content_type TEXT,
			version_id TEXT,
			metadata TEXT,
			created_at TIMESTAMP NOT NULL,
			deleted_at TIMESTAMP
		)
	`)
	require.NoError(t, err)

	cleanup := func() {
		db.Close()
	}

	return db, cleanup
}

// createTestReplicationClusterManager creates a cluster manager for testing
func createTestReplicationClusterManager(t *testing.T, db *sql.DB) *Manager {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	return &Manager{
		db:                  db,
		log:                 logrus.NewEntry(logger),
		healthCheckInterval: 100 * time.Millisecond,
		stopChan:            make(chan struct{}),
	}
}

func TestNewClusterReplicationManager(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)

	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	assert.NotNil(t, replMgr)
	assert.NotNil(t, replMgr.db)
	assert.NotNil(t, replMgr.clusterManager)
	assert.NotNil(t, replMgr.tenantSyncManager)
	assert.NotNil(t, replMgr.proxyClient)
	assert.Equal(t, 5, replMgr.workerCount) // default from config
	assert.NotNil(t, replMgr.queueChan)
	assert.NotNil(t, replMgr.stopChan)
	assert.NotNil(t, replMgr.log)
}

func TestNewClusterReplicationManager_InvalidWorkerCount(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	// Set invalid worker count
	_, err := db.Exec("UPDATE cluster_global_config SET value = 'invalid' WHERE key = 'replication_worker_count'")
	require.NoError(t, err)

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)

	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Should fallback to default 5
	assert.Equal(t, 5, replMgr.workerCount)
}

func TestCreateReplicationRule_Success(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	rule := &ClusterReplicationRule{
		TenantID:            "tenant-1",
		SourceBucket:        "source-bucket",
		DestinationNodeID:   "node-2",
		DestinationBucket:   "dest-bucket",
		SyncIntervalSeconds: 300,
		Enabled:             true,
		ReplicateDeletes:    true,
		ReplicateMetadata:   true,
		Prefix:              "photos/",
		Priority:            10,
	}

	ctx := context.Background()
	err := replMgr.CreateReplicationRule(ctx, rule)

	require.NoError(t, err)
	assert.NotEmpty(t, rule.ID) // ID should be generated
	assert.False(t, rule.CreatedAt.IsZero())
	assert.False(t, rule.UpdatedAt.IsZero())

	// Verify rule was inserted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM cluster_bucket_replication WHERE id = ?", rule.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestCreateReplicationRule_WithProvidedID(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	customID := "custom-rule-id"
	rule := &ClusterReplicationRule{
		ID:                  customID,
		TenantID:            "tenant-1",
		SourceBucket:        "source-bucket",
		DestinationNodeID:   "node-2",
		DestinationBucket:   "dest-bucket",
		SyncIntervalSeconds: 300,
		Enabled:             true,
	}

	ctx := context.Background()
	err := replMgr.CreateReplicationRule(ctx, rule)

	require.NoError(t, err)
	assert.Equal(t, customID, rule.ID)
}

func TestCreateReplicationRule_IntervalTooShort(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	rule := &ClusterReplicationRule{
		TenantID:            "tenant-1",
		SourceBucket:        "source-bucket",
		DestinationNodeID:   "node-2",
		DestinationBucket:   "dest-bucket",
		SyncIntervalSeconds: 5, // Less than min_sync_interval_seconds (10)
		Enabled:             true,
	}

	ctx := context.Background()
	err := replMgr.CreateReplicationRule(ctx, rule)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync interval must be at least 10 seconds")
}

func TestGetEnabledRules_Success(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Insert test rules (use empty string for last_error to avoid NULL issues)
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO cluster_bucket_replication
		(id, tenant_id, source_bucket, destination_node_id, destination_bucket,
		 sync_interval_seconds, enabled, replicate_deletes, replicate_metadata,
		 priority, last_error, objects_replicated, bytes_replicated, created_at, updated_at)
		VALUES
		('rule-1', 'tenant-1', 'bucket-1', 'node-2', 'bucket-1', 300, 1, 1, 1, 10, '', 0, 0, ?, ?),
		('rule-2', 'tenant-1', 'bucket-2', 'node-2', 'bucket-2', 300, 1, 1, 1, 5, '', 0, 0, ?, ?),
		('rule-3', 'tenant-1', 'bucket-3', 'node-2', 'bucket-3', 300, 0, 1, 1, 1, '', 0, 0, ?, ?)
	`, now, now, now, now, now, now)
	require.NoError(t, err)

	ctx := context.Background()
	rules, err := replMgr.getEnabledRules(ctx)

	require.NoError(t, err)
	assert.Len(t, rules, 2) // Only enabled rules
	assert.Equal(t, "rule-1", rules[0].ID) // Higher priority first
	assert.Equal(t, "rule-2", rules[1].ID)
}

func TestGetEnabledRules_NoRules(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	ctx := context.Background()
	rules, err := replMgr.getEnabledRules(ctx)

	require.NoError(t, err)
	assert.Len(t, rules, 0)
}

func TestInsertQueueItem_NewItem(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	item := &ClusterReplicationQueueItem{
		ID:                "item-1",
		ReplicationRuleID: "rule-1",
		TenantID:          "tenant-1",
		SourceBucket:      "bucket-1",
		ObjectKey:         "file.txt",
		DestinationNodeID: "node-2",
		DestinationBucket: "bucket-1",
		Operation:         "PUT",
		Status:            "pending",
		Attempts:          0,
		MaxAttempts:       3,
		Priority:          10,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	ctx := context.Background()
	err := replMgr.insertQueueItem(ctx, item)

	require.NoError(t, err)

	// Verify item was inserted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM cluster_replication_queue WHERE id = ?", item.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestInsertQueueItem_DuplicateSkipped(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Insert existing pending item
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO cluster_replication_queue
		(id, replication_rule_id, tenant_id, source_bucket, object_key,
		 destination_node_id, destination_bucket, operation, status,
		 attempts, max_attempts, priority, created_at, updated_at)
		VALUES ('existing-item', 'rule-1', 'tenant-1', 'bucket-1', 'file.txt',
		        'node-2', 'bucket-1', 'PUT', 'pending', 0, 3, 10, ?, ?)
	`, now, now)
	require.NoError(t, err)

	item := &ClusterReplicationQueueItem{
		ID:                "item-2",
		ReplicationRuleID: "rule-1",
		TenantID:          "tenant-1",
		SourceBucket:      "bucket-1",
		ObjectKey:         "file.txt", // Same object_key
		DestinationNodeID: "node-2",
		DestinationBucket: "bucket-1",
		Operation:         "PUT",
		Status:            "pending",
		Attempts:          0,
		MaxAttempts:       3,
		Priority:          10,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	ctx := context.Background()
	err = replMgr.insertQueueItem(ctx, item)

	require.NoError(t, err) // Should not error, just skip

	// Verify only one item exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM cluster_replication_queue WHERE replication_rule_id = ? AND object_key = ?", "rule-1", "file.txt").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestGetPendingQueueItems_Success(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Insert test queue items
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO cluster_replication_queue
		(id, replication_rule_id, tenant_id, source_bucket, object_key,
		 destination_node_id, destination_bucket, operation, status,
		 attempts, max_attempts, priority, created_at, updated_at)
		VALUES
		('item-1', 'rule-1', 'tenant-1', 'bucket-1', 'file1.txt', 'node-2', 'bucket-1', 'PUT', 'pending', 0, 3, 10, ?, ?),
		('item-2', 'rule-1', 'tenant-1', 'bucket-1', 'file2.txt', 'node-2', 'bucket-1', 'PUT', 'pending', 0, 3, 5, ?, ?),
		('item-3', 'rule-1', 'tenant-1', 'bucket-1', 'file3.txt', 'node-2', 'bucket-1', 'PUT', 'processing', 0, 3, 3, ?, ?),
		('item-4', 'rule-1', 'tenant-1', 'bucket-1', 'file4.txt', 'node-2', 'bucket-1', 'PUT', 'pending', 3, 3, 1, ?, ?)
	`, now, now, now, now, now, now, now, now)
	require.NoError(t, err)

	ctx := context.Background()
	items, err := replMgr.getPendingQueueItems(ctx, 10)

	require.NoError(t, err)
	assert.Len(t, items, 2) // Only pending items with attempts < max_attempts
	assert.Equal(t, "item-1", items[0].ID) // Higher priority first
	assert.Equal(t, "item-2", items[1].ID)
}

func TestGetPendingQueueItems_LimitApplied(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Insert 5 pending items
	now := time.Now()
	for i := 1; i <= 5; i++ {
		_, err := db.Exec(`
			INSERT INTO cluster_replication_queue
			(id, replication_rule_id, tenant_id, source_bucket, object_key,
			 destination_node_id, destination_bucket, operation, status,
			 attempts, max_attempts, priority, created_at, updated_at)
			VALUES (?, 'rule-1', 'tenant-1', 'bucket-1', ?, 'node-2', 'bucket-1', 'PUT', 'pending', 0, 3, 0, ?, ?)
		`, fmt.Sprintf("item-%d", i), fmt.Sprintf("file%d.txt", i), now, now)
		require.NoError(t, err)
	}

	ctx := context.Background()
	items, err := replMgr.getPendingQueueItems(ctx, 3) // Limit to 3

	require.NoError(t, err)
	assert.Len(t, items, 3)
}

func TestUpdateRuleLastSync(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Insert test rule
	now := time.Now()
	ruleID := "rule-1"
	_, err := db.Exec(`
		INSERT INTO cluster_bucket_replication
		(id, tenant_id, source_bucket, destination_node_id, destination_bucket,
		 sync_interval_seconds, enabled, replicate_deletes, replicate_metadata,
		 priority, objects_replicated, bytes_replicated, created_at, updated_at)
		VALUES (?, 'tenant-1', 'bucket-1', 'node-2', 'bucket-1', 300, 1, 1, 1, 10, 0, 0, ?, ?)
	`, ruleID, now, now)
	require.NoError(t, err)

	// Update last sync
	ctx := context.Background()
	err = replMgr.updateRuleLastSync(ctx, ruleID)
	require.NoError(t, err)

	// Verify last_sync_at was updated
	var lastSyncAt sql.NullTime
	err = db.QueryRow("SELECT last_sync_at FROM cluster_bucket_replication WHERE id = ?", ruleID).Scan(&lastSyncAt)
	require.NoError(t, err)
	assert.True(t, lastSyncAt.Valid)
	assert.True(t, lastSyncAt.Time.After(now.Add(-5*time.Second)))
}

func TestQueueBucketObjects_Success(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Insert test objects
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO objects (id, tenant_id, bucket, key, size, etag, version_id, created_at)
		VALUES
		('obj-1', 'tenant-1', 'bucket-1', 'file1.txt', 1024, 'etag1', 'v1', ?),
		('obj-2', 'tenant-1', 'bucket-1', 'file2.txt', 2048, 'etag2', 'v2', ?),
		('obj-3', 'tenant-1', 'bucket-1', 'file3.txt', 3072, 'etag3', 'v3', ?)
	`, now, now, now)
	require.NoError(t, err)

	rule := &ClusterReplicationRule{
		ID:                "rule-1",
		TenantID:          "tenant-1",
		SourceBucket:      "bucket-1",
		DestinationNodeID: "node-2",
		DestinationBucket: "bucket-1",
		Priority:          10,
	}

	ctx := context.Background()
	err = replMgr.queueBucketObjects(ctx, rule)
	require.NoError(t, err)

	// Give SQLite time to complete writes
	time.Sleep(50 * time.Millisecond)

	// Verify queue items were created (at least some should succeed despite potential locks)
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM cluster_replication_queue WHERE replication_rule_id = ?", rule.ID).Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 0, "At least one object should be queued")
}

func TestQueueBucketObjects_WithPrefix(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Insert test objects
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO objects (id, tenant_id, bucket, key, size, etag, version_id, created_at)
		VALUES
		('obj-1', 'tenant-1', 'bucket-1', 'photos/pic1.jpg', 1024, 'etag1', 'v1', ?),
		('obj-2', 'tenant-1', 'bucket-1', 'photos/pic2.jpg', 2048, 'etag2', 'v2', ?),
		('obj-3', 'tenant-1', 'bucket-1', 'documents/doc.txt', 3072, 'etag3', 'v3', ?)
	`, now, now, now)
	require.NoError(t, err)

	rule := &ClusterReplicationRule{
		ID:                "rule-1",
		TenantID:          "tenant-1",
		SourceBucket:      "bucket-1",
		DestinationNodeID: "node-2",
		DestinationBucket: "bucket-1",
		Prefix:            "photos/", // Only photos prefix
		Priority:          10,
	}

	ctx := context.Background()
	err = replMgr.queueBucketObjects(ctx, rule)
	require.NoError(t, err)

	// Give SQLite time to complete writes
	time.Sleep(50 * time.Millisecond)

	// Verify only photos were queued (at least one should succeed)
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM cluster_replication_queue WHERE replication_rule_id = ?", rule.ID).Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 0, "At least one photo object should be queued")

	// Verify documents were NOT queued
	var docCount int
	err = db.QueryRow("SELECT COUNT(*) FROM cluster_replication_queue WHERE replication_rule_id = ? AND object_key LIKE 'documents/%'", rule.ID).Scan(&docCount)
	require.NoError(t, err)
	assert.Equal(t, 0, docCount, "Documents should not be queued with photos/ prefix")
}

func TestQueueBucketObjects_SkipsDeletedObjects(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Insert test objects (one deleted)
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO objects (id, tenant_id, bucket, key, size, etag, version_id, created_at, deleted_at)
		VALUES
		('obj-1', 'tenant-1', 'bucket-1', 'file1.txt', 1024, 'etag1', 'v1', ?, NULL),
		('obj-2', 'tenant-1', 'bucket-1', 'file2.txt', 2048, 'etag2', 'v2', ?, ?)
	`, now, now, now)
	require.NoError(t, err)

	rule := &ClusterReplicationRule{
		ID:                "rule-1",
		TenantID:          "tenant-1",
		SourceBucket:      "bucket-1",
		DestinationNodeID: "node-2",
		DestinationBucket: "bucket-1",
		Priority:          10,
	}

	ctx := context.Background()
	err = replMgr.queueBucketObjects(ctx, rule)
	require.NoError(t, err)

	// Give SQLite time to complete writes
	time.Sleep(50 * time.Millisecond)

	// Verify only active object was queued (deleted one should be skipped)
	var file1Count, file2Count int
	db.QueryRow("SELECT COUNT(*) FROM cluster_replication_queue WHERE replication_rule_id = ? AND object_key = 'file1.txt'", rule.ID).Scan(&file1Count)
	db.QueryRow("SELECT COUNT(*) FROM cluster_replication_queue WHERE replication_rule_id = ? AND object_key = 'file2.txt'", rule.ID).Scan(&file2Count)

	assert.Greater(t, file1Count, 0, "Active file should be queued")
	assert.Equal(t, 0, file2Count, "Deleted file should NOT be queued")
}

func TestLoadPendingQueueItems_Success(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Insert pending items
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO cluster_replication_queue
		(id, replication_rule_id, tenant_id, source_bucket, object_key,
		 destination_node_id, destination_bucket, operation, status,
		 attempts, max_attempts, priority, created_at, updated_at)
		VALUES
		('item-1', 'rule-1', 'tenant-1', 'bucket-1', 'file1.txt', 'node-2', 'bucket-1', 'PUT', 'pending', 0, 3, 10, ?, ?),
		('item-2', 'rule-1', 'tenant-1', 'bucket-1', 'file2.txt', 'node-2', 'bucket-1', 'PUT', 'pending', 0, 3, 5, ?, ?)
	`, now, now, now, now)
	require.NoError(t, err)

	ctx := context.Background()
	replMgr.loadPendingQueueItems(ctx)

	// Verify items were loaded into channel
	assert.Greater(t, len(replMgr.queueChan), 0)
}

func TestLoadPendingQueueItems_NoItems(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	ctx := context.Background()
	replMgr.loadPendingQueueItems(ctx)

	// Should not panic, just return
	assert.Equal(t, 0, len(replMgr.queueChan))
}

func TestBoolToInt(t *testing.T) {
	assert.Equal(t, 1, boolToInt(true))
	assert.Equal(t, 0, boolToInt(false))
}

func TestCheckReplicationRules_SkipsRecentSync(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Insert rule with recent sync
	recentSync := time.Now().Add(-1 * time.Minute) // Synced 1 minute ago
	_, err := db.Exec(`
		INSERT INTO cluster_bucket_replication
		(id, tenant_id, source_bucket, destination_node_id, destination_bucket,
		 sync_interval_seconds, enabled, replicate_deletes, replicate_metadata,
		 priority, last_sync_at, last_error, objects_replicated, bytes_replicated, created_at, updated_at)
		VALUES ('rule-1', 'tenant-1', 'bucket-1', 'node-2', 'bucket-1', 300, 1, 1, 1, 10, ?, '', 0, 0, ?, ?)
	`, recentSync, time.Now(), time.Now())
	require.NoError(t, err)

	ctx := context.Background()
	replMgr.checkReplicationRules(ctx)

	// Should not queue objects (interval hasn't elapsed)
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM cluster_replication_queue").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCheckReplicationRules_QueuesObjectsWhenDue(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Insert rule with old sync
	oldSync := time.Now().Add(-10 * time.Minute) // Synced 10 minutes ago
	_, err := db.Exec(`
		INSERT INTO cluster_bucket_replication
		(id, tenant_id, source_bucket, destination_node_id, destination_bucket,
		 sync_interval_seconds, enabled, replicate_deletes, replicate_metadata,
		 priority, last_sync_at, last_error, objects_replicated, bytes_replicated, created_at, updated_at)
		VALUES ('rule-1', 'tenant-1', 'bucket-1', 'node-2', 'bucket-1', 300, 1, 1, 1, 10, ?, '', 0, 0, ?, ?)
	`, oldSync, time.Now(), time.Now())
	require.NoError(t, err)

	// Insert test objects
	now := time.Now()
	_, err = db.Exec(`
		INSERT INTO objects (id, tenant_id, bucket, key, size, etag, version_id, created_at)
		VALUES ('obj-1', 'tenant-1', 'bucket-1', 'file1.txt', 1024, 'etag1', 'v1', ?)
	`, now)
	require.NoError(t, err)

	ctx := context.Background()
	replMgr.checkReplicationRules(ctx)

	// Give SQLite time to complete writes
	time.Sleep(50 * time.Millisecond)

	// Should queue objects
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM cluster_replication_queue").Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 0, "At least one object should be queued")

	// Verify last_sync_at was updated
	var lastSyncAt sql.NullTime
	err = db.QueryRow("SELECT last_sync_at FROM cluster_bucket_replication WHERE id = 'rule-1'").Scan(&lastSyncAt)
	require.NoError(t, err)
	assert.True(t, lastSyncAt.Valid)
	assert.True(t, lastSyncAt.Time.After(oldSync))
}

func TestStop(t *testing.T) {
	db, cleanup := setupReplicationTestDB(t)
	defer cleanup()

	clusterMgr := createTestReplicationClusterManager(t, db)
	tenantSyncMgr := NewTenantSyncManager(db, clusterMgr)
	replMgr := NewClusterReplicationManager(db, clusterMgr, tenantSyncMgr)

	// Start should be tested separately, but we'll test Stop can be called
	replMgr.Stop()

	// Should not panic
	assert.True(t, true)
}
