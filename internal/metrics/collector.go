package metrics

import (
	"context"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// Collector handles collection of system and custom metrics
type Collector interface {
	// System metrics collection
	CollectSystemMetrics() (*SystemMetrics, error)
	CollectRuntimeMetrics() (*RuntimeMetrics, error)

	// Custom metrics collection
	CollectStorageMetrics(ctx context.Context) (*StorageMetrics, error)
	CollectS3Metrics(ctx context.Context) (*S3Metrics, error)

	// Background collection
	StartBackgroundCollection(ctx context.Context, manager Manager, interval time.Duration)
	StopBackgroundCollection()

	// Health
	IsHealthy() bool
}

// SystemMetrics holds system-level metrics
type SystemMetrics struct {
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	MemoryUsedBytes    int64   `json:"memory_used_bytes"`
	MemoryTotalBytes   int64   `json:"memory_total_bytes"`
	DiskUsagePercent   float64 `json:"disk_usage_percent"`
	DiskUsedBytes      int64   `json:"disk_used_bytes"`
	DiskTotalBytes     int64   `json:"disk_total_bytes"`
	OpenFileDescriptors int64  `json:"open_file_descriptors"`
	NetworkBytesIn     int64   `json:"network_bytes_in"`
	NetworkBytesOut    int64   `json:"network_bytes_out"`
	Timestamp          int64   `json:"timestamp"`
}

// RuntimeMetrics holds Go runtime metrics
type RuntimeMetrics struct {
	GoVersion        string  `json:"go_version"`
	GoRoutines       int     `json:"goroutines"`
	Threads          int     `json:"threads"`
	GCPauses         int64   `json:"gc_pauses"`
	HeapAlloc        int64   `json:"heap_alloc"`
	HeapSys          int64   `json:"heap_sys"`
	HeapInuse        int64   `json:"heap_inuse"`
	HeapIdle         int64   `json:"heap_idle"`
	HeapReleased     int64   `json:"heap_released"`
	StackInuse       int64   `json:"stack_inuse"`
	StackSys         int64   `json:"stack_sys"`
	MSpanInuse       int64   `json:"mspan_inuse"`
	MSpanSys         int64   `json:"mspan_sys"`
	MCacheInuse      int64   `json:"mcache_inuse"`
	MCacheSys        int64   `json:"mcache_sys"`
	NextGC           int64   `json:"next_gc"`
	LastGC           int64   `json:"last_gc"`
	PauseTotalNs     int64   `json:"pause_total_ns"`
	NumGC            int64   `json:"num_gc"`
	NumForcedGC      int64   `json:"num_forced_gc"`
	GCCPUFraction    float64 `json:"gc_cpu_fraction"`
	Timestamp        int64   `json:"timestamp"`
}

// StorageMetrics holds storage-related metrics
type StorageMetrics struct {
	TotalBuckets        int64             `json:"total_buckets"`
	TotalObjects        int64             `json:"total_objects"`
	TotalBytes          int64             `json:"total_bytes"`
	BucketMetrics       map[string]BucketMetric `json:"bucket_metrics"`
	StorageOperations   map[string]int64  `json:"storage_operations"`
	AverageObjectSize   float64           `json:"average_object_size"`
	LargestObjectSize   int64             `json:"largest_object_size"`
	SmallestObjectSize  int64             `json:"smallest_object_size"`
	ObjectSizeDistribution map[string]int64 `json:"object_size_distribution"`
	Timestamp           int64             `json:"timestamp"`
}

// BucketMetric holds metrics for a specific bucket
type BucketMetric struct {
	Name         string `json:"name"`
	ObjectCount  int64  `json:"object_count"`
	TotalSize    int64  `json:"total_size"`
	AverageSize  float64 `json:"average_size"`
	LastModified int64  `json:"last_modified"`
}

// S3Metrics holds S3 API specific metrics
type S3Metrics struct {
	RequestsTotal       map[string]int64 `json:"requests_total"`
	ErrorsTotal         map[string]int64 `json:"errors_total"`
	AverageResponseTime map[string]float64 `json:"average_response_time"`
	ActiveConnections   int64            `json:"active_connections"`
	AuthSuccessRate     float64          `json:"auth_success_rate"`
	AuthFailures        map[string]int64 `json:"auth_failures"`
	Timestamp           int64            `json:"timestamp"`
}

// collector implements the Collector interface
type collector struct {
	running    bool
	stopChan   chan struct{}
	interval   time.Duration
	lastCPU    time.Duration
	lastTime   time.Time
	startTime  time.Time
	dataDir    string
}

// NewCollector creates a new metrics collector
func NewCollector(dataDir string) Collector {
	return &collector{
		stopChan:  make(chan struct{}),
		lastTime:  time.Now(),
		startTime: time.Now(),
		dataDir:   dataDir,
	}
}

// CollectSystemMetrics collects system-level metrics
func (c *collector) CollectSystemMetrics() (*SystemMetrics, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Get actual system memory info
	memInfo, _ := mem.VirtualMemory()

	metrics := &SystemMetrics{
		CPUUsagePercent:    c.getCPUUsage(),
		MemoryUsagePercent: c.getMemoryUsage(&m),
		MemoryUsedBytes:    int64(memInfo.Used),
		MemoryTotalBytes:   c.getTotalMemory(),
		DiskUsagePercent:   c.getDiskUsage(),
		DiskUsedBytes:      c.getDiskUsed(),
		DiskTotalBytes:     c.getDiskTotal(),
		OpenFileDescriptors: c.getOpenFileDescriptors(),
		NetworkBytesIn:     c.getNetworkBytesIn(),
		NetworkBytesOut:    c.getNetworkBytesOut(),
		Timestamp:          time.Now().Unix(),
	}

	return metrics, nil
}

// CollectRuntimeMetrics collects Go runtime metrics
func (c *collector) CollectRuntimeMetrics() (*RuntimeMetrics, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	metrics := &RuntimeMetrics{
		GoVersion:        runtime.Version(),
		GoRoutines:       runtime.NumGoroutine(),
		Threads:          runtime.GOMAXPROCS(0),
		GCPauses:         int64(len(m.PauseNs)),
		HeapAlloc:        int64(m.HeapAlloc),
		HeapSys:          int64(m.HeapSys),
		HeapInuse:        int64(m.HeapInuse),
		HeapIdle:         int64(m.HeapIdle),
		HeapReleased:     int64(m.HeapReleased),
		StackInuse:       int64(m.StackInuse),
		StackSys:         int64(m.StackSys),
		MSpanInuse:       int64(m.MSpanInuse),
		MSpanSys:         int64(m.MSpanSys),
		MCacheInuse:      int64(m.MCacheInuse),
		MCacheSys:        int64(m.MCacheSys),
		NextGC:           int64(m.NextGC),
		LastGC:           int64(m.LastGC),
		PauseTotalNs:     int64(m.PauseTotalNs),
		NumGC:            int64(m.NumGC),
		NumForcedGC:      int64(m.NumForcedGC),
		GCCPUFraction:    m.GCCPUFraction,
		Timestamp:        time.Now().Unix(),
	}

	return metrics, nil
}

// CollectStorageMetrics collects storage-related metrics
func (c *collector) CollectStorageMetrics(ctx context.Context) (*StorageMetrics, error) {
	// This would integrate with the storage manager to collect metrics
	// For MVP, return mock metrics
	metrics := &StorageMetrics{
		TotalBuckets:      0,
		TotalObjects:      0,
		TotalBytes:        0,
		BucketMetrics:     make(map[string]BucketMetric),
		StorageOperations: make(map[string]int64),
		AverageObjectSize: 0,
		LargestObjectSize: 0,
		SmallestObjectSize: 0,
		ObjectSizeDistribution: make(map[string]int64),
		Timestamp:         time.Now().Unix(),
	}

	// TODO: Integrate with actual storage backend
	// This would call storage manager methods to get real metrics

	return metrics, nil
}

// CollectS3Metrics collects S3 API specific metrics
func (c *collector) CollectS3Metrics(ctx context.Context) (*S3Metrics, error) {
	// This would integrate with the S3 API handlers to collect metrics
	// For MVP, return mock metrics
	metrics := &S3Metrics{
		RequestsTotal:       make(map[string]int64),
		ErrorsTotal:         make(map[string]int64),
		AverageResponseTime: make(map[string]float64),
		ActiveConnections:   0,
		AuthSuccessRate:     100.0,
		AuthFailures:        make(map[string]int64),
		Timestamp:           time.Now().Unix(),
	}

	// TODO: Integrate with actual S3 handlers
	// This would collect real request/response metrics

	return metrics, nil
}

// StartBackgroundCollection starts collecting metrics in the background
func (c *collector) StartBackgroundCollection(ctx context.Context, manager Manager, interval time.Duration) {
	if c.running {
		return
	}

	c.running = true
	c.interval = interval

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				c.running = false
				return
			case <-c.stopChan:
				c.running = false
				return
			case <-ticker.C:
				c.collectAndReport(ctx, manager)
			}
		}
	}()
}

