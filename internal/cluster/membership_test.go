package cluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// membershipNode is a node as another node sends it.
func membershipNode(id, name string, changedAt int64) *JoinPackageNode {
	return &JoinPackageNode{ID: id, Name: name, Endpoint: "https://" + id + ":8082", NodeToken: "tok", Priority: 100, ChangedAt: changedAt}
}

func nodeName(t *testing.T, mgr *Manager, id string) string {
	t.Helper()
	n, err := mgr.GetNode(context.Background(), id)
	if err != nil {
		return ""
	}
	return n.Name
}

// A copy of a node is taken when it changed after this node's copy and after
// any removal of the node; a node removed stays removed.
func TestAMembershipChangeIsOrderedByTime(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	apply := func(list *NodeList) {
		t.Helper()
		list.SourceNodeID = "peer"
		_, err := mgr.ApplyNodeList(ctx, list)
		require.NoError(t, err)
	}

	apply(&NodeList{Nodes: []*JoinPackageNode{membershipNode("n1", "first", 100)}})
	assert.Equal(t, "first", nodeName(t, mgr, "n1"), "a node not held is added")
	apply(&NodeList{Nodes: []*JoinPackageNode{membershipNode("n1", "older", 99)}})
	assert.Equal(t, "first", nodeName(t, mgr, "n1"), "an older copy is refused")
	apply(&NodeList{Nodes: []*JoinPackageNode{membershipNode("n1", "same-second", 100)}})
	assert.Equal(t, "first", nodeName(t, mgr, "n1"), "a copy of the same second keeps this node's")
	apply(&NodeList{Nodes: []*JoinPackageNode{membershipNode("n1", "newer", 101)}})
	assert.Equal(t, "newer", nodeName(t, mgr, "n1"))
	apply(&NodeList{Nodes: []*JoinPackageNode{membershipNode("n1", "earlier-release", 0)}})
	assert.Equal(t, "newer", nodeName(t, mgr, "n1"), "a copy without a change time does not replace one")

	// A removal is final, whenever this node's copy changed: a node joins
	// under a new ID every time.
	apply(&NodeList{Removed: map[string]int64{"n1": 50}})
	assert.Empty(t, nodeName(t, mgr, "n1"), "removed though its copy changed later")
	assert.True(t, mgr.RemovedFromCluster(ctx, "n1"))
	for _, at := range []int64{0, 50, 101, time.Now().Unix() + 3600} {
		apply(&NodeList{Nodes: []*JoinPackageNode{membershipNode("n1", "back", at)}})
		assert.Empty(t, nodeName(t, mgr, "n1"), "a copy changed at %d does not bring it back", at)
	}

	// A node never removes itself because another says so.
	apply(&NodeList{Nodes: []*JoinPackageNode{membershipNode("local-node", "local", 1)}})
	apply(&NodeList{Removed: map[string]int64{"local-node": time.Now().Unix() + 60}})
	assert.Equal(t, "local", nodeName(t, mgr, "local-node"))
}

// Removing a node records the removal after the node's last change, and the
// membership a node sends carries it.
func TestARemovalIsSentWithTheMembership(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	future := time.Now().Unix() + 3600
	require.NoError(t, mgr.AddNode(ctx, &Node{ID: "gone", Name: "gone", Endpoint: "https://gone:8082", NodeToken: "tok", Priority: 100, Metadata: "{}", ChangedAt: future}))
	require.NoError(t, mgr.AddNode(ctx, &Node{ID: "kept", Name: "kept", Endpoint: "https://kept:8082", NodeToken: "tok", Priority: 100, Metadata: "{}"}))
	require.NoError(t, mgr.RemoveNode(ctx, "gone"))

	list, err := mgr.NodeListToSend(ctx, "local-node")
	require.NoError(t, err)
	assert.Equal(t, future+1, list.Removed["gone"])
	ids := map[string]bool{}
	for _, n := range list.Nodes {
		ids[n.ID] = true
	}
	assert.True(t, ids["kept"])
	assert.False(t, ids["gone"])
}

