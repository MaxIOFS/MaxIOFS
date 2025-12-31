package metrics

import (
	"testing"
	"time"
)

func TestPerformanceCollector_RecordLatency(t *testing.T) {
	// Create collector with small limits for testing
	collector := NewPerformanceCollector(100, 1*time.Hour)

	// Record some latencies
	collector.RecordLatency(OpPutObject, 10*time.Millisecond, true)
	collector.RecordLatency(OpPutObject, 20*time.Millisecond, true)
	collector.RecordLatency(OpPutObject, 30*time.Millisecond, true)
	collector.RecordLatency(OpPutObject, 40*time.Millisecond, false) // Failed operation

	// Get stats
	stats := collector.GetLatencyStats(OpPutObject)

	// Verify count
	if stats.Count != 4 {
		t.Errorf("Expected count 4, got %d", stats.Count)
	}

	// Verify success rate (3 successes out of 4 = 75%)
	expectedRate := 75.0
	if stats.SuccessRate != expectedRate {
		t.Errorf("Expected success rate %.2f%%, got %.2f%%", expectedRate, stats.SuccessRate)
	}

	// Verify error count
	if stats.ErrorCount != 1 {
		t.Errorf("Expected error count 1, got %d", stats.ErrorCount)
	}

	// Verify min and max
	if stats.Min != 10.0 {
		t.Errorf("Expected min 10ms, got %.2fms", stats.Min)
	}
	if stats.Max != 40.0 {
		t.Errorf("Expected max 40ms, got %.2fms", stats.Max)
	}

	// Verify mean (10+20+30+40)/4 = 25
	expectedMean := 25.0
	if stats.Mean != expectedMean {
		t.Errorf("Expected mean %.2fms, got %.2fms", expectedMean, stats.Mean)
	}
}

func TestPerformanceCollector_Percentiles(t *testing.T) {
	collector := NewPerformanceCollector(1000, 1*time.Hour)

	// Record 100 latencies from 1ms to 100ms
	for i := 1; i <= 100; i++ {
		collector.RecordLatency(OpGetObject, time.Duration(i)*time.Millisecond, true)
	}

	stats := collector.GetLatencyStats(OpGetObject)

	// p50 should be around 50ms (median)
	if stats.P50 < 49 || stats.P50 > 51 {
		t.Errorf("Expected p50 around 50ms, got %.2fms", stats.P50)
	}

	// p95 should be around 95ms
	if stats.P95 < 94 || stats.P95 > 96 {
		t.Errorf("Expected p95 around 95ms, got %.2fms", stats.P95)
	}

	// p99 should be around 99ms
	if stats.P99 < 98 || stats.P99 > 100 {
		t.Errorf("Expected p99 around 99ms, got %.2fms", stats.P99)
	}
}

func TestPerformanceCollector_Throughput(t *testing.T) {
	collector := NewPerformanceCollector(1000, 1*time.Hour)

	// Record some throughput data
	collector.RecordThroughput(1024, 1)      // 1KB, 1 object
	collector.RecordThroughput(2048, 1)      // 2KB, 1 object
	collector.RecordThroughput(4096, 2)      // 4KB, 2 objects

	// Wait a bit to ensure time passes
	time.Sleep(100 * time.Millisecond)

	// Calculate throughput
	throughput := collector.CalculateThroughput()

	// Verify we have data
	if throughput.RequestsPerSecond == 0 {
		t.Error("Expected non-zero requests per second")
	}

	if throughput.BytesPerSecond == 0 {
		t.Error("Expected non-zero bytes per second")
	}

	if throughput.ObjectsPerSecond == 0 {
		t.Error("Expected non-zero objects per second")
	}

	// Verify timestamp is recent
	if time.Since(throughput.Timestamp) > 1*time.Second {
		t.Error("Throughput timestamp is too old")
	}
}

func TestPerformanceCollector_RollingWindow(t *testing.T) {
	// Create collector with max 10 samples
	collector := NewPerformanceCollector(10, 1*time.Hour)

	// Record 20 latencies (should keep only last 10)
	for i := 1; i <= 20; i++ {
		collector.RecordLatency(OpDeleteObject, time.Duration(i)*time.Millisecond, true)
		time.Sleep(1 * time.Millisecond) // Small delay to ensure different timestamps
	}

	stats := collector.GetLatencyStats(OpDeleteObject)

	// Should have exactly 10 samples (rolling window)
	if stats.Count != 10 {
		t.Errorf("Expected count 10 (rolling window), got %d", stats.Count)
	}

	// Min should be 11ms (first 10 were dropped)
	if stats.Min != 11.0 {
		t.Errorf("Expected min 11ms (first 10 dropped), got %.2fms", stats.Min)
	}

	// Max should be 20ms (last sample)
	if stats.Max != 20.0 {
		t.Errorf("Expected max 20ms, got %.2fms", stats.Max)
	}
}

