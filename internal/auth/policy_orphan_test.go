package auth

// Deleting an identity must delete its permissions.
//
// A policy left behind after its owner is gone is a grant nobody can see. It
// stays invisible until an identifier is reused — group and tenant identifiers
// can be — and then applies to whoever inherits it.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func policyRowsFor(t *testing.T, store *SQLiteStore, targetType, targetID string) int {
	t.Helper()
	var inline, attached int
	require.NoError(t, store.db.QueryRow(
		`SELECT COUNT(*) FROM iam_inline_policies WHERE target_type = ? AND target_id = ?`,
		targetType, targetID).Scan(&inline))
	require.NoError(t, store.db.QueryRow(
		`SELECT COUNT(*) FROM iam_policy_attachments WHERE target_type = ? AND target_id = ?`,
		targetType, targetID).Scan(&attached))
	return inline + attached
}

func TestOrphans_DeletingAUserRemovesItsPolicies(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	user := &User{
		ID: "doomed-user", Username: "doomed-user",
		Status: UserStatusActive, Roles: []string{"user"},
	}
	require.NoError(t, am.store.CreateUser(user))
	require.NoError(t, am.store.GrantBucketAccess("data", user.ID, "", PermissionLevelWrite, "admin", 0))
	require.NoError(t, am.store.PutIAMInlinePolicy(IAMTargetUser, user.ID, "extra", readOnlyBucketDocument))
	require.NoError(t, am.store.AttachIAMPolicy("ReadOnlyAccess", IAMTargetUser, user.ID))

	require.Greater(t, policyRowsFor(t, am.store, IAMTargetUser, user.ID), 0)

	require.NoError(t, am.store.DeleteUser(user.ID))

	assert.Equal(t, 0, policyRowsFor(t, am.store, IAMTargetUser, user.ID),
		"a deleted user leaves no permissions behind")
}

func TestOrphans_DeletingAGroupRemovesItsPolicies(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	group := &Group{ID: "doomed-group", Name: "doomed-group"}
	require.NoError(t, am.store.CreateGroup(group))
	require.NoError(t, am.store.GrantGroupBucketAccess("data", group.ID, PermissionLevelWrite, "admin", 0))
	require.NoError(t, am.store.PutIAMInlinePolicy(IAMTargetGroup, group.ID, "extra", readOnlyBucketDocument))

	require.Greater(t, policyRowsFor(t, am.store, IAMTargetGroup, group.ID), 0)

	require.NoError(t, am.store.DeleteGroup(group.ID))

	assert.Equal(t, 0, policyRowsFor(t, am.store, IAMTargetGroup, group.ID),
		"a deleted group leaves no permissions for a future group with the same id")
}

func TestOrphans_DeletingATenantRemovesItsPoliciesAndItsUsers(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	tenant := &Tenant{ID: "doomed-tenant", Name: "doomed-tenant", DisplayName: "Doomed", Status: "active"}
	require.NoError(t, am.store.CreateTenant(tenant))

	user := &User{
		ID: "tenant-member", Username: "tenant-member",
		Status: UserStatusActive, TenantID: tenant.ID, Roles: []string{"user"},
	}
	require.NoError(t, am.store.CreateUser(user))
	require.NoError(t, am.store.PutIAMInlinePolicy(IAMTargetUser, user.ID, "member", readOnlyBucketDocument))
	require.NoError(t, am.store.PutIAMInlinePolicy(IAMTargetTenant, tenant.ID, "tenant-wide", readOnlyBucketDocument))

	require.Greater(t, policyRowsFor(t, am.store, IAMTargetTenant, tenant.ID), 0)
	require.Greater(t, policyRowsFor(t, am.store, IAMTargetUser, user.ID), 0)

	require.NoError(t, am.store.DeleteTenant(tenant.ID))

	assert.Equal(t, 0, policyRowsFor(t, am.store, IAMTargetTenant, tenant.ID),
		"a deleted tenant leaves no tenant-wide permissions")
	assert.Equal(t, 0, policyRowsFor(t, am.store, IAMTargetUser, user.ID),
		"nor permissions belonging to the users it took with it")
}

// TestOrphans_AReusedIdentifierInheritsNothing is why the above matters: it is
// the failure the cleanup prevents.
func TestOrphans_AReusedIdentifierInheritsNothing(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	group := &Group{ID: "recycled-id", Name: "first-group"}
	require.NoError(t, am.store.CreateGroup(group))
	require.NoError(t, am.store.PutIAMInlinePolicy(IAMTargetGroup, group.ID, "wide", readWriteAllDocument))
	require.NoError(t, am.store.DeleteGroup(group.ID))

	// A different group, later, taking the same identifier.
	replacement := &Group{ID: "recycled-id", Name: "second-group"}
	require.NoError(t, am.store.CreateGroup(replacement))

	documents, err := am.store.IAMEffectiveDocuments(IAMTargetGroup, replacement.ID)
	require.NoError(t, err)
	assert.Empty(t, documents,
		"a new group with a reused identifier must not inherit the old one's permissions")
}
