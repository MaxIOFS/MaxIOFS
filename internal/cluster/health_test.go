package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupHealthTestDB creates a test database with cluster schema
func setupHealthTestDB(t *testing.T) (*sql.DB, func()) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_health.db")

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=10000")
	require.NoError(t, err)

	// Initialize schema
	err = InitSchema(db)
	require.NoError(t, err)

	cleanup := func() {
		db.Close()
	}

	return db, cleanup
}

// createTestHealthManager creates a test Manager instance
func createTestHealthManager(t *testing.T, db *sql.DB) *Manager {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise in tests

	return &Manager{
		db:                  db,
		log:                 logrus.NewEntry(logger),
		healthCheckInterval: 100 * time.Millisecond, // Fast for testing
		stopChan:            make(chan struct{}),
	}
}

// TestCheckNodeHealth_Healthy tests checking health of a healthy node
func TestCheckNodeHealth_Healthy(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Create test HTTP server that returns 200 OK with low latency
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create a test node
	node := &Node{
		Name:      "test-node",
		Endpoint:  server.URL,
		NodeToken: "test-token",
	}

	err := manager.AddNode(ctx, node)
	require.NoError(t, err)

	// Perform health check
	healthStatus, err := manager.CheckNodeHealth(ctx, node.ID)
	require.NoError(t, err)
	require.NotNil(t, healthStatus)

	// Verify health status
	assert.Equal(t, node.ID, healthStatus.NodeID)
	assert.Equal(t, HealthStatusHealthy, healthStatus.Status)
	assert.GreaterOrEqual(t, healthStatus.LatencyMs, 0)
	assert.Less(t, healthStatus.LatencyMs, 1000) // Should be fast
	assert.Empty(t, healthStatus.ErrorMessage)

	// Verify database was updated
	var dbStatus string
	var dbLatency int
	err = db.QueryRowContext(ctx, "SELECT health_status, latency_ms FROM cluster_nodes WHERE id = ?", node.ID).Scan(&dbStatus, &dbLatency)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusHealthy, dbStatus)
	assert.Equal(t, healthStatus.LatencyMs, dbLatency)

	// Verify health history was recorded
	var historyCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history WHERE node_id = ?", node.ID).Scan(&historyCount)
	require.NoError(t, err)
	assert.Equal(t, 1, historyCount)
}

// TestCheckNodeHealth_Degraded tests checking health of a degraded node (high latency)
func TestCheckNodeHealth_Degraded(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Create test HTTP server with artificial delay to simulate high latency
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			time.Sleep(1100 * time.Millisecond) // Simulate >1000ms latency
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	node := &Node{
		Name:      "slow-node",
		Endpoint:  server.URL,
		NodeToken: "test-token",
	}

	err := manager.AddNode(ctx, node)
	require.NoError(t, err)

	// Perform health check with longer timeout
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	healthStatus, err := manager.CheckNodeHealth(checkCtx, node.ID)
	require.NoError(t, err)
	require.NotNil(t, healthStatus)

	// Verify it's marked as degraded due to high latency
	assert.Equal(t, HealthStatusDegraded, healthStatus.Status)
	assert.Greater(t, healthStatus.LatencyMs, 1000)
	assert.Empty(t, healthStatus.ErrorMessage)
}

// TestCheckNodeHealth_Unavailable tests checking health of an unavailable node
func TestCheckNodeHealth_Unavailable(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Create test HTTP server that returns non-200 status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	node := &Node{
		Name:      "unavailable-node",
		Endpoint:  server.URL,
		NodeToken: "test-token",
	}

	err := manager.AddNode(ctx, node)
	require.NoError(t, err)

	// Perform health check
	healthStatus, err := manager.CheckNodeHealth(ctx, node.ID)
	require.NoError(t, err)
	require.NotNil(t, healthStatus)

	// Verify it's marked as unavailable
	assert.Equal(t, HealthStatusUnavailable, healthStatus.Status)
	assert.NotEmpty(t, healthStatus.ErrorMessage)
	assert.Contains(t, healthStatus.ErrorMessage, "unexpected status code: 503")
}

