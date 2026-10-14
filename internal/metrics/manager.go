package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// Manager defines the interface for metrics management
type Manager interface {
	// Storage Metrics
	RecordStorageOperation(operation, status string, duration time.Duration)
	RecordObjectOperation(operation, bucket string, objectSize int64, duration time.Duration)

	// Authentication Metrics. reason is empty on success.
	RecordAuthAttempt(method string, success bool, reason string)

	// System Metrics
	RecordSystemEvent(eventType string)

	// Bucket Metrics
	RecordBucketOperation(operation, bucket string, success bool)

	// Object Lock Metrics
	RecordObjectLockOperation(operation, bucket string, success bool)
	UpdateRetentionMetrics(counts map[string]RetentionCount)

	// Performance Metrics
	RecordBackgroundTask(taskType string, duration time.Duration, success bool)

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
	Stop()
}

// Storage operation outcomes.
const (
	StorageSuccess  = "success"
	StorageNotFound = "not_found"
	StorageFailure  = "failure"
)

// BucketUsage is what this node holds of one bucket.
type BucketUsage struct {
	Name    string
	Objects int64
	Bytes   int64
}

// BucketUsageProvider returns this node's usage of every bucket.
type BucketUsageProvider func() ([]BucketUsage, error)

// CacheStatsProvider returns the metadata block cache's hits and misses since
// start, and its size in bytes.
type CacheStatsProvider func() (hits, misses, sizeBytes int64)

// RetentionCount is how many objects of a bucket each retention mode holds.
type RetentionCount struct {
	Governance int64
	Compliance int64
}

// metricsManager implements the Manager interface using Prometheus
type metricsManager struct {
	// Background collection loops, waited for by Stop
	loops sync.WaitGroup

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
	operationLatencyP50  *prometheus.GaugeVec
	operationLatencyP95  *prometheus.GaugeVec
	operationLatencyP99  *prometheus.GaugeVec
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

	// Read at every scrape; scrapeMu keeps two scrapes from interleaving the
	// reset and refill of the per-bucket gauges.
	bucketUsageProvider BucketUsageProvider
	cacheStatsProvider  CacheStatsProvider
	scrapeMu            sync.Mutex

	// Dynamic settings
	settingsManager interface {
		GetInt(key string) (int, error)
		GetBool(key string) (bool, error)
	}

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

// NewManagerWithStore creates a new metrics manager with metadata-store backing.
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

	// Initialize metadata-backed history store if metadata store is provided.
	logrus.WithFields(logrus.Fields{
		"metadataStore_nil":  metadataStore == nil,
		"metadataStore_type": fmt.Sprintf("%T", metadataStore),
		"dataDir":            dataDir,
	}).Info("Initializing metrics history store")

	if metadataStore != nil {
		// Use RawKV-backed history store through the metadata store.
		badgerHistory, err := NewBadgerHistoryStore(metadataStore, 365)
		if err != nil {
			logrus.WithError(err).Warn("Failed to initialize metadata-backed metrics history store")
		} else {
			manager.historyStore = badgerHistory
			logrus.Info("Metrics history store using metadata RawKV backend")

			// Restore persisted counters from the metadata store.
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
			logrus.Warn("Metrics history store using SQLite (deprecated, switch to metadata RawKV backend)")
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
			Help:      "S3 API requests, by S3 operation and HTTP status",
		},
		[]string{"method", "operation", "status"},
	)

	m.httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "S3 API request duration in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "operation"},
	)

	m.httpRequestSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_size_bytes",
			Help:      "S3 API request body size in bytes",
			Buckets:   prometheus.ExponentialBuckets(1024, 2, 10), // 1KB to 512MB
		},
		[]string{"method", "operation"},
	)

	m.httpResponseSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "response_size_bytes",
			Help:      "S3 API response body size in bytes",
			Buckets:   prometheus.ExponentialBuckets(1024, 2, 10), // 1KB to 512MB
		},
		[]string{"method", "operation"},
	)

	// S3 API Metrics
	m.s3OperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "s3",
			Name:      "operations_total",
			Help:      "S3 operations, by bucket and outcome (success below HTTP 400)",
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
		[]string{"operation"},
	)

	m.s3ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "s3",
			Name:      "errors_total",
			Help:      "S3 errors, by S3 error code",
		},
		[]string{"operation", "bucket", "error_type"},
	)

	// Storage Metrics
	m.storageOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "storage",
			Name:      "operations_total",
			Help:      "Storage backend operations, by outcome (success, not_found, failure)",
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

	// Object Metrics
	m.objectOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "object",
			Name:      "operations_total",
			Help:      "Object operations completed, from any entry point",
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
		[]string{"operation"},
	)

	m.objectSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "object",
			Name:      "size_bytes",
			Help:      "Size in bytes of the objects written and read",
			Buckets:   prometheus.ExponentialBuckets(1024, 2, 20), // 1KB to 512GB
		},
		[]string{"operation"},
	)

	// Authentication Metrics
	m.authAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "auth",
			Name:      "attempts_total",
			Help:      "Authentication attempts, by method and outcome",
		},
		[]string{"method", "status"},
	)

	m.authFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "auth",
			Name:      "failures_total",
			Help:      "Authentication failures, by method and reason",
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
			Help:      "System events sent to the console, by type",
		},
		[]string{"event_type"},
	)

	// Bucket Metrics
	m.bucketObjectsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "bucket",
			Name:      "objects_total",
			Help:      "Objects of each bucket this node holds",
		},
		[]string{"bucket"},
	)

	m.bucketBytesTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "bucket",
			Name:      "bytes_total",
			Help:      "Bytes of each bucket this node holds, versions included",
		},
		[]string{"bucket"},
	)

	m.bucketOpsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "bucket",
			Name:      "operations_total",
			Help:      "Bucket creations, deletions and configuration changes",
		},
		[]string{"operation", "bucket", "status"},
	)

	// Object Lock Metrics
	m.objectLockOpsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "object_lock",
			Name:      "operations_total",
			Help:      "Retention, legal hold and Object Lock configuration changes",
		},
		[]string{"operation", "bucket", "status"},
	)

	m.retentionObjectsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "object_lock",
			Name:      "retention_objects_total",
			Help:      "Objects whose current version a retention mode holds",
		},
		[]string{"bucket", "mode"},
	)

	// Performance Metrics
	m.backgroundTasksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "background",
			Name:      "tasks_total",
			Help:      "Runs of background tasks, by outcome",
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
			Help:      "Metadata block cache hit rate since start (0-1)",
		},
	)

	m.cacheSizeBytes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "size_bytes",
			Help:      "Metadata block cache size in bytes",
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

