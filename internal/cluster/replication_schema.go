package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// InitReplicationSchema creates all tables needed for cluster bucket replication
func InitReplicationSchema(db *sql.DB) error {
	ctx := context.Background()

	// Create cluster_bucket_replication table
	if err := createClusterBucketReplicationTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_bucket_replication table: %w", err)
	}

	// Create cluster_replication_queue table
	if err := createClusterReplicationQueueTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_replication_queue table: %w", err)
	}

	// Create cluster_replication_status table
	if err := createClusterReplicationStatusTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_replication_status table: %w", err)
	}

	// Create cluster_tenant_sync table
	if err := createClusterTenantSyncTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_tenant_sync table: %w", err)
	}

	// Create cluster_user_sync table
	if err := createClusterUserSyncTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_user_sync table: %w", err)
	}

	// Create cluster_access_key_sync table
	if err := createClusterAccessKeySyncTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_access_key_sync table: %w", err)
	}

	// Create cluster_bucket_permission_sync table
	if err := createClusterBucketPermissionSyncTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_bucket_permission_sync table: %w", err)
	}

	// Create cluster_global_config table
	if err := createClusterGlobalConfigTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create cluster_global_config table: %w", err)
	}

	return nil
}

// createClusterBucketReplicationTable creates the table for cluster replication rules
// This is SEPARATE from user replication_rules table
func createClusterBucketReplicationTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS cluster_bucket_replication (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '',
		source_bucket TEXT NOT NULL,
		destination_node_id TEXT NOT NULL,
		destination_bucket TEXT NOT NULL,
		sync_interval_seconds INTEGER NOT NULL DEFAULT 10,
		enabled INTEGER NOT NULL DEFAULT 1,
		replicate_deletes INTEGER NOT NULL DEFAULT 1,
		replicate_metadata INTEGER NOT NULL DEFAULT 1,
		prefix TEXT DEFAULT '',
		priority INTEGER NOT NULL DEFAULT 0,
		last_sync_at TIMESTAMP,
		last_error TEXT,
		objects_replicated INTEGER NOT NULL DEFAULT 0,
		bytes_replicated INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		FOREIGN KEY (destination_node_id) REFERENCES cluster_nodes(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_cluster_bucket_repl_source ON cluster_bucket_replication(source_bucket);
	CREATE INDEX IF NOT EXISTS idx_cluster_bucket_repl_dest ON cluster_bucket_replication(destination_node_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_bucket_repl_tenant ON cluster_bucket_replication(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_bucket_repl_enabled ON cluster_bucket_replication(enabled);
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

// createClusterReplicationQueueTable creates the queue for pending cluster replication tasks
func createClusterReplicationQueueTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS cluster_replication_queue (
		id TEXT PRIMARY KEY,
		replication_rule_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT '',
		source_bucket TEXT NOT NULL,
		object_key TEXT NOT NULL,
		destination_node_id TEXT NOT NULL,
		destination_bucket TEXT NOT NULL,
		operation TEXT NOT NULL DEFAULT 'PUT',
		status TEXT NOT NULL DEFAULT 'pending',
		attempts INTEGER NOT NULL DEFAULT 0,
		max_attempts INTEGER NOT NULL DEFAULT 3,
		last_error TEXT,
		priority INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		scheduled_at TIMESTAMP,
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		FOREIGN KEY (replication_rule_id) REFERENCES cluster_bucket_replication(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_cluster_repl_queue_rule ON cluster_replication_queue(replication_rule_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_repl_queue_status ON cluster_replication_queue(status);
	CREATE INDEX IF NOT EXISTS idx_cluster_repl_queue_scheduled ON cluster_replication_queue(scheduled_at);
	CREATE INDEX IF NOT EXISTS idx_cluster_repl_queue_object ON cluster_replication_queue(source_bucket, object_key);
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

// createClusterReplicationStatusTable creates the table for tracking replication status per object
func createClusterReplicationStatusTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS cluster_replication_status (
		id TEXT PRIMARY KEY,
		replication_rule_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT '',
		source_bucket TEXT NOT NULL,
		object_key TEXT NOT NULL,
		destination_node_id TEXT NOT NULL,
		destination_bucket TEXT NOT NULL,
		source_version_id TEXT,
		destination_version_id TEXT,
		source_etag TEXT,
		destination_etag TEXT,
		source_size INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'pending',
		last_sync_at TIMESTAMP,
		last_error TEXT,
		replicated_at TIMESTAMP,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		FOREIGN KEY (replication_rule_id) REFERENCES cluster_bucket_replication(id) ON DELETE CASCADE,
		UNIQUE(replication_rule_id, object_key)
	);
	CREATE INDEX IF NOT EXISTS idx_cluster_repl_status_rule ON cluster_replication_status(replication_rule_id);
	CREATE INDEX IF NOT EXISTS idx_cluster_repl_status_object ON cluster_replication_status(source_bucket, object_key);
	CREATE INDEX IF NOT EXISTS idx_cluster_repl_status_status ON cluster_replication_status(status);
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

// createClusterTenantSyncTable creates the table for tracking tenant synchronization
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

// createClusterUserSyncTable creates the table for tracking user synchronization
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

// createClusterAccessKeySyncTable creates the table for tracking access key synchronization
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

// createClusterBucketPermissionSyncTable creates the table for tracking bucket permission synchronization
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

// createClusterGlobalConfigTable creates the table for global cluster replication settings
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

	// Insert default values if not exists
	defaults := map[string]struct {
		value       string
		description string
	}{
		"default_sync_interval_seconds": {
			value:       "60",
			description: "Default sync interval in seconds for new replication rules",
		},
		"min_sync_interval_seconds": {
			value:       "10",
			description: "Minimum allowed sync interval in seconds",
		},
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
		"replication_worker_count": {
			value:       "5",
			description: "Number of concurrent replication workers",
		},
		"queue_check_interval_seconds": {
			value:       "10",
			description: "Interval for checking replication queue in seconds",
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

// GetGlobalConfig retrieves a global configuration value
func GetGlobalConfig(ctx context.Context, db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM cluster_global_config WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetGlobalConfig sets a global configuration value
func SetGlobalConfig(ctx context.Context, db *sql.DB, key, value string) error {
	now := time.Now()
	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_global_config (key, value, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`, key, value, now, now, value, now)
	return err
}
