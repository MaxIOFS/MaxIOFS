package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// metadataPeer records the metadata changes it receives, answering with the
// status the test sets for each key (204 by default).
type metadataPeer struct {
	mu       sync.Mutex
	received []HAMetadataOp
	status   map[string]int
}

func newMetadataPeer(t *testing.T) (*metadataPeer, *httptest.Server) {
	p := &metadataPeer{status: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/ha/metadata-op") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var op HAMetadataOp
		_ = json.NewDecoder(r.Body).Decode(&op)
		p.mu.Lock()
		defer p.mu.Unlock()
		if code := p.status[op.Key]; code != 0 {
			w.WriteHeader(code)
			return
		}
		p.received = append(p.received, op)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return p, srv
}

func (p *metadataPeer) keys() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var keys []string
	for _, op := range p.received {
		keys = append(keys, op.Key)
	}
	return keys
}

func queuedKeys(t *testing.T, db *sql.DB, nodeID string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT op FROM ha_pending_metadata_ops WHERE node_id = ? ORDER BY id`, nodeID)
	require.NoError(t, err)
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var body string
		require.NoError(t, rows.Scan(&body))
		var op HAMetadataOp
		require.NoError(t, json.Unmarshal([]byte(body), &op))
		keys = append(keys, op.Key)
	}
	return keys
}

func setLegalHold(t *testing.T, ha *HAObjectManager, key string) {
	t.Helper()
	require.NoError(t, ha.SetObjectLegalHold(context.Background(), "bucket", key, &object.LegalHoldConfig{Status: object.LegalHoldStatusOn}))
}

// A metadata change reaches a healthy replica before the request returns.
func TestMetadataChangeReachesTheReplicaBeforeReturning(t *testing.T) {
	peer, srv := newMetadataPeer(t)
	mgr, db, nodes := newClusterWithPeers(t, 2, srv)
	ha := &HAObjectManager{Manager: &rollbackRecorderManager{}, mgr: mgr}
	setLegalHold(t, ha, "a")
	assert.Equal(t, []string{"a"}, peer.keys())
	assert.Empty(t, queuedKeys(t, db, nodes[0].ID))
}

// A replica that is down, fails, or has changes waiting gets the change
// queued behind them; one that refuses it does not.
func TestMetadataChangeIsQueuedForAReplicaThatCannotTakeItNow(t *testing.T) {
	peer, srv := newMetadataPeer(t)
	mgr, db, nodes := newClusterWithPeers(t, 2, srv)
	ha := &HAObjectManager{Manager: &rollbackRecorderManager{}, mgr: mgr}
	node := nodes[0].ID

	peer.status["fails"] = http.StatusInternalServerError
	setLegalHold(t, ha, "fails")
	assert.Equal(t, []string{"fails"}, queuedKeys(t, db, node))
	assert.True(t, missedSince(t, db, node).Valid, "the replica is caught up when it is next healthy")

	setLegalHold(t, ha, "behind")
	assert.Empty(t, peer.keys(), "a change waits behind the queued ones")
	assert.Equal(t, []string{"fails", "behind"}, queuedKeys(t, db, node))

	_, err := db.Exec(`DELETE FROM ha_pending_metadata_ops`)
	require.NoError(t, err)
	peer.status["gone"] = http.StatusNotFound
	setLegalHold(t, ha, "gone")
	assert.Empty(t, queuedKeys(t, db, node), "a refused change is not queued")

	_, err = db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, HealthStatusUnavailable, node)
	require.NoError(t, err)
	setLegalHold(t, ha, "down")
	assert.Equal(t, []string{"down"}, queuedKeys(t, db, node))
	assert.Empty(t, peer.keys())
}

// Queued changes are delivered in order. A change the replica refuses is
// dropped; a failure stops the replay and keeps the rest in order.
func TestQueuedMetadataChangesAreReplayedInOrder(t *testing.T) {
	peer, srv := newMetadataPeer(t)
	mgr, db, nodes := newClusterWithPeers(t, 2, srv)
	ctx := context.Background()
	node, err := mgr.GetNode(ctx, nodes[0].ID)
	require.NoError(t, err)
	queue := func(key string) {
		body, err := json.Marshal(HAMetadataOp{Op: "set-legal-hold", Key: key})
		require.NoError(t, err)
		mgr.queueMetadataOp(ctx, node.ID, "bucket", body)
	}
	for _, k := range []string{"a", "refused", "b", "stuck", "c"} {
		queue(k)
	}
	peer.status["refused"] = http.StatusConflict
	peer.status["stuck"] = http.StatusServiceUnavailable

	require.Error(t, mgr.replayMetadataOps(ctx, NewProxyClient(nil), node, "local"))
	assert.Equal(t, []string{"a", "b"}, peer.keys())
	assert.Equal(t, []string{"stuck", "c"}, queuedKeys(t, db, node.ID), "the failed change and what follows it stay, in order")

	delete(peer.status, "stuck")
	require.NoError(t, mgr.replayMetadataOps(ctx, NewProxyClient(nil), node, "local"))
	assert.Equal(t, []string{"a", "b", "stuck", "c"}, peer.keys())
	assert.Empty(t, queuedKeys(t, db, node.ID))
}

// The catch-up of a returning node delivers its queued metadata changes.
func TestCatchUpDeliversQueuedMetadataChanges(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "meta"}))
	peer, srv := newMetadataPeer(t)
	mgr, db, nodes := newClusterWithPeers(t, 2, srv)
	body, err := json.Marshal(HAMetadataOp{Op: "set-tagging", Key: "k"})
	require.NoError(t, err)
	mgr.queueMetadataOp(ctx, nodes[0].ID, "meta", body)

	scrubber := NewAntiEntropyScrubber(local.objects, local.buckets, mgr, newFakeRawKV())
	scrubber.CatchUp(nodes[0].ID, time.Now())
	scrubber.runCatchUp(ctx)
	assert.Equal(t, []string{"k"}, peer.keys())
	assert.Empty(t, queuedKeys(t, db, nodes[0].ID))
}

// Changes queued for a node are dropped when it leaves the cluster.
func TestRemovedNodeLeavesNoQueuedMetadataChanges(t *testing.T) {
	_, srv := newMetadataPeer(t)
	mgr, db, nodes := newClusterWithPeers(t, 2, srv)
	ctx := context.Background()
	mgr.queueMetadataOp(ctx, nodes[0].ID, "bucket", []byte(`{"op":"set-tagging","key":"k"}`))
	require.NoError(t, mgr.RemoveNode(ctx, nodes[0].ID))
	assert.Empty(t, queuedKeys(t, db, nodes[0].ID))
}