// recordS3Request records one request the S3 API answered.
func (m *metricsManager) recordS3Request(r *http.Request, rec *responseRecorder, duration time.Duration) {
	operation := OperationName(r)
	status := rec.status
	statusLabel := strconv.Itoa(status)
	m.httpRequestsTotal.WithLabelValues(r.Method, operation, statusLabel).Inc()
	m.httpRequestDuration.WithLabelValues(r.Method, operation).Observe(duration.Seconds())
	if r.ContentLength > 0 {
		m.httpRequestSize.WithLabelValues(r.Method, operation).Observe(float64(r.ContentLength))
	}
	m.httpResponseSize.WithLabelValues(r.Method, operation).Observe(float64(rec.bytes))

	code := ""
	if status >= 400 {
		code = rec.errorCode()
	}
	bucket := bucketLabel(r, status, code)
	outcome := "success"
	if status >= 400 {
		outcome = "failure"
		m.s3ErrorsTotal.WithLabelValues(operation, bucket, code).Inc()
		atomic.AddUint64(&m.totalErrors, 1)
	}
	m.s3OperationsTotal.WithLabelValues(operation, bucket, outcome).Inc()
	m.s3OperationDuration.WithLabelValues(operation).Observe(duration.Seconds())

	atomic.AddUint64(&m.totalRequests, 1)
	atomic.AddUint64(&m.totalLatencyMs, uint64(duration.Milliseconds()))
	atomic.AddUint64(&m.latencyCount, 1)
}

// Storage Metrics Implementation

