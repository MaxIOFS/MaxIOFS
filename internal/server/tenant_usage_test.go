package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A tenant synchronized from another node takes that node's configuration,
// never its usage: each node counts what it stores.
func TestTenantSyncKeepsThisNodesUsage(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	send := func(body map[string]interface{}) {
		t.Helper()
		data, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/internal/cluster/tenant-sync", strings.NewReader(string(data)))
		req = req.WithContext(context.WithValue(req.Context(), "cluster_node_id", "peer"))
		w := httptest.NewRecorder()
		server.handleReceiveTenantSync(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	now := time.Now().Unix()
	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{ID: "t-sync-usage", Name: "t-sync-usage", Status: "active",
		MaxStorageBytes: 1000, CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, server.authManager.IncrementTenantStorage(ctx, "t-sync-usage", 500))
	send(map[string]interface{}{
		"id": "t-sync-usage", "name": "t-sync-usage", "status": "active", "max_storage_bytes": 5,
		"updated_at": now - 3600,
	})
	tenant, err := server.authManager.GetTenant(ctx, "t-sync-usage")
	require.NoError(t, err)
	assert.Equal(t, int64(1000), tenant.MaxStorageBytes, "an older configuration is not taken")

	send(map[string]interface{}{
		"id": "t-sync-usage", "name": "t-sync-usage", "status": "active", "max_storage_bytes": 9000,
		"current_storage_bytes": 7, "current_buckets": 3, "updated_at": now + 3600,
	})
	tenant, err = server.authManager.GetTenant(ctx, "t-sync-usage")
	require.NoError(t, err)
	assert.Equal(t, int64(9000), tenant.MaxStorageBytes, "a newer configuration is taken")
	assert.Equal(t, int64(500), tenant.CurrentStorageBytes, "the usage stays this node's")

	send(map[string]interface{}{
		"id": "t-sync-new", "name": "t-sync-new", "status": "active",
		"current_storage_bytes": 777, "created_at": time.Now().Unix(), "updated_at": time.Now().Unix(),
	})
	tenant, err = server.authManager.GetTenant(ctx, "t-sync-new")
	require.NoError(t, err)
	assert.Zero(t, tenant.CurrentStorageBytes, "a tenant new to this node stores nothing here")
}

// A tenant's bucket limit counts every bucket the tenant has, whoever created
// it; a limit of 0 is no limit. The console shows the same count.
func TestConsoleBucketLimitCountsTheTenantsBuckets(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	const tenantID = "t-bucket-limit"
	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{ID: tenantID, Name: tenantID, Status: "active", MaxBuckets: 2}))
	for _, name := range []string{"limit-a", "limit-b"} {
		require.NoError(t, server.bucketManager.CreateBucket(ctx, tenantID, name, "some-user"))
	}
	create := func(name string) int {
		req := createAuthenticatedRequest(http.MethodPost, "/api/v1/buckets", strings.NewReader(`{"name":"`+name+`"}`), tenantID, "tenant-admin", true)
		w := httptest.NewRecorder()
		server.handleCreateBucket(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusForbidden, create("limit-c"), "two buckets fill a limit of two")

	req := createAuthenticatedRequest(http.MethodGet, "/api/v1/tenants/"+tenantID, nil, "", "global-admin", true)
	req = mux.SetURLVars(req, map[string]string{"tenant": tenantID})
	w := httptest.NewRecorder()
	server.handleGetTenant(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got struct {
		Data auth.Tenant `json:"data"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, int64(2), got.Data.CurrentBuckets)

	tenant, err := server.authManager.GetTenant(ctx, tenantID)
	require.NoError(t, err)
	tenant.MaxBuckets = 0
	require.NoError(t, server.authManager.UpdateTenant(ctx, tenant))
	assert.Equal(t, http.StatusOK, create("limit-c"), "a limit of 0 is no limit")
}

// The stats pass sets each tenant's storage on this node to what its buckets
// here hold, whatever the counter had drifted to.
func TestStatsPassCorrectsTheTenantStorage(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	const tenantID = "t-drift"
	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{ID: tenantID, Name: tenantID, Status: "active"}))
	require.NoError(t, server.bucketManager.CreateBucket(ctx, tenantID, "drift", "u"))
	_, err := server.objectManager.PutObject(ctx, tenantID+"/drift", "k", strings.NewReader(strings.Repeat("x", 64)), http.Header{})
	require.NoError(t, err)
	require.NoError(t, server.authManager.IncrementTenantStorage(ctx, tenantID, 12345))

	server.reconcileBucketStats(ctx)

	tenant, err := server.authManager.GetTenant(ctx, tenantID)
	require.NoError(t, err)
	assert.Equal(t, int64(64), tenant.CurrentStorageBytes)
}

// In a cluster a tenant's buckets are counted on every node, each once however
// many nodes hold it.
func TestTenantBucketCountCoversTheCluster(t *testing.T) {
	c := newRoutingPair(t)
	ctx := context.Background()
	const tenantID = "t-cluster-count"
	// The target, outside the cluster here, sends its buckets nowhere; the
	// source sends its own to the target.
	for _, s := range []*Server{c.target, c.source} {
		require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: tenantID, Name: tenantID, Status: "active"}))
	}
	require.NoError(t, c.target.bucketManager.CreateBucket(ctx, tenantID, "on-both", "u"))
	require.NoError(t, c.source.bucketManager.CreateBucket(ctx, tenantID, "on-both", "u"))
	require.NoError(t, c.source.bucketManager.CreateBucket(ctx, tenantID, "only-here", "u"))
	require.NoError(t, c.target.bucketManager.CreateBucket(ctx, tenantID, "only-there", "u"))

	count, err := c.source.tenantBucketCount(ctx, tenantID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)

	req := createAuthenticatedRequest(http.MethodGet, "/api/v1/tenants", nil, "", "global-admin", true)
	w := httptest.NewRecorder()
	c.source.handleListTenants(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listed struct {
		Data struct {
			Data []auth.Tenant `json:"data"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&listed))
	var found bool
	for _, tenant := range listed.Data.Data {
		if tenant.ID == tenantID {
			found = true
			assert.Equal(t, int64(3), tenant.CurrentBuckets)
		}
	}
	assert.True(t, found)
}

// A tenant deletion from another node removes the tenant and its users here,
// unless the tenant changed here after the deletion.
func TestTenantDeletionFromAnotherNodeKeepsANewerTenant(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	now := time.Now().Unix()
	deleteSync := func(id string, deletedAt int64) {
		t.Helper()
		body := `{"id":"` + id + `","deleted_at":` + strconv.FormatInt(deletedAt, 10) + `}`
		req := httptest.NewRequest(http.MethodPost, "/api/internal/cluster/tenant-delete-sync", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), "cluster_node_id", "peer"))
		w := httptest.NewRecorder()
		server.handleReceiveTenantDeleteSync(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	for _, id := range []string{"t-del-newer", "t-del-older"} {
		require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{ID: id, Name: id, Status: "active", CreatedAt: now, UpdatedAt: now}))
		require.NoError(t, server.authManager.CreateUser(ctx, &auth.User{ID: id + "-user", Username: id + "-user", TenantID: id,
			Status: auth.UserStatusActive, Roles: []string{auth.RoleUser}, CreatedAt: now, UpdatedAt: now}))
	}

	deleteSync("t-del-newer", now-3600)
	_, err := server.authManager.GetTenant(ctx, "t-del-newer")
	assert.NoError(t, err, "changed after the deletion, the tenant is kept")
	_, err = server.authManager.GetUser(ctx, "t-del-newer-user")
	assert.NoError(t, err, "and so are its users")

	deleteSync("t-del-older", now+3600)
	_, err = server.authManager.GetTenant(ctx, "t-del-older")
	assert.Error(t, err, "a tenant not changed since is deleted")
	_, err = server.authManager.GetUser(ctx, "t-del-older-user")
	assert.Error(t, err)
}
