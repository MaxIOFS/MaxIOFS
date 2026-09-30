package cluster

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/db/migrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// entitiesDB is a node's database with every table.
func entitiesDB(t *testing.T) *sql.DB {
	t.Helper()
	dir, err := os.MkdirTemp("", "deletion-order-*")
	require.NoError(t, err)
	db, err := sql.Open("sqlite", filepath.Join(dir, "maxiofs.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)")
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
	require.NoError(t, migrations.NewMigrationManager(db, nil).Migrate())
	require.NoError(t, InitSchema(db))
	require.NoError(t, InitReplicationSchema(db))
	return db
}

// A deletion is dated now, or after the last change of what it removes when
// that change is in this second or later.
func TestDeletedAfter(t *testing.T) {
	now := time.Now().Unix()
	assert.InDelta(t, now, DeletedAfter(0), 1)
	assert.InDelta(t, now, DeletedAfter(now-100), 1)
	at := DeletedAfter(now)
	assert.True(t, at == now+1 || at == now+2, "the change is in this second: %d", at-now)
	assert.EqualValues(t, now+501, DeletedAfter(now+500))
}

// An entity and a deletion of it are ordered by time, for every type that
// keeps one; at the same second the entity is kept. A copy another node sends
// is refused only when this node recorded a later deletion.
func TestAnEntityAndItsDeletionAreOrderedByTime(t *testing.T) {
	db := entitiesDB(t)
	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO tenants (id, name, display_name, created_at, updated_at) VALUES ('t1', 't1', 't1', 1, 100)`,
		`INSERT INTO users (id, username, password_hash, tenant_id, status, created_at, updated_at) VALUES ('u1', 'u1', 'x', 't1', 'active', 1, 100)`,
		`INSERT INTO groups (id, name, created_at, updated_at) VALUES ('g1', 'g1', 1, 100)`,
		`INSERT INTO iam_policies (name, arn, path, default_version_id, created_at, updated_at) VALUES ('p1', 'arn:aws:iam:::policy/p1', '/', 'v1', 1, 100)`,
		`INSERT INTO iam_roles (name, arn, path, assume_role_policy, created_at, updated_at) VALUES ('r1', 'arn:aws:iam:::role/r1', '/', '{}', 1, 100)`,
		`INSERT INTO iam_inline_policies (target_type, target_id, name, document, created_at, updated_at) VALUES ('user', 'u1', 'owner-b', '{}', 1, 100)`,
		`INSERT INTO iam_policy_attachments (policy_name, target_type, target_id, attached_at) VALUES ('p1', 'user', 'u1', 100)`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err, stmt)
	}
	for _, e := range [][2]string{
		{EntityTypeTenant, "t1"},
		{EntityTypeUser, "u1"},
		{EntityTypeGroup, "g1"},
		{EntityTypeIAMPolicy, "p1"},
		{EntityTypeIAMRole, "r1"},
		{EntityTypeIAMInlinePolicy, IAMInlinePolicyID("user", "u1", "owner-b")},
		{EntityTypeIAMAttachment, IAMAttachmentID("p1", "user", "u1")},
	} {
		at, ok := EntityUpdatedAt(ctx, db, e[0], e[1])
		require.True(t, ok, e[0])
		assert.EqualValues(t, 100, at, e[0])
		assert.True(t, EntityIsNewerThanTombstone(ctx, db, e[0], e[1], 99), e[0])
		assert.True(t, EntityIsNewerThanTombstone(ctx, db, e[0], e[1], 100), "%s: the same second keeps it", e[0])
		assert.False(t, EntityIsNewerThanTombstone(ctx, db, e[0], e[1], 101), e[0])
	}
	_, ok := EntityUpdatedAt(ctx, db, EntityTypeIAMInlinePolicy, "no-slashes")
	assert.False(t, ok)
	assert.False(t, EntityIsNewerThanTombstone(ctx, db, EntityTypeAccessKey, "k1", 1), "a type without a change time loses")
	assert.False(t, EntityIsNewerThanTombstone(ctx, db, EntityTypeUser, "missing", 1))

	assert.False(t, DeletionSupersedes(ctx, db, EntityTypeUser, "u2", 50), "no deletion recorded")
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "u2", "n", 100))
	assert.True(t, DeletionSupersedes(ctx, db, EntityTypeUser, "u2", 99))
	assert.False(t, DeletionSupersedes(ctx, db, EntityTypeUser, "u2", 100), "a copy changed in the second of the deletion is taken")
	assert.False(t, DeletionSupersedes(ctx, db, EntityTypeUser, "u2", 101))
}

// The deletion log of an earlier release gets its sequence; its object
// deletions and its deletions of IAM entities this node holds are dropped,
// since their times are those of their last reception. It is repaired once.
func TestAnEarlierDeletionLogIsRepaired(t *testing.T) {
	db := entitiesDB(t)
	ctx := context.Background()
	for _, stmt := range []string{
		`DROP TABLE cluster_deletion_log`,
		`CREATE TABLE cluster_deletion_log (id TEXT PRIMARY KEY, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL,
			deleted_by_node_id TEXT NOT NULL, deleted_at INTEGER NOT NULL, UNIQUE(entity_type, entity_id))`,
		`INSERT INTO iam_inline_policies (target_type, target_id, name, document, created_at, updated_at) VALUES ('user', 'u1', 'held', '{}', 1, 1)`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err, stmt)
	}
	for i, e := range [][2]string{
		{EntityTypeObject, ObjectTombstoneID("b", "k")},
		{EntityTypeObjectVersion, ObjectVersionTombstoneID("b", "k", "v1")},
		{EntityTypeIAMInlinePolicy, IAMInlinePolicyID("user", "u1", "held")},
		{EntityTypeIAMInlinePolicy, IAMInlinePolicyID("user", "u1", "gone")},
		{EntityTypeUser, "u9"},
	} {
		_, err := db.Exec(`INSERT INTO cluster_deletion_log (id, entity_type, entity_id, deleted_by_node_id, deleted_at) VALUES (?, ?, ?, 'n', ?)`,
			string(rune('a'+i)), e[0], e[1], 5000)
		require.NoError(t, err)
	}

	require.NoError(t, createClusterDeletionLogTable(ctx, db))
	rows, err := db.Query(`SELECT entity_type, entity_id, seq FROM cluster_deletion_log ORDER BY seq`)
	require.NoError(t, err)
	var kept []string
	var seqs []int64
	for rows.Next() {
		var entityType, entityID string
		var seq int64
		require.NoError(t, rows.Scan(&entityType, &entityID, &seq))
		kept = append(kept, entityType+":"+entityID)
		seqs = append(seqs, seq)
	}
	require.NoError(t, rows.Close())
	assert.Equal(t, []string{
		EntityTypeIAMInlinePolicy + ":" + IAMInlinePolicyID("user", "u1", "gone"),
		EntityTypeUser + ":u9",
	}, kept)
	for _, seq := range seqs {
		assert.Positive(t, seq, "every kept deletion is sent")
	}

	require.NoError(t, RecordDeletion(ctx, db, EntityTypeObject, ObjectTombstoneID("b", "k"), "n", 6000))
	require.NoError(t, createClusterDeletionLogTable(ctx, db))
	assert.EqualValues(t, 6000, DeletionTime(ctx, db, EntityTypeObject, ObjectTombstoneID("b", "k")), "repaired once")
	var delivery int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'cluster_deletion_log_delivery'`).Scan(&delivery))
	assert.Equal(t, 1, delivery)
}

// The IAM entities a node sends carry the time of each deletion it recorded.
func TestIAMPayloadCarriesDeletionTimes(t *testing.T) {
	db := entitiesDB(t)
	ctx := context.Background()
	id := IAMInlinePolicyID("user", "u1", "p")
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeIAMInlinePolicy, id, "n", 4242))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeIAMRole, "r", "n", 4343))
	m := NewIAMSyncManager(db, NewManager(db, "", ""))
	payload, err := m.buildPayload(ctx)
	require.NoError(t, err)
	require.NotNil(t, payload)
	assert.Equal(t, []string{id}, payload.Deletions[EntityTypeIAMInlinePolicy])
	assert.EqualValues(t, 4242, payload.DeletedAt[EntityTypeIAMInlinePolicy][id])
	assert.EqualValues(t, 4343, payload.DeletedAt[EntityTypeIAMRole]["r"])
}
