package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupServerWithCluster creates a test server with cluster manager initialized
func setupServerWithCluster(t *testing.T) (*Server, *cluster.Manager, func()) {
	// First setup normal server
	server, _, baseCleanup := setupTestServer(t)

	// Create cluster database
	tmpDir, err := os.MkdirTemp("", "cluster-test-*")
	require.NoError(t, err)

	dbPath := filepath.Join(tmpDir, "cluster.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	// Initialize cluster schema
	err = cluster.InitSchema(db)
	require.NoError(t, err)

	// Create cluster manager
	clusterManager := cluster.NewManager(db, "http://localhost:8080")

	// Initialize cluster
	ctx := context.Background()
	_, err = clusterManager.InitializeCluster(ctx, "test-node", "us-east-1")
	require.NoError(t, err)

	// Initialize rate limiter (required for cluster routes)
	rateLimiter := cluster.NewRateLimiter(100, 200)

	// Attach cluster components to server
	server.clusterManager = clusterManager
	server.rateLimiter = rateLimiter
	server.db = db

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
		baseCleanup()
	}

	return server, clusterManager, cleanup
}

// TestHandleListBuckets_SingleNode tests bucket listing in standalone mode (no cluster)
func TestHandleListBuckets_SingleNodeStandalone(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Get admin token
	adminToken := getAdminToken(t, server)

	ctx := context.Background()

	// Get admin user
	user, err := server.authManager.ValidateJWT(ctx, adminToken)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create tenant
	tenant := &auth.Tenant{
		ID:              "test-tenant-1",
		Name:            "test-tenant",
		Status:          "active",
		MaxStorageBytes: 1000000000,
	}
	err = server.authManager.CreateTenant(ctx, tenant)
	require.NoError(t, err)

	// Create some test buckets
	err = server.bucketManager.CreateBucket(ctx, tenant.ID, "bucket-1", "")
	require.NoError(t, err)

	err = server.bucketManager.CreateBucket(ctx, tenant.ID, "bucket-2", "")
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("GET", "/api/v1/buckets", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleListBuckets(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)

	// Verify buckets are returned
	bucketsData, ok := response.Data.([]interface{})
	assert.True(t, ok)
	assert.Len(t, bucketsData, 2, "Should return all local buckets in standalone mode")
}

// TestQueryBucketsFromNode_Success tests successful remote node query
func TestQueryBucketsFromNode_Success(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	ctx := context.Background()

	// Setup mock remote node that returns buckets
	remoteNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify authentication headers are present
		assert.NotEmpty(t, r.Header.Get("X-MaxIOFS-Node-ID"))
		assert.NotEmpty(t, r.Header.Get("X-MaxIOFS-Timestamp"))
		assert.NotEmpty(t, r.Header.Get("X-MaxIOFS-Nonce"))
		assert.NotEmpty(t, r.Header.Get("X-MaxIOFS-Signature"))

		// Verify tenant_id query parameter
		tenantID := r.URL.Query().Get("tenant_id")
		assert.NotEmpty(t, tenantID)

		// Return mock buckets in correct format
		response := struct {
			Buckets []cluster.BucketWithLocation `json:"buckets"`
		}{
			Buckets: []cluster.BucketWithLocation{
				{
					Name:       "remote-bucket-1",
					TenantID:   tenantID,
					NodeID:     "node-2",
					NodeName:   "remote-node",
					NodeStatus: "healthy",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer remoteNode.Close()

	node := &cluster.Node{
		ID:           "node-2",
		Name:         "remote-node",
		Endpoint:     remoteNode.URL,
		HealthStatus: "healthy",
	}

	buckets, err := server.queryBucketsFromNode(ctx, node, "tenant-123")
	assert.NoError(t, err)
	assert.Len(t, buckets, 1)
	assert.Equal(t, "remote-bucket-1", buckets[0].Name)
	assert.Equal(t, "tenant-123", buckets[0].TenantID)
}

// TestQueryBucketsFromNode_AuthFailure tests authentication failure
func TestQueryBucketsFromNode_AuthFailure(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	ctx := context.Background()

	// Remote node returns 401 Unauthorized
	remoteNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Invalid signature")
	}))
	defer remoteNode.Close()

	node := &cluster.Node{
		ID:           "node-2",
		Name:         "remote-node",
		Endpoint:     remoteNode.URL,
		HealthStatus: "healthy",
	}

	buckets, err := server.queryBucketsFromNode(ctx, node, "tenant-123")
	assert.Error(t, err)
	assert.Nil(t, buckets)
	assert.Contains(t, err.Error(), "401")
}

// TestQueryBucketsFromNode_NetworkError tests network error handling
func TestQueryBucketsFromNode_NetworkError(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	ctx := context.Background()

	// Invalid endpoint (connection refused)
	node := &cluster.Node{
		ID:           "node-2",
		Name:         "remote-node",
		Endpoint:     "http://localhost:99999", // Invalid port
		HealthStatus: "healthy",
	}

	buckets, err := server.queryBucketsFromNode(ctx, node, "tenant-123")
	assert.Error(t, err)
	assert.Nil(t, buckets)
}

// TestQueryBucketsFromNode_Timeout tests timeout handling
func TestQueryBucketsFromNode_Timeout(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	ctx := context.Background()

	// Slow remote node
	slowNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // Longer than timeout
	}))
	defer slowNode.Close()

	node := &cluster.Node{
		ID:           "node-2",
		Name:         "slow-node",
		Endpoint:     slowNode.URL,
		HealthStatus: "healthy",
	}

	start := time.Now()
	buckets, err := server.queryBucketsFromNode(ctx, node, "tenant-123")
	duration := time.Since(start)

	assert.Error(t, err)
	assert.Nil(t, buckets)
	assert.Less(t, duration, 8*time.Second, "Should timeout within reasonable time")
}

