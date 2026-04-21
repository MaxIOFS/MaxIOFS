package cluster

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupDeadNodeReconcilerDB(t *testing.T) *sql.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_dead_node.db")

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=10000")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	require.NoError(t, InitSchema(db))
	require.NoError(t, InitReplicationSchema(db))
	return db
}

func enableCluster(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
		VALUES (?, ?, ?, ?)`, "local-node", "local", "tok", 1)
	require.NoError(t, err)
}

func setReplicationFactor(t *testing.T, db *sql.DB, factor int) {
	t.Helper()
	require.NoError(t, SetGlobalConfig(context.Background(), db, "ha.replication_factor",
		fmtInt(factor)))
}

func fmtInt(i int) string {
	switch {
	case i == 1:
		return "1"
	case i == 2:
		return "2"
	case i == 3:
		return "3"
	}
	return "1"
}

func newTestManager(t *testing.T, db *sql.DB) *Manager {
	t.Helper()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	return &Manager{
		db:                  db,
		log:                 logrus.NewEntry(logger),
		healthCheckInterval: 30 * time.Second,
		stopChan:            make(chan struct{}),
	}
}

// fakeSyncTrigger records how many times Trigger was called.
type fakeSyncTrigger struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeSyncTrigger) Trigger(_ context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
}

func (f *fakeSyncTrigger) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// captureEmitter records every event the reconciler emits.
type captureEmitter struct {
	mu     sync.Mutex
	events []DeadNodeEvent
}

func (c *captureEmitter) emit(ev DeadNodeEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *captureEmitter) snapshot() []DeadNodeEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]DeadNodeEvent, len(c.events))
	copy(out, c.events)
	return out
}

func insertNode(t *testing.T, db *sql.DB, id, name, status string, unavailableSince *time.Time) {
	t.Helper()
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO cluster_nodes (id, name, endpoint, api_url, node_token, region, priority,
		                            health_status, last_seen, latency_ms, capacity_total, capacity_used,
		                            bucket_count, metadata, created_at, updated_at, unavailable_since)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, "http://"+id+":8082", "", "tok", "", 100,
		status, now, 0, 0, 0, 0, "{}", now, now, unavailableSince)
	require.NoError(t, err)
}

// ── Tests ──────────────────────────────────────────────────────────────────

// TestRunOnce_NoCluster verifies the reconciler is a no-op when the cluster
// is not enabled.
func TestRunOnce_NoCluster(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	mgr := newTestManager(t, db)

	emitter := &captureEmitter{}
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, emitter.emit)

	require.NoError(t, r.RunOnce(context.Background()))
	assert.Empty(t, emitter.snapshot())
}

// TestRunOnce_RedistributionDisabled verifies the kill-switch.
func TestRunOnce_RedistributionDisabled(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)
	require.NoError(t, SetGlobalConfig(context.Background(), db, redistributionEnabledKey, "false"))

	old := time.Now().Add(-48 * time.Hour)
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusHealthy, nil)
	insertNode(t, db, "n3", "n3", HealthStatusUnavailable, &old)

	mgr := newTestManager(t, db)
	emitter := &captureEmitter{}
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, emitter.emit)

	require.NoError(t, r.RunOnce(context.Background()))

	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n3'`).Scan(&status))
	assert.Equal(t, HealthStatusUnavailable, status, "kill-switch must prevent dead transition")
}

