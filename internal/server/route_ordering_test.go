package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRouteOrdering_ClusterNotCapturedByS3 verifies that cluster endpoints are NOT captured by S3 routes
// This is the CRITICAL test that ensures the bug doesn't return
func TestRouteOrdering_ClusterNotCapturedByS3(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	// Start the actual HTTP server to test routing
	err := server.setupRoutes()
	require.NoError(t, err)

	// Create test server
	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()

	// Test cluster endpoints WITHOUT authentication
	clusterEndpoints := []string{
		"/api/internal/cluster/buckets",
		"/api/internal/cluster/tenant/test-tenant/storage",
	}

	for _, endpoint := range clusterEndpoints {
		t.Run(endpoint, func(t *testing.T) {
			resp, err := http.Get(ts.URL + endpoint)
			require.NoError(t, err)
			defer resp.Body.Close()

			// CRITICAL ASSERTION: Should return 401 Unauthorized, NOT 403 Forbidden
			// If S3 routes captured this endpoint, we would get 403 with "Access denied. Object is not shared"
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
				"Cluster endpoint should return 401 (missing auth), not 403 (S3 captured it)")

			// Read body to verify it's NOT an S3 error
			buf := make([]byte, 1024)
			n, _ := resp.Body.Read(buf)
			body := string(buf[:n])

			// Should NOT contain S3 error messages
			assert.NotContains(t, body, "Access denied",
				"Should not show S3 'Access denied' error")
			assert.NotContains(t, body, "not shared",
				"Should not show S3 'not shared' error")
			assert.NotContains(t, body, "NoSuchBucket",
				"Should not show S3 'NoSuchBucket' error")

			// Should contain authentication-related error
			assert.Contains(t, body, "authentication",
				"Should show cluster authentication error")
		})
	}
}

// TestRouteOrdering_ClusterWithValidAuth verifies cluster endpoints work with proper authentication
func TestRouteOrdering_ClusterWithValidAuth(t *testing.T) {
	server, clusterMgr, cleanup := setupServerWithCluster(t)
	defer cleanup()

	ctx := context.Background()

	// Get cluster config for authentication
	config, err := clusterMgr.GetConfig(ctx)
	require.NoError(t, err)

	// Start server
	err = server.setupRoutes()
	require.NoError(t, err)

	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()

	// Create authenticated request
	proxyClient := cluster.NewProxyClient(nil)
	req, err := proxyClient.CreateAuthenticatedRequest(
		ctx,
		"GET",
		ts.URL+"/api/internal/cluster/buckets?tenant_id=test",
		nil,
		config.NodeID,
		config.ClusterToken,
	)
	require.NoError(t, err)

	// Execute request
	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should succeed with proper authentication
	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"Cluster endpoint should work with proper HMAC authentication")

	// Should NOT get S3 error
	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	assert.NotContains(t, body, "Access denied",
		"Should not show S3 error with valid cluster auth")
}