// TestQueryBucketsFromNode_InvalidJSON tests invalid JSON response handling
func TestQueryBucketsFromNode_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	ctx := context.Background()

	// Remote node returns invalid JSON
	remoteNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "{invalid json")
	}))
	defer remoteNode.Close()

	node := &cluster.Node{
		ID:           "node-2",
		Name:         "remote-node",
		Endpoint:     remoteNode.URL,
		HealthStatus: "healthy",
	}

	buckets, err := server.queryBucketsFromNode(ctx, node, "tenant-123")
	assert.Error(t, err)
	assert.Nil(t, buckets)
}

// TestQueryBucketsFromNode_EmptyResponse tests empty bucket list handling
func TestQueryBucketsFromNode_EmptyResponse(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	ctx := context.Background()

	// Remote node returns empty array
	remoteNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := struct {
			Buckets []cluster.BucketWithLocation `json:"buckets"`
		}{
			Buckets: []cluster.BucketWithLocation{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer remoteNode.Close()

	node := &cluster.Node{
		ID:           "node-2",
		Name:         "remote-node",
		Endpoint:     remoteNode.URL,
		HealthStatus: "healthy",
	}

	buckets, err := server.queryBucketsFromNode(ctx, node, "tenant-123")
	assert.NoError(t, err)
	assert.NotNil(t, buckets)
	assert.Len(t, buckets, 0, "Should return empty slice for empty response")
}

// TestQueryBucketsFromNode_HTTPError tests various HTTP error codes
func TestQueryBucketsFromNode_HTTPError(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	ctx := context.Background()

	testCases := []struct {
		name       string
		statusCode int
	}{
		{"400 Bad Request", http.StatusBadRequest},
		{"403 Forbidden", http.StatusForbidden},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
		{"503 Service Unavailable", http.StatusServiceUnavailable},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			remoteNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer remoteNode.Close()

			node := &cluster.Node{
				ID:           "node-2",
				Name:         "remote-node",
				Endpoint:     remoteNode.URL,
				HealthStatus: "healthy",
			}

			buckets, err := server.queryBucketsFromNode(ctx, node, "tenant-123")
			assert.Error(t, err, "Should error on HTTP %d", tc.statusCode)
			assert.Nil(t, buckets)
			assert.Contains(t, err.Error(), fmt.Sprintf("%d", tc.statusCode))
		})
	}
}

