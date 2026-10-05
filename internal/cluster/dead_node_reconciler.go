package cluster

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/maxiofs/maxiofs/internal/bgwork"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	defaultDeadNodeThresholdHours      = 24
	defaultRedistributionCheckInterval = 5 * time.Minute
	deadNodeConfigKey                  = "ha.dead_node_threshold_hours"
	redistributionIntervalKey          = "ha.redistribution_check_interval_minutes"
	redistributionEnabledKey           = "ha.redistribution_enabled"
	clusterDegradedReasonKey           = "ha.cluster_degraded_reason"
)

// DeadNodeEventKind enumerates the lifecycle events the reconciler emits to
// the host (typically wired to SSE notifications).
type DeadNodeEventKind string

const (
	EventNodeDead                DeadNodeEventKind = "node_dead"
	EventClusterDegraded         DeadNodeEventKind = "cluster_degraded"
	EventClusterDegradedResolved DeadNodeEventKind = "cluster_degraded_resolved"
)

// DeadNodeEvent carries the payload for a reconciler lifecycle event.
type DeadNodeEvent struct {
	Kind     DeadNodeEventKind
	NodeID   string
	NodeName string
	Reason   string
	// Cluster-wide context for monitoring dashboards.
	Factor       int
	NonDeadNodes int
}

// EventEmitter is the callback the host supplies to receive reconciler events.
// It must be non-blocking; emission failures should be logged by the caller.
type EventEmitter func(DeadNodeEvent)

// SyncTrigger is the minimal HASyncWorker capability the reconciler needs to
// kick off catch-up sync after marking nodes dead.
type SyncTrigger interface {
	Trigger(ctx context.Context)
}

// DeadNodeReconciler periodically inspects cluster_nodes for unavailable nodes
type DeadNodeReconciler struct {
	mgr    *Manager
	syncer SyncTrigger
	emit   EventEmitter
	log    *logrus.Entry

	bgwork.Worker
	mu sync.Mutex
}

// NewDeadNodeReconciler builds a reconciler bound to the cluster manager and
// the HA sync trigger. emit may be nil — events will simply be logged in that
// case.
func NewDeadNodeReconciler(mgr *Manager, syncer SyncTrigger, emit EventEmitter) *DeadNodeReconciler {
	return &DeadNodeReconciler{
		mgr:    mgr,
		syncer: syncer,
		emit:   emit,
		log:    logrus.WithField("component", "dead-node-reconciler"),
	}
}

// Start launches the background goroutine. It returns immediately; Stop ends it
// and waits for it.
func (r *DeadNodeReconciler) Start(ctx context.Context) {
	r.Spawn(func() { r.run(ctx) })
}

func (r *DeadNodeReconciler) run(ctx context.Context) {
	interval := r.checkInterval(ctx)
	r.log.WithField("interval", interval).Info("Dead-node reconciler started")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once shortly after startup so a freshly-restarted node catches any
	// nodes that crossed the threshold while it was down.
	r.Spawn(func() {
		select {
		case <-ctx.Done():
			return
		case <-r.Stopped():
			return
		case <-time.After(30 * time.Second):
			if err := r.RunOnce(ctx); err != nil {
				r.log.WithError(err).Warn("Initial reconciliation pass failed")
			}
		}
	})

	for {
		select {
		case <-ctx.Done():
			r.log.Info("Dead-node reconciler stopped")
			return
		case <-ticker.C:
			if err := r.RunOnce(ctx); err != nil {
				r.log.WithError(err).Warn("Reconciliation pass failed")
			}
			// Pick up any live config change to the interval.
			if newInterval := r.checkInterval(ctx); newInterval != interval {
				ticker.Reset(newInterval)
				interval = newInterval
				r.log.WithField("interval", interval).Info("Reconciler interval updated from config")
			}
		}
	}
}

