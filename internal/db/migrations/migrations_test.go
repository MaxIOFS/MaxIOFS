package migrations

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func createTestDB(t *testing.T) *sql.DB {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func TestNewMigrationManager(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)

	manager := NewMigrationManager(db, logger)
	require.NotNil(t, manager)
	assert.NotNil(t, manager.db)
	assert.NotNil(t, manager.logger)
	assert.Greater(t, len(manager.migrations), 0)
}

func TestMigrationManager_Initialize(t *testing.T) {
	db := createTestDB(t)
	manager := NewMigrationManager(db, nil)

	err := manager.Initialize()
	require.NoError(t, err)

	// Verify schema_version table exists
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version'").Scan(&tableName)
	require.NoError(t, err)
	assert.Equal(t, "schema_version", tableName)
}

func TestMigrationManager_GetCurrentVersion_EmptyDB(t *testing.T) {
	db := createTestDB(t)
	manager := NewMigrationManager(db, nil)

	err := manager.Initialize()
	require.NoError(t, err)

	version, err := manager.GetCurrentVersion()
	require.NoError(t, err)
	assert.Equal(t, 0, version)
}

func TestMigrationManager_GetTargetVersion(t *testing.T) {
	db := createTestDB(t)
	manager := NewMigrationManager(db, nil)

	targetVersion := manager.GetTargetVersion()
	assert.Greater(t, targetVersion, 0)
	assert.Equal(t, 11, targetVersion)
}

func TestMigrationManager_Migrate_EmptyDB(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	err := manager.Migrate()
	require.NoError(t, err)

	// Verify current version is now the target version
	currentVersion, err := manager.GetCurrentVersion()
	require.NoError(t, err)
	assert.Equal(t, manager.GetTargetVersion(), currentVersion)

	// Verify all core tables exist
	tables := []string{
		"tenants",
		"users",
		"access_keys",
		"shares",
		"system_settings",
		"replication_rules",
		"cluster_nodes",
		"metric_snapshots",
		"bucket_permissions",
		"bucket_inventory_configs",
	}

	for _, table := range tables {
		var tableName string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&tableName)
		assert.NoError(t, err, "Table %s should exist", table)
	}
}

func TestMigrationManager_Migrate_AlreadyUpToDate(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	// Run migrations first time
	err := manager.Migrate()
	require.NoError(t, err)

	// Run migrations second time (should be no-op)
	err = manager.Migrate()
	require.NoError(t, err)

	// Verify version didn't change
	currentVersion, err := manager.GetCurrentVersion()
	require.NoError(t, err)
	assert.Equal(t, manager.GetTargetVersion(), currentVersion)
}

func TestMigrationManager_MigrateTo(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	// Migrate to version 3
	err := manager.MigrateTo(3)
	require.NoError(t, err)

	currentVersion, err := manager.GetCurrentVersion()
	require.NoError(t, err)
	assert.Equal(t, 3, currentVersion)

	// Verify tables from migration 1 exist
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='tenants'").Scan(&tableName)
	assert.NoError(t, err)

	// Verify tables from migration 2 exist
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='shares'").Scan(&tableName)
	assert.NoError(t, err)

	// Verify tables from migration 3 exist
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='system_settings'").Scan(&tableName)
	assert.NoError(t, err)

	// Verify tables from migration 4 don't exist yet
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='replication_rules'").Scan(&tableName)
	assert.Error(t, err)
}

func TestMigrationManager_MigrateTo_ThenMigrateAll(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	// First migrate to version 3
	err := manager.MigrateTo(3)
	require.NoError(t, err)

	currentVersion, err := manager.GetCurrentVersion()
	require.NoError(t, err)
	assert.Equal(t, 3, currentVersion)

	// Now migrate to latest
	err = manager.Migrate()
	require.NoError(t, err)

	currentVersion, err = manager.GetCurrentVersion()
	require.NoError(t, err)
	assert.Equal(t, manager.GetTargetVersion(), currentVersion)
}

func TestMigrationManager_GetMigrationHistory(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	// Migrate to version 3
	err := manager.MigrateTo(3)
	require.NoError(t, err)

	history, err := manager.GetMigrationHistory()
	require.NoError(t, err)
	assert.Equal(t, 3, len(history))

	// Verify first migration record
	assert.Equal(t, 1, history[0].Version)
	assert.Contains(t, history[0].Description, "v0.1.0")
	assert.False(t, history[0].AppliedAt.IsZero())

	// Verify second migration record
	assert.Equal(t, 2, history[1].Version)
	assert.Contains(t, history[1].Description, "v0.2.0")

	// Verify third migration record
	assert.Equal(t, 3, history[2].Version)
	assert.Contains(t, history[2].Description, "v0.4.0")
}

func TestMigrationManager_MigrateWithTransaction(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	// Run migration
	err := manager.Migrate()
	require.NoError(t, err)

	// Verify migration was recorded in schema_version
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, manager.GetTargetVersion(), count)
}

