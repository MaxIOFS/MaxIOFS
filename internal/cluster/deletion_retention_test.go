package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deletionsHeld(t *testing.T, mgr *Manager) []string {
	t.Helper()
	rows, err := mgr.db.Query(`SELECT entity_id FROM cluster_deletion_log ORDER BY seq`)
	require.NoError(t, err)
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	return ids
}

func delivered(t *testing.T, mgr *Manager, nodeID string, seq int64) {
	t.Helper()
	_, err := mgr.db.Exec(`INSERT INTO cluster_deletion_log_delivery (node_id, delivered_seq) VALUES (?, ?)
		ON CONFLICT(node_id) DO UPDATE SET delivered_seq = excluded.delivered_seq`, nodeID, seq)
	require.NoError(t, err)
}

// A deletion older than the retention is forgotten only once every member of
// the cluster has it: it was sent every other node and no catch-up that sends
// it is pending. A node removed from the cluster holds nothing back.
func TestADeletionIsKeptUntilEveryMemberHasIt(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "near", "near", HealthStatusHealthy, nil)
	insertNode(t, db, "away", "away", HealthStatusUnavailable, nil)
	old := time.Now().Add(-30 * 24 * time.Hour).Unix()
	for _, id := range []string{"d1", "d2", "d3"} {
		require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, id, "local-node", old))
	}
	forget := func() {
		t.Helper()
		_, err := CleanupOldDeletions(ctx, db, 7*24*time.Hour)
		require.NoError(t, err)
	}

	forget()
	assert.Equal(t, []string{"d1", "d2", "d3"}, deletionsHeld(t, mgr), "sent to no member yet")
	delivered(t, mgr, "near", 3)
	delivered(t, mgr, "away", 1)
	forget()
	assert.Equal(t, []string{"d2", "d3"}, deletionsHeld(t, mgr), "the away node has the first only")

	delivered(t, mgr, "away", 3)
	_, err := db.Exec(`UPDATE cluster_nodes SET replica_missed_since = ? WHERE id = 'away'`, old-1)
	require.NoError(t, err)
	forget()
	assert.Equal(t, []string{"d2", "d3"}, deletionsHeld(t, mgr), "a catch-up that replays them is pending")
	_, err = db.Exec(`UPDATE cluster_nodes SET replica_missed_since = NULL, replica_catchup_since = ? WHERE id = 'away'`, old-1)
	require.NoError(t, err)
	forget()
	assert.Equal(t, []string{"d2", "d3"}, deletionsHeld(t, mgr), "the catch-up has not ended")
	mgr.CatchUpEnded(ctx, "away", old-1)
	forget()
	assert.Empty(t, deletionsHeld(t, mgr))

	// A recent deletion stays for the retention whoever has it.
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "recent", "local-node", time.Now().Unix()))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "kept-for-away", "local-node", old))
	delivered(t, mgr, "near", 1000)
	forget()
	assert.Equal(t, []string{"recent", "kept-for-away"}, deletionsHeld(t, mgr))
	require.NoError(t, mgr.RemoveNodeAt(ctx, "away", time.Now().Unix(), "local-node"))
	forget()
	assert.Equal(t, []string{"recent", "away"}, deletionsHeld(t, mgr), "a removed node holds nothing back; its removal is kept")
	var deliveries int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM cluster_deletion_log_delivery WHERE node_id = 'away'`).Scan(&deliveries))
	assert.Zero(t, deliveries)
}

// The deletions of buckets and rows are kept while a member of the cluster
// has yet to be sent what changed since before them.
func TestBucketAndRowDeletionsAreKeptForAMemberThatMissedThem(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	ctx := context.Background()
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "away", "away", HealthStatusUnavailable, nil)
	month := time.Now().Add(-30 * 24 * time.Hour)
	_, err := db.Exec(`INSERT INTO ha_bucket_tombstones (path, deleted_at) VALUES ('before', ?), ('after', ?)`,
		month.Add(-time.Hour).UnixNano(), month.Add(time.Hour).UnixNano())
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO ha_row_versions (tbl, id, version, deleted) VALUES ('shares', 'before', ?, 1), ('shares', 'after', ?, 1)`,
		month.Add(-time.Hour).UnixNano(), month.Add(time.Hour).UnixNano())
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE cluster_nodes SET replica_missed_since = ? WHERE id = 'away'`, month.Unix())
	require.NoError(t, err)
	held := func() (buckets, rows []string) {
		t.Helper()
		for _, q := range []struct {
			query string
			into  *[]string
		}{
			{`SELECT path FROM ha_bucket_tombstones ORDER BY path`, &buckets},
			{`SELECT id FROM ha_row_versions ORDER BY id`, &rows},
		} {
			r, err := db.Query(q.query)
			require.NoError(t, err)
			for r.Next() {
				var s string
				require.NoError(t, r.Scan(&s))
				*q.into = append(*q.into, s)
			}
			r.Close()
		}
		return
	}

	ForgetOldDeletions(ctx, db, 7*24*time.Hour)
	b, r := held()
	assert.Equal(t, []string{"after"}, b, "the one made after the node began to miss changes stays")
	assert.Equal(t, []string{"after"}, r)

	_, err = db.Exec(`UPDATE cluster_nodes SET replica_missed_since = NULL WHERE id = 'away'`)
	require.NoError(t, err)
	ForgetOldDeletions(ctx, db, 7*24*time.Hour)
	b, r = held()
	assert.Empty(t, b)
	assert.Empty(t, r)
}

// A catch-up keeps the time it began from until it ends; one begun later does
// not end an earlier one.
func TestACatchUpIsTrackedUntilItEnds(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	var began []time.Time
	mgr.OnReplicaBack(func(_ string, since time.Time) { began = append(began, since) })
	insertNode(t, db, "back", "back", HealthStatusHealthy, nil)
	_, err := db.Exec(`UPDATE cluster_nodes SET replica_missed_since = 100 WHERE id = 'back'`)
	require.NoError(t, err)

	mgr.catchUpReplica(ctx, "back")
	require.Len(t, began, 1)
	since, ok := oldestUnsentChange(ctx, db)
	require.True(t, ok)
	assert.EqualValues(t, 100, since, "the record of missed writes is cleared, the catch-up is not over")

	_, err = db.Exec(`UPDATE cluster_nodes SET replica_missed_since = 200 WHERE id = 'back'`)
	require.NoError(t, err)
	mgr.catchUpReplica(ctx, "back")
	mgr.CatchUpEnded(ctx, "back", 200)
	since, ok = oldestUnsentChange(ctx, db)
	require.True(t, ok)
	assert.EqualValues(t, 100, since, "the later catch-up does not end the earlier")
	mgr.CatchUpEnded(ctx, "back", 100)
	_, ok = oldestUnsentChange(ctx, db)
	assert.False(t, ok)
}

// The deletions are numbered in a sequence that never goes back, also once
// the newest are forgotten: a node sent up to a number is sent what follows.
// A log numbered from its highest entry is sent whole again, once.
func TestTheDeletionSequenceNeverGoesBack(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	seqOf := func(id string) int64 {
		t.Helper()
		var seq int64
		require.NoError(t, db.QueryRow(`SELECT seq FROM cluster_deletion_log WHERE entity_id = ?`, id).Scan(&seq))
		return seq
	}
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "first", "n", time.Now().Unix()))
	first := seqOf("first")
	_, err := CleanupOldDeletions(ctx, db, -time.Hour)
	require.NoError(t, err)
	require.Empty(t, deletionsHeld(t, mgr))
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "second", "n", time.Now().Unix()))
	assert.Greater(t, seqOf("second"), first)

	// An earlier release's log: no counter, a node marked past the log.
	_, err = db.Exec(`DROP TABLE cluster_deletion_log_counter`)
	require.NoError(t, err)
	delivered(t, mgr, "peer", 500)
	require.NoError(t, createClusterDeletionLogTable(ctx, db))
	var mark int64
	require.NoError(t, db.QueryRow(`SELECT delivered_seq FROM cluster_deletion_log_delivery WHERE node_id = 'peer'`).Scan(&mark))
	assert.Zero(t, mark, "sent the whole log again")
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeUser, "third", "n", time.Now().Unix()))
	assert.Greater(t, seqOf("third"), int64(500))
}
