package cluster

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "cluster-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create database
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to open database: %v", err)
	}

	// Initialize schema
	if err := InitSchema(db); err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

func TestInitializeCluster(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	token, err := manager.InitializeCluster(ctx, "test-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	if token == "" {
		t.Error("Expected non-empty cluster token")
	}

	// Verify config was created
	config, err := manager.GetConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	if config.NodeName != "test-node" {
		t.Errorf("Expected node name 'test-node', got '%s'", config.NodeName)
	}

	if config.Region != "us-east-1" {
		t.Errorf("Expected region 'us-east-1', got '%s'", config.Region)
	}

	if !config.IsClusterEnabled {
		t.Error("Expected cluster to be enabled")
	}
}

func TestInitializeCluster_AlreadyInitialized(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize once
	_, err := manager.InitializeCluster(ctx, "test-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	// Try to initialize again
	_, err = manager.InitializeCluster(ctx, "test-node-2", "us-west-1")
	if err == nil {
		t.Error("Expected error when initializing cluster twice")
	}
}

func TestAddNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	node := &Node{
		Name:      "node-1",
		Endpoint:  "http://node1.example.com:8080",
		NodeToken: "test-token-123",
		Region:    "us-east-1",
		Priority:  100,
		Metadata:  "{}",
	}

	err := manager.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	if node.ID == "" {
		t.Error("Expected node ID to be generated")
	}
}

func TestGetNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Add a node
	node := &Node{
		Name:      "node-1",
		Endpoint:  "http://node1.example.com:8080",
		NodeToken: "test-token-123",
		Region:    "us-east-1",
		Priority:  100,
		Metadata:  "{}",
	}

	err := manager.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	// Get the node
	retrieved, err := manager.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("Failed to get node: %v", err)
	}

	if retrieved.Name != node.Name {
		t.Errorf("Expected name '%s', got '%s'", node.Name, retrieved.Name)
	}

	if retrieved.Endpoint != node.Endpoint {
		t.Errorf("Expected endpoint '%s', got '%s'", node.Endpoint, retrieved.Endpoint)
	}
}

func TestGetNode_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	_, err := manager.GetNode(ctx, "non-existent-id")
	if err == nil {
		t.Error("Expected error when getting non-existent node")
	}
}

func TestListNodes(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Add multiple nodes
	for i := 1; i <= 3; i++ {
		node := &Node{
			Name:      "node-" + string(rune('0'+i)),
			Endpoint:  "http://node" + string(rune('0'+i)) + ".example.com:8080",
			NodeToken: "token-" + string(rune('0'+i)),
			Region:    "us-east-1",
			Priority:  100 + i,
			Metadata:  "{}",
		}

		err := manager.AddNode(ctx, node)
		if err != nil {
			t.Fatalf("Failed to add node: %v", err)
		}
	}

	// List all nodes
	nodes, err := manager.ListNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to list nodes: %v", err)
	}

	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(nodes))
	}
}

func TestUpdateNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Add a node
	node := &Node{
		Name:      "node-1",
		Endpoint:  "http://node1.example.com:8080",
		NodeToken: "test-token-123",
		Region:    "us-east-1",
		Priority:  100,
		Metadata:  "{}",
	}

	err := manager.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	// Update the node
	node.Name = "updated-node-1"
	node.Priority = 200

	err = manager.UpdateNode(ctx, node)
	if err != nil {
		t.Fatalf("Failed to update node: %v", err)
	}

	// Verify update
	retrieved, err := manager.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("Failed to get node: %v", err)
	}

	if retrieved.Name != "updated-node-1" {
		t.Errorf("Expected name 'updated-node-1', got '%s'", retrieved.Name)
	}

	if retrieved.Priority != 200 {
		t.Errorf("Expected priority 200, got %d", retrieved.Priority)
	}
}

func TestRemoveNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Add a node
	node := &Node{
		Name:      "node-1",
		Endpoint:  "http://node1.example.com:8080",
		NodeToken: "test-token-123",
		Region:    "us-east-1",
		Priority:  100,
		Metadata:  "{}",
	}

	err := manager.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	// Remove the node
	err = manager.RemoveNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("Failed to remove node: %v", err)
	}

	// Verify removal
	_, err = manager.GetNode(ctx, node.ID)
	if err == nil {
		t.Error("Expected error when getting removed node")
	}
}

func TestGetHealthyNodes(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Add healthy node
	healthyNode := &Node{
		Name:         "healthy-node",
		Endpoint:     "http://healthy.example.com:8080",
		NodeToken:    "token-1",
		Region:       "us-east-1",
		Priority:     100,
		Metadata:     "{}",
		HealthStatus: HealthStatusHealthy,
	}

	err := manager.AddNode(ctx, healthyNode)
	if err != nil {
		t.Fatalf("Failed to add healthy node: %v", err)
	}

	// Update health status to healthy
	_, err = db.ExecContext(ctx, "UPDATE cluster_nodes SET health_status = ? WHERE id = ?", HealthStatusHealthy, healthyNode.ID)
	if err != nil {
		t.Fatalf("Failed to update health status: %v", err)
	}

	// Add unhealthy node
	unhealthyNode := &Node{
		Name:         "unhealthy-node",
		Endpoint:     "http://unhealthy.example.com:8080",
		NodeToken:    "token-2",
		Region:       "us-east-1",
		Priority:     100,
		Metadata:     "{}",
		HealthStatus: HealthStatusUnavailable,
	}

	err = manager.AddNode(ctx, unhealthyNode)
	if err != nil {
		t.Fatalf("Failed to add unhealthy node: %v", err)
	}

	// Get healthy nodes
	healthyNodes, err := manager.GetHealthyNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to get healthy nodes: %v", err)
	}

	// Should only have 1 healthy node
	if len(healthyNodes) != 1 {
		t.Errorf("Expected 1 healthy node, got %d", len(healthyNodes))
	}

	if len(healthyNodes) > 0 && healthyNodes[0].Name != "healthy-node" {
		t.Errorf("Expected healthy node to be 'healthy-node', got '%s'", healthyNodes[0].Name)
	}
}

func TestGetClusterStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster first
	_, err := manager.InitializeCluster(ctx, "test-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	// Add nodes with different health statuses
	nodes := []*Node{
		{Name: "node-1", Endpoint: "http://node1:8080", NodeToken: "token-1", HealthStatus: HealthStatusHealthy},
		{Name: "node-2", Endpoint: "http://node2:8080", NodeToken: "token-2", HealthStatus: HealthStatusDegraded},
		{Name: "node-3", Endpoint: "http://node3:8080", NodeToken: "token-3", HealthStatus: HealthStatusUnavailable},
	}

	for _, node := range nodes {
		err := manager.AddNode(ctx, node)
		if err != nil {
			t.Fatalf("Failed to add node: %v", err)
		}

		// Update health status
		_, err = db.ExecContext(ctx, "UPDATE cluster_nodes SET health_status = ? WHERE id = ?", node.HealthStatus, node.ID)
		if err != nil {
			t.Fatalf("Failed to update health status: %v", err)
		}
	}

	// Get cluster status
	status, err := manager.GetClusterStatus(ctx)
	if err != nil {
		t.Fatalf("Failed to get cluster status: %v", err)
	}

	if !status.IsEnabled {
		t.Error("Expected cluster to be enabled")
	}

	// InitializeCluster creates 1 node (test-node) + 3 added nodes = 4 total
	if status.TotalNodes != 4 {
		t.Errorf("Expected 4 total nodes, got %d", status.TotalNodes)
	}

	// test-node (healthy by default) + node-1 (healthy) = 2 healthy nodes
	if status.HealthyNodes != 2 {
		t.Errorf("Expected 2 healthy nodes, got %d", status.HealthyNodes)
	}

	if status.DegradedNodes != 1 {
		t.Errorf("Expected 1 degraded node, got %d", status.DegradedNodes)
	}

	if status.UnavailableNodes != 1 {
		t.Errorf("Expected 1 unavailable node, got %d", status.UnavailableNodes)
	}
}

