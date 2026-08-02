package auth

// Bootstrap checks: what a real installation looks like on the first boot after
// upgrading, and on a database that reached the IAM schema without ever being
// converted.
//
// This is the failure that locked the administrator out of the console: the
// request path reads only IAM policies, so an installation whose permissions
// were never converted has nobody with any permission at all.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBootstrap_AdminCanUseTheConsole is the check that would have caught the
// lockout: after opening the store the way the server does, an administrator
// must be able to sign in.
func TestBootstrap_AdminCanUseTheConsole(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	admin := &User{
		ID: "bootstrap-admin", Username: "bootstrap-admin",
		Status: UserStatusActive, Roles: []string{RoleAdmin},
	}
	require.NoError(t, am.store.CreateUser(admin))

	allowed, err := am.HasCapability(ctx, admin.ID, admin.Roles, CapConsoleAccess)
	require.NoError(t, err)
	assert.True(t, allowed, "an administrator must be able to reach the console")

	for _, capability := range AllCapabilities {
		allowed, err := am.HasCapability(ctx, admin.ID, admin.Roles, capability)
		require.NoError(t, err)
		assert.True(t, allowed, "an administrator must hold %q", capability)
	}

	hasAccess, level, err := am.CheckBucketAccess(ctx, "any-bucket", admin.ID)
	require.NoError(t, err)
	assert.True(t, hasAccess, "an administrator reaches every bucket without a grant")
	assert.Equal(t, PermissionLevelAdmin, level)
}

// TestBootstrap_OrdinaryUserKeepsConsoleAccess covers the other half: the
// conversion has to carry over what the non-admin roles granted, or everyone
// but the administrator is locked out instead.
func TestBootstrap_OrdinaryUserKeepsConsoleAccess(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	user := &User{
		ID: "bootstrap-user", Username: "bootstrap-user",
		Status: UserStatusActive, Roles: []string{"user"},
	}
	require.NoError(t, am.store.CreateUser(user))

	allowed, err := am.HasCapability(ctx, user.ID, user.Roles, CapConsoleAccess)
	require.NoError(t, err)
	assert.True(t, allowed, "the user role granted console access and must keep it")

	allowed, err = am.HasCapability(ctx, user.ID, user.Roles, CapIAMManage)
	require.NoError(t, err)
	assert.False(t, allowed, "and must not gain anything it never had")
}

// TestBootstrap_ConversionHealsAnUnconvertedDatabase covers the case that broke:
// the policies are gone but the old tables are intact, which is what a database
// that reached the IAM schema without being converted looks like.
func TestBootstrap_ConversionHealsAnUnconvertedDatabase(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	admin := &User{
		ID: "healed-admin", Username: "healed-admin",
		Status: UserStatusActive, Roles: []string{RoleAdmin},
	}
	require.NoError(t, am.store.CreateUser(admin))

	// Wipe every policy, leaving the pre-IAM tables as they are.
	_, err := am.store.db.Exec(`DELETE FROM iam_policy_attachments`)
	require.NoError(t, err)
	_, err = am.store.db.Exec(`DELETE FROM iam_inline_policies`)
	require.NoError(t, err)

	allowed, err := am.HasCapability(ctx, admin.ID, admin.Roles, CapConsoleAccess)
	require.NoError(t, err)
	require.False(t, allowed, "with the policies gone there is nothing to grant access")

	needs, err := am.store.NeedsLegacyConversion()
	require.NoError(t, err)
	assert.True(t, needs, "an unconverted database must be recognised as such")

	require.NoError(t, am.store.ConvertLegacyPermissions())

	allowed, err = am.HasCapability(ctx, admin.ID, admin.Roles, CapConsoleAccess)
	require.NoError(t, err)
	assert.True(t, allowed, "converting restores the administrator's access")
}

// TestBootstrap_ConversionIsIdempotent guards against a second run duplicating
// or contradicting what the first one wrote.
func TestBootstrap_ConversionIsIdempotent(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	user := &User{
		ID: "idempotent-user", Username: "idempotent-user",
		Status: UserStatusActive, Roles: []string{"user"},
	}
	require.NoError(t, am.store.CreateUser(user))
	require.NoError(t, am.store.GrantBucketAccess("data", user.ID, "", PermissionLevelWrite, "admin", 0))

	require.NoError(t, am.store.ConvertLegacyPermissions())
	first, err := am.store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)

	require.NoError(t, am.store.ConvertLegacyPermissions())
	second, err := am.store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)

	assert.Equal(t, len(first), len(second), "running the conversion again must not add policies")

	allowed, err := am.HasCapability(ctx, user.ID, user.Roles, CapObjectUpload)
	require.NoError(t, err)
	assert.True(t, allowed)
}
