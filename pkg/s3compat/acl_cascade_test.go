package s3compat

// An ACL cannot overrule the policies for an authenticated caller.
//
// Every permission check used to fall through, on a policy denial, into a
// cascade: the object ACL, the bucket ACL, the AuthenticatedUsers group,
// FULL_CONTROL, then public access. Each rung could only widen the answer, so
// an explicit IAM Deny was overridden by an ACL — and because AuthenticatedUsers
// means "anyone with a credential", a bucket set to authenticated-read-write at
// any point in its life handed that access to every credential in the
// deployment, permanently and invisibly.
//
// ACLs and public access still decide ANONYMOUS requests. Those have no
// policies to consult, which is the whole reason the mechanism exists.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// grantEveryAuthenticatedUser opens a bucket to the AuthenticatedUsers group,
// which is the widest grant an ACL can express short of public.
func grantEveryAuthenticatedUser(t *testing.T, env *coverageTestEnv, bucketName string, perm acl.Permission) {
	t.Helper()
	bucketACL, err := env.bucketManager.GetBucketACL(context.Background(), env.tenantID, bucketName)
	require.NoError(t, err)

	data, ok := bucketACL.(*acl.ACL)
	require.True(t, ok)

	data.Grants = append(data.Grants, acl.Grant{
		Grantee:    acl.Grantee{Type: acl.GranteeTypeGroup, URI: acl.GroupAuthenticatedUsers},
		Permission: perm,
	})
	require.NoError(t, env.bucketManager.SetBucketACL(context.Background(), env.tenantID, bucketName, data))
}

func TestACLCascade_DoesNotOverruleAPolicyDenialOnDelete(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "aclwide", env.userID))
	grantEveryAuthenticatedUser(t, env, "aclwide", acl.PermissionWrite)

	// A member of the bucket's tenant holding no policy over it.
	stranger := &auth.User{ID: "acl-stranger", TenantID: env.tenantID, Roles: []string{"readonly"}}

	allowed := env.handler.checkDeleteObjectPermission(ctx, stranger, true,
		env.tenantID, "aclwide", env.tenantID+"/aclwide", "victim.txt", "")

	assert.False(t, allowed,
		"an AuthenticatedUsers grant must not hand delete to a caller the policies refuse")
}

func TestACLCascade_DoesNotOverruleAPolicyDenialOnHeadBucket(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "aclhead", env.userID))
	grantEveryAuthenticatedUser(t, env, "aclhead", acl.PermissionRead)

	stranger := &auth.User{ID: "acl-stranger", TenantID: env.tenantID, Roles: []string{"readonly"}}

	req := httptest.NewRequest(http.MethodHead, "/aclhead", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "aclhead"})
	req = req.WithContext(setUserInContext(req.Context(), stranger))

	w := httptest.NewRecorder()
	env.handler.HeadBucket(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code,
		"an AuthenticatedUsers grant must not hand a bucket to every credential")
}

// TestACLCascade_AGrantedUserStillReads is the other half: removing the cascade
// must not take away what the policies legitimately give.
func TestACLCascade_AGrantedUserStillReads(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "aclowner", env.userID))

	owner := &auth.User{ID: env.userID, TenantID: env.tenantID, Roles: []string{"user"}}

	req := httptest.NewRequest(http.MethodHead, "/aclowner", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "aclowner"})
	req = req.WithContext(setUserInContext(req.Context(), owner))

	w := httptest.NewRecorder()
	env.handler.HeadBucket(w, req)

	assert.Equal(t, http.StatusOK, w.Code,
		"the bucket's owner holds the policy and must still reach it")
}
