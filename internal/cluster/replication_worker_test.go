package cluster

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClusterReplicationWorker(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)

	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	assert.NotNil(t, worker)
	assert.Equal(t, 1, worker.id)
	assert.NotNil(t, worker.db)
	assert.NotNil(t, worker.clusterManager)
	assert.NotNil(t, worker.proxyClient)
	assert.NotNil(t, worker.queueChan)
	assert.NotNil(t, worker.log)
}

func TestClusterReplicationWorker_UpdateItemStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitReplicationSchema(db))

	// Create a test queue item
	itemID := "test-item-1"
	now := time.Now()
	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_replication_queue (
			id, replication_rule_id, tenant_id, source_bucket, object_key,
			destination_node_id, destination_bucket, operation, status, created_at, updated_at
		) VALUES (?, 'rule-1', 'tenant-1', 'source-bucket', 'test.txt', 'node-1', 'dest-bucket', 'PUT', 'pending', ?, ?)
	`, itemID, now, now)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)
	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	// Update status
	err = worker.updateItemStatus(ctx, itemID, "processing", "")
	require.NoError(t, err)

	// Verify status was updated
	var status string
	err = db.QueryRowContext(ctx, "SELECT status FROM cluster_replication_queue WHERE id = ?", itemID).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "processing", status)

	// Update with error
	err = worker.updateItemStatus(ctx, itemID, "failed", "test error")
	require.NoError(t, err)

	var lastError string
	err = db.QueryRowContext(ctx, "SELECT status, last_error FROM cluster_replication_queue WHERE id = ?", itemID).Scan(&status, &lastError)
	require.NoError(t, err)
	assert.Equal(t, "failed", status)
	assert.Equal(t, "test error", lastError)
}

func TestClusterReplicationWorker_UpdateItemRetry(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitReplicationSchema(db))

	itemID := "test-item-1"
	now := time.Now()
	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_replication_queue (
			id, replication_rule_id, tenant_id, source_bucket, object_key,
			destination_node_id, destination_bucket, operation, status, attempts, created_at, updated_at
		) VALUES (?, 'rule-1', 'tenant-1', 'source-bucket', 'test.txt', 'node-1', 'dest-bucket', 'PUT', 'failed', 1, ?, ?)
	`, itemID, now, now)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)
	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	// Update for retry
	err = worker.updateItemRetry(ctx, itemID, 2, "retry error")
	require.NoError(t, err)

	// Verify update
	var status, lastError string
	var attempts int
	err = db.QueryRowContext(ctx, `
		SELECT status, attempts, last_error
		FROM cluster_replication_queue
		WHERE id = ?
	`, itemID).Scan(&status, &attempts, &lastError)
	require.NoError(t, err)

	assert.Equal(t, "pending", status)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, "retry error", lastError)
}

func TestClusterReplicationWorker_UpdateItemCompleted(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitReplicationSchema(db))

	itemID := "test-item-1"
	now := time.Now()
	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_replication_queue (
			id, replication_rule_id, tenant_id, source_bucket, object_key,
			destination_node_id, destination_bucket, operation, status, created_at, updated_at
		) VALUES (?, 'rule-1', 'tenant-1', 'source-bucket', 'test.txt', 'node-1', 'dest-bucket', 'PUT', 'processing', ?, ?)
	`, itemID, now, now)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)
	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	// Mark as completed
	err = worker.updateItemCompleted(ctx, itemID)
	require.NoError(t, err)

	// Verify update
	var status string
	var completedAt sql.NullTime
	err = db.QueryRowContext(ctx, `
		SELECT status, completed_at
		FROM cluster_replication_queue
		WHERE id = ?
	`, itemID).Scan(&status, &completedAt)
	require.NoError(t, err)

	assert.Equal(t, "completed", status)
	assert.True(t, completedAt.Valid)
}

func TestClusterReplicationWorker_UpdateReplicationStats(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitReplicationSchema(db))

	ruleID := "rule-1"
	now := time.Now()
	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_bucket_replication (
			id, tenant_id, source_bucket, destination_node_id, destination_bucket,
			enabled, objects_replicated, created_at, updated_at
		) VALUES (?, 'tenant-1', 'source-bucket', 'node-1', 'dest-bucket', 1, 5, ?, ?)
	`, ruleID, now, now)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)
	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	// Update stats
	err = worker.updateReplicationStats(ctx, ruleID)
	require.NoError(t, err)

	// Verify update
	var objectsReplicated int
	err = db.QueryRowContext(ctx, `
		SELECT objects_replicated
		FROM cluster_bucket_replication
		WHERE id = ?
	`, ruleID).Scan(&objectsReplicated)
	require.NoError(t, err)

	assert.Equal(t, 6, objectsReplicated)
}

