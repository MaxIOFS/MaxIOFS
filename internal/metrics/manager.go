package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// Manager defines the interface for metrics management
type Manager interface {
	// HTTP Metrics
	RecordHTTPRequest(method, path, status string, duration time.Duration)
	RecordHTTPRequestSize(method, path string, size int64)
	RecordHTTPResponseSize(method, path string, size int64)

	// S3 API Metrics
	RecordS3Operation(operation, bucket string, success bool, duration time.Duration)
	RecordS3Error(operation, bucket, errorType string)

	// Storage Metrics
	RecordStorageOperation(operation string, success bool, duration time.Duration)
	UpdateStorageUsage(bucket string, objects, bytes int64)
	RecordObjectOperation(operation, bucket string, objectSize int64, duration time.Duration)

	// Authentication Metrics
	RecordAuthAttempt(method string, success bool)
	RecordAuthFailure(method, reason string)

	// System Metrics
	UpdateSystemMetrics(cpuUsage, memoryUsage, diskUsage float64)
	RecordSystemEvent(eventType string, details map[string]string)

	// Bucket Metrics
	UpdateBucketMetrics(bucket string, objects, bytes int64)
	RecordBucketOperation(operation, bucket string, success bool)

	// Object Lock Metrics
	RecordObjectLockOperation(operation, bucket string, success bool)
	UpdateRetentionMetrics(bucket string, governanceObjects, complianceObjects int64)

	// Performance Metrics
	RecordBackgroundTask(taskType string, duration time.Duration, success bool)
	UpdateCacheMetrics(hitRate float64, size int64)

	// Export and Health
	GetMetricsHandler() http.Handler
	GetMetricsSnapshot() (map[string]interface{}, error)
	GetS3MetricsSnapshot() (map[string]interface{}, error)
	IsHealthy() bool
	Reset() error

	// Historical Metrics
	GetHistoricalMetrics(metricType string, start, end time.Time) ([]MetricSnapshot, error)
	GetHistoryStats() (map[string]interface{}, error)

	// HTTP Middleware
	Middleware() func(http.Handler) http.Handler

	// Lifecycle
	Start(ctx context.Context) error
	Stop() error
}

// StorageMetricsProvider is a function that returns current storage metrics
type StorageMetricsProvider func() (totalBuckets, totalObjects, totalSize int64)

// metricsManager implements the Manager interface using Prometheus
type metricsManager struct {
	// Configuration
	config MetricsConfig

	// Prometheus registry and metrics
	registry *prometheus.Registry

	// HTTP Metrics
	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	httpRequestSize     *prometheus.HistogramVec
	httpResponseSize    *prometheus.HistogramVec

	// S3 API Metrics
	s3OperationsTotal   *prometheus.CounterVec
	s3OperationDuration *prometheus.HistogramVec
	s3ErrorsTotal       *prometheus.CounterVec

	// Storage Metrics
	storageOperationsTotal   *prometheus.CounterVec
	storageOperationDuration *prometheus.HistogramVec
	storageBytesTotal        *prometheus.GaugeVec
	storageObjectsTotal      *prometheus.GaugeVec

	// Object Metrics
	objectOperationsTotal   *prometheus.CounterVec
	objectOperationDuration *prometheus.HistogramVec
	objectSizeBytes         *prometheus.HistogramVec

	// Authentication Metrics
	authAttemptsTotal *prometheus.CounterVec
	authFailuresTotal *prometheus.CounterVec

	// System Metrics
	systemCPUUsage         prometheus.Gauge
	systemMemoryUsage      prometheus.Gauge
	systemDiskUsagePercent prometheus.Gauge
	systemDiskUsedBytes    prometheus.Gauge
	systemDiskTotalBytes   prometheus.Gauge
	systemEventsTotal      *prometheus.CounterVec

	// Bucket Metrics
	bucketObjectsTotal *prometheus.GaugeVec
	bucketBytesTotal   *prometheus.GaugeVec
	bucketOpsTotal     *prometheus.CounterVec

	// Object Lock Metrics
	objectLockOpsTotal    *prometheus.CounterVec
	retentionObjectsTotal *prometheus.GaugeVec

	// Performance Metrics
	backgroundTasksTotal   *prometheus.CounterVec
	backgroundTaskDuration *prometheus.HistogramVec
	cacheHitRate           prometheus.Gauge
	cacheSizeBytes         prometheus.Gauge

	// Operation Latency Metrics (from PerformanceCollector)
	operationLatencyP50 *prometheus.GaugeVec
	operationLatencyP95 *prometheus.GaugeVec
	operationLatencyP99 *prometheus.GaugeVec
	operationLatencyMean *prometheus.GaugeVec
	operationSuccessRate *prometheus.GaugeVec
	operationCount       *prometheus.GaugeVec

	// Throughput Metrics (from PerformanceCollector)
	throughputRequestsPerSecond prometheus.Gauge
	throughputBytesPerSecond    prometheus.Gauge
	throughputObjectsPerSecond  prometheus.Gauge

	// Aggregate tracking for quick access
	totalRequests     uint64
	totalErrors       uint64
	totalLatencyMs    uint64
	latencyCount      uint64
	requestsStartTime time.Time
	serverStartTime   time.Time // Actual server start time (persisted)

	// Historical metrics storage
	historyStore HistoryStoreInterface
	dataDir      string

	// System metrics tracker
	systemMetrics *SystemMetricsTracker

	// Storage metrics provider
	storageMetricsProvider StorageMetricsProvider

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.RWMutex
}