func (m *metricsManager) RecordStorageOperation(operation, status string, duration time.Duration) {
	m.storageOperationsTotal.WithLabelValues(operation, status).Inc()
	m.storageOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

func (m *metricsManager) RecordObjectOperation(operation, bucket string, objectSize int64, duration time.Duration) {
	m.objectOperationsTotal.WithLabelValues(operation, bucket).Inc()
	m.objectOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
	if objectSize >= 0 {
		m.objectSizeBytes.WithLabelValues(operation).Observe(float64(objectSize))
	}
}

// Authentication Metrics Implementation

func (m *metricsManager) RecordAuthAttempt(method string, success bool, reason string) {
	if success {
		m.authAttemptsTotal.WithLabelValues(method, "success").Inc()
		return
	}
	m.authAttemptsTotal.WithLabelValues(method, "failure").Inc()
	m.authFailuresTotal.WithLabelValues(method, reason).Inc()
}

// System Metrics Implementation

func (m *metricsManager) RecordSystemEvent(eventType string) {
	m.systemEventsTotal.WithLabelValues(eventType).Inc()
}

// Bucket Metrics Implementation

func (m *metricsManager) RecordBucketOperation(operation, bucket string, success bool) {
	m.bucketOpsTotal.WithLabelValues(operation, bucket, outcomeLabel(success)).Inc()
}

// refreshBucketUsage replaces the per-bucket gauges with the provider's
// answer, so a deleted bucket leaves them. Requires scrapeMu.
func (m *metricsManager) refreshBucketUsage() {
	m.mu.RLock()
	provider := m.bucketUsageProvider
	m.mu.RUnlock()
	if provider == nil {
		return
	}
	usage, err := provider()
	if err != nil {
		logrus.WithError(err).Debug("Metrics: bucket usage not read; the gauges keep their last values")
		return
	}
	m.bucketObjectsTotal.Reset()
	m.bucketBytesTotal.Reset()
	for _, b := range usage {
		m.bucketObjectsTotal.WithLabelValues(b.Name).Set(float64(b.Objects))
		m.bucketBytesTotal.WithLabelValues(b.Name).Set(float64(b.Bytes))
	}
}

// Object Lock Metrics Implementation

func (m *metricsManager) RecordObjectLockOperation(operation, bucket string, success bool) {
	m.objectLockOpsTotal.WithLabelValues(operation, bucket, outcomeLabel(success)).Inc()
}

// UpdateRetentionMetrics replaces the retention gauges with counts, which
// name every bucket that holds retained objects.
func (m *metricsManager) UpdateRetentionMetrics(counts map[string]RetentionCount) {
	m.scrapeMu.Lock()
	defer m.scrapeMu.Unlock()
	m.retentionObjectsTotal.Reset()
	for bucket, c := range counts {
		m.retentionObjectsTotal.WithLabelValues(bucket, "governance").Set(float64(c.Governance))
		m.retentionObjectsTotal.WithLabelValues(bucket, "compliance").Set(float64(c.Compliance))
	}
}

// Performance Metrics Implementation

func (m *metricsManager) RecordBackgroundTask(taskType string, duration time.Duration, success bool) {
	m.backgroundTasksTotal.WithLabelValues(taskType, outcomeLabel(success)).Inc()
	m.backgroundTaskDuration.WithLabelValues(taskType).Observe(duration.Seconds())
}

// refreshCache sets the cache gauges from the provider. Requires scrapeMu.
func (m *metricsManager) refreshCache() {
	m.mu.RLock()
	provider := m.cacheStatsProvider
	m.mu.RUnlock()
	if provider == nil {
		return
	}
	hits, misses, size := provider()
	if hits+misses > 0 {
		m.cacheHitRate.Set(float64(hits) / float64(hits+misses))
	}
	m.cacheSizeBytes.Set(float64(size))
}

func outcomeLabel(success bool) string {
	if success {
		return "success"
	}
	return "failure"
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
		m.scrapeMu.Lock()
		defer m.scrapeMu.Unlock()
		m.UpdatePerformanceMetrics()
		m.refreshBucketUsage()
		m.refreshCache()
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
	return nil
}

// HTTP Middleware Implementation

// Middleware records every S3 API request. It goes before authentication, so
// refused requests are counted too.
func (m *metricsManager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			m.recordS3Request(r, rec, time.Since(start))
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

// SetSettingsManager wires the settings manager for dynamic configuration (hot-reload).
func (m *metricsManager) SetSettingsManager(sm interface {
	GetInt(key string) (int, error)
	GetBool(key string) (bool, error)
}) {
	m.mu.Lock()
	m.settingsManager = sm
	m.mu.Unlock()
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
		m.loops.Add(2)
		go func() { defer m.loops.Done(); m.metricsCollectionLoop() }()
		go func() { defer m.loops.Done(); m.metricsMaintenanceLoop() }()
	} else {
		logrus.Warn("Metrics history store is nil, collection will not start")
	}

	return nil
}

// Stop is safe to call more than once and waits for the collection loops before
// closing the history store: they write to it, and it is backed by the metadata
// store that shutdown is about to close.
func (m *metricsManager) Stop() {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	m.started = false

	// Persist counters before stopping
	if err := m.persistCounters(); err != nil {
		logrus.WithError(err).Warn("Failed to persist counters on stop")
	}

	// Cancel context to stop background goroutines
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()

	m.loops.Wait()

	if m.historyStore != nil {
		m.historyStore.Close()
	}
}

// metricsCollectionLoop periodically collects and stores metrics snapshots.
// The collection interval is re-read from settings on every tick (hot-reload).
func (m *metricsManager) metricsCollectionLoop() {
	// Take an immediate snapshot on startup so the chart has no gap at the
	// beginning, then take a final snapshot on shutdown to close the gap.
	m.collectAndStoreMetrics()
	defer m.collectAndStoreMetrics()

	currentInterval := m.config.Interval
	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			// Hot-reload: re-read the interval from settings
			m.mu.RLock()
			sm := m.settingsManager
			m.mu.RUnlock()
			if sm != nil {
				if secs, err := sm.GetInt("metrics.collection_interval"); err == nil && secs > 0 {
					newInterval := time.Duration(secs) * time.Second
					if newInterval != currentInterval {
						ticker.Reset(newInterval)
						currentInterval = newInterval
					}
				}
			}
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
	m.mu.RLock()
	provider := m.bucketUsageProvider
	m.mu.RUnlock()
	if provider != nil {
		if usage, err := provider(); err == nil {
			var totalObjects, totalSize int64
			for _, b := range usage {
				totalObjects += b.Objects
				totalSize += b.Bytes
			}
			if storageSnapshot, err := m.GetStorageMetricsSnapshot(int64(len(usage)), totalObjects, totalSize); err == nil {
				m.historyStore.SaveSnapshot("storage", storageSnapshot)
			}
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

// SetBucketUsageProvider sets where the per-bucket gauges and the storage
// history read this node's usage.
func (m *metricsManager) SetBucketUsageProvider(provider BucketUsageProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bucketUsageProvider = provider
}

// SetCacheStatsProvider sets where the cache gauges are read.
func (m *metricsManager) SetCacheStatsProvider(provider CacheStatsProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cacheStatsProvider = provider
}

// errorBodyLimit is how much of an error response is kept to read its S3
// error code. S3 error bodies are small.
const errorBodyLimit = 2048

// responseRecorder keeps a response's status, its size, and the start of an
// error body.
type responseRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	bytes       int64
	errorBody   []byte
}

func (w *responseRecorder) WriteHeader(statusCode int) {
	if !w.wroteHeader {
		w.status = statusCode
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseRecorder) Write(b []byte) (int, error) {
	w.wroteHeader = true
	if w.status >= 400 && len(w.errorBody) < errorBodyLimit {
		keep := errorBodyLimit - len(w.errorBody)
		if keep > len(b) {
			keep = len(b)
		}
		w.errorBody = append(w.errorBody, b[:keep]...)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Flush implements http.Flusher so that streaming responses (e.g. CompleteMultipartUpload
// keepalive whitespace) reach the client through this wrapper.
func (w *responseRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

var s3ErrorCode = regexp.MustCompile(`<Code>([A-Za-z0-9.]{1,64})</Code>`)

// errorCode is the S3 error code of the response, or its HTTP status when the
// body names none (a HEAD answer has no body).
func (w *responseRecorder) errorCode() string {
	if match := s3ErrorCode.FindSubmatch(w.errorBody); match != nil {
		return string(match[1])
	}
	return strconv.Itoa(w.status)
}

// OperationName is the name of the S3 route a request matched, or "Unknown".
func OperationName(r *http.Request) string {
	if route := mux.CurrentRoute(r); route != nil {
		if name := route.GetName(); name != "" {
			return name
		}
	}
	return "Unknown"
}

// unprovenBucket are the error codes of a request whose caller does not show
// the bucket it names exists.
var unprovenBucket = map[string]bool{
	"NoSuchBucket":          true,
	"InvalidBucketName":     true,
	"AccessDenied":          true,
	"AllAccessDisabled":     true,
	"InvalidAccessKeyId":    true,
	"SignatureDoesNotMatch": true,
	"RequestTimeTooSkewed":  true,
	"ExpiredToken":          true,
	"InvalidToken":          true,
}

// bucketLabel is the bucket a request names, or "" when a refused request
// names it: anyone can name any bucket, and each name would be a new series.
func bucketLabel(r *http.Request, status int, code string) string {
	bucket := mux.Vars(r)["bucket"]
	if bucket == "" || !utf8.ValidString(bucket) {
		return ""
	}
	if status < 400 {
		return bucket
	}
	if unprovenBucket[code] {
		return ""
	}
	query := r.URL.Query()
	if r.Header.Get("Authorization") == "" && query.Get("X-Amz-Algorithm") == "" && query.Get("AWSAccessKeyId") == "" {
		return ""
	}
	return bucket
}

// noopManager is a no-op implementation when metrics are disabled
type noopManager struct{}

func (n *noopManager) RecordStorageOperation(operation, status string, duration time.Duration) {}
func (n *noopManager) RecordObjectOperation(operation, bucket string, objectSize int64, duration time.Duration) {
}
func (n *noopManager) RecordAuthAttempt(method string, success bool, reason string)     {}
func (n *noopManager) RecordSystemEvent(eventType string)                               {}
func (n *noopManager) RecordBucketOperation(operation, bucket string, success bool)     {}
func (n *noopManager) RecordObjectLockOperation(operation, bucket string, success bool) {}
func (n *noopManager) UpdateRetentionMetrics(counts map[string]RetentionCount)          {}
func (n *noopManager) RecordBackgroundTask(taskType string, duration time.Duration, success bool) {
}
func (n *noopManager) GetMetricsHandler() http.Handler { return http.NotFoundHandler() }
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
func (n *noopManager) Stop()                           {}
