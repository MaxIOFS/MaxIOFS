package s3compat

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTenantBoundary_TenantlessUserCannotReachATenantBucket is the direction
func TestTenantBoundary_TenantlessUserCannotReachATenantBucket(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "shared-name", env.userID))

	// No tenant, and holding the owner policy for a bucket of the same name.
	global := &auth.User{ID: env.userID, TenantID: "", Roles: []string{"user"}}

	assert.False(t,
		env.handler.userCanPerformS3ActionInTenant(ctx, global, env.tenantID,
			auth.ActionGetObject, objectARN("shared-name", "x.txt")),
		"a tenant-less caller must not reach a bucket that belongs to a tenant")
}

// TestTenantBoundary_HoldsInBothDirections keeps the original direction pinned
// too: a caller inside one tenant cannot reach another's bucket.
func TestTenantBoundary_HoldsInBothDirections(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	member := &auth.User{ID: env.userID, TenantID: "tenant-alpha", Roles: []string{"user"}}

	assert.False(t,
		env.handler.userCanPerformS3ActionInTenant(ctx, member, "tenant-beta",
			auth.ActionGetObject, objectARN("shared-name", "x.txt")),
		"a member of one tenant must not reach another tenant's bucket")

	assert.False(t,
		env.handler.userCanPerformS3ActionInTenant(ctx, member, "",
			auth.ActionGetObject, objectARN("shared-name", "x.txt")),
		"nor a bucket outside every tenant")
}

// TestTenantBoundary_SuperAdminCrossesReadOnly: global administrators can audit
// tenant data, but they do not own or mutate tenant buckets.
func TestTenantBoundary_SuperAdminCrossesReadOnly(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "admin-reach", env.userID))

	// Granted outright rather than through the admin role: what lets a caller
	// cross is the permission, and that is what this pins.
	writer, ok := env.authManager.(interface {
		PutIAMInlinePolicy(ctx context.Context, targetType, targetID, name, document string) error
	})
	require.True(t, ok, "the harness must be able to grant a permission")
	require.NoError(t, writer.PutIAMInlinePolicy(ctx, auth.IAMTargetUser, env.userID, "super",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow",`+
			`"Action":["`+auth.ActionSuperAdmin+`","`+auth.ActionGetObject+`"],"Resource":["*"]}]}`))

	super := &auth.User{ID: env.userID, TenantID: "", Roles: []string{auth.RoleAdmin}}

	assert.True(t,
		env.handler.userCanPerformS3ActionInTenant(ctx, super, env.tenantID,
			auth.ActionGetObject, objectARN("admin-reach", "x.txt")),
		"a super administrator crosses the boundary for read/audit")
	assert.False(t,
		env.handler.userCanPerformS3ActionInTenant(ctx, super, env.tenantID,
			auth.ActionDeleteObject, objectARN("admin-reach", "x.txt")),
		"a super administrator must not mutate tenant data")
	assert.False(t,
		env.handler.userCanPerformS3ActionInTenant(ctx, super, env.tenantID,
			auth.ActionPutBucketPolicy, bucketARN("admin-reach")),
		"a super administrator must not change tenant bucket configuration")
}