// Editing a node stamps the change after the node's last one, so the edit
// is taken by every node.
func TestANodeEditIsStampedAfterItsLastChange(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	future := time.Now().Unix() + 3600
	require.NoError(t, mgr.AddNode(ctx, &Node{ID: "n1", Name: "n1", Endpoint: "https://n1:8082", NodeToken: "tok", Priority: 100, Metadata: "{}", ChangedAt: future}))
	require.NoError(t, mgr.UpdateNode(ctx, &Node{ID: "n1", Name: "renamed", Priority: 10, Metadata: "{}"}))
	n, err := mgr.GetNode(ctx, "n1")
	require.NoError(t, err)
	assert.Equal(t, "renamed", n.Name)
	assert.Equal(t, future+1, n.ChangedAt)

	require.NoError(t, mgr.AddNode(ctx, &Node{ID: "n2", Name: "n2", Endpoint: "https://n2:8082", NodeToken: "tok", Priority: 100, Metadata: "{}"}))
	n, err = mgr.GetNode(ctx, "n2")
	require.NoError(t, err)
	assert.InDelta(t, time.Now().Unix(), n.ChangedAt, 2, "a node added here is stamped now")

	// Added again with what another node sent, it takes that change's time.
	require.NoError(t, mgr.AddNode(ctx, &Node{ID: "n2", Name: "n2-again", Endpoint: "https://n2:8082", NodeToken: "tok", Priority: 100, Metadata: "{}", ChangedAt: future + 7}))
	n, err = mgr.GetNode(ctx, "n2")
	require.NoError(t, err)
	assert.Equal(t, "n2-again", n.Name)
	assert.Equal(t, future+7, n.ChangedAt)
}

// A node dead by this node's probes is back in service when it answers, and
// is caught up from the time it was marked dead at the latest. A drained
// node stays dead.
func TestADeadNodeIsBackWhenItAnswers(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	}))
	t.Cleanup(healthy.Close)
	var caughtUp []string
	var since time.Time
	mgr.OnReplicaBack(func(nodeID string, s time.Time) {
		caughtUp = append(caughtUp, nodeID)
		since = s
	})

	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusHealthy, nil)
	long := time.Now().Add(-48 * time.Hour)
	for _, id := range []string{"back", "drained"} {
		insertNode(t, db, id, id, HealthStatusUnavailable, &long)
		_, err := db.Exec(`UPDATE cluster_nodes SET endpoint = ? WHERE id = ?`, healthy.URL, id)
		require.NoError(t, err)
	}
	insertNode(t, db, "silent", "silent", HealthStatusUnavailable, &long)
	_, err := db.Exec(`UPDATE cluster_nodes SET endpoint = 'http://127.0.0.1:1' WHERE id = 'silent'`)
	require.NoError(t, err)
	r := NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, nil)
	require.NoError(t, SetGlobalConfig(ctx, db, redistributionEnabledKey, "true"))
	markedAt := time.Now().Unix()
	require.NoError(t, r.RunOnce(ctx))
	for _, id := range []string{"back", "drained", "silent"} {
		n, err := mgr.GetNode(ctx, id)
		require.NoError(t, err)
		require.Equal(t, HealthStatusDead, n.HealthStatus, id)
	}
	require.NoError(t, r.DrainNode(ctx, "drained", ""))

	st, err := mgr.CheckNodeHealth(ctx, "back")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusHealthy, st.Status)
	assert.Equal(t, []string{"back"}, caughtUp)
	assert.InDelta(t, markedAt, since.Unix(), 2, "caught up from when it was marked dead")

	st, err = mgr.CheckNodeHealth(ctx, "drained")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusDead, st.Status)
	st, err = mgr.CheckNodeHealth(ctx, "silent")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusDead, st.Status, "a dead node that does not answer stays dead")
	assert.Equal(t, []string{"back"}, caughtUp)
}

// A node drained on another node is marked dead here; one that would leave
// fewer healthy nodes than the replication factor is not drained at all.
func TestADrainIsTakenByEveryNode(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 2)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "n2", "n2", HealthStatusHealthy, nil)
	insertNode(t, db, "n3", "n3", HealthStatusHealthy, nil)
	syncer := &fakeSyncTrigger{}
	r := NewDeadNodeReconciler(mgr, syncer, nil)

	drained, err := mgr.ApplyNodeList(ctx, &NodeList{SourceNodeID: "n2", Nodes: []*JoinPackageNode{
		{ID: "n3", Name: "n3", Endpoint: "http://n3:8082", NodeToken: "tok", Priority: 100, ChangedAt: time.Now().Unix() + 5, Drained: true},
		{ID: "local-node", Name: "local", Endpoint: "http://local-node:8082", NodeToken: "tok", Priority: 100, ChangedAt: time.Now().Unix() + 5, Drained: true},
	}})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"n3", "local-node"}, drained)
	r.ApplyDrains(ctx)
	n3, err := mgr.GetNode(ctx, "n3")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusDead, n3.HealthStatus)
	assert.Eventually(t, func() bool { return syncer.Calls() == 1 }, time.Second, 10*time.Millisecond, "its copies are made again elsewhere")
	self, err := mgr.GetNode(ctx, "local-node")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusHealthy, self.HealthStatus, "a node does not mark itself dead")

	// The periodic pass takes drains too, with automatic redistribution off.
	insertNode(t, db, "n4", "n4", HealthStatusHealthy, nil)
	_, err = mgr.ApplyNodeList(ctx, &NodeList{SourceNodeID: "n2", Nodes: []*JoinPackageNode{
		{ID: "n4", Name: "n4", Endpoint: "http://n4:8082", NodeToken: "tok", Priority: 100, ChangedAt: time.Now().Unix() + 5, Drained: true},
	}})
	require.NoError(t, err)
	require.NoError(t, SetGlobalConfig(ctx, db, redistributionEnabledKey, "false"))
	require.NoError(t, r.RunOnce(ctx))
	n4, err := mgr.GetNode(ctx, "n4")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusDead, n4.HealthStatus)

	err = r.DrainNode(ctx, "n2", "")
	assert.ErrorIs(t, err, ErrBelowReplicationFactor)
	n2, err := mgr.GetNode(ctx, "n2")
	require.NoError(t, err)
	assert.False(t, n2.Drained)
	assert.Equal(t, HealthStatusHealthy, n2.HealthStatus)
}

