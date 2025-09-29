package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/maxiofs/maxiofs/internal/config"
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
	UpdateSystemMetrics(cpuUsage, memoryUsage float64)
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
	IsHealthy() bool
	Reset() error

	// HTTP Middleware
	Middleware() func(http.Handler) http.Handler

	// Lifecycle
	Start(ctx context.Context) error
	Stop() error
}

// metricsManager implements the Manager interface using Prometheus
type metricsManager struct {
	// Configuration
	config MetricsConfig

	// Prometheus registry and metrics
	registry *prometheus.Registry

	// HTTP Metrics
	httpRequestsTotal    *prometheus.CounterVec
	httpRequestDuration  *prometheus.HistogramVec
	httpRequestSize      *prometheus.HistogramVec
	httpResponseSize     *prometheus.HistogramVec

	// S3 API Metrics
	s3OperationsTotal    *prometheus.CounterVec
	s3OperationDuration  *prometheus.HistogramVec
	s3ErrorsTotal        *prometheus.CounterVec

	// Storage Metrics
	storageOperationsTotal    *prometheus.CounterVec
	storageOperationDuration  *prometheus.HistogramVec
	storageBytesTotal         *prometheus.GaugeVec
	storageObjectsTotal       *prometheus.GaugeVec

	// Object Metrics
	objectOperationsTotal    *prometheus.CounterVec
	objectOperationDuration  *prometheus.HistogramVec
	objectSizeBytes          *prometheus.HistogramVec

	// Authentication Metrics
	authAttemptsTotal  *prometheus.CounterVec
	authFailuresTotal  *prometheus.CounterVec

	// System Metrics
	systemCPUUsage     prometheus.Gauge
	systemMemoryUsage  prometheus.Gauge
	systemEventsTotal  *prometheus.CounterVec

	// Bucket Metrics
	bucketObjectsTotal *prometheus.GaugeVec
	bucketBytesTotal   *prometheus.GaugeVec
	bucketOpsTotal     *prometheus.CounterVec

	// Object Lock Metrics
	objectLockOpsTotal        *prometheus.CounterVec
	retentionObjectsTotal     *prometheus.GaugeVec

	// Performance Metrics
	backgroundTasksTotal      *prometheus.CounterVec
	backgroundTaskDuration    *prometheus.HistogramVec
	cacheHitRate             prometheus.Gauge
	cacheSizeBytes           prometheus.Gauge

	// Lifecycle
	started bool
	mu      sync.RWMutex
}

// MetricsConfig holds configuration for the metrics system
type MetricsConfig struct {
	Enabled    bool          `json:"enabled"`
	Path       string        `json:"path"`
	Namespace  string        `json:"namespace"`
	Subsystem  string        `json:"subsystem"`
	Interval   time.Duration `json:"interval"`
	Labels     map[string]string `json:"labels"`
}

// NewManager creates a new metrics manager
func NewManager(cfg config.MetricsConfig) Manager {
	// Convert config.MetricsConfig to our internal MetricsConfig
	metricsConfig := MetricsConfig{
		Enabled:   cfg.Enable,
		Path:      cfg.Path,
		Namespace: "maxiofs",  // Default namespace
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
		metricsConfig.Interval = 15 * time.Second
	}

	registry := prometheus.NewRegistry()

	manager := &metricsManager{
		config:   metricsConfig,
		registry: registry,
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
	}

	for _, metric := range metrics {
		m.registry.MustRegister(metric)
	}
}

// HTTP Metrics Implementation

func (m *metricsManager) RecordHTTPRequest(method, path, status string, duration time.Duration) {
	m.httpRequestsTotal.WithLabelValues(method, path, status).Inc()
	m.httpRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
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

func (m *metricsManager) UpdateSystemMetrics(cpuUsage, memoryUsage float64) {
	m.systemCPUUsage.Set(cpuUsage)
	m.systemMemoryUsage.Set(memoryUsage)
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

// Export and Health Implementation

func (m *metricsManager) GetMetricsHandler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *metricsManager) GetMetricsSnapshot() (map[string]interface{}, error) {
	// This would collect all current metric values
	// For MVP, return basic info
	snapshot := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"namespace": m.config.Namespace,
		"status":    "healthy",
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

func (m *metricsManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("metrics manager already started")
	}

	m.started = true
	return nil
}

func (m *metricsManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return fmt.Errorf("metrics manager not started")
	}

	m.started = false
	return nil
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
func (n *noopManager) RecordHTTPRequestSize(method, path string, size int64) {}
func (n *noopManager) RecordHTTPResponseSize(method, path string, size int64) {}
func (n *noopManager) RecordS3Operation(operation, bucket string, success bool, duration time.Duration) {}
func (n *noopManager) RecordS3Error(operation, bucket, errorType string) {}
func (n *noopManager) RecordStorageOperation(operation string, success bool, duration time.Duration) {}
func (n *noopManager) UpdateStorageUsage(bucket string, objects, bytes int64) {}
func (n *noopManager) RecordObjectOperation(operation, bucket string, objectSize int64, duration time.Duration) {}
func (n *noopManager) RecordAuthAttempt(method string, success bool) {}
func (n *noopManager) RecordAuthFailure(method, reason string) {}
func (n *noopManager) UpdateSystemMetrics(cpuUsage, memoryUsage float64) {}
func (n *noopManager) RecordSystemEvent(eventType string, details map[string]string) {}
func (n *noopManager) UpdateBucketMetrics(bucket string, objects, bytes int64) {}
func (n *noopManager) RecordBucketOperation(operation, bucket string, success bool) {}
func (n *noopManager) RecordObjectLockOperation(operation, bucket string, success bool) {}
func (n *noopManager) UpdateRetentionMetrics(bucket string, governanceObjects, complianceObjects int64) {}
func (n *noopManager) RecordBackgroundTask(taskType string, duration time.Duration, success bool) {}
func (n *noopManager) UpdateCacheMetrics(hitRate float64, size int64) {}
func (n *noopManager) GetMetricsHandler() http.Handler { return http.NotFoundHandler() }
func (n *noopManager) GetMetricsSnapshot() (map[string]interface{}, error) { return nil, fmt.Errorf("metrics disabled") }
func (n *noopManager) IsHealthy() bool { return true }
func (n *noopManager) Reset() error { return nil }
func (n *noopManager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
func (n *noopManager) Start(ctx context.Context) error { return nil }
func (n *noopManager) Stop() error { return nil }