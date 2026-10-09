package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupSPTestDB initializes a fresh DB with cluster + replication schema so
// the storage-pressure config defaults are already seeded.
func setupSPTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sp.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	require.NoError(t, InitSchema(db))
	require.NoError(t, InitReplicationSchema(db))
	return db
}

// newSPHealthServer returns an httptest server whose /health endpoint reports
// capacity from the supplied pointers, so tests can flip values between calls.
func newSPHealthServer(t *testing.T, capTotal, capUsed *int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body := map[string]interface{}{
			"capacity_total": *capTotal,
			"capacity_used":  *capUsed,
			"bucket_count":   0,
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

// captureSPEmitter records every storage-pressure event the manager fires.
type captureSPEmitter struct {
	mu     sync.Mutex
	events []StoragePressureEvent
}

func (c *captureSPEmitter) emit(ev StoragePressureEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *captureSPEmitter) snapshot() []StoragePressureEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]StoragePressureEvent, len(c.events))
	copy(out, c.events)
	return out
}

func TestLoadStoragePressureThresholds_Defaults(t *testing.T) {
	db := setupSPTestDB(t)
	m := createTestHealthManager(t, db)
	th, rl := m.loadStoragePressureThresholds(context.Background())
	assert.Equal(t, 90.0, th)
	assert.Equal(t, 85.0, rl)
}

func TestLoadStoragePressureThresholds_Overrides(t *testing.T) {
	db := setupSPTestDB(t)
	ctx := context.Background()
	require.NoError(t, SetGlobalConfig(ctx, db, storagePressureThresholdKey, "75"))
	require.NoError(t, SetGlobalConfig(ctx, db, storagePressureReleaseKey, "70"))
	m := createTestHealthManager(t, db)
	th, rl := m.loadStoragePressureThresholds(ctx)
	assert.Equal(t, 75.0, th)
	assert.Equal(t, 70.0, rl)
}

func TestLoadStoragePressureThresholds_ClampsInvertedConfig(t *testing.T) {
	// Misconfiguration: release >= threshold. Loader collapses release to
	// threshold-5 so the hysteresis loop is preserved.
	db := setupSPTestDB(t)
	ctx := context.Background()
	require.NoError(t, SetGlobalConfig(ctx, db, storagePressureThresholdKey, "80"))
	require.NoError(t, SetGlobalConfig(ctx, db, storagePressureReleaseKey, "82"))
	m := createTestHealthManager(t, db)
	th, rl := m.loadStoragePressureThresholds(ctx)
	assert.Equal(t, 80.0, th)
	assert.Equal(t, 75.0, rl)
}

func TestCheckNodeHealth_StoragePressure_CrossThreshold(t *testing.T) {
	db := setupSPTestDB(t)
	m := createTestHealthManager(t, db)
	capTotal, capUsed := int64(1000), int64(500) // 50% — below threshold
	srv := newSPHealthServer(t, &capTotal, &capUsed)
	defer srv.Close()

	emitter := &captureSPEmitter{}
	m.SetStoragePressureEmitter(emitter.emit)

	node := &Node{Name: "n1", Endpoint: srv.URL, NodeToken: "t"}
	require.NoError(t, m.AddNode(context.Background(), node))

	// 50% — should be healthy.
	s, err := m.CheckNodeHealth(context.Background(), node.ID)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusHealthy, s.Status)
	assert.Empty(t, emitter.snapshot())

	// 95% — should flip to storage_pressure and emit.
	capUsed = 950
	s, err = m.CheckNodeHealth(context.Background(), node.ID)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusStoragePressure, s.Status)
	evs := emitter.snapshot()
	require.Len(t, evs, 1)
	assert.Equal(t, "node_storage_pressure", evs[0].Kind)
	assert.InDelta(t, 95.0, evs[0].UsagePercent, 0.001)
	assert.Equal(t, 90.0, evs[0].ThresholdPercent)
	assert.Equal(t, node.ID, evs[0].NodeID)
}

func TestCheckNodeHealth_StoragePressure_HysteresisSticky(t *testing.T) {
	// Once in storage_pressure, must stay until usage drops below release (85%).
	db := setupSPTestDB(t)
	m := createTestHealthManager(t, db)
	capTotal, capUsed := int64(1000), int64(950)
	srv := newSPHealthServer(t, &capTotal, &capUsed)
	defer srv.Close()

	node := &Node{Name: "n1", Endpoint: srv.URL, NodeToken: "t"}
	require.NoError(t, m.AddNode(context.Background(), node))

	s, _ := m.CheckNodeHealth(context.Background(), node.ID)
	require.Equal(t, HealthStatusStoragePressure, s.Status)

	// 87% — between release (85) and threshold (90); should stay stuck.
	capUsed = 870
	s, _ = m.CheckNodeHealth(context.Background(), node.ID)
	assert.Equal(t, HealthStatusStoragePressure, s.Status)

	// 84% — below release; should recover.
	capUsed = 840
	s, _ = m.CheckNodeHealth(context.Background(), node.ID)
	assert.Equal(t, HealthStatusHealthy, s.Status)
}

func TestCheckNodeHealth_StoragePressure_EmitsResolved(t *testing.T) {
	db := setupSPTestDB(t)
	m := createTestHealthManager(t, db)
	capTotal, capUsed := int64(1000), int64(950)
	srv := newSPHealthServer(t, &capTotal, &capUsed)
	defer srv.Close()

	emitter := &captureSPEmitter{}
	m.SetStoragePressureEmitter(emitter.emit)

	node := &Node{Name: "n1", Endpoint: srv.URL, NodeToken: "t"}
	require.NoError(t, m.AddNode(context.Background(), node))

	_, _ = m.CheckNodeHealth(context.Background(), node.ID) // → storage_pressure
	capUsed = 800
	_, _ = m.CheckNodeHealth(context.Background(), node.ID) // → healthy

	evs := emitter.snapshot()
	require.Len(t, evs, 2)
	assert.Equal(t, "node_storage_pressure", evs[0].Kind)
	assert.Equal(t, "node_storage_pressure_resolved", evs[1].Kind)
}

