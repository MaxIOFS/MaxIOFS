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

// classDB is a cluster of healthy nodes local-node, n2, n3…, with STANDARD
// kept in factor copies.
func classDB(t *testing.T, nodes, factor int) (*sql.DB, *Manager) {
	t.Helper()
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	mgr := newTestManager(t, db)
	setReplicationFactor(t, db, factor)
	for i := range nodes {
		id := "local-node"
		if i > 0 {
			id = "n" + string(rune('1'+i))
		}
		insertNode(t, db, id, id, HealthStatusHealthy, nil)
	}
	return db, mgr
}

func nodeClass(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var class string
	require.NoError(t, db.QueryRow(`SELECT storage_class FROM cluster_nodes WHERE id = ?`, id).Scan(&class))
	return class
}

// The classes a cluster keeps beyond STANDARD, each with its protection, are
// set, changed and deleted; deleting one puts its nodes back in STANDARD.
func TestTheStorageClassesOfACluster(t *testing.T) {
	db, mgr := classDB(t, 3, 1)
	ctx := context.Background()
	classes, err := mgr.StorageClasses(ctx)
	require.NoError(t, err)
	assert.Empty(t, classes)

	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "DEEP_ARCHIVE", Factor: 3, Parity: true}))
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 2}))
	assert.ErrorIs(t, mgr.SetStorageClass(ctx, StorageClass{Name: "STANDARD", Factor: 2}), ErrStorageClassUnknown)
	assert.ErrorIs(t, mgr.SetStorageClass(ctx, StorageClass{Name: "COLD", Factor: 2}), ErrStorageClassUnknown)
	assert.Error(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 4}))
	classes, err = mgr.StorageClasses(ctx)
	require.NoError(t, err)
	assert.Equal(t, []StorageClass{{Name: "GLACIER", Factor: 2}, {Name: "DEEP_ARCHIVE", Factor: 1, Parity: true}}, classes,
		"from the warmest; parity takes no factor")

	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 3}))
	classes, err = mgr.StorageClasses(ctx)
	require.NoError(t, err)
	assert.Equal(t, StorageClass{Name: "GLACIER", Factor: 3}, classes[0])

	before := changedAt(t, db, "n2")
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n2", "GLACIER"))
	assert.Equal(t, "GLACIER", nodeClass(t, db, "n2"))
	assert.Greater(t, changedAt(t, db, "n2"), before, "the class travels with the node's row")
	assert.ErrorIs(t, mgr.SetNodeStorageClass(ctx, "n3", "STANDARD_IA"), ErrStorageClassUnknown)
	assert.ErrorIs(t, mgr.SetNodeStorageClass(ctx, "absent", "GLACIER"), ErrNodeNotFound)

	require.NoError(t, mgr.DeleteStorageClass(ctx, "GLACIER"))
	assert.Equal(t, "", nodeClass(t, db, "n2"), "back in STANDARD")
	assert.ErrorIs(t, mgr.DeleteStorageClass(ctx, "GLACIER"), ErrStorageClassUnknown)
	classes, err = mgr.StorageClasses(ctx)
	require.NoError(t, err)
	assert.Equal(t, []StorageClass{{Name: "DEEP_ARCHIVE", Factor: 1, Parity: true}}, classes)
}

func changedAt(t *testing.T, db *sql.DB, id string) int64 {
	t.Helper()
	var changed int64
	require.NoError(t, db.QueryRow(`SELECT changed_at FROM cluster_nodes WHERE id = ?`, id).Scan(&changed))
	return changed
}

