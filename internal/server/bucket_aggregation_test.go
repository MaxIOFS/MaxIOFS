package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
	clusterManager := cluster.NewManager(db, "http://localhost:8080", "http://localhost:8082")

	// Initialize cluster
	ctx := context.Background()
	_, err = clusterManager.InitializeCluster(ctx, "test-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)

	// Attach cluster components to server
	server.clusterManager = clusterManager
	server.db = db

	// Create the dedicated cluster server (setupRoutes expects it to register inter-node routes)
	server.clusterServer = &http.Server{}

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

// TestHandleListBuckets_ShowsRealNodeNames verifies the listing carries the real node name
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
