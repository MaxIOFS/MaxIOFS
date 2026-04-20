package cluster

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── ClusterCanAcceptWrites ─────────────────────────────────────────────────────

// setupQuorumTestDB returns a DB with both core and replication schemas so that
// SetReplicationFactor (which writes cluster_global_config) works.
func setupQuorumTestDB(t *testing.T) (*sql.DB, func()) {
	db, cleanup := setupTestDB(t)
	if err := InitReplicationSchema(db); err != nil {
		cleanup()
		t.Fatalf("InitReplicationSchema: %v", err)
	}
	return db, cleanup
}

func TestClusterCanAcceptWrites_ClusterDisabled(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")

	ok, err := mgr.ClusterCanAcceptWrites(context.Background())
	require.NoError(t, err)
	assert.True(t, ok, "single-node mode should always accept writes")
}

func TestClusterCanAcceptWrites_FactorOneAlwaysOK(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 1))

	ok, err := mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "factor=1 needs no replicas")
}

func TestClusterCanAcceptWrites_FactorTwoNeedsZeroReplicas(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 2))

	// No peers added — factor=2 still passes because needed replicas = ceil(2/2)-1 = 0.
	ok, err := mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "factor=2 should accept writes even with no replicas (best-effort)")
}

func TestClusterCanAcceptWrites_FactorThreeRejectsWhenNoHealthyPeers(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 3))

	// Add an UNHEALTHY peer — should not satisfy quorum.
	require.NoError(t, mgr.AddNode(ctx, &Node{
		Name: "peer-down", Endpoint: "http://peer-down:8080", NodeToken: "t",
		Region: "us-east-1", Priority: 100, Metadata: "{}",
		HealthStatus: HealthStatusUnavailable,
	}))

	ok, err := mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "factor=3 with no healthy peers must reject writes")
}

func TestClusterCanAcceptWrites_FactorThreeAcceptsWithOneHealthyPeer(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 3))

	// One healthy peer is exactly the quorum threshold for factor=3.
	// AddNode always seeds with HealthStatusUnknown, so flip to Healthy explicitly.
	peer := &Node{
		Name: "peer-up", Endpoint: "http://peer-up:8080", NodeToken: "t",
		Region: "us-east-1", Priority: 100, Metadata: "{}",
	}
	require.NoError(t, mgr.AddNode(ctx, peer))
	_, err = db.ExecContext(ctx,
		`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`,
		HealthStatusHealthy, peer.ID)
	require.NoError(t, err)

	ok, err := mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "factor=3 with 1 healthy peer should accept writes")
}

// ── collectAndCheckQuorum ──────────────────────────────────────────────────────

func TestCollectAndCheckQuorum_AllSuccess(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	h := &HAObjectManager{mgr: mgr}

	ch := make(chan fanoutResult, 2)
	ch <- fanoutResult{nodeID: "n1", err: nil}
	ch <- fanoutResult{nodeID: "n2", err: nil}

	err := h.collectAndCheckQuorum(context.Background(), ch, 2, 1, "PUT", "b", "k")
	assert.NoError(t, err)
}

func TestCollectAndCheckQuorum_QuorumMet(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	// Pre-seed the node so the UPDATE side-effect can find it.
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO cluster_nodes (id, name, endpoint, api_url, node_token, region, priority,
		 health_status, latency_ms, capacity_total, capacity_used, bucket_count, metadata, created_at, updated_at)
		 VALUES ('n2', 'peer2', 'http://x', 'http://x', 't', 'r', 1, ?, 0, 0, 0, 0, '{}', ?, ?)`,
		HealthStatusHealthy, time.Now(), time.Now())
	require.NoError(t, err)
	h := &HAObjectManager{mgr: mgr}

	ch := make(chan fanoutResult, 2)
	ch <- fanoutResult{nodeID: "n1", err: nil}
	ch <- fanoutResult{nodeID: "n2", err: errors.New("boom")}

	// 1 success >= needed=1 → no error, but n2 must be marked unavailable.
	err = h.collectAndCheckQuorum(context.Background(), ch, 2, 1, "PUT", "b", "k")
	assert.NoError(t, err)

	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n2'`).Scan(&status))
	assert.Equal(t, HealthStatusUnavailable, status, "failed peer must be marked unavailable")
}

func TestCollectAndCheckQuorum_QuorumMissed(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	h := &HAObjectManager{mgr: mgr}

	ch := make(chan fanoutResult, 2)
	ch <- fanoutResult{nodeID: "n1", err: errors.New("boom1")}
	ch <- fanoutResult{nodeID: "n2", err: errors.New("boom2")}

	err := h.collectAndCheckQuorum(context.Background(), ch, 2, 1, "PUT", "b", "k")
	assert.ErrorIs(t, err, ErrClusterDegraded)
}

// ── HA context markers ─────────────────────────────────────────────────────────

func TestHARollbackContext(t *testing.T) {
	ctx := context.Background()
	assert.False(t, isHARollback(ctx))
	assert.False(t, isHAReplica(ctx))

	rb := WithHARollbackContext(ctx)
	assert.True(t, isHARollback(rb))
	assert.False(t, isHAReplica(rb), "rollback marker must not also flag as replica")

	rep := WithHAReplicaContext(ctx)
	assert.True(t, isHAReplica(rep))
	assert.False(t, isHARollback(rep), "replica marker must not also flag as rollback")
}
