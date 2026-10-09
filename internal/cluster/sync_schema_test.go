package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A default no one changed is dated at the epoch, a default seeded with the
// time of its seeding too; a changed setting keeps the time of its change.
func TestUnchangedDefaultsAreOlderThanAnyChange(t *testing.T) {
	ctx := context.Background()
	db := setupDeadNodeReconcilerDB(t)
	updatedAt := func(key string) int64 {
		t.Helper()
		var raw any
		require.NoError(t, db.QueryRowContext(ctx, `SELECT updated_at FROM cluster_global_config WHERE key = ?`, key).Scan(&raw))
		ts, ok := SQLiteTimestampUnix(raw)
		require.True(t, ok, key)
		return ts
	}
	assert.Zero(t, updatedAt("ha.replication_factor"))

	seeded := time.Unix(1_700_000_000, 0).UTC()
	changed := seeded.Add(time.Hour)
	for _, row := range []struct {
		key, value       string
		created, updated time.Time
	}{
		{"ha.replication_factor", "1", seeded, seeded},
		{"ha.scrub_interval_hours", "24", seeded, changed},
		{"ha.dead_node_threshold_hours", "48", seeded, seeded},
	} {
		_, err := db.ExecContext(ctx, `UPDATE cluster_global_config SET value = ?, created_at = ?, updated_at = ? WHERE key = ?`,
			row.value, row.created, row.updated, row.key)
		require.NoError(t, err)
	}
	require.NoError(t, InitReplicationSchema(db))

	assert.Zero(t, updatedAt("ha.replication_factor"), "seeded and never changed")
	assert.Equal(t, changed.Unix(), updatedAt("ha.scrub_interval_hours"), "changed back to its default")
	assert.Equal(t, seeded.Unix(), updatedAt("ha.dead_node_threshold_hours"), "a value other than the default")
}