// TestRunOnce_MarksDeadPastThreshold confirms a node past the 24h threshold
// is transitioned to dead and a sync trigger fires.
func TestRunOnce_MarksDeadPastThreshold(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)

	// 4 nodes total — marking one dead leaves 3 (>= factor 2).
	old := time.Now().Add(-25 * time.Hour)
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusHealthy, nil)
	insertNode(t, db, "n3", "n3", HealthStatusHealthy, nil)
	insertNode(t, db, "n4", "n4", HealthStatusUnavailable, &old)

	mgr := newTestManager(t, db)
	emitter := &captureEmitter{}
	syncer := &fakeSyncTrigger{}
	r := NewDeadNodeReconciler(mgr, syncer, emitter.emit)

	require.NoError(t, r.RunOnce(context.Background()))

	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n4'`).Scan(&status))
	assert.Equal(t, HealthStatusDead, status)

	// One node_dead event emitted.
	events := emitter.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t, EventNodeDead, events[0].Kind)
	assert.Equal(t, "n4", events[0].NodeID)

	// Sync trigger called asynchronously — give the goroutine a moment.
	require.Eventually(t, func() bool { return syncer.Calls() >= 1 },
		time.Second, 10*time.Millisecond)
}

// TestRunOnce_SkipsNodesBeforeThreshold confirms an unavailable node younger
// than the threshold is left alone.
func TestRunOnce_SkipsNodesBeforeThreshold(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)

	young := time.Now().Add(-1 * time.Hour) // well under default 24h
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusHealthy, nil)
	insertNode(t, db, "n3", "n3", HealthStatusUnavailable, &young)

	mgr := newTestManager(t, db)
	emitter := &captureEmitter{}
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, emitter.emit)
	require.NoError(t, r.RunOnce(context.Background()))

	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n3'`).Scan(&status))
	assert.Equal(t, HealthStatusUnavailable, status)
}

// TestRunOnce_LastSurvivorProtection covers scenario D from the design:
// when marking a node dead would drop the cluster below the replication
// factor, the reconciler refuses and instead enters degraded state.
func TestRunOnce_LastSurvivorProtection(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)

	// 2 nodes: 1 healthy local + 1 unavailable past threshold.
	// Marking the unavailable dead would leave 1 non-dead < factor 2.
	old := time.Now().Add(-48 * time.Hour)
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusUnavailable, &old)

	mgr := newTestManager(t, db)
	emitter := &captureEmitter{}
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, emitter.emit)
	require.NoError(t, r.RunOnce(context.Background()))

	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n2'`).Scan(&status))
	assert.Equal(t, HealthStatusUnavailable, status, "must not be marked dead — last survivor")

	// Degraded reason should be set.
	reason := ClusterDegradedReason(context.Background(), db)
	assert.NotEmpty(t, reason)

	// At least one cluster_degraded event emitted.
	events := emitter.snapshot()
	var foundDegraded bool
	for _, ev := range events {
		if ev.Kind == EventClusterDegraded {
			foundDegraded = true
			break
		}
	}
	assert.True(t, foundDegraded)
}

// TestRunOnce_ClusterDegradedResolved verifies the degraded reason is cleared
// once enough healthy nodes are available again, and an SSE event is emitted.
func TestRunOnce_ClusterDegradedResolved(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)

	// Pre-seed degraded state.
	require.NoError(t, SetGlobalConfig(context.Background(), db, clusterDegradedReasonKey,
		"prior degraded reason"))

	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusHealthy, nil)

	mgr := newTestManager(t, db)
	emitter := &captureEmitter{}
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, emitter.emit)

	require.NoError(t, r.RunOnce(context.Background()))

	assert.Empty(t, ClusterDegradedReason(context.Background(), db))

	events := emitter.snapshot()
	var foundResolved bool
	for _, ev := range events {
		if ev.Kind == EventClusterDegradedResolved {
			foundResolved = true
			break
		}
	}
	assert.True(t, foundResolved, "must emit cluster_degraded_resolved on recovery")
}

// TestDrainNode_Success verifies the manual drain path marks dead immediately.
func TestDrainNode_Success(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)

	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusHealthy, nil)
	insertNode(t, db, "n3", "n3", HealthStatusHealthy, nil)

	mgr := newTestManager(t, db)
	emitter := &captureEmitter{}
	syncer := &fakeSyncTrigger{}
	r := NewDeadNodeReconciler(mgr, syncer, emitter.emit)

	require.NoError(t, r.DrainNode(context.Background(), "n3", "decommission"))

	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n3'`).Scan(&status))
	assert.Equal(t, HealthStatusDead, status)

	events := emitter.snapshot()
	require.NotEmpty(t, events)
	assert.Equal(t, EventNodeDead, events[0].Kind)
	assert.Equal(t, "decommission", events[0].Reason)
}

