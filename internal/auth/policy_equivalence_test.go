package auth

// Equivalence between the stored permission model and its policy translation.
//
// This is the safety net for the one-time conversion. The comparison is made
// against BOTH old mechanisms combined, over a matrix of user × bucket ×
// action, because each one alone was only half a decision: a capability said
// which operations were possible and a bucket permission said where, and a
// request was served only when both agreed.
//
// The sequence in every test is: create the legacy rows, run the conversion,
// then ask the IAM tables. Nothing translates at request time.
//
// A failure here is not a test to adjust. It means the translation would change
// what some existing user is allowed to do.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capabilityFirstAction returns a representative action of a capability, used
// to ask each model the same question.
func capabilityFirstAction(capability string) string {
	for _, name := range PoliciesForCapability(capability) {
		if entry, ok := CatalogEntry(name); ok && len(entry.Actions) > 0 {
			return entry.Actions[0]
		}
	}
	return ""
}

// legacyDecision reproduces how the two old mechanisms combined: the capability
// system decided whether the operation was possible at all, and the bucket
// permission decided whether it was possible on this bucket.
func legacyDecision(t *testing.T, store *SQLiteStore, userID string, roles []string, capability, bucket string) bool {
	t.Helper()

	allowed, err := store.HasCapability(userID, roles, capability)
	require.NoError(t, err)
	if !allowed {
		return false
	}

	// Admin is never filtered by bucket permissions: every caller short-
	// circuits before consulting them.
	for _, r := range roles {
		if r == RoleAdmin {
			return true
		}
	}

	hasAccess, level, err := store.CheckBucketAccess(bucket, userID)
	require.NoError(t, err)
	if !hasAccess {
		return false
	}
	return containsAction(levelActions(level), capabilityFirstAction(capability))
}

// convertAndAsk runs the conversion and then asks the IAM tables, which is the
// only thing the request path reads.
func convertAndAsk(t *testing.T, store *SQLiteStore) {
	t.Helper()
	require.NoError(t, store.ConvertLegacyPermissions())
}

// policyDecision asks the converted model the same question.
func policyDecision(t *testing.T, store *SQLiteStore, userID string, roles []string, capability, bucket string) bool {
	t.Helper()

	documents, err := store.EffectivePolicyDocuments(userID, roles)
	require.NoError(t, err)

	action := capabilityFirstAction(capability)
	return EvaluateIAMDocuments(documents, action, "arn:aws:s3:::"+bucket+"/key") ||
		EvaluateIAMDocuments(documents, action, "arn:aws:s3:::"+bucket)
}

// TestPolicyTranslation_MatrixMatchesLegacyModel is the core check: over every
// combination of role, bucket-permission level and S3 capability, the unified
// policy set must reach the same verdict the two old mechanisms reached.
func TestPolicyTranslation_MatrixMatchesLegacyModel(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	store := am.store

	// S3 capabilities only — the resource-scoped ones, governed by both
	// mechanisms. The rest are checked separately below.
	s3Capabilities := []string{
		CapObjectDownload, CapObjectUpload, CapObjectDelete,
		CapObjectManageTags, CapObjectManageVersions,
		CapBucketConfigure, CapBucketManagePolicy, CapBucketDelete,
	}

	roles := []string{RoleAdmin, "user", "read", RoleReadOnly, RoleGuest}
	levels := []string{"none", PermissionLevelRead, PermissionLevelWrite, PermissionLevelAdmin}

	for _, role := range roles {
		for _, level := range levels {
			userID := "matrix-" + role + "-" + level
			bucket := "matrix-bucket"

			user := &User{
				ID:       userID,
				Username: userID,
				Status:   UserStatusActive,
				Roles:    []string{role},
			}
			require.NoError(t, store.CreateUser(user))
			if level != "none" {
				require.NoError(t, store.GrantBucketAccess(bucket, userID, "", level, "admin", 0))
			}

			convertAndAsk(t, store)

			for _, capability := range s3Capabilities {
				expected := legacyDecision(t, store, userID, []string{role}, capability, bucket)
				actual := policyDecision(t, store, userID, []string{role}, capability, bucket)

				assert.Equal(t, expected, actual,
					"role=%s level=%s capability=%s: the unified model must decide what the old one decided",
					role, level, capability)
			}
		}
	}
}

// TestPolicyTranslation_RoleAloneGrantsNoBucket is the flaw this harness was
// rebuilt to catch: a capability is not a grant. A user whose role permits
// reading must still not read a bucket nobody granted them.
func TestPolicyTranslation_RoleAloneGrantsNoBucket(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	store := am.store

	user := &User{
		ID: "role-only-user", Username: "role-only-user",
		Status: UserStatusActive, Roles: []string{"user"},
	}
	require.NoError(t, store.CreateUser(user))
	convertAndAsk(t, store)

	documents, err := store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)

	assert.False(t, EvaluateIAMDocuments(documents, ActionGetObject, "arn:aws:s3:::someone-elses/key"),
		"the user role permits reading, but permits it nowhere until a bucket is granted")
	assert.False(t, EvaluateIAMDocuments(documents, ActionPutObject, "arn:aws:s3:::someone-elses/key"))
}

