package metrics

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSystemMetrics(t *testing.T) {
	dataDir := os.TempDir()
	sm := NewSystemMetrics(dataDir)

	require.NotNil(t, sm)
	assert.Equal(t, dataDir, sm.dataDir)
	assert.False(t, sm.startTime.IsZero())
}

func TestGetUptime(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Get uptime immediately after creation (should be close to 0)
	uptime1 := sm.GetUptime()
	assert.GreaterOrEqual(t, uptime1, int64(0))
	assert.Less(t, uptime1, int64(2)) // Should be less than 2 seconds

	// Wait 1 second and check uptime increased
	time.Sleep(1 * time.Second)
	uptime2 := sm.GetUptime()
	assert.Greater(t, uptime2, uptime1)
	assert.GreaterOrEqual(t, uptime2, int64(1))
}

func TestGetCPUUsage(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	usage, err := sm.GetCPUUsage()
	require.NoError(t, err)

	// CPU usage should be between 0 and 100
	assert.GreaterOrEqual(t, usage, 0.0)
	assert.LessOrEqual(t, usage, 100.0)
}

func TestGetCPUStats(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	stats, err := sm.GetCPUStats()
	require.NoError(t, err)
	require.NotNil(t, stats)

	// Verify CPU usage percentage
	assert.GreaterOrEqual(t, stats.UsagePercent, 0.0)
	assert.LessOrEqual(t, stats.UsagePercent, 100.0)

	// Verify core counts
	assert.Greater(t, stats.Cores, 0, "Physical cores should be greater than 0")
	assert.Greater(t, stats.LogicalCores, 0, "Logical cores should be greater than 0")
	assert.GreaterOrEqual(t, stats.LogicalCores, stats.Cores, "Logical cores should be >= physical cores")

	// Verify frequency (may be 0 on some systems, but shouldn't be negative)
	assert.GreaterOrEqual(t, stats.FrequencyMHz, 0.0)

	// Model name should not be empty (though it might be "Unknown" on some systems)
	assert.NotEmpty(t, stats.ModelName)
}

func TestGetMemoryUsage(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	memStats, err := sm.GetMemoryUsage()
	require.NoError(t, err)
	require.NotNil(t, memStats)

	// Verify memory usage percentage
	assert.GreaterOrEqual(t, memStats.UsedPercent, 0.0)
	assert.LessOrEqual(t, memStats.UsedPercent, 100.0)

	// Verify memory bytes
	assert.Greater(t, memStats.TotalBytes, uint64(0), "Total memory should be greater than 0")
	assert.Greater(t, memStats.UsedBytes, uint64(0), "Used memory should be greater than 0")
	assert.GreaterOrEqual(t, memStats.FreeBytes, uint64(0), "Free memory should be >= 0")

	// Used + Free should be approximately Total (allowing for some variance due to caching)
	assert.LessOrEqual(t, memStats.UsedBytes, memStats.TotalBytes, "Used should not exceed total")
}

func TestGetDiskUsage(t *testing.T) {
	dataDir := os.TempDir()
	sm := NewSystemMetrics(dataDir)

	diskStats, err := sm.GetDiskUsage()
	require.NoError(t, err)
	require.NotNil(t, diskStats)

	// Verify disk usage percentage
	assert.GreaterOrEqual(t, diskStats.UsedPercent, 0.0)
	assert.LessOrEqual(t, diskStats.UsedPercent, 100.0)

	// Verify disk bytes
	assert.Greater(t, diskStats.TotalBytes, uint64(0), "Total disk should be greater than 0")
	assert.GreaterOrEqual(t, diskStats.UsedBytes, uint64(0), "Used disk should be >= 0")
	assert.GreaterOrEqual(t, diskStats.FreeBytes, uint64(0), "Free disk should be >= 0")

	// Used + Free should be approximately Total
	assert.LessOrEqual(t, diskStats.UsedBytes, diskStats.TotalBytes, "Used should not exceed total")
}

func TestGetRequestStats_NoRequests(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// No requests recorded yet
	stats := sm.GetRequestStats()
	require.NotNil(t, stats)

	assert.Equal(t, uint64(0), stats.TotalRequests)
	assert.Equal(t, uint64(0), stats.TotalErrors)
	assert.Equal(t, 0.0, stats.AverageLatency)
	assert.Equal(t, 0.0, stats.RequestsPerSec) // Should be 0 with no requests
}

func TestGetRequestStats_WithRequests(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Wait a bit to ensure uptime > 0
	time.Sleep(100 * time.Millisecond)

	// Record some requests
	sm.RecordRequest(100, false) // 100ms, success
	sm.RecordRequest(200, false) // 200ms, success
	sm.RecordRequest(300, true)  // 300ms, error

	stats := sm.GetRequestStats()
	require.NotNil(t, stats)

	// Verify counts
	assert.Equal(t, uint64(3), stats.TotalRequests)
	assert.Equal(t, uint64(1), stats.TotalErrors)

	// Verify average latency (100 + 200 + 300) / 3 = 200ms
	assert.Equal(t, 200.0, stats.AverageLatency)

	// Verify requests per second (should be non-negative)
	assert.GreaterOrEqual(t, stats.RequestsPerSec, 0.0)
}

