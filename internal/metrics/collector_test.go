package metrics

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCollector(t *testing.T) {
	dataDir := os.TempDir()
	collector := NewCollector(dataDir)

	require.NotNil(t, collector)

	// Verify collector is functional
	assert.True(t, collector.IsHealthy())
}

func TestCollector_CollectSystemMetrics(t *testing.T) {
	collector := NewCollector(os.TempDir())

	metrics, err := collector.CollectSystemMetrics()
	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Verify CPU metrics
	assert.GreaterOrEqual(t, metrics.CPUUsagePercent, 0.0)
	assert.LessOrEqual(t, metrics.CPUUsagePercent, 100.0)

	// Verify memory metrics
	assert.GreaterOrEqual(t, metrics.MemoryUsagePercent, 0.0)
	assert.LessOrEqual(t, metrics.MemoryUsagePercent, 100.0)
	assert.Greater(t, metrics.MemoryTotalBytes, int64(0))
	assert.Greater(t, metrics.MemoryUsedBytes, int64(0))

	// Verify disk metrics
	assert.GreaterOrEqual(t, metrics.DiskUsagePercent, 0.0)
	assert.LessOrEqual(t, metrics.DiskUsagePercent, 100.0)
	assert.Greater(t, metrics.DiskTotalBytes, int64(0))

	// Verify timestamp
	assert.Greater(t, metrics.Timestamp, int64(0))
	assert.Less(t, time.Now().Unix()-metrics.Timestamp, int64(5)) // Within 5 seconds
}

func TestCollector_CollectRuntimeMetrics(t *testing.T) {
	collector := NewCollector(os.TempDir())

	metrics, err := collector.CollectRuntimeMetrics()
	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Verify Go version
	assert.NotEmpty(t, metrics.GoVersion)
	assert.Contains(t, metrics.GoVersion, "go")

	// Verify goroutines
	assert.Greater(t, metrics.GoRoutines, 0, "Should have at least 1 goroutine")

	// Verify threads
	assert.Greater(t, metrics.Threads, 0, "Should have at least 1 thread")

	// Verify heap metrics
	assert.Greater(t, metrics.HeapAlloc, int64(0), "Heap allocation should be positive")
	assert.Greater(t, metrics.HeapSys, int64(0), "Heap system should be positive")
	assert.GreaterOrEqual(t, metrics.HeapSys, metrics.HeapAlloc, "HeapSys should be >= HeapAlloc")

	// Verify stack metrics
	assert.GreaterOrEqual(t, metrics.StackInuse, int64(0))
	assert.GreaterOrEqual(t, metrics.StackSys, int64(0))

	// Verify GC metrics
	assert.GreaterOrEqual(t, metrics.NumGC, int64(0))
	assert.GreaterOrEqual(t, metrics.PauseTotalNs, int64(0))
	assert.GreaterOrEqual(t, metrics.GCCPUFraction, 0.0)
	assert.LessOrEqual(t, metrics.GCCPUFraction, 1.0)

	// Verify timestamp
	assert.Greater(t, metrics.Timestamp, int64(0))
}

func TestCollector_CollectStorageMetrics(t *testing.T) {
	collector := NewCollector(os.TempDir())
	ctx := context.Background()

	metrics, err := collector.CollectStorageMetrics(ctx)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Verify structure (current implementation returns mock/zero metrics)
	assert.GreaterOrEqual(t, metrics.TotalBuckets, int64(0))
	assert.GreaterOrEqual(t, metrics.TotalObjects, int64(0))
	assert.GreaterOrEqual(t, metrics.TotalBytes, int64(0))
	assert.NotNil(t, metrics.BucketMetrics)
	assert.NotNil(t, metrics.StorageOperations)
	assert.NotNil(t, metrics.ObjectSizeDistribution)
	assert.Greater(t, metrics.Timestamp, int64(0))
}

func TestCollector_CollectS3Metrics(t *testing.T) {
	collector := NewCollector(os.TempDir())
	ctx := context.Background()

	metrics, err := collector.CollectS3Metrics(ctx)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Verify structure (current implementation returns mock/zero metrics)
	assert.NotNil(t, metrics.RequestsTotal)
	assert.NotNil(t, metrics.ErrorsTotal)
	assert.NotNil(t, metrics.AverageResponseTime)
	assert.NotNil(t, metrics.AuthFailures)
	assert.GreaterOrEqual(t, metrics.ActiveConnections, int64(0))
	assert.GreaterOrEqual(t, metrics.AuthSuccessRate, 0.0)
	assert.LessOrEqual(t, metrics.AuthSuccessRate, 100.0)
	assert.Greater(t, metrics.Timestamp, int64(0))
}

func TestCollector_IsHealthy(t *testing.T) {
	collector := NewCollector(os.TempDir())

	// Collector should always be healthy
	assert.True(t, collector.IsHealthy())
}