// RunOnce performs a single reconciliation pass: detect nodes past the
func (r *DeadNodeReconciler) RunOnce(ctx context.Context) error {
	if !r.mgr.IsClusterEnabled() {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// A drain is an administrator's decision: it applies with automatic
	// redistribution off as well.
	r.applyDrains(ctx)
	if !r.redistributionEnabled(ctx) {
		return nil
	}

	threshold := r.deadThreshold(ctx)
	cutoff := time.Now().Add(-threshold)

	candidates, err := r.findDeadCandidates(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("find dead candidates: %w", err)
	}

	for _, node := range candidates {
		if err := r.markDeadIfSafe(ctx, node, "threshold exceeded"); err != nil && !errors.Is(err, ErrBelowReplicationFactor) {
			r.log.WithError(err).WithField("node_id", node.ID).
				Warn("Failed to mark node dead; will retry next cycle")
		}
	}

	r.recomputeClusterDegradedState(ctx)
	return nil
}

// DrainNode is invoked by the admin endpoint to immediately mark a node dead,
// bypassing the threshold timer. The local node cannot be drained via this
// path — callers must check that before invoking this function.
func (r *DeadNodeReconciler) DrainNode(ctx context.Context, nodeID, reason string) error {
	if !r.mgr.IsClusterEnabled() {
		return fmt.Errorf("cluster is not enabled")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	node, err := r.mgr.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}
	if node.Drained {
		return fmt.Errorf("node already drained")
	}
	if reason == "" {
		reason = "manual drain"
	}

	if node.HealthStatus != HealthStatusDead {
		if err := r.markDeadIfSafe(ctx, node, reason); err != nil {
			return err
		}
	}
	if err := r.mgr.setDrained(ctx, r.mgr.db, nodeID); err != nil {
		return err
	}

	r.recomputeClusterDegradedState(ctx)
	return nil
}

// ApplyDrains marks dead the nodes drained on another node.
func (r *DeadNodeReconciler) ApplyDrains(ctx context.Context) {
	if !r.mgr.IsClusterEnabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applyDrains(ctx)
	r.recomputeClusterDegradedState(ctx)
}

func (r *DeadNodeReconciler) applyDrains(ctx context.Context) {
	nodes, err := r.mgr.queryNodes(ctx, `SELECT `+nodeColumns("")+` FROM cluster_nodes WHERE drained = 1 AND health_status != ?`, HealthStatusDead)
	if err != nil {
		r.log.WithError(err).Warn("Failed to list drained nodes")
		return
	}
	localID, _ := r.mgr.GetLocalNodeID(ctx)
	for _, node := range nodes {
		if node.ID == localID {
			continue
		}
		if err := r.markDeadIfSafe(ctx, node, "drained"); err != nil && !errors.Is(err, ErrBelowReplicationFactor) {
			r.log.WithError(err).WithField("node_id", node.ID).Warn("Failed to mark a drained node dead")
		}
	}
}

// ── Internal helpers ────────────────────────────────────────────────────────

// findDeadCandidates returns nodes whose status is unavailable AND whose
// unavailable_since is older than cutoff. Dead nodes are excluded.
func (r *DeadNodeReconciler) findDeadCandidates(ctx context.Context, cutoff time.Time) ([]*Node, error) {
	return r.mgr.queryNodes(ctx, `SELECT `+nodeColumns("")+` FROM cluster_nodes
		WHERE health_status = ? AND unavailable_since IS NOT NULL AND unavailable_since <= ?`,
		HealthStatusUnavailable, cutoff)
}

// markDeadIfSafe marks the node dead unless doing so would reduce the count of
func (r *DeadNodeReconciler) markDeadIfSafe(ctx context.Context, node *Node, reason string) error {
	factor, err := r.mgr.GetReplicationFactor(ctx)
	if err != nil {
		factor = 1
	}

	tx, err := r.mgr.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback on failure is intentional

	// Count healthy nodes excluding the candidate. UNAVAILABLE/DEGRADED nodes
	// do not contribute to write capacity and are not counted, so an UNAVAILABLE
	// node reaching the dead threshold never inflates the safety count.
	var healthyExcludingCandidate int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cluster_nodes WHERE health_status = ? AND id != ?`,
		HealthStatusHealthy, node.ID,
	).Scan(&healthyExcludingCandidate); err != nil {
		return fmt.Errorf("count healthy nodes: %w", err)
	}

	if healthyExcludingCandidate < factor {
		r.log.WithFields(logrus.Fields{
			"node_id":            node.ID,
			"node_name":          node.Name,
			"healthy_after":      healthyExcludingCandidate,
			"replication_factor": factor,
		}).Warn("Refusing to mark node dead: would drop cluster below replication factor (last-survivor protection)")

		// Surface this to operators via the degraded-state path so the UI
		// banner explains the situation.
		r.setClusterDegradedReason(ctx, fmt.Sprintf(
			"node %q has been unavailable past the dead-node threshold but cannot be transitioned to dead — only %d healthy node(s) remain and replication factor is %d. Add capacity to the cluster.",
			node.Name, healthyExcludingCandidate, factor,
		))
		r.emitEvent(DeadNodeEvent{
			Kind:         EventClusterDegraded,
			NodeID:       node.ID,
			NodeName:     node.Name,
			Reason:       "last-survivor protection: cannot mark dead without dropping below replication factor",
			Factor:       factor,
			NonDeadNodes: healthyExcludingCandidate,
		})
		return ErrBelowReplicationFactor
	}

	// Writes are not noted for a dead node: should it answer again, it is
	// caught up from now at the latest.
	now := time.Now()
	result, err := tx.ExecContext(ctx, `
		UPDATE cluster_nodes
		SET health_status = ?, updated_at = ?, replica_missed_since = COALESCE(replica_missed_since, ?)
		WHERE id = ? AND health_status != ?
	`, HealthStatusDead, now, now.Unix(), node.ID, HealthStatusDead)
	if err != nil {
		return fmt.Errorf("mark dead: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mark-dead transaction: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// Node was already dead (concurrent call won the race); skip the SSE event.
		return nil
	}

	r.log.WithFields(logrus.Fields{
		"node_id":   node.ID,
		"node_name": node.Name,
		"reason":    reason,
	}).Warn("Node marked dead and scheduled for redistribution")

	r.emitEvent(DeadNodeEvent{
		Kind:     EventNodeDead,
		NodeID:   node.ID,
		NodeName: node.Name,
		Reason:   reason,
		Factor:   factor,
	})

	if r.syncer != nil {
		r.Spawn(func() { r.syncer.Trigger(ctx) })
	}
	return nil
}

// ErrBelowReplicationFactor is returned when marking a node dead would leave
// fewer healthy nodes than the replication factor.
var ErrBelowReplicationFactor = errors.New("marking the node dead would leave fewer healthy nodes than the replication factor")

// recomputeClusterDegradedState compares healthy node count against the
// replication factor. Sets/clears the degraded reason and emits SSE events on
// transitions.
func (r *DeadNodeReconciler) recomputeClusterDegradedState(ctx context.Context) {
	factor, err := r.mgr.GetReplicationFactor(ctx)
	if err != nil || factor <= 1 {
		// Factor 1 (no replication) cannot be degraded.
		r.clearClusterDegradedReason(ctx, factor, 0)
		return
	}

	var healthy int
	if err := r.mgr.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cluster_nodes WHERE health_status = ?`, HealthStatusHealthy,
	).Scan(&healthy); err != nil {
		r.log.WithError(err).Warn("Failed to count healthy nodes for degraded-state check")
		return
	}

	prevReason, _ := GetGlobalConfig(ctx, r.mgr.db, clusterDegradedReasonKey)

	if healthy < factor {
		consequence := "writes are kept on fewer nodes than the factor; the nodes that miss them are caught up when they are back"
		if accepts, err := r.mgr.ClusterCanAcceptWrites(ctx); err == nil && !accepts {
			consequence = "writes are refused with 503 until a node is back"
		}
		newReason := fmt.Sprintf("cluster has %d healthy node(s), replication factor is %d: %s", healthy, factor, consequence)
		// Avoid noisy re-emission if the reason hasn't changed.
		if prevReason != newReason {
			r.setClusterDegradedReason(ctx, newReason)
			r.emitEvent(DeadNodeEvent{
				Kind:         EventClusterDegraded,
				Reason:       newReason,
				Factor:       factor,
				NonDeadNodes: healthy,
			})
		}
		return
	}
	r.clearClusterDegradedReason(ctx, factor, healthy)
}

