package cluster

import (
	"database/sql"
)

const Schema = `
-- Cluster configuration for this node
CREATE TABLE IF NOT EXISTS cluster_config (
    node_id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    cluster_token TEXT NOT NULL,
    is_cluster_enabled INTEGER NOT NULL DEFAULT 0,
    region TEXT DEFAULT '',
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
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
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
	return err
}
