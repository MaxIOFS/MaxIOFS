package auth

// Tests for IAM roles, trust policies and AssumeRole.

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// iamRequest builds a request the way the S3 router hands one to authorization.
func iamRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(method, "http://localhost:8080"+target, nil)
	require.NoError(t, err)
	return r
}

// trustAnyone is the widest trust policy that can be written, and has to be
// written explicitly — nothing defaults to it.
const trustAnyone = `{"Version":"2012-10-17","Statement":[
	{"Effect":"Allow","Principal":{"AWS":"*"},"Action":"sts:AssumeRole"}]}`

func trustUser(username string) string {
	return `{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Principal":{"AWS":"` + IAMUserARN(username) + `"},"Action":"sts:AssumeRole"}]}`
}

// --- trust policy parsing ---

func TestParseTrustPolicy_RequiresPrincipal(t *testing.T) {
	_, err := ParseTrustPolicy(`{"Statement":[{"Effect":"Allow","Action":"sts:AssumeRole"}]}`)
	assert.ErrorIs(t, err, ErrIAMInvalidInput,
		"a trust policy with no Principal says nothing about who may assume the role")
}

func TestParseTrustPolicy_Rejections(t *testing.T) {
	cases := map[string]string{
		"empty":         ``,
		"not json":      `{`,
		"no statements": `{"Statement":[]}`,
		"bad effect":    `{"Statement":[{"Effect":"Sure","Principal":{"AWS":"*"}}]}`,
		"has condition": `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"*"},"Condition":{"Bool":{"a":"b"}}}]}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseTrustPolicy(doc)
			assert.ErrorIs(t, err, ErrIAMInvalidInput)
		})
	}
}

// --- trust policy evaluation ---

func TestEvaluateTrustPolicy(t *testing.T) {
	user := &User{ID: "user-1", Username: "alice", TenantID: "acme"}
	principals := callerPrincipals(user)

	allowAlice, err := ParseTrustPolicy(trustUser("alice"))
	require.NoError(t, err)
	assert.True(t, EvaluateTrustPolicy(allowAlice, principals))

	allowBob, err := ParseTrustPolicy(trustUser("bob"))
	require.NoError(t, err)
	assert.False(t, EvaluateTrustPolicy(allowBob, principals))

	anyone, err := ParseTrustPolicy(trustAnyone)
	require.NoError(t, err)
	assert.True(t, EvaluateTrustPolicy(anyone, principals))

	tenant, err := ParseTrustPolicy(`{"Statement":[{"Effect":"Allow",
		"Principal":{"AWS":"arn:aws:iam:::tenant/acme"},"Action":"sts:AssumeRole"}]}`)
	require.NoError(t, err)
	assert.True(t, EvaluateTrustPolicy(tenant, principals))
}

func TestEvaluateTrustPolicy_ExplicitDenyWins(t *testing.T) {
	user := &User{ID: "user-1", Username: "alice"}
	policy, err := ParseTrustPolicy(`{"Statement":[
		{"Effect":"Allow","Principal":{"AWS":"*"},"Action":"sts:AssumeRole"},
		{"Effect":"Deny","Principal":{"AWS":"arn:aws:iam:::user/alice"},"Action":"sts:AssumeRole"}]}`)
	require.NoError(t, err)
	assert.False(t, EvaluateTrustPolicy(policy, callerPrincipals(user)))
}

func TestEvaluateTrustPolicy_NonSTSActionDoesNotGrantAssume(t *testing.T) {
	user := &User{ID: "user-1", Username: "alice"}
	policy, err := ParseTrustPolicy(`{"Statement":[
		{"Effect":"Allow","Principal":{"AWS":"*"},"Action":"s3:GetObject"}]}`)
	require.NoError(t, err)
	assert.False(t, EvaluateTrustPolicy(policy, callerPrincipals(user)),
		"a statement about S3 must not double as permission to assume the role")
}

// --- AssumeRole ---

func TestAssumeIAMRole_UnknownRoleIsRefused(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()

	_, err := am.AssumeIAMRole(context.Background(), user, IAMRoleARN("does-not-exist"), "session", 3600, "")
	assert.ErrorIs(t, err, ErrIAMNoSuchEntity,
		"asking for a role that does not exist must fail rather than quietly return other credentials")
}

func TestAssumeIAMRole_RequiresSessionName(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMRole(ctx, "backup", "/", "", trustAnyone, 3600, "")
	require.NoError(t, err)

	_, err = am.AssumeIAMRole(ctx, user, IAMRoleARN("backup"), "", 3600, "")
	assert.ErrorIs(t, err, ErrIAMInvalidInput)
}

func TestAssumeIAMRole_TrustPolicyDecides(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMRole(ctx, "closed", "/", "", trustUser("somebody-else"), 3600, "")
	require.NoError(t, err)
	_, err = am.AssumeIAMRole(ctx, user, IAMRoleARN("closed"), "s", 3600, "")
	assert.ErrorIs(t, err, ErrAccessDenied)

	_, err = am.CreateIAMRole(ctx, "open", "/", "", trustUser(user.Username), 3600, "")
	require.NoError(t, err)
	session, err := am.AssumeIAMRole(ctx, user, IAMRoleARN("open"), "s", 3600, "")
	require.NoError(t, err)
	assert.Equal(t, STSPolicyModeRole, session.PolicyMode)
	assert.Equal(t, IAMRoleARN("open"), session.RoleARN)
	assert.True(t, IsSTSAccessKey(session.TempAccessKeyID))
}

func TestAssumeIAMRole_HonoursMaxSessionDuration(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMRole(ctx, "short", "/", "", trustAnyone, 1800, "")
	require.NoError(t, err)

	_, err = am.AssumeIAMRole(ctx, user, IAMRoleARN("short"), "s", 3600, "")
	assert.ErrorIs(t, err, ErrSTSInvalidDuration, "the role's own cap must bound the session")

	_, err = am.AssumeIAMRole(ctx, user, IAMRoleARN("short"), "s", 1800, "")
	assert.NoError(t, err)
}

func TestAssumeIAMRole_TenantRoleIsNotAssumableFromAnotherTenant(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMRole(ctx, "tenant-scoped", "/", "", trustAnyone, 3600, "other-tenant")
	require.NoError(t, err)

	_, err = am.AssumeIAMRole(ctx, user, IAMRoleARN("tenant-scoped"), "s", 3600, "")
	assert.ErrorIs(t, err, ErrAccessDenied,
		"no trust policy should be able to hand one tenant a credential inside another")
}

// --- what a role session may actually do ---

func TestRoleSession_PermissionsComeFromTheRole(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMRole(ctx, "reader", "/", "", trustAnyone, 3600, "")
	require.NoError(t, err)
	require.NoError(t, am.PutIAMInlinePolicy(ctx, IAMTargetRole, "reader", "scope", readOnlyBucketDocument))

	session, err := am.AssumeIAMRole(ctx, user, IAMRoleARN("reader"), "job", 3600, "")
	require.NoError(t, err)

	stored, err := am.store.GetSTSSession(session.TempAccessKeyID)
	require.NoError(t, err)

	assert.NoError(t, am.enforceSTSSession(stored, iamRequest(t, http.MethodGet, "/backups/f.txt")))
	assert.ErrorIs(t, am.enforceSTSSession(stored, iamRequest(t, http.MethodPut, "/backups/f.txt")),
		ErrAccessDenied, "the role is read-only, so the session is too")
	assert.ErrorIs(t, am.enforceSTSSession(stored, iamRequest(t, http.MethodGet, "/elsewhere/f.txt")),
		ErrAccessDenied)
}

func TestRoleSession_AttachesRolePolicySetToRequest(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMRole(ctx, "reader", "/", "", trustAnyone, 3600, "")
	require.NoError(t, err)
	require.NoError(t, am.PutIAMInlinePolicy(ctx, IAMTargetRole, "reader", "scope", readOnlyBucketDocument))

	session, err := am.AssumeIAMRole(ctx, user, IAMRoleARN("reader"), "job", 3600, "")
	require.NoError(t, err)
	stored, err := am.store.GetSTSSession(session.TempAccessKeyID)
	require.NoError(t, err)

	r := iamRequest(t, http.MethodGet, "/backups/f.txt")
	am.attachRolePolicySetToRequest(r, stored)

	set, ok := PolicySetFromContext(r.Context())
	require.True(t, ok)
	assert.Equal(t, user.ID, set.UserID)
	assert.True(t, set.Allows(ActionGetObject, "arn:aws:s3:::backups/f.txt"))
	assert.False(t, set.Allows(ActionPutObject, "arn:aws:s3:::backups/f.txt"))
}

func TestRoleSession_WithoutRolePoliciesCanDoNothing(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMRole(ctx, "empty", "/", "", trustAnyone, 3600, "")
	require.NoError(t, err)

	session, err := am.AssumeIAMRole(ctx, user, IAMRoleARN("empty"), "job", 3600, "")
	require.NoError(t, err)
	stored, err := am.store.GetSTSSession(session.TempAccessKeyID)
	require.NoError(t, err)

	assert.ErrorIs(t, am.enforceSTSSession(stored, iamRequest(t, http.MethodGet, "/anything/k")), ErrAccessDenied)
}

func TestRoleSession_SessionPolicyNarrowsTheRoleFurther(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMRole(ctx, "wide", "/", "", trustAnyone, 3600, "")
	require.NoError(t, err)
	require.NoError(t, am.PutIAMInlinePolicy(ctx, IAMTargetRole, "wide", "all", readWriteAllDocument))

	narrow := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow",
		"Action":"s3:GetObject","Resource":"arn:aws:s3:::backups/*"}]}`

	session, err := am.AssumeIAMRole(ctx, user, IAMRoleARN("wide"), "job", 3600, narrow)
	require.NoError(t, err)
	stored, err := am.store.GetSTSSession(session.TempAccessKeyID)
	require.NoError(t, err)

	assert.NoError(t, am.enforceSTSSession(stored, iamRequest(t, http.MethodGet, "/backups/f")))
	assert.ErrorIs(t, am.enforceSTSSession(stored, iamRequest(t, http.MethodPut, "/backups/f")), ErrAccessDenied,
		"the session policy narrows a role that would otherwise allow writes")
	assert.ErrorIs(t, am.enforceSTSSession(stored, iamRequest(t, http.MethodGet, "/other/f")), ErrAccessDenied)
}

func TestRoleSession_RevokingTheRoleTakesEffectImmediately(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMRole(ctx, "temporary", "/", "", trustAnyone, 3600, "")
	require.NoError(t, err)
	require.NoError(t, am.PutIAMInlinePolicy(ctx, IAMTargetRole, "temporary", "all", readWriteAllDocument))

	session, err := am.AssumeIAMRole(ctx, user, IAMRoleARN("temporary"), "job", 3600, "")
	require.NoError(t, err)
	stored, err := am.store.GetSTSSession(session.TempAccessKeyID)
	require.NoError(t, err)
	require.NoError(t, am.enforceSTSSession(stored, iamRequest(t, http.MethodGet, "/backups/f")))

	require.NoError(t, am.DeleteIAMRole(ctx, "temporary"))

	assert.ErrorIs(t, am.enforceSTSSession(stored, iamRequest(t, http.MethodGet, "/backups/f")), ErrAccessDenied,
		"deleting a role must cut off the sessions issued from it, not wait for them to expire")
}

func TestParseIAMARN(t *testing.T) {
	resourceType, name, err := ParseIAMARN("arn:aws:iam:::role/backup")
	require.NoError(t, err)
	assert.Equal(t, "role", resourceType)
	assert.Equal(t, "backup", name)

	// An ARN copied from an AWS example carries an account id; the entity is
	// still identified by its name here.
	_, name, err = ParseIAMARN("arn:aws:iam::123456789012:role/backup")
	require.NoError(t, err)
	assert.Equal(t, "backup", name)

	_, _, err = ParseIAMARN("not-an-arn")
	assert.ErrorIs(t, err, ErrIAMInvalidInput)
}
