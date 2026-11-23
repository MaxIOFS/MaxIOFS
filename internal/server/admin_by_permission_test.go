package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callAs answers handler for the user id with vars and body, as a console
// session of it does.
func callAs(t *testing.T, s *Server, id string, handler http.HandlerFunc, vars map[string]string, body string) int {
	t.Helper()
	user, err := s.authManager.GetUser(context.Background(), id)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req = mux.SetURLVars(req.WithContext(context.WithValue(req.Context(), "user", user)), vars)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec.Code
}

// administrators is a deployment with a tenant acme administered in each way
// the server accepts, a member of it, a tenant other with its administrator,
// and global administrators by the role and by the permission.
func administrators(t *testing.T) *Server {
	t.Helper()
	auditDir := t.TempDir() // removed after the server, which closes the audit log, stops
	s := newClusterTestNode(t)
	auditStore, err := audit.NewSQLiteStore(filepath.Join(auditDir, "audit.db"), logrus.StandardLogger())
	require.NoError(t, err)
	s.auditManager = audit.NewManager(auditStore, logrus.StandardLogger())
	ctx := context.Background()
	for _, id := range []string{"acme", "other"} {
		require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: id, Name: id, Status: "active"}))
	}
	store := s.authManager.(interface {
		SetUserPermissions(string, *auth.UserPermissions) error
	})
	add := func(id, tenantID string, roles []string, permissions ...string) {
		now := time.Now().Unix()
		require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: id, Username: id, Password: "Secret123!",
			Email: id + "@example.com", TenantID: tenantID, Status: auth.UserStatusActive, Roles: roles,
			CreatedAt: now, UpdatedAt: now}))
		if len(permissions) > 0 {
			require.NoError(t, store.SetUserPermissions(id, &auth.UserPermissions{Global: permissions, Buckets: []auth.BucketGrant{}}))
		}
	}
	add("acme-by-admin-role", "acme", []string{auth.RoleAdmin})
	add("acme-by-tenant-admin-role", "acme", []string{auth.RoleTenantAdmin})
	add("acme-by-permission", "acme", []string{auth.RoleUser}, auth.ActionTenantAdmin, auth.ActionConsoleAccess)
	add("acme-member", "acme", []string{auth.RoleUser})
	add("other-admin", "other", []string{auth.RoleTenantAdmin})
	add("global-by-role", "", []string{auth.RoleAdmin})
	add("global-by-permission", "", []string{auth.RoleUser}, auth.ActionSuperAdmin, auth.ActionConsoleAccess)
	return s
}

var acmeAdministrators = []string{"acme-by-admin-role", "acme-by-tenant-admin-role", "acme-by-permission"}

// Whoever administers a tenant, by a role or by the permission, reaches what
// an administrator of a tenant reaches: the audit logs, the integrity status
// of its buckets, recounting a bucket. A member of the tenant does not.
func TestATenantIsAdministeredAlikeByRoleOrPermission(t *testing.T) {
	s := administrators(t)
	require.NoError(t, s.bucketManager.CreateBucket(context.Background(), "acme", "data", "acme-member"))
	bucket := map[string]string{"bucket": "data"}

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		vars    map[string]string
	}{
		{"audit logs", s.handleListAuditLogs, nil},
		{"an audit log", s.handleGetAuditLog, map[string]string{"id": "1"}},
		{"integrity status", s.handleGetIntegrityStatus, bucket},
		{"recount a bucket", s.handleRecalculateBucketStats, bucket},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, id := range acmeAdministrators {
				assert.NotEqual(t, http.StatusForbidden, callAs(t, s, id, tc.handler, tc.vars, ""), id)
			}
			assert.Equal(t, http.StatusForbidden, callAs(t, s, "acme-member", tc.handler, tc.vars, ""), "a member")
		})
	}
}

// With two-factor authentication required of administrators, an
// administrator without it does not sign in, whatever makes it one.
func TestAdministratorsWithoutTwoFactorDoNotSignInWhenItIsRequired(t *testing.T) {
	s := administrators(t)
	router := mux.NewRouter()
	s.setupConsoleAPIRoutes(router)
	require.NoError(t, s.settingsManager.Set("security.require_2fa_admin", "true"))

	for _, id := range append([]string{"global-by-role", "global-by-permission"}, acmeAdministrators...) {
		assert.Equal(t, http.StatusForbidden, signInFrom(t, router, "198.51.100.40", id, "Secret123!"), id)
	}
	assert.Equal(t, http.StatusOK, signInFrom(t, router, "198.51.100.40", "acme-member", "Secret123!"), "a member")
}

