package cluster

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterMetrics_BucketAggregation(t *testing.T) {
	cm := NewClusterMetrics()

	t.Run("Records successful bucket aggregation", func(t *testing.T) {
		cm.RecordBucketAggregation(100*time.Millisecond, true)

		stats := cm.GetStats()
		bucketStats := stats["bucket_aggregation"].(map[string]interface{})

		assert.Equal(t, int64(1), bucketStats["total_requests"])
		assert.Equal(t, int64(1), bucketStats["successes"])
		assert.Equal(t, int64(0), bucketStats["failures"])
		assert.Equal(t, 100.0, bucketStats["success_rate"])
	})

	t.Run("Records failed bucket aggregation", func(t *testing.T) {
		cm.RecordBucketAggregation(50*time.Millisecond, false)

		stats := cm.GetStats()
		bucketStats := stats["bucket_aggregation"].(map[string]interface{})

		assert.Equal(t, int64(2), bucketStats["total_requests"])
		assert.Equal(t, int64(1), bucketStats["successes"])
		assert.Equal(t, int64(1), bucketStats["failures"])
		assert.Equal(t, 50.0, bucketStats["success_rate"])
	})

	t.Run("Tracks latency", func(t *testing.T) {
		cm.RecordBucketAggregation(200*time.Millisecond, true)

		stats := cm.GetStats()
		bucketStats := stats["bucket_aggregation"].(map[string]interface{})
		latency := bucketStats["latency_ms"].(map[string]interface{})

		assert.Equal(t, int64(3), latency["count"])
		assert.Greater(t, latency["avg_ms"].(float64), 0.0)
		assert.Equal(t, int64(50), latency["min_ms"]) // From previous test
		assert.Equal(t, int64(200), latency["max_ms"])
	})
}

func TestClusterMetrics_QuotaAggregation(t *testing.T) {
	cm := NewClusterMetrics()

	cm.RecordQuotaAggregation(150*time.Millisecond, true)
	cm.RecordQuotaAggregation(250*time.Millisecond, true)
	cm.RecordQuotaAggregation(100*time.Millisecond, false)

	stats := cm.GetStats()
	quotaStats := stats["quota_aggregation"].(map[string]interface{})

	assert.Equal(t, int64(3), quotaStats["total_requests"])
	assert.Equal(t, int64(2), quotaStats["successes"])
	assert.Equal(t, int64(1), quotaStats["failures"])
	assert.InDelta(t, 66.67, quotaStats["success_rate"].(float64), 0.1)

	latency := quotaStats["latency_ms"].(map[string]interface{})
	assert.Equal(t, int64(3), latency["count"])
	assert.InDelta(t, 166.67, latency["avg_ms"].(float64), 1.0)
}

func TestClusterMetrics_NodeRequests(t *testing.T) {
	cm := NewClusterMetrics()

	// Record 5 successful, 2 failed
	for i := 0; i < 5; i++ {
		cm.RecordNodeRequest(50*time.Millisecond, true)
	}
	for i := 0; i < 2; i++ {
		cm.RecordNodeRequest(30*time.Millisecond, false)
	}

	stats := cm.GetStats()
	nodeStats := stats["node_requests"].(map[string]interface{})

	assert.Equal(t, int64(7), nodeStats["total"])
	assert.Equal(t, int64(5), nodeStats["successes"])
	assert.Equal(t, int64(2), nodeStats["failures"])
	assert.InDelta(t, 71.43, nodeStats["success_rate"].(float64), 0.1)

	latency := nodeStats["latency_ms"].(map[string]interface{})
	assert.Equal(t, int64(7), latency["count"])
}

func TestClusterMetrics_CircuitBreakerOpens(t *testing.T) {
	cm := NewClusterMetrics()

	cm.RecordCircuitBreakerOpen()
	cm.RecordCircuitBreakerOpen()
	cm.RecordCircuitBreakerOpen()

	stats := cm.GetStats()
	cbStats := stats["circuit_breaker"].(map[string]interface{})

	assert.Equal(t, int64(3), cbStats["total_opens"])
}

func TestClusterMetrics_RateLimiting(t *testing.T) {
	cm := NewClusterMetrics()

	// Record 10 hits, 3 misses
	for i := 0; i < 10; i++ {
		cm.RecordRateLimitHit()
	}
	for i := 0; i < 3; i++ {
		cm.RecordRateLimitMiss()
	}

	stats := cm.GetStats()
	rlStats := stats["rate_limiting"].(map[string]interface{})

	assert.Equal(t, int64(10), rlStats["hits"])
	assert.Equal(t, int64(3), rlStats["misses"])
	assert.Equal(t, int64(13), rlStats["total"])
}

