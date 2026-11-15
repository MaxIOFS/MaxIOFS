package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userHolding is a user of tenantID holding actions everywhere in it, as an
// administrator gives them: owning a bucket gives its owner only what it holds.
func userHolding(t *testing.T, s *Server, id, tenantID string, actions ...string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: id, Username: id, TenantID: tenantID,
		Status: auth.UserStatusActive, Roles: []string{auth.RoleUser}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}))
	store := s.authManager.(interface {
		SetUserPermissions(string, *auth.UserPermissions) error
	})
	require.NoError(t, store.SetUserPermissions(id, &auth.UserPermissions{Global: actions, Buckets: []auth.BucketGrant{}}))
}

// A tenant administrator grants within what it holds, wherever it grants: a
// user's permissions, a level on a bucket, a group with more, a role with
// more, or a policy through the IAM API. What a user already had stays when
// it edits it, and taking away is always allowed.
func TestATenantAdministratorGrantsWithinWhatItHolds(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", Status: "active"}))
	store := s.authManager.(interface {
		SetUserPermissions(string, *auth.UserPermissions) error
	})
	im := s.authManager.(auth.IAMManager)
	newUser := func(id string, global ...string) *auth.User {
		require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: id, Username: id, TenantID: "acme",
			Status: auth.UserStatusActive, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}))
		require.NoError(t, store.SetUserPermissions(id, &auth.UserPermissions{Global: global, Buckets: []auth.BucketGrant{}}))
		user, err := s.authManager.GetUser(ctx, id)
		require.NoError(t, err)
		return user
	}
	admin := newUser("limited-admin", auth.ActionTenantAdmin, auth.ActionConsoleAccess, auth.ActionListBucket, auth.ActionGetObject)
	newUser("member", auth.ActionConsoleAccess)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "data", "member"))

	as := func(handler http.HandlerFunc, vars map[string]string, body string) int {
		req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		req = mux.SetURLVars(req.WithContext(context.WithValue(req.Context(), "user", admin)), vars)
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec.Code
	}
	permissions := func(global ...string) string {
		return fmt.Sprintf(`{"global":["%s"],"buckets":[]}`, strings.Join(global, `","`))
	}
	user := map[string]string{"id": "member"}

	assert.Equal(t, http.StatusOK, as(s.handleSetUserPermissions, user, permissions(auth.ActionConsoleAccess, auth.ActionGetObject)), "what it holds")
	assert.Equal(t, http.StatusForbidden, as(s.handleSetUserPermissions, user, permissions(auth.ActionConsoleAccess, auth.ActionPutObject)), "what it does not hold")
	assert.Equal(t, http.StatusForbidden, as(s.handleSetUserPermissions, user,
		`{"global":["console:Access"],"buckets":[{"bucket":"data","actions":["s3:PutBucketObjectLockConfiguration"]}]}`), "on a bucket")

	require.NoError(t, store.SetUserPermissions("member", &auth.UserPermissions{
		Global: []string{auth.ActionConsoleAccess, auth.ActionPutObject}, Buckets: []auth.BucketGrant{}}))
	assert.Equal(t, http.StatusOK, as(s.handleSetUserPermissions, user, permissions(auth.ActionConsoleAccess, auth.ActionPutObject, auth.ActionGetObject)),
		"what the user already had stays")
	assert.Equal(t, http.StatusOK, as(s.handleSetUserPermissions, user, permissions(auth.ActionConsoleAccess)), "taking away")

	assert.Equal(t, http.StatusForbidden, as(s.handleGrantBucketPermission, map[string]string{"bucket": "data"},
		`{"userId":"member","permissionLevel":"write"}`), "a level on a bucket")

	group := &auth.Group{ID: "writers", Name: "writers", TenantID: "acme"}
	require.NoError(t, s.authManager.CreateGroup(ctx, group))
	require.NoError(t, im.PutIAMInlinePolicy(ctx, auth.IAMTargetGroup, "writers", "write",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":"*"}]}`))
	assert.Equal(t, http.StatusForbidden, as(s.handleAddGroupMember, map[string]string{"group": "writers"}, `{"userId":"member"}`),
		"a group that grants more")

	_, err := im.CreateIAMRole(ctx, "uploader", "/", "", "", 0, "")
	require.NoError(t, err)
	require.NoError(t, im.PutIAMInlinePolicy(ctx, auth.IAMTargetRole, "uploader", "permissions",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":"*"}]}`))
	assert.Equal(t, http.StatusForbidden, as(s.handleUpdateUser, map[string]string{"user": "member"}, `{"roles":["uploader"]}`), "a role that grants more")

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", admin))
	req.PostForm = url.Values{"UserName": {"member"}, "PolicyName": {"more"},
		"PolicyDocument": {`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]}`}}
	rec := httptest.NewRecorder()
	s.iamPutInlinePolicy(rec, req, im, "PutUserPolicy")
	assert.Equal(t, http.StatusForbidden, rec.Code, "s3:* through the IAM API: %s", rec.Body.String())

	held, err := s.authManager.(interface {
		GetUserPermissions(string) (*auth.UserPermissions, error)
	}).GetUserPermissions("member")
	require.NoError(t, err)
	assert.NotContains(t, held.Global, auth.ActionPutObject, "nothing refused reached the user")
}

// A tenant administrator holds everything a bucket or an object takes, in its
// tenant and only there, named action by action and never as s3:*: it grants
// any of it, and a built-in policy of partial wildcards, but not s3:*.
func TestATenantAdministratorHoldsEveryBucketPermissionButNotTheWildcard(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", Status: "active"}))
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "acme-admin", Username: "acme-admin", TenantID: "acme",
		Status: auth.UserStatusActive, Roles: []string{auth.RoleAdmin}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}))
	admin, err := s.authManager.GetUser(ctx, "acme-admin")
	require.NoError(t, err)
	set, err := s.authManager.(interface {
		ResolvePolicySet(context.Context, *auth.User) (*auth.PolicySet, error)
	}).ResolvePolicySet(ctx, admin)
	require.NoError(t, err)

	for _, action := range []string{auth.ActionBypassGovernanceRetention, "s3:PutBucketReplication", "s3:PutBucketLogging",
		"s3:PutBucketOwnershipControls", auth.ActionPutBucketObjectLockConfiguration} {
		assert.True(t, set.Allows(auth.AccessRequest{Action: action, Resource: "arn:aws:s3:::b", Owner: auth.OwnedBy("acme")}),
			"%s on a bucket of its tenant", action)
		assert.False(t, set.Allows(auth.AccessRequest{Action: action, Resource: "arn:aws:s3:::b", Owner: auth.OwnedBy("")}),
			"%s on a global bucket", action)
	}
	for _, document := range set.Documents {
		assert.NotContains(t, document, `"s3:*"`)
	}

	grant := func(document string) string { return set.FirstNotHeld([]string{document}) }
	assert.Empty(t, grant(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:Get*","s3:List*","s3:Describe*"],"Resource":["*"]}]}`),
		"a built-in policy of partial wildcards")
	assert.Equal(t, "s3:*", grant(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]}`))
	assert.Equal(t, "*", grant(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"*","Resource":"*"}]}`))
}