// TestPolicyTranslation_GrantIsNarrowedByTheRole is the other half of the cross
// product: granting write on a bucket to a read-only user gives them read.
func TestPolicyTranslation_GrantIsNarrowedByTheRole(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	store := am.store

	user := &User{
		ID: "narrowed-user", Username: "narrowed-user",
		Status: UserStatusActive, Roles: []string{RoleReadOnly},
	}
	require.NoError(t, store.CreateUser(user))
	require.NoError(t, store.GrantBucketAccess("shared", user.ID, "", PermissionLevelWrite, "admin", 0))
	convertAndAsk(t, store)

	documents, err := store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)

	assert.True(t, EvaluateIAMDocuments(documents, ActionGetObject, "arn:aws:s3:::shared/key"),
		"the readonly role permits reading and the bucket was granted, so reading works")
	assert.False(t, EvaluateIAMDocuments(documents, ActionPutObject, "arn:aws:s3:::shared/key"),
		"a write grant cannot give a readonly role a permission its role never had")
}

// TestPolicyTranslation_NonResourceActionsAreGrantedOutright covers the actions
// that name no bucket. Scoping them to a bucket ARN would make them
// unreachable, since no request against them carries one.
func TestPolicyTranslation_NonResourceActionsAreGrantedOutright(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	store := am.store

	user := &User{
		ID: "console-user", Username: "console-user",
		Status: UserStatusActive, Roles: []string{"user"},
	}
	require.NoError(t, store.CreateUser(user))
	convertAndAsk(t, store)

	documents, err := store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)

	assert.True(t, EvaluateIAMDocuments(documents, ActionConsoleAccess, "*"),
		"the user role grants console access, and it belongs to no bucket")
	assert.True(t, EvaluateIAMDocuments(documents, ActionManageOwnKeys, "*"))
	assert.False(t, EvaluateIAMDocuments(documents, ActionIAMManage, "*"),
		"the user role does not administer IAM")
}

// TestPolicyTranslation_AdminIsUnconditional mirrors the capability system's
// admin short-circuit: everything, everywhere, with no rows consulted.
func TestPolicyTranslation_AdminIsUnconditional(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	store := am.store

	// Strip every capability row for admin: the role was unconditional in the
	// old model and must convert to full access regardless of what is stored.
	_, err := store.db.Exec(`DELETE FROM role_capabilities WHERE role = ?`, RoleAdmin)
	require.NoError(t, err)

	user := &User{
		ID: "admin-user", Username: "admin-user",
		Status: UserStatusActive, Roles: []string{RoleAdmin},
	}
	require.NoError(t, store.CreateUser(user))
	convertAndAsk(t, store)

	documents, err := store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)

	assert.True(t, EvaluateIAMDocuments(documents, ActionPutObject, "arn:aws:s3:::any-bucket/key"),
		"admin needs no bucket grant and no capability rows")
	assert.True(t, EvaluateIAMDocuments(documents, ActionConsoleAccess, "*"))
	assert.True(t, EvaluateIAMDocuments(documents, ActionIAMManage, "*"))
}

// TestPolicyTranslation_RevocationWins checks the per-user override layer: an
// explicit revocation must beat everything the roles and grants allow.
func TestPolicyTranslation_RevocationWins(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	store := am.store

	user := &User{
		ID: "revoked-user", Username: "revoked-user",
		Status: UserStatusActive, Roles: []string{"user"},
	}
	require.NoError(t, store.CreateUser(user))
	require.NoError(t, store.GrantBucketAccess("data", user.ID, "", PermissionLevelWrite, "admin", 0))
	convertAndAsk(t, store)

	before, err := store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)
	require.True(t, EvaluateIAMDocuments(before, ActionDeleteObject, "arn:aws:s3:::data/key"))

	require.NoError(t, store.DenyPermission(user.ID, CapObjectDelete))
	convertAndAsk(t, store)

	after, err := store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)
	assert.False(t, EvaluateIAMDocuments(after, ActionDeleteObject, "arn:aws:s3:::data/key"),
		"an explicit revocation must win, as it does today")
}

// TestPolicyTranslation_GrantOverrideWidens is the mirror case: granting a
// capability a user's role lacks writes an Allow, and an Allow grants. Scoping
// it to particular buckets is done by attaching a policy that names them.
func TestPolicyTranslation_GrantOverrideWidens(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	store := am.store

	user := &User{
		ID: "widened-user", Username: "widened-user",
		Status: UserStatusActive, Roles: []string{RoleReadOnly},
	}
	require.NoError(t, store.CreateUser(user))
	require.NoError(t, store.GrantBucketAccess("data", user.ID, "", PermissionLevelWrite, "admin", 0))
	// Granting is attaching a policy, which is what the console writes.
	require.NoError(t, store.PutIAMInlinePolicy(IAMTargetUser, user.ID,
		"grant-upload", capabilityDocument(CapObjectUpload, true)))
	convertAndAsk(t, store)

	documents, err := store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)

	assert.True(t, EvaluateIAMDocuments(documents, ActionPutObject, "arn:aws:s3:::data/key"),
		"an explicit grant is a policy, and a policy grants what it says")
}

// TestPolicyCatalog_CoversEveryCapability makes sure no capability is left
// without a policy to express it. One with none would silently become
// unenforceable the moment the evaluator takes over.
func TestPolicyCatalog_CoversEveryCapability(t *testing.T) {
	for _, capability := range AllCapabilities {
		policies := PoliciesForCapability(capability)
		require.NotEmpty(t, policies, "capability %q has no policy to express it", capability)

		for _, name := range policies {
			entry, ok := CatalogEntry(name)
			require.True(t, ok, "capability %q maps to unknown policy %q", capability, name)
			require.NotEmpty(t, entry.Actions, "policy %q grants no actions", name)
		}
	}
}
