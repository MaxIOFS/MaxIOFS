package auth

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/db/migrations"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// preIAMSchemaVersion is the schema a v1.5.x deployment is running.
const preIAMSchemaVersion = 17

// seedLegacyInstallation builds a database at the pre-IAM schema holding what a
// real deployment holds.
func seedLegacyInstallation(t *testing.T, dataDir string) {
	t.Helper()

	dbPath := filepath.Join(dataDir, "db", "maxiofs.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	defer db.Close()

	manager := migrations.NewMigrationManager(db, logrus.StandardLogger())
	require.NoError(t, manager.MigrateTo(preIAMSchemaVersion),
		"the deployment is at the schema that shipped before IAM")

	version, err := manager.GetCurrentVersion()
	require.NoError(t, err)
	require.Equal(t, preIAMSchemaVersion, version)

	now := time.Now().Unix()
	insertUser := func(id, username, role, tenant string) {
		roles, _ := json.Marshal([]string{role})
		_, err := db.Exec(`
			INSERT INTO users (id, username, password_hash, display_name, email, status,
				tenant_id, roles, policies, metadata, created_at, updated_at, auth_provider, external_id)
			VALUES (?, ?, 'x', ?, '', 'active', ?, ?, '[]', '{}', ?, ?, 'local', NULL)
		`, id, username, username, nullString(tenant), string(roles), now, now)
		require.NoError(t, err)
	}

	insertUser("u-admin", "the-admin", "admin", "")
	insertUser("u-writer", "the-writer", "user", "")
	insertUser("u-reader", "the-reader", "readonly", "")

	// A bucket grant, the way an operator gives someone access to one bucket.
	_, err = db.Exec(`
		INSERT INTO bucket_permissions (id, bucket_name, bucket_tenant_id, user_id, permission_level, granted_by, granted_at)
		VALUES ('perm-1', 'reports', '', 'u-writer', 'write', 'the-admin', ?)
	`, now)
	require.NoError(t, err)

	// A read-only user given write on one bucket: under the old model the role
	// still capped them at reading.
	_, err = db.Exec(`
		INSERT INTO bucket_permissions (id, bucket_name, bucket_tenant_id, user_id, permission_level, granted_by, granted_at)
		VALUES ('perm-2', 'reports', '', 'u-reader', 'write', 'the-admin', ?)
	`, now)
	require.NoError(t, err)

	// An explicit revocation, which has to keep winning after the upgrade.
	_, err = db.Exec(`
		INSERT INTO user_capability_overrides (id, user_id, capability, granted, granted_by, created_at)
		VALUES ('ovr-1', 'u-writer', 'object:delete', 0, 'the-admin', ?)
	`, now)
	require.NoError(t, err)
}

// openUpgradedStore opens the auth store the way the server does.
func openUpgradedStore(t *testing.T, dataDir string) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(dataDir)
	require.NoError(t, err, "the upgrade must not abort startup")
	return store
}

// TestUpgrade_FromPreIAMDeployment is the check that matters for a running
// installation: after the upgrade, every user can do exactly what they could
// do before, and nothing more.
func TestUpgrade_FromPreIAMDeployment(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "maxiofs-upgrade-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	seedLegacyInstallation(t, dataDir)
	store := openUpgradedStore(t, dataDir)
	defer store.db.Close()

	version, err := migrations.NewMigrationManager(store.db, logrus.StandardLogger()).GetCurrentVersion()
	require.NoError(t, err)
	assert.Equal(t, 18, version, "the upgrade applies the IAM migration")

	allows := func(userID string, roles []string, action, resource string) bool {
		documents, err := store.EffectivePolicyDocuments(userID, roles)
		require.NoError(t, err)
		return EvaluateIAMDocuments(documents, action, resource)
	}

	// The administrator keeps everything, including the console.
	adminDocs, err := store.EffectivePolicyDocuments("u-admin", []string{"admin"})
	require.NoError(t, err)
	assert.True(t, EvaluateIAMDocuments(adminDocs, ActionConsoleAccess, "*"),
		"the administrator must still reach the console after upgrading")
	assert.True(t, EvaluateIAMDocuments(adminDocs, ActionPutObject, "arn:aws:s3:::anything/key"))

	// The writer keeps the bucket they were granted, and only that one.
	assert.True(t, allows("u-writer", []string{"user"}, ActionPutObject, "arn:aws:s3:::reports/f"),
		"a granted bucket keeps working")
	assert.False(t, allows("u-writer", []string{"user"}, ActionPutObject, "arn:aws:s3:::other/f"),
		"and the grant does not spread to buckets nobody gave them")

	// Their explicit revocation still wins.
	assert.False(t, allows("u-writer", []string{"user"}, ActionDeleteObject, "arn:aws:s3:::reports/f"),
		"a revoked capability stays revoked across the upgrade")

	// The read-only user granted write is still limited by their role.
	assert.True(t, allows("u-reader", []string{"readonly"}, ActionGetObject, "arn:aws:s3:::reports/f"))
	assert.False(t, allows("u-reader", []string{"readonly"}, ActionPutObject, "arn:aws:s3:::reports/f"),
		"a write grant must not give a readonly role something its role never had")
}

// TestUpgrade_ConsoleSurfacesAreUsable checks the things the console reads, so
// an upgraded installation does not open to empty screens.
func TestUpgrade_ConsoleSurfacesAreUsable(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "maxiofs-upgrade-console-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	seedLegacyInstallation(t, dataDir)
	store := openUpgradedStore(t, dataDir)
	defer store.db.Close()

	groups, err := store.ListPermissionCatalog()
	require.NoError(t, err)
	assert.NotEmpty(t, groups, "the permission catalogue must exist after upgrading")

	roles, err := store.ListIAMRoles()
	require.NoError(t, err)
	names := map[string]bool{}
	for _, role := range roles {
		names[role.Name] = true
	}
	for _, expected := range []string{"admin", "user", "readonly"} {
		assert.True(t, names[expected], "role %q must be assignable after upgrading", expected)
	}

	permissions, err := store.GetUserPermissions("u-admin")
	require.NoError(t, err)
	assert.NotEmpty(t, permissions.Global, "the administrator must not load with nothing selected")
}

// TestUpgrade_SecondBootChangesNothing guards against the conversion running
// again and duplicating or widening what the first boot produced.
func TestUpgrade_SecondBootChangesNothing(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "maxiofs-upgrade-twice-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	seedLegacyInstallation(t, dataDir)

	first := openUpgradedStore(t, dataDir)
	before, err := first.EffectivePolicyDocuments("u-writer", []string{"user"})
	require.NoError(t, err)
	require.NoError(t, first.db.Close())

	second := openUpgradedStore(t, dataDir)
	defer second.db.Close()
	after, err := second.EffectivePolicyDocuments("u-writer", []string{"user"})
	require.NoError(t, err)

	assert.Equal(t, len(before), len(after), "restarting must not change anyone's permissions")
	assert.False(t, EvaluateIAMDocuments(after, ActionPutObject, "arn:aws:s3:::other/f"),
		"nor widen them")
}