// A node joining a cluster takes its membership: what it held of a cluster
// it was in before, its own former entry included, is gone.
func TestJoiningTakesTheClusterMembership(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	mgr := newTestManager(t, db)
	ctx := context.Background()
	insertNode(t, db, "former-self", "me", HealthStatusHealthy, nil)
	insertNode(t, db, "former-peer", "old", HealthStatusHealthy, nil)
	_, err := db.Exec(`INSERT INTO ha_pending_metadata_ops (node_id, bucket, op, created_at) VALUES ('former-peer', 'b', '{}', 1)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO cluster_deletion_log_delivery (node_id, delivered_seq) VALUES ('former-peer', 7)`)
	require.NoError(t, err)

	ca, caKey, err := GenerateCA()
	require.NoError(t, err)
	require.NoError(t, mgr.AcceptClusterJoin(ctx, &ClusterJoinPackage{
		NodeID: "new-self", NodeName: "me", ClusterToken: "tok", CACertPEM: string(ca), CAKeyPEM: string(caKey),
		SelfEndpoint: "https://127.0.0.1:8082",
		Nodes:        []*JoinPackageNode{{ID: "primary", Name: "primary", Endpoint: "https://10.0.0.1:8082", NodeToken: "tok", Priority: 100, ChangedAt: 42}},
	}))
	nodes, err := mgr.ListNodes(ctx)
	require.NoError(t, err)
	var ids []string
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	assert.ElementsMatch(t, []string{"new-self", "primary"}, ids)
	primary, err := mgr.GetNode(ctx, "primary")
	require.NoError(t, err)
	assert.EqualValues(t, 42, primary.ChangedAt, "the cluster's change time is kept")
	assert.False(t, mgr.hasQueuedMetadataOps(ctx, "former-peer"))
	var marks int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM cluster_deletion_log_delivery`).Scan(&marks))
	assert.Zero(t, marks, "no deletion is counted as sent to the cluster's nodes")
}

// The degraded reason says what happens to writes: kept on fewer nodes while
// the cluster accepts them, refused when it does not. It is this node's own
// view and is not sent to the other nodes.
func TestTheDegradedReasonSaysWhatHappensToWrites(t *testing.T) {
	for _, c := range []struct {
		factor  int
		refused bool
	}{{2, false}, {3, true}} {
		db := setupDeadNodeReconcilerDB(t)
		enableCluster(t, db)
		setReplicationFactor(t, db, c.factor)
		mgr := newTestManager(t, db)
		ctx := context.Background()
		insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
		insertNode(t, db, "down-1", "down-1", HealthStatusUnavailable, nil)
		insertNode(t, db, "down-2", "down-2", HealthStatusUnavailable, nil)
		accepts, err := mgr.ClusterCanAcceptWrites(ctx)
		require.NoError(t, err)
		require.Equal(t, !c.refused, accepts)

		NewDeadNodeReconciler(mgr, &fakeSyncTrigger{}, nil).recomputeClusterDegradedState(ctx)
		reason, err := GetGlobalConfig(ctx, db, clusterDegradedReasonKey)
		require.NoError(t, err)
		assert.Equal(t, c.refused, strings.Contains(reason, "refused"), "factor %d: %s", c.factor, reason)
		assert.Equal(t, !c.refused, strings.Contains(reason, "caught up"), "factor %d: %s", c.factor, reason)

		entries, err := NewGlobalConfigSyncManager(db, mgr).listGlobalConfig(ctx)
		require.NoError(t, err)
		for _, e := range entries {
			assert.NotEqual(t, clusterDegradedReasonKey, e.Key, "not sent")
		}
	}
}
