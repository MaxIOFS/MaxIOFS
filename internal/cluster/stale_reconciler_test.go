package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

// setupReconcilerDB creates a test database with the cluster schema, all entity
// tables, and a seeded local node.  It returns the db, manager, reconciler, and
// a cleanup function.
func setupReconcilerDB(t *testing.T) (*sql.DB, *Manager, *StaleReconciler, func()) {
	t.Helper()
	db, cleanup := setupTestDB(t)
	ctx := context.Background()

	require.NoError(t, InitReplicationSchema(db), "init replication schema")
	require.NoError(t, createEntityTablesForReconciler(ctx, db), "create entity tables")

	// Local cluster config
	_, err := db.ExecContext(ctx, `
		INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
		VALUES ('local-node', 'Local', 'test-token', 1)
	`)
	require.NoError(t, err)

	// Local cluster node — not stale by default
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status, is_stale)
		VALUES ('local-node', 'Local', 'http://localhost:8081', 'test-token', 'healthy', 0)
	`)
	require.NoError(t, err)

	mgr := NewManager(db, "http://localhost:8081")
	r := NewStaleReconciler(db, mgr)
	return db, mgr, r, cleanup
}

// createEntityTablesForReconciler creates the entity tables that live outside the
// cluster schema but are queried by BuildLocalSnapshot, EntityIsNewerThanTombstone,
// and the per-entity push methods.
func createEntityTablesForReconciler(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		// tenants — updated_at is a TIMESTAMP column (time.Time when scanned)
		`CREATE TABLE IF NOT EXISTS tenants (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			display_name TEXT DEFAULT '',
			description TEXT DEFAULT '',
			status TEXT DEFAULT 'active',
			max_access_keys INTEGER DEFAULT 5,
			max_storage_bytes INTEGER DEFAULT 0,
			current_storage_bytes INTEGER DEFAULT 0,
			max_buckets INTEGER DEFAULT 10,
			current_buckets INTEGER DEFAULT 0,
			metadata TEXT DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		// users — updated_at stored as INTEGER (Unix seconds)
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			display_name TEXT DEFAULT '',
			email TEXT DEFAULT '',
			status TEXT DEFAULT 'active',
			tenant_id TEXT,
			roles TEXT DEFAULT '',
			policies TEXT DEFAULT '',
			metadata TEXT DEFAULT '{}',
			failed_login_attempts INTEGER DEFAULT 0,
			locked_until INTEGER DEFAULT 0,
			last_failed_login INTEGER DEFAULT 0,
			theme_preference TEXT DEFAULT 'light',
			language_preference TEXT DEFAULT 'en',
			auth_provider TEXT DEFAULT 'local',
			external_id TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		// access_keys — no updated_at; created_at used as timestamp proxy in snapshots
		`CREATE TABLE IF NOT EXISTS access_keys (
			access_key_id TEXT PRIMARY KEY,
			secret_access_key TEXT NOT NULL,
			user_id TEXT NOT NULL,
			status TEXT DEFAULT 'active',
			created_at INTEGER NOT NULL,
			last_used INTEGER
		)`,
		// bucket_permissions — no updated_at; granted_at used as timestamp proxy
		`CREATE TABLE IF NOT EXISTS bucket_permissions (
			id TEXT PRIMARY KEY,
			bucket_name TEXT NOT NULL,
			user_id TEXT,
			tenant_id TEXT,
			permission_level TEXT NOT NULL,
			granted_by TEXT NOT NULL,
			granted_at INTEGER NOT NULL,
			expires_at INTEGER
		)`,
		// identity_providers — updated_at as INTEGER
		`CREATE TABLE IF NOT EXISTS identity_providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			tenant_id TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			config TEXT NOT NULL,
			created_by TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		// idp_group_mappings — updated_at as INTEGER
		`CREATE TABLE IF NOT EXISTS idp_group_mappings (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL,
			external_group TEXT NOT NULL,
			external_group_name TEXT,
			role TEXT NOT NULL,
			tenant_id TEXT,
			auto_sync BOOLEAN DEFAULT 0,
			last_synced_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// addRemoteNode inserts a healthy cluster node pointing at the given URL.
func addRemoteNode(t *testing.T, ctx context.Context, db *sql.DB, id, url string) {
	t.Helper()
	_, err := db.ExecContext(ctx,
		`INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status)
		 VALUES (?, ?, ?, 'remote-token', 'healthy')`,
		id, id, url)
	require.NoError(t, err)
}

// markLocalNodeStale sets is_stale and optionally last_local_write_at on the
// local node so Reconcile enters the desired mode.
func markLocalNodeStale(t *testing.T, ctx context.Context, db *sql.DB, writeAt *time.Time) {
	t.Helper()
	if writeAt == nil {
		_, err := db.ExecContext(ctx,
			`UPDATE cluster_nodes SET is_stale = 1, last_local_write_at = NULL WHERE id = 'local-node'`)
		require.NoError(t, err)
	} else {
		_, err := db.ExecContext(ctx,
			`UPDATE cluster_nodes SET is_stale = 1, last_local_write_at = ? WHERE id = 'local-node'`,
			*writeAt)
		require.NoError(t, err)
	}
}

// emptySnapshotServer returns an httptest.Server that responds to state-snapshot
// with an empty snapshot and to all other POSTs with HTTP 200.
func emptySnapshotServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/internal/cluster/state-snapshot" {
			snap := StateSnapshot{NodeID: "remote-node", SnapshotAt: time.Now().Unix()}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(snap)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
}

// ── newerStamps (pure function) ───────────────────────────────────────────────

func TestNewerStamps(t *testing.T) {
	t.Run("entity_absent_from_remote_is_included", func(t *testing.T) {
		local := []EntityStamp{{ID: "a", UpdatedAt: 100}}
		result := newerStamps(local, map[string]int64{})
		require.Len(t, result, 1)
		assert.Equal(t, "a", result[0].ID)
	})

	t.Run("local_strictly_newer_than_remote_is_included", func(t *testing.T) {
		local := []EntityStamp{{ID: "a", UpdatedAt: 200}}
		result := newerStamps(local, map[string]int64{"a": 100})
		require.Len(t, result, 1)
		assert.Equal(t, "a", result[0].ID)
	})

	t.Run("local_equal_to_remote_is_skipped", func(t *testing.T) {
		local := []EntityStamp{{ID: "a", UpdatedAt: 100}}
		result := newerStamps(local, map[string]int64{"a": 100})
		assert.Empty(t, result)
	})

	t.Run("local_older_than_remote_is_skipped", func(t *testing.T) {
		local := []EntityStamp{{ID: "a", UpdatedAt: 50}}
		result := newerStamps(local, map[string]int64{"a": 100})
		assert.Empty(t, result)
	})

	t.Run("mixed_batch_filters_correctly", func(t *testing.T) {
		local := []EntityStamp{
			{ID: "newer", UpdatedAt: 200},
			{ID: "equal", UpdatedAt: 100},
			{ID: "older", UpdatedAt: 50},
			{ID: "missing", UpdatedAt: 1},
		}
		remoteIdx := map[string]int64{
			"newer": 100,
			"equal": 100,
			"older": 200,
			// "missing" intentionally absent
		}
		result := newerStamps(local, remoteIdx)
		require.Len(t, result, 2)
		ids := map[string]bool{}
		for _, s := range result {
			ids[s.ID] = true
		}
		assert.True(t, ids["newer"])
		assert.True(t, ids["missing"])
		assert.False(t, ids["equal"])
		assert.False(t, ids["older"])
	})

	t.Run("empty_local_returns_empty", func(t *testing.T) {
		result := newerStamps(nil, map[string]int64{"a": 100})
		assert.Empty(t, result)
	})
}

func TestBuildStampIndex(t *testing.T) {
	snap := &StateSnapshot{
		Tenants:           []EntityStamp{{ID: "t1", UpdatedAt: 10}},
		Users:             []EntityStamp{{ID: "u1", UpdatedAt: 20}},
		AccessKeys:        []EntityStamp{{ID: "ak1", UpdatedAt: 30}},
		BucketPermissions: []EntityStamp{{ID: "bp1", UpdatedAt: 40}},
		IDPProviders:      []EntityStamp{{ID: "idp1", UpdatedAt: 50}},
		GroupMappings:     []EntityStamp{{ID: "gm1", UpdatedAt: 60}},
	}
	idx := buildStampIndex(snap)

	assert.Equal(t, int64(10), idx["tenants"]["t1"])
	assert.Equal(t, int64(20), idx["users"]["u1"])
	assert.Equal(t, int64(30), idx["access_keys"]["ak1"])
	assert.Equal(t, int64(40), idx["bucket_permissions"]["bp1"])
	assert.Equal(t, int64(50), idx["idp_providers"]["idp1"])
	assert.Equal(t, int64(60), idx["group_mappings"]["gm1"])
	// Absent key returns zero value
	assert.Equal(t, int64(0), idx["tenants"]["nonexistent"])
}

// ── BuildLocalSnapshot ────────────────────────────────────────────────────────

func TestBuildLocalSnapshot_Empty(t *testing.T) {
	db, _, _, cleanup := setupReconcilerDB(t)
	defer cleanup()

	ctx := context.Background()
	snap, err := BuildLocalSnapshot(ctx, "local-node", db)
	require.NoError(t, err)

	assert.Equal(t, "local-node", snap.NodeID)
	assert.Empty(t, snap.Tenants)
	assert.Empty(t, snap.Users)
	assert.Empty(t, snap.AccessKeys)
	assert.Empty(t, snap.BucketPermissions)
	assert.Empty(t, snap.IDPProviders)
	assert.Empty(t, snap.GroupMappings)
	assert.Empty(t, snap.Tombstones)
}

func TestBuildLocalSnapshot_WithEntities(t *testing.T) {
	db, _, _, cleanup := setupReconcilerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	nowUnix := now.Unix()

	// One of each entity type
	_, err := db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, status, created_at, updated_at) VALUES ('t1', 'Tenant1', 'active', ?, ?)`,
		now, now)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES ('u1', 'alice', 'hash', ?, ?)`,
		nowUnix, nowUnix)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO access_keys (access_key_id, secret_access_key, user_id, status, created_at) VALUES ('ak1', 'secret', 'u1', 'active', ?)`,
		nowUnix)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO bucket_permissions (id, bucket_name, permission_level, granted_by, granted_at) VALUES ('bp1', 'my-bucket', 'read', 'u1', ?)`,
		nowUnix)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO identity_providers (id, name, type, status, config, created_by, created_at, updated_at) VALUES ('idp1', 'LDAP', 'ldap', 'active', '{}', 'admin', ?, ?)`,
		nowUnix, nowUnix)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO idp_group_mappings (id, provider_id, external_group, role, created_at, updated_at) VALUES ('gm1', 'idp1', 'cn=admins', 'admin', ?, ?)`,
		nowUnix, nowUnix)
	require.NoError(t, err)

	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "u-deleted", "node-1"))

	snap, err := BuildLocalSnapshot(ctx, "local-node", db)
	require.NoError(t, err)

	require.Len(t, snap.Tenants, 1)
	assert.Equal(t, "t1", snap.Tenants[0].ID)
	assert.Equal(t, nowUnix, snap.Tenants[0].UpdatedAt)

	require.Len(t, snap.Users, 1)
	assert.Equal(t, "u1", snap.Users[0].ID)
	assert.Equal(t, nowUnix, snap.Users[0].UpdatedAt)

	require.Len(t, snap.AccessKeys, 1)
	assert.Equal(t, "ak1", snap.AccessKeys[0].ID)
	assert.Equal(t, nowUnix, snap.AccessKeys[0].UpdatedAt) // created_at used as proxy

	require.Len(t, snap.BucketPermissions, 1)
	assert.Equal(t, "bp1", snap.BucketPermissions[0].ID)
	assert.Equal(t, nowUnix, snap.BucketPermissions[0].UpdatedAt) // granted_at used as proxy

	require.Len(t, snap.IDPProviders, 1)
	assert.Equal(t, "idp1", snap.IDPProviders[0].ID)
	assert.Equal(t, nowUnix, snap.IDPProviders[0].UpdatedAt)

	require.Len(t, snap.GroupMappings, 1)
	assert.Equal(t, "gm1", snap.GroupMappings[0].ID)
	assert.Equal(t, nowUnix, snap.GroupMappings[0].UpdatedAt)

	require.Len(t, snap.Tombstones, 1)
	assert.Equal(t, EntityTypeUser, snap.Tombstones[0].EntityType)
	assert.Equal(t, "u-deleted", snap.Tombstones[0].EntityID)
}

