package cluster

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
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

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
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

// A drain leaves the node in service and moves its copies; the node is out
// of service once its health check says it holds no data, and the drained
// node is not counted among those that take data.
func TestDrainNode_MovesTheCopiesThenTakesTheNodeOut(t *testing.T) {
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
	ctx := context.Background()

	require.NoError(t, r.DrainNode(ctx, "n3", "decommission"))
	n3, err := mgr.GetNode(ctx, "n3")
	require.NoError(t, err)
	assert.True(t, n3.Drained)
	assert.Equal(t, HealthStatusHealthy, n3.HealthStatus, "it serves while its copies move")
	assert.True(t, n3.InService())
	assert.False(t, n3.TakesData())
	assert.Eventually(t, func() bool { return syncer.Calls() == 1 }, time.Second, 10*time.Millisecond, "its copies are moved")
	assert.Empty(t, emitter.snapshot())
	assert.ErrorIs(t, r.DrainNode(ctx, "n2", ""), ErrDrainBelowReplicationFactor, "one node would be left to take data")

	require.NoError(t, r.RunOnce(ctx))
	n3, err = mgr.GetNode(ctx, "n3")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusHealthy, n3.HealthStatus, "it holds data still")
	assert.Eventually(t, func() bool { return syncer.Calls() == 3 }, time.Second, 10*time.Millisecond, "the move is retried, and the nodes of the class changed")

	mgr.noteDrainDone("n3", true)
	require.NoError(t, r.RunOnce(ctx))
	n3, err = mgr.GetNode(ctx, "n3")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusDead, n3.HealthStatus)
	events := emitter.snapshot()
	require.NotEmpty(t, events)
	assert.Equal(t, EventNodeDead, events[0].Kind)
	assert.Equal(t, "drained", events[0].Reason)
}

// A node being drained takes no data: it does not count among the nodes that
// keep the factor, when another node would be marked dead or for the degraded
// state of the cluster.
func TestADrainedNodeDoesNotCountForTheDeadSafety(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)
	require.NoError(t, SetGlobalConfig(context.Background(), db, redistributionEnabledKey, "true"))
	long := time.Now().Add(-48 * time.Hour)
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "draining", "draining", HealthStatusHealthy, nil)
	insertNode(t, db, "gone", "gone", HealthStatusUnavailable, &long)
	_, err := db.Exec(`UPDATE cluster_nodes SET drained = 1 WHERE id = 'draining'`)
	require.NoError(t, err)
	mgr := newTestManager(t, db)
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, nil)

	require.NoError(t, r.RunOnce(context.Background()))
	gone, err := mgr.GetNode(context.Background(), "gone")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusUnavailable, gone.HealthStatus, "one node would be left to take data")
	assert.NotEmpty(t, ClusterDegradedReason(context.Background(), db), "the cluster cannot keep the factor")
}

// A drained node that does not answer is out of service at once when its
// copies are held elsewhere; with a factor of 1 it is waited for.
func TestDrainNode_AnUnavailableNode(t *testing.T) {
	for _, factor := range []int{1, 2} {
		db := setupDeadNodeReconcilerDB(t)
		enableCluster(t, db)
		setReplicationFactor(t, db, factor)
		insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
		insertNode(t, db, "n2", "n2", HealthStatusHealthy, nil)
		insertNode(t, db, "down", "down", HealthStatusUnavailable, nil)
		mgr := newTestManager(t, db)
		r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, nil)

		require.NoError(t, r.DrainNode(context.Background(), "down", ""))
		n, err := mgr.GetNode(context.Background(), "down")
		require.NoError(t, err)
		assert.True(t, n.Drained)
		if factor == 1 {
			assert.Equal(t, HealthStatusUnavailable, n.HealthStatus, "its objects are moved once it is back")
		} else {
			assert.Equal(t, HealthStatusDead, n.HealthStatus, "the others hold its copies")
		}
	}
}

// TestDrainNode_DeadNode drains a node already dead by the probes, so it
// is not back in service when it answers; a drained node is drained once.
func TestDrainNode_DeadNode(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)

	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusDead, nil)

	mgr := newTestManager(t, db)
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, nil)
	require.NoError(t, r.DrainNode(context.Background(), "n2", ""))
	n2, err := mgr.GetNode(context.Background(), "n2")
	require.NoError(t, err)
	assert.True(t, n2.Drained)
	err = r.DrainNode(context.Background(), "n2", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already drained")
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

// The degraded state follows the health of the nodes: a change of a node's
// health recomputes it, without waiting for the reconciler's next pass.
func TestTheDegradedStateFollowsTheHealthOfTheNodes(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	var answer atomic.Int32
	answer.Store(http.StatusOK)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(answer.Load()))
	}))
	defer peer.Close()
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "peer", "peer", HealthStatusUnknown, nil)
	_, err := db.Exec(`UPDATE cluster_nodes SET endpoint = ? WHERE id = ?`, peer.URL, "peer")
	require.NoError(t, err)

	emitter := &captureEmitter{}
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, emitter.emit)
	defer r.Stop()
	mgr.OnHealthChange(func() { r.RecomputeDegradedState(ctx) })
	r.recomputeClusterDegradedState(ctx)
	require.NotEmpty(t, ClusterDegradedReason(ctx, db), "the peer is not known to be healthy yet")

	degraded := func(want bool) func() bool {
		return func() bool { return (ClusterDegradedReason(ctx, db) != "") == want }
	}
	_, err = mgr.CheckNodeHealth(ctx, "peer")
	require.NoError(t, err)
	assert.Eventually(t, degraded(false), 5*time.Second, 10*time.Millisecond, "the peer answers")

	answer.Store(http.StatusServiceUnavailable)
	_, err = mgr.CheckNodeHealth(ctx, "peer")
	require.NoError(t, err)
	assert.Eventually(t, degraded(true), 5*time.Second, 10*time.Millisecond, "the peer answers no more")

	answer.Store(http.StatusOK)
	_, err = mgr.CheckNodeHealth(ctx, "peer")
	require.NoError(t, err)
	assert.Eventually(t, degraded(false), 5*time.Second, 10*time.Millisecond, "the peer is back")

	mgr.markNodeUnavailable(ctx, "peer", "a copy to it failed")
	assert.Eventually(t, degraded(true), 5*time.Second, 10*time.Millisecond, "a request to the peer failed")

	var kinds []DeadNodeEventKind
	for _, ev := range emitter.snapshot() {
		kinds = append(kinds, ev.Kind)
	}
	assert.Equal(t, []DeadNodeEventKind{EventClusterDegraded, EventClusterDegradedResolved, EventClusterDegraded,
		EventClusterDegradedResolved, EventClusterDegraded}, kinds)
}

// The degraded state the console reads is that of the nodes as they are now.
func TestTheDegradedStateReadIsComputedNow(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "peer", "peer", HealthStatusHealthy, nil)
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, nil)
	require.NoError(t, SetGlobalConfig(ctx, db, clusterDegradedReasonKey, "cluster has 1 healthy node(s), replication factor is 2"))
	assert.Empty(t, r.DegradedReason(ctx), "both nodes are healthy")

	setReplicationFactor(t, db, 3)
	assert.Contains(t, r.DegradedReason(ctx), "replication factor is 3")
}

// A node out of a cluster is not degraded, whatever factor it was left with.
func TestANodeOutOfAClusterIsNotDegraded(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	setReplicationFactor(t, db, 2)
	r := NewDeadNodeReconciler(newTestManager(t, db), &fakeSyncTrigger{}, nil)
	assert.Empty(t, r.DegradedReason(context.Background()))
}