func TestClusterReplicationWorker_UpdateReplicationStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitReplicationSchema(db))

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)
	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	item := &ClusterReplicationQueueItem{
		ID:                  "item-1",
		ReplicationRuleID:   "rule-1",
		TenantID:            "tenant-1",
		SourceBucket:        "source-bucket",
		ObjectKey:           "test.txt",
		DestinationNodeID:   "node-1",
		DestinationBucket:   "dest-bucket",
		Operation:           "PUT",
	}

	// Insert replication status
	err := worker.updateReplicationStatus(ctx, item, "etag123", "version1", 1024)
	require.NoError(t, err)

	// Verify insertion
	var sourceETag, sourceVersionID string
	var sourceSize int64
	err = db.QueryRowContext(ctx, `
		SELECT source_etag, source_version_id, source_size
		FROM cluster_replication_status
		WHERE replication_rule_id = ? AND object_key = ?
	`, item.ReplicationRuleID, item.ObjectKey).Scan(&sourceETag, &sourceVersionID, &sourceSize)
	require.NoError(t, err)

	assert.Equal(t, "etag123", sourceETag)
	assert.Equal(t, "version1", sourceVersionID)
	assert.Equal(t, int64(1024), sourceSize)

	// Update with new values
	err = worker.updateReplicationStatus(ctx, item, "etag456", "version2", 2048)
	require.NoError(t, err)

	// Verify update
	err = db.QueryRowContext(ctx, `
		SELECT source_etag, source_version_id, source_size
		FROM cluster_replication_status
		WHERE replication_rule_id = ? AND object_key = ?
	`, item.ReplicationRuleID, item.ObjectKey).Scan(&sourceETag, &sourceVersionID, &sourceSize)
	require.NoError(t, err)

	assert.Equal(t, "etag456", sourceETag)
	assert.Equal(t, "version2", sourceVersionID)
	assert.Equal(t, int64(2048), sourceSize)
}

func TestClusterReplicationWorker_ReplicateDelete(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitSchema(db))
	require.NoError(t, InitReplicationSchema(db))

	// Initialize cluster
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

	// Create mock destination server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Contains(t, r.URL.Path, "/api/internal/cluster/objects/tenant-1/dest-bucket/test.txt")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Add destination node
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status)
		VALUES ('dest-node', 'Destination', ?, 'dest-token', 'healthy')
	`, server.URL)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)
	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	item := &ClusterReplicationQueueItem{
		ID:                  "item-1",
		ReplicationRuleID:   "rule-1",
		TenantID:            "tenant-1",
		SourceBucket:        "source-bucket",
		ObjectKey:           "test.txt",
		DestinationNodeID:   "dest-node",
		DestinationBucket:   "dest-bucket",
		Operation:           "DELETE",
	}

	err = worker.replicateDelete(ctx, item)
	require.NoError(t, err)
}

func TestClusterReplicationWorker_Start(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, InitSchema(db))
	require.NoError(t, InitReplicationSchema(db))

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)
	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	// Start worker in background
	go worker.Start(ctx)

	// Give worker time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context to stop worker
	cancel()

	// Give worker time to stop
	time.Sleep(50 * time.Millisecond)
}

func TestClusterReplicationWorker_Start_ChannelClosed(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)
	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	// Close channel before starting
	close(queueChan)

	// Start worker - should stop immediately
	worker.Start(ctx)
}

func TestClusterReplicationWorker_ProcessItem_UnknownOperation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitReplicationSchema(db))

	itemID := "test-item-1"
	now := time.Now()
	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_replication_queue (
			id, replication_rule_id, tenant_id, source_bucket, object_key,
			destination_node_id, destination_bucket, operation, status, max_attempts, attempts, created_at, updated_at
		) VALUES (?, 'rule-1', 'tenant-1', 'source-bucket', 'test.txt', 'node-1', 'dest-bucket', 'UNKNOWN', 'pending', 3, 0, ?, ?)
	`, itemID, now, now)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)
	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	item := &ClusterReplicationQueueItem{
		ID:                  itemID,
		ReplicationRuleID:   "rule-1",
		TenantID:            "tenant-1",
		SourceBucket:        "source-bucket",
		ObjectKey:           "test.txt",
		DestinationNodeID:   "node-1",
		DestinationBucket:   "dest-bucket",
		Operation:           "UNKNOWN",
		Status:              "pending",
		Attempts:            0,
		MaxAttempts:         3,
	}

	worker.processItem(ctx, item)

	// Verify item status was updated to pending (for retry)
	var status string
	var attempts int
	err = db.QueryRowContext(ctx, `
		SELECT status, attempts
		FROM cluster_replication_queue
		WHERE id = ?
	`, itemID).Scan(&status, &attempts)
	require.NoError(t, err)

	assert.Equal(t, "pending", status)
	assert.Equal(t, 1, attempts)
}

