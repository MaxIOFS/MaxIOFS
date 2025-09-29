package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
)

// Manager defines the interface for metrics collection and management
type Manager interface {
	// Metrics collection
	IncrementRequestCount(method, endpoint string, statusCode int)
	RecordRequestDuration(method, endpoint string, duration time.Duration)
	RecordStorageUsage(bucket string, size int64)
	RecordObjectCount(bucket string, count int64)
	IncrementErrorCount(errorType, component string)

	// Custom metrics
	RecordCustomMetric(name string, value float64, labels map[string]string)
	IncrementCustomCounter(name string, labels map[string]string)

	// Background collection
	Start(ctx context.Context)
	Stop()

	// HTTP handlers
	MetricsHandler() http.Handler
	Middleware() func(http.Handler) http.Handler

	// Health check
	IsReady() bool
}

// metricsManager implements the Manager interface
type metricsManager struct {
	config   config.MetricsConfig
	enabled  bool
	stopChan chan struct{}
}

// NewManager creates a new metrics manager
func NewManager(config config.MetricsConfig) Manager {
	manager := &metricsManager{
		config:   config,
		enabled:  config.Enable,
		stopChan: make(chan struct{}),
	}

	// TODO: Initialize Prometheus metrics in Fase 2.2 - Metrics System
	return manager
}

// IncrementRequestCount increments the request counter
func (mm *metricsManager) IncrementRequestCount(method, endpoint string, statusCode int) {
	// TODO: Implement in Fase 2.2 - Metrics System
	if !mm.enabled {
		return
	}
	// Prometheus counter increment
}

// RecordRequestDuration records request duration
func (mm *metricsManager) RecordRequestDuration(method, endpoint string, duration time.Duration) {
	// TODO: Implement in Fase 2.2 - Metrics System
	if !mm.enabled {
		return
	}
	// Prometheus histogram observation
}

// RecordStorageUsage records storage usage metrics
func (mm *metricsManager) RecordStorageUsage(bucket string, size int64) {
	// TODO: Implement in Fase 2.2 - Metrics System
	if !mm.enabled {
		return
	}
	// Prometheus gauge set
}

// RecordObjectCount records object count metrics
func (mm *metricsManager) RecordObjectCount(bucket string, count int64) {
	// TODO: Implement in Fase 2.2 - Metrics System
	if !mm.enabled {
		return
	}
	// Prometheus gauge set
}

// IncrementErrorCount increments error counter
func (mm *metricsManager) IncrementErrorCount(errorType, component string) {
	// TODO: Implement in Fase 2.2 - Metrics System
	if !mm.enabled {
		return
	}
	// Prometheus counter increment
}

// RecordCustomMetric records a custom metric value
func (mm *metricsManager) RecordCustomMetric(name string, value float64, labels map[string]string) {
	// TODO: Implement in Fase 2.2 - Metrics System
	if !mm.enabled {
		return
	}
	// Custom metric recording
}

// IncrementCustomCounter increments a custom counter
func (mm *metricsManager) IncrementCustomCounter(name string, labels map[string]string) {
	// TODO: Implement in Fase 2.2 - Metrics System
	if !mm.enabled {
		return
	}
	// Custom counter increment
}

// Start begins background metrics collection
func (mm *metricsManager) Start(ctx context.Context) {
	// TODO: Implement in Fase 2.2 - Metrics System
	if !mm.enabled {
		return
	}

	go func() {
		ticker := time.NewTicker(time.Duration(mm.config.Interval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-mm.stopChan:
				return
			case <-ticker.C:
				// Collect system metrics
				mm.collectSystemMetrics()
			}
		}
	}()
}

// Stop stops metrics collection
func (mm *metricsManager) Stop() {
	close(mm.stopChan)
}

// MetricsHandler returns the Prometheus metrics HTTP handler
func (mm *metricsManager) MetricsHandler() http.Handler {
	// TODO: Implement in Fase 2.2 - Metrics System
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mm.enabled {
			http.Error(w, "Metrics disabled", http.StatusNotFound)
			return
		}
		// Return Prometheus metrics
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# Metrics endpoint - not implemented yet\n"))
	})
}

// Middleware returns HTTP middleware for request metrics
func (mm *metricsManager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !mm.enabled {
				next.ServeHTTP(w, r)
				return
			}

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
			mm.IncrementRequestCount(r.Method, r.URL.Path, wrapped.statusCode)
			mm.RecordRequestDuration(r.Method, r.URL.Path, duration)

			if wrapped.statusCode >= 400 {
				mm.IncrementErrorCount("http_error", "api")
			}
		})
	}
}

// IsReady checks if metrics manager is ready
func (mm *metricsManager) IsReady() bool {
	return true
}

// collectSystemMetrics collects system-level metrics
func (mm *metricsManager) collectSystemMetrics() {
	// TODO: Implement in Fase 2.2 - Metrics System
	// Collect CPU, memory, disk usage, etc.
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