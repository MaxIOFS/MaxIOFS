package cluster

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parityDB(t *testing.T, statuses ...string) (*sql.DB, *Manager) {
	t.Helper()
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	mgr := newTestManager(t, db)
	require.NoError(t, mgr.SetParity(context.Background(), true))
	for i, status := range statuses {
		id := "local-node"
		if i > 0 {
			id = "n" + string(rune('1'+i))
		}
		insertNode(t, db, id, id, status, nil)
	}
	return db, mgr
}

// Parity is off until turned on; its layout follows the nodes that hold
// data: one parity shard up to four of them, two from five. Dead and drained
// nodes hold none.
func TestTheProtectionAndTheLayoutOfParity(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	p, err := mgr.GetProtection(ctx)
	require.NoError(t, err)
	assert.Equal(t, Protection{Factor: 1}, p)

	require.NoError(t, mgr.SetParity(ctx, true))
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusUnavailable, nil)
	insertNode(t, db, "n3", "n3", HealthStatusStoragePressure, nil)
	insertNode(t, db, "n4", "n4", HealthStatusDead, nil)
	insertNode(t, db, "n5", "n5", HealthStatusHealthy, nil)
	_, err = db.Exec(`UPDATE cluster_nodes SET drained = 1 WHERE id = 'n5'`)
	require.NoError(t, err)
	p, err = mgr.GetProtection(ctx)
	require.NoError(t, err)
	assert.True(t, p.Parity)
	layout, ok, err := mgr.ParityLayout(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock}, layout)
	quorum, err := mgr.quorumFactor(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, quorum)

	insertNode(t, db, "n6", "n6", HealthStatusHealthy, nil)
	insertNode(t, db, "n7", "n7", HealthStatusHealthy, nil)
	layout, _, err = mgr.ParityLayout(ctx)
	require.NoError(t, err)
	assert.Equal(t, erasure.Layout{Data: 3, Parity: 2, Block: erasure.DefaultBlock}, layout)
	quorum, err = mgr.quorumFactor(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, quorum, "two parity shards: a delete needs the quorum of three copies")

	require.NoError(t, mgr.SetParity(ctx, false))
	setReplicationFactor(t, db, 3)
	quorum, err = mgr.quorumFactor(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, quorum)
}

// The data of a write with parity is cut in shards; an empty one and one in
// a cluster too small for shards are kept in whole copies.
func TestTheTargetOfAWrite(t *testing.T) {
	layout := erasure.Layout{Data: 3, Parity: 2, Block: erasure.DefaultBlock}
	parity := Protection{Factor: 1, Parity: true}
	assert.Equal(t, dataTarget{layout: layout}, targetOf(parity, layout, true, 10))
	assert.Equal(t, dataTarget{copies: 3}, targetOf(parity, layout, true, 0))
	assert.Equal(t, dataTarget{copies: 2}, targetOf(parity, erasure.Layout{}, false, 10))
	assert.Equal(t, dataTarget{copies: 2}, targetOf(Protection{Factor: 2}, layout, true, 10))
	assert.Equal(t, 2, targetOf(parity, layout, true, 10).tolerance())
	assert.Equal(t, 1, targetOf(Protection{Factor: 2}, layout, true, 10).tolerance())

	// Two thirds of three equal disks hold data cut in two data shards and
	// one of parity; each shard of a byte is on its own node.
	room := []int64{1000, 1000, 1000}
	assert.EqualValues(t, 2000, dataTarget{layout: erasure.Layout{Data: 2, Parity: 1, Block: 1}}.space(room))
	assert.EqualValues(t, 1500, dataTarget{copies: 2}.space(room))
	assert.EqualValues(t, 200, dataTarget{layout: erasure.Layout{Data: 2, Parity: 1, Block: 1}}.space([]int64{800, 600, 100}))
}

// A write with parity needs another node that takes data; without parity a
// factor of 1 never refuses one.
func TestAWriteWithParityNeedsAnotherNode(t *testing.T) {
	db, mgr := parityDB(t, HealthStatusHealthy, HealthStatusHealthy, HealthStatusUnavailable)
	ctx := context.Background()
	ok, err := mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.True(t, ok)

	_, err = db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id != 'local-node'`, HealthStatusUnavailable)
	require.NoError(t, err)
	ok, err = mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, mgr.SetParity(ctx, false))
	ok, err = mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "a factor of 1")
}

// With parity a node is not marked dead nor drained when fewer than three
// nodes would take data.
func TestParityKeepsThreeNodesTakingData(t *testing.T) {
	db, mgr := parityDB(t, HealthStatusHealthy, HealthStatusHealthy, HealthStatusHealthy)
	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour)
	_, err := db.Exec(`UPDATE cluster_nodes SET health_status = ?, unavailable_since = ? WHERE id = 'n3'`, HealthStatusUnavailable, old)
	require.NoError(t, err)
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, (&captureEmitter{}).emit)
	require.NoError(t, r.RunOnce(ctx))
	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n3'`).Scan(&status))
	assert.Equal(t, HealthStatusUnavailable, status, "two nodes would be left")
	assert.ErrorIs(t, r.DrainNode(ctx, "n2", ""), ErrDrainBelowReplicationFactor)

	insertNode(t, db, "n4", "n4", HealthStatusHealthy, nil)
	require.NoError(t, r.RunOnce(ctx))
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n3'`).Scan(&status))
	assert.Equal(t, HealthStatusDead, status, "three nodes are left")
	insertNode(t, db, "n5", "n5", HealthStatusHealthy, nil)
	assert.NoError(t, r.DrainNode(ctx, "n2", ""))
}

// A cluster with parity is degraded when fewer nodes take data than a write
// is spread over.
func TestParityIsDegradedWithANodeDown(t *testing.T) {
	db, mgr := parityDB(t, HealthStatusHealthy, HealthStatusHealthy, HealthStatusHealthy, HealthStatusHealthy)
	ctx := context.Background()
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, (&captureEmitter{}).emit)
	require.NoError(t, r.RunOnce(ctx))
	assert.Empty(t, ClusterDegradedReason(ctx, db))

	_, err := db.Exec(`UPDATE cluster_nodes SET health_status = ?, unavailable_since = ? WHERE id = 'n4'`, HealthStatusUnavailable, time.Now())
	require.NoError(t, err)
	require.NoError(t, r.RunOnce(ctx))
	reason := ClusterDegradedReason(ctx, db)
	assert.True(t, strings.Contains(reason, "parity spreads each write over 4"), reason)
}
