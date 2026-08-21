package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRouteOrdering_ClusterNotCapturedByS3 verifies that cluster endpoints on the
func TestRouteOrdering_ClusterNotCapturedByS3(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	err := server.setupRoutes()
	require.NoError(t, err)

	// Cluster routes are on the dedicated cluster server, not the S3 server.
	ts := httptest.NewServer(server.clusterServer.Handler)
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

	// Cluster routes live on the dedicated cluster server.
	ts := httptest.NewServer(server.clusterServer.Handler)
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

	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Test S3 ListBuckets endpoint
	resp, err := noRedirectClient.Get(ts.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	// S3 endpoints should respond (will require auth or redirect to console, but endpoint exists)
	// Should NOT be 404 Not Found
	assert.NotEqual(t, http.StatusNotFound, resp.StatusCode,
		"S3 ListBuckets endpoint should be registered")

	// Test S3 bucket endpoint
	resp2, err := noRedirectClient.Get(ts.URL + "/test-bucket")
	require.NoError(t, err)
	defer resp2.Body.Close()

	assert.NotEqual(t, http.StatusNotFound, resp2.StatusCode,
		"S3 bucket endpoint should be registered")
}

func TestRouteOrdering_ConsoleObjectSubresourcesBeforeGenericObject(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	router := mux.NewRouter()
	server.setupConsoleAPIRoutes(router)

	tests := []string{
		"/buckets/test-bucket/objects/dir/file.txt/acl",
		"/buckets/test-bucket/objects/dir/file.txt/legal-hold",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			match := &mux.RouteMatch{}
			require.True(t, router.Match(req, match), "object subresource route should match")

			vars := match.Vars
			assert.Equal(t, "test-bucket", vars["bucket"])
			assert.Equal(t, "dir/file.txt", vars["object"])
		})
	}
}

// TestRouteOrdering_BugReproduction tests the scenario that caused the original bug.
// With the dedicated cluster port this can no longer happen, but we verify the
// cluster server still responds correctly to unauthenticated requests.
func TestRouteOrdering_BugReproduction(t *testing.T) {
	server, _, cleanup := setupServerWithCluster(t)
	defer cleanup()

	err := server.setupRoutes()
	require.NoError(t, err)

	// Cluster routes now live on a dedicated port; test against the cluster server.
	ts := httptest.NewServer(server.clusterServer.Handler)
	defer ts.Close()

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

	// Cluster routes live on the dedicated cluster server.
	ts := httptest.NewServer(server.clusterServer.Handler)
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

	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Test that S3 ListBuckets endpoint works
	resp, err := noRedirectClient.Get(ts.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should NOT be 404 - S3 handler is registered and responding
	assert.NotEqual(t, http.StatusNotFound, resp.StatusCode,
		"S3 ListBuckets endpoint should be registered when cluster is disabled")

	// Test that S3 bucket endpoint works
	resp2, err := noRedirectClient.Get(ts.URL + "/test-bucket")
	require.NoError(t, err)
	defer resp2.Body.Close()

	assert.NotEqual(t, http.StatusNotFound, resp2.StatusCode,
		"S3 bucket endpoint should be registered when cluster is disabled")
}