// A node leaves its class only when the nodes left in it that take data are
// as many as its protection needs; a node that is down or drained counts for
// none.
func TestANodeLeavesItsClassOnlyWhenTheClassKeepsItsProtection(t *testing.T) {
	db, mgr := classDB(t, 5, 2)
	ctx := context.Background()
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 2}))
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n2", "GLACIER"))
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n3", "GLACIER"))
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n4", "GLACIER"), "STANDARD keeps two nodes")
	assert.ErrorIs(t, mgr.SetNodeStorageClass(ctx, "n5", "GLACIER"), ErrStorageClassBelowProtection, "STANDARD would keep one node")
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n2", "STANDARD"), "GLACIER keeps two nodes")
	assert.ErrorIs(t, mgr.SetNodeStorageClass(ctx, "n3", "STANDARD"), ErrStorageClassBelowProtection, "GLACIER would keep one node")
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n3", "GLACIER"), "a node already in the class")

	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "DEEP_ARCHIVE", Factor: 1}))
	_, err := db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = 'local-node'`, HealthStatusUnavailable)
	require.NoError(t, err)
	assert.ErrorIs(t, mgr.SetNodeStorageClass(ctx, "n5", "DEEP_ARCHIVE"), ErrStorageClassBelowProtection, "another node of STANDARD is down")
	_, err = db.Exec(`UPDATE cluster_nodes SET health_status = ?, drained = 1 WHERE id = 'local-node'`, HealthStatusHealthy)
	require.NoError(t, err)
	assert.ErrorIs(t, mgr.SetNodeStorageClass(ctx, "n5", "DEEP_ARCHIVE"), ErrStorageClassBelowProtection, "another node of STANDARD is drained")
	_, err = db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = 'n2'`, HealthStatusUnavailable)
	require.NoError(t, err)
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "local-node", "DEEP_ARCHIVE"), "a drained node holds no data of its class")
}

// Each class has its own protection: the layout of a write with parity
// follows the nodes of its class, and so do the quorum of its deletes and
// whether it is taken. A write of a class the cluster does not keep is of
// STANDARD.
func TestTheProtectionOfEachClass(t *testing.T) {
	db, mgr := classDB(t, 6, 2)
	ctx := context.Background()
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Parity: true}))
	for _, id := range []string{"n3", "n4", "n5", "n6"} {
		require.NoError(t, mgr.SetNodeStorageClass(ctx, id, "GLACIER"))
	}
	layout, ok, err := mgr.ClassLayout(ctx, "GLACIER")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, erasure.Layout{Data: 3, Parity: 1, Block: erasure.DefaultBlock}, layout, "four nodes of the class")
	_, ok, err = mgr.ParityLayout(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "two nodes of STANDARD")

	quorum, err := mgr.quorumFactor(ctx, "GLACIER")
	require.NoError(t, err)
	assert.Equal(t, 2, quorum)
	quorum, err = mgr.quorumFactor(ctx, "STANDARD_IA")
	require.NoError(t, err)
	assert.Equal(t, 2, quorum, "the factor of STANDARD")
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "DEEP_ARCHIVE", Factor: 3}))
	quorum, err = mgr.quorumFactor(ctx, "DEEP_ARCHIVE")
	require.NoError(t, err)
	assert.Equal(t, 3, quorum, "a class of no nodes keeps its own")

	target, err := mgr.writeTarget(ctx, "GLACIER", 10)
	require.NoError(t, err)
	assert.Equal(t, dataTarget{layout: layout}, target)
	target, err = mgr.writeTarget(ctx, "", 10)
	require.NoError(t, err)
	assert.Equal(t, dataTarget{copies: 2}, target)

	_, err = db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id != 'local-node'`, HealthStatusUnavailable)
	require.NoError(t, err)
	accepts, err := mgr.canAcceptWrites(ctx, "GLACIER")
	require.NoError(t, err)
	assert.False(t, accepts, "parity needs another node that takes data")
	accepts, err = mgr.canAcceptWrites(ctx, "")
	require.NoError(t, err)
	assert.True(t, accepts, "a mirror takes writes with its peer down")
	_, err = db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = 'n4'`, HealthStatusHealthy)
	require.NoError(t, err)
	accepts, err = mgr.canAcceptWrites(ctx, "GLACIER")
	require.NoError(t, err)
	assert.True(t, accepts)
}

// The room the cluster reports is that of STANDARD's nodes.
func TestTheRoomOfTheClusterIsThatOfStandard(t *testing.T) {
	db, mgr := classDB(t, 3, 1)
	ctx := context.Background()
	_, err := db.Exec(`UPDATE cluster_nodes SET capacity_total = 1000, capacity_used = 0`)
	require.NoError(t, err)
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 1}))
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n3", "GLACIER"))
	capacity, available, ok := mgr.Space(ctx)
	require.True(t, ok)
	assert.EqualValues(t, 2000, capacity)
	assert.EqualValues(t, 2000, available)

	states, err := mgr.StorageClassStatus(ctx)
	require.NoError(t, err)
	require.Len(t, states, 2)
	assert.Equal(t, StorageClassState{Name: "STANDARD", Factor: 1, Nodes: 2, Takers: 2, RequiredNodes: 1,
		TotalBytes: 2000, UsableBytes: 2000}, states[0])
	assert.Equal(t, StorageClassState{Name: "GLACIER", Factor: 1, Nodes: 1, Takers: 1, RequiredNodes: 1,
		TotalBytes: 1000, UsableBytes: 1000}, states[1])

	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 2}))
	states, err = mgr.StorageClassStatus(ctx)
	require.NoError(t, err)
	assert.True(t, states[1].BelowProtection, "one node for two copies")
	assert.Equal(t, 1, states[1].ToleratedFailures)
}

