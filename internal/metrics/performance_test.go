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
