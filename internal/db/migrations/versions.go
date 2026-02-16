package migrations

import (
	"database/sql"
)

// getAllMigrations returns all available migrations
// Each migration corresponds to a MaxIOFS version
func getAllMigrations() []Migration {
	return []Migration{
		migration1_v010_CoreTables(),
		migration2_v020_Shares(),
		migration3_v040_SettingsAndAudit(),
		migration4_v050_Replication(),
		migration5_v060_ClusterAndMetrics(),
		migration6_v061_ClusterReplication(),
		migration7_v062_BucketInventoryAndPermissions(),
		migration8_v062_CurrentSchema(),
		migration9_v090_IdentityProviders(),
		migration10_v091_ClusterDeletionLog(),
	}
}

// migration1_v010_CoreTables creates the core authentication tables
// Corresponds to MaxIOFS v0.1.0 - Initial release
func migration1_v010_CoreTables() Migration {
	return Migration{
		Version:     1,
		Description: "v0.1.0 - Create core tables (tenants, users, access_keys)",
		Up: func(tx *sql.Tx) error {
			// Tenants table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS tenants (
					id TEXT PRIMARY KEY,
					name TEXT UNIQUE NOT NULL,
					display_name TEXT,
					description TEXT,
					status TEXT DEFAULT 'active',
					max_access_keys INTEGER DEFAULT 10,
					max_storage_bytes INTEGER DEFAULT 107374182400,
					current_storage_bytes INTEGER DEFAULT 0,
					max_buckets INTEGER DEFAULT 100,
					current_buckets INTEGER DEFAULT 0,
					metadata TEXT,
					created_at INTEGER NOT NULL,
					updated_at INTEGER NOT NULL
				)
			`); err != nil {
				return err
			}

			// Tenants indexes
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tenants_name ON tenants(name)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status)`); err != nil {
				return err
			}

			// Users table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS users (
					id TEXT PRIMARY KEY,
					username TEXT UNIQUE NOT NULL,
					password_hash TEXT NOT NULL,
					display_name TEXT,
					email TEXT,
					status TEXT DEFAULT 'active',
					tenant_id TEXT,
					roles TEXT,
					policies TEXT,
					metadata TEXT,
					created_at INTEGER NOT NULL,
					updated_at INTEGER NOT NULL,
					FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE SET NULL
				)
			`); err != nil {
				return err
			}

			// Users indexes
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_users_status ON users(status)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_users_tenant_id ON users(tenant_id)`); err != nil {
				return err
			}

			// Access keys table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS access_keys (
					access_key_id TEXT PRIMARY KEY,
					secret_access_key TEXT NOT NULL,
					user_id TEXT NOT NULL,
					status TEXT DEFAULT 'active',
					created_at INTEGER NOT NULL,
					last_used INTEGER,
					FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
				)
			`); err != nil {
				return err
			}

			// Access keys indexes
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_access_keys_user_id ON access_keys(user_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_access_keys_status ON access_keys(status)`); err != nil {
				return err
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			// Down migrations not supported yet
			return nil
		},
	}
}

// migration2_v020_Shares creates the shares table for presigned URLs
// Corresponds to MaxIOFS v0.2.0 - Presigned URLs feature
func migration2_v020_Shares() Migration {
	return Migration{
		Version:     2,
		Description: "v0.2.0 - Add shares table for presigned URLs",
		Up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS shares (
					id TEXT PRIMARY KEY,
					bucket_name TEXT NOT NULL,
					object_key TEXT NOT NULL,
					tenant_id TEXT DEFAULT '',
					access_key_id TEXT NOT NULL,
					secret_key TEXT NOT NULL,
					share_token TEXT NOT NULL UNIQUE,
					expires_at INTEGER,
					created_at INTEGER NOT NULL,
					created_by TEXT NOT NULL,
					UNIQUE(bucket_name, object_key, tenant_id)
				)
			`); err != nil {
				return err
			}

			// Shares indexes
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_shares_token ON shares(share_token)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_shares_bucket_object ON shares(bucket_name, object_key)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_shares_created_by ON shares(created_by)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_shares_expires_at ON shares(expires_at)`); err != nil {
				return err
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	}
}

