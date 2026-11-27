package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userUpdateFixture is tenants acme and beta, acme's administrator and a
// user of each, and a user of acme signing in through an identity provider.
func userUpdateFixture(t *testing.T, s *Server) (tenantAdmin, member, betaUser, idpUser *auth.User) {
	t.Helper()
	ctx := context.Background()
	for _, id := range []string{"acme", "beta"} {
		require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: id, Name: id, Status: "active"}))
	}
	tenantAdmin = consoleUserOf(t, s, "acme-admin", "acme", auth.RoleTenantAdmin)
	member = consoleUserOf(t, s, "acme-user", "acme", auth.RoleUser)
	betaUser = consoleUserOf(t, s, "beta-user", "beta", auth.RoleUser)
	now := time.Now().Unix()
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "acme-sso", Username: "sso@acme.test", TenantID: "acme",
		Email: "sso@acme.test", Status: auth.UserStatusActive, Roles: []string{auth.RoleUser}, AuthProvider: "oauth:corp",
		ExternalID: "sso@acme.test", CreatedAt: now, UpdatedAt: now}))
	idpUser, err := s.authManager.GetUser(ctx, "acme-sso")
	require.NoError(t, err)
	return tenantAdmin, member, betaUser, idpUser
}

func updateUser(s *Server, caller *auth.User, id, body string) *httptest.ResponseRecorder {
	return consoleAs(caller, s.handleUpdateUser, http.MethodPut, map[string]string{"user": id}, body)
}

func storedUser(t *testing.T, s *Server, id string) *auth.User {
	t.Helper()
	user, err := s.authManager.GetUser(context.Background(), id)
	require.NoError(t, err)
	return user
}

// A user changes its own email and nothing else of itself; who administers a
// user changes the rest, within its tenant; only a global administrator moves
// a user to another tenant or makes an administrator.
func TestAUserIsUpdatedByItselfOrWhoAdministersIt(t *testing.T) {
	s := newClusterTestNode(t)
	tenantAdmin, member, betaUser, idpUser := userUpdateFixture(t, s)
	admin := storedUser(t, s, "admin")

	updated := consoleData[UserResponse](t, updateUser(s, member, member.ID, `{"email":"me@acme.test"}`), http.StatusOK)
	assert.Equal(t, "me@acme.test", updated.Email)
	assert.Equal(t, "me@acme.test", storedUser(t, s, member.ID).Email)
	for _, tc := range []struct {
		caller *auth.User
		id     string
		body   string
		status int
	}{
		{nil, member.ID, `{"email":"x@acme.test"}`, http.StatusForbidden},
		{member, betaUser.ID, `{"email":"x@acme.test"}`, http.StatusForbidden},
		{member, member.ID, `{"roles":["admin"]}`, http.StatusForbidden},
		{member, member.ID, `{"status":"inactive"}`, http.StatusForbidden},
		{member, member.ID, `{"tenantId":"beta"}`, http.StatusForbidden},
		{member, member.ID, `{"mustChangePassword":true}`, http.StatusForbidden},
		{member, member.ID, `{"managedByGroups":true}`, http.StatusForbidden},
		{admin, "ghost", `{}`, http.StatusNotFound},
		{admin, member.ID, `not json`, http.StatusBadRequest},
		{tenantAdmin, betaUser.ID, `{"status":"suspended"}`, http.StatusForbidden},
		{tenantAdmin, member.ID, `{"tenantId":"beta"}`, http.StatusForbidden},
		{tenantAdmin, member.ID, `{"roles":["admin"]}`, http.StatusForbidden},
		{admin, member.ID, `{"email":"sso@acme.test"}`, http.StatusConflict},
		{admin, member.ID, `{"managedByGroups":true}`, http.StatusBadRequest},
		{admin, member.ID, `{"status":"bogus"}`, http.StatusBadRequest},
	} {
		assert.Equal(t, tc.status, updateUser(s, tc.caller, tc.id, tc.body).Code, "%v %s %s", tc.caller != nil, tc.id, tc.body)
	}
	assert.Equal(t, auth.UserStatusActive, storedUser(t, s, member.ID).Status, "refused changes change nothing")
	assert.Equal(t, "me@acme.test", storedUser(t, s, member.ID).Email)

	assert.Equal(t, auth.UserStatusSuspended, consoleData[UserResponse](t,
		updateUser(s, tenantAdmin, member.ID, `{"status":"suspended"}`), http.StatusOK).Status)
	assert.Equal(t, auth.UserStatusSuspended, storedUser(t, s, member.ID).Status)
	assert.Equal(t, []string{auth.RoleReadOnly}, consoleData[UserResponse](t,
		updateUser(s, tenantAdmin, member.ID, `{"roles":["`+auth.RoleReadOnly+`"]}`), http.StatusOK).Roles)

	assert.True(t, consoleData[UserResponse](t, updateUser(s, tenantAdmin, member.ID, `{"mustChangePassword":true}`), http.StatusOK).MustChangePassword)
	assert.Equal(t, http.StatusBadRequest, updateUser(s, admin, member.ID, `{"mustChangePassword":false}`).Code,
		"only a new password lifts it")
	assert.True(t, storedUser(t, s, member.ID).MustChangePassword)

	assert.True(t, consoleData[UserResponse](t, updateUser(s, tenantAdmin, idpUser.ID, `{"managedByGroups":true}`), http.StatusOK).ManagedByGroups)
	assert.True(t, groupManaged(storedUser(t, s, idpUser.ID)))
	assert.False(t, consoleData[UserResponse](t, updateUser(s, tenantAdmin, idpUser.ID, `{"managedByGroups":false}`), http.StatusOK).ManagedByGroups)
	assert.False(t, groupManaged(storedUser(t, s, idpUser.ID)))

	moved := consoleData[UserResponse](t, updateUser(s, admin, member.ID, `{"tenantId":"beta"}`), http.StatusOK)
	assert.Equal(t, "beta", moved.TenantID)
	assert.Equal(t, "beta", storedUser(t, s, member.ID).TenantID)
	madeAdmin := consoleData[UserResponse](t, updateUser(s, admin, betaUser.ID, `{"roles":["`+auth.RoleTenantAdmin+`"]}`), http.StatusOK)
	assert.Equal(t, []string{auth.RoleTenantAdmin}, madeAdmin.Roles)
}

