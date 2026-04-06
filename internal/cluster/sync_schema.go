package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// InitReplicationSchema creates all tables needed for cluster synchronization.
// The name is kept for backwards compatibility with existing test setups.
func InitReplicationSchema(db *sql.DB) error {
	ctx := context.Background()

	if err := createClusterTenantSyncTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_tenant_sync table: %w", err)
	}
	if err := createClusterUserSyncTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_user_sync table: %w", err)
	}
	if err := createClusterAccessKeySyncTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_access_key_sync table: %w", err)
	}
	if err := createClusterBucketPermissionSyncTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_bucket_permission_sync table: %w", err)
	}
	if err := createClusterIDPProviderSyncTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_idp_provider_sync table: %w", err)
	}
	if err := createClusterGroupMappingSyncTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_group_mapping_sync table: %w", err)
	}
	if err := createClusterGlobalConfigTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_global_config table: %w", err)
	}
	if err := createClusterDeletionLogTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_deletion_log table: %w", err)
	}

	return nil
}

func createClusterTenantSyncTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS cluster_tenant_sync (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		source_node_id TEXT NOT NULL,
		destination_node_id TEXT NOT NULL,
		tenant_checksum TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		last_sync_at TIMESTAMP,
		last_error TEXT,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		UNIQUE(tenant_id, destination_node_id)
	);
	CREATE INDEX IF NOT EXISTS idx_cluster_tenant_sync_tenant ON cluster_tenant_sync(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_tenant_sync_dest ON cluster_tenant_sync(destination_node_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_tenant_sync_status ON cluster_tenant_sync(status);
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

func createClusterUserSyncTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS cluster_user_sync (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		source_node_id TEXT NOT NULL,
		destination_node_id TEXT NOT NULL,
		user_checksum TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		last_sync_at TIMESTAMP,
		last_error TEXT,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		UNIQUE(user_id, destination_node_id)
	);
	CREATE INDEX IF NOT EXISTS idx_cluster_user_sync_user ON cluster_user_sync(user_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_user_sync_dest ON cluster_user_sync(destination_node_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_user_sync_status ON cluster_user_sync(status);
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

func createClusterAccessKeySyncTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS cluster_access_key_sync (
		id TEXT PRIMARY KEY,
		access_key_id TEXT NOT NULL,
		source_node_id TEXT NOT NULL,
		destination_node_id TEXT NOT NULL,
		key_checksum TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		last_sync_at TIMESTAMP,
		last_error TEXT,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		UNIQUE(access_key_id, destination_node_id)
	);
	CREATE INDEX IF NOT EXISTS idx_cluster_access_key_sync_key ON cluster_access_key_sync(access_key_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_access_key_sync_dest ON cluster_access_key_sync(destination_node_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_access_key_sync_status ON cluster_access_key_sync(status);
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

func createClusterBucketPermissionSyncTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS cluster_bucket_permission_sync (
		id TEXT PRIMARY KEY,
		permission_id TEXT NOT NULL,
		source_node_id TEXT NOT NULL,
		destination_node_id TEXT NOT NULL,
		permission_checksum TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		last_sync_at TIMESTAMP,
		last_error TEXT,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		UNIQUE(permission_id, destination_node_id)
	);
	CREATE INDEX IF NOT EXISTS idx_cluster_bucket_perm_sync_perm ON cluster_bucket_permission_sync(permission_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_bucket_perm_sync_dest ON cluster_bucket_permission_sync(destination_node_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_bucket_perm_sync_status ON cluster_bucket_permission_sync(status);
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

func createClusterGlobalConfigTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS cluster_global_config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		description TEXT,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);
	`
	if _, err := db.ExecContext(ctx, query); err != nil {
		return err
	}

	defaults := map[string]struct {
		value       string
		description string
	}{
		"auto_tenant_sync_enabled": {
			value:       "true",
			description: "Enable automatic tenant synchronization between all nodes",
		},
		"tenant_sync_interval_seconds": {
			value:       "30",
			description: "Interval for tenant synchronization checks in seconds",
		},
		"auto_user_sync_enabled": {
			value:       "true",
			description: "Enable automatic user synchronization between all nodes",
		},
		"user_sync_interval_seconds": {
			value:       "30",
			description: "Interval for user synchronization checks in seconds",
		},
		"auto_access_key_sync_enabled": {
			value:       "true",
			description: "Enable automatic access key synchronization between all nodes",
		},
		"access_key_sync_interval_seconds": {
			value:       "30",
			description: "Interval for access key synchronization checks in seconds",
		},
		"auto_bucket_permission_sync_enabled": {
			value:       "true",
			description: "Enable automatic bucket permission synchronization between all nodes",
		},
		"bucket_permission_sync_interval_seconds": {
			value:       "30",
			description: "Interval for bucket permission synchronization checks in seconds",
		},
		"auto_idp_provider_sync_enabled": {
			value:       "true",
			description: "Enable automatic IDP provider synchronization between all nodes",
		},
		"idp_provider_sync_interval_seconds": {
			value:       "30",
			description: "Interval for IDP provider synchronization checks in seconds",
		},
		"auto_group_mapping_sync_enabled": {
			value:       "true",
			description: "Enable automatic IDP group mapping synchronization between all nodes",
		},
		"group_mapping_sync_interval_seconds": {
			value:       "30",
			description: "Interval for IDP group mapping synchronization checks in seconds",
		},
		"ha.replication_factor": {
			value:       "1",
			description: "Cluster-wide replication factor: 1 = no replication, 2 = mirror (tolerates 1 node failure), 3 = triple copy (tolerates 2 node failures)",
		},
	}

	now := time.Now()
	for key, config := range defaults {
		_, err := db.ExecContext(ctx, `
			INSERT INTO cluster_global_config (key, value, description, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(key) DO NOTHING
		`, key, config.value, config.description, now, now)
		if err != nil {
			return fmt.Errorf("failed to insert default config %s: %w", key, err)
		}
	}

	return nil
}

func createClusterIDPProviderSyncTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS cluster_idp_provider_sync (
		id TEXT PRIMARY KEY,
		provider_id TEXT NOT NULL,
		source_node_id TEXT NOT NULL,
		destination_node_id TEXT NOT NULL,
		provider_checksum TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		last_sync_at TIMESTAMP,
		last_error TEXT,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		UNIQUE(provider_id, destination_node_id)
	);
	CREATE INDEX IF NOT EXISTS idx_cluster_idp_provider_sync_provider ON cluster_idp_provider_sync(provider_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_idp_provider_sync_dest ON cluster_idp_provider_sync(destination_node_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_idp_provider_sync_status ON cluster_idp_provider_sync(status);
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

func createClusterGroupMappingSyncTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS cluster_group_mapping_sync (
		id TEXT PRIMARY KEY,
		mapping_id TEXT NOT NULL,
		source_node_id TEXT NOT NULL,
		destination_node_id TEXT NOT NULL,
		mapping_checksum TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		last_sync_at TIMESTAMP,
		last_error TEXT,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		UNIQUE(mapping_id, destination_node_id)
	);
	CREATE INDEX IF NOT EXISTS idx_cluster_group_mapping_sync_mapping ON cluster_group_mapping_sync(mapping_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_group_mapping_sync_dest ON cluster_group_mapping_sync(destination_node_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_group_mapping_sync_status ON cluster_group_mapping_sync(status);
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

func createClusterDeletionLogTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS cluster_deletion_log (
		id TEXT PRIMARY KEY,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		deleted_by_node_id TEXT NOT NULL,
		deleted_at INTEGER NOT NULL,
		UNIQUE(entity_type, entity_id)
	);
	CREATE INDEX IF NOT EXISTS idx_deletion_log_type ON cluster_deletion_log(entity_type);
	CREATE INDEX IF NOT EXISTS idx_deletion_log_deleted_at ON cluster_deletion_log(deleted_at);
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

// GetGlobalConfig retrieves a global configuration value from the cluster config table.
func GetGlobalConfig(ctx context.Context, db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM cluster_global_config WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetGlobalConfig sets a global configuration value in the cluster config table.
func SetGlobalConfig(ctx context.Context, db *sql.DB, key, value string) error {
	now := time.Now()
	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_global_config (key, value, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`, key, value, now, now, value, now)
	return err
}