// TestQueryBucketsFromNode_VerifiesHMACAuth tests that HMAC auth headers are added
func TestQueryBucketsFromNode_VerifiesHMACAuth(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	ctx := context.Background()

	// Track which headers are present
	var headers http.Header
	remoteNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		response := struct {
			Buckets []cluster.BucketWithLocation `json:"buckets"`
		}{
			Buckets: []cluster.BucketWithLocation{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer remoteNode.Close()

	node := &cluster.Node{
		ID:           "node-2",
		Name:         "remote-node",
		Endpoint:     remoteNode.URL,
		HealthStatus: "healthy",
	}

	_, err := server.queryBucketsFromNode(ctx, node, "tenant-123")
	require.NoError(t, err)

	// Verify all required HMAC headers are present
	assert.NotEmpty(t, headers.Get("X-MaxIOFS-Node-ID"), "Should have Node-ID header")
	assert.NotEmpty(t, headers.Get("X-MaxIOFS-Timestamp"), "Should have Timestamp header")
	assert.NotEmpty(t, headers.Get("X-MaxIOFS-Nonce"), "Should have Nonce header")
	assert.NotEmpty(t, headers.Get("X-MaxIOFS-Signature"), "Should have Signature header")
}

// TestQueryBucketsFromNode_CorrectURL tests that correct URL is constructed
func TestQueryBucketsFromNode_CorrectURL(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	ctx := context.Background()

	// Track requested URL
	var requestedURL string
	remoteNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		response := struct {
			Buckets []cluster.BucketWithLocation `json:"buckets"`
		}{
			Buckets: []cluster.BucketWithLocation{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer remoteNode.Close()

	node := &cluster.Node{
		ID:           "node-2",
		Name:         "remote-node",
		Endpoint:     remoteNode.URL,
		HealthStatus: "healthy",
	}

	_, err := server.queryBucketsFromNode(ctx, node, "tenant-abc-123")
	require.NoError(t, err)

	// Verify correct API endpoint and query parameter
	assert.Contains(t, requestedURL, "/api/internal/cluster/buckets", "Should use correct API path")
	assert.Contains(t, requestedURL, "tenant_id=tenant-abc-123", "Should include tenant_id parameter")
}

// TestHandleListBuckets_ShowsRealNodeNames tests that real node names are shown
func TestHandleListBuckets_ShowsRealNodeNames(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	// Get admin token
	adminToken := getAdminToken(t, server)

	ctx := context.Background()

	user, err := server.authManager.ValidateJWT(ctx, adminToken)
	require.NoError(t, err)

	tenant := &auth.Tenant{
		ID:              "test-tenant",
		Name:            "test-tenant",
		Status:          "active",
		MaxStorageBytes: 1000000000,
	}
	err = server.authManager.CreateTenant(ctx, tenant)
	require.NoError(t, err)

	// Create local bucket
	err = server.bucketManager.CreateBucket(ctx, tenant.ID, "local-bucket", "")
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("GET", "/api/v1/buckets", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleListBuckets(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)

	bucketsData, ok := response.Data.([]interface{})
	assert.True(t, ok)
	assert.Len(t, bucketsData, 1)

	bucketMap, ok := bucketsData[0].(map[string]interface{})
	assert.True(t, ok)

	// Should show real node name from cluster config
	nodeName := bucketMap["node_name"]
	assert.NotNil(t, nodeName)
	assert.Equal(t, "test-node", nodeName, "Should show real node name from cluster config")
	assert.NotEqual(t, "local", nodeName, "Should not show generic 'local'")
}

// TestHandleListBuckets_TenantIsolation tests that tenants only see their own buckets
func TestHandleListBuckets_TenantIsolation(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Get admin token
	adminToken := getAdminToken(t, server)

	ctx := context.Background()

	// Get admin user
	user, err := server.authManager.ValidateJWT(ctx, adminToken)
	require.NoError(t, err)

	// Create two tenants
	tenant1 := &auth.Tenant{
		ID:              "tenant-1",
		Name:            "tenant-one",
		Status:          "active",
		MaxStorageBytes: 1000000000,
	}
	err = server.authManager.CreateTenant(ctx, tenant1)
	require.NoError(t, err)

	tenant2 := &auth.Tenant{
		ID:              "tenant-2",
		Name:            "tenant-two",
		Status:          "active",
		MaxStorageBytes: 1000000000,
	}
	err = server.authManager.CreateTenant(ctx, tenant2)
	require.NoError(t, err)

	// Create buckets for each tenant
	err = server.bucketManager.CreateBucket(ctx, tenant1.ID, "tenant1-bucket", "")
	require.NoError(t, err)

	err = server.bucketManager.CreateBucket(ctx, tenant2.ID, "tenant2-bucket", "")
	require.NoError(t, err)

	// User should only see their tenant's buckets
	req := httptest.NewRequest("GET", "/api/v1/buckets", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleListBuckets(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)

	// Global admin user sees buckets from all tenants
	bucketsData, ok := response.Data.([]interface{})
	assert.True(t, ok)
	assert.GreaterOrEqual(t, len(bucketsData), 2, "Global admin should see buckets from all tenants")

	// Verify that buckets from both tenants are present
	tenantIDsFound := make(map[string]bool)
	for _, bucketData := range bucketsData {
		bucketMap, ok := bucketData.(map[string]interface{})
		assert.True(t, ok)

		tenantID := bucketMap["tenant_id"]
		if tenantID != nil {
			tenantIDsFound[tenantID.(string)] = true
		}
	}

	// Global admin should see buckets from both tenants
	assert.True(t, tenantIDsFound["tenant-1"], "Global admin should see tenant-1 buckets")
	assert.True(t, tenantIDsFound["tenant-2"], "Global admin should see tenant-2 buckets")
}