// TestCheckNodeHealth_ConnectionError tests checking health when node is unreachable
func TestCheckNodeHealth_ConnectionError(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	node := &Node{
		Name:      "unreachable-node",
		Endpoint:  "http://localhost:99999", // Invalid port - connection will fail
		NodeToken: "test-token",
	}

	err := manager.AddNode(ctx, node)
	require.NoError(t, err)

	// Perform health check
	healthStatus, err := manager.CheckNodeHealth(ctx, node.ID)
	require.NoError(t, err)
	require.NotNil(t, healthStatus)

	// Verify it's marked as unavailable with error message
	assert.Equal(t, HealthStatusUnavailable, healthStatus.Status)
	assert.NotEmpty(t, healthStatus.ErrorMessage)
	assert.GreaterOrEqual(t, healthStatus.LatencyMs, 0)
}

// TestCheckNodeHealth_NodeNotFound tests checking health of non-existent node
func TestCheckNodeHealth_NodeNotFound(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Try to check health of non-existent node
	_, err := manager.CheckNodeHealth(ctx, "non-existent-node-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get node")
}

// TestPerformHealthCheck_Success tests performHealthCheck with successful response
func TestPerformHealthCheck_Success(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := manager.performHealthCheck(server.URL)

	assert.True(t, result.Healthy)
	assert.GreaterOrEqual(t, result.LatencyMs, 0)
	assert.Less(t, result.LatencyMs, 100) // Should be fast
	assert.Empty(t, result.ErrorMessage)
}

// TestPerformHealthCheck_NonOKStatus tests performHealthCheck with non-200 response
func TestPerformHealthCheck_NonOKStatus(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)

	testCases := []struct {
		name       string
		statusCode int
	}{
		{"Service Unavailable", http.StatusServiceUnavailable},
		{"Internal Server Error", http.StatusInternalServerError},
		{"Not Found", http.StatusNotFound},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			result := manager.performHealthCheck(server.URL)

			assert.False(t, result.Healthy)
			assert.GreaterOrEqual(t, result.LatencyMs, 0)
			assert.Contains(t, result.ErrorMessage, fmt.Sprintf("unexpected status code: %d", tc.statusCode))
		})
	}
}

// TestPerformHealthCheck_ConnectionError tests performHealthCheck with connection error
func TestPerformHealthCheck_ConnectionError(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)

	// Use invalid endpoint
	result := manager.performHealthCheck("http://invalid-host-that-does-not-exist:99999")

	assert.False(t, result.Healthy)
	assert.GreaterOrEqual(t, result.LatencyMs, 0)
	assert.NotEmpty(t, result.ErrorMessage)
}

// TestPerformHealthCheck_Timeout tests performHealthCheck with timeout
func TestPerformHealthCheck_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)

	// Create server that takes longer than timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(6 * time.Second) // Longer than 5s timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := manager.performHealthCheck(server.URL)

	assert.False(t, result.Healthy)
	assert.NotEmpty(t, result.ErrorMessage)
	assert.Contains(t, result.ErrorMessage, "deadline exceeded")
}

// TestStartHealthChecker_PeriodicExecution tests that health checker runs periodically
func TestStartHealthChecker_PeriodicExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping periodic execution test in short mode")
	}

	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Create healthy test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Add test node
	node := &Node{
		Name:      "test-node",
		Endpoint:  server.URL,
		NodeToken: "test-token",
	}
	err := manager.AddNode(ctx, node)
	require.NoError(t, err)

	// Start health checker in background
	go manager.StartHealthChecker(ctx)

	// Wait for at least 3 check intervals
	time.Sleep(350 * time.Millisecond)

	// Verify multiple health checks were recorded
	var historyCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history WHERE node_id = ?", node.ID).Scan(&historyCount)
	require.NoError(t, err)

	// Should have at least 2-3 checks (100ms interval, 350ms wait)
	assert.GreaterOrEqual(t, historyCount, 2, "Expected at least 2 health checks")
}