// migration3_v040_SettingsAndAudit creates settings and audit tables
// Corresponds to MaxIOFS v0.4.0 - Dynamic settings and audit logging
func migration3_v040_SettingsAndAudit() Migration {
	return Migration{
		Version:     3,
		Description: "v0.4.0 - Add system_settings and 2FA support to users",
		Up: func(tx *sql.Tx) error {
			// System settings table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS system_settings (
					key TEXT PRIMARY KEY,
					value TEXT NOT NULL,
					type TEXT NOT NULL,
					category TEXT NOT NULL,
					description TEXT,
					editable INTEGER DEFAULT 1,
					created_at INTEGER NOT NULL,
					updated_at INTEGER NOT NULL
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_settings_category ON system_settings(category)`); err != nil {
				return err
			}

			// Add 2FA columns to users table
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN failed_login_attempts INTEGER DEFAULT 0`); err != nil {
				// Column might already exist, ignore error
			}
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN locked_until INTEGER DEFAULT 0`); err != nil {
				// Column might already exist, ignore error
			}
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN last_failed_login INTEGER DEFAULT 0`); err != nil {
				// Column might already exist, ignore error
			}
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN two_factor_enabled BOOLEAN DEFAULT 0`); err != nil {
				// Column might already exist, ignore error
			}
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN two_factor_secret TEXT`); err != nil {
				// Column might already exist, ignore error
			}
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN two_factor_setup_at INTEGER`); err != nil {
				// Column might already exist, ignore error
			}
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN backup_codes TEXT`); err != nil {
				// Column might already exist, ignore error
			}
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN backup_codes_used TEXT`); err != nil {
				// Column might already exist, ignore error
			}
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN theme_preference TEXT DEFAULT 'system'`); err != nil {
				// Column might already exist, ignore error
			}
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN language_preference TEXT DEFAULT 'en'`); err != nil {
				// Column might already exist, ignore error
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	}
}

// migration4_v050_Replication creates replication tables
// Corresponds to MaxIOFS v0.5.0 - Bucket replication feature
func migration4_v050_Replication() Migration {
	return Migration{
		Version:     4,
		Description: "v0.5.0 - Add replication tables (rules, queue, status)",
		Up: func(tx *sql.Tx) error {
			// Replication rules table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS replication_rules (
					id TEXT PRIMARY KEY,
					tenant_id TEXT NOT NULL,
					source_bucket TEXT NOT NULL,
					destination_endpoint TEXT NOT NULL,
					destination_bucket TEXT NOT NULL,
					destination_access_key TEXT NOT NULL,
					destination_secret_key TEXT NOT NULL,
					destination_region TEXT DEFAULT '',
					prefix TEXT DEFAULT '',
					enabled INTEGER NOT NULL DEFAULT 1,
					priority INTEGER NOT NULL DEFAULT 0,
					mode TEXT NOT NULL DEFAULT 'realtime',
					schedule_interval INTEGER DEFAULT 0,
					conflict_resolution TEXT NOT NULL DEFAULT 'last_write_wins',
					replicate_deletes INTEGER NOT NULL DEFAULT 1,
					replicate_metadata INTEGER NOT NULL DEFAULT 1,
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_replication_rules_tenant ON replication_rules(tenant_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_replication_rules_source ON replication_rules(source_bucket)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_replication_rules_enabled ON replication_rules(enabled)`); err != nil {
				return err
			}

			// Replication queue table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS replication_queue (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					rule_id TEXT NOT NULL,
					tenant_id TEXT NOT NULL,
					bucket TEXT NOT NULL,
					object_key TEXT NOT NULL,
					version_id TEXT DEFAULT '',
					action TEXT NOT NULL,
					status TEXT NOT NULL DEFAULT 'pending',
					attempts INTEGER NOT NULL DEFAULT 0,
					max_retries INTEGER NOT NULL DEFAULT 3,
					last_error TEXT DEFAULT '',
					scheduled_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					processed_at TIMESTAMP,
					completed_at TIMESTAMP,
					bytes_replicated INTEGER NOT NULL DEFAULT 0,
					FOREIGN KEY (rule_id) REFERENCES replication_rules(id) ON DELETE CASCADE
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_replication_queue_status ON replication_queue(status)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_replication_queue_rule ON replication_queue(rule_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_replication_queue_tenant ON replication_queue(tenant_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_replication_queue_scheduled ON replication_queue(scheduled_at)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_replication_queue_bucket_key ON replication_queue(bucket, object_key)`); err != nil {
				return err
			}

			// Replication status table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS replication_status (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					rule_id TEXT NOT NULL,
					tenant_id TEXT NOT NULL,
					source_bucket TEXT NOT NULL,
					source_key TEXT NOT NULL,
					source_version_id TEXT DEFAULT '',
					destination_bucket TEXT NOT NULL,
					destination_key TEXT NOT NULL,
					status TEXT NOT NULL,
					last_attempt TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					replicated_at TIMESTAMP,
					error_message TEXT DEFAULT '',
					FOREIGN KEY (rule_id) REFERENCES replication_rules(id) ON DELETE CASCADE
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_replication_status_unique ON replication_status(rule_id, source_bucket, source_key, source_version_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_replication_status_tenant ON replication_status(tenant_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_replication_status_status ON replication_status(status)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_replication_status_destination ON replication_status(destination_bucket, destination_key)`); err != nil {
				return err
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	}
}

