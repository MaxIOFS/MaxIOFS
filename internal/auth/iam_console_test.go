package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsole_AdminLoadsWithEverythingSelected is the one the administrator hit:
// their permissions come from their role, so a screen reading only their own
// policies showed nothing at all.
func TestConsole_AdminLoadsWithEverythingSelected(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	admin := &User{
		ID: "console-admin", Username: "console-admin",
		Status: UserStatusActive, Roles: []string{RoleAdmin},
	}
	require.NoError(t, am.store.CreateUser(admin))

	permissions, err := am.store.GetUserPermissions(admin.ID)
	require.NoError(t, err)

	assert.NotEmpty(t, permissions.Global, "an administrator must not load with nothing selected")
	assert.Contains(t, permissions.Global, "s3:GetObject",
		"a wildcard grant has to expand into the permissions it covers")
	assert.Contains(t, permissions.Global, "console:Access")
	assert.Contains(t, permissions.Global, "s3:BypassGovernanceRetention")
}

// TestConsole_OrdinaryUserLoadsWhatTheirRoleGrants covers the same path for a
// role that grants some things and not others.
func TestConsole_OrdinaryUserLoadsWhatTheirRoleGrants(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	user := &User{
		ID: "console-user", Username: "console-user",
		Status: UserStatusActive, Roles: []string{"user"},
	}
	require.NoError(t, am.store.CreateUser(user))

	permissions, err := am.store.GetUserPermissions(user.ID)
	require.NoError(t, err)

	assert.Contains(t, permissions.Global, "console:Access",
		"the user role grants console access and the screen must show it")
	assert.NotContains(t, permissions.Global, "iam:*",
		"and must not show something the role never granted")
}

// TestConsole_BucketGrantsAppearAgainstTheirBucket checks the per-bucket half:
// a grant has to load against the bucket it names, not into the global scope.
func TestConsole_BucketGrantsAppearAgainstTheirBucket(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	user := &User{
		ID: "console-bucket-user", Username: "console-bucket-user",
		Status: UserStatusActive, Roles: []string{"user"},
	}
	require.NoError(t, am.store.CreateUser(user))
	require.NoError(t, am.store.GrantBucketAccess("reports", user.ID, "", PermissionLevelWrite, "admin", 0))

	permissions, err := am.store.GetUserPermissions(user.ID)
	require.NoError(t, err)

	var found *BucketGrant
	for i := range permissions.Buckets {
		if permissions.Buckets[i].Bucket == "reports" {
			found = &permissions.Buckets[i]
		}
	}
	require.NotNil(t, found, "the granted bucket must appear as its own scope")
	assert.Contains(t, found.Actions, "s3:PutObject")
}

// TestConsole_AssignableRolesExist is the empty-dropdown failure: creating a
// user offers the roles from the role table, and a database an earlier build
// converted has the attachments but not the rows.
func TestConsole_AssignableRolesExist(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	roles, err := am.store.ListIAMRoles()
	require.NoError(t, err)

	byName := make(map[string]*IAMRole)
	for _, role := range roles {
		byName[role.Name] = role
	}

	for _, expected := range []string{RoleAdmin, "user", "readonly", "guest"} {
		require.Contains(t, byName, expected, "role %q must exist so it can be assigned", expected)
		assert.Empty(t, byName[expected].AssumeRolePolicy,
			"a role users are assigned carries no trust policy")
	}
}

// TestConsole_AssignableRolesHealAnOldDatabase reproduces the state that broke
// the dropdown: attachments present, role rows gone.
func TestConsole_AssignableRolesHealAnOldDatabase(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	_, err := am.store.db.Exec(`DELETE FROM iam_roles`)
	require.NoError(t, err)

	roles, err := am.store.ListIAMRoles()
	require.NoError(t, err)
	require.Empty(t, roles, "the dropdown would be empty in this state")

	require.NoError(t, am.store.EnsureAssignableRoles())

	roles, err = am.store.ListIAMRoles()
	require.NoError(t, err)
	assert.NotEmpty(t, roles, "the roles come back without touching anything else")
}
