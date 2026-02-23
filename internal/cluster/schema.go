package cluster

import (
	"database/sql"
	"fmt"
)

const Schema = `
-- Cluster configuration for this node
CREATE TABLE IF NOT EXISTS cluster_config (
    node_id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    cluster_token TEXT NOT NULL,
    is_cluster_enabled INTEGER NOT NULL DEFAULT 0,
    region TEXT DEFAULT '',
    ca_cert TEXT DEFAULT '',
    ca_key TEXT DEFAULT '',
    node_cert TEXT DEFAULT '',
    node_key TEXT DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Cluster nodes (other nodes in the cluster)
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
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_stale BOOLEAN NOT NULL DEFAULT 0,
    last_local_write_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cluster_nodes_region ON cluster_nodes(region);
CREATE INDEX IF NOT EXISTS idx_cluster_nodes_health ON cluster_nodes(health_status);
CREATE INDEX IF NOT EXISTS idx_cluster_nodes_priority ON cluster_nodes(priority);

-- Health check history for monitoring trends
CREATE TABLE IF NOT EXISTS cluster_health_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL,
    health_status TEXT NOT NULL,
    latency_ms INTEGER DEFAULT 0,
    timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    error_message TEXT DEFAULT '',
    FOREIGN KEY (node_id) REFERENCES cluster_nodes(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cluster_health_node ON cluster_health_history(node_id);
CREATE INDEX IF NOT EXISTS idx_cluster_health_timestamp ON cluster_health_history(timestamp);
CREATE INDEX IF NOT EXISTS idx_cluster_health_status ON cluster_health_history(health_status);

-- Bucket migrations between cluster nodes
CREATE TABLE IF NOT EXISTS cluster_migrations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bucket_name TEXT NOT NULL,
    source_node_id TEXT NOT NULL,
    target_node_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',  -- pending, in_progress, completed, failed, cancelled
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
);

CREATE INDEX IF NOT EXISTS idx_cluster_migrations_bucket ON cluster_migrations(bucket_name);
CREATE INDEX IF NOT EXISTS idx_cluster_migrations_status ON cluster_migrations(status);
CREATE INDEX IF NOT EXISTS idx_cluster_migrations_source ON cluster_migrations(source_node_id);
CREATE INDEX IF NOT EXISTS idx_cluster_migrations_target ON cluster_migrations(target_node_id);
CREATE INDEX IF NOT EXISTS idx_cluster_migrations_created ON cluster_migrations(created_at);
`

// InitSchema initializes the cluster database schema
func InitSchema(db *sql.DB) error {
	_, err := db.Exec(Schema)
	if err != nil {
		return err
	}
	if err := applyTLSMigration(db); err != nil {
		return err
	}
	return applyStaleNodeMigration(db)
}

// applyStaleNodeMigration adds stale-node tracking columns to cluster_nodes for existing databases.
func applyStaleNodeMigration(db *sql.DB) error {
	type colDef struct {
		name       string
		definition string
	}
	cols := []colDef{
		{"is_stale", "is_stale BOOLEAN NOT NULL DEFAULT 0"},
		{"last_local_write_at", "last_local_write_at TIMESTAMP"},
	}
	for _, c := range cols {
		var exists bool
		rows, err := db.Query("PRAGMA table_info(cluster_nodes)")
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull int
			var dfltValue sql.NullString
			var pk int
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
				rows.Close()
				return err
			}
			if name == c.name {
				exists = true
				break
			}
		}
		rows.Close()

		if !exists {
			_, err := db.Exec("ALTER TABLE cluster_nodes ADD COLUMN " + c.definition)
			if err != nil {
				return fmt.Errorf("failed to add column %s: %w", c.name, err)
			}
		}
	}
	return nil
}

// applyTLSMigration adds TLS certificate columns to cluster_config for existing databases.
func applyTLSMigration(db *sql.DB) error {
	columns := []string{"ca_cert", "ca_key", "node_cert", "node_key"}
	for _, col := range columns {
		// SQLite: ALTER TABLE ADD COLUMN is a no-op if column already exists when using IF NOT EXISTS (3.35+),
		// but for broader compatibility we check pragma first.
		var exists bool
		rows, err := db.Query("PRAGMA table_info(cluster_config)")
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull int
			var dfltValue sql.NullString
			var pk int
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
				rows.Close()
				return err
			}
			if name == col {
				exists = true
				break
			}
		}
		rows.Close()

		if !exists {
			_, err := db.Exec("ALTER TABLE cluster_config ADD COLUMN " + col + " TEXT DEFAULT ''")
			if err != nil {
				return fmt.Errorf("failed to add column %s: %w", col, err)
			}
		}
	}
	return nil
}