// migration5_v060_ClusterAndMetrics creates cluster and metrics tables
// Corresponds to MaxIOFS v0.6.0 - Cluster management and performance monitoring
func migration5_v060_ClusterAndMetrics() Migration {
	return Migration{
		Version:     5,
		Description: "v0.6.0 - Add cluster and metrics tables",
		Up: func(tx *sql.Tx) error {
			// Cluster config table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS cluster_config (
					node_id TEXT PRIMARY KEY,
					node_name TEXT NOT NULL,
					cluster_token TEXT NOT NULL,
					is_cluster_enabled INTEGER NOT NULL DEFAULT 0,
					region TEXT DEFAULT '',
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
				)
			`); err != nil {
				return err
			}

			// Cluster nodes table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS cluster_nodes (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					endpoint TEXT NOT NULL,
					node_token TEXT NOT NULL,
					region TEXT DEFAULT '',
					priority INTEGER NOT NULL DEFAULT 100,
					health_status TEXT NOT NULL DEFAULT 'unknown',
					last_health_check TIMESTAMP,
					last_seen TIMESTAMP,
					latency_ms INTEGER NOT NULL DEFAULT 0,
					capacity_total INTEGER NOT NULL DEFAULT 0,
					capacity_used INTEGER NOT NULL DEFAULT 0,
					bucket_count INTEGER NOT NULL DEFAULT 0,
					metadata TEXT DEFAULT '{}',
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_nodes_region ON cluster_nodes(region)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_nodes_health ON cluster_nodes(health_status)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_nodes_priority ON cluster_nodes(priority)`); err != nil {
				return err
			}

			// Cluster health history table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS cluster_health_history (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					node_id TEXT NOT NULL,
					health_status TEXT NOT NULL,
					latency_ms INTEGER DEFAULT 0,
					timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					error_message TEXT DEFAULT '',
					FOREIGN KEY (node_id) REFERENCES cluster_nodes(id) ON DELETE CASCADE
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_health_node ON cluster_health_history(node_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_health_timestamp ON cluster_health_history(timestamp)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_health_status ON cluster_health_history(health_status)`); err != nil {
				return err
			}

			// Cluster migrations table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS cluster_migrations (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					bucket_name TEXT NOT NULL,
					source_node_id TEXT NOT NULL,
					target_node_id TEXT NOT NULL,
					status TEXT NOT NULL DEFAULT 'pending',
					objects_total INTEGER DEFAULT 0,
					objects_migrated INTEGER DEFAULT 0,
					bytes_total INTEGER DEFAULT 0,
					bytes_migrated INTEGER DEFAULT 0,
					delete_source INTEGER NOT NULL DEFAULT 0,
					verify_data INTEGER NOT NULL DEFAULT 1,
					started_at TIMESTAMP,
					completed_at TIMESTAMP,
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					error_message TEXT DEFAULT '',
					FOREIGN KEY (source_node_id) REFERENCES cluster_nodes(id),
					FOREIGN KEY (target_node_id) REFERENCES cluster_nodes(id)
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_migrations_bucket ON cluster_migrations(bucket_name)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_migrations_status ON cluster_migrations(status)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_migrations_source ON cluster_migrations(source_node_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_migrations_target ON cluster_migrations(target_node_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_migrations_created ON cluster_migrations(created_at)`); err != nil {
				return err
			}

			// Metrics tables
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS metric_snapshots (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					timestamp DATETIME NOT NULL,
					type TEXT NOT NULL,
					data TEXT NOT NULL,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_metric_snapshots_timestamp ON metric_snapshots(timestamp)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_metric_snapshots_type ON metric_snapshots(type)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_metric_snapshots_type_timestamp ON metric_snapshots(type, timestamp)`); err != nil {
				return err
			}

			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS metric_aggregates (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					hour DATETIME NOT NULL,
					type TEXT NOT NULL,
					data TEXT NOT NULL,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_metric_aggregates_hour ON metric_aggregates(hour)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_metric_aggregates_type ON metric_aggregates(type)`); err != nil {
				return err
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	}
}