// ── EntityIsNewerThanTombstone ────────────────────────────────────────────────

func TestEntityIsNewerThanTombstone(t *testing.T) {
	db, _, _, cleanup := setupReconcilerDB(t)
	defer cleanup()

	ctx := context.Background()
	past := time.Now().Add(-24 * time.Hour)
	future := time.Now().Add(24 * time.Hour)
	pastUnix := past.Unix()
	futureUnix := future.Unix()
	tombstoneAt := time.Now().Unix() // tombstone is "now"

	// Tenants (TIMESTAMP column)
	_, err := db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, status, created_at, updated_at) VALUES ('t-old', 'Old', 'active', ?, ?)`,
		past, past)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, status, created_at, updated_at) VALUES ('t-new', 'New', 'active', ?, ?)`,
		future, future)
	require.NoError(t, err)

	// Users (INTEGER column)
	nowUnix := time.Now().Unix()
	_, err = db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES ('u-old', 'u-old', 'h', ?, ?)`,
		nowUnix, pastUnix)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES ('u-new', 'u-new', 'h', ?, ?)`,
		nowUnix, futureUnix)
	require.NoError(t, err)

	// IDP providers
	_, err = db.ExecContext(ctx,
		`INSERT INTO identity_providers (id, name, type, status, config, created_by, created_at, updated_at) VALUES ('idp-old', 'L', 'ldap', 'active', '{}', 'a', ?, ?)`,
		nowUnix, pastUnix)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO identity_providers (id, name, type, status, config, created_by, created_at, updated_at) VALUES ('idp-new', 'L2', 'ldap', 'active', '{}', 'a', ?, ?)`,
		nowUnix, futureUnix)
	require.NoError(t, err)

	// Group mappings
	_, err = db.ExecContext(ctx,
		`INSERT INTO idp_group_mappings (id, provider_id, external_group, role, created_at, updated_at) VALUES ('gm-old', 'idp-old', 'g', 'admin', ?, ?)`,
		nowUnix, pastUnix)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO idp_group_mappings (id, provider_id, external_group, role, created_at, updated_at) VALUES ('gm-new', 'idp-old', 'g2', 'admin', ?, ?)`,
		nowUnix, futureUnix)
	require.NoError(t, err)

	// Access key (even a "future" created_at → tombstone still wins)
	_, err = db.ExecContext(ctx,
		`INSERT INTO access_keys (access_key_id, secret_access_key, user_id, created_at) VALUES ('ak-future', 's', 'u-old', ?)`,
		futureUnix)
	require.NoError(t, err)

	tests := []struct {
		name       string
		entityType string
		entityID   string
		wantNewer  bool
	}{
		{"tenant_older_than_tombstone", EntityTypeTenant, "t-old", false},
		{"tenant_newer_than_tombstone", EntityTypeTenant, "t-new", true},
		{"user_older_than_tombstone", EntityTypeUser, "u-old", false},
		{"user_newer_than_tombstone", EntityTypeUser, "u-new", true},
		{"idp_provider_older_than_tombstone", EntityTypeIDPProvider, "idp-old", false},
		{"idp_provider_newer_than_tombstone", EntityTypeIDPProvider, "idp-new", true},
		{"group_mapping_older_than_tombstone", EntityTypeGroupMapping, "gm-old", false},
		{"group_mapping_newer_than_tombstone", EntityTypeGroupMapping, "gm-new", true},
		// AccessKey and BucketPermission always return false (no updated_at column)
		{"access_key_always_false", EntityTypeAccessKey, "ak-future", false},
		{"bucket_permission_always_false", EntityTypeBucketPermission, "bp-nonexistent", false},
		// Non-existent entity
		{"entity_not_found_returns_false", EntityTypeUser, "ghost", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EntityIsNewerThanTombstone(ctx, db, tc.entityType, tc.entityID, tombstoneAt)
			assert.Equal(t, tc.wantNewer, got)
		})
	}
}