// MetricsConfig holds configuration for the metrics system
type MetricsConfig struct {
	Enabled   bool              `json:"enabled"`
	Path      string            `json:"path"`
	Namespace string            `json:"namespace"`
	Subsystem string            `json:"subsystem"`
	Interval  time.Duration     `json:"interval"`
	Labels    map[string]string `json:"labels"`
}

// NewManager creates a new metrics manager
func NewManager(cfg config.MetricsConfig) Manager {
	return NewManagerWithDataDir(cfg, "")
}

// NewManagerWithDataDir creates a new metrics manager with a custom data directory
// Deprecated: Use NewManagerWithStore instead
func NewManagerWithDataDir(cfg config.MetricsConfig, dataDir string) Manager {
	return NewManagerWithStore(cfg, dataDir, nil)
}

// NewManagerWithStore creates a new metrics manager with BadgerDB backing
func NewManagerWithStore(cfg config.MetricsConfig, dataDir string, metadataStore interface{}) Manager {
	// Convert config.MetricsConfig to our internal MetricsConfig
	metricsConfig := MetricsConfig{
		Enabled:   cfg.Enable,
		Path:      cfg.Path,
		Namespace: "maxiofs", // Default namespace
		Interval:  time.Duration(cfg.Interval) * time.Second,
	}

	if !metricsConfig.Enabled {
		return &noopManager{}
	}

	// Set defaults
	if metricsConfig.Namespace == "" {
		metricsConfig.Namespace = "maxiofs"
	}
	if metricsConfig.Path == "" {
		metricsConfig.Path = "/metrics"
	}
	if metricsConfig.Interval == 0 {
		metricsConfig.Interval = 10 * time.Second // Default: collect every 10 seconds (was 60s)
	}

	registry := prometheus.NewRegistry()

	manager := &metricsManager{
		config:            metricsConfig,
		registry:          registry,
		requestsStartTime: time.Now(),
		serverStartTime:   time.Now(), // Will be updated from persisted value if available
		dataDir:           dataDir,
	}

	// Initialize BadgerDB history store if metadata store is provided
	logrus.WithFields(logrus.Fields{
		"metadataStore_nil":  metadataStore == nil,
		"metadataStore_type": fmt.Sprintf("%T", metadataStore),
		"dataDir":            dataDir,
	}).Info("Initializing metrics history store")

	if metadataStore != nil {
		// Use BadgerDB history store - cast to metadata.Store
		badgerHistory, err := NewBadgerHistoryStore(metadataStore, 365)
		if err != nil {
			logrus.WithError(err).Warn("Failed to initialize BadgerDB metrics history store")
		} else {
			manager.historyStore = badgerHistory
			logrus.Info("Metrics history store using BadgerDB")

			// Restore persisted counters from BadgerDB
			if err := manager.restorePersistedCounters(); err != nil {
				logrus.WithError(err).Warn("Failed to restore persisted counters, starting fresh")
			}
		}
	} else if dataDir != "" {
		// Fallback to SQLite for backward compatibility
		historyStore, err := NewHistoryStore(dataDir, 365)
		if err != nil {
			logrus.WithError(err).Warn("Failed to initialize SQLite metrics history store")
		} else {
			manager.historyStore = historyStore
			logrus.Warn("Metrics history store using SQLite (deprecated, switch to BadgerDB)")
		}
	}

	manager.initializeMetrics()
	return manager
}

