package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJoinCluster_Success tests successful cluster join operation
func TestJoinCluster_Success(t *testing.T) {
	// Setup test database for the joining node
	db, cleanup := setupTestDB(t)
	defer cleanup()

	joiningManager := NewManager(db, "http://localhost:9090")
	ctx := context.Background()

	// Setup mock server to simulate existing cluster node
	mockClusterNode := setupMockClusterNode(t, "test-cluster-token")
	defer mockClusterNode.Close()

	// Execute join
	err := joiningManager.JoinCluster(ctx, "test-cluster-token", mockClusterNode.URL)
	require.NoError(t, err, "JoinCluster should succeed")

	// Verify cluster config was updated
	config, err := joiningManager.GetConfig(ctx)
	require.NoError(t, err)
	assert.True(t, config.IsClusterEnabled, "Cluster should be enabled")
	assert.Equal(t, "test-cluster-token", config.ClusterToken, "Cluster token should match")
	assert.Equal(t, "us-east-1", config.Region, "Region should be set from cluster")
	assert.NotEmpty(t, config.NodeID, "Node ID should be generated")
	assert.NotEmpty(t, config.NodeName, "Node name should be generated")
}

// TestJoinCluster_InvalidToken tests join with invalid cluster token
func TestJoinCluster_InvalidToken(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	joiningManager := NewManager(db, "http://localhost:9090")
	ctx := context.Background()

	// Setup mock server that rejects the token
	mockClusterNode := setupMockClusterNodeWithInvalidToken(t)
	defer mockClusterNode.Close()

	// Execute join with invalid token
	err := joiningManager.JoinCluster(ctx, "invalid-token", mockClusterNode.URL)
	require.Error(t, err, "JoinCluster should fail with invalid token")
	assert.Contains(t, err.Error(), "invalid cluster token", "Error should mention invalid token")
}

// TestJoinCluster_NodeRegistrationFailure tests join when node registration fails
func TestJoinCluster_NodeRegistrationFailure(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	joiningManager := NewManager(db, "http://localhost:9090")
	ctx := context.Background()

	// Setup mock server that validates token but fails registration
	mockClusterNode := setupMockClusterNodeWithRegistrationFailure(t, "test-cluster-token")
	defer mockClusterNode.Close()

	// Execute join
	err := joiningManager.JoinCluster(ctx, "test-cluster-token", mockClusterNode.URL)
	require.Error(t, err, "JoinCluster should fail when registration fails")
	assert.Contains(t, err.Error(), "failed to register with cluster", "Error should mention registration failure")
}

// TestJoinCluster_NetworkError tests join when network communication fails
func TestJoinCluster_NetworkError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	joiningManager := NewManager(db, "http://localhost:9090")
	ctx := context.Background()

	// Use invalid endpoint to trigger network error
	err := joiningManager.JoinCluster(ctx, "test-cluster-token", "http://invalid-endpoint:99999")
	require.Error(t, err, "JoinCluster should fail with network error")
	assert.Contains(t, err.Error(), "failed to validate cluster token", "Error should mention token validation failure")
}

// TestJoinCluster_NodeSynchronization tests that nodes are synchronized after join
func TestJoinCluster_NodeSynchronization(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	joiningManager := NewManager(db, "http://localhost:9090")
	ctx := context.Background()

	// Setup mock server with multiple existing nodes
	mockClusterNode := setupMockClusterNodeWithNodes(t, "test-cluster-token")
	defer mockClusterNode.Close()

	// Execute join
	err := joiningManager.JoinCluster(ctx, "test-cluster-token", mockClusterNode.URL)
	require.NoError(t, err, "JoinCluster should succeed")

	// Verify nodes were synchronized
	nodes, err := joiningManager.ListNodes(ctx)
	require.NoError(t, err)

	// Should have at least the nodes from the cluster (excluding self)
	// Mock returns 3 nodes, one will be self, so we should see 2 others
	assert.GreaterOrEqual(t, len(nodes), 2, "Should have synchronized other cluster nodes")
}

// Helper function to setup a mock cluster node server that validates tokens
func setupMockClusterNode(t *testing.T, validToken string) *httptest.Server {
	mux := http.NewServeMux()

	// Validate token endpoint
	mux.HandleFunc("/api/internal/cluster/validate-token", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ClusterToken string `json:"cluster_token"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if req.ClusterToken == validToken {
			response := map[string]interface{}{
				"valid": true,
				"cluster_info": map[string]interface{}{
					"cluster_id": "cluster-123",
					"region":     "us-east-1",
					"node_count": 2,
				},
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Invalid cluster token",
			})
		}
	})

	// Register node endpoint
	mux.HandleFunc("/api/internal/cluster/register-node", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ClusterToken string `json:"cluster_token"`
			Node         *Node  `json:"node"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if req.ClusterToken == validToken {
			response := map[string]interface{}{
				"node": req.Node,
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		} else {
			w.WriteHeader(http.StatusUnauthorized)
		}
	})

	// List nodes endpoint
	mux.HandleFunc("/api/internal/cluster/nodes", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("cluster_token")

		if token == validToken {
			nodes := []*Node{
				{
					ID:       "node-1",
					Name:     "master-node",
					Endpoint: "http://localhost:8080",
					Region:   "us-east-1",
				},
			}
			response := map[string]interface{}{
				"nodes": nodes,
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		} else {
			w.WriteHeader(http.StatusUnauthorized)
		}
	})

	return httptest.NewServer(mux)
}

// Helper function for mock server that rejects tokens
func setupMockClusterNodeWithInvalidToken(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/internal/cluster/validate-token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Invalid cluster token",
		})
	})

	return httptest.NewServer(mux)
}

// Helper function for mock server that fails registration
func setupMockClusterNodeWithRegistrationFailure(t *testing.T, validToken string) *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/internal/cluster/validate-token", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"valid": true,
			"cluster_info": map[string]interface{}{
				"cluster_id": "cluster-123",
				"region":     "us-east-1",
				"node_count": 1,
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	mux.HandleFunc("/api/internal/cluster/register-node", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Failed to register node",
		})
	})

	return httptest.NewServer(mux)
}

// Helper function for mock server with multiple nodes
func setupMockClusterNodeWithNodes(t *testing.T, validToken string) *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/internal/cluster/validate-token", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"valid": true,
			"cluster_info": map[string]interface{}{
				"cluster_id": "cluster-123",
				"region":     "us-east-1",
				"node_count": 3,
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	mux.HandleFunc("/api/internal/cluster/register-node", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Node *Node `json:"node"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		response := map[string]interface{}{
			"node": req.Node,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	mux.HandleFunc("/api/internal/cluster/nodes", func(w http.ResponseWriter, r *http.Request) {
		nodes := []*Node{
			{
				ID:       "node-1",
				Name:     "master-node",
				Endpoint: "http://localhost:8080",
				Region:   "us-east-1",
			},
			{
				ID:       "node-2",
				Name:     "worker-node-1",
				Endpoint: "http://localhost:8081",
				Region:   "us-east-1",
			},
			{
				ID:       "new-node-id",
				Name:     "new-joining-node",
				Endpoint: "http://localhost:9090",
				Region:   "us-east-1",
			},
		}
		response := map[string]interface{}{
			"nodes": nodes,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	return httptest.NewServer(mux)
}
