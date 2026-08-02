package auth

// Tests for IAM policies, roles and their effect on authorization.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const readWriteAllDocument = `{"Version":"2012-10-17","Statement":[
	{"Effect":"Allow","Action":["s3:*"],"Resource":["*"]}]}`

const readOnlyBucketDocument = `{"Version":"2012-10-17","Statement":[
	{"Effect":"Allow","Action":["s3:GetObject","s3:ListBucket"],
	 "Resource":["arn:aws:s3:::backups","arn:aws:s3:::backups/*"]}]}`

// --- document parsing ---

func TestParseIAMPolicy_Valid(t *testing.T) {
	policy, err := ParseIAMPolicy(readOnlyBucketDocument, IAMMaxManagedPolicyBytes)
	require.NoError(t, err)
	require.Len(t, policy.Statement, 1)
	assert.Equal(t, EffectAllow, policy.Statement[0].Effect)
}

func TestParseIAMPolicy_Rejections(t *testing.T) {
	cases := map[string]string{
		"empty":            ``,
		"not json":         `{`,
		"no statements":    `{"Version":"2012-10-17","Statement":[]}`,
		"bad effect":       `{"Statement":[{"Effect":"Maybe","Action":"s3:*","Resource":"*"}]}`,
		"has principal":    `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"*"},"Action":"s3:*","Resource":"*"}]}`,
		"has condition":    `{"Statement":[{"Effect":"Allow","Condition":{"Bool":{"x":"y"}},"Action":"s3:*","Resource":"*"}]}`,
		"missing action":   `{"Statement":[{"Effect":"Allow","Resource":"*"}]}`,
		"missing resource": `{"Statement":[{"Effect":"Allow","Action":"s3:*"}]}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseIAMPolicy(doc, IAMMaxManagedPolicyBytes)
			assert.ErrorIs(t, err, ErrIAMInvalidInput)
		})
	}
}

func TestParseIAMPolicy_RespectsSizeBudget(t *testing.T) {
	padding := make([]byte, IAMMaxInlinePolicyBytes)
	for i := range padding {
		padding[i] = 'a'
	}
	doc := `{"Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"` + string(padding) + `"}]}`

	_, err := ParseIAMPolicy(doc, IAMMaxInlinePolicyBytes)
	assert.ErrorIs(t, err, ErrIAMInvalidInput, "an inline policy over budget must be refused")

	_, err = ParseIAMPolicy(doc, IAMMaxManagedPolicyBytes)
	assert.NoError(t, err, "the same document fits the managed-policy budget")
}

// --- evaluation ---

func TestEvaluateIAMDocuments(t *testing.T) {
	tests := []struct {
		name      string
		documents []string
		action    string
		resource  string
		want      bool
	}{
		{"allow matching", []string{readOnlyBucketDocument}, "s3:GetObject", "arn:aws:s3:::backups/file.txt", true},
		{"deny other bucket", []string{readOnlyBucketDocument}, "s3:GetObject", "arn:aws:s3:::other/file.txt", false},
		{"deny other action", []string{readOnlyBucketDocument}, "s3:PutObject", "arn:aws:s3:::backups/file.txt", false},
		{"wildcard allows", []string{readWriteAllDocument}, "s3:DeleteObject", "arn:aws:s3:::any/key", true},
		{"no documents denies", nil, "s3:GetObject", "arn:aws:s3:::backups/f", false},
		{"unmapped action denies", []string{readWriteAllDocument}, "", "arn:aws:s3:::any", false},
		{"unparsable document denies", []string{`{`}, "s3:GetObject", "arn:aws:s3:::any", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, EvaluateIAMDocuments(tc.documents, tc.action, tc.resource))
		})
	}
}

func TestEvaluateIAMDocuments_ExplicitDenyBeatsAllowInAnotherDocument(t *testing.T) {
	deny := `{"Statement":[{"Effect":"Deny","Action":"s3:DeleteObject","Resource":"*"}]}`

	assert.True(t, EvaluateIAMDocuments([]string{readWriteAllDocument}, "s3:DeleteObject", "arn:aws:s3:::b/k"))
	assert.False(t, EvaluateIAMDocuments([]string{readWriteAllDocument, deny}, "s3:DeleteObject", "arn:aws:s3:::b/k"),
		"a Deny in any attached policy must win over an Allow in another")
}