// The deployment keeps an active global administrator by the admin role, and
// a tenant keeps an active administrator: neither the last of them is
// demoted, deactivated or moved, by itself or anybody else, as neither is
// deleted.
func TestTheLastAdministratorsAreKept(t *testing.T) {
	s := newClusterTestNode(t)
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	admin := storedUser(t, s, "admin")
	for _, tc := range []struct {
		caller *auth.User
		id     string
		body   string
	}{
		{admin, admin.ID, `{"roles":["` + auth.RoleUser + `"]}`},
		{admin, admin.ID, `{"status":"inactive"}`},
		{admin, admin.ID, `{"tenantId":"beta"}`},
		{admin, tenantAdmin.ID, `{"roles":["` + auth.RoleUser + `"]}`},
		{admin, tenantAdmin.ID, `{"status":"suspended"}`},
		{admin, tenantAdmin.ID, `{"tenantId":"beta"}`},
		{tenantAdmin, tenantAdmin.ID, `{"roles":["` + auth.RoleUser + `"]}`},
		{tenantAdmin, tenantAdmin.ID, `{"status":"inactive"}`},
	} {
		assert.Equal(t, http.StatusConflict, updateUser(s, tc.caller, tc.id, tc.body).Code, "%s %s", tc.id, tc.body)
	}
	assert.Equal(t, []string{auth.RoleTenantAdmin}, storedUser(t, s, tenantAdmin.ID).Roles)
	assert.Equal(t, auth.UserStatusActive, storedUser(t, s, tenantAdmin.ID).Status)
	assert.Equal(t, auth.UserStatusActive, storedUser(t, s, admin.ID).Status)

	consoleUserOf(t, s, "admin-two", "", auth.RoleAdmin)
	consoleUserOf(t, s, "acme-admin-two", "acme", auth.RoleTenantAdmin)
	assert.Equal(t, http.StatusOK, updateUser(s, admin, admin.ID, `{"status":"inactive"}`).Code, "another is left")
	assert.Equal(t, http.StatusOK, updateUser(s, tenantAdmin, tenantAdmin.ID, `{"roles":["`+auth.RoleUser+`"]}`).Code, "another is left")
}