// A user sets its own preferences; another user's only who administers it:
// the administrator of a tenant its tenant's users, never a global
// administrator's or another tenant's.
func TestPreferencesAreSetByTheUserOrWhoAdministersIt(t *testing.T) {
	s := administrators(t)
	prefs := `{"themePreference":"dark","languagePreference":"en"}`
	of := func(id string) map[string]string { return map[string]string{"user": id} }

	assert.Equal(t, http.StatusOK, callAs(t, s, "acme-member", s.handleUpdateUserPreferences, of("acme-member"), prefs), "its own")
	assert.Equal(t, http.StatusForbidden, callAs(t, s, "acme-member", s.handleUpdateUserPreferences, of("acme-by-admin-role"), prefs), "a member, another's")
	assert.Equal(t, http.StatusOK, callAs(t, s, "acme-by-tenant-admin-role", s.handleUpdateUserPreferences, of("acme-member"), prefs), "its tenant's user")
	assert.Equal(t, http.StatusForbidden, callAs(t, s, "acme-by-admin-role", s.handleUpdateUserPreferences, of("global-by-role"), prefs), "a global administrator's")
	assert.Equal(t, http.StatusForbidden, callAs(t, s, "acme-by-admin-role", s.handleUpdateUserPreferences, of("other-admin"), prefs), "another tenant's")
	assert.Equal(t, http.StatusOK, callAs(t, s, "global-by-permission", s.handleUpdateUserPreferences, of("acme-member"), prefs), "a global administrator")
}

// An alert about the servers goes to the global administrators only: it names
// buckets and objects of every tenant. One about a tenant goes to them and to
// that tenant's administrators, whatever makes them so.
func TestAlertsGoToTheAdministratorsOfWhatTheyConcern(t *testing.T) {
	s := administrators(t)
	ctx := context.Background()

	servers, err := s.adminEmails(ctx, "")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"admin@maxiofs.local", "global-by-role@example.com", "global-by-permission@example.com"}, servers)

	acme, err := s.adminEmails(ctx, "acme")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"admin@maxiofs.local", "global-by-role@example.com", "global-by-permission@example.com",
		"acme-by-admin-role@example.com", "acme-by-tenant-admin-role@example.com", "acme-by-permission@example.com"}, acme)
}

// A client of the notification stream is sent, on connecting, the state of
// the tenants it sees: a tenant's administrator never another tenant's
// buckets.
func TestTheNotificationStreamSendsATenantOnlyItsOwnState(t *testing.T) {
	s := administrators(t)
	if s.quotaAlerts == nil {
		s.quotaAlerts = newQuotaAlertTracker()
	}
	if s.bucketQuotaAlerts == nil {
		s.bucketQuotaAlerts = newBucketQuotaAlertTracker()
	}
	s.quotaAlerts.levels.Store("acme", alertLevelNone)
	s.quotaAlerts.levels.Store("other", alertLevelNone)
	s.bucketQuotaAlerts.levels.Store(bucketAlertKey("acme", "acme-bucket"), alertLevelNone)
	s.bucketQuotaAlerts.levels.Store(bucketAlertKey("other", "other-bucket"), alertLevelNone)

	user, err := s.authManager.GetUser(context.Background(), "acme-by-permission")
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	s.syncAlertStateToClient(rec, rec, user, s.isGlobalAdmin(user))
	assert.Contains(t, rec.Body.String(), "acme-bucket")
	assert.NotContains(t, rec.Body.String(), "other-bucket")
	assert.NotContains(t, rec.Body.String(), `"tenantId":"other"`)

	global, err := s.authManager.GetUser(context.Background(), "global-by-permission")
	require.NoError(t, err)
	rec = httptest.NewRecorder()
	s.syncAlertStateToClient(rec, rec, global, s.isGlobalAdmin(global))
	assert.Contains(t, rec.Body.String(), "other-bucket")
}

