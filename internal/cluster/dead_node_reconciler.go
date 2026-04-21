package cluster

import (
	"context"
	"database/sql"
	"fmt"
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
	EventNodeDead                 DeadNodeEventKind = "node_dead"
	EventClusterDegraded          DeadNodeEventKind = "cluster_degraded"
	EventClusterDegradedResolved  DeadNodeEventKind = "cluster_degraded_resolved"
)

// DeadNodeEvent carries the payload for a reconciler lifecycle event.
type DeadNodeEvent struct {
	Kind     DeadNodeEventKind
	NodeID   string
	NodeName string
	Reason   string
	// Cluster-wide context for monitoring dashboards.
	Factor      int
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
// that have crossed the dead-node threshold, marks them dead, triggers a
// catch-up sync on the remaining healthy replicas, and emits SSE events when
// the cluster as a whole can no longer satisfy the configured replication
// factor.
//
// Concurrency: a single goroutine drives the loop. Each tick processes nodes
// serially under an internal mutex so that a slow Trigger() does not race a
// drain endpoint.
type DeadNodeReconciler struct {
	mgr     *Manager
	syncer  SyncTrigger
	emit    EventEmitter
	log     *logrus.Entry

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

// Start launches the background goroutine. It returns immediately; callers
// cancel ctx to stop the reconciler.
func (r *DeadNodeReconciler) Start(ctx context.Context) {
	go r.run(ctx)
}

func (r *DeadNodeReconciler) run(ctx context.Context) {
	interval := r.checkInterval(ctx)
	r.log.WithField("interval", interval).Info("Dead-node reconciler started")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once shortly after startup so a freshly-restarted node catches any
	// nodes that crossed the threshold while it was down.
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
			if err := r.RunOnce(ctx); err != nil {
				r.log.WithError(err).Warn("Initial reconciliation pass failed")
			}
		}
	}()

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
// threshold, mark them dead (subject to the last-survivor rule), trigger
// catch-up sync, and recompute cluster degraded state. Exported for tests
// and the drain endpoint.
func (r *DeadNodeReconciler) RunOnce(ctx context.Context) error {
	if !r.mgr.IsClusterEnabled() {
		return nil
	}
	if !r.redistributionEnabled(ctx) {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	threshold := r.deadThreshold(ctx)
	cutoff := time.Now().Add(-threshold)

	candidates, err := r.findDeadCandidates(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("find dead candidates: %w", err)
	}

	for _, node := range candidates {
		if err := r.markDeadIfSafe(ctx, node, "threshold exceeded"); err != nil {
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
	if node.HealthStatus == HealthStatusDead {
		return fmt.Errorf("node already dead")
	}
	if reason == "" {
		reason = "manual drain"
	}

	if err := r.markDeadIfSafe(ctx, node, reason); err != nil {
		return err
	}

	r.recomputeClusterDegradedState(ctx)
	return nil
}

// ── Internal helpers ────────────────────────────────────────────────────────

// findDeadCandidates returns nodes whose status is unavailable AND whose
// unavailable_since is older than cutoff. Dead nodes are excluded.
func (r *DeadNodeReconciler) findDeadCandidates(ctx context.Context, cutoff time.Time) ([]*Node, error) {
	rows, err := r.mgr.db.QueryContext(ctx, `
		SELECT id, name, endpoint, api_url, node_token, region, priority,
		       health_status, last_health_check, last_seen, latency_ms,
		       capacity_total, capacity_used, bucket_count, metadata,
		       created_at, updated_at, is_stale, last_local_write_at, unavailable_since
		FROM cluster_nodes
		WHERE health_status = ?
		  AND unavailable_since IS NOT NULL
		  AND unavailable_since <= ?
	`, HealthStatusUnavailable, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Node
	for rows.Next() {
		var n Node
		var lastHealthCheck, lastSeen, lastLocalWriteAt, unavailableSince sql.NullTime
		if err := rows.Scan(
			&n.ID, &n.Name, &n.Endpoint, &n.APIURL, &n.NodeToken, &n.Region, &n.Priority,
			&n.HealthStatus, &lastHealthCheck, &lastSeen, &n.LatencyMs,
			&n.CapacityTotal, &n.CapacityUsed, &n.BucketCount, &n.Metadata,
			&n.CreatedAt, &n.UpdatedAt, &n.IsStale, &lastLocalWriteAt, &unavailableSince,
		); err != nil {
			return nil, err
		}
		if lastHealthCheck.Valid {
			n.LastHealthCheck = &lastHealthCheck.Time
		}
		if lastSeen.Valid {
			n.LastSeen = &lastSeen.Time
		}
		if lastLocalWriteAt.Valid {
			n.LastLocalWriteAt = &lastLocalWriteAt.Time
		}
		if unavailableSince.Valid {
			n.UnavailableSince = &unavailableSince.Time
		}
		out = append(out, &n)
	}
	return out, rows.Err()
}

// markDeadIfSafe marks the node dead unless doing so would drop the count of
// non-dead nodes below the configured replication factor. When refused, the
// reconciler emits cluster_degraded with a "last-survivor protection" reason
// so the admin sees why the node wasn't transitioned.
func (r *DeadNodeReconciler) markDeadIfSafe(ctx context.Context, node *Node, reason string) error {
	factor, err := r.mgr.GetReplicationFactor(ctx)
	if err != nil {
		factor = 1
	}

	nonDead, err := r.countNonDeadNodes(ctx)
	if err != nil {
		return fmt.Errorf("count non-dead nodes: %w", err)
	}
	// nonDead currently INCLUDES the candidate (it's not dead yet). After
	// marking dead, the cluster would have nonDead-1 non-dead nodes.
	if nonDead-1 < factor {
		r.log.WithFields(logrus.Fields{
			"node_id":           node.ID,
			"node_name":         node.Name,
			"non_dead_after":    nonDead - 1,
			"replication_factor": factor,
		}).Warn("Refusing to mark node dead: would drop cluster below replication factor (last-survivor protection)")

		// Surface this to operators via the degraded-state path so the UI
		// banner explains the situation.
		r.setClusterDegradedReason(ctx, fmt.Sprintf(
			"node %q has been unavailable past the dead-node threshold but cannot be transitioned to dead — only %d non-dead node(s) remain and replication factor is %d. Add capacity to the cluster.",
			node.Name, nonDead-1, factor,
		))
		r.emitEvent(DeadNodeEvent{
			Kind:         EventClusterDegraded,
			NodeID:       node.ID,
			NodeName:     node.Name,
			Reason:       "last-survivor protection: cannot mark dead without dropping below replication factor",
			Factor:       factor,
			NonDeadNodes: nonDead - 1,
		})
		return nil
	}

	now := time.Now()
	if _, err := r.mgr.db.ExecContext(ctx, `
		UPDATE cluster_nodes
		SET health_status = ?, updated_at = ?
		WHERE id = ? AND health_status != ?
	`, HealthStatusDead, now, node.ID, HealthStatusDead); err != nil {
		return fmt.Errorf("mark dead: %w", err)
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

	// In this symmetric replication model every healthy node holds every
	// bucket. Triggering the HA sync worker ensures any healthy node that
	// hasn't completed initial sync yet starts catching up so the cluster
	// can rebuild factor across the surviving healthy set.
	if r.syncer != nil {
		go r.syncer.Trigger(ctx)
	}
	return nil
}

// countNonDeadNodes returns the count of nodes whose health_status is anything
// other than HealthStatusDead. Used by the last-survivor check.
func (r *DeadNodeReconciler) countNonDeadNodes(ctx context.Context) (int, error) {
	var n int
	err := r.mgr.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cluster_nodes WHERE health_status != ?`,
		HealthStatusDead,
	).Scan(&n)
	return n, err
}

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
		newReason := fmt.Sprintf(
			"cluster has %d healthy node(s), replication factor is %d — writes will be refused with 503 until the gap closes",
			healthy, factor,
		)
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
