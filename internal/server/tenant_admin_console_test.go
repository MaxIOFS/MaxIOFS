package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asUser answers handler for the user id, as a console session of it does.
func asUser(t *testing.T, s *Server, id string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	user, err := s.authManager.GetUser(context.Background(), id)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", user))
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// Whoever the server takes for a tenant's administrator, by the admin or
// tenant-admin role or by the TenantAdmin permission, is told so by /auth/me:
// the console offers it the Users page by that, not by the role's name.
func TestTheConsoleIsToldWhoAdministersATenant(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", Status: "active"}))
	withRoles := func(id, tenantID string, roles ...string) {
		now := time.Now().Unix()
		require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: id, Username: id, TenantID: tenantID,
			Status: auth.UserStatusActive, Roles: roles, CreatedAt: now, UpdatedAt: now}))
	}
	withRoles("by-tenant-admin-role", "acme", auth.RoleTenantAdmin)
	withRoles("by-admin-role", "acme", auth.RoleAdmin)
	userHolding(t, s, "by-permission", "acme", auth.ActionTenantAdmin, auth.ActionConsoleAccess)
	withRoles("member", "acme", auth.RoleUser)
	withRoles("global-admin", "", auth.RoleAdmin)
	withRoles("tenant-admin-of-nothing", "", auth.RoleTenantAdmin)

	for _, tc := range []struct {
		id                     string
		isAdmin, isGlobalAdmin bool
	}{
		{"by-tenant-admin-role", true, false},
		{"by-admin-role", true, false},
		{"by-permission", true, false},
		{"member", false, false},
		{"global-admin", true, true},
		{"tenant-admin-of-nothing", false, false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			rec := asUser(t, s, tc.id, s.handleGetCurrentUser)
			require.Equal(t, http.StatusOK, rec.Code)
			var me struct{ Data UserResponse }
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &me))
			assert.Equal(t, tc.isAdmin, me.Data.IsAdmin, "isAdmin")
			assert.Equal(t, tc.isGlobalAdmin, me.Data.IsGlobalAdmin, "isGlobalAdmin")
		})
	}
}

// The administrator of a tenant lists the roles it may give its users, for the
// Create User form: those of its tenant and of every tenant. Not an
// administrator role, not another tenant's, not one granting what it does not
// hold. Anybody else who is not a global administrator lists none.
func TestATenantAdministratorListsTheRolesItMayGive(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	for _, id := range []string{"acme", "other"} {
		require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: id, Name: id, Status: "active"}))
	}
	im := s.authManager.(auth.IAMManager)
	role := func(name, tenantID, action string) {
		_, err := im.CreateIAMRole(ctx, name, "/", "", "", 0, tenantID)
		require.NoError(t, err)
		require.NoError(t, im.PutIAMInlinePolicy(ctx, auth.IAMTargetRole, name, "grant",
			`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["`+action+`"],"Resource":["arn:aws:s3:::*/*"]}]}`))
	}
	role("acme-reader", "acme", auth.ActionGetObject)
	role("acme-writer", "acme", auth.ActionPutObject)
	role("other-reader", "other", auth.ActionGetObject)

	userHolding(t, s, "acme-admin", "acme", auth.ActionTenantAdmin, auth.ActionConsoleAccess, auth.ActionGetObject)
	userHolding(t, s, "acme-member", "acme", auth.ActionConsoleAccess, auth.ActionGetObject)

	rec := asUser(t, s, "acme-admin", s.handleListIAMRoles)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var listed struct{ Roles []iamRoleView }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	names := map[string]bool{}
	for _, r := range listed.Roles {
		names[r.Name] = true
	}
	assert.True(t, names[auth.RoleUser], "a role of every tenant")
	assert.True(t, names["acme-reader"], "a role of its tenant within what it holds")
	assert.False(t, names[auth.RoleAdmin], "the admin role")
	assert.False(t, names[auth.RoleTenantAdmin], "the tenant-admin role")
	assert.False(t, names["other-reader"], "another tenant's role")
	assert.False(t, names["acme-writer"], "a role granting what it does not hold")

	assert.Equal(t, http.StatusForbidden, asUser(t, s, "acme-member", s.handleListIAMRoles).Code)
}