// initializeMetrics sets up all Prometheus metrics
func (m *metricsManager) initializeMetrics() {
	namespace := m.config.Namespace

	// HTTP Metrics
	m.httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	m.httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	m.httpRequestSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_size_bytes",
			Help:      "HTTP request size in bytes",
			Buckets:   prometheus.ExponentialBuckets(1024, 2, 10), // 1KB to 512MB
		},
		[]string{"method", "path"},
	)

	m.httpResponseSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "response_size_bytes",
			Help:      "HTTP response size in bytes",
			Buckets:   prometheus.ExponentialBuckets(1024, 2, 10), // 1KB to 512MB
		},
		[]string{"method", "path"},
	)

	// S3 API Metrics
	m.s3OperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "s3",
			Name:      "operations_total",
			Help:      "Total number of S3 operations",
		},
		[]string{"operation", "bucket", "status"},
	)

	m.s3OperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "s3",
			Name:      "operation_duration_seconds",
			Help:      "S3 operation duration in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"operation", "bucket"},
	)

	m.s3ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "s3",
			Name:      "errors_total",
			Help:      "Total number of S3 errors",
		},
		[]string{"operation", "bucket", "error_type"},
	)

	// Storage Metrics
	m.storageOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "storage",
			Name:      "operations_total",
			Help:      "Total number of storage operations",
		},
		[]string{"operation", "status"},
	)

	m.storageOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "storage",
			Name:      "operation_duration_seconds",
			Help:      "Storage operation duration in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	m.storageBytesTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "storage",
			Name:      "bytes_total",
			Help:      "Total storage bytes used",
		},
		[]string{"bucket"},
	)

	m.storageObjectsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "storage",
			Name:      "objects_total",
			Help:      "Total number of objects in storage",
		},
		[]string{"bucket"},
	)

	// Object Metrics
	m.objectOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "object",
			Name:      "operations_total",
			Help:      "Total number of object operations",
		},
		[]string{"operation", "bucket"},
	)

	m.objectOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "object",
			Name:      "operation_duration_seconds",
			Help:      "Object operation duration in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"operation", "bucket"},
	)

	m.objectSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "object",
			Name:      "size_bytes",
			Help:      "Object size in bytes",
			Buckets:   prometheus.ExponentialBuckets(1024, 2, 20), // 1KB to 512GB
		},
		[]string{"bucket"},
	)

	// Authentication Metrics
	m.authAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "auth",
			Name:      "attempts_total",
			Help:      "Total number of authentication attempts",
		},
		[]string{"method", "status"},
	)

	m.authFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "auth",
			Name:      "failures_total",
			Help:      "Total number of authentication failures",
		},
		[]string{"method", "reason"},
	)

	// System Metrics
	m.systemCPUUsage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "cpu_usage_percent",
			Help:      "System CPU usage percentage",
		},
	)

	m.systemMemoryUsage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "memory_usage_percent",
			Help:      "System memory usage percentage",
		},
	)

	m.systemDiskUsagePercent = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "disk_usage_percent",
			Help:      "System disk usage percentage",
		},
	)

	m.systemDiskUsedBytes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "disk_used_bytes",
			Help:      "System disk used bytes",
		},
	)

	m.systemDiskTotalBytes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "disk_total_bytes",
			Help:      "System disk total bytes",
		},
	)

	m.systemEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "events_total",
			Help:      "Total number of system events",
		},
		[]string{"event_type"},
	)

	// Bucket Metrics
	m.bucketObjectsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "bucket",
			Name:      "objects_total",
			Help:      "Total number of objects per bucket",
		},
		[]string{"bucket"},
	)

	m.bucketBytesTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "bucket",
			Name:      "bytes_total",
			Help:      "Total bytes per bucket",
		},
		[]string{"bucket"},
	)

	m.bucketOpsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "bucket",
			Name:      "operations_total",
			Help:      "Total bucket operations",
		},
		[]string{"operation", "bucket", "status"},
	)

	// Object Lock Metrics
	m.objectLockOpsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "object_lock",
			Name:      "operations_total",
			Help:      "Total object lock operations",
		},
		[]string{"operation", "bucket", "status"},
	)

	m.retentionObjectsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "object_lock",
			Name:      "retention_objects_total",
			Help:      "Total objects under retention",
		},
		[]string{"bucket", "mode"},
	)

	// Performance Metrics
	m.backgroundTasksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "background",
			Name:      "tasks_total",
			Help:      "Total background tasks executed",
		},
		[]string{"task_type", "status"},
	)

	m.backgroundTaskDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "background",
			Name:      "task_duration_seconds",
			Help:      "Background task duration in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"task_type"},
	)

	m.cacheHitRate = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "hit_rate",
			Help:      "Cache hit rate",
		},
	)

	m.cacheSizeBytes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "size_bytes",
			Help:      "Cache size in bytes",
		},
	)

	// Operation Latency Metrics (from PerformanceCollector)
	m.operationLatencyP50 = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "operation",
			Name:      "latency_p50_milliseconds",
			Help:      "P50 (median) operation latency in milliseconds",
		},
		[]string{"operation"},
	)

	m.operationLatencyP95 = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "operation",
			Name:      "latency_p95_milliseconds",
			Help:      "P95 operation latency in milliseconds",
		},
		[]string{"operation"},
	)

	m.operationLatencyP99 = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "operation",
			Name:      "latency_p99_milliseconds",
			Help:      "P99 operation latency in milliseconds",
		},
		[]string{"operation"},
	)

	m.operationLatencyMean = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "operation",
			Name:      "latency_mean_milliseconds",
			Help:      "Mean operation latency in milliseconds",
		},
		[]string{"operation"},
	)

	m.operationSuccessRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "operation",
			Name:      "success_rate_percent",
			Help:      "Operation success rate in percent (0-100)",
		},
		[]string{"operation"},
	)

	m.operationCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "operation",
			Name:      "count_total",
			Help:      "Total operation count",
		},
		[]string{"operation"},
	)

	// Throughput Metrics (from PerformanceCollector)
	m.throughputRequestsPerSecond = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "throughput",
			Name:      "requests_per_second",
			Help:      "Current throughput in requests per second",
		},
	)

	m.throughputBytesPerSecond = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "throughput",
			Name:      "bytes_per_second",
			Help:      "Current throughput in bytes per second",
		},
	)

	m.throughputObjectsPerSecond = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "throughput",
			Name:      "objects_per_second",
			Help:      "Current throughput in objects per second",
		},
	)

	// Register all metrics
	m.registerMetrics()
}