func TestCollector_StartBackgroundCollection(t *testing.T) {
	collector := NewCollector(os.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create mock manager
	manager := NewManager(testMetricsConfig())

	// Start background collection
	interval := 100 * time.Millisecond
	collector.StartBackgroundCollection(ctx, manager, interval)

	// Wait for collection to start
	time.Sleep(50 * time.Millisecond)

	// Stop by canceling context
	cancel()
	time.Sleep(150 * time.Millisecond)

	// Collector should still be healthy after stopping
	assert.True(t, collector.IsHealthy())
}

func TestCollector_StartBackgroundCollection_AlreadyRunning(t *testing.T) {
	collector := NewCollector(os.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := NewManager(testMetricsConfig())

	// Start first time
	collector.StartBackgroundCollection(ctx, manager, 100*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Try to start again (should be no-op)
	collector.StartBackgroundCollection(ctx, manager, 200*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Collector should still be healthy
	assert.True(t, collector.IsHealthy())

	cancel()
	time.Sleep(150 * time.Millisecond)
}

func TestCollector_StopBackgroundCollection(t *testing.T) {
	collector := NewCollector(os.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := NewManager(testMetricsConfig())

	// Start collection
	collector.StartBackgroundCollection(ctx, manager, 100*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Stop collection
	collector.StopBackgroundCollection()
	time.Sleep(150 * time.Millisecond)

	// Collector should still be healthy after stopping
	assert.True(t, collector.IsHealthy())
}

func TestCollector_StopBackgroundCollection_NotRunning(t *testing.T) {
	collector := NewCollector(os.TempDir())

	// Try to stop when not running (should be no-op, not panic)
	collector.StopBackgroundCollection()

	// Should still be healthy
	assert.True(t, collector.IsHealthy())
}

func TestCollector_BackgroundCollectionInterval(t *testing.T) {
	collector := NewCollector(os.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := NewManager(testMetricsConfig())

	// Start with fast interval for testing
	interval := 100 * time.Millisecond
	collector.StartBackgroundCollection(ctx, manager, interval)

	// Wait for a few collection cycles
	time.Sleep(350 * time.Millisecond)

	// Stop collection
	collector.StopBackgroundCollection()
	time.Sleep(50 * time.Millisecond)

	// Verify collection ran (collector should still be healthy)
	assert.True(t, collector.IsHealthy())
}

func TestCollector_BackgroundCollectionCancellation(t *testing.T) {
	collector := NewCollector(os.TempDir())

	ctx, cancel := context.WithCancel(context.Background())

	manager := NewManager(testMetricsConfig())

	// Start collection
	collector.StartBackgroundCollection(ctx, manager, 50*time.Millisecond)
	time.Sleep(25 * time.Millisecond)

	// Cancel context
	cancel()
	time.Sleep(100 * time.Millisecond)

	// Collector should still be healthy after cancellation
	assert.True(t, collector.IsHealthy())
}

// Note: Helper methods like getCPUUsage, getMemoryUsage, etc. are private
// and tested indirectly through CollectSystemMetrics and CollectRuntimeMetrics

func TestCollector_MultipleCollections(t *testing.T) {
	collector := NewCollector(os.TempDir())

	// Collect system metrics multiple times
	for i := 0; i < 5; i++ {
		metrics, err := collector.CollectSystemMetrics()
		require.NoError(t, err)
		require.NotNil(t, metrics)

		// Each collection should return valid data
		assert.GreaterOrEqual(t, metrics.CPUUsagePercent, 0.0)
		assert.GreaterOrEqual(t, metrics.MemoryUsagePercent, 0.0)
		assert.Greater(t, metrics.Timestamp, int64(0))

		time.Sleep(50 * time.Millisecond)
	}
}

func TestCollector_ConcurrentCollections(t *testing.T) {
	collector := NewCollector(os.TempDir())
	const numGoroutines = 10

	done := make(chan bool)

	// Collect metrics concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			// Collect different types of metrics
			_, err1 := collector.CollectSystemMetrics()
			assert.NoError(t, err1)

			_, err2 := collector.CollectRuntimeMetrics()
			assert.NoError(t, err2)

			_, err3 := collector.CollectStorageMetrics(context.Background())
			assert.NoError(t, err3)

			_, err4 := collector.CollectS3Metrics(context.Background())
			assert.NoError(t, err4)

			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

func TestCollector_ContextCancellation(t *testing.T) {
	collector := NewCollector(os.TempDir())

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context immediately
	cancel()

	// Collections should still work (they don't currently use context for main logic)
	metrics, err := collector.CollectStorageMetrics(ctx)
	require.NoError(t, err)
	require.NotNil(t, metrics)
}

func TestCollector_SystemMetricsTimestamp(t *testing.T) {
	collector := NewCollector(os.TempDir())

	before := time.Now().Unix()
	metrics, err := collector.CollectSystemMetrics()
	after := time.Now().Unix()

	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Timestamp should be between before and after
	assert.GreaterOrEqual(t, metrics.Timestamp, before)
	assert.LessOrEqual(t, metrics.Timestamp, after)
}

func TestCollector_RuntimeMetricsTimestamp(t *testing.T) {
	collector := NewCollector(os.TempDir())

	before := time.Now().Unix()
	metrics, err := collector.CollectRuntimeMetrics()
	after := time.Now().Unix()

	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Timestamp should be between before and after
	assert.GreaterOrEqual(t, metrics.Timestamp, before)
	assert.LessOrEqual(t, metrics.Timestamp, after)
}

// Helper functions for testing

func testMetricsConfig() config.MetricsConfig {
	return config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}
}
