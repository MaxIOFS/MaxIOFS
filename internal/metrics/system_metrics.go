package metrics

import (
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// SystemMetricsTracker tracks system-level metrics like CPU, memory, disk, uptime, and requests
type SystemMetricsTracker struct {
	startTime      time.Time
	dataDir        string
	requestCount   atomic.Uint64
	errorCount     atomic.Uint64
	totalLatencyMs atomic.Uint64
}

// NewSystemMetrics creates a new SystemMetricsTracker instance
func NewSystemMetrics(dataDir string) *SystemMetricsTracker {
	return &SystemMetricsTracker{
		startTime: time.Now(),
		dataDir:   dataDir,
	}
}

// GetUptime returns the system uptime in seconds
func (sm *SystemMetricsTracker) GetUptime() int64 {
	return int64(time.Since(sm.startTime).Seconds())
}

// CPUStats represents CPU usage and information
type CPUStats struct {
	UsagePercent float64 `json:"usage_percent"`
	Cores        int     `json:"cores"`
	LogicalCores int     `json:"logical_cores"`
	FrequencyMHz float64 `json:"frequency_mhz"`
	ModelName    string  `json:"model_name"`
}

// GetCPUUsage returns current CPU usage percentage
func (sm *SystemMetricsTracker) GetCPUUsage() (float64, error) {
	percentages, err := cpu.Percent(time.Second, false)
	if err != nil || len(percentages) == 0 {
		return 0.0, err
	}
	return percentages[0], nil
}

// GetCPUStats returns detailed CPU statistics
func (sm *SystemMetricsTracker) GetCPUStats() (*CPUStats, error) {
	// Get CPU usage
	percentages, err := cpu.Percent(time.Second, false)
	usagePercent := 0.0
	if err == nil && len(percentages) > 0 {
		usagePercent = percentages[0]
	}

	// Get physical and logical CPU counts
	physicalCores, _ := cpu.Counts(false)
	logicalCores, _ := cpu.Counts(true)

	// Get CPU info (model, frequency, etc.)
	cpuInfo, err := cpu.Info()
	modelName := "Unknown"
	frequencyMHz := 0.0
	if err == nil && len(cpuInfo) > 0 {
		modelName = cpuInfo[0].ModelName
		frequencyMHz = cpuInfo[0].Mhz
	}

	return &CPUStats{
		UsagePercent: usagePercent,
		Cores:        physicalCores,
		LogicalCores: logicalCores,
		FrequencyMHz: frequencyMHz,
		ModelName:    modelName,
	}, nil
}

// MemoryStats represents memory usage statistics
type MemoryStats struct {
	UsedPercent float64 `json:"used_percent"`
	UsedBytes   uint64  `json:"used_bytes"`
	TotalBytes  uint64  `json:"total_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
}

// GetMemoryUsage returns current memory usage statistics
func (sm *SystemMetricsTracker) GetMemoryUsage() (*MemoryStats, error) {
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	return &MemoryStats{
		UsedPercent: memInfo.UsedPercent,
		UsedBytes:   memInfo.Used,
		TotalBytes:  memInfo.Total,
		FreeBytes:   memInfo.Free,
	}, nil
}

// DiskStats represents disk usage statistics
type DiskStats struct {
	UsedPercent float64 `json:"used_percent"`
	UsedBytes   uint64  `json:"used_bytes"`
	TotalBytes  uint64  `json:"total_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
}

// GetDiskUsage returns current disk usage statistics for the data directory
func (sm *SystemMetricsTracker) GetDiskUsage() (*DiskStats, error) {
	diskInfo, err := disk.Usage(sm.dataDir)
	if err != nil {
		return nil, err
	}

	return &DiskStats{
		UsedPercent: diskInfo.UsedPercent,
		UsedBytes:   diskInfo.Used,
		TotalBytes:  diskInfo.Total,
		FreeBytes:   diskInfo.Free,
	}, nil
}

// RequestStats represents request tracking statistics
type RequestStats struct {
	TotalRequests   uint64  `json:"total_requests"`
	TotalErrors     uint64  `json:"total_errors"`
	AverageLatency  float64 `json:"average_latency_ms"`
	RequestsPerSec  float64 `json:"requests_per_sec"`
}

// GetRequestStats returns request tracking statistics
func (sm *SystemMetricsTracker) GetRequestStats() *RequestStats {
	totalRequests := sm.requestCount.Load()
	totalErrors := sm.errorCount.Load()
	totalLatency := sm.totalLatencyMs.Load()

	var avgLatency float64
	if totalRequests > 0 {
		avgLatency = float64(totalLatency) / float64(totalRequests)
	}

	uptime := time.Since(sm.startTime).Seconds()
	var reqPerSec float64
	if uptime > 0 {
		reqPerSec = float64(totalRequests) / uptime
	}

	return &RequestStats{
		TotalRequests:  totalRequests,
		TotalErrors:    totalErrors,
		AverageLatency: avgLatency,
		RequestsPerSec: reqPerSec,
	}
}

// PerformanceStats represents performance metrics
type PerformanceStats struct {
	Uptime          int64   `json:"uptime_seconds"`
	GoRoutines      int     `json:"goroutines"`
	HeapAllocMB     float64 `json:"heap_alloc_mb"`
	TotalAllocMB    float64 `json:"total_alloc_mb"`
	GCRuns          uint32  `json:"gc_runs"`
}

// GetPerformanceStats returns performance statistics
func (sm *SystemMetricsTracker) GetPerformanceStats() *PerformanceStats {
	collector := NewCollector(sm.dataDir)
	runtimeMetrics, _ := collector.CollectRuntimeMetrics()

	return &PerformanceStats{
		Uptime:       sm.GetUptime(),
		GoRoutines:   runtimeMetrics.GoRoutines,
		HeapAllocMB:  float64(runtimeMetrics.HeapAlloc) / 1024 / 1024,
		TotalAllocMB: float64(runtimeMetrics.HeapSys) / 1024 / 1024,
		GCRuns:       uint32(runtimeMetrics.NumGC),
	}
}

// RecordRequest records a request with its latency
func (sm *SystemMetricsTracker) RecordRequest(latencyMs uint64, isError bool) {
	sm.requestCount.Add(1)
	sm.totalLatencyMs.Add(latencyMs)
	if isError {
		sm.errorCount.Add(1)
	}
}