// registerMetrics registers all metrics with the Prometheus registry
func (m *metricsManager) registerMetrics() {
	metrics := []prometheus.Collector{
		// HTTP
		m.httpRequestsTotal,
		m.httpRequestDuration,
		m.httpRequestSize,
		m.httpResponseSize,

		// S3
		m.s3OperationsTotal,
		m.s3OperationDuration,
		m.s3ErrorsTotal,

		// Storage
		m.storageOperationsTotal,
		m.storageOperationDuration,
		m.storageBytesTotal,
		m.storageObjectsTotal,

		// Object
		m.objectOperationsTotal,
		m.objectOperationDuration,
		m.objectSizeBytes,

		// Auth
		m.authAttemptsTotal,
		m.authFailuresTotal,

		// System
		m.systemCPUUsage,
		m.systemMemoryUsage,
		m.systemDiskUsagePercent,
		m.systemDiskUsedBytes,
		m.systemDiskTotalBytes,
		m.systemEventsTotal,

		// Bucket
		m.bucketObjectsTotal,
		m.bucketBytesTotal,
		m.bucketOpsTotal,

		// Object Lock
		m.objectLockOpsTotal,
		m.retentionObjectsTotal,

		// Performance
		m.backgroundTasksTotal,
		m.backgroundTaskDuration,
		m.cacheHitRate,
		m.cacheSizeBytes,

		// Operation Latency (from PerformanceCollector)
		m.operationLatencyP50,
		m.operationLatencyP95,
		m.operationLatencyP99,
		m.operationLatencyMean,
		m.operationSuccessRate,
		m.operationCount,

		// Throughput (from PerformanceCollector)
		m.throughputRequestsPerSecond,
		m.throughputBytesPerSecond,
		m.throughputObjectsPerSecond,
	}

	for _, metric := range metrics {
		m.registry.MustRegister(metric)
	}
}

// HTTP Metrics Implementation

func (m *metricsManager) RecordHTTPRequest(method, path, status string, duration time.Duration) {
	m.httpRequestsTotal.WithLabelValues(method, path, status).Inc()
	m.httpRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())

	// Track aggregates
	atomic.AddUint64(&m.totalRequests, 1)
	atomic.AddUint64(&m.totalLatencyMs, uint64(duration.Milliseconds()))
	atomic.AddUint64(&m.latencyCount, 1)

	// Track errors (4xx and 5xx status codes)
	if len(status) > 0 && (status[0] == '4' || status[0] == '5') {
		atomic.AddUint64(&m.totalErrors, 1)
	}
}

func (m *metricsManager) RecordHTTPRequestSize(method, path string, size int64) {
	m.httpRequestSize.WithLabelValues(method, path).Observe(float64(size))
}

func (m *metricsManager) RecordHTTPResponseSize(method, path string, size int64) {
	m.httpResponseSize.WithLabelValues(method, path).Observe(float64(size))
}

// S3 API Metrics Implementation

func (m *metricsManager) RecordS3Operation(operation, bucket string, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.s3OperationsTotal.WithLabelValues(operation, bucket, status).Inc()
	m.s3OperationDuration.WithLabelValues(operation, bucket).Observe(duration.Seconds())
}

func (m *metricsManager) RecordS3Error(operation, bucket, errorType string) {
	m.s3ErrorsTotal.WithLabelValues(operation, bucket, errorType).Inc()
}

// Storage Metrics Implementation

