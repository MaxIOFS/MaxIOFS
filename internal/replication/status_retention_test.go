package replication

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func statusRows(t *testing.T, m *Manager) map[string]string {
	t.Helper()
	rows, err := m.db.Query(`SELECT source_key, status FROM replication_status`)
	require.NoError(t, err)
	defer rows.Close()

	found := map[string]string{}
	for rows.Next() {
		var key, status string
		require.NoError(t, rows.Scan(&key, &status))
		found[key] = status
	}
	require.NoError(t, rows.Err())
	return found
}

// One row per object and version, kept forever, grows with the store itself.
func TestCleanup_DropsOldCopiesAndKeepsFailures(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := t.Context()

	old := time.Now().AddDate(0, 0, -manager.config.RetentionDays-1)
	recent := time.Now().AddDate(0, 0, -1)

	for _, row := range []struct {
		key         string
		status      ReplicationStatus
		replicated  time.Time
		hasReplTime bool
	}{
		{"copied-long-ago", StatusCompleted, old, true},
		{"copied-yesterday", StatusCompleted, recent, true},
		{"failed-long-ago", StatusFailed, old, true},
		{"never-finished", StatusPending, time.Time{}, false},
	} {
		var replicatedAt interface{}
		if row.hasReplTime {
			replicatedAt = row.replicated
		}
		_, err := manager.db.ExecContext(ctx, `
			INSERT INTO replication_status
			(rule_id, tenant_id, source_bucket, source_key, source_version_id,
			 destination_bucket, destination_key, status, last_attempt, replicated_at, error_message)
			VALUES (?, '', 'src', ?, '', 'dst', ?, ?, ?, ?, '')
		`, "rule-1", row.key, row.key, string(row.status), row.replicated, replicatedAt)
		require.NoError(t, err)
	}

	manager.cleanup(ctx)

	remaining := statusRows(t, manager)
	assert.NotContains(t, remaining, "copied-long-ago", "a copy older than the retention should be gone")
	assert.Contains(t, remaining, "copied-yesterday", "a recent copy stays")
	assert.Contains(t, remaining, "failed-long-ago", "a failure is what an operator comes here to find")
	assert.Contains(t, remaining, "never-finished", "a row that never replicated has no age to judge")
}