// TestDrainNode_AlreadyDead refuses to redo the work (idempotency guard).
func TestDrainNode_AlreadyDead(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)

	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusDead, nil)

	mgr := newTestManager(t, db)
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, nil)
	err := r.DrainNode(context.Background(), "n2", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already dead")
}

// TestMarkNodeUnavailable_PreservesUnavailableSince confirms the column is
// set on the first transition and not clobbered on subsequent fanout
// failures.
func TestMarkNodeUnavailable_PreservesUnavailableSince(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	insertNode(t, db, "n1", "n1", HealthStatusHealthy, nil)

	mgr := newTestManager(t, db)
	ctx := context.Background()

	mgr.markNodeUnavailable(ctx, "n1", "first failure")
	var first sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT unavailable_since FROM cluster_nodes WHERE id = 'n1'`).Scan(&first))
	require.True(t, first.Valid, "unavailable_since must be set on transition")

	// Sleep past clock granularity, then mark again.
	time.Sleep(20 * time.Millisecond)
	mgr.markNodeUnavailable(ctx, "n1", "second failure")
	var second sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT unavailable_since FROM cluster_nodes WHERE id = 'n1'`).Scan(&second))
	require.True(t, second.Valid)
	assert.True(t, first.Time.Equal(second.Time),
		"unavailable_since must NOT advance on repeated failures (got %v vs %v)", first.Time, second.Time)
}

// TestMarkNodeUnavailable_SkipsDeadNodes ensures dead nodes can't be
// resurrected to unavailable by transient probe failures.
func TestMarkNodeUnavailable_SkipsDeadNodes(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	insertNode(t, db, "n1", "n1", HealthStatusDead, nil)

	mgr := newTestManager(t, db)
	mgr.markNodeUnavailable(context.Background(), "n1", "spurious")

	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n1'`).Scan(&status))
	assert.Equal(t, HealthStatusDead, status, "dead nodes must not be flipped back by markNodeUnavailable")
}

// TestDeadThreshold_DefaultsAndOverrides round-trips the live config.
func TestDeadThreshold_DefaultsAndOverrides(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	mgr := newTestManager(t, db)
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, nil)

	// Default 24h.
	assert.Equal(t, 24*time.Hour, r.deadThreshold(context.Background()))

	require.NoError(t, SetGlobalConfig(context.Background(), db, deadNodeConfigKey, "1"))
	assert.Equal(t, time.Hour, r.deadThreshold(context.Background()))

	// Invalid values fall back to default.
	require.NoError(t, SetGlobalConfig(context.Background(), db, deadNodeConfigKey, "garbage"))
	assert.Equal(t, 24*time.Hour, r.deadThreshold(context.Background()))
}

// TestCheckInterval_Override verifies the live config controls the loop cadence.
func TestCheckInterval_Override(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	mgr := newTestManager(t, db)
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, nil)

	assert.Equal(t, defaultRedistributionCheckInterval, r.checkInterval(context.Background()))

	require.NoError(t, SetGlobalConfig(context.Background(), db, redistributionIntervalKey, "10"))
	assert.Equal(t, 10*time.Minute, r.checkInterval(context.Background()))
}

// TestClusterDegradedReason_RoundTrip exercises the read-only helper used by
// the console handler.
func TestClusterDegradedReason_RoundTrip(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	assert.Empty(t, ClusterDegradedReason(context.Background(), db))

	require.NoError(t, SetGlobalConfig(context.Background(), db, clusterDegradedReasonKey, "foo"))
	assert.Equal(t, "foo", ClusterDegradedReason(context.Background(), db))
}