func (m *metricsManager) RecordStorageOperation(operation string, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.storageOperationsTotal.WithLabelValues(operation, status).Inc()
	m.storageOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

func (m *metricsManager) UpdateStorageUsage(bucket string, objects, bytes int64) {
	m.storageObjectsTotal.WithLabelValues(bucket).Set(float64(objects))
	m.storageBytesTotal.WithLabelValues(bucket).Set(float64(bytes))
}

func (m *metricsManager) RecordObjectOperation(operation, bucket string, objectSize int64, duration time.Duration) {
	m.objectOperationsTotal.WithLabelValues(operation, bucket).Inc()
	m.objectOperationDuration.WithLabelValues(operation, bucket).Observe(duration.Seconds())
	m.objectSizeBytes.WithLabelValues(bucket).Observe(float64(objectSize))
}

// Authentication Metrics Implementation

func (m *metricsManager) RecordAuthAttempt(method string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.authAttemptsTotal.WithLabelValues(method, status).Inc()
}

func (m *metricsManager) RecordAuthFailure(method, reason string) {
	m.authFailuresTotal.WithLabelValues(method, reason).Inc()
}

// System Metrics Implementation

func (m *metricsManager) UpdateSystemMetrics(cpuUsage, memoryUsage, diskUsage float64) {
	m.systemCPUUsage.Set(cpuUsage)
	m.systemMemoryUsage.Set(memoryUsage)
	m.systemDiskUsagePercent.Set(diskUsage)
}

func (m *metricsManager) RecordSystemEvent(eventType string, details map[string]string) {
	m.systemEventsTotal.WithLabelValues(eventType).Inc()
}

// Bucket Metrics Implementation

func (m *metricsManager) UpdateBucketMetrics(bucket string, objects, bytes int64) {
	m.bucketObjectsTotal.WithLabelValues(bucket).Set(float64(objects))
	m.bucketBytesTotal.WithLabelValues(bucket).Set(float64(bytes))
}

func (m *metricsManager) RecordBucketOperation(operation, bucket string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.bucketOpsTotal.WithLabelValues(operation, bucket, status).Inc()
}

// Object Lock Metrics Implementation

func (m *metricsManager) RecordObjectLockOperation(operation, bucket string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.objectLockOpsTotal.WithLabelValues(operation, bucket, status).Inc()
}

func (m *metricsManager) UpdateRetentionMetrics(bucket string, governanceObjects, complianceObjects int64) {
	m.retentionObjectsTotal.WithLabelValues(bucket, "governance").Set(float64(governanceObjects))
	m.retentionObjectsTotal.WithLabelValues(bucket, "compliance").Set(float64(complianceObjects))
}

// Performance Metrics Implementation

func (m *metricsManager) RecordBackgroundTask(taskType string, duration time.Duration, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.backgroundTasksTotal.WithLabelValues(taskType, status).Inc()
	m.backgroundTaskDuration.WithLabelValues(taskType).Observe(duration.Seconds())
}

func (m *metricsManager) UpdateCacheMetrics(hitRate float64, size int64) {
	m.cacheHitRate.Set(hitRate)
	m.cacheSizeBytes.Set(float64(size))
}

// UpdatePerformanceMetrics syncs PerformanceCollector data to Prometheus metrics
func (m *metricsManager) UpdatePerformanceMetrics() {
	collector := GetGlobalPerformanceCollector()
	if collector == nil {
		return
	}

	// Update operation latency metrics
	allStats := collector.GetAllLatencyStats()
	for operation, stats := range allStats {
		opLabel := string(operation)
		m.operationLatencyP50.WithLabelValues(opLabel).Set(stats.P50)
		m.operationLatencyP95.WithLabelValues(opLabel).Set(stats.P95)
		m.operationLatencyP99.WithLabelValues(opLabel).Set(stats.P99)
		m.operationLatencyMean.WithLabelValues(opLabel).Set(stats.Mean)
		m.operationSuccessRate.WithLabelValues(opLabel).Set(stats.SuccessRate)
		m.operationCount.WithLabelValues(opLabel).Set(float64(stats.Count))
	}

	// Update throughput metrics
	throughput := collector.GetCurrentThroughput()
	m.throughputRequestsPerSecond.Set(throughput.RequestsPerSecond)
	m.throughputBytesPerSecond.Set(float64(throughput.BytesPerSecond))
	m.throughputObjectsPerSecond.Set(throughput.ObjectsPerSecond)
}

// Export and Health Implementation

func (m *metricsManager) GetMetricsHandler() http.Handler {
	// Wrap the Prometheus handler to update performance metrics before each scrape
	handler := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Update performance metrics from PerformanceCollector before scraping
		m.UpdatePerformanceMetrics()
		// Serve the metrics
		handler.ServeHTTP(w, r)
	})
}

