package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// MockBucketManager implements BucketManager interface for testing
type MockBucketManager struct {
	buckets map[string]string // bucket -> tenantID
}

func NewMockBucketManager() *MockBucketManager {
	return &MockBucketManager{
		buckets: make(map[string]string),
	}
}

func (m *MockBucketManager) AddBucket(bucket, tenantID string) {
	m.buckets[bucket] = tenantID
}

func (m *MockBucketManager) GetBucketTenant(ctx context.Context, bucket string) (string, error) {
	tenantID, exists := m.buckets[bucket]
	if !exists {
		return "", fmt.Errorf("bucket not found: %s", bucket)
	}
	return tenantID, nil
}

func (m *MockBucketManager) BucketExists(ctx context.Context, tenant, bucket string) (bool, error) {
	storedTenant, exists := m.buckets[bucket]
	if !exists {
		return false, nil
	}
	return storedTenant == tenant, nil
}

// MockReplicationManager implements ReplicationManager interface for testing
type MockReplicationManager struct {
	rules map[string][]ReplicationRule // "tenantID/bucket" -> rules
}

func NewMockReplicationManager() *MockReplicationManager {
	return &MockReplicationManager{
		rules: make(map[string][]ReplicationRule),
	}
}

func (m *MockReplicationManager) AddRule(tenantID, bucket string, rule ReplicationRule) {
	key := fmt.Sprintf("%s/%s", tenantID, bucket)
	m.rules[key] = append(m.rules[key], rule)
}

func (m *MockReplicationManager) GetReplicationRules(ctx context.Context, tenantID, bucket string) ([]ReplicationRule, error) {
	key := fmt.Sprintf("%s/%s", tenantID, bucket)
	rules, exists := m.rules[key]
	if !exists {
		return nil, nil
	}
	return rules, nil
}

// setupRouterTestDB creates a test database with cluster schema
func setupRouterTestDB(t *testing.T) (*sql.DB, func()) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_router.db")

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=10000")
	require.NoError(t, err)

	err = InitSchema(db)
	require.NoError(t, err)

	cleanup := func() {
		db.Close()
	}

	return db, cleanup
}

// createTestRouter creates a test Router instance
func createTestRouter(t *testing.T, manager *Manager, bucketMgr *MockBucketManager, replMgr *MockReplicationManager, localNodeID string) *Router {
	return NewRouter(manager, bucketMgr, replMgr, localNodeID)
}

// TestNewRouter tests router creation
func TestNewRouter(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")

	assert.NotNil(t, router)
	assert.NotNil(t, router.cache)
	assert.NotNil(t, router.proxyClient)
	assert.Equal(t, "local-node", router.localNodeID)
}

// TestGetBucketNode_LocalBucket tests getting node for local bucket
func TestGetBucketNode_LocalBucket(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	// Add local bucket
	bucketMgr.AddBucket("local-bucket", "tenant-1")

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")
	ctx := context.Background()

	node, err := router.GetBucketNode(ctx, "local-bucket")
	require.NoError(t, err)
	assert.Nil(t, node, "Local bucket should return nil node")
}

// TestGetBucketNode_BucketNotFound tests getting node for non-existent bucket
func TestGetBucketNode_BucketNotFound(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")
	ctx := context.Background()

	node, err := router.GetBucketNode(ctx, "non-existent-bucket")
	require.Error(t, err)
	assert.Nil(t, node)
	assert.Contains(t, err.Error(), "bucket not found on any node")
}

// TestGetBucketReplicas_NoReplicationRules tests getting replicas when no rules exist
func TestGetBucketReplicas_NoReplicationRules(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	bucketMgr.AddBucket("my-bucket", "tenant-1")

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")
	ctx := context.Background()

	replicas, err := router.GetBucketReplicas(ctx, "my-bucket")
	require.NoError(t, err)
	assert.Nil(t, replicas, "No replicas should exist without replication rules")
}

// TestGetBucketReplicas_WithReplicationRules tests getting replicas with active rules
func TestGetBucketReplicas_WithReplicationRules(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	// Add local bucket
	bucketMgr.AddBucket("my-bucket", "tenant-1")

	// Add cluster nodes
	node1 := &Node{Name: "replica-1", Endpoint: "http://replica1:8080", NodeToken: "token1"}
	node2 := &Node{Name: "replica-2", Endpoint: "http://replica2:8080", NodeToken: "token2"}
	err := manager.AddNode(ctx, node1)
	require.NoError(t, err)
	err = manager.AddNode(ctx, node2)
	require.NoError(t, err)

	// Add replication rules
	replMgr.AddRule("tenant-1", "my-bucket", ReplicationRule{
		ID:                  "rule-1",
		DestinationEndpoint: "http://replica1:8080",
		DestinationBucket:   "my-bucket",
		Enabled:             true,
	})
	replMgr.AddRule("tenant-1", "my-bucket", ReplicationRule{
		ID:                  "rule-2",
		DestinationEndpoint: "http://replica2:8080",
		DestinationBucket:   "my-bucket",
		Enabled:             true,
	})

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")

	replicas, err := router.GetBucketReplicas(ctx, "my-bucket")
	require.NoError(t, err)
	require.NotNil(t, replicas)
	assert.Len(t, replicas, 2, "Should find 2 replica nodes")
	assert.Equal(t, "replica-1", replicas[0].Name)
	assert.Equal(t, "replica-2", replicas[1].Name)
}