// ── detectMode ────────────────────────────────────────────────────────────────

func TestDetectMode(t *testing.T) {
	t.Run("offline_when_last_local_write_at_is_null", func(t *testing.T) {
		_, _, r, cleanup := setupReconcilerDB(t)
		defer cleanup()

		ctx := context.Background()
		// local-node was seeded with last_local_write_at = NULL
		mode, err := r.detectMode(ctx, "local-node")
		require.NoError(t, err)
		assert.Equal(t, ModeOffline, mode)
	})

	t.Run("partition_when_last_local_write_at_is_set", func(t *testing.T) {
		db, _, r, cleanup := setupReconcilerDB(t)
		defer cleanup()

		ctx := context.Background()
		_, err := db.ExecContext(ctx,
			`UPDATE cluster_nodes SET last_local_write_at = ? WHERE id = 'local-node'`,
			time.Now())
		require.NoError(t, err)

		mode, err := r.detectMode(ctx, "local-node")
		require.NoError(t, err)
		assert.Equal(t, ModePartition, mode)
	})
}

// ── applyRemoteTombstones ─────────────────────────────────────────────────────

func TestApplyRemoteTombstones(t *testing.T) {
	t.Run("new_tombstone_is_recorded", func(t *testing.T) {
		db, _, r, cleanup := setupReconcilerDB(t)
		defer cleanup()
		ctx := context.Background()

		remote := []*DeletionEntry{{
			EntityType: EntityTypeUser, EntityID: "u1",
			DeletedByNodeID: "node-2", DeletedAt: time.Now().Unix(),
		}}
		require.NoError(t, r.applyRemoteTombstones(ctx, remote, nil, "local-node"))

		found, err := HasDeletion(ctx, db, EntityTypeUser, "u1")
		require.NoError(t, err)
		assert.True(t, found, "tombstone should have been recorded")
	})

	t.Run("skips_when_local_tombstone_is_equal_or_newer", func(t *testing.T) {
		db, _, r, cleanup := setupReconcilerDB(t)
		defer cleanup()
		ctx := context.Background()

		deletedAt := time.Now().Unix()
		require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "u1", "local-node"))
		local, err := ListDeletions(ctx, db, EntityTypeUser)
		require.NoError(t, err)

		// Remote tombstone has same deleted_at
		remote := []*DeletionEntry{{
			EntityType: EntityTypeUser, EntityID: "u1",
			DeletedByNodeID: "node-2", DeletedAt: deletedAt,
		}}
		require.NoError(t, r.applyRemoteTombstones(ctx, remote, local, "local-node"))

		// Still exactly one tombstone (the original local one)
		entries, err := ListDeletions(ctx, db, EntityTypeUser)
		require.NoError(t, err)
		assert.Len(t, entries, 1)
	})

	t.Run("skips_when_entity_is_newer_than_tombstone", func(t *testing.T) {
		db, _, r, cleanup := setupReconcilerDB(t)
		defer cleanup()
		ctx := context.Background()

		// Insert a user whose updated_at is in the future
		futureUnix := time.Now().Add(time.Hour).Unix()
		nowUnix := time.Now().Unix()
		_, err := db.ExecContext(ctx,
			`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES ('u1', 'u1', 'h', ?, ?)`,
			nowUnix, futureUnix)
		require.NoError(t, err)

		// Tombstone is older than the user's updated_at (entity wins)
		oldTombstoneAt := time.Now().Add(-time.Hour).Unix()
		remote := []*DeletionEntry{{
			EntityType: EntityTypeUser, EntityID: "u1",
			DeletedByNodeID: "node-2", DeletedAt: oldTombstoneAt,
		}}
		require.NoError(t, r.applyRemoteTombstones(ctx, remote, nil, "local-node"))

		found, err := HasDeletion(ctx, db, EntityTypeUser, "u1")
		require.NoError(t, err)
		assert.False(t, found, "tombstone should NOT have been recorded — entity wins LWW")
	})

	t.Run("applies_multiple_tombstones_and_skips_entity_winner", func(t *testing.T) {
		db, _, r, cleanup := setupReconcilerDB(t)
		defer cleanup()
		ctx := context.Background()

		nowUnix := time.Now().Unix()
		futureUnix := time.Now().Add(time.Hour).Unix()
		oldAt := time.Now().Add(-time.Hour).Unix()

		// u-tombstone-wins: no local entity → tombstone should be recorded
		// u-entity-wins: entity updated_at is in the future → tombstone discarded
		_, err := db.ExecContext(ctx,
			`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES ('u-entity-wins', 'u-ew', 'h', ?, ?)`,
			nowUnix, futureUnix)
		require.NoError(t, err)

		remote := []*DeletionEntry{
			{EntityType: EntityTypeUser, EntityID: "u-tombstone-wins", DeletedByNodeID: "n2", DeletedAt: nowUnix},
			{EntityType: EntityTypeUser, EntityID: "u-entity-wins", DeletedByNodeID: "n2", DeletedAt: oldAt},
		}
		require.NoError(t, r.applyRemoteTombstones(ctx, remote, nil, "local-node"))

		tombstoneFound, err := HasDeletion(ctx, db, EntityTypeUser, "u-tombstone-wins")
		require.NoError(t, err)
		assert.True(t, tombstoneFound)

		entityWinsFound, err := HasDeletion(ctx, db, EntityTypeUser, "u-entity-wins")
		require.NoError(t, err)
		assert.False(t, entityWinsFound)
	})
}