func (m *metricsManager) GetMetricsSnapshot() (map[string]interface{}, error) {
	// Collect real system metrics
	if m.systemMetrics == nil {
		return map[string]interface{}{
			"timestamp": time.Now().Unix(),
			"namespace": m.config.Namespace,
			"status":    "healthy",
		}, nil
	}

	snapshot := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"namespace": m.config.Namespace,
	}

	// Get CPU usage
	if cpuUsage, err := m.systemMetrics.GetCPUUsage(); err == nil {
		snapshot["cpuUsagePercent"] = cpuUsage
	} else {
		snapshot["cpuUsagePercent"] = 0.0
	}

	// Get memory usage
	if memStats, err := m.systemMetrics.GetMemoryUsage(); err == nil {
		snapshot["memoryUsagePercent"] = memStats.UsedPercent
		snapshot["memoryUsedBytes"] = memStats.UsedBytes
		snapshot["memoryTotalBytes"] = memStats.TotalBytes
	} else {
		snapshot["memoryUsagePercent"] = 0.0
		snapshot["memoryUsedBytes"] = uint64(0)
		snapshot["memoryTotalBytes"] = uint64(0)
	}

	// Get disk usage
	if diskStats, err := m.systemMetrics.GetDiskUsage(); err == nil {
		snapshot["diskUsagePercent"] = diskStats.UsedPercent
		snapshot["diskUsedBytes"] = diskStats.UsedBytes
		snapshot["diskTotalBytes"] = diskStats.TotalBytes
	} else {
		snapshot["diskUsagePercent"] = 0.0
		snapshot["diskUsedBytes"] = uint64(0)
		snapshot["diskTotalBytes"] = uint64(0)
	}

	// Get performance stats
	perfStats := m.systemMetrics.GetPerformanceStats()
	snapshot["goroutines"] = perfStats.GoRoutines
	snapshot["heapAllocBytes"] = uint64(perfStats.HeapAllocMB * 1024 * 1024)
	snapshot["gcRuns"] = perfStats.GCRuns
	snapshot["uptime"] = perfStats.Uptime

	return snapshot, nil
}

func (m *metricsManager) GetS3MetricsSnapshot() (map[string]interface{}, error) {
	totalReqs := atomic.LoadUint64(&m.totalRequests)
	totalErrs := atomic.LoadUint64(&m.totalErrors)
	totalLatency := atomic.LoadUint64(&m.totalLatencyMs)
	latencyCount := atomic.LoadUint64(&m.latencyCount)

	// Calculate average latency
	var avgLatency float64
	if latencyCount > 0 {
		avgLatency = float64(totalLatency) / float64(latencyCount)
	}

	// Calculate requests per second
	uptime := time.Since(m.serverStartTime).Seconds()
	var requestsPerSec float64
	if uptime > 0 {
		requestsPerSec = float64(totalReqs) / uptime
	}

	snapshot := map[string]interface{}{
		"totalRequests":  totalReqs,
		"totalErrors":    totalErrs,
		"avgLatency":     avgLatency,
		"requestsPerSec": requestsPerSec,
		"timestamp":      time.Now().Unix(),
	}
	return snapshot, nil
}

// GetStorageMetricsSnapshot returns a snapshot of storage metrics
func (m *metricsManager) GetStorageMetricsSnapshot(totalBuckets, totalObjects, totalSize int64) (map[string]interface{}, error) {
	snapshot := map[string]interface{}{
		"timestamp":    time.Now().Unix(),
		"totalSize":    totalSize,
		"totalObjects": totalObjects,
		"totalBuckets": totalBuckets,
	}
	return snapshot, nil
}

func (m *metricsManager) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

func (m *metricsManager) Reset() error {
	// Reset would clear all metrics
	// For Prometheus, we'd need to recreate the metrics
	// For MVP, just return nil
	return nil
}

// HTTP Middleware Implementation

func (m *metricsManager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Create response writer wrapper to capture status code
			wrapped := &responseWriterWrapper{
				ResponseWriter: w,
				statusCode:     200,
			}

			// Process request
			next.ServeHTTP(wrapped, r)

			// Record metrics
			duration := time.Since(start)
			m.RecordHTTPRequest(r.Method, r.URL.Path, fmt.Sprintf("%d", wrapped.statusCode), duration)

			// Record request/response sizes if available
			if r.ContentLength > 0 {
				m.RecordHTTPRequestSize(r.Method, r.URL.Path, r.ContentLength)
			}
		})
	}
}

// Lifecycle Implementation

// restorePersistedCounters loads persisted counter values from the kvStore.
func (m *metricsManager) restorePersistedCounters() error {
	if m.historyStore == nil {
		return fmt.Errorf("history store not available")
	}

	badgerHistory, ok := m.historyStore.(*BadgerHistoryStore)
	if !ok {
		return fmt.Errorf("history store is not a BadgerHistoryStore")
	}

	var persistedState struct {
		TotalRequests   uint64    `json:"total_requests"`
		TotalErrors     uint64    `json:"total_errors"`
		TotalLatencyMs  uint64    `json:"total_latency_ms"`
		LatencyCount    uint64    `json:"latency_count"`
		ServerStartTime time.Time `json:"server_start_time"`
	}

	data, err := badgerHistory.kvStore.GetRaw(context.Background(), "metrics:persisted_state")
	if err == metadata.ErrNotFound {
		logrus.Info("No persisted metrics counters found, starting fresh")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to restore persisted counters: %w", err)
	}

	if err := json.Unmarshal(data, &persistedState); err != nil {
		return fmt.Errorf("failed to unmarshal persisted state: %w", err)
	}

	atomic.StoreUint64(&m.totalRequests, persistedState.TotalRequests)
	atomic.StoreUint64(&m.totalErrors, persistedState.TotalErrors)
	atomic.StoreUint64(&m.totalLatencyMs, persistedState.TotalLatencyMs)
	atomic.StoreUint64(&m.latencyCount, persistedState.LatencyCount)
	m.serverStartTime = persistedState.ServerStartTime

	logrus.WithFields(logrus.Fields{
		"total_requests":    persistedState.TotalRequests,
		"total_errors":      persistedState.TotalErrors,
		"server_start_time": persistedState.ServerStartTime,
	}).Info("Restored persisted metrics counters")

	return nil
}