// The list of users says who administers, as the server decides it: by the
// admin or tenant-admin role or by the permission, and who does not.
func TestTheUserListSaysWhoAdministers(t *testing.T) {
	s := administrators(t)
	user, err := s.authManager.GetUser(context.Background(), "global-by-role")
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", user))
	rec := httptest.NewRecorder()
	s.handleListUsers(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var listed struct{ Data []UserResponse }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))

	flags := map[string][2]bool{}
	for _, u := range listed.Data {
		flags[u.Username] = [2]bool{u.IsAdmin, u.IsGlobalAdmin}
	}
	for name, want := range map[string][2]bool{
		"acme-by-admin-role":        {true, false},
		"acme-by-tenant-admin-role": {true, false},
		"acme-by-permission":        {true, false},
		"acme-member":               {false, false},
		"other-admin":               {true, false},
		"global-by-role":            {true, true},
		"global-by-permission":      {true, true},
	} {
		assert.Equal(t, want, flags[name], name)
	}
}

// A locked account is unlocked by whoever administers it, by a role or by the
// permission; not by a member, another tenant's administrator or a tenant's
// administrator for a global user.
func TestAnAccountIsUnlockedByWhoAdministersIt(t *testing.T) {
	s := administrators(t)
	ctx := context.Background()
	maxFailed, err := s.settingsManager.GetInt("security.max_failed_attempts")
	require.NoError(t, err)
	lock := func(id string) {
		for range maxFailed {
			require.NoError(t, s.authManager.RecordFailedLogin(ctx, id, "203.0.113.9"))
		}
		locked, _, err := s.authManager.IsAccountLocked(ctx, id)
		require.NoError(t, err)
		require.True(t, locked, id+" locked")
	}
	unlock := func(by, id string, want int) {
		t.Helper()
		lock(id)
		assert.Equal(t, want, callAs(t, s, by, s.handleUnlockAccount, map[string]string{"user": id}, ""), by+" unlocking "+id)
		locked, _, err := s.authManager.IsAccountLocked(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, want != http.StatusOK, locked, id+" locked after "+by)
		require.NoError(t, s.authManager.UnlockAccount(ctx, "global-by-role", id))
	}

	for _, id := range append([]string{"global-by-role", "global-by-permission"}, acmeAdministrators...) {
		unlock(id, "acme-member", http.StatusOK)
	}
	unlock("acme-member", "acme-member", http.StatusForbidden)
	unlock("other-admin", "acme-member", http.StatusForbidden)
	unlock("acme-by-tenant-admin-role", "global-by-role", http.StatusForbidden)
}

