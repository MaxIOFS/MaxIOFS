package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// With every node holding every bucket, a bucket is listed once: in the
// console and to S3 clients alike.
func TestABucketOnEveryNodeIsListedOnce(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "everywhere", "admin"))
	require.True(t, hasBucket(p.b, "", "everywhere"))

	listed, err := p.a.bucketAggregator.ListAllBucketsFromAllNodes(ctx, "")
	require.NoError(t, err)
	count := 0
	for _, b := range listed {
		if b.Name == "everywhere" {
			count++
			assert.Equal(t, p.aID, b.NodeID, "the copy of the node asked")
		}
	}
	assert.Equal(t, 1, count)

	w := httptest.NewRecorder()
	p.a.handleListBuckets(w, asGlobalAdmin(httptest.NewRequest(http.MethodGet, "/api/v1/buckets", nil), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Data []BucketResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	count = 0
	for _, b := range body.Data {
		if b.Name == "everywhere" {
			count++
		}
	}
	assert.Equal(t, 1, count, "the console lists it once")
}

// The degraded reason another node sends is its own view and is not taken.
func TestAPeersDegradedReasonIsNotTaken(t *testing.T) {
	s := newClusterTestNode(t)
	w := httptest.NewRecorder()
	s.handleReceiveGlobalConfigSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/global-config-sync", map[string]any{
		"entries": []cluster.GlobalConfigEntry{
			{Key: "ha.cluster_degraded_reason", Value: "seen from the peer", UpdatedAt: time.Now().Unix() + 60},
			{Key: "ha.dead_node_threshold_hours", Value: "48", UpdatedAt: time.Now().Unix() + 60},
		},
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	reason, _ := cluster.GetGlobalConfig(context.Background(), s.db, "ha.cluster_degraded_reason")
	assert.Empty(t, reason)
	threshold, _ := cluster.GetGlobalConfig(context.Background(), s.db, "ha.dead_node_threshold_hours")
	assert.Equal(t, "48", threshold, "the cluster's settings are taken")
}

// A read is served from this node's copy: it is not sent to the other node,
// whose cluster port serves no S3 request.
func TestAReadIsServedHere(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "readable", "admin"))
	_, err := p.a.objectManager.PutObject(ctx, "readable", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	_, err = p.a.shareManager.CreateShare(ctx, "readable", "k", "", "AKID", "secret", "admin", nil)
	require.NoError(t, err)
	_, err = p.a.db.Exec(`INSERT INTO ha_sync_jobs (target_node_id, status) VALUES (?, 'done')`, p.bID)
	require.NoError(t, err)

	code, body := anonymousGet(t, p.a, "readable", "k")
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, "data", body)
	assert.Zero(t, p.bStray.Load(), "nothing is sent to b for the read")
}