// migration6_v061_ClusterReplication creates cluster replication sync tables
// Corresponds to MaxIOFS v0.6.1 - Cluster data synchronization
func migration6_v061_ClusterReplication() Migration {
	return Migration{
		Version:     6,
		Description: "v0.6.1 - Add cluster replication and sync tables",
		Up: func(tx *sql.Tx) error {
			// Cluster bucket replication table
			if _, err := tx.Exec(`
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
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_bucket_repl_source ON cluster_bucket_replication(source_bucket)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_bucket_repl_dest ON cluster_bucket_replication(destination_node_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_bucket_repl_tenant ON cluster_bucket_replication(tenant_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_bucket_repl_enabled ON cluster_bucket_replication(enabled)`); err != nil {
				return err
			}

			// Cluster replication queue table
			if _, err := tx.Exec(`
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
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_repl_queue_rule ON cluster_replication_queue(replication_rule_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_repl_queue_status ON cluster_replication_queue(status)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_repl_queue_scheduled ON cluster_replication_queue(scheduled_at)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_repl_queue_object ON cluster_replication_queue(source_bucket, object_key)`); err != nil {
				return err
			}

			// Cluster replication status table
			if _, err := tx.Exec(`
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
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_repl_status_rule ON cluster_replication_status(replication_rule_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_repl_status_object ON cluster_replication_status(source_bucket, object_key)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_repl_status_status ON cluster_replication_status(status)`); err != nil {
				return err
			}

			// Cluster tenant sync table
			if _, err := tx.Exec(`
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
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_tenant_sync_tenant ON cluster_tenant_sync(tenant_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_tenant_sync_dest ON cluster_tenant_sync(destination_node_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_tenant_sync_status ON cluster_tenant_sync(status)`); err != nil {
				return err
			}

			// Cluster user sync table
			if _, err := tx.Exec(`
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
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_user_sync_user ON cluster_user_sync(user_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_user_sync_dest ON cluster_user_sync(destination_node_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_user_sync_status ON cluster_user_sync(status)`); err != nil {
				return err
			}

			// Cluster access key sync table
			if _, err := tx.Exec(`
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
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_access_key_sync_key ON cluster_access_key_sync(access_key_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_access_key_sync_dest ON cluster_access_key_sync(destination_node_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_access_key_sync_status ON cluster_access_key_sync(status)`); err != nil {
				return err
			}

			// Cluster bucket permission sync table
			if _, err := tx.Exec(`
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
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_bucket_perm_sync_perm ON cluster_bucket_permission_sync(permission_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_bucket_perm_sync_dest ON cluster_bucket_permission_sync(destination_node_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cluster_bucket_perm_sync_status ON cluster_bucket_permission_sync(status)`); err != nil {
				return err
			}

			// Cluster global config table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS cluster_global_config (
					key TEXT PRIMARY KEY,
					value TEXT NOT NULL,
					description TEXT,
					created_at TIMESTAMP NOT NULL,
					updated_at TIMESTAMP NOT NULL
				)
			`); err != nil {
				return err
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	}
}

// migration7_v062_BucketInventoryAndPermissions creates bucket inventory and permissions tables
// Corresponds to MaxIOFS v0.6.2 - Bucket inventory system and bucket permissions
func migration7_v062_BucketInventoryAndPermissions() Migration {
	return Migration{
		Version:     7,
		Description: "v0.6.2 - Add bucket_permissions and bucket_inventory tables",
		Up: func(tx *sql.Tx) error {
			// Bucket permissions table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS bucket_permissions (
					id TEXT PRIMARY KEY,
					bucket_name TEXT NOT NULL,
					user_id TEXT,
					tenant_id TEXT,
					permission_level TEXT NOT NULL,
					granted_by TEXT NOT NULL,
					granted_at INTEGER NOT NULL,
					expires_at INTEGER,
					FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
					FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
					UNIQUE(bucket_name, user_id),
					UNIQUE(bucket_name, tenant_id)
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_bucket_permissions_bucket ON bucket_permissions(bucket_name)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_bucket_permissions_user ON bucket_permissions(user_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_bucket_permissions_tenant ON bucket_permissions(tenant_id)`); err != nil {
				return err
			}

			// Bucket inventory configs table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS bucket_inventory_configs (
					id TEXT PRIMARY KEY,
					bucket_name TEXT NOT NULL,
					tenant_id TEXT,
					enabled BOOLEAN DEFAULT 1,
					frequency TEXT NOT NULL CHECK(frequency IN ('daily', 'weekly')),
					format TEXT NOT NULL CHECK(format IN ('csv', 'json')),
					destination_bucket TEXT NOT NULL,
					destination_prefix TEXT DEFAULT '',
					included_fields TEXT NOT NULL,
					schedule_time TEXT NOT NULL,
					last_run_at INTEGER,
					next_run_at INTEGER,
					created_at INTEGER NOT NULL,
					updated_at INTEGER NOT NULL,
					FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
					UNIQUE(bucket_name, tenant_id)
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_inventory_configs_bucket ON bucket_inventory_configs(bucket_name)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_inventory_configs_tenant ON bucket_inventory_configs(tenant_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_inventory_configs_enabled ON bucket_inventory_configs(enabled)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_inventory_configs_next_run ON bucket_inventory_configs(next_run_at)`); err != nil {
				return err
			}

			// Bucket inventory reports table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS bucket_inventory_reports (
					id TEXT PRIMARY KEY,
					config_id TEXT NOT NULL,
					bucket_name TEXT NOT NULL,
					report_path TEXT NOT NULL,
					object_count INTEGER DEFAULT 0,
					total_size INTEGER DEFAULT 0,
					status TEXT NOT NULL CHECK(status IN ('pending', 'completed', 'failed')),
					started_at INTEGER,
					completed_at INTEGER,
					error_message TEXT,
					created_at INTEGER NOT NULL,
					FOREIGN KEY (config_id) REFERENCES bucket_inventory_configs(id) ON DELETE CASCADE
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_inventory_reports_config ON bucket_inventory_reports(config_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_inventory_reports_bucket ON bucket_inventory_reports(bucket_name)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_inventory_reports_status ON bucket_inventory_reports(status)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_inventory_reports_created ON bucket_inventory_reports(created_at)`); err != nil {
				return err
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	}
}