// --- store round-trips ---

func TestIAMPolicyLifecycle(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	policy, err := am.CreateIAMPolicy(ctx, "BackupsReadOnly", "/", "read backups", readOnlyBucketDocument)
	require.NoError(t, err)
	assert.Equal(t, "arn:aws:iam:::policy/BackupsReadOnly", policy.ARN)
	assert.Equal(t, "v1", policy.DefaultVersionID)

	fetched, err := am.GetIAMPolicy(ctx, "BackupsReadOnly")
	require.NoError(t, err)
	assert.Equal(t, readOnlyBucketDocument, fetched.Document)

	// A second version becomes the default and changes what the policy means.
	version, err := am.CreateIAMPolicyVersion(ctx, "BackupsReadOnly", readWriteAllDocument, true)
	require.NoError(t, err)
	assert.Equal(t, "v2", version.VersionID)

	fetched, err = am.GetIAMPolicy(ctx, "BackupsReadOnly")
	require.NoError(t, err)
	assert.Equal(t, readWriteAllDocument, fetched.Document)

	versions, err := am.ListIAMPolicyVersions(ctx, "BackupsReadOnly")
	require.NoError(t, err)
	assert.Len(t, versions, 2)

	// The default version is load-bearing and cannot be removed out from under
	// the policy that points at it.
	assert.ErrorIs(t, am.DeleteIAMPolicyVersion(ctx, "BackupsReadOnly", "v2"), ErrIAMDeleteConflict)
	assert.NoError(t, am.DeleteIAMPolicyVersion(ctx, "BackupsReadOnly", "v1"))

	require.NoError(t, am.DeleteIAMPolicy(ctx, "BackupsReadOnly"))
	_, err = am.GetIAMPolicy(ctx, "BackupsReadOnly")
	assert.ErrorIs(t, err, ErrIAMNoSuchEntity)
}

func TestIAMPolicy_AttachedPolicyCannotBeDeleted(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMPolicy(ctx, "Attached", "/", "", readWriteAllDocument)
	require.NoError(t, err)
	require.NoError(t, am.AttachIAMPolicy(ctx, "Attached", IAMTargetUser, user.ID))

	assert.ErrorIs(t, am.DeleteIAMPolicy(ctx, "Attached"), ErrIAMDeleteConflict)

	require.NoError(t, am.DetachIAMPolicy(ctx, "Attached", IAMTargetUser, user.ID))
	assert.NoError(t, am.DeleteIAMPolicy(ctx, "Attached"))
}

func TestIAMPolicy_BuiltinsAreSeededAndProtected(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	policies, err := am.ListIAMPolicies(ctx)
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, p := range policies {
		names[p.Name] = true
	}
	assert.True(t, names["ReadOnlyAccess"])
	assert.True(t, names["ReadWriteAccess"])
	assert.True(t, names["WriteOnlyAccess"])

	assert.ErrorIs(t, am.DeleteIAMPolicy(ctx, "ReadOnlyAccess"), ErrIAMInvalidInput,
		"a built-in policy must not be deletable")
}

func TestIAMInlinePolicy_RoundTrip(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, am.PutIAMInlinePolicy(ctx, IAMTargetUser, user.ID, "backups", readOnlyBucketDocument))

	inline, err := am.GetIAMInlinePolicy(ctx, IAMTargetUser, user.ID, "backups")
	require.NoError(t, err)
	assert.Equal(t, readOnlyBucketDocument, inline.Document)

	list, err := am.ListIAMInlinePolicies(ctx, IAMTargetUser, user.ID)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	require.NoError(t, am.DeleteIAMInlinePolicy(ctx, IAMTargetUser, user.ID, "backups"))
	_, err = am.GetIAMInlinePolicy(ctx, IAMTargetUser, user.ID, "backups")
	assert.ErrorIs(t, err, ErrIAMNoSuchEntity)
}

// --- one model: a policy is a grant, whoever holds it ---