// ── Reconcile ─────────────────────────────────────────────────────────────────

func TestReconcile_SkipsWhenNotStale(t *testing.T) {
	_, _, r, cleanup := setupReconcilerDB(t)
	defer cleanup()

	ctx := context.Background()
	// local-node has is_stale = 0 — Reconcile should return nil without doing anything
	err := r.Reconcile(ctx)
	require.NoError(t, err)
}

func TestReconcile_SkipsWhenNoPeers(t *testing.T) {
	db, _, r, cleanup := setupReconcilerDB(t)
	defer cleanup()
	ctx := context.Background()

	// Mark stale but add no remote nodes
	markLocalNodeStale(t, ctx, db, nil)

	err := r.Reconcile(ctx)
	require.NoError(t, err)

	// is_stale must remain 1 — cannot reconcile without peers
	var isStale bool
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT is_stale FROM cluster_nodes WHERE id = 'local-node'`).Scan(&isStale))
	assert.True(t, isStale, "is_stale should remain set when no peers are available")
}

func TestReconcile_ClearsStaleFlag(t *testing.T) {
	db, _, r, cleanup := setupReconcilerDB(t)
	defer cleanup()
	ctx := context.Background()

	peer := emptySnapshotServer(t)
	defer peer.Close()

	// ModeOffline (no last_local_write_at)
	markLocalNodeStale(t, ctx, db, nil)
	addRemoteNode(t, ctx, db, "remote-node", peer.URL)

	require.NoError(t, r.Reconcile(ctx))

	var isStale bool
	var lastWriteAt sql.NullTime
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT is_stale, last_local_write_at FROM cluster_nodes WHERE id = 'local-node'`).
		Scan(&isStale, &lastWriteAt))

	assert.False(t, isStale, "is_stale should be cleared after reconciliation")
	assert.False(t, lastWriteAt.Valid, "last_local_write_at should be NULL after reconciliation")
}

