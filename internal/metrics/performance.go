package metrics

import (
	"sort"
	"sync"
	"time"
)

// OperationType represents different types of operations
type OperationType string

const (
	OpPutObject         OperationType = "PutObject"
	OpGetObject         OperationType = "GetObject"
	OpDeleteObject      OperationType = "DeleteObject"
	OpListObjects       OperationType = "ListObjects"
	OpHeadObject        OperationType = "HeadObject"
	OpCopyObject        OperationType = "CopyObject"
	OpMultipartUpload   OperationType = "MultipartUpload"
	OpMetadataOperation OperationType = "MetadataOperation"
	OpClusterProxy      OperationType = "ClusterProxy"
	OpDatabaseQuery     OperationType = "DatabaseQuery"
	OpFilesystemIO      OperationType = "FilesystemIO"
)

// OperationLatency stores latency information for a single operation
type OperationLatency struct {
	Operation OperationType
	Duration  time.Duration
	Timestamp time.Time
	Success   bool
}

// LatencyStats contains percentile statistics for an operation
type LatencyStats struct {
	Operation    OperationType `json:"operation"`
	Count        int64         `json:"count"`
	P50          float64       `json:"p50_ms"`
	P95          float64       `json:"p95_ms"`
	P99          float64       `json:"p99_ms"`
	Mean         float64       `json:"mean_ms"`
	Min          float64       `json:"min_ms"`
	Max          float64       `json:"max_ms"`
	SuccessRate  float64       `json:"success_rate"`
	ErrorCount   int64         `json:"error_count"`
	LastRecorded time.Time     `json:"last_recorded"`
}

// ThroughputStats contains throughput metrics
type ThroughputStats struct {
	RequestsPerSecond float64   `json:"requests_per_second"`
	BytesPerSecond    int64     `json:"bytes_per_second"`
	ObjectsPerSecond  float64   `json:"objects_per_second"`
	Timestamp         time.Time `json:"timestamp"`
}

// PerformanceCollector collects and aggregates performance metrics
type PerformanceCollector struct {
	mu         sync.RWMutex
	latencies  map[OperationType][]OperationLatency
	maxSamples int           // Maximum samples to keep per operation (rolling window)
	retention  time.Duration // How long to keep samples

	// Throughput tracking
	requestCount  int64
	bytesProcessed int64
	objectsProcessed int64
	lastThroughputCalc time.Time

	// Current throughput (calculated periodically)
	currentThroughput ThroughputStats
}

// NewPerformanceCollector creates a new performance collector
func NewPerformanceCollector(maxSamples int, retention time.Duration) *PerformanceCollector {
	return &PerformanceCollector{
		latencies:          make(map[OperationType][]OperationLatency),
		maxSamples:         maxSamples,
		retention:          retention,
		lastThroughputCalc: time.Now(),
	}
}

// RecordLatency records a latency measurement for an operation
func (pc *PerformanceCollector) RecordLatency(op OperationType, duration time.Duration, success bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	latency := OperationLatency{
		Operation: op,
		Duration:  duration,
		Timestamp: time.Now(),
		Success:   success,
	}

	// Initialize slice if needed
	if pc.latencies[op] == nil {
		pc.latencies[op] = make([]OperationLatency, 0, pc.maxSamples)
	}

	// Add new latency
	pc.latencies[op] = append(pc.latencies[op], latency)

	// Remove old samples if exceeded max
	if len(pc.latencies[op]) > pc.maxSamples {
		pc.latencies[op] = pc.latencies[op][len(pc.latencies[op])-pc.maxSamples:]
	}

	// Clean up old samples based on retention period
	cutoff := time.Now().Add(-pc.retention)
	for i, l := range pc.latencies[op] {
		if l.Timestamp.After(cutoff) {
			pc.latencies[op] = pc.latencies[op][i:]
			break
		}
	}
}

// RecordThroughput records throughput data
func (pc *PerformanceCollector) RecordThroughput(bytes int64, objects int) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	pc.requestCount++
	pc.bytesProcessed += bytes
	pc.objectsProcessed += int64(objects)
}

// CalculateThroughput calculates current throughput (should be called periodically)
func (pc *PerformanceCollector) CalculateThroughput() ThroughputStats {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(pc.lastThroughputCalc).Seconds()

	if elapsed == 0 {
		return pc.currentThroughput
	}

	stats := ThroughputStats{
		RequestsPerSecond: float64(pc.requestCount) / elapsed,
		BytesPerSecond:    int64(float64(pc.bytesProcessed) / elapsed),
		ObjectsPerSecond:  float64(pc.objectsProcessed) / elapsed,
		Timestamp:         now,
	}

	// Reset counters
	pc.requestCount = 0
	pc.bytesProcessed = 0
	pc.objectsProcessed = 0
	pc.lastThroughputCalc = now
	pc.currentThroughput = stats

	return stats
}