func TestClusterMetrics_Reset(t *testing.T) {
	cm := NewClusterMetrics()

	// Record some metrics
	cm.RecordBucketAggregation(100*time.Millisecond, true)
	cm.RecordQuotaAggregation(150*time.Millisecond, false)
	cm.RecordNodeRequest(50*time.Millisecond, true)
	cm.RecordCircuitBreakerOpen()
	cm.RecordRateLimitHit()

	// Verify metrics are recorded
	stats := cm.GetStats()
	bucketStats := stats["bucket_aggregation"].(map[string]interface{})
	assert.Equal(t, int64(1), bucketStats["total_requests"])

	// Reset
	cm.Reset()

	// Verify all metrics are zero
	stats = cm.GetStats()

	bucketStats = stats["bucket_aggregation"].(map[string]interface{})
	assert.Equal(t, int64(0), bucketStats["total_requests"])
	assert.Equal(t, int64(0), bucketStats["successes"])
	assert.Equal(t, int64(0), bucketStats["failures"])

	quotaStats := stats["quota_aggregation"].(map[string]interface{})
	assert.Equal(t, int64(0), quotaStats["total_requests"])

	nodeStats := stats["node_requests"].(map[string]interface{})
	assert.Equal(t, int64(0), nodeStats["total"])

	cbStats := stats["circuit_breaker"].(map[string]interface{})
	assert.Equal(t, int64(0), cbStats["total_opens"])

	rlStats := stats["rate_limiting"].(map[string]interface{})
	assert.Equal(t, int64(0), rlStats["hits"])
	assert.Equal(t, int64(0), rlStats["misses"])
}

func TestLatencyTracker(t *testing.T) {
	lt := &LatencyTracker{minDuration: time.Hour}

	t.Run("Tracks min, max, avg latency", func(t *testing.T) {
		lt.Record(100 * time.Millisecond)
		lt.Record(200 * time.Millisecond)
		lt.Record(50 * time.Millisecond)
		lt.Record(150 * time.Millisecond)

		stats := lt.GetStats()

		assert.Equal(t, int64(4), stats["count"])
		assert.Equal(t, int64(50), stats["min_ms"])
		assert.Equal(t, int64(200), stats["max_ms"])
		assert.InDelta(t, 125.0, stats["avg_ms"].(float64), 0.1) // (100+200+50+150)/4 = 125
	})

	t.Run("Zero count returns zero average", func(t *testing.T) {
		emptyLt := &LatencyTracker{minDuration: time.Hour}
		stats := emptyLt.GetStats()

		assert.Equal(t, int64(0), stats["count"])
		assert.Equal(t, float64(0), stats["avg_ms"])
	})
}

func TestClusterMetrics_SuccessRateCalculation(t *testing.T) {
	cm := NewClusterMetrics()

	tests := []struct {
		name           string
		successes      int
		failures       int
		expectedRate   float64
	}{
		{"All success", 10, 0, 100.0},
		{"All failures", 0, 10, 0.0},
		{"Half and half", 5, 5, 50.0},
		{"No requests", 0, 0, 0.0},
		{"75% success", 75, 25, 75.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm.Reset()

			for i := 0; i < tt.successes; i++ {
				cm.RecordBucketAggregation(10*time.Millisecond, true)
			}
			for i := 0; i < tt.failures; i++ {
				cm.RecordBucketAggregation(10*time.Millisecond, false)
			}

			stats := cm.GetStats()
			bucketStats := stats["bucket_aggregation"].(map[string]interface{})

			assert.InDelta(t, tt.expectedRate, bucketStats["success_rate"].(float64), 0.01)
		})
	}
}

func TestClusterMetrics_ConcurrentRecording(t *testing.T) {
	cm := NewClusterMetrics()

	// Record metrics concurrently
	done := make(chan bool, 100)

	for i := 0; i < 50; i++ {
		go func() {
			cm.RecordBucketAggregation(10*time.Millisecond, true)
			done <- true
		}()

		go func() {
			cm.RecordQuotaAggregation(15*time.Millisecond, true)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	stats := cm.GetStats()

	bucketStats := stats["bucket_aggregation"].(map[string]interface{})
	assert.Equal(t, int64(50), bucketStats["total_requests"])

	quotaStats := stats["quota_aggregation"].(map[string]interface{})
	assert.Equal(t, int64(50), quotaStats["total_requests"])
}

func TestClusterMetrics_GetStats_Structure(t *testing.T) {
	cm := NewClusterMetrics()

	stats := cm.GetStats()

	require.NotNil(t, stats)

	// Verify structure
	require.Contains(t, stats, "bucket_aggregation")
	require.Contains(t, stats, "quota_aggregation")
	require.Contains(t, stats, "node_requests")
	require.Contains(t, stats, "circuit_breaker")
	require.Contains(t, stats, "rate_limiting")

	// Verify nested structures
	bucketStats := stats["bucket_aggregation"].(map[string]interface{})
	require.Contains(t, bucketStats, "total_requests")
	require.Contains(t, bucketStats, "successes")
	require.Contains(t, bucketStats, "failures")
	require.Contains(t, bucketStats, "success_rate")
	require.Contains(t, bucketStats, "latency_ms")

	latency := bucketStats["latency_ms"].(map[string]interface{})
	require.Contains(t, latency, "count")
	require.Contains(t, latency, "avg_ms")
	require.Contains(t, latency, "min_ms")
	require.Contains(t, latency, "max_ms")
}