func TestSubresourceAuthorization_SuperAdminCannotPutBucketPolicy(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "subresource-admin", env.userID))

	super := &auth.User{ID: env.userID, TenantID: "", Roles: []string{auth.RoleAdmin}}
	body := strings.NewReader(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::subresource-admin/*"]}]}`)
	req := httptest.NewRequest(http.MethodPut, "/subresource-admin?policy", body)
	req = mux.SetURLVars(req, map[string]string{"bucket": "subresource-admin"})
	req = req.WithContext(setUserInContext(req.Context(), super))
	w := httptest.NewRecorder()

	env.handler.PutBucketPolicy(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "AccessDenied")
}

func TestSubresourceAuthorization_TenantAdminCanPutBucketPolicy(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "tenant-admin-policy", env.userID))

	tenantAdmin := &auth.User{ID: "tenant-admin", TenantID: env.tenantID, Roles: []string{auth.RoleTenantAdmin}}
	body := strings.NewReader(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::tenant-admin-policy/*"]}]}`)
	req := httptest.NewRequest(http.MethodPut, "/tenant-admin-policy?policy", body)
	req = mux.SetURLVars(req, map[string]string{"bucket": "tenant-admin-policy"})
	req = req.WithContext(setUserInContext(req.Context(), tenantAdmin))
	w := httptest.NewRecorder()

	env.handler.PutBucketPolicy(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestSubresourceAuthorization_ObjectTaggingIsResourceScoped(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "tag-target", env.userID))
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "tag-other", env.userID))

	peer := &auth.User{
		ID:        "same-tenant-peer",
		Username:  "peer",
		Email:     "peer@example.com",
		Status:    "active",
		TenantID:  env.tenantID,
		Roles:     []string{"user"},
		CreatedAt: 1,
		UpdatedAt: 1,
	}
	require.NoError(t, env.authManager.CreateUser(ctx, peer))

	writer, ok := env.authManager.(interface {
		PutIAMInlinePolicy(ctx context.Context, targetType, targetID, name, document string) error
	})
	require.True(t, ok)
	require.NoError(t, writer.PutIAMInlinePolicy(ctx, auth.IAMTargetUser, peer.ID, "tag-other-only",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow",`+
			`"Action":["`+auth.ActionPutObjectTagging+`"],`+
			`"Resource":["`+objectARN("tag-other", "*")+`"]}]}`))

	reqBody := strings.NewReader(`<Tagging><TagSet><Tag><Key>env</Key><Value>dev</Value></Tag></TagSet></Tagging>`)
	req := httptest.NewRequest(http.MethodPut, "/tag-target/private.txt?tagging", reqBody)
	req = mux.SetURLVars(req, map[string]string{"bucket": "tag-target", "object": "private.txt"})
	req = req.WithContext(setUserInContext(req.Context(), peer))
	w := httptest.NewRecorder()

	env.handler.PutObjectTagging(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "AccessDenied")
}

func TestSubresourceAuthorization_BatchDeleteIsCheckedPerObject(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	const bucketName = "batch-guard"
	const objectKey = "keep.txt"
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))

	ownerReq := httptest.NewRequest(http.MethodGet, "/"+bucketName, nil)
	ownerReq = mux.SetURLVars(ownerReq, map[string]string{"bucket": bucketName})
	ownerReq = ownerReq.WithContext(setUserInContext(ownerReq.Context(), &auth.User{
		ID:       env.userID,
		TenantID: env.tenantID,
		Roles:    []string{"user"},
	}))
	bucketPath := env.handler.getBucketPath(ownerReq, bucketName)
	_, err := env.objectManager.PutObject(ctx, bucketPath, objectKey, bytes.NewReader([]byte("keep")), http.Header{})
	require.NoError(t, err)

	peer := &auth.User{ID: "batch-peer", TenantID: env.tenantID, Roles: []string{"user"}}
	body := strings.NewReader(`<Delete><Object><Key>` + objectKey + `</Key></Object></Delete>`)
	req := httptest.NewRequest(http.MethodPost, "/batch-guard?delete", body)
	req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})
	req = req.WithContext(setUserInContext(req.Context(), peer))
	w := httptest.NewRecorder()

	env.handler.DeleteObjects(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result DeleteObjectsResult
	require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "AccessDenied", result.Errors[0].Code)

	_, err = env.objectManager.GetObjectMetadata(ctx, bucketPath, objectKey)
	assert.NoError(t, err, "batch delete must not delete unauthorized objects")
}

// TestTenantBoundary_SuperAdminWritesInTheGlobalNamespace is the other side of
func TestTenantBoundary_SuperAdminWritesInTheGlobalNamespace(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, "", "global-space", env.userID))

	writer, ok := env.authManager.(interface {
		PutIAMInlinePolicy(ctx context.Context, targetType, targetID, name, document string) error
	})
	require.True(t, ok)
	require.NoError(t, writer.PutIAMInlinePolicy(ctx, auth.IAMTargetUser, env.userID, "super",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["*"],"Resource":["*"]}]}`))

	super := &auth.User{ID: env.userID, TenantID: "", Roles: []string{auth.RoleAdmin}}

	assert.True(t,
		env.handler.userCanPerformS3ActionInTenant(ctx, super, "",
			auth.ActionPutObject, objectARN("global-space", "backup.bin")),
		"an administrator must be able to write in their own namespace")
	assert.True(t,
		env.handler.userCanPerformS3ActionInTenant(ctx, super, "",
			auth.ActionDeleteObject, objectARN("global-space", "backup.bin")))

	// And crossing into a tenant is still read-only.
	assert.False(t,
		env.handler.userCanPerformS3ActionInTenant(ctx, super, "someone-elses-tenant",
			auth.ActionPutObject, objectARN("their-bucket", "x")),
		"crossing into a tenant stays read-only")
}
