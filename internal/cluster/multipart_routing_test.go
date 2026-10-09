package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/maxiofs/maxiofs/internal/object"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A multipart upload is made on the node its ID names, whatever the factor;
// with an ID naming no node, outside a cluster or on its own node, it is made
// where the request is.
func TestTheNodeOfAMultipartUpload(t *testing.T) {
	ctx := context.Background()
	db := setupDeadNodeReconcilerDB(t)
	mgr := newTestManager(t, db)

	setReplicationFactor(t, db, 2)
	_, local, err := mgr.UploadNode(ctx, "other-node.0123abcd")
	require.NoError(t, err)
	assert.True(t, local, "outside a cluster, whatever factor a former cluster left")

	enableCluster(t, db)
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "other-node", "other", HealthStatusHealthy, nil)
	for _, factor := range []int{1, 2} {
		setReplicationFactor(t, db, factor)
		node, local, err := mgr.UploadNode(ctx, "other-node.0123abcd")
		require.NoError(t, err)
		assert.False(t, local, "factor %d", factor)
		require.NotNil(t, node)
		assert.Equal(t, "other-node", node.ID)
	}
	for _, id := range []string{"local-node.0123abcd", "0123abcd"} {
		_, local, err = mgr.UploadNode(ctx, id)
		require.NoError(t, err)
		assert.True(t, local, id)
	}
	_, _, err = mgr.UploadNode(ctx, "gone-node.0123abcd")
	assert.ErrorIs(t, err, ErrNodeNotFound)
}

// The multipart uploads of a bucket started on the other nodes are asked for,
// whatever the factor; a node that does not answer is left out.
func TestThePeersMultipartUploads(t *testing.T) {
	var asked atomic.Int32
	answering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		assert.Equal(t, "tenant/bucket", r.URL.Query().Get("bucket"))
		_ = json.NewEncoder(w).Encode(map[string]any{"uploads": []object.MultipartUpload{{UploadID: "peer.1", Key: "k"}}})
	}))
	defer answering.Close()
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer failing.Close()
	mgr, db, _ := newClusterWithPeers(t, 1, answering, failing)
	ctx := context.Background()

	for i, factor := range []int{1, 2} {
		setReplicationFactor(t, db, factor)
		uploads, err := mgr.PeerMultipartUploads(ctx, "tenant/bucket")
		require.NoError(t, err)
		require.Len(t, uploads, 1, "factor %d", factor)
		assert.Equal(t, "peer.1", uploads[0].UploadID)
		assert.EqualValues(t, i+1, asked.Load())
	}
}
