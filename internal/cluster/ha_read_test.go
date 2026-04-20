package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addReadyReplica inserts a healthy node with a completed sync job so that
// GetReadyReplicaNodes returns it. latencyMs feeds SelectReadNodes ordering.
func addReadyReplica(t *testing.T, db *sql.DB, id, name, endpoint string, latencyMs int) {
	t.Helper()
	now := time.Now()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO cluster_nodes (id, name, endpoint, api_url, node_token, region, priority,
		 health_status, latency_ms, capacity_total, capacity_used, bucket_count, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'token', 'r', 100, ?, ?, 0, 0, 0, '{}', ?, ?)`,
		id, name, endpoint, endpoint, HealthStatusHealthy, latencyMs, now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO ha_sync_jobs (target_node_id, status, started_at, completed_at) VALUES (?, ?, ?, ?)`,
		id, SyncJobDone, now, now)
	require.NoError(t, err)
}

// ── SelectReadNodes ────────────────────────────────────────────────────────────

func TestSelectReadNodes_EmptyWhenClusterDisabled(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")

	nodes, err := mgr.SelectReadNodes(context.Background(), "any")
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestSelectReadNodes_EmptyWhenFactorOne(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local", "r", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 1))

	nodes, err := mgr.SelectReadNodes(ctx, "bucket")
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestSelectReadNodes_EmptyWhenNoReadyReplicas(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local", "r", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 3))

	// Add an unhealthy peer + a healthy peer with no sync job — neither qualifies.
	now := time.Now()
	_, err = db.ExecContext(ctx, `INSERT INTO cluster_nodes (id, name, endpoint, api_url, node_token, region, priority,
		 health_status, latency_ms, capacity_total, capacity_used, bucket_count, metadata, created_at, updated_at)
		 VALUES ('p1', 'p1', 'http://p1', 'http://p1', 't', 'r', 1, ?, 5, 0, 0, 0, '{}', ?, ?)`,
		HealthStatusUnavailable, now, now)
	require.NoError(t, err)

	nodes, err := mgr.SelectReadNodes(ctx, "bucket")
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestSelectReadNodes_SortedByLatencyAndRotated(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local", "r", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 3))

	// Insert in a non-sorted order to confirm the sort runs.
	addReadyReplica(t, db, "slow", "slow", "http://slow", 50)
	addReadyReplica(t, db, "fast", "fast", "http://fast", 5)
	addReadyReplica(t, db, "mid", "mid", "http://mid", 20)

	// Reset the counter so we can predict the rotation.
	atomic.StoreUint64(&mgr.readCounter, 0)

	first, err := mgr.SelectReadNodes(ctx, "bucket")
	require.NoError(t, err)
	require.Len(t, first, 3)
	// readCounter went 0→1; 1 % 3 == 1, so rotation starts at index 1 of the
	// sorted list [fast, mid, slow] → expect [mid, slow, fast].
	assert.Equal(t, []string{"mid", "slow", "fast"}, []string{first[0].ID, first[1].ID, first[2].ID})

	second, err := mgr.SelectReadNodes(ctx, "bucket")
	require.NoError(t, err)
	require.Len(t, second, 3)
	// counter 1→2; 2 % 3 == 2 → start at index 2 → [slow, fast, mid]
	assert.Equal(t, []string{"slow", "fast", "mid"}, []string{second[0].ID, second[1].ID, second[2].ID})

	third, err := mgr.SelectReadNodes(ctx, "bucket")
	require.NoError(t, err)
	// counter 2→3; 3 % 3 == 0 → start at index 0 → [fast, mid, slow]
	assert.Equal(t, []string{"fast", "mid", "slow"}, []string{third[0].ID, third[1].ID, third[2].ID})
}

// ── TryProxyRead ───────────────────────────────────────────────────────────────

// captureWriter records WriteHeader + writes so tests can assert that nothing
// reached the client when TryProxyRead returned served=false.
type captureWriter struct {
	header http.Header
	status int
	body   strings.Builder
	wrote  bool
}

func newCaptureWriter() *captureWriter { return &captureWriter{header: http.Header{}} }
func (c *captureWriter) Header() http.Header { return c.header }
func (c *captureWriter) WriteHeader(s int)   { c.status = s; c.wrote = true }
func (c *captureWriter) Write(b []byte) (int, error) {
	c.wrote = true
	return c.body.Write(b)
}

// peerWithStatus stands up an httptest server that always responds with the
// given status and body.
func peerWithStatus(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

// readReq builds a minimal *http.Request the proxy code expects (it reads
// Method/URL.RequestURI()/Body/Header).
func readReq(t *testing.T) *http.Request {
	t.Helper()
	u, err := url.Parse("/buckets/b/objects/k")
	require.NoError(t, err)
	return &http.Request{Method: "GET", URL: u, Header: http.Header{}}
}

func TestTryProxyRead_2xxServesAndReturnsTrue(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")

	srv := peerWithStatus(t, 200, "hello")
	defer srv.Close()
	node := &Node{ID: "p1", Endpoint: srv.URL, NodeToken: "t"}

	w := newCaptureWriter()
	served, err := mgr.TryProxyRead(context.Background(), w, readReq(t), node)
	require.NoError(t, err)
	assert.True(t, served)
	assert.Equal(t, 200, w.status)
	assert.Equal(t, "hello", w.body.String())
}

func TestTryProxyRead_404DoesNotWriteAndReturnsRetryable(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	now := time.Now()
	_, err := db.Exec(`INSERT INTO cluster_nodes (id, name, endpoint, api_url, node_token, region, priority,
		 health_status, latency_ms, capacity_total, capacity_used, bucket_count, metadata, created_at, updated_at)
		 VALUES ('p1', 'p1', 'http://x', 'http://x', 't', 'r', 1, ?, 0, 0, 0, 0, '{}', ?, ?)`,
		HealthStatusHealthy, now, now)
	require.NoError(t, err)

	srv := peerWithStatus(t, 404, "not found")
	defer srv.Close()
	node := &Node{ID: "p1", Endpoint: srv.URL, NodeToken: "t"}

	w := newCaptureWriter()
	served, err := mgr.TryProxyRead(context.Background(), w, readReq(t), node)
	assert.False(t, served)
	assert.Error(t, err)
	assert.False(t, w.wrote, "404 must not commit any bytes to the client")

	// 404 must NOT mark the node unavailable — the object just isn't there yet.
	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'p1'`).Scan(&status))
	assert.Equal(t, HealthStatusHealthy, status)
}

func TestTryProxyRead_5xxRetryableAndMarksNodeUnavailable(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	now := time.Now()
	_, err := db.Exec(`INSERT INTO cluster_nodes (id, name, endpoint, api_url, node_token, region, priority,
		 health_status, latency_ms, capacity_total, capacity_used, bucket_count, metadata, created_at, updated_at)
		 VALUES ('p1', 'p1', 'http://x', 'http://x', 't', 'r', 1, ?, 0, 0, 0, 0, '{}', ?, ?)`,
		HealthStatusHealthy, now, now)
	require.NoError(t, err)

	srv := peerWithStatus(t, 503, "down")
	defer srv.Close()
	node := &Node{ID: "p1", Endpoint: srv.URL, NodeToken: "t"}

	w := newCaptureWriter()
	served, err := mgr.TryProxyRead(context.Background(), w, readReq(t), node)
	assert.False(t, served)
	assert.Error(t, err)
	assert.False(t, w.wrote, "5xx must not commit any bytes to the client")

	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'p1'`).Scan(&status))
	assert.Equal(t, HealthStatusUnavailable, status, "5xx must flip node to unavailable")
}

func TestTryProxyRead_403IsServedAsDefinitive(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")

	srv := peerWithStatus(t, 403, "forbidden")
	defer srv.Close()
	node := &Node{ID: "p1", Endpoint: srv.URL, NodeToken: "t"}

	w := newCaptureWriter()
	served, err := mgr.TryProxyRead(context.Background(), w, readReq(t), node)
	require.NoError(t, err)
	assert.True(t, served, "403 is a definitive client error and must be passed through")
	assert.Equal(t, 403, w.status)
	assert.Equal(t, "forbidden", w.body.String())
}

func TestTryProxyRead_TransportFailureRetryableAndMarksUnavailable(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	now := time.Now()
	_, err := db.Exec(`INSERT INTO cluster_nodes (id, name, endpoint, api_url, node_token, region, priority,
		 health_status, latency_ms, capacity_total, capacity_used, bucket_count, metadata, created_at, updated_at)
		 VALUES ('p1', 'p1', 'http://x', 'http://x', 't', 'r', 1, ?, 0, 0, 0, 0, '{}', ?, ?)`,
		HealthStatusHealthy, now, now)
	require.NoError(t, err)

	// Endpoint that no listener is bound to → transport-level dial failure.
	node := &Node{ID: "p1", Endpoint: fmt.Sprintf("http://127.0.0.1:%d", 1), NodeToken: "t"}

	w := newCaptureWriter()
	served, err := mgr.TryProxyRead(context.Background(), w, readReq(t), node)
	assert.False(t, served)
	assert.Error(t, err)
	assert.False(t, w.wrote)

	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'p1'`).Scan(&status))
	assert.Equal(t, HealthStatusUnavailable, status)
}