func TestMigrationManager_Migration1_CoreTables(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	err := manager.MigrateTo(1)
	require.NoError(t, err)

	// Verify core tables
	tables := []string{"tenants", "users", "access_keys"}
	for _, table := range tables {
		var tableName string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&tableName)
		assert.NoError(t, err, "Table %s should exist after migration 1", table)
	}

	// Verify indexes exist
	indexes := []string{
		"idx_tenants_name",
		"idx_users_username",
		"idx_access_keys_user_id",
	}
	for _, index := range indexes {
		var indexName string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&indexName)
		assert.NoError(t, err, "Index %s should exist after migration 1", index)
	}
}

func TestMigrationManager_Migration2_Shares(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	err := manager.MigrateTo(2)
	require.NoError(t, err)

	// Verify shares table exists
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='shares'").Scan(&tableName)
	assert.NoError(t, err)

	// Verify shares indexes
	var indexName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name='idx_shares_token'").Scan(&indexName)
	assert.NoError(t, err)
}

func TestMigrationManager_Migration3_Settings(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	err := manager.MigrateTo(3)
	require.NoError(t, err)

	// Verify system_settings table exists
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='system_settings'").Scan(&tableName)
	assert.NoError(t, err)

	// Verify users table has 2FA columns (added in this migration)
	rows, err := db.Query("PRAGMA table_info(users)")
	require.NoError(t, err)
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, dfltValue, pk interface{}
		err = rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk)
		require.NoError(t, err)
		columns[name] = true
	}

	// Check for 2FA columns
	assert.True(t, columns["two_factor_enabled"], "two_factor_enabled column should exist")
	assert.True(t, columns["two_factor_secret"], "two_factor_secret column should exist")
}

func TestMigrationManager_Migration4_Replication(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	err := manager.MigrateTo(4)
	require.NoError(t, err)

	// Verify replication tables exist
	tables := []string{"replication_rules", "replication_queue", "replication_status"}
	for _, table := range tables {
		var tableName string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&tableName)
		assert.NoError(t, err, "Table %s should exist after migration 4", table)
	}
}

func TestMigrationManager_Migration5_Cluster(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	err := manager.MigrateTo(5)
	require.NoError(t, err)

	// Verify cluster tables exist
	tables := []string{
		"cluster_config",
		"cluster_nodes",
		"cluster_health_history",
		"cluster_migrations",
		"metric_snapshots",
		"metric_aggregates",
	}
	for _, table := range tables {
		var tableName string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&tableName)
		assert.NoError(t, err, "Table %s should exist after migration 5", table)
	}
}

func TestMigrationManager_Migration6_ClusterReplication(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	err := manager.MigrateTo(6)
	require.NoError(t, err)

	// Verify cluster replication and sync tables exist
	tables := []string{
		"cluster_bucket_replication",
		"cluster_replication_queue",
		"cluster_replication_status",
		"cluster_tenant_sync",
		"cluster_user_sync",
		"cluster_access_key_sync",
		"cluster_bucket_permission_sync",
		"cluster_global_config",
	}
	for _, table := range tables {
		var tableName string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&tableName)
		assert.NoError(t, err, "Table %s should exist after migration 6", table)
	}
}

func TestMigrationManager_Migration7_BucketInventory(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	err := manager.MigrateTo(7)
	require.NoError(t, err)

	// Verify bucket inventory and permissions tables exist
	tables := []string{
		"bucket_permissions",
		"bucket_inventory_configs",
		"bucket_inventory_reports",
	}
	for _, table := range tables {
		var tableName string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&tableName)
		assert.NoError(t, err, "Table %s should exist after migration 7", table)
	}
}

func TestMigrationManager_FullMigration_AllTables(t *testing.T) {
	db := createTestDB(t)
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	manager := NewMigrationManager(db, logger)

	// Run full migration
	err := manager.Migrate()
	require.NoError(t, err)

	// Verify ALL expected tables exist (29 tables total, excluding schema_version)
	expectedTables := []string{
		// Migration 1
		"tenants", "users", "access_keys",
		// Migration 2
		"shares",
		// Migration 3
		"system_settings",
		// Migration 4
		"replication_rules", "replication_queue", "replication_status",
		// Migration 5
		"cluster_config", "cluster_nodes", "cluster_health_history",
		"cluster_migrations", "metric_snapshots", "metric_aggregates",
		// Migration 6
		"cluster_bucket_replication", "cluster_replication_queue",
		"cluster_replication_status", "cluster_tenant_sync",
		"cluster_user_sync", "cluster_access_key_sync",
		"cluster_bucket_permission_sync", "cluster_global_config",
		// Migration 7
		"bucket_permissions", "bucket_inventory_configs",
		"bucket_inventory_reports",
	}

	for _, table := range expectedTables {
		var tableName string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&tableName)
		assert.NoError(t, err, "Table %s should exist after full migration", table)
	}

	// Count total tables (should include all expected tables + schema_version)
	var tableCount int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tableCount)
	require.NoError(t, err)
	// We have 25 expected tables + schema_version = 26 minimum
	assert.GreaterOrEqual(t, tableCount, len(expectedTables)+1, "Should have at least all expected tables plus schema_version")
}
