package cluster

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockManager simulates a cluster manager for integration testing
type mockManager struct {
	nodes          []*Node
	localNodeID    string
	localNodeToken string
	mu             sync.RWMutex
}

func (m *mockManager) GetHealthyNodes(ctx context.Context) ([]*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nodes, nil
}

func (m *mockManager) GetLocalNodeID(ctx context.Context) (string, error) {
	return m.localNodeID, nil
}

func (m *mockManager) GetLocalNodeToken(ctx context.Context) (string, error) {
	return m.localNodeToken, nil
}

func (m *mockManager) GetLocalNodeName(ctx context.Context) (string, error) {
	return "local-node", nil
}

func (m *mockManager) GetTLSConfig() *tls.Config {
	return nil
}

func (m *mockManager) IsClusterEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.nodes) > 0
}

// TestQuotaAggregator_GetTenantTotalStorage_Integration tests the full aggregation flow
func TestQuotaAggregator_GetTenantTotalStorage_Integration(t *testing.T) {
	// Create 3 mock nodes with different storage values
	node1Storage := int64(1024 * 1024 * 100) // 100MB
	node2Storage := int64(1024 * 1024 * 200) // 200MB
	node3Storage := int64(1024 * 1024 * 300) // 300MB
	expectedTotal := node1Storage + node2Storage + node3Storage // 600MB

	// Create mock servers for each node
	server1 := createStorageServer(t, "tenant1", node1Storage)
	defer server1.Close()

	server2 := createStorageServer(t, "tenant1", node2Storage)
	defer server2.Close()

	server3 := createStorageServer(t, "tenant1", node3Storage)
	defer server3.Close()

	// Create mock cluster manager
	mgr := &mockManager{
		nodes: []*Node{
			{ID: "node1", Name: "node-1", Endpoint: server1.URL, HealthStatus: "healthy"},
			{ID: "node2", Name: "node-2", Endpoint: server2.URL, HealthStatus: "healthy"},
			{ID: "node3", Name: "node-3", Endpoint: server3.URL, HealthStatus: "healthy"},
		},
		localNodeID:    "test-node",
		localNodeToken: "test-token",
	}

	// Create quota aggregator
	aggregator := NewQuotaAggregator(mgr)

	// Test GetTenantTotalStorage
	ctx := context.Background()
	totalStorage, err := aggregator.GetTenantTotalStorage(ctx, "tenant1")

	require.NoError(t, err)
	assert.Equal(t, expectedTotal, totalStorage, "Should sum storage from all 3 nodes")

	t.Logf("✓ Successfully aggregated storage: %d bytes from 3 nodes", totalStorage)
	t.Logf("  Node1: %d bytes", node1Storage)
	t.Logf("  Node2: %d bytes", node2Storage)
	t.Logf("  Node3: %d bytes", node3Storage)
	t.Logf("  Total: %d bytes", totalStorage)
}

// TestQuotaAggregator_GetTenantTotalStorage_PartialFailure tests graceful degradation
func TestQuotaAggregator_GetTenantTotalStorage_PartialFailure(t *testing.T) {
	node1Storage := int64(1024 * 1024 * 100) // 100MB
	node3Storage := int64(1024 * 1024 * 300) // 300MB
	expectedTotal := node1Storage + node3Storage // 400MB (node2 fails)

	// Node1: success
	server1 := createStorageServer(t, "tenant1", node1Storage)
	defer server1.Close()

	// Node2: failure
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server2.Close()

	// Node3: success
	server3 := createStorageServer(t, "tenant1", node3Storage)
	defer server3.Close()

	mgr := &mockManager{
		nodes: []*Node{
			{ID: "node1", Name: "node-1", Endpoint: server1.URL, HealthStatus: "healthy"},
			{ID: "node2", Name: "node-2", Endpoint: server2.URL, HealthStatus: "healthy"},
			{ID: "node3", Name: "node-3", Endpoint: server3.URL, HealthStatus: "healthy"},
		},
		localNodeID:    "test-node",
		localNodeToken: "test-token",
	}

	aggregator := NewQuotaAggregator(mgr)

	ctx := context.Background()
	totalStorage, err := aggregator.GetTenantTotalStorage(ctx, "tenant1")

	// Should succeed with partial results
	require.NoError(t, err)
	assert.Equal(t, expectedTotal, totalStorage, "Should sum storage from 2 healthy nodes only")

	t.Logf("✓ Gracefully handled node failure:")
	t.Logf("  Node1: %d bytes (success)", node1Storage)
	t.Logf("  Node2: failed (skipped)")
	t.Logf("  Node3: %d bytes (success)", node3Storage)
	t.Logf("  Total: %d bytes", totalStorage)
}