// The deployment always keeps an active global administrator by the admin
// role: permissions can be taken from anybody, through a user, a group, a role
// or a policy, and the role from nobody. One by the permission is removed while
// one by the role remains, and taking its permission leaves the deployment
// administered.
func TestAGlobalAdministratorByTheRoleIsAlwaysKept(t *testing.T) {
	s := administrators(t)
	ctx := context.Background()
	users, err := s.authManager.ListUsers(ctx)
	require.NoError(t, err)
	builtIn := ""
	for _, u := range users {
		if u.Username == "admin" {
			builtIn = u.ID
		}
	}
	require.NotEmpty(t, builtIn)
	of := func(id string) map[string]string { return map[string]string{"user": id} }
	inactive, active := `{"status":"inactive"}`, `{"status":"active"}`

	require.Equal(t, http.StatusOK, callAs(t, s, "global-by-role", s.handleUpdateUser, of(builtIn), inactive), "the built-in one deactivated")
	assert.Equal(t, http.StatusOK, callAs(t, s, "global-by-role", s.handleUpdateUser, of("global-by-permission"), inactive), "one by the permission, one by the role left")
	require.Equal(t, http.StatusOK, callAs(t, s, "global-by-role", s.handleUpdateUser, of("global-by-permission"), active))

	// global-by-role is the last active one by the role.
	assert.Equal(t, http.StatusConflict, callAs(t, s, "global-by-permission", s.handleDeleteUser, of("global-by-role"), ""), "not deleted")
	assert.Equal(t, http.StatusConflict, callAs(t, s, "global-by-permission", s.handleUpdateUser, of("global-by-role"), inactive), "nor deactivated")
	assert.Equal(t, http.StatusConflict, callAs(t, s, "global-by-permission", s.handleUpdateUser, of("global-by-role"), `{"roles":["user"]}`), "nor left without the role")

	permissions := func(id, body string) int {
		user, err := s.authManager.GetUser(ctx, id)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPut, "/users/"+id+"/permissions", strings.NewReader(body))
		req = mux.SetURLVars(req.WithContext(context.WithValue(req.Context(), "user", user)), map[string]string{"id": id})
		rec := httptest.NewRecorder()
		s.handleSetUserPermissions(rec, req)
		return rec.Code
	}
	require.Equal(t, http.StatusOK, permissions("global-by-permission", `{"global":["console:Access"],"buckets":[]}`), "its own SuperAdmin taken")
	administered := 0
	for _, u := range must(s.authManager.ListUsers(ctx)) {
		if u.Status == auth.UserStatusActive && s.isGlobalAdmin(&u) {
			administered++
		}
	}
	assert.Equal(t, 1, administered, "the one by the role is left")

	require.Equal(t, http.StatusOK, callAs(t, s, "global-by-role", s.handleUpdateUser, of(builtIn), active))
	assert.Equal(t, http.StatusConflict, callAs(t, s, "global-by-role", s.handleDeleteUser, of(builtIn), ""), "the built-in one is deactivated, never deleted")
	assert.Equal(t, http.StatusNoContent, callAs(t, s, "global-by-role", s.handleDeleteUser, of("global-by-permission"), ""), "one by the permission, while one by the role is left")
	assert.Equal(t, http.StatusNoContent, callAs(t, s, builtIn, s.handleDeleteUser, of("global-by-role"), ""), "one of two by the role")
	assert.Equal(t, http.StatusConflict, callAs(t, s, builtIn, s.handleUpdateUser, of(builtIn), inactive), "the last one by the role")
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// A locked user's own record says until when it is locked, as the list does.
func TestAUsersRecordSaysItIsLocked(t *testing.T) {
	s := administrators(t)
	ctx := context.Background()
	maxFailed, err := s.settingsManager.GetInt("security.max_failed_attempts")
	require.NoError(t, err)
	for range maxFailed {
		require.NoError(t, s.authManager.RecordFailedLogin(ctx, "acme-member", "203.0.113.9"))
	}
	admin, err := s.authManager.GetUser(ctx, "acme-by-admin-role")
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/users/acme-member", nil)
	req = mux.SetURLVars(req.WithContext(context.WithValue(req.Context(), "user", admin)), map[string]string{"user": "acme-member"})
	rec := httptest.NewRecorder()
	s.handleGetUser(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct{ Data UserResponse }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Greater(t, got.Data.LockedUntil, time.Now().Unix(), "locked until a time to come")
}

// A user of no tenant allowed everything by a role of its own, without the
// admin role or the SuperAdmin permission, is no global administrator anywhere:
// it lists no other user, sees no tenant's buckets and reads no tenant's
// objects, in the console or through S3.
func TestAWildcardMakesNoGlobalAdministrator(t *testing.T) {
	s := administrators(t)
	ctx := context.Background()
	im := s.authManager.(auth.IAMManager)
	_, err := im.CreateIAMRole(ctx, "operators", "/", "", "", 0, "")
	require.NoError(t, err)
	require.NoError(t, im.AttachIAMPolicy(ctx, auth.PolicyFullAccess, auth.IAMTargetRole, "operators"))
	now := time.Now().Unix()
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "global-by-wildcard", Username: "global-by-wildcard",
		Password: "Secret123!", Status: auth.UserStatusActive, Roles: []string{"operators"}, CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "acme-data", "acme-member"))

	user, err := s.authManager.GetUser(ctx, "global-by-wildcard")
	require.NoError(t, err)
	set, err := s.authManager.(interface {
		ResolvePolicySet(context.Context, *auth.User) (*auth.PolicySet, error)
	}).ResolvePolicySet(ctx, user)
	require.NoError(t, err)
	as := func(method, target string, vars map[string]string) *http.Request {
		req := httptest.NewRequest(method, target, nil)
		reqCtx := auth.WithPolicySet(context.WithValue(req.Context(), "user", user), set)
		return mux.SetURLVars(req.WithContext(reqCtx), vars)
	}

	assert.False(t, s.isGlobalAdmin(user), "the console's own answer")
	assert.False(t, auth.IsAdminUser(as(http.MethodGet, "/", nil).Context()), "every other console check")

	rec := httptest.NewRecorder()
	s.handleListUsers(rec, as(http.MethodGet, "/users", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var listed struct{ Data []UserResponse }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed.Data, 1, "only itself")
	assert.Equal(t, "global-by-wildcard", listed.Data[0].Username)

	assert.False(t, s.userCanPerformConsoleS3Action(as(http.MethodGet, "/", nil), user, "acme", auth.ActionGetObject, consoleObjectARN("acme-data", "k")),
		"a tenant's object in the console")
	assert.False(t, set.Allows(auth.AccessRequest{Action: auth.ActionGetObject, Resource: "arn:aws:s3:::acme-data/k", Owner: auth.OwnedBy("acme")}),
		"a tenant's object through S3")
	assert.True(t, set.Allows(auth.AccessRequest{Action: auth.ActionPutObject, Resource: "arn:aws:s3:::global-data/k", Owner: auth.OwnedBy("")}),
		"everything in the global namespace")
}

