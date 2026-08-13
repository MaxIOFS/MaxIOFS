package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetUserPermissions_DoesNotBakeInheritedGrants(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	store := am.store

	user := &User{ID: "screen-user", Username: "screen-user",
		Status: UserStatusActive, Roles: []string{RoleAdmin}}
	require.NoError(t, store.CreateUser(user))

	// What the screen would display for them.
	shown, err := store.GetUserPermissions(user.ID)
	require.NoError(t, err)
	require.NotEmpty(t, shown.Global, "an administrator shows a full screen")

	// Pressing Save without changing anything.
	require.NoError(t, store.SetUserPermissions(user.ID, shown))

	// Now take the role away. Nothing should be left behind.
	user.Roles = []string{RoleReadOnly}
	require.NoError(t, store.UpdateUser(user))

	set, err := am.buildPolicySetFor(user.ID, user.Roles, "")
	require.NoError(t, err)

	assert.False(t, set.AllowsAnywhere(ActionDeleteBucket),
		"saving an unchanged screen must not make a role's permissions permanent")
	assert.False(t, set.AllowsAnywhere(ActionSuperAdmin),
		"least of all administration")
}

// TestSetUserPermissions_StillStoresRealGrants: subtracting what is inherited
// must not swallow what an administrator is actually granting.
func TestSetUserPermissions_StillStoresRealGrants(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	store := am.store

	user := &User{ID: "granted-user", Username: "granted-user",
		Status: UserStatusActive, Roles: []string{RoleGuest}}
	require.NoError(t, store.CreateUser(user))

	require.NoError(t, store.SetUserPermissions(user.ID, &UserPermissions{
		Global: []string{},
		Buckets: []BucketGrant{{
			Bucket:  "reports",
			Actions: []string{ActionGetObject, ActionPutObject},
		}},
	}))

	set, err := am.buildPolicySetFor(user.ID, user.Roles, "")
	require.NoError(t, err)

	assert.True(t, set.AllowsOwnAccount(ActionPutObject, "arn:aws:s3:::reports/q1.csv"),
		"a grant the user did not already hold must be stored")
	assert.False(t, set.AllowsOwnAccount(ActionPutObject, "arn:aws:s3:::other/q1.csv"),
		"and only where it was given")
}
