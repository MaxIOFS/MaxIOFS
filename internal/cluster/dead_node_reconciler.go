package cluster

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/maxiofs/maxiofs/internal/bgwork"
	"github.com/maxiofs/maxiofs/internal/storageclass"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// degradedMu orders the computations of the degraded state; recompute is
	// set while one is to come.
	degradedMu sync.Mutex
	recompute  atomic.Bool
	// shape is what the placement of the writes followed when they were last
	// repaired for it (see classView.placementShape).
	shape string
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
			if err := r.mgr.observeTask("dead_node_check", func() error { return r.RunOnce(ctx) }); err != nil {
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

	// A drain is an administrator's decision: it goes on with automatic
	// redistribution off as well.
	r.applyDrains(ctx)
	r.followPlacement(ctx)
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

// followPlacement repairs the writes when what their placement follows
// changes (see classView.placementShape): the protection of a class, the
// layout of a write with parity, once every node of its class that holds
// data takes it, or the nodes of a class. Each node repairs the writes it is
// the first holder of.
func (r *DeadNodeReconciler) followPlacement(ctx context.Context) {
	view, err := r.mgr.classView(ctx)
	if err != nil {
		return
	}
	nodes, err := r.mgr.ListNodes(ctx)
	if err != nil {
		return
	}
	shape, settled := view.placementShape(nodes)
	if !settled || shape == r.shape {
		return
	}
	r.shape = shape
	r.log.WithField("placement", shape).Info("The placement of the writes changed: the writes are repaired")
	if r.syncer != nil {
		r.Spawn(func() { r.syncer.Trigger(ctx) })
	}
}

// DrainNode starts the drain of a node, asked for by an administrator: the
// node takes no new data and its copies are moved to the other nodes (see
// RepairPlacement); once it holds none it is out of service on every node. A
// drain is refused when the other nodes that take data are fewer than the
// most demanding class needs.
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
	view, err := r.mgr.classView(ctx)
	if err != nil {
		return err
	}
	if node.HealthStatus != HealthStatusDead {
		var takers int
		if err := r.mgr.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM cluster_nodes WHERE health_status IN (?, ?) AND drained = 0 AND id != ?`,
			HealthStatusHealthy, HealthStatusDegraded, nodeID,
		).Scan(&takers); err != nil {
			return fmt.Errorf("count the nodes that take data: %w", err)
		}
		if takers < view.requiredNodes() {
			return ErrDrainBelowReplicationFactor
		}
	}
	if err := r.mgr.setDrained(ctx, r.mgr.db, nodeID); err != nil {
		return err
	}
	if reason == "" {
		reason = "manual drain"
	}
	r.log.WithFields(logrus.Fields{"node_id": nodeID, "reason": reason}).Info("Drain started")
	node.Drained = true
	localID, _ := r.mgr.GetLocalNodeID(ctx)
	r.applyDrain(context.WithoutCancel(ctx), node, localID, view)

	r.recomputeClusterDegradedState(ctx)
	return nil
}

// ErrDrainBelowReplicationFactor is returned when the nodes left to hold a
// drained node's copies would be fewer than the replication factor.
var ErrDrainBelowReplicationFactor = errors.New("the other nodes that take data are fewer than the replication factor")

// ApplyDrains acts on the drains made on another node, as the periodic pass
// does.
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
	if len(nodes) == 0 {
		return
	}
	view, err := r.mgr.classView(ctx)
	if err != nil {
		return
	}
	localID, _ := r.mgr.GetLocalNodeID(ctx)
	for _, node := range nodes {
		r.applyDrain(ctx, node, localID, view)
	}
}

// applyDrain acts on a drained node not yet out of service. Another node that
// said it holds no data any more is taken out of service, and so is one not in
// service when its copies are held elsewhere (a factor above 1, or parity, in
// every class). Otherwise the copies are moved on: by every node, this one
// included, and by the drained node itself.
func (r *DeadNodeReconciler) applyDrain(ctx context.Context, node *Node, localID string, view *classView) {
	var err error
	switch {
	case node.ID == localID:
		r.moveCopies(ctx)
		return
	case r.mgr.drainDone(node.ID):
		// It holds no copy: the nodes left need not hold the factor's.
		err = r.markDead(ctx, node, "drained", false)
	case !node.InService() && view.redundant():
		err = r.markDeadIfSafe(ctx, node, "drained while unavailable")
	default:
		r.moveCopies(ctx)
		return
	}
	if err != nil && !errors.Is(err, ErrBelowReplicationFactor) {
		r.log.WithError(err).WithField("node_id", node.ID).Warn("Failed to take a drained node out of service")
	}
}

// moveCopies starts the copy repair, which moves the copies of drained nodes.
func (r *DeadNodeReconciler) moveCopies(ctx context.Context) {
	if r.syncer != nil {
		r.Spawn(func() { r.syncer.Trigger(ctx) })
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
// nodes that take data below the replication factor.
func (r *DeadNodeReconciler) markDeadIfSafe(ctx context.Context, node *Node, reason string) error {
	return r.markDead(ctx, node, reason, true)
}

// markDead marks the node dead; with safe, not when the nodes left that take
// data would be fewer than the most demanding class needs.
func (r *DeadNodeReconciler) markDead(ctx context.Context, node *Node, reason string, safe bool) error {
	factor := 1
	if view, err := r.mgr.classView(ctx); err == nil {
		factor = view.requiredNodes()
	}

	// Count the nodes that take data, healthy or slow and not drained,
	// excluding the candidate. Unavailable, drained and storage-pressure nodes
	// do not contribute to write capacity and are not counted, so an
	// unavailable node reaching the dead threshold never inflates the safety
	// count.
	const others = `SELECT COUNT(*) FROM cluster_nodes WHERE health_status IN (?, ?) AND drained = 0 AND id != ?`
	required := 0
	if safe {
		required = factor
	}

	// The count and the change are one statement: SQLite takes the write lock
	// for it, waiting for another writer, where a transaction that had read
	// before that writer committed failed with SQLITE_BUSY.
	// Writes are not noted for a dead node, and the copies it held are made
	// again elsewhere: should it answer again, it is compared on every write.
	now := time.Now()
	result, err := r.mgr.db.ExecContext(ctx, `
		UPDATE cluster_nodes
		SET health_status = ?, updated_at = ?, replica_missed_since = ?
		WHERE id = ? AND health_status != ? AND (`+others+`) >= ?
	`, HealthStatusDead, now, wholeCatchUp, node.ID, HealthStatusDead,
		HealthStatusHealthy, HealthStatusDegraded, node.ID, required)
	if err != nil {
		return fmt.Errorf("mark dead: %w", err)
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		if !safe {
			// Already dead.
			return nil
		}
		var healthyExcludingCandidate int
		if err := r.mgr.db.QueryRowContext(ctx, others, HealthStatusHealthy, HealthStatusDegraded, node.ID).
			Scan(&healthyExcludingCandidate); err != nil {
			return fmt.Errorf("count healthy nodes: %w", err)
		}
		if healthyExcludingCandidate >= factor {
			// Already dead.
			return nil
		}
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

// wholeCatchUp is the time (unix seconds) a node is noted as having missed
// writes from when it is to be compared on every write once it is back.
const wholeCatchUp = 1

// ErrBelowReplicationFactor is returned when marking a node dead would leave
// fewer healthy nodes than the replication factor.
var ErrBelowReplicationFactor = errors.New("marking the node dead would leave fewer healthy nodes than the replication factor")

// recomputeClusterDegradedState compares, in each class, the nodes that take
// data against what its protection spreads a write over. Sets/clears the
// degraded reason and emits SSE events on transitions.
func (r *DeadNodeReconciler) recomputeClusterDegradedState(ctx context.Context) {
	r.degradedMu.Lock()
	defer r.degradedMu.Unlock()
	view, err := r.mgr.classView(ctx)
	if err != nil {
		return
	}
	nodes, err := r.mgr.ListNodes(ctx)
	if err != nil {
		r.log.WithError(err).Warn("Failed to count healthy nodes for degraded-state check")
		return
	}
	factor := view.protection[storageclass.Standard].Factor
	prevReason, _ := GetGlobalConfig(ctx, r.mgr.db, clusterDegradedReasonKey)

	var reasons []string
	healthy := 0
	for _, class := range view.names() {
		protection := view.protection[class]
		if !protection.redundant() {
			// A factor of 1 (no replication) cannot be degraded.
			continue
		}
		takers := view.takers(nodes, class)
		needed := protection.Factor
		if protection.Parity {
			// Every write is cut over every node holding data, up to its shards.
			needed = 2
			if layout, ok := view.layout(nodes, class); ok {
				needed = layout.Width()
			}
		}
		if takers >= needed {
			continue
		}
		consequence := "writes are kept on fewer nodes than the factor; the nodes that miss them are caught up when they are back"
		if protection.Parity {
			consequence = "writes are cut in fewer shards, or kept in whole copies, until the nodes are back"
		}
		if class != storageclass.Standard {
			consequence = "its writes are kept on nodes of other classes as well, until it has the nodes"
		}
		if accepts, err := r.mgr.canAcceptWrites(ctx, class); err == nil && !accepts {
			consequence = "writes are refused with 503 until a node is back"
		}
		reason := fmt.Sprintf("cluster has %d healthy node(s), replication factor is %d: %s", takers, protection.Factor, consequence)
		if protection.Parity {
			reason = fmt.Sprintf("cluster has %d healthy node(s), parity spreads each write over %d: %s", takers, needed, consequence)
		}
		if class != storageclass.Standard {
			reason = "storage class " + class + ": " + strings.Replace(reason, "cluster has", "it has", 1)
		} else {
			healthy = takers
		}
		reasons = append(reasons, reason)
	}
	if len(reasons) == 0 {
		r.clearClusterDegradedReason(ctx, factor, view.takers(nodes, storageclass.Standard))
		return
	}
	newReason := strings.Join(reasons, "; ")
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

// RecomputeDegradedState computes the degraded state again in the background.
// A call made while one is to come is folded into it.
func (r *DeadNodeReconciler) RecomputeDegradedState(ctx context.Context) {
	if !r.recompute.CompareAndSwap(false, true) {
		return
	}
	if !r.Spawn(func() {
		r.recompute.Store(false)
		r.recomputeClusterDegradedState(ctx)
	}) {
		r.recompute.Store(false)
	}
}

// DegradedReason returns the degraded reason of the nodes as they are now (""
// when the cluster is healthy).
func (r *DeadNodeReconciler) DegradedReason(ctx context.Context) string {
	if r.mgr.IsClusterEnabled() {
		r.recomputeClusterDegradedState(ctx)
	}
	return ClusterDegradedReason(ctx, r.mgr.db)
}

// ClusterDegradedReason returns the persisted degraded reason ("" when the
// cluster is healthy). Exposed for the console handler so the UI can render
// the banner without round-tripping back through SSE state.
func ClusterDegradedReason(ctx context.Context, db *sql.DB) string {
	v, _ := GetGlobalConfig(ctx, db, clusterDegradedReasonKey)
	return v
}