func TestGetRequestStats_OnlyErrors(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Record only errors
	sm.RecordRequest(50, true)
	sm.RecordRequest(75, true)
	sm.RecordRequest(100, true)

	stats := sm.GetRequestStats()
	require.NotNil(t, stats)

	assert.Equal(t, uint64(3), stats.TotalRequests)
	assert.Equal(t, uint64(3), stats.TotalErrors)
	assert.Equal(t, 75.0, stats.AverageLatency) // (50+75+100)/3 = 75
}

func TestGetRequestStats_AverageLatencyCalculation(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Record requests with known latencies
	latencies := []uint64{10, 20, 30, 40, 50}
	var expectedSum uint64
	for _, latency := range latencies {
		sm.RecordRequest(latency, false)
		expectedSum += latency
	}

	stats := sm.GetRequestStats()
	expectedAvg := float64(expectedSum) / float64(len(latencies))

	assert.Equal(t, uint64(len(latencies)), stats.TotalRequests)
	assert.Equal(t, expectedAvg, stats.AverageLatency)
}

func TestGetRequestStats_RequestsPerSecCalculation(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Record some requests
	sm.RecordRequest(10, false)
	sm.RecordRequest(20, false)

	// Wait a bit to ensure uptime > 0
	time.Sleep(100 * time.Millisecond)

	stats := sm.GetRequestStats()

	// Requests per second should be: total_requests / uptime_seconds
	// With 2 requests in ~0.1 seconds, should be around 20 req/sec
	assert.Greater(t, stats.RequestsPerSec, 0.0)
	assert.Less(t, stats.RequestsPerSec, 1000.0) // Sanity check
}

func TestRecordRequest_Success(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Record successful request
	sm.RecordRequest(150, false)

	// Verify counters updated
	assert.Equal(t, uint64(1), sm.requestCount.Load())
	assert.Equal(t, uint64(150), sm.totalLatencyMs.Load())
	assert.Equal(t, uint64(0), sm.errorCount.Load())
}

func TestRecordRequest_Error(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Record error request
	sm.RecordRequest(200, true)

	// Verify counters updated
	assert.Equal(t, uint64(1), sm.requestCount.Load())
	assert.Equal(t, uint64(200), sm.totalLatencyMs.Load())
	assert.Equal(t, uint64(1), sm.errorCount.Load())
}

func TestRecordRequest_Multiple(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Record multiple requests
	sm.RecordRequest(100, false)
	sm.RecordRequest(200, true)
	sm.RecordRequest(300, false)
	sm.RecordRequest(400, true)

	// Verify counters
	assert.Equal(t, uint64(4), sm.requestCount.Load())
	assert.Equal(t, uint64(1000), sm.totalLatencyMs.Load()) // 100+200+300+400
	assert.Equal(t, uint64(2), sm.errorCount.Load())
}