func TestPerformanceCollector_MultipleOperations(t *testing.T) {
	collector := NewPerformanceCollector(100, 1*time.Hour)

	// Record latencies for different operations
	collector.RecordLatency(OpPutObject, 10*time.Millisecond, true)
	collector.RecordLatency(OpGetObject, 5*time.Millisecond, true)
	collector.RecordLatency(OpDeleteObject, 3*time.Millisecond, true)
	collector.RecordLatency(OpListObjects, 50*time.Millisecond, true)

	// Get all stats
	allStats := collector.GetAllLatencyStats()

	// Verify we have stats for 4 operations
	if len(allStats) != 4 {
		t.Errorf("Expected stats for 4 operations, got %d", len(allStats))
	}

	// Verify each operation has correct data
	if stats, ok := allStats[OpPutObject]; ok {
		if stats.Count != 1 || stats.Mean != 10.0 {
			t.Error("PutObject stats incorrect")
		}
	} else {
		t.Error("Missing PutObject stats")
	}

	if stats, ok := allStats[OpGetObject]; ok {
		if stats.Count != 1 || stats.Mean != 5.0 {
			t.Error("GetObject stats incorrect")
		}
	} else {
		t.Error("Missing GetObject stats")
	}
}

func TestPerformanceCollector_LatencyHistory(t *testing.T) {
	collector := NewPerformanceCollector(100, 1*time.Hour)

	// Record 50 latencies
	for i := 1; i <= 50; i++ {
		collector.RecordLatency(OpMultipartUpload, time.Duration(i)*time.Millisecond, true)
	}

	// Get last 10 samples
	history := collector.GetLatencyHistory(OpMultipartUpload, 10)

	// Verify we got 10 samples
	if len(history) != 10 {
		t.Errorf("Expected 10 history samples, got %d", len(history))
	}

	// Verify they are the last 10 (41-50ms)
	if history[0].Duration != 41*time.Millisecond {
		t.Errorf("Expected first history sample to be 41ms, got %v", history[0].Duration)
	}

	if history[9].Duration != 50*time.Millisecond {
		t.Errorf("Expected last history sample to be 50ms, got %v", history[9].Duration)
	}
}

func TestPerformanceCollector_Reset(t *testing.T) {
	collector := NewPerformanceCollector(100, 1*time.Hour)

	// Record some data
	collector.RecordLatency(OpPutObject, 10*time.Millisecond, true)
	collector.RecordThroughput(1024, 1)

	// Verify data exists
	stats := collector.GetLatencyStats(OpPutObject)
	if stats.Count == 0 {
		t.Error("Expected data before reset")
	}

	// Reset
	collector.Reset()

	// Verify data is cleared
	statsAfter := collector.GetLatencyStats(OpPutObject)
	if statsAfter.Count != 0 {
		t.Error("Expected no data after reset")
	}

	throughputAfter := collector.GetCurrentThroughput()
	if throughputAfter.RequestsPerSecond != 0 {
		t.Error("Expected zero throughput after reset")
	}
}

func TestCalculatePercentile(t *testing.T) {
	tests := []struct {
		name       string
		data       []float64
		percentile int
		expected   float64
	}{
		{
			name:       "p50 of 1-10",
			data:       []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			percentile: 50,
			expected:   5.5,
		},
		{
			name:       "p95 of 1-100",
			data:       []float64{},
			percentile: 95,
			expected:   95.05,
		},
		{
			name:       "p0 (min)",
			data:       []float64{5, 10, 15, 20},
			percentile: 0,
			expected:   5.0,
		},
		{
			name:       "p100 (max)",
			data:       []float64{5, 10, 15, 20},
			percentile: 100,
			expected:   20.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate data for p95 test
			if tt.name == "p95 of 1-100" {
				for i := 1; i <= 100; i++ {
					tt.data = append(tt.data, float64(i))
				}
			}

			result := calculatePercentile(tt.data, tt.percentile)
			if result < tt.expected-0.1 || result > tt.expected+0.1 {
				t.Errorf("Expected %.2f, got %.2f", tt.expected, result)
			}
		})
	}
}

