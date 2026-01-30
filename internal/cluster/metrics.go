package cluster

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// ClusterMetrics tracks metrics for cluster operations
type ClusterMetrics struct {
	// Request counters
	bucketAggregationRequests   int64
	quotaAggregationRequests    int64
	bucketAggregationSuccesses  int64
	bucketAggregationFailures   int64
	quotaAggregationSuccesses   int64
	quotaAggregationFailures    int64

	// Node communication metrics
	nodeRequestsTotal    int64
	nodeRequestsSuccess  int64
	nodeRequestsFailure  int64
	circuitBreakerOpens  int64

	// Latency tracking
	bucketAggregationLatency *LatencyTracker
	quotaAggregationLatency  *LatencyTracker
	nodeRequestLatency       *LatencyTracker

	// Rate limit metrics
	rateLimitHits   int64
	rateLimitMisses int64

	mu  sync.RWMutex
	log *logrus.Entry
}

// LatencyTracker tracks latency statistics
type LatencyTracker struct {
	totalDuration time.Duration
	count         int64
	minDuration   time.Duration
	maxDuration   time.Duration
	mu            sync.RWMutex
}

// NewClusterMetrics creates a new cluster metrics tracker
func NewClusterMetrics() *ClusterMetrics {
	return &ClusterMetrics{
		bucketAggregationLatency: &LatencyTracker{minDuration: time.Hour},
		quotaAggregationLatency:  &LatencyTracker{minDuration: time.Hour},
		nodeRequestLatency:       &LatencyTracker{minDuration: time.Hour},
		log:                      logrus.WithField("component", "cluster_metrics"),
	}
}

// RecordBucketAggregation records a bucket aggregation request
func (cm *ClusterMetrics) RecordBucketAggregation(duration time.Duration, success bool) {
	atomic.AddInt64(&cm.bucketAggregationRequests, 1)

	if success {
		atomic.AddInt64(&cm.bucketAggregationSuccesses, 1)
	} else {
		atomic.AddInt64(&cm.bucketAggregationFailures, 1)
	}

	cm.bucketAggregationLatency.Record(duration)
}

// RecordQuotaAggregation records a quota aggregation request
func (cm *ClusterMetrics) RecordQuotaAggregation(duration time.Duration, success bool) {
	atomic.AddInt64(&cm.quotaAggregationRequests, 1)

	if success {
		atomic.AddInt64(&cm.quotaAggregationSuccesses, 1)
	} else {
		atomic.AddInt64(&cm.quotaAggregationFailures, 1)
	}

	cm.quotaAggregationLatency.Record(duration)
}

// RecordNodeRequest records a node communication request
func (cm *ClusterMetrics) RecordNodeRequest(duration time.Duration, success bool) {
	atomic.AddInt64(&cm.nodeRequestsTotal, 1)

	if success {
		atomic.AddInt64(&cm.nodeRequestsSuccess, 1)
	} else {
		atomic.AddInt64(&cm.nodeRequestsFailure, 1)
	}

	cm.nodeRequestLatency.Record(duration)
}

// RecordCircuitBreakerOpen records a circuit breaker opening
func (cm *ClusterMetrics) RecordCircuitBreakerOpen() {
	atomic.AddInt64(&cm.circuitBreakerOpens, 1)
}

// RecordRateLimitHit records a rate limit hit
func (cm *ClusterMetrics) RecordRateLimitHit() {
	atomic.AddInt64(&cm.rateLimitHits, 1)
}

// RecordRateLimitMiss records a rate limit miss
func (cm *ClusterMetrics) RecordRateLimitMiss() {
	atomic.AddInt64(&cm.rateLimitMisses, 1)
}