func TestReconcile_ModeOffline_FetchesSnapshot(t *testing.T) {
	db, _, r, cleanup := setupReconcilerDB(t)
	defer cleanup()
	ctx := context.Background()

	snapshotFetched := false
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/internal/cluster/state-snapshot" {
			snapshotFetched = true
			snap := StateSnapshot{NodeID: "remote-node", SnapshotAt: time.Now().Unix()}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(snap)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer peer.Close()

	markLocalNodeStale(t, ctx, db, nil) // ModeOffline
	addRemoteNode(t, ctx, db, "remote-node", peer.URL)

	require.NoError(t, r.Reconcile(ctx))
	assert.True(t, snapshotFetched, "state-snapshot endpoint should have been fetched from peer")
}

func TestReconcile_ModeOffline_DoesNotPushEntities(t *testing.T) {
	db, _, r, cleanup := setupReconcilerDB(t)
	defer cleanup()
	ctx := context.Background()

	// Insert a local tenant so there's something that *could* be pushed
	now := time.Now()
	_, err := db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, status, created_at, updated_at) VALUES ('t1', 'T1', 'active', ?, ?)`,
		now, now)
	require.NoError(t, err)

	var mu sync.Mutex
	entityPushCalls := 0
	entityPushPaths := map[string]struct{}{
		"/api/internal/cluster/tenant-sync":           {},
		"/api/internal/cluster/user-sync":             {},
		"/api/internal/cluster/access-key-sync":       {},
		"/api/internal/cluster/bucket-permission-sync": {},
		"/api/internal/cluster/idp-provider-sync":     {},
		"/api/internal/cluster/group-mapping-sync":    {},
	}

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/internal/cluster/state-snapshot" {
			snap := StateSnapshot{NodeID: "remote-node", SnapshotAt: time.Now().Unix()}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(snap)
			return
		}
		mu.Lock()
		if _, ok := entityPushPaths[req.URL.Path]; ok {
			entityPushCalls++
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer peer.Close()

	markLocalNodeStale(t, ctx, db, nil) // ModeOffline — no last_local_write_at
	addRemoteNode(t, ctx, db, "remote-node", peer.URL)

	require.NoError(t, r.Reconcile(ctx))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 0, entityPushCalls,
		"ModeOffline must not push entities to the peer (peer is authoritative)")
}

func TestReconcile_ModePartition_PushesLocallyNewerEntities(t *testing.T) {
	db, _, r, cleanup := setupReconcilerDB(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	nowUnix := now.Unix()

	// Insert a local user that does not exist on the remote
	_, err := db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES ('u1', 'alice', 'hash', ?, ?)`,
		nowUnix, nowUnix)
	require.NoError(t, err)

	var mu sync.Mutex
	userSyncCalls := 0

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/internal/cluster/state-snapshot" {
			// Remote has no users → local u1 is "newer" (absent on remote)
			snap := StateSnapshot{NodeID: "remote-node", SnapshotAt: time.Now().Unix()}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(snap)
			return
		}
		if req.URL.Path == "/api/internal/cluster/user-sync" {
			mu.Lock()
			userSyncCalls++
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer peer.Close()

	writeAt := time.Now()
	markLocalNodeStale(t, ctx, db, &writeAt) // ModePartition
	addRemoteNode(t, ctx, db, "remote-node", peer.URL)

	require.NoError(t, r.Reconcile(ctx))

	mu.Lock()
	defer mu.Unlock()
	assert.GreaterOrEqual(t, userSyncCalls, 1,
		"ModePartition should push locally-newer user to peer")
}

