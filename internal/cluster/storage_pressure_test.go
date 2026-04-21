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
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=10000")
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

func TestCheckNodeHealth_StoragePressure_SkippedWhenDead(t *testing.T) {
	// Dead is terminal: CheckNodeHealth must not flip it (and must not emit).
	db := setupSPTestDB(t)
	m := createTestHealthManager(t, db)
	capTotal, capUsed := int64(1000), int64(990)
	srv := newSPHealthServer(t, &capTotal, &capUsed)
	defer srv.Close()

	emitter := &captureSPEmitter{}
	m.SetStoragePressureEmitter(emitter.emit)

	node := &Node{Name: "n1", Endpoint: srv.URL, NodeToken: "t"}
	require.NoError(t, m.AddNode(context.Background(), node))
	_, err := db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, HealthStatusDead, node.ID)
	require.NoError(t, err)

	s, err := m.CheckNodeHealth(context.Background(), node.ID)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusDead, s.Status)
	assert.Empty(t, emitter.snapshot())
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

func TestGetReadyReplicaNodes_IncludesStoragePressure(t *testing.T) {
	db := setupSPTestDB(t)
	ctx := context.Background()
	m := createTestHealthManager(t, db)

	_, err := db.Exec(`INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled)
		VALUES (?, ?, ?, ?)`, "local", "local", "tok", 1)
	require.NoError(t, err)

	insert := func(id, status string) {
		_, err := db.Exec(`INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status, is_stale)
			VALUES (?, ?, ?, ?, ?, 0)`, id, id, "http://"+id, "tok", status)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO ha_sync_jobs (target_node_id, status, started_at)
			VALUES (?, ?, ?)`, id, SyncJobDone, time.Now())
		require.NoError(t, err)
	}
	insert("n-healthy", HealthStatusHealthy)
	insert("n-sp", HealthStatusStoragePressure)
	insert("n-unavail", HealthStatusUnavailable)
	insert("n-dead", HealthStatusDead)

	out, err := m.GetReadyReplicaNodes(ctx)
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, n := range out {
		ids[n.ID] = true
	}
	assert.True(t, ids["n-healthy"])
	assert.True(t, ids["n-sp"], "storage_pressure node must be served reads")
	assert.False(t, ids["n-unavail"])
	assert.False(t, ids["n-dead"])
}

func TestGetHealthyNodes_ExcludesStoragePressure(t *testing.T) {
	// Write path uses GetHealthyNodes — it must NOT include storage_pressure
	// (otherwise replicaTargets would still pick a saturated node).
	db := setupSPTestDB(t)
	m := createTestHealthManager(t, db)

	insert := func(id, status string) {
		_, err := db.Exec(`INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status, is_stale)
			VALUES (?, ?, ?, ?, ?, 0)`, id, id, "http://"+id, "tok", status)
		require.NoError(t, err)
	}
	insert("n-healthy", HealthStatusHealthy)
	insert("n-sp", HealthStatusStoragePressure)

	nodes, err := m.GetHealthyNodes(context.Background())
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "n-healthy", nodes[0].ID)
}
