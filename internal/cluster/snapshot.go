package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// EntityStamp is a minimal representation of a local entity used for LWW comparison
// during reconciliation. UpdatedAt is always a Unix timestamp (seconds).
type EntityStamp struct {
	ID        string `json:"id"`
	UpdatedAt int64  `json:"updated_at"`
}

// StateSnapshot is the full local state of a node at a point in time.
// It is returned by GET /api/internal/cluster/state-snapshot and consumed by
// the StaleReconciler when a node reconnects after a partition or stale period.
type StateSnapshot struct {
	// Identity of the node that produced this snapshot.
	NodeID string `json:"node_id"`

	// Unix timestamp (seconds) at which the snapshot was taken.
	SnapshotAt int64 `json:"snapshot_at"`

	// Per-entity-type lists.  All UpdatedAt values are Unix seconds.
	// AccessKeys uses created_at (no updated_at column exists).
	// BucketPermissions uses granted_at (no updated_at column exists).
	Tenants           []EntityStamp    `json:"tenants"`
	Users             []EntityStamp    `json:"users"`
	AccessKeys        []EntityStamp    `json:"access_keys"`
	BucketPermissions []EntityStamp    `json:"bucket_permissions"`
	IDPProviders      []EntityStamp    `json:"idp_providers"`
	GroupMappings     []EntityStamp    `json:"group_mappings"`

	// All tombstones in the local deletion log.
	Tombstones []*DeletionEntry `json:"tombstones"`
}

// SnapshotAge returns how old the snapshot is relative to now.
func (s *StateSnapshot) SnapshotAge() time.Duration {
	return time.Since(time.Unix(s.SnapshotAt, 0))
}

// BuildLocalSnapshot queries the local database and returns the full state
// snapshot for this node.  Shared by the HTTP handler and the StaleReconciler.
func BuildLocalSnapshot(ctx context.Context, nodeID string, db *sql.DB) (*StateSnapshot, error) {
	snap := &StateSnapshot{
		NodeID:     nodeID,
		SnapshotAt: time.Now().Unix(),
	}

	var err error

	// Tenants — updated_at is a TIMESTAMP column (scanned as time.Time).
	snap.Tenants, err = snapshotTenantStamps(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("snapshot tenants: %w", err)
	}

	// Users — updated_at stored as int64 (Unix seconds).
	snap.Users, err = snapshotInt64Stamps(ctx, db, "SELECT id, updated_at FROM users")
	if err != nil {
		return nil, fmt.Errorf("snapshot users: %w", err)
	}

	// Access keys — no updated_at; use created_at; PK is access_key_id.
	snap.AccessKeys, err = snapshotInt64Stamps(ctx, db, "SELECT access_key_id, created_at FROM access_keys")
	if err != nil {
		return nil, fmt.Errorf("snapshot access keys: %w", err)
	}

	// Bucket permissions — no updated_at; use granted_at.
	snap.BucketPermissions, err = snapshotInt64Stamps(ctx, db, "SELECT id, granted_at FROM bucket_permissions")
	if err != nil {
		return nil, fmt.Errorf("snapshot bucket permissions: %w", err)
	}

	// IDP providers — updated_at stored as int64.
	snap.IDPProviders, err = snapshotInt64Stamps(ctx, db, "SELECT id, updated_at FROM identity_providers")
	if err != nil {
		return nil, fmt.Errorf("snapshot IDP providers: %w", err)
	}

	// Group mappings — updated_at stored as int64.
	snap.GroupMappings, err = snapshotInt64Stamps(ctx, db, "SELECT id, updated_at FROM idp_group_mappings")
	if err != nil {
		return nil, fmt.Errorf("snapshot group mappings: %w", err)
	}

	// All tombstones from the local deletion log.
	snap.Tombstones, err = snapshotTombstones(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("snapshot tombstones: %w", err)
	}

	return snap, nil
}

// snapshotTenantStamps reads (id, updated_at) from the tenants table.
// tenants.updated_at is a TIMESTAMP column that SQLite returns as time.Time.
func snapshotTenantStamps(ctx context.Context, db *sql.DB) ([]EntityStamp, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, updated_at FROM tenants")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stamps []EntityStamp
	for rows.Next() {
		var id string
		var updatedAt time.Time
		if err := rows.Scan(&id, &updatedAt); err != nil {
			return nil, err
		}
		stamps = append(stamps, EntityStamp{ID: id, UpdatedAt: updatedAt.Unix()})
	}
	return stamps, rows.Err()
}

// snapshotInt64Stamps executes a query returning exactly (id TEXT, ts INTEGER)
// and maps each row to an EntityStamp.
func snapshotInt64Stamps(ctx context.Context, db *sql.DB, query string) ([]EntityStamp, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stamps []EntityStamp
	for rows.Next() {
		var id string
		var ts int64
		if err := rows.Scan(&id, &ts); err != nil {
			return nil, err
		}
		stamps = append(stamps, EntityStamp{ID: id, UpdatedAt: ts})
	}
	return stamps, rows.Err()
}

// snapshotTombstones returns every entry in cluster_deletion_log.
func snapshotTombstones(ctx context.Context, db *sql.DB) ([]*DeletionEntry, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, entity_type, entity_id, deleted_by_node_id, deleted_at
		FROM cluster_deletion_log
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*DeletionEntry
	for rows.Next() {
		e := &DeletionEntry{}
		if err := rows.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.DeletedByNodeID, &e.DeletedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