func TestCheckNodeHealth_StoragePressure_DrainedNodeStaysDead(t *testing.T) {
	// A drained node stays dead and reports nothing; a node dead by the probes
	// that answers is back, with its storage pressure.
	db := setupSPTestDB(t)
	m := createTestHealthManager(t, db)
	capTotal, capUsed := int64(1000), int64(990)
	srv := newSPHealthServer(t, &capTotal, &capUsed)
	defer srv.Close()

	emitter := &captureSPEmitter{}
	m.SetStoragePressureEmitter(emitter.emit)

	node := &Node{Name: "n1", Endpoint: srv.URL, NodeToken: "t"}
	require.NoError(t, m.AddNode(context.Background(), node))
	_, err := db.Exec(`UPDATE cluster_nodes SET health_status = ?, drained = 1 WHERE id = ?`, HealthStatusDead, node.ID)
	require.NoError(t, err)

	s, err := m.CheckNodeHealth(context.Background(), node.ID)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusDead, s.Status)
	assert.Empty(t, emitter.snapshot())

	_, err = db.Exec(`UPDATE cluster_nodes SET drained = 0 WHERE id = ?`, node.ID)
	require.NoError(t, err)
	s, err = m.CheckNodeHealth(context.Background(), node.ID)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusStoragePressure, s.Status)
	assert.Len(t, emitter.snapshot(), 1)
}

func TestCheckNodeHealth_StoragePressure_NotSetWhenUnreachable(t *testing.T) {
	// Unreachable nodes flip to unavailable — storage-pressure branch must be
	// skipped so we don't emit false positives based on stale capacity.
	db := setupSPTestDB(t)
	m := createTestHealthManager(t, db)

	emitter := &captureSPEmitter{}
	m.SetStoragePressureEmitter(emitter.emit)

	node := &Node{Name: "n1", Endpoint: "http://127.0.0.1:1", NodeToken: "t"}
	require.NoError(t, m.AddNode(context.Background(), node))
	// Pre-fill capacity to simulate stale data from before the outage.
	_, err := db.Exec(`UPDATE cluster_nodes SET capacity_total = 1000, capacity_used = 990 WHERE id = ?`, node.ID)
	require.NoError(t, err)

	s, err := m.CheckNodeHealth(context.Background(), node.ID)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusUnavailable, s.Status)
	assert.Empty(t, emitter.snapshot())
}

// A node under storage pressure stays in service: it is listed with the
// healthy nodes, serves and is sent every change, but takes no new data. A
// write counts only the nodes that take data, and a node under pressure has
// missed none of the writes.
func TestANodeUnderStoragePressureIsInServiceButTakesNoData(t *testing.T) {
	ctx := context.Background()
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	setReplicationFactor(t, db, 3)
	m := newTestManager(t, db)
	insertNode(t, db, "local-node", "local", HealthStatusHealthy, nil)
	insertNode(t, db, "pressed", "pressed", HealthStatusStoragePressure, nil)
	insertNode(t, db, "away", "away", HealthStatusUnavailable, nil)

	nodes, err := m.GetHealthyNodes(ctx)
	require.NoError(t, err)
	var ids []string
	for _, n := range nodes {
		ids = append(ids, n.ID)
		assert.True(t, n.InService(), n.ID)
		assert.Equal(t, n.ID != "pressed", n.TakesData(), n.ID)
	}
	assert.ElementsMatch(t, []string{"local-node", "pressed"}, ids)

	ok, err := m.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "no other node takes data")
	insertNode(t, db, "spare", "spare", HealthStatusHealthy, nil)
	ok, err = m.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.True(t, ok)

	m.noteMissedWrites(ctx, "local-node", time.Unix(100, 0))
	missed := func(id string) bool {
		var since sql.NullInt64
		require.NoError(t, db.QueryRow(`SELECT replica_missed_since FROM cluster_nodes WHERE id = ?`, id).Scan(&since))
		return since.Valid
	}
	assert.False(t, missed("pressed"), "a node under pressure is sent the writes")
	assert.True(t, missed("away"))
}

// A node that answers again under storage pressure is caught up from its first
// missed write, as a healthy one is.
func TestCheckNodeHealth_StoragePressure_IsCaughtUp(t *testing.T) {
	ctx := context.Background()
	db := setupSPTestDB(t)
	m := createTestHealthManager(t, db)
	capTotal, capUsed := int64(1000), int64(950)
	srv := newSPHealthServer(t, &capTotal, &capUsed)
	defer srv.Close()
	var caughtUp []string
	m.OnReplicaBack(func(nodeID string, _ time.Time) { caughtUp = append(caughtUp, nodeID) })
	node := &Node{Name: "n1", Endpoint: srv.URL, NodeToken: "t"}
	require.NoError(t, m.AddNode(ctx, node))
	_, err := db.Exec(`UPDATE cluster_nodes SET health_status = ?, replica_missed_since = 100 WHERE id = ?`, HealthStatusUnavailable, node.ID)
	require.NoError(t, err)

	s, err := m.CheckNodeHealth(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, HealthStatusStoragePressure, s.Status)
	assert.Equal(t, []string{node.ID}, caughtUp)
}
