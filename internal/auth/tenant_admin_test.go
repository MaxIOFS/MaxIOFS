package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantAdmin_RolePoliciesStillApply(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, am.CreateTenant(ctx, &Tenant{
		ID: "ta1", Name: "ta1", DisplayName: "ta1", Status: "active",
		MaxAccessKeys: 10, MaxStorageBytes: 1 << 30, MaxBuckets: 10}))

	// An operator attaches something extra to the admin role, and revokes
	// something else from it.
	require.NoError(t, am.store.PutIAMInlinePolicy(IAMTargetRole, RoleAdmin, "extra",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow",`+
			`"Action":["`+ActionBypassGovernanceRetention+`"],"Resource":["*"]}]}`))
	require.NoError(t, am.store.PutIAMInlinePolicy(IAMTargetRole, RoleAdmin, "revoked",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Deny",`+
			`"Action":["`+ActionDeleteObject+`"],"Resource":["*"]}]}`))

	set, err := am.ResolvePolicySet(ctx, &User{
		ID: "ta-admin", TenantID: "ta1", Roles: []string{RoleAdmin}})
	require.NoError(t, err)

	assert.True(t, set.AllowsAnywhere(ActionBypassGovernanceRetention),
		"a policy attached to the role must reach a tenant administrator")
	assert.False(t, set.Allows(ActionDeleteObject, "arn:aws:s3:::any/key"),
		"and a revocation on the role must reach them too")
}

func TestTenantAdmin_ManagesImmutabilityForTheirTenant(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, am.CreateTenant(ctx, &Tenant{
		ID: "ta2", Name: "ta2", DisplayName: "ta2", Status: "active",
		MaxAccessKeys: 10, MaxStorageBytes: 1 << 30, MaxBuckets: 10}))

	set, err := am.ResolvePolicySet(ctx, &User{
		ID: "ta-admin2", TenantID: "ta2", Roles: []string{RoleAdmin}})
	require.NoError(t, err)

	assert.True(t, set.Allows(ActionGetBucketObjectLockConfiguration, "arn:aws:s3:::b"))
	assert.True(t, set.Allows(ActionPutBucketObjectLockConfiguration, "arn:aws:s3:::b"))
}

// TestTenantAdmin_HoldsNoUnscopedFullAccess keeps the scoping that the
// substitution exists for: what is dropped is the unscoped grant, nothing else.
func TestTenantAdmin_HoldsNoUnscopedFullAccess(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, am.CreateTenant(ctx, &Tenant{
		ID: "ta3", Name: "ta3", DisplayName: "ta3", Status: "active",
		MaxAccessKeys: 10, MaxStorageBytes: 1 << 30, MaxBuckets: 10}))

	set, err := am.ResolvePolicySet(ctx, &User{
		ID: "ta-admin3", TenantID: "ta3", Roles: []string{RoleAdmin}})
	require.NoError(t, err)

	assert.False(t, set.AllowsAnywhere(ActionSuperAdmin),
		"administering a tenant is not administering the system")
}
