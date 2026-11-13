package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requestAs is a console request of user as the console's middleware leaves it.
func requestAs(user *auth.User, method, vars map[string]string, body string) *http.Request {
	req := httptest.NewRequest(method["method"], "/", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), "user", user))
	return mux.SetURLVars(req, vars)
}

// A password change ends the console sessions of its account, the one that
// changed it among them: whoever held one signs in with the new password. The
// account's access keys, and the sessions of anybody else, stay.
func TestAPasswordChangeEndsTheSessionsOfItsAccount(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	const password = "First-pass-12345!"

	newUser := func(name string, roles ...string) *auth.User {
		user := &auth.User{ID: name, Username: name, Password: password, Status: auth.UserStatusActive,
			Roles: roles, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
		require.NoError(t, s.authManager.CreateUser(ctx, user))
		stored, err := s.authManager.GetUser(ctx, name)
		require.NoError(t, err)
		return stored
	}
	signIn := func(user *auth.User) *auth.TokenPair {
		pair, err := s.authManager.StartSession(ctx, user, &auth.ConsoleSession{Method: auth.SessionMethodPassword})
		require.NoError(t, err)
		_, err = s.authManager.ValidateJWT(ctx, pair.AccessToken)
		require.NoError(t, err)
		return pair
	}
	changePassword := func(caller *auth.User, target, current, next string) int {
		req := requestAs(caller, map[string]string{"method": http.MethodPut}, map[string]string{"user": target},
			fmt.Sprintf(`{"currentPassword":%q,"newPassword":%q}`, current, next))
		rec := httptest.NewRecorder()
		s.handleChangePassword(rec, req)
		return rec.Code
	}

	ana := newUser("ana", auth.RoleUser)
	boss := newUser("boss", auth.RoleAdmin)
	key, err := s.authManager.GenerateAccessKey(ctx, ana.ID)
	require.NoError(t, err)
	here, elsewhere, bossSession := signIn(ana), signIn(ana), signIn(boss)

	require.Equal(t, http.StatusOK, changePassword(ana, ana.ID, password, "Second-pass-12345!"))
	for name, pair := range map[string]*auth.TokenPair{"the session that changed it": here, "another session": elsewhere} {
		_, err := s.authManager.ValidateJWT(ctx, pair.AccessToken)
		assert.ErrorIs(t, err, auth.ErrSessionEnded, name)
		_, _, err = s.authManager.RefreshSession(ctx, pair.RefreshToken)
		assert.Error(t, err, "%s is not renewed", name)
	}
	_, err = s.authManager.ValidateJWT(ctx, bossSession.AccessToken)
	assert.NoError(t, err, "somebody else's session stays")
	_, err = s.authManager.ValidateCredentials(ctx, key.AccessKeyID, key.SecretAccessKey)
	assert.NoError(t, err, "an access key is not a session")

	again := signIn(ana)
	require.Equal(t, http.StatusOK, changePassword(boss, ana.ID, "", "Third-pass-12345!"), "an administrator sets it")
	_, err = s.authManager.ValidateJWT(ctx, again.AccessToken)
	assert.ErrorIs(t, err, auth.ErrSessionEnded, "the account's session ends")
	_, err = s.authManager.ValidateJWT(ctx, bossSession.AccessToken)
	assert.NoError(t, err, "the administrator's stays")
}

// A tenant is administered by one of its users: the tenant administrator role
// or permission on a user of no tenant administers nothing, the global
// administrator's account least of all.
func TestATenantAdministratorOfNoTenantAdministersNothing(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", Status: "active"}))

	stray := &auth.User{ID: "stray", Username: "stray", Password: "Stray-pass-12345!", Status: auth.UserStatusActive,
		Roles: []string{auth.RoleTenantAdmin}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	member := &auth.User{ID: "member", Username: "member", Password: "Member-pass-12345!", Status: auth.UserStatusActive,
		TenantID: "acme", Roles: []string{auth.RoleUser}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	require.NoError(t, s.authManager.CreateUser(ctx, stray))
	require.NoError(t, s.authManager.CreateUser(ctx, member))
	admin, err := s.authManager.GetUserByUsername(ctx, "admin")
	require.NoError(t, err)

	assert.False(t, s.isAdmin(stray))
	req := requestAs(stray, map[string]string{"method": http.MethodPut}, map[string]string{"user": admin.ID},
		`{"newPassword":"Taken-over-12345!"}`)
	rec := httptest.NewRecorder()
	s.handleChangePassword(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "the global administrator's password is not theirs to set")

	setPermissions := func(target, action string) int {
		req := requestAs(admin, map[string]string{"method": http.MethodPut}, map[string]string{"id": target},
			fmt.Sprintf(`{"global":[%q],"buckets":[]}`, action))
		rec := httptest.NewRecorder()
		s.handleSetUserPermissions(rec, req)
		return rec.Code
	}
	assert.Equal(t, http.StatusBadRequest, setPermissions("stray", auth.ActionTenantAdmin), "tenant administrator, on a user of no tenant")
	assert.Equal(t, http.StatusBadRequest, setPermissions("member", auth.ActionSuperAdmin), "super administrator, on a user of a tenant")
	assert.Equal(t, http.StatusOK, setPermissions("member", auth.ActionTenantAdmin), "tenant administrator, on a user of a tenant")
}