// TestStartHealthChecker_StopsOnContextCancel tests that health checker stops when context is cancelled
func TestStartHealthChecker_StopsOnContextCancel(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx, cancel := context.WithCancel(context.Background())

	// Start health checker
	done := make(chan bool)
	go func() {
		manager.StartHealthChecker(ctx)
		done <- true
	}()

	// Cancel context after short delay
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Wait for health checker to stop
	select {
	case <-done:
		// Success - health checker stopped
	case <-time.After(1 * time.Second):
		t.Fatal("Health checker did not stop after context cancellation")
	}
}

// TestStartHealthChecker_StopsOnStopChan tests that health checker stops when stopChan is closed
func TestStartHealthChecker_StopsOnStopChan(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Start health checker
	done := make(chan bool)
	go func() {
		manager.StartHealthChecker(ctx)
		done <- true
	}()

	// Close stopChan after short delay
	time.Sleep(50 * time.Millisecond)
	close(manager.stopChan)

	// Wait for health checker to stop
	select {
	case <-done:
		// Success - health checker stopped
	case <-time.After(1 * time.Second):
		t.Fatal("Health checker did not stop after stopChan closed")
	}
}

// TestPerformHealthChecks_AllNodes tests that performHealthChecks checks all nodes
func TestPerformHealthChecks_AllNodes(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Create test servers
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	// Add multiple nodes
	node1 := &Node{Name: "node-1", Endpoint: server1.URL, NodeToken: "token-1"}
	node2 := &Node{Name: "node-2", Endpoint: server2.URL, NodeToken: "token-2"}

	err := manager.AddNode(ctx, node1)
	require.NoError(t, err)
	err = manager.AddNode(ctx, node2)
	require.NoError(t, err)

	// Perform health checks
	manager.performHealthChecks(ctx)

	// Verify both nodes have health history
	var count1, count2 int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history WHERE node_id = ?", node1.ID).Scan(&count1)
	require.NoError(t, err)
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history WHERE node_id = ?", node2.ID).Scan(&count2)
	require.NoError(t, err)

	assert.Equal(t, 1, count1, "Node 1 should have 1 health check")
	assert.Equal(t, 1, count2, "Node 2 should have 1 health check")
}

// TestPerformHealthChecks_NoNodes tests performHealthChecks with no nodes
func TestPerformHealthChecks_NoNodes(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Should not panic or error with no nodes
	manager.performHealthChecks(ctx)

	// Verify no history entries
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestPerformHealthChecks_HandlesFailures tests that performHealthChecks continues despite failures
func TestPerformHealthChecks_HandlesFailures(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Create one healthy server and one that fails
	healthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthyServer.Close()

	// Add nodes
	healthyNode := &Node{Name: "healthy", Endpoint: healthyServer.URL, NodeToken: "token-1"}
	unhealthyNode := &Node{Name: "unhealthy", Endpoint: "http://localhost:99999", NodeToken: "token-2"}

	err := manager.AddNode(ctx, healthyNode)
	require.NoError(t, err)
	err = manager.AddNode(ctx, unhealthyNode)
	require.NoError(t, err)

	// Perform health checks - should not panic
	manager.performHealthChecks(ctx)

	// Verify both nodes have health history despite one failing
	var healthyCount, unhealthyCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history WHERE node_id = ?", healthyNode.ID).Scan(&healthyCount)
	require.NoError(t, err)
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history WHERE node_id = ?", unhealthyNode.ID).Scan(&unhealthyCount)
	require.NoError(t, err)

	assert.Equal(t, 1, healthyCount, "Healthy node should have health check")
	assert.Equal(t, 1, unhealthyCount, "Unhealthy node should still have health check recorded")

	// Verify healthy node is marked healthy
	var healthyStatus string
	err = db.QueryRowContext(ctx, "SELECT health_status FROM cluster_nodes WHERE id = ?", healthyNode.ID).Scan(&healthyStatus)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusHealthy, healthyStatus)

	// Verify unhealthy node is marked unavailable
	var unhealthyStatus string
	err = db.QueryRowContext(ctx, "SELECT health_status FROM cluster_nodes WHERE id = ?", unhealthyNode.ID).Scan(&unhealthyStatus)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusUnavailable, unhealthyStatus)
}

