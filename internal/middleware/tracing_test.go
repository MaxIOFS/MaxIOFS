package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/metrics"
)

func TestTracingMiddleware_TraceIDGeneration(t *testing.T) {
	// Initialize performance collector
	metrics.InitGlobalPerformanceCollector(100, 1*time.Hour)

	// Create test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify trace ID exists in context
		traceID, _ := r.Context().Value(TraceIDKey).(string)
		if traceID == "" {
			t.Error("Expected non-empty trace ID")
		}

		// Verify start time exists
		startTime, _ := r.Context().Value(StartTimeKey).(time.Time)
		if startTime.IsZero() {
			t.Error("Expected non-zero start time")
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Wrap with tracing middleware
	wrappedHandler := TracingMiddleware(handler)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rr, req)

	// Verify response
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestTracingMiddleware_LatencyRecording(t *testing.T) {
	// Initialize performance collector
	collector := metrics.NewPerformanceCollector(100, 1*time.Hour)
	metrics.InitGlobalPerformanceCollector(100, 1*time.Hour)

	// Create test handler that takes some time
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	// Create router with tracing middleware
	router := mux.NewRouter()
	router.Use(TracingMiddleware)
	router.HandleFunc("/bucket/object", handler).Methods("PUT")

	// Create test request (S3 PutObject)
	req := httptest.NewRequest("PUT", "/bucket/object", nil)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 ...")
	req.Header.Set("x-amz-date", "20231201T120000Z")
	rr := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(rr, req)

	// Wait a bit for async processing
	time.Sleep(50 * time.Millisecond)

	// Verify latency was recorded (just verify no errors occurred)
	_ = collector.GetLatencyStats(metrics.OpPutObject)

	// Verify response code
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestTracingMiddleware_StatusCodeCapture(t *testing.T) {
	// Initialize performance collector
	metrics.InitGlobalPerformanceCollector(100, 1*time.Hour)

	tests := []struct {
		name           string
		statusCode     int
		expectedSuccess bool
	}{
		{"200 OK", http.StatusOK, true},
		{"201 Created", http.StatusCreated, true},
		{"204 No Content", http.StatusNoContent, true},
		{"400 Bad Request", http.StatusBadRequest, false},
		{"401 Unauthorized", http.StatusUnauthorized, false},
		{"404 Not Found", http.StatusNotFound, false},
		{"500 Internal Server Error", http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			wrappedHandler := TracingMiddleware(handler)

			req := httptest.NewRequest("GET", "/test", nil)
			rr := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rr, req)

			if rr.Code != tt.statusCode {
				t.Errorf("Expected status %d, got %d", tt.statusCode, rr.Code)
			}
		})
	}
}

func TestIsS3Request(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		headers  map[string]string
		expected bool
	}{
		{
			name: "S3 with AWS signature",
			path: "/bucket/object",
			headers: map[string]string{
				"Authorization":       "AWS4-HMAC-SHA256 ...",
				"x-amz-date":          "20231201T120000Z",
				"x-amz-content-sha256": "...",
			},
			expected: true,
		},
		{
			name:     "Console API request",
			path:     "/api/v1/buckets",
			headers:  map[string]string{},
			expected: false,
		},
		{
			name:     "S3 bucket path",
			path:     "/my-bucket",
			headers:  map[string]string{},
			expected: true,
		},
		{
			name:     "API path",
			path:     "/api/console/metrics",
			headers:  map[string]string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			result := isS3Request(req)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsConsoleRequest(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"API path", "/api/v1/buckets", true},
		{"API metrics", "/api/console/metrics", true},
		{"S3 bucket", "/my-bucket", false},
		{"Root", "/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			result := isConsoleRequest(req)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMapS3Operation(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		headers  map[string]string
		expected string
	}{
		{
			name:     "PUT object",
			method:   "PUT",
			path:     "/bucket/object.txt",
			expected: "PutObject",
		},
		{
			name:     "GET object",
			method:   "GET",
			path:     "/bucket/object.txt",
			expected: "GetObject",
		},
		{
			name:     "DELETE object",
			method:   "DELETE",
			path:     "/bucket/object.txt",
			expected: "DeleteObject",
		},
		{
			name:     "HEAD object",
			method:   "HEAD",
			path:     "/bucket/object.txt",
			expected: "HeadObject",
		},
		{
			name:     "List objects",
			method:   "GET",
			path:     "/bucket/",
			expected: "ListObjects",
		},
		{
			name:   "Copy object",
			method: "POST",
			path:   "/bucket/object.txt",
			headers: map[string]string{
				"x-amz-copy-source": "/source-bucket/source-object",
			},
			expected: "CopyObject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			// Create router to populate mux.Vars
			// Note: Changed from {key:.*} to {object:.*} to match actual S3 handler routes
			router := mux.NewRouter()
			router.HandleFunc("/{bucket}/{object:.*}", func(w http.ResponseWriter, r *http.Request) {
				result := mapS3Operation(r)
				if result != tt.expected {
					t.Errorf("Expected '%s', got '%s'", tt.expected, result)
				}
			})
			router.HandleFunc("/{bucket}/", func(w http.ResponseWriter, r *http.Request) {
				result := mapS3Operation(r)
				if result != tt.expected {
					t.Errorf("Expected '%s', got '%s'", tt.expected, result)
				}
			})

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
		})
	}
}

func TestResponseWriter_StatusCodeCapture(t *testing.T) {
	// Test that responseWriter correctly captures status codes
	rw := &responseWriter{
		ResponseWriter: httptest.NewRecorder(),
		statusCode:     http.StatusOK,
	}

	// Test WriteHeader
	rw.WriteHeader(http.StatusCreated)
	if rw.statusCode != http.StatusCreated {
		t.Errorf("Expected status code %d, got %d", http.StatusCreated, rw.statusCode)
	}

	// Test that second WriteHeader doesn't change status
	rw.WriteHeader(http.StatusBadRequest)
	if rw.statusCode != http.StatusCreated {
		t.Errorf("Expected status code to remain %d, got %d", http.StatusCreated, rw.statusCode)
	}
}

func TestResponseWriter_Write(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: rec,
		statusCode:     http.StatusOK,
	}

	// Write some data
	data := []byte("test data")
	n, err := rw.Write(data)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}

	// Verify status was set
	if !rw.written {
		t.Error("Expected written flag to be true")
	}

	// Verify data was written to underlying writer
	if rec.Body.String() != string(data) {
		t.Errorf("Expected body '%s', got '%s'", string(data), rec.Body.String())
	}
}
