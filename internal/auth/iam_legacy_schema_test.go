package auth

// Startup against the schema an installation actually has.
//
// Amending a migration that a database already applied changes nothing there.
// Three times today a change was made to migration 18 and verified only against
// a fresh database, where the amended migration runs; on an installation that
// had already applied it the old shape remained and the server died on boot.
// These tests run against the old shape on purpose.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLegacySchema_RolesWithNotNullTrustPolicy reproduces the crash: the column
// was made nullable in the migration, but a database that applied it earlier
// still has NOT NULL, and inserting a role with no trust policy aborted startup.
func TestLegacySchema_RolesWithNotNullTrustPolicy(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	// Rebuild the table the way an already-upgraded installation has it.
	_, err := am.store.db.Exec(`DROP TABLE iam_roles`)
	require.NoError(t, err)
	_, err = am.store.db.Exec(`
		CREATE TABLE iam_roles (
			name TEXT PRIMARY KEY,
			arn TEXT NOT NULL UNIQUE,
			path TEXT NOT NULL DEFAULT '/',
			description TEXT,
			assume_role_policy TEXT NOT NULL,
			max_session_duration INTEGER NOT NULL DEFAULT 3600,
			tenant_id TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	require.NoError(t, err)

	require.NoError(t, am.store.EnsureAssignableRoles(),
		"startup must work against the schema an upgraded installation actually has")

	roles, err := am.store.ListIAMRoles()
	require.NoError(t, err)
	assert.NotEmpty(t, roles)

	for _, role := range roles {
		assert.Empty(t, role.AssumeRolePolicy, "an assigned role reads back with no trust policy")
	}
}

// TestLegacySchema_CatalogueIsCreatedWhenMissing covers the same class for the
// permission catalogue, which was also added to an already-applied migration.
func TestLegacySchema_CatalogueIsCreatedWhenMissing(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	_, err := am.store.db.Exec(`DROP TABLE iam_permissions`)
	require.NoError(t, err)

	_, err = am.store.ListPermissionCatalog()
	require.Error(t, err, "the table is gone, so the console would fail")

	require.NoError(t, am.store.EnsurePermissionCatalog())

	groups, err := am.store.ListPermissionCatalog()
	require.NoError(t, err)
	assert.NotEmpty(t, groups, "the catalogue is rebuilt without a migration")
}