func TestPolicies_AreAGrantForAnyUser(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	// A user created through the IAM API has no role, so everything it can do
	// comes from the policies attached to it — there is no separate class of
	// identity, only a user that happens to hold no role.
	service, err := am.CreateIAMUser(ctx, "veeam-agent", "/", "")
	require.NoError(t, err)
	require.Empty(t, service.Roles)

	documents, err := am.store.EffectivePolicyDocuments(service.ID, service.Roles)
	require.NoError(t, err)
	assert.False(t, EvaluateIAMDocuments(documents, ActionGetObject, "arn:aws:s3:::backups/f"),
		"with no policy attached, the user can do nothing")

	require.NoError(t, am.PutIAMInlinePolicy(ctx, IAMTargetUser, service.ID, "job", readOnlyBucketDocument))

	documents, err = am.store.EffectivePolicyDocuments(service.ID, service.Roles)
	require.NoError(t, err)
	assert.True(t, EvaluateIAMDocuments(documents, ActionGetObject, "arn:aws:s3:::backups/file.txt"),
		"the attached policy is the grant")
	assert.False(t, EvaluateIAMDocuments(documents, ActionGetObject, "arn:aws:s3:::other/file.txt"),
		"and grants only what it names")
}

func TestPolicies_ReachTheCapabilityAndBucketChecks(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	service, err := am.CreateIAMUser(ctx, "veeam-checks", "/", "")
	require.NoError(t, err)
	require.NoError(t, am.PutIAMInlinePolicy(ctx, IAMTargetUser, service.ID, "job", readOnlyBucketDocument))

	// Both questions are answered from the same policy set, so a policy that
	// allows reading a bucket satisfies the capability check and the bucket
	// check alike. Neither can contradict the other.
	allowed, err := am.HasCapability(ctx, service.ID, service.Roles, CapObjectDownload)
	require.NoError(t, err)
	assert.True(t, allowed)

	hasAccess, level, err := am.CheckBucketAccess(ctx, "backups", service.ID)
	require.NoError(t, err)
	assert.True(t, hasAccess)
	assert.Equal(t, PermissionLevelRead, level)

	hasAccess, _, err = am.CheckBucketAccess(ctx, "not-granted", service.ID)
	require.NoError(t, err)
	assert.False(t, hasAccess, "the policy names one bucket, so only that one is reachable")

	// The policy is read-only, so an upload is refused at the same single point.
	allowed, err = am.HasCapability(ctx, service.ID, service.Roles, CapObjectUpload)
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestPolicies_AttachedToAnExistingUserWiden(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	// The user role permits reading, but grants no bucket on its own.
	before, err := am.store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)
	require.False(t, EvaluateIAMDocuments(before, ActionGetObject, "arn:aws:s3:::backups/f"))

	require.NoError(t, am.PutIAMInlinePolicy(ctx, IAMTargetUser, user.ID, "extra", readOnlyBucketDocument))

	after, err := am.store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)
	assert.True(t, EvaluateIAMDocuments(after, ActionGetObject, "arn:aws:s3:::backups/f"),
		"an attached policy grants, exactly as it does in AWS")
}

func TestPolicies_DenyStatementWinsOverEverything(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, am.store.GrantBucketAccess("data", user.ID, "", PermissionLevelWrite, "admin", 0))
	require.NoError(t, am.PutIAMInlinePolicy(ctx, IAMTargetUser, user.ID, "no-deletes",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Deny",`+
			`"Action":["s3:DeleteObject"],"Resource":["*"]}]}`))

	documents, err := am.store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)

	assert.True(t, EvaluateIAMDocuments(documents, ActionPutObject, "arn:aws:s3:::data/f"))
	assert.False(t, EvaluateIAMDocuments(documents, ActionDeleteObject, "arn:aws:s3:::data/f"),
		"an explicit Deny beats the grant the bucket permission produced")
}

func TestCreateIAMUser_RejectsDuplicate(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMUser(ctx, "dup", "/", "")
	require.NoError(t, err)
	_, err = am.CreateIAMUser(ctx, "dup", "/", "")
	assert.ErrorIs(t, err, ErrIAMEntityExists)
}

func TestDeleteIAMUser_RefusesUsersHoldingARole(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()

	assert.ErrorIs(t, am.DeleteIAMUser(context.Background(), user.Username), ErrIAMInvalidInput,
		"an integration must not be able to delete an account that holds a role")
}
