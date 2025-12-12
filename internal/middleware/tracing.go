package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/sirupsen/logrus"
)

// Context keys for tracing
type contextKey string

const (
	TraceIDKey     contextKey = "trace_id"
	StartTimeKey   contextKey = "start_time"
	OperationKey   contextKey = "operation"
)

// TracingMiddleware adds request tracing and automatic latency recording
func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate unique trace ID
		traceID := uuid.New().String()

		// Record start time
		startTime := time.Now()

		// Add trace context
		ctx := r.Context()
		ctx = context.WithValue(ctx, TraceIDKey, traceID)
		ctx = context.WithValue(ctx, StartTimeKey, startTime)

		// Try to determine operation type from route
		operation := determineOperation(r)
		ctx = context.WithValue(ctx, OperationKey, operation)

		// Wrap response writer to capture status code
		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Log request start
		logrus.WithFields(logrus.Fields{
			"trace_id": traceID,
			"method":   r.Method,
			"path":     r.URL.Path,
			"operation": operation,
		}).Debug("Request started")

		// Process request
		next.ServeHTTP(wrapped, r.WithContext(ctx))

		// Calculate duration
		duration := time.Since(startTime)

		// Determine success based on status code
		success := wrapped.statusCode >= 200 && wrapped.statusCode < 400

		// Record latency in performance collector
		if collector := metrics.GetGlobalPerformanceCollector(); collector != nil && operation != "" {
			collector.RecordLatency(metrics.OperationType(operation), duration, success)
		}

		// Log request completion
		logrus.WithFields(logrus.Fields{
			"trace_id":    traceID,
			"method":      r.Method,
			"path":        r.URL.Path,
			"operation":   operation,
			"duration_ms": duration.Milliseconds(),
			"status_code": wrapped.statusCode,
			"success":     success,
		}).Debug("Request completed")
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// determineOperation attempts to determine the operation type from the request
func determineOperation(r *http.Request) string {
	// Check if it's an S3 API request
	if isS3Request(r) {
		return mapS3Operation(r)
	}

	// Check if it's a Console API request
	if isConsoleRequest(r) {
		return mapConsoleOperation(r)
	}

	return ""
}

// isS3Request checks if the request is to the S3 API
func isS3Request(r *http.Request) bool {
	// S3 API typically uses the main port and has specific headers
	// or path patterns (e.g., /bucket/key)
	// This is a simplified check - adjust based on your routing

	// Check for AWS signature headers
	if r.Header.Get("Authorization") != "" &&
	   (r.Header.Get("x-amz-date") != "" || r.Header.Get("x-amz-content-sha256") != "") {
		return true
	}

	// Check path patterns - if not under /api/, likely S3
	if len(r.URL.Path) > 0 && r.URL.Path[0] == '/' {
		if len(r.URL.Path) < 5 || r.URL.Path[:5] != "/api/" {
			return true
		}
	}

	return false
}

// isConsoleRequest checks if the request is to the Console API
func isConsoleRequest(r *http.Request) bool {
	// Console API is under /api/
	return len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/"
}

// mapS3Operation maps S3 request to operation type
func mapS3Operation(r *http.Request) string {
	// Get route vars (bucket, object)
	// Note: The S3 handler uses "object" not "key" for the path variable
	vars := mux.Vars(r)
	bucket := vars["bucket"]
	object := vars["object"] // Changed from "key" to "object"

	// Determine operation based on method and path
	switch r.Method {
	case http.MethodPut:
		if object != "" {
			// Check for multipart upload
			if r.URL.Query().Get("uploadId") != "" {
				return string(metrics.OpMultipartUpload)
			}
			return string(metrics.OpPutObject)
		}
		return string(metrics.OpMetadataOperation) // Create bucket or set config

	case http.MethodGet:
		if object != "" {
			return string(metrics.OpGetObject)
		}
		if bucket != "" {
			return string(metrics.OpListObjects)
		}
		return string(metrics.OpMetadataOperation) // List buckets

	case http.MethodHead:
		return string(metrics.OpHeadObject)

	case http.MethodDelete:
		if object != "" {
			return string(metrics.OpDeleteObject)
		}
		return string(metrics.OpMetadataOperation) // Delete bucket

	case http.MethodPost:
		// Copy object or complete multipart
		if r.Header.Get("x-amz-copy-source") != "" {
			return string(metrics.OpCopyObject)
		}
		if r.URL.Query().Get("uploadId") != "" {
			return string(metrics.OpMultipartUpload)
		}
		return string(metrics.OpMetadataOperation)
	}

	return string(metrics.OpMetadataOperation)
}

// mapConsoleOperation maps Console API request to operation type
func mapConsoleOperation(r *http.Request) string {
	// Most console operations are metadata operations
	// Could be more granular if needed
	return string(metrics.OpMetadataOperation)
}

// GetTraceID extracts trace ID from context
func GetTraceID(ctx context.Context) string {
	if traceID, ok := ctx.Value(TraceIDKey).(string); ok {
		return traceID
	}
	return ""
}

// GetStartTime extracts start time from context
func GetStartTime(ctx context.Context) time.Time {
	if startTime, ok := ctx.Value(StartTimeKey).(time.Time); ok {
		return startTime
	}
	return time.Time{}
}

// GetOperation extracts operation from context
func GetOperation(ctx context.Context) string {
	if operation, ok := ctx.Value(OperationKey).(string); ok {
		return operation
	}
	return ""
}

// RecordCustomLatency allows manual recording of latencies for operations
// that need custom timing (e.g., database queries, filesystem operations)
func RecordCustomLatency(operation metrics.OperationType, duration time.Duration, success bool) {
	if collector := metrics.GetGlobalPerformanceCollector(); collector != nil {
		collector.RecordLatency(operation, duration, success)
	}
}

// RecordDatabaseLatency records database operation latency
func RecordDatabaseLatency(duration time.Duration, success bool) {
	RecordCustomLatency(metrics.OpDatabaseQuery, duration, success)
}

// RecordFilesystemLatency records filesystem operation latency
func RecordFilesystemLatency(duration time.Duration, success bool) {
	RecordCustomLatency(metrics.OpFilesystemIO, duration, success)
}

// RecordClusterProxyLatency records cluster proxy operation latency
func RecordClusterProxyLatency(duration time.Duration, success bool) {
	RecordCustomLatency(metrics.OpClusterProxy, duration, success)
}