// TestGetBucketReplicas_IgnoresDisabledRules tests that disabled rules are ignored
func TestGetBucketReplicas_IgnoresDisabledRules(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	bucketMgr.AddBucket("my-bucket", "tenant-1")

	node1 := &Node{Name: "replica-1", Endpoint: "http://replica1:8080", NodeToken: "token1"}
	err := manager.AddNode(ctx, node1)
	require.NoError(t, err)

	// Add disabled rule
	replMgr.AddRule("tenant-1", "my-bucket", ReplicationRule{
		ID:                  "rule-1",
		DestinationEndpoint: "http://replica1:8080",
		DestinationBucket:   "my-bucket",
		Enabled:             false, // Disabled
	})

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")

	replicas, err := router.GetBucketReplicas(ctx, "my-bucket")
	require.NoError(t, err)
	assert.Nil(t, replicas, "Disabled rules should not return replicas")
}

// TestGetBucketReplicas_BucketNotFound tests getting replicas for non-existent bucket
func TestGetBucketReplicas_BucketNotFound(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")
	ctx := context.Background()

	replicas, err := router.GetBucketReplicas(ctx, "non-existent")
	require.Error(t, err)
	assert.Nil(t, replicas)
	assert.Contains(t, err.Error(), "failed to get bucket tenant")
}