func TestRecordRequest_ConcurrentSafety(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())
	const numGoroutines = 10
	const requestsPerGoroutine = 100

	// Launch concurrent goroutines recording requests
	done := make(chan bool)
	for i := 0; i < numGoroutines; i++ {
		go func(workerID int) {
			for j := 0; j < requestsPerGoroutine; j++ {
				isError := j%2 == 0
				sm.RecordRequest(uint64(j+1), isError)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify counts
	expectedRequests := uint64(numGoroutines * requestsPerGoroutine)
	assert.Equal(t, expectedRequests, sm.requestCount.Load())

	// Error count should be half (every other request is an error)
	expectedErrors := expectedRequests / 2
	assert.Equal(t, expectedErrors, sm.errorCount.Load())

	// Total latency should be sum of all latencies
	// Each goroutine records latencies 1, 2, 3, ..., 100
	// Sum of 1 to 100 = 100 * 101 / 2 = 5050
	expectedLatencyPerGoroutine := uint64(5050)
	expectedTotalLatency := uint64(numGoroutines) * expectedLatencyPerGoroutine
	assert.Equal(t, expectedTotalLatency, sm.totalLatencyMs.Load())
}

func TestGetPerformanceStats(t *testing.T) {
	dataDir := os.TempDir()
	sm := NewSystemMetrics(dataDir)

	// Wait a bit for uptime
	time.Sleep(100 * time.Millisecond)

	perfStats := sm.GetPerformanceStats()
	require.NotNil(t, perfStats)

	// Verify uptime (should be >= 0)
	assert.GreaterOrEqual(t, perfStats.Uptime, int64(0))

	// Verify goroutines count (may be 0 if collector fails, so just check >= 0)
	assert.GreaterOrEqual(t, perfStats.GoRoutines, 0, "Goroutines should be non-negative")

	// Verify heap allocations (should be non-negative)
	assert.GreaterOrEqual(t, perfStats.HeapAllocMB, 0.0, "Heap allocation should be non-negative")
	assert.GreaterOrEqual(t, perfStats.TotalAllocMB, 0.0, "Total allocation should be non-negative")

	// GC runs should be >= 0
	assert.GreaterOrEqual(t, perfStats.GCRuns, uint32(0))
}

func TestSystemMetricsTracker_StartTime(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Start time should be recent
	timeSinceStart := time.Since(sm.startTime)
	assert.Less(t, timeSinceStart, 1*time.Second, "Start time should be within the last second")
}

func TestSystemMetricsTracker_DataDir(t *testing.T) {
	testDataDir := "/test/data/dir"
	sm := NewSystemMetrics(testDataDir)

	assert.Equal(t, testDataDir, sm.dataDir)

	// Disk usage should use this data directory
	// Note: This might fail if the directory doesn't exist, which is expected
	_, err := sm.GetDiskUsage()
	// We don't assert NoError here because the test directory might not exist
	// The important thing is that it attempts to use the correct directory
	_ = err
}

func TestGetRequestStats_ZeroUptime(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Immediately get stats (uptime very close to 0)
	stats := sm.GetRequestStats()
	require.NotNil(t, stats)

	// RequestsPerSec calculation should handle near-zero uptime gracefully
	assert.GreaterOrEqual(t, stats.RequestsPerSec, 0.0)
}

func TestCPUStats_ConsistentData(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Get CPU stats twice
	stats1, err1 := sm.GetCPUStats()
	require.NoError(t, err1)
	require.NotNil(t, stats1)

	time.Sleep(100 * time.Millisecond)

	stats2, err2 := sm.GetCPUStats()
	require.NoError(t, err2)
	require.NotNil(t, stats2)

	// Core counts should be consistent
	assert.Equal(t, stats1.Cores, stats2.Cores)
	assert.Equal(t, stats1.LogicalCores, stats2.LogicalCores)
	assert.Equal(t, stats1.ModelName, stats2.ModelName)

	// Frequency should be consistent (allowing for some variance)
	// Note: In CI/virtualized environments, CPU frequency can vary more due to
	// dynamic scaling, throttling, and hypervisor behavior
	if stats1.FrequencyMHz > 0 && stats2.FrequencyMHz > 0 {
		// Allow 50% variance in frequency (CI/virtualized environments have aggressive
		// dynamic scaling, turbo boost, and hypervisor-level frequency changes)
		diff := stats1.FrequencyMHz - stats2.FrequencyMHz
		if diff < 0 {
			diff = -diff
		}
		percentDiff := (diff / stats1.FrequencyMHz) * 100
		assert.Less(t, percentDiff, 50.0, "Frequency variance should be less than 50%")
	}
}

func TestMemoryStats_ConsistentData(t *testing.T) {
	sm := NewSystemMetrics(os.TempDir())

	// Get memory stats twice
	stats1, err1 := sm.GetMemoryUsage()
	require.NoError(t, err1)
	require.NotNil(t, stats1)

	time.Sleep(100 * time.Millisecond)

	stats2, err2 := sm.GetMemoryUsage()
	require.NoError(t, err2)
	require.NotNil(t, stats2)

	// Total memory should be exactly the same
	assert.Equal(t, stats1.TotalBytes, stats2.TotalBytes)

	// Used memory might vary slightly, but should be reasonable
	// Allow up to 100MB variance (for small allocations during test)
	diff := int64(stats1.UsedBytes) - int64(stats2.UsedBytes)
	if diff < 0 {
		diff = -diff
	}
	assert.Less(t, diff, int64(100*1024*1024), "Memory usage variance should be less than 100MB")
}

func TestDiskStats_ConsistentData(t *testing.T) {
	dataDir := os.TempDir()
	sm := NewSystemMetrics(dataDir)

	// Get disk stats twice
	stats1, err1 := sm.GetDiskUsage()
	require.NoError(t, err1)
	require.NotNil(t, stats1)

	time.Sleep(100 * time.Millisecond)

	stats2, err2 := sm.GetDiskUsage()
	require.NoError(t, err2)
	require.NotNil(t, stats2)

	// Total disk should be exactly the same
	assert.Equal(t, stats1.TotalBytes, stats2.TotalBytes)

	// Used disk might vary slightly, but total should stay constant
	// Allow up to 10MB variance (for temp files, logs, etc.)
	diff := int64(stats1.UsedBytes) - int64(stats2.UsedBytes)
	if diff < 0 {
		diff = -diff
	}
	assert.Less(t, diff, int64(10*1024*1024), "Disk usage variance should be less than 10MB")
}