// StopBackgroundCollection stops background collection
func (c *collector) StopBackgroundCollection() {
	if !c.running {
		return
	}

	close(c.stopChan)
	c.running = false
}

// IsHealthy returns the health status of the collector
func (c *collector) IsHealthy() bool {
	return true // Simple health check for MVP
}

// collectAndReport collects metrics and reports them to the manager
func (c *collector) collectAndReport(ctx context.Context, manager Manager) {
	// Collect system metrics
	sysMetrics, err := c.CollectSystemMetrics()
	if err == nil {
		manager.UpdateSystemMetrics(sysMetrics.CPUUsagePercent, sysMetrics.MemoryUsagePercent)
		manager.RecordSystemEvent("metrics_collection", map[string]string{
			"type": "system",
		})
	}

	// Collect runtime metrics
	runtimeMetrics, err := c.CollectRuntimeMetrics()
	if err == nil {
		// Report runtime metrics as custom events
		manager.RecordSystemEvent("runtime_stats", map[string]string{
			"goroutines": string(runtimeMetrics.GoRoutines),
			"gc_pauses":  string(runtimeMetrics.GCPauses),
		})
	}

	// Collect storage metrics
	storageMetrics, err := c.CollectStorageMetrics(ctx)
	if err == nil {
		// Report storage metrics
		for bucketName, bucketMetric := range storageMetrics.BucketMetrics {
			manager.UpdateBucketMetrics(bucketName, bucketMetric.ObjectCount, bucketMetric.TotalSize)
		}
	}
}