// migration8_v062_CurrentSchema ensures all current schema is in place
// This is a safety migration to add any missing pieces from the current schema
func migration8_v062_CurrentSchema() Migration {
	return Migration{
		Version:     8,
		Description: "v0.6.2 - Current schema validation and completion",
		Up: func(tx *sql.Tx) error {
			// This migration ensures all tables from previous migrations exist
			// It's essentially a no-op if all previous migrations ran successfully
			// But it serves as a checkpoint for the current schema version
			return nil
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	}
}

// migration9_v090_IdentityProviders creates IDP tables and adds auth_provider/external_id to users
// Corresponds to MaxIOFS v0.9.0 - Identity Provider (IDP) System
func migration9_v090_IdentityProviders() Migration {
	return Migration{
		Version:     9,
		Description: "v0.9.0 - Add identity provider tables and user auth_provider fields",
		Up: func(tx *sql.Tx) error {
			// Identity providers table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS identity_providers (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					type TEXT NOT NULL,
					tenant_id TEXT,
					status TEXT NOT NULL DEFAULT 'active',
					config TEXT NOT NULL,
					created_by TEXT NOT NULL,
					created_at INTEGER NOT NULL,
					updated_at INTEGER NOT NULL,
					FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_idp_tenant ON identity_providers(tenant_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_idp_type ON identity_providers(type)`); err != nil {
				return err
			}

			// IDP group mappings table
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS idp_group_mappings (
					id TEXT PRIMARY KEY,
					provider_id TEXT NOT NULL,
					external_group TEXT NOT NULL,
					external_group_name TEXT,
					role TEXT NOT NULL,
					tenant_id TEXT,
					auto_sync BOOLEAN DEFAULT 0,
					last_synced_at INTEGER,
					created_at INTEGER NOT NULL,
					updated_at INTEGER NOT NULL,
					FOREIGN KEY (provider_id) REFERENCES identity_providers(id) ON DELETE CASCADE,
					UNIQUE(provider_id, external_group)
				)
			`); err != nil {
				return err
			}

			// Add auth_provider and external_id columns to users table
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN auth_provider TEXT NOT NULL DEFAULT 'local'`); err != nil {
				// Column might already exist, ignore error
			}
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN external_id TEXT`); err != nil {
				// Column might already exist, ignore error
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_users_auth_provider ON users(auth_provider)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_users_external_id ON users(external_id)`); err != nil {
				return err
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	}
}

// migration10_v091_ClusterDeletionLog creates the cluster deletion log table for tombstone-based deletion sync
// Corresponds to MaxIOFS v0.9.1 - Cluster deletion synchronization
func migration10_v091_ClusterDeletionLog() Migration {
	return Migration{
		Version:     10,
		Description: "v0.9.1 - Add cluster_deletion_log table for tombstone-based deletion sync",
		Up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS cluster_deletion_log (
					id TEXT PRIMARY KEY,
					entity_type TEXT NOT NULL,
					entity_id TEXT NOT NULL,
					deleted_by_node_id TEXT NOT NULL,
					deleted_at INTEGER NOT NULL,
					UNIQUE(entity_type, entity_id)
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_deletion_log_type ON cluster_deletion_log(entity_type)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_deletion_log_deleted_at ON cluster_deletion_log(deleted_at)`); err != nil {
				return err
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	}
}
