package s3compat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTenantIsNotAPermission_GetObject is the read path, and the one that
// mattered most: it returns object data.
func TestTenantIsNotAPermission_GetObject(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	// A bucket belonging to one tenant.
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "not-mine", "another-user"))

	// Somebody from a different tenant.
	outsider := &auth.User{ID: "outsider", TenantID: "other-tenant", Roles: []string{"user"}}

	req := httptest.NewRequest(http.MethodGet, "/not-mine/secret.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "not-mine", "object": "secret.txt"})
	req = req.WithContext(setUserInContext(req.Context(), outsider))
	req.Header.Set("X-Tenant-ID", env.tenantID)

	w := httptest.NewRecorder()
	env.handler.GetObject(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code,
		"a bucket in another tenant is not readable, whatever the request looks like")
}

// TestTenantIsNotAPermission_AnotherTenantIsRefused is the boundary that must
func TestTenantIsNotAPermission_AnotherTenantIsRefused(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "private-bucket", "owner-user"))

	stranger := &auth.User{ID: "stranger", TenantID: "some-other-tenant", Roles: []string{"user"}}

	req := httptest.NewRequest(http.MethodHead, "/private-bucket", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "private-bucket"})
	req = req.WithContext(setUserInContext(req.Context(), stranger))
	req.Header.Set("X-Tenant-ID", env.tenantID)

	w := httptest.NewRecorder()
	env.handler.HeadBucket(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code,
		"another tenant's bucket is out of reach")
}

func TestTenantIsNotAPermission_SameTenantMemberIsRefused(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "same-tenant-private", env.userID))

	peer := &auth.User{ID: "peer-user", TenantID: env.tenantID, Roles: []string{"user"}}

	req := httptest.NewRequest(http.MethodHead, "/same-tenant-private", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "same-tenant-private"})
	req = req.WithContext(setUserInContext(req.Context(), peer))
	req.Header.Set("X-Tenant-ID", env.tenantID)

	w := httptest.NewRecorder()
	env.handler.HeadBucket(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code,
		"a tenant peer must not inherit owner permissions from the bucket creator")
}

func TestTenantScopedAdminStaysInsideTenant(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	const otherTenantID = "tenant-admin-other"
	require.NoError(t, env.authManager.CreateTenant(ctx, &auth.Tenant{
		ID:              otherTenantID,
		Name:            otherTenantID,
		DisplayName:     "Other Tenant",
		Status:          "active",
		MaxStorageBytes: 1 << 30,
		MaxBuckets:      10,
		MaxAccessKeys:   10,
	}))

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "tenant-admin-home", "home-owner"))
	require.NoError(t, env.bucketManager.CreateBucket(ctx, otherTenantID, "tenant-admin-away", "away-owner"))

	tenantAdmin := &auth.User{ID: "tenant-admin-user", TenantID: env.tenantID, Roles: []string{auth.RoleAdmin}}

	req := httptest.NewRequest(http.MethodHead, "/tenant-admin-home", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "tenant-admin-home"})
	req = req.WithContext(setUserInContext(req.Context(), tenantAdmin))
	req.Header.Set("X-Tenant-ID", env.tenantID)
	w := httptest.NewRecorder()
	env.handler.HeadBucket(w, req)
	assert.Equal(t, http.StatusOK, w.Code,
		"a tenant-scoped admin has broad access inside their own tenant")

	req = httptest.NewRequest(http.MethodHead, "/tenant-admin-away", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "tenant-admin-away"})
	req = req.WithContext(setUserInContext(req.Context(), tenantAdmin))
	req.Header.Set("X-Tenant-ID", otherTenantID)
	w = httptest.NewRecorder()
	env.handler.HeadBucket(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code,
		"a tenant-scoped admin must not use wildcard policy against another tenant")
}

// TestTenantIsNotAPermission_TheOwnerStillWorks is the other half: tightening
// the check must not lock out the person the bucket belongs to.
func TestTenantIsNotAPermission_TheOwnerStillWorks(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "mine", env.userID))

	owner := &auth.User{ID: env.userID, TenantID: env.tenantID, Roles: []string{"user"}}

	req := httptest.NewRequest(http.MethodHead, "/mine", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "mine"})
	req = req.WithContext(setUserInContext(req.Context(), owner))
	req.Header.Set("X-Tenant-ID", env.tenantID)

	w := httptest.NewRecorder()
	env.handler.HeadBucket(w, req)

	assert.Equal(t, http.StatusOK, w.Code,
		"the bucket's owner reaches their own bucket")
}