// GetLatencyStats calculates latency statistics for a specific operation
func (pc *PerformanceCollector) GetLatencyStats(op OperationType) *LatencyStats {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	latencies := pc.latencies[op]
	if len(latencies) == 0 {
		return &LatencyStats{
			Operation: op,
			Count:     0,
		}
	}

	// Extract durations and sort for percentile calculation
	durations := make([]float64, 0, len(latencies))
	successCount := int64(0)
	errorCount := int64(0)
	sum := float64(0)
	minDuration := float64(latencies[0].Duration.Milliseconds())
	maxDuration := float64(latencies[0].Duration.Milliseconds())
	lastRecorded := latencies[0].Timestamp

	for _, l := range latencies {
		durationMs := float64(l.Duration.Milliseconds())
		durations = append(durations, durationMs)
		sum += durationMs

		if durationMs < minDuration {
			minDuration = durationMs
		}
		if durationMs > maxDuration {
			maxDuration = durationMs
		}

		if l.Success {
			successCount++
		} else {
			errorCount++
		}

		if l.Timestamp.After(lastRecorded) {
			lastRecorded = l.Timestamp
		}
	}

	sort.Float64s(durations)

	stats := &LatencyStats{
		Operation:    op,
		Count:        int64(len(durations)),
		P50:          calculatePercentile(durations, 50),
		P95:          calculatePercentile(durations, 95),
		P99:          calculatePercentile(durations, 99),
		Mean:         sum / float64(len(durations)),
		Min:          minDuration,
		Max:          maxDuration,
		SuccessRate:  float64(successCount) / float64(len(latencies)) * 100,
		ErrorCount:   errorCount,
		LastRecorded: lastRecorded,
	}

	return stats
}

// GetAllLatencyStats returns latency statistics for all operations
func (pc *PerformanceCollector) GetAllLatencyStats() map[OperationType]*LatencyStats {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	stats := make(map[OperationType]*LatencyStats)
	for op := range pc.latencies {
		// Temporarily unlock to call GetLatencyStats (which needs read lock)
		pc.mu.RUnlock()
		stats[op] = pc.GetLatencyStats(op)
		pc.mu.RLock()
	}

	return stats
}

// GetCurrentThroughput returns the most recently calculated throughput
func (pc *PerformanceCollector) GetCurrentThroughput() ThroughputStats {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.currentThroughput
}

// GetLatencyHistory returns recent latency measurements for an operation
func (pc *PerformanceCollector) GetLatencyHistory(op OperationType, limit int) []OperationLatency {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	latencies := pc.latencies[op]
	if len(latencies) == 0 {
		return []OperationLatency{}
	}

	// Return last N samples
	start := 0
	if len(latencies) > limit {
		start = len(latencies) - limit
	}

	// Make a copy to avoid data races
	result := make([]OperationLatency, len(latencies)-start)
	copy(result, latencies[start:])

	return result
}

// Reset clears all collected metrics
func (pc *PerformanceCollector) Reset() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	pc.latencies = make(map[OperationType][]OperationLatency)
	pc.requestCount = 0
	pc.bytesProcessed = 0
	pc.objectsProcessed = 0
	pc.lastThroughputCalc = time.Now()
	pc.currentThroughput = ThroughputStats{}
}

// calculatePercentile calculates the percentile value from sorted data
func calculatePercentile(sortedData []float64, percentile int) float64 {
	if len(sortedData) == 0 {
		return 0
	}

	if percentile <= 0 {
		return sortedData[0]
	}
	if percentile >= 100 {
		return sortedData[len(sortedData)-1]
	}

	// Calculate index (percentile rank)
	rank := float64(percentile) / 100.0 * float64(len(sortedData)-1)
	lowerIndex := int(rank)
	upperIndex := lowerIndex + 1

	if upperIndex >= len(sortedData) {
		return sortedData[lowerIndex]
	}

	// Linear interpolation between two values
	weight := rank - float64(lowerIndex)
	return sortedData[lowerIndex]*(1-weight) + sortedData[upperIndex]*weight
}

// Global performance collector instance
var globalPerformanceCollector *PerformanceCollector

// InitGlobalPerformanceCollector initializes the global performance collector
func InitGlobalPerformanceCollector(maxSamples int, retention time.Duration) {
	globalPerformanceCollector = NewPerformanceCollector(maxSamples, retention)

	// Start background goroutine to calculate throughput every 5 seconds
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			globalPerformanceCollector.CalculateThroughput()
		}
	}()
}

// GetGlobalPerformanceCollector returns the global performance collector
func GetGlobalPerformanceCollector() *PerformanceCollector {
	return globalPerformanceCollector
}