func TestClusterReplicationWorker_ReplicateObject(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitSchema(db))
	require.NoError(t, InitReplicationSchema(db))

	// Create objects table (simplified schema for testing)
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS objects (
			bucket TEXT NOT NULL,
			key TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			size INTEGER NOT NULL,
			etag TEXT NOT NULL,
			content_type TEXT NOT NULL,
			version_id TEXT NOT NULL,
			metadata TEXT DEFAULT '{}',
			created_at TIMESTAMP NOT NULL,
			deleted_at TIMESTAMP,
			PRIMARY KEY (bucket, key, tenant_id)
		)
	`)
	require.NoError(t, err)

	// Insert test object
	now := time.Now()
	_, err = db.ExecContext(ctx, `
		INSERT INTO objects (bucket, key, tenant_id, size, etag, content_type, version_id, metadata, created_at)
		VALUES ('source-bucket', 'test.txt', 'tenant-1', 1024, 'etag123', 'text/plain', 'version1', '{}', ?)
	`, now)
	require.NoError(t, err)

	// Initialize cluster
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
		VALUES ('local-node', 'Local', 'local-token', 1)
	`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status)
		VALUES ('local-node', 'Local', 'http://localhost:8080', 'local-token', 'healthy')
	`)
	require.NoError(t, err)

	// Create mock destination server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Contains(t, r.URL.Path, "/api/internal/cluster/objects/tenant-1/dest-bucket/test.txt")
		assert.Equal(t, "text/plain", r.Header.Get("Content-Type"))
		assert.Equal(t, "1024", r.Header.Get("X-Object-Size"))
		assert.Equal(t, "etag123", r.Header.Get("X-Object-ETag"))
		assert.Equal(t, "version1", r.Header.Get("X-Source-Version-ID"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Add destination node
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status)
		VALUES ('dest-node', 'Destination', ?, 'dest-token', 'healthy')
	`, server.URL)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)
	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	item := &ClusterReplicationQueueItem{
		ID:                  "item-1",
		ReplicationRuleID:   "rule-1",
		TenantID:            "tenant-1",
		SourceBucket:        "source-bucket",
		ObjectKey:           "test.txt",
		DestinationNodeID:   "dest-node",
		DestinationBucket:   "dest-bucket",
		Operation:           "PUT",
	}

	err = worker.replicateObject(ctx, item)
	require.NoError(t, err)

	// Verify replication status was created
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM cluster_replication_status
		WHERE replication_rule_id = ? AND object_key = ?
	`, item.ReplicationRuleID, item.ObjectKey).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestClusterReplicationWorker_ReplicateObject_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, InitSchema(db))
	require.NoError(t, InitReplicationSchema(db))

	// Create objects table but don't insert any object
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS objects (
			bucket TEXT NOT NULL,
			key TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			size INTEGER NOT NULL,
			etag TEXT NOT NULL,
			content_type TEXT NOT NULL,
			version_id TEXT NOT NULL,
			metadata TEXT DEFAULT '{}',
			created_at TIMESTAMP NOT NULL,
			deleted_at TIMESTAMP,
			PRIMARY KEY (bucket, key, tenant_id)
		)
	`)
	require.NoError(t, err)

	clusterManager := NewManager(db, "http://localhost:8080")
	proxyClient := NewProxyClient()
	queueChan := make(chan *ClusterReplicationQueueItem, 10)
	worker := NewClusterReplicationWorker(1, db, clusterManager, nil, proxyClient, queueChan)

	item := &ClusterReplicationQueueItem{
		ID:                  "item-1",
		ReplicationRuleID:   "rule-1",
		TenantID:            "tenant-1",
		SourceBucket:        "source-bucket",
		ObjectKey:           "nonexistent.txt",
		DestinationNodeID:   "dest-node",
		DestinationBucket:   "dest-bucket",
		Operation:           "PUT",
	}

	err = worker.replicateObject(ctx, item)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "object not found")
}