// persistCounters saves current counter values to the kvStore.
func (m *metricsManager) persistCounters() error {
	if m.historyStore == nil {
		return nil
	}

	badgerHistory, ok := m.historyStore.(*BadgerHistoryStore)
	if !ok {
		return nil
	}

	persistedState := struct {
		TotalRequests   uint64    `json:"total_requests"`
		TotalErrors     uint64    `json:"total_errors"`
		TotalLatencyMs  uint64    `json:"total_latency_ms"`
		LatencyCount    uint64    `json:"latency_count"`
		ServerStartTime time.Time `json:"server_start_time"`
	}{
		TotalRequests:   atomic.LoadUint64(&m.totalRequests),
		TotalErrors:     atomic.LoadUint64(&m.totalErrors),
		TotalLatencyMs:  atomic.LoadUint64(&m.totalLatencyMs),
		LatencyCount:    atomic.LoadUint64(&m.latencyCount),
		ServerStartTime: m.serverStartTime,
	}

	data, err := json.Marshal(persistedState)
	if err != nil {
		return fmt.Errorf("failed to marshal persisted state: %w", err)
	}

	if err := badgerHistory.kvStore.PutRaw(context.Background(), "metrics:persisted_state", data); err != nil {
		return fmt.Errorf("failed to persist counters: %w", err)
	}
	return nil
}

func (m *metricsManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("metrics manager already started")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true

	// Start metrics collection goroutine if history store is available
	if m.historyStore != nil {
		logrus.WithField("interval", m.config.Interval).Info("Starting metrics collection loops")
		go m.metricsCollectionLoop()
		go m.metricsMaintenanceLoop()
	} else {
		logrus.Warn("Metrics history store is nil, collection will not start")
	}

	return nil
}

func (m *metricsManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return fmt.Errorf("metrics manager not started")
	}

	// Persist counters before stopping
	if err := m.persistCounters(); err != nil {
		logrus.WithError(err).Warn("Failed to persist counters on stop")
	}

	// Cancel context to stop background goroutines
	if m.cancel != nil {
		m.cancel()
	}

	// Close history store
	if m.historyStore != nil {
		m.historyStore.Close()
	}

	m.started = false
	return nil
}

// metricsCollectionLoop periodically collects and stores metrics snapshots
func (m *metricsManager) metricsCollectionLoop() {
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.collectAndStoreMetrics()
		}
	}
}

// metricsMaintenanceLoop performs periodic maintenance on metrics history
func (m *metricsManager) metricsMaintenanceLoop() {
	// Run maintenance every hour
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Also persist counters every 5 minutes
	persistTicker := time.NewTicker(5 * time.Minute)
	defer persistTicker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-persistTicker.C:
			// Persist counters periodically
			if err := m.persistCounters(); err != nil {
				logrus.WithError(err).Debug("Failed to persist counters")
			}
		case <-ticker.C:
			// Aggregate old metrics
			if err := m.historyStore.AggregateHourlyMetrics(); err != nil {
				logrus.WithError(err).Debug("Failed to aggregate metrics")
			}

			// Clean up old metrics
			if err := m.historyStore.CleanupOldMetrics(); err != nil {
				logrus.WithError(err).Debug("Failed to clean up old metrics")
			}
		}
	}
}

// collectAndStoreMetrics collects current metrics and stores them in history
func (m *metricsManager) collectAndStoreMetrics() {
	if m.historyStore == nil {
		logrus.Debug("historyStore is nil, skipping metrics collection")
		return
	}

	// Update Prometheus system metrics from system metrics tracker
	if m.systemMetrics != nil {
		if cpuUsage, err := m.systemMetrics.GetCPUUsage(); err == nil {
			m.systemCPUUsage.Set(cpuUsage)
		}
		if memStats, err := m.systemMetrics.GetMemoryUsage(); err == nil {
			m.systemMemoryUsage.Set(memStats.UsedPercent)
		}
		if diskStats, err := m.systemMetrics.GetDiskUsage(); err == nil {
			m.systemDiskUsagePercent.Set(diskStats.UsedPercent)
			m.systemDiskUsedBytes.Set(float64(diskStats.UsedBytes))
			m.systemDiskTotalBytes.Set(float64(diskStats.TotalBytes))
		}
	}

	// Collect system metrics
	if systemSnapshot, err := m.GetMetricsSnapshot(); err == nil {
		if err := m.historyStore.SaveSnapshot("system", systemSnapshot); err != nil {
			logrus.WithError(err).Debug("Failed to save system snapshot")
		} else {
			logrus.Debug("System snapshot saved successfully")
		}
	}

	// Collect S3 metrics
	if s3Snapshot, err := m.GetS3MetricsSnapshot(); err == nil {
		if err := m.historyStore.SaveSnapshot("s3", s3Snapshot); err != nil {
			logrus.WithError(err).Debug("Failed to save S3 snapshot")
		}
	}

	// Collect storage metrics
	if m.storageMetricsProvider != nil {
		totalBuckets, totalObjects, totalSize := m.storageMetricsProvider()
		if storageSnapshot, err := m.GetStorageMetricsSnapshot(totalBuckets, totalObjects, totalSize); err == nil {
			m.historyStore.SaveSnapshot("storage", storageSnapshot)
		}
	}
}