// Helper methods for system metrics collection
// Note: These are simplified implementations. In production, you'd use
// system-specific calls or libraries like gopsutil for accurate metrics

func (c *collector) getCPUUsage() float64 {
	// Get CPU usage percentage
	percentages, err := cpu.Percent(time.Second, false)
	if err != nil || len(percentages) == 0 {
		return 0.0
	}
	return percentages[0]
}

func (c *collector) getMemoryUsage(m *runtime.MemStats) float64 {
	// Get actual system memory usage
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return 0.0
	}
	return memInfo.UsedPercent
}

func (c *collector) getTotalMemory() int64 {
	// Get total system memory
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return 0
	}
	return int64(memInfo.Total)
}

func (c *collector) getDiskUsage() float64 {
	// Get disk usage percentage for data directory
	diskInfo, err := disk.Usage(c.dataDir)
	if err != nil {
		return 0.0
	}
	return diskInfo.UsedPercent
}

func (c *collector) getDiskUsed() int64 {
	// Get used disk space for data directory
	diskInfo, err := disk.Usage(c.dataDir)
	if err != nil {
		return 0
	}
	return int64(diskInfo.Used)
}

func (c *collector) getDiskTotal() int64 {
	// Get total disk space for data directory
	diskInfo, err := disk.Usage(c.dataDir)
	if err != nil {
		return 0
	}
	return int64(diskInfo.Total)
}

func (c *collector) getOpenFileDescriptors() int64 {
	// Get number of open file descriptors
	// In production, this would use system calls
	return 100 // Placeholder
}

func (c *collector) getNetworkBytesIn() int64 {
	// Get network bytes received
	// In production, this would use system calls or gopsutil
	return 1024 * 1024 * 1024 // 1GB placeholder
}

func (c *collector) getNetworkBytesOut() int64 {
	// Get network bytes sent
	// In production, this would use system calls or gopsutil
	return 512 * 1024 * 1024 // 512MB placeholder
}

// Custom Prometheus Collector implementation
// This allows the metrics system to be used as a Prometheus collector

type prometheusCollector struct {
	metricsManager Manager
	systemMetrics  *prometheus.Desc
	runtimeMetrics *prometheus.Desc
}

// NewPrometheusCollector creates a new Prometheus collector
func NewPrometheusCollector(manager Manager) prometheus.Collector {
	return &prometheusCollector{
		metricsManager: manager,
		systemMetrics: prometheus.NewDesc(
			"maxiofs_system_info",
			"System information",
			[]string{"metric", "value"},
			nil,
		),
		runtimeMetrics: prometheus.NewDesc(
			"maxiofs_runtime_info",
			"Runtime information",
			[]string{"metric", "value"},
			nil,
		),
	}
}

// Describe implements prometheus.Collector
func (pc *prometheusCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- pc.systemMetrics
	ch <- pc.runtimeMetrics
}

// Collect implements prometheus.Collector
func (pc *prometheusCollector) Collect(ch chan<- prometheus.Metric) {
	collector := NewCollector("")

	// Collect system metrics
	sysMetrics, err := collector.CollectSystemMetrics()
	if err == nil {
		ch <- prometheus.MustNewConstMetric(
			pc.systemMetrics,
			prometheus.GaugeValue,
			sysMetrics.CPUUsagePercent,
			"cpu_usage_percent", "current",
		)
		ch <- prometheus.MustNewConstMetric(
			pc.systemMetrics,
			prometheus.GaugeValue,
			sysMetrics.MemoryUsagePercent,
			"memory_usage_percent", "current",
		)
	}

	// Collect runtime metrics
	runtimeMetrics, err := collector.CollectRuntimeMetrics()
	if err == nil {
		ch <- prometheus.MustNewConstMetric(
			pc.runtimeMetrics,
			prometheus.GaugeValue,
			float64(runtimeMetrics.GoRoutines),
			"goroutines", "current",
		)
		ch <- prometheus.MustNewConstMetric(
			pc.runtimeMetrics,
			prometheus.GaugeValue,
			float64(runtimeMetrics.HeapAlloc),
			"heap_alloc_bytes", "current",
		)
	}
}