func TestIsClusterEnabled(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initially, cluster should not be enabled
	if manager.IsClusterEnabled() {
		t.Error("Expected cluster to be disabled initially")
	}

	// Initialize cluster
	_, err := manager.InitializeCluster(ctx, "test-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	// Now cluster should be enabled
	if !manager.IsClusterEnabled() {
		t.Error("Expected cluster to be enabled after initialization")
	}
}

func TestLeaveCluster(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster
	_, err := manager.InitializeCluster(ctx, "test-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	// Verify cluster is enabled
	if !manager.IsClusterEnabled() {
		t.Error("Expected cluster to be enabled")
	}

	// Leave cluster
	err = manager.LeaveCluster(ctx)
	if err != nil {
		t.Fatalf("Failed to leave cluster: %v", err)
	}

	// Verify cluster is disabled
	if manager.IsClusterEnabled() {
		t.Error("Expected cluster to be disabled after leaving")
	}
}

func TestSetStorage(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")

	// SetStorage should not panic and should set the storage
	manager.SetStorage(nil)

	// Verify storage is set (can't directly test private field, but no panic is success)
	if manager.storage != nil {
		t.Error("Expected storage to be nil after SetStorage(nil)")
	}
}

func TestSetACLManager(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")

	// SetACLManager should not panic and should set the ACL manager
	manager.SetACLManager(nil)

	// Verify ACL manager is set (can't directly test private field, but no panic is success)
	if manager.aclManager != nil {
		t.Error("Expected aclManager to be nil after SetACLManager(nil)")
	}
}

func TestUpdateNodeBucketCount(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster
	_, err := manager.InitializeCluster(ctx, "test-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	// Add a node
	node := &Node{
		ID:           "test-node-1",
		Name:         "Test Node 1",
		Endpoint:     "http://node1:8080",
		NodeToken:    "token123",
		HealthStatus: "healthy",
	}
	err = manager.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	// Update bucket count
	err = manager.UpdateNodeBucketCount(ctx, "test-node-1", 5)
	if err != nil {
		t.Fatalf("Failed to update bucket count: %v", err)
	}

	// Verify bucket count was updated
	retrievedNode, err := manager.GetNode(ctx, "test-node-1")
	if err != nil {
		t.Fatalf("Failed to get node: %v", err)
	}

	if retrievedNode.BucketCount != 5 {
		t.Errorf("Expected bucket count 5, got %d", retrievedNode.BucketCount)
	}
}

func TestUpdateLocalNodeBucketCount(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster (this also adds the local node to cluster_nodes)
	_, err := manager.InitializeCluster(ctx, "test-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	// Get the local node ID from cluster config
	config, err := manager.GetConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get cluster config: %v", err)
	}

	// Update local bucket count
	err = manager.UpdateLocalNodeBucketCount(ctx, 10)
	if err != nil {
		t.Fatalf("Failed to update local bucket count: %v", err)
	}

	// Verify bucket count was updated
	retrievedNode, err := manager.GetNode(ctx, config.NodeID)
	if err != nil {
		t.Fatalf("Failed to get node: %v", err)
	}

	if retrievedNode.BucketCount != 10 {
		t.Errorf("Expected bucket count 10, got %d", retrievedNode.BucketCount)
	}
}

func TestClose(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")

	// Close should not panic
	err := manager.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	// Stop channel should be closed
	select {
	case <-manager.stopChan:
		// Expected - channel is closed
	default:
		t.Error("Expected stop channel to be closed")
	}
}