// A secret set through the settings, the metrics token or the SMTP password,
// is not kept by the audit log, set alone or with others.
func TestTheAuditLogKeepsNoSettingSecret(t *testing.T) {
	s := administrators(t)
	ctx := context.Background()
	require.Equal(t, http.StatusOK, callAs(t, s, "global-by-role", s.handleUpdateSetting,
		map[string]string{"key": "metrics.token"}, `{"value":"token-alone-SECRET"}`))
	require.Equal(t, http.StatusOK, callAs(t, s, "global-by-role", s.handleBulkUpdateSettings, nil,
		`{"settings":{"metrics.token":"token-bulk-SECRET","email.smtp_password":"smtp-SECRET"}}`))

	// The audit log is written in the background.
	var audited []string
	require.Eventually(t, func() bool {
		logs, _, err := s.auditManager.GetLogs(ctx, &audit.AuditLogFilters{Page: 1, PageSize: 100})
		require.NoError(t, err)
		audited = audited[:0]
		for _, entry := range logs {
			details, err := json.Marshal(entry.Details)
			require.NoError(t, err)
			if strings.Contains(string(details), "metrics.token") {
				audited = append(audited, string(details))
			}
		}
		return len(audited) == 2
	}, 10*time.Second, 50*time.Millisecond, "both changes are audited")
	for _, details := range audited {
		assert.NotContains(t, details, "SECRET")
	}
}

// usersUnreadable is an auth manager whose list of users cannot be read.
type usersUnreadable struct{ auth.Manager }

func (usersUnreadable) ListUsers(context.Context) ([]auth.User, error) {
	return nil, errors.New("database is locked")
}

// A delete of a global administrator by the IAM API that cannot count the
// others fails as the failure it is, not as the last administrator.
func TestIAMDeleteUserThatCannotCountTheAdministratorsSaysSo(t *testing.T) {
	s := administrators(t)
	ctx := context.Background()
	caller, err := s.authManager.GetUser(ctx, "global-by-role")
	require.NoError(t, err)
	im := s.authManager.(auth.IAMManager)
	s.authManager = usersUnreadable{s.authManager}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.PostForm = url.Values{"UserName": {caller.Username}}
	rec := httptest.NewRecorder()
	s.iamDeleteUser(rec, req, im, caller)
	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Equal(t, "ServiceFailure", iamErrorCodeOf(t, rec.Body.String()))
}

// A directory that did not give the groups holds back the sign-in of a user
// they decide, or one to be made from them; anyone else signs in.
func TestASignInWithoutItsGroupsWaitsOnlyForThoseTheyDecide(t *testing.T) {
	managed := &auth.User{ID: "m"}
	markGroupManaged(managed)
	unknown := &idp.ExternalUser{Email: "ana@empresa.com", GroupsUnknown: true}
	known := &idp.ExternalUser{Email: "ana@empresa.com"}

	assert.True(t, waitsForGroups(nil, unknown), "a user to be made from the groups")
	assert.True(t, waitsForGroups(managed, unknown), "a user the groups decide")
	assert.False(t, waitsForGroups(&auth.User{ID: "linked"}, unknown), "a user the groups do not decide")
	assert.False(t, waitsForGroups(nil, known))
	assert.False(t, waitsForGroups(managed, known))
}

// A setting set alone takes what a bulk update takes: an empty text, and no
// empty number.
func TestASettingSetAloneTakesAnEmptyText(t *testing.T) {
	s := administrators(t)
	put := func(key, body string) int {
		return callAs(t, s, "global-by-role", s.handleUpdateSetting, map[string]string{"key": key}, body)
	}
	require.Equal(t, http.StatusOK, put("metrics.token", `{"value":"a-token"}`))
	require.Equal(t, http.StatusOK, put("metrics.token", `{"value":""}`), "no token: /metrics answers 401")
	value, err := s.settingsManager.Get("metrics.token")
	require.NoError(t, err)
	assert.Empty(t, value)
	assert.Equal(t, http.StatusBadRequest, put("security.ratelimit_api_per_second", `{"value":""}`), "an empty number")
}