// TestIsNodeHealthy tests node health checking
func TestIsNodeHealthy(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")

	testCases := []struct {
		name     string
		node     *Node
		expected bool
	}{
		{
			name:     "Nil node",
			node:     nil,
			expected: false,
		},
		{
			name: "Healthy node",
			node: &Node{
				Name:         "healthy",
				HealthStatus: HealthStatusHealthy,
			},
			expected: true,
		},
		{
			name: "Degraded node",
			node: &Node{
				Name:         "degraded",
				HealthStatus: HealthStatusDegraded,
			},
			expected: false,
		},
		{
			name: "Unavailable node",
			node: &Node{
				Name:         "unavailable",
				HealthStatus: HealthStatusUnavailable,
			},
			expected: false,
		},
		{
			name: "Unknown status node",
			node: &Node{
				Name:         "unknown",
				HealthStatus: HealthStatusUnknown,
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := router.isNodeHealthy(tc.node)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestGetHealthyNodeForBucket_LocalBucket tests getting healthy node for local bucket
func TestGetHealthyNodeForBucket_LocalBucket(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	bucketMgr.AddBucket("local-bucket", "tenant-1")

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")
	ctx := context.Background()

	node, err := router.GetHealthyNodeForBucket(ctx, "local-bucket")
	require.NoError(t, err)
	assert.Nil(t, node, "Local bucket should return nil (local node)")
}

// TestGetHealthyNodeForBucket_BucketNotFound tests error when bucket doesn't exist
func TestGetHealthyNodeForBucket_BucketNotFound(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")
	ctx := context.Background()

	node, err := router.GetHealthyNodeForBucket(ctx, "non-existent")
	require.Error(t, err)
	assert.Nil(t, node)
}

// TestShouldRouteToRemoteNode_ClusterDisabled tests routing when cluster is disabled
func TestShouldRouteToRemoteNode_ClusterDisabled(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	// Cluster is disabled by default

	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")
	ctx := context.Background()

	shouldRoute, node, err := router.ShouldRouteToRemoteNode(ctx, "any-bucket")
	require.NoError(t, err)
	assert.False(t, shouldRoute)
	assert.Nil(t, node)
}

// TestShouldRouteToRemoteNode_LocalBucket tests routing for local bucket
func TestShouldRouteToRemoteNode_LocalBucket(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	bucketMgr.AddBucket("local-bucket", "tenant-1")

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")
	ctx := context.Background()

	shouldRoute, node, err := router.ShouldRouteToRemoteNode(ctx, "local-bucket")
	require.NoError(t, err)
	assert.False(t, shouldRoute)
	assert.Nil(t, node)
}

// TestRouteRequest_ClusterDisabled tests routing when cluster is disabled
func TestRouteRequest_ClusterDisabled(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")
	ctx := context.Background()

	node, isLocal, err := router.RouteRequest(ctx, "any-bucket")
	require.NoError(t, err)
	assert.True(t, isLocal, "Should be local when cluster disabled")
	assert.Nil(t, node)
}

// TestRouteRequest_LocalBucket_NoCache tests routing local bucket without cache
func TestRouteRequest_LocalBucket_NoCache(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Initialize cluster to enable it
	_, err := manager.InitializeCluster(ctx, "local-node", "us-east-1")
	require.NoError(t, err)

	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	bucketMgr.AddBucket("local-bucket", "tenant-1")

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")

	node, isLocal, err := router.RouteRequest(ctx, "local-bucket")
	require.NoError(t, err)
	assert.True(t, isLocal)
	assert.Nil(t, node)

	// Verify cache was updated
	cachedNodeID := router.cache.Get("local-bucket")
	assert.Equal(t, "local-node", cachedNodeID)
}

// TestRouteRequest_LocalBucket_WithCache tests routing local bucket with cache hit
func TestRouteRequest_LocalBucket_WithCache(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	bucketMgr.AddBucket("local-bucket", "tenant-1")

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")
	ctx := context.Background()

	// Warm up cache
	router.cache.Set("local-bucket", "local-node")

	node, isLocal, err := router.RouteRequest(ctx, "local-bucket")
	require.NoError(t, err)
	assert.True(t, isLocal)
	assert.Nil(t, node)
}

// TestRouteRequest_BucketNotFound tests routing for non-existent bucket
func TestRouteRequest_BucketNotFound(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Initialize cluster to enable it
	_, err := manager.InitializeCluster(ctx, "local-node", "us-east-1")
	require.NoError(t, err)

	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")

	node, isLocal, err := router.RouteRequest(ctx, "non-existent")
	require.Error(t, err)
	assert.False(t, isLocal)
	assert.Nil(t, node)
	assert.Contains(t, err.Error(), "bucket not found")
}

// TestInvalidateCache tests cache invalidation
func TestInvalidateCache(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")

	// Add to cache
	router.cache.Set("test-bucket", "node-123")

	// Verify it's in cache
	assert.Equal(t, "node-123", router.cache.Get("test-bucket"))

	// Invalidate
	router.InvalidateCache("test-bucket")

	// Verify it's removed
	assert.Empty(t, router.cache.Get("test-bucket"))
}

// TestGetCacheStats tests cache statistics retrieval
func TestGetCacheStats(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")

	// Add some entries to cache
	router.cache.Set("bucket1", "node1")
	router.cache.Set("bucket2", "node2")

	stats := router.GetCacheStats()
	assert.NotNil(t, stats)
	// Stats should contain size and other metrics
	size, exists := stats["size"]
	if exists {
		assert.Equal(t, 2, size)
	}
}

// TestRouteRequest_CacheEviction tests that stale cache entries are invalidated
func TestRouteRequest_CacheEviction(t *testing.T) {
	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Initialize cluster to enable it
	_, err := manager.InitializeCluster(ctx, "local-node", "us-east-1")
	require.NoError(t, err)

	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	// Add local bucket
	bucketMgr.AddBucket("test-bucket", "tenant-1")

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")

	// Add non-existent node to cache (simulate stale cache)
	router.cache.Set("test-bucket", "non-existent-node")

	// Route request - should invalidate cache and find local bucket
	node, isLocal, err := router.RouteRequest(ctx, "test-bucket")
	require.NoError(t, err)
	assert.True(t, isLocal)
	assert.Nil(t, node)

	// Verify cache was updated to correct value
	cachedNodeID := router.cache.Get("test-bucket")
	assert.Equal(t, "local-node", cachedNodeID)
}

// TestRouteRequest_ConcurrentAccess tests concurrent access to router
func TestRouteRequest_ConcurrentAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	db, cleanup := setupRouterTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	bucketMgr := NewMockBucketManager()
	replMgr := NewMockReplicationManager()

	// Add multiple buckets
	for i := 0; i < 10; i++ {
		bucketMgr.AddBucket(fmt.Sprintf("bucket-%d", i), "tenant-1")
	}

	router := createTestRouter(t, manager, bucketMgr, replMgr, "local-node")
	ctx := context.Background()

	// Spawn multiple goroutines accessing router concurrently
	done := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func(id int) {
			bucketName := fmt.Sprintf("bucket-%d", id%10)
			_, _, err := router.RouteRequest(ctx, bucketName)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 20; i++ {
		select {
		case <-done:
			// Success
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent requests")
		}
	}
}