func TestPerformanceCollector_EmptyStats(t *testing.T) {
	collector := NewPerformanceCollector(100, 1*time.Hour)

	// Get stats for operation with no data
	stats := collector.GetLatencyStats(OpClusterProxy)

	// Verify stats are zero/empty
	if stats.Count != 0 {
		t.Errorf("Expected count 0 for empty stats, got %d", stats.Count)
	}

	if stats.P50 != 0 || stats.P95 != 0 || stats.P99 != 0 {
		t.Error("Expected zero percentiles for empty stats")
	}
}

// TestPerformanceCollector_ConcurrentRecordLatency tests thread-safety with concurrent operations
func TestPerformanceCollector_ConcurrentRecordLatency(t *testing.T) {
	collector := NewPerformanceCollector(1000, 1*time.Hour)

	// Launch 10 goroutines recording latencies concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(workerID int) {
			for j := 0; j < 100; j++ {
				collector.RecordLatency(OpPutObject, time.Duration(j+1)*time.Millisecond, j%2 == 0)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to finish
	for i := 0; i < 10; i++ {
		<-done
	}

	// Get stats
	stats := collector.GetLatencyStats(OpPutObject)

	// Should have recorded 1000 latencies (10 workers * 100 each)
	if stats.Count != 1000 {
		t.Errorf("Expected count 1000 from concurrent operations, got %d", stats.Count)
	}

	// Success rate should be 50% (every other operation succeeded)
	if stats.SuccessRate < 49 || stats.SuccessRate > 51 {
		t.Errorf("Expected success rate around 50%%, got %.2f%%", stats.SuccessRate)
	}
}

// TestPerformanceCollector_ConcurrentMixedOperations tests concurrent reads and writes
func TestPerformanceCollector_ConcurrentMixedOperations(t *testing.T) {
	collector := NewPerformanceCollector(1000, 1*time.Hour)

	done := make(chan bool)

	// Writers: Record latencies
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				collector.RecordLatency(OpGetObject, time.Duration(j+1)*time.Millisecond, true)
				time.Sleep(1 * time.Millisecond)
			}
			done <- true
		}()
	}

	// Readers: Get stats while writing
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				collector.GetLatencyStats(OpGetObject)
				collector.GetAllLatencyStats()
				time.Sleep(1 * time.Millisecond)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify we got all the data
	stats := collector.GetLatencyStats(OpGetObject)
	if stats.Count != 250 {
		t.Errorf("Expected count 250 from concurrent writes, got %d", stats.Count)
	}
}

// TestCalculatePercentile_EdgeCases tests edge cases in percentile calculation
func TestCalculatePercentile_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		data       []float64
		percentile int
		expected   float64
	}{
		{
			name:       "Empty array",
			data:       []float64{},
			percentile: 50,
			expected:   0.0,
		},
		{
			name:       "Single element",
			data:       []float64{42.0},
			percentile: 50,
			expected:   42.0,
		},
		{
			name:       "Single element p95",
			data:       []float64{100.0},
			percentile: 95,
			expected:   100.0,
		},
		{
			name:       "Two elements p50",
			data:       []float64{10.0, 20.0},
			percentile: 50,
			expected:   15.0,
		},
		{
			name:       "All same values",
			data:       []float64{5.0, 5.0, 5.0, 5.0, 5.0},
			percentile: 50,
			expected:   5.0,
		},
		{
			name:       "Negative percentile",
			data:       []float64{1, 2, 3, 4, 5},
			percentile: -1,
			expected:   1.0, // Should clamp to min
		},
		{
			name:       "Percentile > 100",
			data:       []float64{1, 2, 3, 4, 5},
			percentile: 150,
			expected:   5.0, // Should clamp to max
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculatePercentile(tt.data, tt.percentile)
			if result < tt.expected-0.01 || result > tt.expected+0.01 {
				t.Errorf("Expected %.2f, got %.2f", tt.expected, result)
			}
		})
	}
}