// GetHistoricalMetrics retrieves historical metrics for a given type and time range
func (m *metricsManager) GetHistoricalMetrics(metricType string, start, end time.Time) ([]MetricSnapshot, error) {
	if m.historyStore == nil {
		return nil, fmt.Errorf("metrics history not enabled")
	}

	return m.historyStore.GetSnapshotsIntelligent(metricType, start, end)
}

// GetHistoryStats returns statistics about the metrics history
func (m *metricsManager) GetHistoryStats() (map[string]interface{}, error) {
	if m.historyStore == nil {
		return nil, fmt.Errorf("metrics history not enabled")
	}

	return m.historyStore.GetStats()
}

// SetSystemMetrics sets the system metrics tracker
func (m *metricsManager) SetSystemMetrics(tracker *SystemMetricsTracker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.systemMetrics = tracker
}

// SetStorageMetricsProvider sets a function that provides storage metrics
func (m *metricsManager) SetStorageMetricsProvider(provider StorageMetricsProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storageMetricsProvider = provider
}

// responseWriterWrapper wraps http.ResponseWriter to capture status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// noopManager is a no-op implementation when metrics are disabled
type noopManager struct{}

func (n *noopManager) RecordHTTPRequest(method, path, status string, duration time.Duration) {}
func (n *noopManager) RecordHTTPRequestSize(method, path string, size int64)                 {}
func (n *noopManager) RecordHTTPResponseSize(method, path string, size int64)                {}
func (n *noopManager) RecordS3Operation(operation, bucket string, success bool, duration time.Duration) {
}
func (n *noopManager) RecordS3Error(operation, bucket, errorType string) {}
func (n *noopManager) RecordStorageOperation(operation string, success bool, duration time.Duration) {
}
func (n *noopManager) UpdateStorageUsage(bucket string, objects, bytes int64) {}
func (n *noopManager) RecordObjectOperation(operation, bucket string, objectSize int64, duration time.Duration) {
}
func (n *noopManager) RecordAuthAttempt(method string, success bool)                    {}
func (n *noopManager) RecordAuthFailure(method, reason string)                          {}
func (n *noopManager) UpdateSystemMetrics(cpuUsage, memoryUsage, diskUsage float64)     {}
func (n *noopManager) RecordSystemEvent(eventType string, details map[string]string)    {}
func (n *noopManager) UpdateBucketMetrics(bucket string, objects, bytes int64)          {}
func (n *noopManager) RecordBucketOperation(operation, bucket string, success bool)     {}
func (n *noopManager) RecordObjectLockOperation(operation, bucket string, success bool) {}
func (n *noopManager) UpdateRetentionMetrics(bucket string, governanceObjects, complianceObjects int64) {
}
func (n *noopManager) RecordBackgroundTask(taskType string, duration time.Duration, success bool) {}
func (n *noopManager) UpdateCacheMetrics(hitRate float64, size int64)                             {}
func (n *noopManager) GetMetricsHandler() http.Handler                                            { return http.NotFoundHandler() }
func (n *noopManager) GetMetricsSnapshot() (map[string]interface{}, error) {
	return nil, fmt.Errorf("metrics disabled")
}
func (n *noopManager) GetS3MetricsSnapshot() (map[string]interface{}, error) {
	return map[string]interface{}{
		"totalRequests":  0,
		"totalErrors":    0,
		"avgLatency":     0.0,
		"requestsPerSec": 0.0,
		"timestamp":      time.Now().Unix(),
	}, nil
}
func (n *noopManager) IsHealthy() bool { return true }
func (n *noopManager) Reset() error    { return nil }
func (n *noopManager) GetHistoricalMetrics(metricType string, start, end time.Time) ([]MetricSnapshot, error) {
	return nil, fmt.Errorf("metrics disabled")
}
func (n *noopManager) GetHistoryStats() (map[string]interface{}, error) {
	return nil, fmt.Errorf("metrics disabled")
}
func (n *noopManager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
func (n *noopManager) Start(ctx context.Context) error { return nil }
func (n *noopManager) Stop() error                     { return nil }
