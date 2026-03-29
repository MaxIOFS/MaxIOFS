package replication

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupSyncManager creates a Manager backed by an in-memory SQLite DB with a
// configurable BucketLister so individual tests can control ListObjects output.
func setupSyncManager(t *testing.T, lister *MockBucketLister) *Manager {
	t.Helper()
	db := setupTestDB(t)
	t.Cleanup(func() { db.Close() })

	config := ReplicationConfig{
		WorkerCount:     1,
		QueueSize:       100,
		BatchSize:       50,
		RetryInterval:   1 * time.Minute,
		MaxRetries:      3,
		CleanupInterval: 1 * time.Hour,
		RetentionDays:   30,
	}
	m, err := NewManager(db, config, &MockObjectAdapter{}, &MockObjectManager{}, lister)
	require.NoError(t, err)
	return m
}

// insertScheduledRule inserts a rule with mode=scheduled and the given interval (minutes).
func insertScheduledRule(t *testing.T, m *Manager, id string, interval int) *ReplicationRule {
	t.Helper()
	rule := &ReplicationRule{
		ID:                "sched-" + id,
		TenantID:          "tenant1",
		SourceBucket:      "src-bucket",
		DestinationBucket: "dst-bucket",
		Enabled:           true,
		Mode:              ModeScheduled,
		ScheduleInterval:  interval,
	}
	insertTestRule(t, m.db, rule)
	return rule
}

// queueCount returns the number of rows in replication_queue for the given rule.
func queueCount(t *testing.T, m *Manager, ruleID string) int {
	t.Helper()
	var n int
	err := m.db.QueryRow(
		`SELECT COUNT(*) FROM replication_queue WHERE rule_id = ?`, ruleID,
	).Scan(&n)
	require.NoError(t, err)
	return n
}

// ---------------------------------------------------------------------------
// SyncBucket
// ---------------------------------------------------------------------------