func (r *DeadNodeReconciler) setClusterDegradedReason(ctx context.Context, reason string) {
	if err := SetGlobalConfig(ctx, r.mgr.db, clusterDegradedReasonKey, reason); err != nil {
		r.log.WithError(err).Warn("Failed to persist cluster degraded reason")
	}
}

func (r *DeadNodeReconciler) clearClusterDegradedReason(ctx context.Context, factor, healthy int) {
	prev, _ := GetGlobalConfig(ctx, r.mgr.db, clusterDegradedReasonKey)
	if prev == "" {
		return
	}
	if err := SetGlobalConfig(ctx, r.mgr.db, clusterDegradedReasonKey, ""); err != nil {
		r.log.WithError(err).Warn("Failed to clear cluster degraded reason")
		return
	}
	r.log.WithFields(logrus.Fields{
		"factor":  factor,
		"healthy": healthy,
	}).Info("Cluster degraded state resolved")
	r.emitEvent(DeadNodeEvent{
		Kind:         EventClusterDegradedResolved,
		Reason:       "healthy node count restored",
		Factor:       factor,
		NonDeadNodes: healthy,
	})
}

func (r *DeadNodeReconciler) emitEvent(ev DeadNodeEvent) {
	if r.emit == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			r.log.WithField("panic", rec).Warn("Event emitter panicked")
		}
	}()
	r.emit(ev)
}

