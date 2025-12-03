package replication

import "database/sql"

const Schema = `
-- Replication rules define how objects should be replicated
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
);

CREATE INDEX IF NOT EXISTS idx_replication_rules_tenant ON replication_rules(tenant_id);
CREATE INDEX IF NOT EXISTS idx_replication_rules_source ON replication_rules(source_bucket);
CREATE INDEX IF NOT EXISTS idx_replication_rules_enabled ON replication_rules(enabled);

-- Replication queue holds pending replication tasks
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
);

CREATE INDEX IF NOT EXISTS idx_replication_queue_status ON replication_queue(status);
CREATE INDEX IF NOT EXISTS idx_replication_queue_rule ON replication_queue(rule_id);
CREATE INDEX IF NOT EXISTS idx_replication_queue_tenant ON replication_queue(tenant_id);
CREATE INDEX IF NOT EXISTS idx_replication_queue_scheduled ON replication_queue(scheduled_at);
CREATE INDEX IF NOT EXISTS idx_replication_queue_bucket_key ON replication_queue(bucket, object_key);

-- Replication status tracks the state of replicated objects
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
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_replication_status_unique ON replication_status(rule_id, source_bucket, source_key, source_version_id);
CREATE INDEX IF NOT EXISTS idx_replication_status_tenant ON replication_status(tenant_id);
CREATE INDEX IF NOT EXISTS idx_replication_status_status ON replication_status(status);
CREATE INDEX IF NOT EXISTS idx_replication_status_destination ON replication_status(destination_bucket, destination_key);
`

// InitSchema initializes the replication database schema
func InitSchema(db *sql.DB) error {
	_, err := db.Exec(Schema)
	return err
}