// TestPerformanceCollector_RetentionCleanup tests that old samples are removed
func TestPerformanceCollector_RetentionCleanup(t *testing.T) {
	// Create collector with 1 second retention
	collector := NewPerformanceCollector(1000, 1*time.Second)

	// Record old latencies
	collector.RecordLatency(OpPutObject, 10*time.Millisecond, true)
	collector.RecordLatency(OpPutObject, 20*time.Millisecond, true)

	// Verify we have 2 samples
	stats := collector.GetLatencyStats(OpPutObject)
	if stats.Count != 2 {
		t.Errorf("Expected count 2 before retention, got %d", stats.Count)
	}

	// Wait for retention period to pass
	time.Sleep(1100 * time.Millisecond)

	// Record new latencies - this triggers automatic cleanup of expired samples
	collector.RecordLatency(OpPutObject, 30*time.Millisecond, true)
	collector.RecordLatency(OpPutObject, 40*time.Millisecond, true)

	// Get stats - should only have the 2 recent samples (old ones cleaned up automatically)
	stats = collector.GetLatencyStats(OpPutObject)
	if stats.Count != 2 {
		t.Errorf("Expected count 2 after retention cleanup, got %d", stats.Count)
	}

	// Min should be 30ms (old 10ms and 20ms removed by retention cleanup)
	if stats.Min != 30.0 {
		t.Errorf("Expected min 30ms after cleanup, got %.2fms", stats.Min)
	}
}

// TestPerformanceCollector_ThroughputZeroElapsedTime tests edge case of zero elapsed time
func TestPerformanceCollector_ThroughputZeroElapsedTime(t *testing.T) {
	collector := NewPerformanceCollector(100, 1*time.Hour)

	// Immediately calculate throughput (zero elapsed time)
	throughput := collector.CalculateThroughput()

	// Should return zero values, not panic or divide by zero
	if throughput.RequestsPerSecond != 0 {
		t.Errorf("Expected 0 requests/sec with zero elapsed time, got %.2f", throughput.RequestsPerSecond)
	}

	if throughput.BytesPerSecond != 0 {
		t.Errorf("Expected 0 bytes/sec with zero elapsed time, got %d", throughput.BytesPerSecond)
	}

	if throughput.ObjectsPerSecond != 0 {
		t.Errorf("Expected 0 objects/sec with zero elapsed time, got %.2f", throughput.ObjectsPerSecond)
	}
}

// TestPerformanceCollector_GetCurrentThroughput tests current throughput getter
func TestPerformanceCollector_GetCurrentThroughput(t *testing.T) {
	collector := NewPerformanceCollector(100, 1*time.Hour)

	// Record some throughput
	collector.RecordThroughput(5120, 10) // 5KB, 10 objects

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Calculate throughput (this updates currentThroughput)
	collector.CalculateThroughput()

	// Get current throughput
	throughput := collector.GetCurrentThroughput()

	// Should be positive values
	if throughput.BytesPerSecond <= 0 {
		t.Error("Expected positive bytes per second")
	}

	if throughput.ObjectsPerSecond <= 0 {
		t.Error("Expected positive objects per second")
	}

	// Timestamp should be recent
	if time.Since(throughput.Timestamp) > 1*time.Second {
		t.Error("Throughput timestamp is too old")
	}
}

// TestPerformanceCollector_GetLatencyHistoryBoundary tests boundary conditions
func TestPerformanceCollector_GetLatencyHistoryBoundary(t *testing.T) {
	collector := NewPerformanceCollector(100, 1*time.Hour)

	// Record 10 latencies
	for i := 1; i <= 10; i++ {
		collector.RecordLatency(OpGetObject, time.Duration(i)*time.Millisecond, true)
	}

	tests := []struct {
		name          string
		limit         int
		expectedCount int
	}{
		{
			name:          "Limit zero",
			limit:         0,
			expectedCount: 0,
		},
		{
			name:          "Limit exact count",
			limit:         10,
			expectedCount: 10,
		},
		{
			name:          "Limit greater than count",
			limit:         20,
			expectedCount: 10,
		},
		{
			name:          "Limit 1",
			limit:         1,
			expectedCount: 1,
		},
		{
			name:          "Limit 5",
			limit:         5,
			expectedCount: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			history := collector.GetLatencyHistory(OpGetObject, tt.limit)
			if len(history) != tt.expectedCount {
				t.Errorf("Expected %d history samples, got %d", tt.expectedCount, len(history))
			}
		})
	}
}