// ── Live config helpers ─────────────────────────────────────────────────────

func (r *DeadNodeReconciler) deadThreshold(ctx context.Context) time.Duration {
	v, err := GetGlobalConfig(ctx, r.mgr.db, deadNodeConfigKey)
	if err != nil || v == "" {
		return defaultDeadNodeThresholdHours * time.Hour
	}
	hours, err := strconv.Atoi(v)
	if err != nil || hours <= 0 {
		return defaultDeadNodeThresholdHours * time.Hour
	}
	return time.Duration(hours) * time.Hour
}

func (r *DeadNodeReconciler) checkInterval(ctx context.Context) time.Duration {
	v, err := GetGlobalConfig(ctx, r.mgr.db, redistributionIntervalKey)
	if err != nil || v == "" {
		return defaultRedistributionCheckInterval
	}
	mins, err := strconv.Atoi(v)
	if err != nil || mins <= 0 {
		return defaultRedistributionCheckInterval
	}
	return time.Duration(mins) * time.Minute
}

func (r *DeadNodeReconciler) redistributionEnabled(ctx context.Context) bool {
	v, err := GetGlobalConfig(ctx, r.mgr.db, redistributionEnabledKey)
	if err != nil || v == "" {
		return true
	}
	return v == "true" || v == "1"
}

// ClusterDegradedReason returns the persisted degraded reason ("" when the
// cluster is healthy). Exposed for the console handler so the UI can render
// the banner without round-tripping back through SSE state.
func ClusterDegradedReason(ctx context.Context, db *sql.DB) string {
	v, _ := GetGlobalConfig(ctx, db, clusterDegradedReasonKey)
	return v
}