// TestRouteOrdering_S3EndpointsStillWork verifies S3 routes still function correctly
func TestRouteOrdering_S3EndpointsStillWork(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	err := server.setupRoutes()
	require.NoError(t, err)

	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()

	// Test S3 ListBuckets endpoint
	resp, err := http.Get(ts.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	// S3 endpoints should respond (will require auth, but endpoint exists)
	// Should NOT be 404 Not Found
	assert.NotEqual(t, http.StatusNotFound, resp.StatusCode,
		"S3 ListBuckets endpoint should be registered")

	// Test S3 bucket endpoint
	resp2, err := http.Get(ts.URL + "/test-bucket")
	require.NoError(t, err)
	defer resp2.Body.Close()

	assert.NotEqual(t, http.StatusNotFound, resp2.StatusCode,
		"S3 bucket endpoint should be registered")
}

// TestRouteOrdering_BugReproduction tests the EXACT scenario that caused the original bug
func TestRouteOrdering_BugReproduction(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	err := server.setupRoutes()
	require.NoError(t, err)

	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()

	// Reproduce the EXACT request that was failing:
	// GET /api/internal/cluster/buckets
	//
	// With WRONG route ordering, S3 handler would capture this as:
	// - Bucket name: "api"
	// - Object key: "internal/cluster/buckets"
	// - Result: 403 "Access denied. Object is not shared."
	//
	// With CORRECT route ordering, cluster handler captures it:
	// - Result: 401 "Missing authentication headers"

	resp, err := http.Get(ts.URL + "/api/internal/cluster/buckets")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read response body
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	// CRITICAL ASSERTIONS - These would FAIL if routes are in wrong order

	// 1. Should NOT be 403 Forbidden (S3 error)
	assert.NotEqual(t, http.StatusForbidden, resp.StatusCode,
		"BUG DETECTED: S3 handler captured cluster endpoint! Check route ordering in server.go setupRoutes()")

	// 2. Should be 401 Unauthorized (cluster auth error)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"Expected 401 from cluster auth middleware")

	// 3. Response should NOT contain S3 error messages
	assert.NotContains(t, body, "Access denied. Object is not shared",
		"BUG DETECTED: Got S3 error message! Routes are in wrong order!")

	assert.NotContains(t, body, "api", // bucket name that S3 would use
		"BUG DETECTED: Response mentions 'api' as bucket - S3 captured the request!")

	// 4. Response SHOULD contain cluster authentication error
	assert.Contains(t, body, "authentication",
		"Should get cluster authentication error, not S3 error")
}

// TestRouteOrdering_MultipleClusterEndpoints tests various cluster endpoints
func TestRouteOrdering_MultipleClusterEndpoints(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	err := server.setupRoutes()
	require.NoError(t, err)

	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()

	// Test multiple cluster endpoints
	endpoints := []struct {
		path        string
		method      string
		description string
	}{
		{"/api/internal/cluster/buckets", "GET", "Bucket aggregation endpoint"},
		{"/api/internal/cluster/tenant/abc123/storage", "GET", "Storage quota endpoint"},
		{"/api/internal/cluster/access-key-sync", "POST", "Access key sync endpoint"},
		{"/api/internal/cluster/user-sync", "POST", "User sync endpoint"},
	}

	for _, ep := range endpoints {
		t.Run(ep.description, func(t *testing.T) {
			var resp *http.Response
			var err error

			if ep.method == "POST" {
				resp, err = http.Post(ts.URL+ep.path, "application/json", nil)
			} else {
				resp, err = http.Get(ts.URL + ep.path)
			}
			require.NoError(t, err)
			defer resp.Body.Close()

			// All cluster endpoints should return 401 (not 403)
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
				"%s should return 401 (cluster auth), not 403 (S3 error)", ep.description)

			// Verify no S3 error in response
			buf := make([]byte, 1024)
			n, _ := resp.Body.Read(buf)
			body := string(buf[:n])

			assert.NotContains(t, body, "Access denied",
				"%s should not show S3 error", ep.description)
		})
	}
}

// TestRouteOrdering_WithoutCluster tests route ordering when cluster is disabled
func TestRouteOrdering_WithoutCluster(t *testing.T) {
	// Setup server WITHOUT cluster
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	err := server.setupRoutes()
	require.NoError(t, err)

	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()

	// When cluster is disabled, /api/internal/cluster/* paths will be captured by S3 handler
	// This is expected behavior - cluster routes aren't registered, so S3 (with PathPrefix("/")) gets them
	// The key test is that S3 endpoints continue to work properly

	// Test that S3 ListBuckets endpoint works
	resp, err := http.Get(ts.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should NOT be 404 - S3 handler is registered and responding
	assert.NotEqual(t, http.StatusNotFound, resp.StatusCode,
		"S3 ListBuckets endpoint should be registered when cluster is disabled")

	// Test that S3 bucket endpoint works
	resp2, err := http.Get(ts.URL + "/test-bucket")
	require.NoError(t, err)
	defer resp2.Body.Close()

	assert.NotEqual(t, http.StatusNotFound, resp2.StatusCode,
		"S3 bucket endpoint should be registered when cluster is disabled")
}
