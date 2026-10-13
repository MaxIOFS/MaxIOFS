package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node is marked dead while another writer holds the database: the change
// waits for it. It read the count of the nodes first and then failed with
// SQLITE_BUSY when the other writer committed in between.
func TestMarkDeadWaitsForAnotherWriter(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 1)
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusHealthy, nil)
	insertNode(t, db, "gone", "gone", HealthStatusUnavailable, nil)
	mgr := newTestManager(t, db)
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, (&captureEmitter{}).emit)
	node, err := mgr.GetNode(context.Background(), "gone")
	require.NoError(t, err)

	ctx := context.Background()
	writer, err := db.Conn(ctx)
	require.NoError(t, err)
	defer writer.Close()
	_, err = writer.ExecContext(ctx, "BEGIN IMMEDIATE")
	require.NoError(t, err)
	_, err = writer.ExecContext(ctx, `UPDATE cluster_nodes SET updated_at = ? WHERE id = 'n2'`, time.Now())
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- r.markDeadIfSafe(ctx, node, "test") }()
	time.Sleep(300 * time.Millisecond)
	_, err = writer.ExecContext(ctx, "COMMIT")
	require.NoError(t, err)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("marking the node dead did not end")
	}
	after, err := mgr.GetNode(ctx, "gone")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusDead, after.HealthStatus)
}

// Marking a node that is already dead changes nothing and tells no one; a
// drained node is marked whatever the count of the others.
func TestMarkDeadOfADeadNodeChangesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		factor int
		safe   bool
	}{
		"safe, enough nodes left":   {factor: 1, safe: true},
		"drained, below the factor": {factor: 3, safe: false},
	} {
		t.Run(name, func(t *testing.T) {
			db := setupDeadNodeReconcilerDB(t)
			enableCluster(t, db)
			setReplicationFactor(t, db, tc.factor)
			insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
			insertNode(t, db, "n2", "n2", HealthStatusHealthy, nil)
			insertNode(t, db, "gone", "gone", HealthStatusDead, nil)
			mgr := newTestManager(t, db)
			emitter := &captureEmitter{}
			r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, emitter.emit)
			node, err := mgr.GetNode(context.Background(), "gone")
			require.NoError(t, err)

			require.NoError(t, r.markDead(context.Background(), node, "test", tc.safe))
			assert.Empty(t, emitter.snapshot())
		})
	}
}