// GetStats returns current metrics statistics
func (cm *ClusterMetrics) GetStats() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return map[string]interface{}{
		"bucket_aggregation": map[string]interface{}{
			"total_requests":  atomic.LoadInt64(&cm.bucketAggregationRequests),
			"successes":       atomic.LoadInt64(&cm.bucketAggregationSuccesses),
			"failures":        atomic.LoadInt64(&cm.bucketAggregationFailures),
			"success_rate":    cm.calculateSuccessRate(cm.bucketAggregationSuccesses, cm.bucketAggregationRequests),
			"latency_ms":      cm.bucketAggregationLatency.GetStats(),
		},
		"quota_aggregation": map[string]interface{}{
			"total_requests":  atomic.LoadInt64(&cm.quotaAggregationRequests),
			"successes":       atomic.LoadInt64(&cm.quotaAggregationSuccesses),
			"failures":        atomic.LoadInt64(&cm.quotaAggregationFailures),
			"success_rate":    cm.calculateSuccessRate(cm.quotaAggregationSuccesses, cm.quotaAggregationRequests),
			"latency_ms":      cm.quotaAggregationLatency.GetStats(),
		},
		"node_requests": map[string]interface{}{
			"total":        atomic.LoadInt64(&cm.nodeRequestsTotal),
			"successes":    atomic.LoadInt64(&cm.nodeRequestsSuccess),
			"failures":     atomic.LoadInt64(&cm.nodeRequestsFailure),
			"success_rate": cm.calculateSuccessRate(cm.nodeRequestsSuccess, cm.nodeRequestsTotal),
			"latency_ms":   cm.nodeRequestLatency.GetStats(),
		},
		"circuit_breaker": map[string]interface{}{
			"total_opens": atomic.LoadInt64(&cm.circuitBreakerOpens),
		},
		"rate_limiting": map[string]interface{}{
			"hits":   atomic.LoadInt64(&cm.rateLimitHits),
			"misses": atomic.LoadInt64(&cm.rateLimitMisses),
			"total":  atomic.LoadInt64(&cm.rateLimitHits) + atomic.LoadInt64(&cm.rateLimitMisses),
		},
	}
}

// calculateSuccessRate calculates success rate percentage
func (cm *ClusterMetrics) calculateSuccessRate(successes, total int64) float64 {
	if total == 0 {
		return 0.0
	}
	return (float64(successes) / float64(total)) * 100.0
}

// Record adds a latency measurement
func (lt *LatencyTracker) Record(duration time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	lt.totalDuration += duration
	lt.count++

	if duration < lt.minDuration {
		lt.minDuration = duration
	}

	if duration > lt.maxDuration {
		lt.maxDuration = duration
	}
}

// GetStats returns latency statistics
func (lt *LatencyTracker) GetStats() map[string]interface{} {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	var avgMs float64
	if lt.count > 0 {
		avgMs = float64(lt.totalDuration.Milliseconds()) / float64(lt.count)
	}

	return map[string]interface{}{
		"count":   lt.count,
		"avg_ms":  avgMs,
		"min_ms":  lt.minDuration.Milliseconds(),
		"max_ms":  lt.maxDuration.Milliseconds(),
	}
}

// Reset resets all metrics to zero
func (cm *ClusterMetrics) Reset() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	atomic.StoreInt64(&cm.bucketAggregationRequests, 0)
	atomic.StoreInt64(&cm.quotaAggregationRequests, 0)
	atomic.StoreInt64(&cm.bucketAggregationSuccesses, 0)
	atomic.StoreInt64(&cm.bucketAggregationFailures, 0)
	atomic.StoreInt64(&cm.quotaAggregationSuccesses, 0)
	atomic.StoreInt64(&cm.quotaAggregationFailures, 0)
	atomic.StoreInt64(&cm.nodeRequestsTotal, 0)
	atomic.StoreInt64(&cm.nodeRequestsSuccess, 0)
	atomic.StoreInt64(&cm.nodeRequestsFailure, 0)
	atomic.StoreInt64(&cm.circuitBreakerOpens, 0)
	atomic.StoreInt64(&cm.rateLimitHits, 0)
	atomic.StoreInt64(&cm.rateLimitMisses, 0)

	cm.bucketAggregationLatency = &LatencyTracker{minDuration: time.Hour}
	cm.quotaAggregationLatency = &LatencyTracker{minDuration: time.Hour}
	cm.nodeRequestLatency = &LatencyTracker{minDuration: time.Hour}

	cm.log.Info("Cluster metrics reset")
}

// LogStats logs current statistics
func (cm *ClusterMetrics) LogStats() {
	stats := cm.GetStats()
	cm.log.WithField("stats", stats).Info("Cluster metrics snapshot")
}
