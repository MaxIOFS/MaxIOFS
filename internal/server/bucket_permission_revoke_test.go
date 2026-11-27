package server

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

// A permission on a bucket is revoked from the tenant or the user it names, on
// the bucket of the tenant the request names as the bucket's: a global
// administrator revokes one given to a tenant on a bucket of no tenant.
func TestAPermissionIsRevokedFromWhoHoldsIt(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	_, member, _, _ := userUpdateFixture(t, s)
	admin := storedUser(t, s, "admin")
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "shared", ""))
	scoped := s.authManager.(interface {
		GrantBucketAccessScoped(ctx context.Context, bucketName, bucketTenantID, userID, tenantID, permissionLevel, grantedBy string, expiresAt int64) error
	})
	require.NoError(t, scoped.GrantBucketAccessScoped(ctx, "shared", "", "", "acme", "read", admin.ID, 0))
	require.NoError(t, scoped.GrantBucketAccessScoped(ctx, "shared", "", member.ID, "", "read", admin.ID, 0))
	held := func(column, id string) bool {
		var n int
		require.NoError(t, s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM bucket_permissions WHERE bucket_name = 'shared' AND bucket_tenant_id = '' AND `+column+` = ?`, id).Scan(&n))
		return n > 0
	}
	revoke := func(user *auth.User, query string) int {
		req := mux.SetURLVars(requestAs(user, map[string]string{"method": http.MethodDelete}, nil, ""), map[string]string{"bucket": "shared"})
		req.URL.RawQuery = query
		rec := httptest.NewRecorder()
		s.handleRevokeBucketPermission(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusForbidden, revoke(member, "userId="+member.ID))
	assert.Equal(t, http.StatusBadRequest, revoke(admin, ""))
	assert.Equal(t, http.StatusNoContent, revoke(admin, "tenantId=acme"), "the tenant given it, on the bucket of no tenant")
	assert.False(t, held("tenant_id", "acme"))
	assert.True(t, held("user_id", member.ID), "the user's stays")
	assert.Equal(t, http.StatusNoContent, revoke(admin, "userId="+member.ID+"&bucketTenantId="))
	assert.False(t, held("user_id", member.ID))
}