// TestPerformanceCollector_SuccessRateScenarios tests different success/failure scenarios
func TestPerformanceCollector_SuccessRateScenarios(t *testing.T) {
	tests := []struct {
		name               string
		successCount       int
		failureCount       int
		expectedRate       float64
		expectedErrorCount int64
	}{
		{
			name:               "All successes",
			successCount:       10,
			failureCount:       0,
			expectedRate:       100.0,
			expectedErrorCount: 0,
		},
		{
			name:               "All failures",
			successCount:       0,
			failureCount:       10,
			expectedRate:       0.0,
			expectedErrorCount: 10,
		},
		{
			name:               "Mixed 50/50",
			successCount:       5,
			failureCount:       5,
			expectedRate:       50.0,
			expectedErrorCount: 5,
		},
		{
			name:               "Single success",
			successCount:       1,
			failureCount:       0,
			expectedRate:       100.0,
			expectedErrorCount: 0,
		},
		{
			name:               "Single failure",
			successCount:       0,
			failureCount:       1,
			expectedRate:       0.0,
			expectedErrorCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := NewPerformanceCollector(100, 1*time.Hour)

			// Record successes
			for i := 0; i < tt.successCount; i++ {
				collector.RecordLatency(OpDeleteObject, 10*time.Millisecond, true)
			}

			// Record failures
			for i := 0; i < tt.failureCount; i++ {
				collector.RecordLatency(OpDeleteObject, 10*time.Millisecond, false)
			}

			stats := collector.GetLatencyStats(OpDeleteObject)

			if stats.SuccessRate != tt.expectedRate {
				t.Errorf("Expected success rate %.2f%%, got %.2f%%", tt.expectedRate, stats.SuccessRate)
			}

			if stats.ErrorCount != tt.expectedErrorCount {
				t.Errorf("Expected error count %d, got %d", tt.expectedErrorCount, stats.ErrorCount)
			}
		})
	}
}

// TestPerformanceCollector_AllOperationTypes tests all operation types
func TestPerformanceCollector_AllOperationTypes(t *testing.T) {
	collector := NewPerformanceCollector(100, 1*time.Hour)

	operations := []OperationType{
		OpPutObject,
		OpGetObject,
		OpDeleteObject,
		OpListObjects,
		OpHeadObject,
		OpCopyObject,
		OpMultipartUpload,
		OpClusterProxy,
	}

	// Record latency for each operation type
	for i, op := range operations {
		collector.RecordLatency(op, time.Duration(i+1)*time.Millisecond, true)
	}

	// Verify each operation has stats
	allStats := collector.GetAllLatencyStats()
	if len(allStats) != len(operations) {
		t.Errorf("Expected stats for %d operations, got %d", len(operations), len(allStats))
	}

	// Verify each operation type is present
	for _, op := range operations {
		if stats, ok := allStats[op]; !ok {
			t.Errorf("Missing stats for operation type: %s", op)
		} else if stats.Count != 1 {
			t.Errorf("Expected count 1 for %s, got %d", op, stats.Count)
		}
	}
}

// TestPerformanceCollector_LargeDataset tests with large number of samples
func TestPerformanceCollector_LargeDataset(t *testing.T) {
	collector := NewPerformanceCollector(10000, 1*time.Hour)

	// Record 5000 latencies with varying durations
	for i := 1; i <= 5000; i++ {
		duration := time.Duration(i%100+1) * time.Millisecond
		success := i%10 != 0 // 90% success rate
		collector.RecordLatency(OpListObjects, duration, success)
	}

	stats := collector.GetLatencyStats(OpListObjects)

	// Verify count
	if stats.Count != 5000 {
		t.Errorf("Expected count 5000, got %d", stats.Count)
	}

	// Verify success rate is around 90%
	if stats.SuccessRate < 89 || stats.SuccessRate > 91 {
		t.Errorf("Expected success rate around 90%%, got %.2f%%", stats.SuccessRate)
	}

	// Error count should be around 500 (10% of 5000)
	if stats.ErrorCount < 490 || stats.ErrorCount > 510 {
		t.Errorf("Expected error count around 500, got %d", stats.ErrorCount)
	}

	// Verify percentiles are calculated correctly
	if stats.P50 == 0 || stats.P95 == 0 || stats.P99 == 0 {
		t.Error("Percentiles should not be zero with large dataset")
	}
}

// TestPerformanceCollector_ResetPreservesConfiguration tests that reset keeps config
func TestPerformanceCollector_ResetPreservesConfiguration(t *testing.T) {
	maxSamples := 500
	retention := 2 * time.Hour
	collector := NewPerformanceCollector(maxSamples, retention)

	// Record some data
	collector.RecordLatency(OpPutObject, 10*time.Millisecond, true)
	collector.RecordThroughput(1024, 1)

	// Reset
	collector.Reset()

	// Record new data and verify configuration is preserved
	// by recording more than default samples
	for i := 0; i < 600; i++ {
		collector.RecordLatency(OpGetObject, time.Duration(i+1)*time.Millisecond, true)
	}

	stats := collector.GetLatencyStats(OpGetObject)

	// Should have maxSamples (500), not all 600
	if stats.Count != int64(maxSamples) {
		t.Errorf("Expected count %d (maxSamples preserved), got %d", maxSamples, stats.Count)
	}
}