func TestReconcile_ModePartition_SkipsRemoteNewerEntities(t *testing.T) {
	db, _, r, cleanup := setupReconcilerDB(t)
	defer cleanup()
	ctx := context.Background()

	nowUnix := time.Now().Unix()
	futureUnix := time.Now().Add(time.Hour).Unix()

	// Local user updated_at = now; remote user updated_at = future (peer is newer)
	_, err := db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES ('u1', 'alice', 'hash', ?, ?)`,
		nowUnix, nowUnix)
	require.NoError(t, err)

	var mu sync.Mutex
	userSyncCalls := 0

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/internal/cluster/state-snapshot" {
			// Remote u1 has a newer updated_at than local
			snap := StateSnapshot{
				NodeID:     "remote-node",
				SnapshotAt: time.Now().Unix(),
				Users:      []EntityStamp{{ID: "u1", UpdatedAt: futureUnix}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(snap)
			return
		}
		if req.URL.Path == "/api/internal/cluster/user-sync" {
			mu.Lock()
			userSyncCalls++
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer peer.Close()

	writeAt := time.Now()
	markLocalNodeStale(t, ctx, db, &writeAt) // ModePartition
	addRemoteNode(t, ctx, db, "remote-node", peer.URL)

	require.NoError(t, r.Reconcile(ctx))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 0, userSyncCalls,
		"should NOT push user when remote is strictly newer (LWW: peer wins)")
}

func TestReconcile_PushesLocalTombstonesToPeer(t *testing.T) {
	db, _, r, cleanup := setupReconcilerDB(t)
	defer cleanup()
	ctx := context.Background()

	// Record a local tombstone
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "deleted-user", "local-node"))

	var mu sync.Mutex
	var receivedDeletions []*DeletionEntry

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/internal/cluster/state-snapshot" {
			snap := StateSnapshot{NodeID: "remote-node", SnapshotAt: time.Now().Unix()}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(snap)
			return
		}
		if req.URL.Path == "/api/internal/cluster/deletion-log-sync" {
			var entries []*DeletionEntry
			_ = json.NewDecoder(req.Body).Decode(&entries)
			mu.Lock()
			receivedDeletions = append(receivedDeletions, entries...)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer peer.Close()

	markLocalNodeStale(t, ctx, db, nil) // ModeOffline
	addRemoteNode(t, ctx, db, "remote-node", peer.URL)

	require.NoError(t, r.Reconcile(ctx))

	mu.Lock()
	defer mu.Unlock()

	require.NotEmpty(t, receivedDeletions, "local tombstone should have been pushed to peer")
	found := false
	for _, e := range receivedDeletions {
		if e.EntityType == EntityTypeUser && e.EntityID == "deleted-user" {
			found = true
			break
		}
	}
	assert.True(t, found, "deleted-user tombstone should be in pushed tombstones")
}

func TestReconcile_AppliesRemoteTombstonesLocally(t *testing.T) {
	db, _, r, cleanup := setupReconcilerDB(t)
	defer cleanup()
	ctx := context.Background()

	remoteDeletion := &DeletionEntry{
		EntityType:      EntityTypeTenant,
		EntityID:        "t-remote-deleted",
		DeletedByNodeID: "remote-node",
		DeletedAt:       time.Now().Unix(),
	}

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/internal/cluster/state-snapshot" {
			snap := StateSnapshot{
				NodeID:     "remote-node",
				SnapshotAt: time.Now().Unix(),
				Tombstones: []*DeletionEntry{remoteDeletion},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(snap)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer peer.Close()

	markLocalNodeStale(t, ctx, db, nil) // ModeOffline
	addRemoteNode(t, ctx, db, "remote-node", peer.URL)

	// Verify tombstone is not present before reconciliation
	found, err := HasDeletion(ctx, db, EntityTypeTenant, "t-remote-deleted")
	require.NoError(t, err)
	assert.False(t, found, "tombstone should not exist before reconciliation")

	require.NoError(t, r.Reconcile(ctx))

	// After reconciliation the remote tombstone should be recorded locally
	found, err = HasDeletion(ctx, db, EntityTypeTenant, "t-remote-deleted")
	require.NoError(t, err)
	assert.True(t, found, "remote tombstone should have been applied locally")
}