// The writes are repaired when the protection or the nodes of a class change.
func TestTheWritesFollowTheNodesOfTheirClass(t *testing.T) {
	db, mgr := classDB(t, 4, 2)
	ctx := context.Background()
	syncer := &fakeSyncTrigger{}
	r := NewDeadNodeReconciler(mgr, syncer, (&captureEmitter{}).emit)
	t.Cleanup(r.Stop)
	repairs := func(want int) {
		t.Helper()
		require.Eventually(t, func() bool { return syncer.Calls() == want }, 5*time.Second, 10*time.Millisecond)
		time.Sleep(50 * time.Millisecond)
		assert.Equal(t, want, syncer.Calls())
	}
	require.NoError(t, r.RunOnce(ctx))
	repairs(1)

	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 1}))
	require.NoError(t, r.RunOnce(ctx))
	repairs(2)
	require.NoError(t, r.RunOnce(ctx))
	repairs(2)
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n4", "GLACIER"))
	require.NoError(t, r.RunOnce(ctx))
	repairs(3)
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 2}))
	require.NoError(t, r.RunOnce(ctx))
	repairs(4)
	_, err := db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = 'n2'`, HealthStatusUnavailable)
	require.NoError(t, err)
	require.NoError(t, r.RunOnce(ctx))
	repairs(4)
	require.NoError(t, mgr.DeleteStorageClass(ctx, "GLACIER"))
	require.NoError(t, r.RunOnce(ctx))
	repairs(5)
}

// A class with fewer nodes taking data than it spreads a write over makes the
// cluster degraded, by name.
func TestAClassBelowItsProtectionIsDegraded(t *testing.T) {
	db, mgr := classDB(t, 3, 1)
	ctx := context.Background()
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, (&captureEmitter{}).emit)
	t.Cleanup(r.Stop)
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 2}))
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n3", "GLACIER"))
	require.NoError(t, r.RunOnce(ctx))
	reason := ClusterDegradedReason(ctx, db)
	assert.True(t, strings.HasPrefix(reason, "storage class GLACIER: it has 1 healthy node(s), replication factor is 2"), reason)

	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n2", "GLACIER"))
	require.NoError(t, r.RunOnce(ctx))
	assert.Empty(t, ClusterDegradedReason(ctx, db))
}

// A node is drained, or marked dead, only when the nodes left that take data
// are as many as the most demanding class needs.
func TestADrainKeepsTheNodesOfTheMostDemandingClass(t *testing.T) {
	db, mgr := classDB(t, 3, 1)
	ctx := context.Background()
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, (&captureEmitter{}).emit)
	t.Cleanup(r.Stop)
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 3}))
	assert.ErrorIs(t, r.DrainNode(ctx, "n2", ""), ErrDrainBelowReplicationFactor)
	long := time.Now().Add(-48 * time.Hour)
	_, err := db.Exec(`UPDATE cluster_nodes SET health_status = ?, unavailable_since = ? WHERE id = 'n3'`, HealthStatusUnavailable, long)
	require.NoError(t, err)
	require.NoError(t, r.RunOnce(ctx))
	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n3'`).Scan(&status))
	assert.Equal(t, HealthStatusUnavailable, status, "two nodes would be left for three copies")

	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 2}))
	require.NoError(t, r.RunOnce(ctx))
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n3'`).Scan(&status))
	assert.Equal(t, HealthStatusDead, status)
	assert.ErrorIs(t, r.DrainNode(ctx, "n2", ""), ErrDrainBelowReplicationFactor, "the local node alone would be left")
}

// The class of a node travels with its row to every node.
func TestTheClassOfANodeTravelsWithItsRow(t *testing.T) {
	db, mgr := classDB(t, 2, 1)
	ctx := context.Background()
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 1}))
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n2", "GLACIER"))
	list, err := mgr.NodeListToSend(ctx, "local-node")
	require.NoError(t, err)

	otherDB, other := classDB(t, 2, 1)
	_, err = other.ApplyNodeList(ctx, list)
	require.NoError(t, err)
	assert.Equal(t, "GLACIER", nodeClass(t, otherDB, "n2"))
	_ = db
}