func TestSyncBucket_Success(t *testing.T) {
	objects := []string{"a.txt", "b.txt", "c.txt"}
	lister := &MockBucketLister{
		ListObjectsFunc: func(_ context.Context, _, _, _ string, _ int) ([]string, error) {
			return objects, nil
		},
	}
	m := setupSyncManager(t, lister)
	ctx := context.Background()

	rule := &ReplicationRule{
		ID:                "sync-ok",
		TenantID:          "tenant1",
		SourceBucket:      "src-bucket",
		DestinationBucket: "dst-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	insertTestRule(t, m.db, rule)

	count, err := m.SyncBucket(ctx, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	assert.Equal(t, 3, queueCount(t, m, rule.ID))
}

func TestSyncBucket_RuleNotFound(t *testing.T) {
	m := setupSyncManager(t, &MockBucketLister{})
	_, err := m.SyncBucket(context.Background(), "does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule not found")
}

func TestSyncBucket_RuleDisabled(t *testing.T) {
	m := setupSyncManager(t, &MockBucketLister{})
	ctx := context.Background()

	rule := &ReplicationRule{
		ID:                "sync-disabled",
		TenantID:          "tenant1",
		SourceBucket:      "src-bucket",
		DestinationBucket: "dst-bucket",
		Enabled:           false,
		Mode:              ModeRealTime,
	}
	insertTestRule(t, m.db, rule)

	_, err := m.SyncBucket(ctx, rule.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule is disabled")
}

func TestSyncBucket_NoObjects(t *testing.T) {
	lister := &MockBucketLister{
		ListObjectsFunc: func(_ context.Context, _, _, _ string, _ int) ([]string, error) {
			return []string{}, nil
		},
	}
	m := setupSyncManager(t, lister)
	ctx := context.Background()

	rule := &ReplicationRule{
		ID:                "sync-empty",
		TenantID:          "tenant1",
		SourceBucket:      "src-bucket",
		DestinationBucket: "dst-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	insertTestRule(t, m.db, rule)

	count, err := m.SyncBucket(ctx, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestSyncBucket_ListError(t *testing.T) {
	lister := &MockBucketLister{
		ListObjectsFunc: func(_ context.Context, _, _, _ string, _ int) ([]string, error) {
			return nil, fmt.Errorf("storage unavailable")
		},
	}
	m := setupSyncManager(t, lister)
	ctx := context.Background()

	rule := &ReplicationRule{
		ID:                "sync-listerr",
		TenantID:          "tenant1",
		SourceBucket:      "src-bucket",
		DestinationBucket: "dst-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	insertTestRule(t, m.db, rule)

	_, err := m.SyncBucket(ctx, rule.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list objects")
}

func TestSyncBucket_PrefixFilter(t *testing.T) {
	// Lister returns objects with and without the rule's prefix.
	// Only matching ones should be queued.
	lister := &MockBucketLister{
		ListObjectsFunc: func(_ context.Context, _, _, _ string, _ int) ([]string, error) {
			return []string{"logs/a.txt", "logs/b.txt", "other/c.txt"}, nil
		},
	}
	m := setupSyncManager(t, lister)
	ctx := context.Background()

	rule := &ReplicationRule{
		ID:                "sync-prefix",
		TenantID:          "tenant1",
		SourceBucket:      "src-bucket",
		DestinationBucket: "dst-bucket",
		Prefix:            "logs/",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	insertTestRule(t, m.db, rule)

	count, err := m.SyncBucket(ctx, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// ---------------------------------------------------------------------------
// SyncRule
// ---------------------------------------------------------------------------

func TestSyncRule_Success(t *testing.T) {
	lister := &MockBucketLister{
		ListObjectsFunc: func(_ context.Context, _, _, _ string, _ int) ([]string, error) {
			return []string{"x.txt", "y.txt"}, nil
		},
	}
	m := setupSyncManager(t, lister)
	ctx := context.Background()

	rule := &ReplicationRule{
		ID:                "syncrule-ok",
		TenantID:          "tenant1",
		SourceBucket:      "src-bucket",
		DestinationBucket: "dst-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	insertTestRule(t, m.db, rule)

	count, err := m.SyncRule(ctx, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestSyncRule_LockContention(t *testing.T) {
	// Hold the rule lock so the second SyncRule call cannot acquire it.
	m := setupSyncManager(t, &MockBucketLister{})
	ruleID := "syncrule-lock"

	// Manually acquire the lock before calling SyncRule
	m.locksMu.Lock()
	lock := &sync.Mutex{}
	lock.Lock() // pre-locked
	m.ruleLocks[ruleID] = lock
	m.locksMu.Unlock()

	_, err := m.SyncRule(context.Background(), ruleID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync already in progress")
}

// ---------------------------------------------------------------------------
// processScheduledRules
// ---------------------------------------------------------------------------

func TestProcessScheduledRules_NoRules(t *testing.T) {
	m := setupSyncManager(t, &MockBucketLister{})
	lastSync := make(map[string]time.Time)

	// Should not panic
	m.processScheduledRules(context.Background(), lastSync)

	assert.Empty(t, lastSync)
}

func TestProcessScheduledRules_RealTimeModeNotScheduled(t *testing.T) {
	// A real-time rule should never be picked up by the scheduler.
	lister := &MockBucketLister{
		ListObjectsFunc: func(_ context.Context, _, _, _ string, _ int) ([]string, error) {
			return []string{"obj.txt"}, nil
		},
	}
	m := setupSyncManager(t, lister)
	ctx := context.Background()

	rule := &ReplicationRule{
		ID:                "rt-rule",
		TenantID:          "tenant1",
		SourceBucket:      "src-bucket",
		DestinationBucket: "dst-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
		ScheduleInterval:  0,
	}
	insertTestRule(t, m.db, rule)

	lastSync := make(map[string]time.Time)
	m.processScheduledRules(ctx, lastSync)

	// No goroutines should have been spawned → queue stays empty
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, queueCount(t, m, rule.ID))
}

func TestProcessScheduledRules_TriggersOverdueRule(t *testing.T) {
	// A scheduled rule whose last sync was more than interval minutes ago
	// should be queued by processScheduledRules.
	lister := &MockBucketLister{
		ListObjectsFunc: func(_ context.Context, _, _, _ string, _ int) ([]string, error) {
			return []string{"obj.txt"}, nil
		},
	}
	m := setupSyncManager(t, lister)
	ctx := context.Background()

	rule := insertScheduledRule(t, m, "overdue", 1) // 1-minute interval

	// Pretend last sync was 2 minutes ago → overdue
	lastSync := map[string]time.Time{
		rule.ID: time.Now().Add(-2 * time.Minute),
	}

	m.processScheduledRules(ctx, lastSync)

	// Poll until the spawned goroutine queues the object or we time out.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if queueCount(t, m, rule.ID) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	assert.Equal(t, 1, queueCount(t, m, rule.ID))
}

func TestProcessScheduledRules_SkipsRecentlySyncedRule(t *testing.T) {
	lister := &MockBucketLister{
		ListObjectsFunc: func(_ context.Context, _, _, _ string, _ int) ([]string, error) {
			return []string{"obj.txt"}, nil
		},
	}
	m := setupSyncManager(t, lister)
	ctx := context.Background()

	rule := insertScheduledRule(t, m, "recent", 60) // 60-minute interval

	// Last sync was 30 seconds ago — not due yet
	lastSync := map[string]time.Time{
		rule.ID: time.Now().Add(-30 * time.Second),
	}

	m.processScheduledRules(ctx, lastSync)

	// Brief pause: no goroutine should have been spawned
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, queueCount(t, m, rule.ID))
}

func TestProcessScheduledRules_CleansStaleLastSyncEntries(t *testing.T) {
	// Rules that no longer exist in the DB should be removed from lastSync.
	m := setupSyncManager(t, &MockBucketLister{})
	ctx := context.Background()

	lastSync := map[string]time.Time{
		"ghost-rule-1": time.Now().Add(-5 * time.Minute),
		"ghost-rule-2": time.Now().Add(-10 * time.Minute),
	}

	m.processScheduledRules(ctx, lastSync)

	assert.Empty(t, lastSync, "stale entries for non-existent rules should be removed")
}

// ---------------------------------------------------------------------------
// cleanup
// ---------------------------------------------------------------------------

func TestCleanup_RemovesExpiredItems(t *testing.T) {
	m := setupSyncManager(t, &MockBucketLister{})
	ctx := context.Background()

	// Insert a rule first (foreign key)
	rule := &ReplicationRule{
		ID:                "cleanup-rule",
		TenantID:          "tenant1",
		SourceBucket:      "src-bucket",
		DestinationBucket: "dst-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	insertTestRule(t, m.db, rule)

	// Insert a completed item with completed_at well beyond the retention window
	old := time.Now().AddDate(0, 0, -(m.config.RetentionDays + 1))
	_, err := m.db.Exec(`
		INSERT INTO replication_queue
			(rule_id, tenant_id, bucket, object_key, action, status, attempts, max_retries,
			 scheduled_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, "tenant1", "src-bucket", "old.txt", "PUT",
		StatusCompleted, 1, 3, old, old,
	)
	require.NoError(t, err)

	m.cleanup(ctx)

	assert.Equal(t, 0, queueCount(t, m, rule.ID))
}

func TestCleanup_KeepsRecentItems(t *testing.T) {
	m := setupSyncManager(t, &MockBucketLister{})
	ctx := context.Background()

	rule := &ReplicationRule{
		ID:                "cleanup-keep-rule",
		TenantID:          "tenant1",
		SourceBucket:      "src-bucket",
		DestinationBucket: "dst-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	insertTestRule(t, m.db, rule)

	// Completed item within retention window
	recent := time.Now().AddDate(0, 0, -1)
	_, err := m.db.Exec(`
		INSERT INTO replication_queue
			(rule_id, tenant_id, bucket, object_key, action, status, attempts, max_retries,
			 scheduled_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, "tenant1", "src-bucket", "new.txt", "PUT",
		StatusCompleted, 1, 3, recent, recent,
	)
	require.NoError(t, err)

	m.cleanup(ctx)

	assert.Equal(t, 1, queueCount(t, m, rule.ID))
}

func TestCleanup_KeepsPendingItems(t *testing.T) {
	m := setupSyncManager(t, &MockBucketLister{})
	ctx := context.Background()

	rule := &ReplicationRule{
		ID:                "cleanup-pending-rule",
		TenantID:          "tenant1",
		SourceBucket:      "src-bucket",
		DestinationBucket: "dst-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	insertTestRule(t, m.db, rule)

	// Pending item with an old scheduled_at — should NOT be deleted (only completed/failed are cleaned)
	old := time.Now().AddDate(0, 0, -(m.config.RetentionDays + 5))
	_, err := m.db.Exec(`
		INSERT INTO replication_queue
			(rule_id, tenant_id, bucket, object_key, action, status, attempts, max_retries, scheduled_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, "tenant1", "src-bucket", "pending.txt", "PUT",
		StatusPending, 0, 3, old,
	)
	require.NoError(t, err)

	m.cleanup(ctx)

	assert.Equal(t, 1, queueCount(t, m, rule.ID))
}
