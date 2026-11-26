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

const (
	roleTrust       = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam:::user/alice"},"Action":"sts:AssumeRole"}]}`
	roleTrustBob    = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam:::user/bob"},"Action":"sts:AssumeRole"}]}`
	rolePermissions = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::backups/*"}]}`
	roleWrites      = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:PutObject","Resource":"arn:aws:s3:::backups/*"}]}`
)

// consoleAs is handler's answer to a console request of user.
func consoleAs(user *auth.User, handler http.HandlerFunc, method string, vars map[string]string, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler(rec, requestAs(user, map[string]string{"method": method}, vars, body))
	return rec
}

// answer decodes a console answer that says it succeeded.
func answer[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Success bool `json:"success"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.True(t, body.Success)
	var decoded T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &decoded))
	return decoded
}

func saveRole(s *Server, user *auth.User, body string) int {
	rec := httptest.NewRecorder()
	s.handleCreateIAMRole(rec, requestAs(user, map[string]string{"method": http.MethodPost}, nil, body))
	return rec.Code
}

// A role is created by a global administrator only, with the trust policy,
// permissions and managed policies it is given; saved again, its trust policy
// and permissions are those of the last save.
func TestARoleIsSavedByAGlobalAdministrator(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	im := s.authManager.(auth.IAMManager)
	admin, err := s.authManager.GetUser(ctx, "admin")
	require.NoError(t, err)
	plain := consoleUser(t, s, "role-plain", true)
	body := `{"name":"backup","description":"Backups","trustPolicy":` + roleTrust + `,"permissions":` + rolePermissions +
		`,"maxSessionDuration":7200,"policies":["ReadOnlyAccess"]}`

	assert.Equal(t, http.StatusForbidden, saveRole(s, nil, body))
	assert.Equal(t, http.StatusForbidden, saveRole(s, plain, body))
	_, err = im.GetIAMRole(ctx, "backup")
	require.ErrorIs(t, err, auth.ErrIAMNoSuchEntity, "refused requests create nothing")

	require.Equal(t, http.StatusOK, saveRole(s, admin, body))
	role, err := im.GetIAMRole(ctx, "backup")
	require.NoError(t, err)
	assert.JSONEq(t, roleTrust, role.AssumeRolePolicy)
	assert.Equal(t, 7200, role.MaxSessionDuration)
	assert.Equal(t, "Backups", role.Description)
	inline, err := im.GetIAMInlinePolicy(ctx, auth.IAMTargetRole, "backup", "permissions")
	require.NoError(t, err)
	assert.JSONEq(t, rolePermissions, inline.Document)
	attached, err := im.ListAttachedIAMPolicies(ctx, auth.IAMTargetRole, "backup")
	require.NoError(t, err)
	require.Len(t, attached, 1)
	assert.Equal(t, "ReadOnlyAccess", attached[0].Name)

	require.Equal(t, http.StatusOK, saveRole(s, admin, `{"name":"backup","trustPolicy":`+quoted(t, roleTrustBob)+`,"permissions":`+roleWrites+`}`))
	role, err = im.GetIAMRole(ctx, "backup")
	require.NoError(t, err)
	assert.JSONEq(t, roleTrustBob, role.AssumeRolePolicy)
	inline, err = im.GetIAMInlinePolicy(ctx, auth.IAMTargetRole, "backup", "permissions")
	require.NoError(t, err)
	assert.JSONEq(t, roleWrites, inline.Document)

	require.Equal(t, http.StatusOK, saveRole(s, admin, `{"name":"backup"}`))
	role, err = im.GetIAMRole(ctx, "backup")
	require.NoError(t, err)
	assert.JSONEq(t, roleTrustBob, role.AssumeRolePolicy, "a save without a trust policy keeps the one it has")
}

// A role whose request, name, permissions or policies are not valid is refused.
func TestARoleThatCannotBeSavedIsRefused(t *testing.T) {
	s := newClusterTestNode(t)
	admin, err := s.authManager.GetUser(context.Background(), "admin")
	require.NoError(t, err)
	for body, status := range map[string]int{
		`not json`:                                  http.StatusBadRequest,
		`{"name":"bad name!"}`:                      http.StatusBadRequest,
		`{"name":"r1","permissions":{}}`:            http.StatusBadRequest,
		`{"name":"r2","policies":["NoSuchPolicy"]}`: http.StatusNotFound,
	} {
		assert.Equal(t, status, saveRole(s, admin, body), body)
	}
}

// A role lists the managed policies attached to it and its own permissions;
// it is deleted by a global administrator only.
func TestARoleIsListedAndDeletedByAGlobalAdministrator(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	admin, err := s.authManager.GetUser(ctx, "admin")
	require.NoError(t, err)
	plain := consoleUser(t, s, "role-plain", true)
	require.Equal(t, http.StatusOK, saveRole(s, admin, `{"name":"backup","permissions":`+rolePermissions+`,"policies":["ReadOnlyAccess"]}`))

	listed := answer[struct {
		Roles []iamRoleView `json:"roles"`
	}](t, consoleAs(admin, s.handleListIAMRoles, http.MethodGet, nil, ""))
	var backup *iamRoleView
	for i := range listed.Roles {
		if listed.Roles[i].Name == "backup" {
			backup = &listed.Roles[i]
		}
	}
	require.NotNil(t, backup)
	assert.ElementsMatch(t, []string{"ReadOnlyAccess", "permissions (inline)"}, backup.Policies)

	remove := func(user *auth.User, name string) *httptest.ResponseRecorder {
		return consoleAs(user, s.handleDeleteIAMRole, http.MethodDelete, map[string]string{"name": name}, "")
	}
	assert.Equal(t, http.StatusForbidden, remove(nil, "backup").Code)
	assert.Equal(t, http.StatusForbidden, remove(plain, "backup").Code)
	assert.Equal(t, http.StatusNotFound, remove(admin, "no-such-role").Code)
	answer[struct{}](t, remove(admin, "backup"))
	_, err = s.authManager.(auth.IAMManager).GetIAMRole(ctx, "backup")
	assert.ErrorIs(t, err, auth.ErrIAMNoSuchEntity)
}

// A managed policy is listed, created, given a new version and deleted by a
// global administrator only; one still attached is not deleted.
func TestAManagedPolicyIsKeptByAGlobalAdministrator(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	im := s.authManager.(auth.IAMManager)
	admin, err := s.authManager.GetUser(ctx, "admin")
	require.NoError(t, err)
	plain := consoleUser(t, s, "policy-plain", true)
	create := `{"name":"backups-read","description":"Read backups","document":` + rolePermissions + `}`
	save := func(user *auth.User, body string) *httptest.ResponseRecorder {
		return consoleAs(user, s.handleCreateIAMPolicy, http.MethodPost, nil, body)
	}
	remove := func(user *auth.User, name string) *httptest.ResponseRecorder {
		return consoleAs(user, s.handleDeleteIAMPolicy, http.MethodDelete, map[string]string{"name": name}, "")
	}

	for _, user := range []*auth.User{nil, plain} {
		assert.Equal(t, http.StatusForbidden, save(user, create).Code)
		assert.Equal(t, http.StatusForbidden, consoleAs(user, s.handleListIAMPolicies, http.MethodGet, nil, "").Code)
		assert.Equal(t, http.StatusForbidden, remove(user, "ReadOnlyAccess").Code)
	}
	_, err = im.GetIAMPolicy(ctx, "backups-read")
	require.ErrorIs(t, err, auth.ErrIAMNoSuchEntity, "refused requests create nothing")

	answer[struct{}](t, save(admin, create))
	policy, err := im.GetIAMPolicy(ctx, "backups-read")
	require.NoError(t, err)
	assert.JSONEq(t, rolePermissions, policy.Document)
	assert.Equal(t, "Read backups", policy.Description)

	answer[struct{}](t, save(admin, `{"name":"backups-read","document":`+quoted(t, roleWrites)+`}`))
	versions, err := im.ListIAMPolicyVersions(ctx, "backups-read")
	require.NoError(t, err)
	assert.Len(t, versions, 2, "saved again, a policy keeps its former version")
	listed := answer[struct {
		Policies []iamPolicyView `json:"policies"`
	}](t, consoleAs(admin, s.handleListIAMPolicies, http.MethodGet, nil, ""))
	var found *iamPolicyView
	for i := range listed.Policies {
		if listed.Policies[i].Name == "backups-read" {
			found = &listed.Policies[i]
		}
	}
	require.NotNil(t, found)
	assert.JSONEq(t, roleWrites, found.Document)

	for body, status := range map[string]int{
		`not json`:                       http.StatusBadRequest,
		`{"name":"no-document"}`:         http.StatusBadRequest,
		`{"name":"bad","document":"{}"}`: http.StatusBadRequest,
	} {
		assert.Equal(t, status, save(admin, body).Code, body)
	}

	require.NoError(t, im.AttachIAMPolicy(ctx, "backups-read", auth.IAMTargetUser, plain.ID))
	assert.Equal(t, http.StatusConflict, remove(admin, "backups-read").Code)
	require.NoError(t, im.DetachIAMPolicy(ctx, "backups-read", auth.IAMTargetUser, plain.ID))
	answer[struct{}](t, remove(admin, "backups-read"))
	_, err = im.GetIAMPolicy(ctx, "backups-read")
	assert.ErrorIs(t, err, auth.ErrIAMNoSuchEntity)
	assert.Equal(t, http.StatusNotFound, remove(admin, "backups-read").Code)
}

// The permissions a console offers are listed with what each one is.
func TestThePermissionCatalogIsListed(t *testing.T) {
	s := newClusterTestNode(t)
	admin, err := s.authManager.GetUser(context.Background(), "admin")
	require.NoError(t, err)
	catalog := answer[struct {
		Groups []auth.PermissionGroup `json:"groups"`
	}](t, consoleAs(admin, s.handleListPermissionCatalog, http.MethodGet, nil, ""))
	var actions []string
	for _, group := range catalog.Groups {
		for _, permission := range group.Permissions {
			actions = append(actions, permission.Action)
		}
	}
	assert.Contains(t, actions, auth.ActionManageOwnKeys)
}

// A user's permissions are read and set by who administers it, and an
// administration permission by a global administrator, on the kind of user
// it is for. A user that does not exist has none to read or set.
func TestAUsersPermissionsAreSetByWhoAdministersIt(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	now := time.Now().Unix()
	for _, id := range []string{"acme", "other"} {
		require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: id, Name: id, Status: "active"}))
	}
	user := func(id, tenantID, role string) *auth.User {
		require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: id, Username: id, TenantID: tenantID,
			Status: auth.UserStatusActive, Roles: []string{role}, CreatedAt: now, UpdatedAt: now}))
		stored, err := s.authManager.GetUser(ctx, id)
		require.NoError(t, err)
		return stored
	}
	admin, err := s.authManager.GetUser(ctx, "admin")
	require.NoError(t, err)
	tenantAdmin := user("acme-admin", "acme", auth.RoleTenantAdmin)
	member := user("acme-user", "acme", auth.RoleUser)
	user("other-user", "other", auth.RoleUser)
	user("loner", "", auth.RoleUser)
	get := func(caller *auth.User, id string) *httptest.ResponseRecorder {
		return consoleAs(caller, s.handleGetUserPermissions, http.MethodGet, map[string]string{"id": id}, "")
	}
	set := func(caller *auth.User, id, body string) *httptest.ResponseRecorder {
		return consoleAs(caller, s.handleSetUserPermissions, http.MethodPut, map[string]string{"id": id}, body)
	}
	type held struct {
		Permissions auth.UserPermissions `json:"permissions"`
	}
	manageKeys := `{"global":["` + auth.ActionManageOwnKeys + `"]}`

	assert.Equal(t, http.StatusForbidden, get(nil, "acme-user").Code)
	assert.Equal(t, http.StatusForbidden, get(member, "acme-user").Code)
	assert.Equal(t, http.StatusForbidden, get(tenantAdmin, "other-user").Code)
	assert.Equal(t, http.StatusForbidden, set(tenantAdmin, "other-user", manageKeys).Code)
	assert.Equal(t, http.StatusNotFound, get(tenantAdmin, "ghost").Code)
	assert.Equal(t, http.StatusNotFound, get(admin, "ghost").Code)
	assert.Equal(t, http.StatusNotFound, set(admin, "ghost", manageKeys).Code)

	answer[struct{}](t, set(tenantAdmin, "acme-user", manageKeys))
	selected := answer[held](t, get(tenantAdmin, "acme-user")).Permissions.Global
	assert.Contains(t, selected, auth.ActionManageOwnKeys)

	for _, tc := range []struct {
		caller *auth.User
		id     string
		body   string
		status int
	}{
		{tenantAdmin, "acme-user", `not json`, http.StatusBadRequest},
		{tenantAdmin, "acme-user", `{"global":["` + auth.ActionTenantAdmin + `"]}`, http.StatusForbidden},
		{tenantAdmin, "acme-user", `{"buckets":[{"bucket":"b","actions":["` + auth.ActionSuperAdmin + `"]}]}`, http.StatusForbidden},
		{admin, "loner", `{"global":["` + auth.ActionTenantAdmin + `"]}`, http.StatusBadRequest},
		{admin, "acme-user", `{"buckets":[{"bucket":"b","actions":["` + auth.ActionSuperAdmin + `"]}]}`, http.StatusBadRequest},
	} {
		assert.Equal(t, tc.status, set(tc.caller, tc.id, tc.body).Code, "%s %s", tc.id, tc.body)
	}
	assert.Equal(t, selected, answer[held](t, get(admin, "acme-user")).Permissions.Global,
		"a refused selection changes nothing")
	answer[struct{}](t, set(admin, "acme-user", `{"global":["`+auth.ActionTenantAdmin+`"]}`))
}