// TestCleanupHealthHistory_RemovesOldEntries tests cleanup of old health history
func TestCleanupHealthHistory_RemovesOldEntries(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Create a test node
	node := &Node{Name: "test-node", Endpoint: "http://localhost", NodeToken: "token"}
	err := manager.AddNode(ctx, node)
	require.NoError(t, err)

	// Insert old health history entries (older than retention)
	oldTime := time.Now().AddDate(0, 0, -40) // 40 days ago
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_health_history (node_id, health_status, latency_ms, timestamp)
		VALUES (?, ?, ?, ?)
	`, node.ID, HealthStatusHealthy, 10, oldTime)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_health_history (node_id, health_status, latency_ms, timestamp)
		VALUES (?, ?, ?, ?)
	`, node.ID, HealthStatusHealthy, 15, oldTime.Add(1*time.Hour))
	require.NoError(t, err)

	// Insert recent entries (within retention)
	recentTime := time.Now().AddDate(0, 0, -10) // 10 days ago
	_, err = db.ExecContext(ctx, `
		INSERT INTO cluster_health_history (node_id, health_status, latency_ms, timestamp)
		VALUES (?, ?, ?, ?)
	`, node.ID, HealthStatusHealthy, 20, recentTime)
	require.NoError(t, err)

	// Verify we have 3 entries
	var countBefore int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history").Scan(&countBefore)
	require.NoError(t, err)
	assert.Equal(t, 3, countBefore)

	// Cleanup with 30 day retention
	err = manager.CleanupHealthHistory(ctx, 30)
	require.NoError(t, err)

	// Verify old entries were removed
	var countAfter int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history").Scan(&countAfter)
	require.NoError(t, err)
	assert.Equal(t, 1, countAfter, "Only recent entry should remain")

	// Verify the remaining entry is the recent one
	var latency int
	err = db.QueryRowContext(ctx, "SELECT latency_ms FROM cluster_health_history").Scan(&latency)
	require.NoError(t, err)
	assert.Equal(t, 20, latency, "Remaining entry should be the recent one")
}

// TestCleanupHealthHistory_KeepsRecentEntries tests that recent entries are not deleted
func TestCleanupHealthHistory_KeepsRecentEntries(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Create a test node
	node := &Node{Name: "test-node", Endpoint: "http://localhost", NodeToken: "token"}
	err := manager.AddNode(ctx, node)
	require.NoError(t, err)

	// Insert recent entries only
	for i := 0; i < 5; i++ {
		recentTime := time.Now().AddDate(0, 0, -i) // Last 5 days
		_, err = db.ExecContext(ctx, `
			INSERT INTO cluster_health_history (node_id, health_status, latency_ms, timestamp)
			VALUES (?, ?, ?, ?)
		`, node.ID, HealthStatusHealthy, 10+i, recentTime)
		require.NoError(t, err)
	}

	// Verify we have 5 entries
	var countBefore int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history").Scan(&countBefore)
	require.NoError(t, err)
	assert.Equal(t, 5, countBefore)

	// Cleanup with 30 day retention
	err = manager.CleanupHealthHistory(ctx, 30)
	require.NoError(t, err)

	// Verify no entries were removed
	var countAfter int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history").Scan(&countAfter)
	require.NoError(t, err)
	assert.Equal(t, 5, countAfter, "All recent entries should remain")
}

// TestCleanupHealthHistory_EmptyTable tests cleanup on empty table
func TestCleanupHealthHistory_EmptyTable(t *testing.T) {
	db, cleanup := setupHealthTestDB(t)
	defer cleanup()

	manager := createTestHealthManager(t, db)
	ctx := context.Background()

	// Cleanup empty table - should not error
	err := manager.CleanupHealthHistory(ctx, 30)
	require.NoError(t, err)

	// Verify table is still empty
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_health_history").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