// TestQuotaAggregator_GetTenantTotalStorage_AllNodesFail tests complete failure
func TestQuotaAggregator_GetTenantTotalStorage_AllNodesFail(t *testing.T) {
	// All nodes fail
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server2.Close()

	mgr := &mockManager{
		nodes: []*Node{
			{ID: "node1", Name: "node-1", Endpoint: server1.URL, HealthStatus: "healthy"},
			{ID: "node2", Name: "node-2", Endpoint: server2.URL, HealthStatus: "healthy"},
		},
		localNodeID:    "test-node",
		localNodeToken: "test-token",
	}

	aggregator := NewQuotaAggregator(mgr)

	ctx := context.Background()
	totalStorage, err := aggregator.GetTenantTotalStorage(ctx, "tenant1")

	// Should fail when all nodes fail
	assert.Error(t, err)
	assert.Equal(t, int64(0), totalStorage)
	assert.Contains(t, err.Error(), "failed to query storage from all")

	t.Logf("✓ Correctly returned error when all nodes failed")
}

// TestQuotaAggregator_GetTenantTotalStorage_EmptyCluster tests with no nodes
func TestQuotaAggregator_GetTenantTotalStorage_EmptyCluster(t *testing.T) {
	mgr := &mockManager{
		nodes:          []*Node{},
		localNodeID:    "test-node",
		localNodeToken: "test-token",
	}

	aggregator := NewQuotaAggregator(mgr)

	ctx := context.Background()
	totalStorage, err := aggregator.GetTenantTotalStorage(ctx, "tenant1")

	require.NoError(t, err)
	assert.Equal(t, int64(0), totalStorage, "Should return 0 for empty cluster")

	t.Logf("✓ Correctly handled empty cluster")
}

// TestQuotaAggregator_GetTenantStorageByNode tests breakdown by node
func TestQuotaAggregator_GetTenantStorageByNode(t *testing.T) {
	node1Storage := int64(1024 * 1024 * 100) // 100MB
	node2Storage := int64(1024 * 1024 * 200) // 200MB

	server1 := createStorageServer(t, "tenant1", node1Storage)
	defer server1.Close()

	server2 := createStorageServer(t, "tenant1", node2Storage)
	defer server2.Close()

	mgr := &mockManager{
		nodes: []*Node{
			{ID: "node1", Name: "node-1", Endpoint: server1.URL, HealthStatus: "healthy"},
			{ID: "node2", Name: "node-2", Endpoint: server2.URL, HealthStatus: "healthy"},
		},
		localNodeID:    "test-node",
		localNodeToken: "test-token",
	}

	aggregator := NewQuotaAggregator(mgr)

	ctx := context.Background()
	breakdown, err := aggregator.GetTenantStorageByNode(ctx, "tenant1")

	require.NoError(t, err)
	assert.Len(t, breakdown, 2, "Should have storage info for 2 nodes")
	assert.Equal(t, node1Storage, breakdown["node-1"])
	assert.Equal(t, node2Storage, breakdown["node-2"])

	t.Logf("✓ Storage breakdown by node:")
	for nodeName, storage := range breakdown {
		t.Logf("  %s: %d bytes", nodeName, storage)
	}
}

// TestQuotaAggregator_LargeScale tests with many nodes
func TestQuotaAggregator_LargeScale(t *testing.T) {
	const numNodes = 10
	const storagePerNode = int64(1024 * 1024 * 100) // 100MB per node
	expectedTotal := storagePerNode * numNodes      // 1GB total

	var servers []*httptest.Server
	var nodes []*Node

	// Create 10 nodes
	for i := 0; i < numNodes; i++ {
		server := createStorageServer(t, "tenant1", storagePerNode)
		servers = append(servers, server)
		nodes = append(nodes, &Node{
			ID:           string(rune('a' + i)),
			Name:         string(rune('a' + i)),
			Endpoint:     server.URL,
			HealthStatus: "healthy",
		})
	}

	// Clean up all servers
	defer func() {
		for _, s := range servers {
			s.Close()
		}
	}()

	mgr := &mockManager{
		nodes:          nodes,
		localNodeID:    "test-node",
		localNodeToken: "test-token",
	}

	aggregator := NewQuotaAggregator(mgr)

	ctx := context.Background()
	start := time.Now()
	totalStorage, err := aggregator.GetTenantTotalStorage(ctx, "tenant1")
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, expectedTotal, totalStorage, "Should sum storage from all 10 nodes")

	t.Logf("✓ Large scale test (10 nodes):")
	t.Logf("  Total storage: %d bytes", totalStorage)
	t.Logf("  Query duration: %v", duration)
	t.Logf("  Avg per node: %v", duration/numNodes)

	// Parallel queries should be fast (under 1 second for 10 nodes with 5s timeout each)
	assert.Less(t, duration.Seconds(), 6.0, "Parallel queries should complete quickly")
}

// Helper function to create a storage server
func createStorageServer(t *testing.T, tenantID string, storageBytes int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/api/internal/cluster/tenant/")
		assert.Contains(t, r.URL.Path, "/storage")

		storageInfo := TenantStorageInfo{
			TenantID:            tenantID,
			CurrentStorageBytes: storageBytes,
			NodeID:              "",
			NodeName:            "",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(storageInfo)
	}))
}