// Classes stored that the cluster cannot keep are passed over; a node put
// back in STANDARD keeps no class of its own.
func TestTheStoredClassesAreThoseTheClusterCanKeep(t *testing.T) {
	db, mgr := classDB(t, 3, 1)
	ctx := context.Background()
	require.NoError(t, SetGlobalConfig(ctx, db, storageClassesConfigKey,
		`[{"name":"COLD","factor":2},{"name":"GLACIER","factor":9},{"name":"DEEP_ARCHIVE","factor":2},{"name":"DEEP_ARCHIVE","factor":3}]`))
	classes, err := mgr.StorageClasses(ctx)
	require.NoError(t, err)
	assert.Equal(t, []StorageClass{{Name: "DEEP_ARCHIVE", Factor: 2}}, classes)

	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 1}))
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n2", "GLACIER"))
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n3", "GLACIER"))
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n3", "STANDARD"))
	assert.Equal(t, "", nodeClass(t, db, "n3"))
}

// The settings of the placement change with the protection of a class and
// with the classes kept, not with the nodes in them.
func TestThePlacementSettingsFollowTheClasses(t *testing.T) {
	db, mgr := classDB(t, 3, 1)
	ctx := context.Background()
	settings := mgr.PlacementSettings(ctx)
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 1}))
	kept := mgr.PlacementSettings(ctx)
	assert.NotEqual(t, settings, kept)
	require.NoError(t, mgr.SetNodeStorageClass(ctx, "n2", "GLACIER"))
	assert.Equal(t, kept, mgr.PlacementSettings(ctx))
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 2}))
	assert.NotEqual(t, kept, mgr.PlacementSettings(ctx))
	setReplicationFactor(t, db, 2)
	changed := mgr.PlacementSettings(ctx)
	require.NoError(t, mgr.SetParity(ctx, true))
	assert.NotEqual(t, changed, mgr.PlacementSettings(ctx))
	require.NoError(t, mgr.DeleteStorageClass(ctx, "GLACIER"))
	assert.Equal(t, "STANDARD:2:true", mgr.PlacementSettings(ctx))
}

// A class with parity is below its protection while fewer of its nodes take
// data than a write is cut over, though as many as parity needs.
func TestAClassWithParityIsBelowItsProtectionShortOfItsWidth(t *testing.T) {
	db, mgr := classDB(t, 5, 1)
	ctx := context.Background()
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Parity: true}))
	for _, id := range []string{"n2", "n3", "n4", "n5"} {
		require.NoError(t, mgr.SetNodeStorageClass(ctx, id, "GLACIER"))
	}
	glacier := func() StorageClassState {
		t.Helper()
		states, err := mgr.StorageClassStatus(ctx)
		require.NoError(t, err)
		require.Len(t, states, 2)
		return states[1]
	}
	assert.False(t, glacier().BelowProtection)
	_, err := db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = 'n5'`, HealthStatusUnavailable)
	require.NoError(t, err)
	state := glacier()
	assert.Equal(t, 3, state.DataShards)
	assert.Equal(t, 3, state.Takers)
	assert.True(t, state.BelowProtection, "a write is cut over four nodes")
}

// A drained node that is down is taken out of service at once only when every
// class keeps more than one copy's worth: one of a class kept in one copy is
// waited for.
func TestADrainedNodeDownIsWaitedForWhileAClassKeepsOneCopy(t *testing.T) {
	db, mgr := classDB(t, 4, 2)
	ctx := context.Background()
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, (&captureEmitter{}).emit)
	t.Cleanup(r.Stop)
	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 1}))
	_, err := db.Exec(`UPDATE cluster_nodes SET health_status = ?, unavailable_since = ? WHERE id = 'n4'`, HealthStatusUnavailable, time.Now())
	require.NoError(t, err)
	require.NoError(t, r.DrainNode(ctx, "n4", ""))
	require.NoError(t, r.RunOnce(ctx))
	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n4'`).Scan(&status))
	assert.Equal(t, HealthStatusUnavailable, status, "its copies of GLACIER may be the only ones")

	require.NoError(t, mgr.SetStorageClass(ctx, StorageClass{Name: "GLACIER", Factor: 2}))
	require.NoError(t, r.RunOnce(ctx))
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n4'`).Scan(&status))
	assert.Equal(t, HealthStatusDead, status)
}
