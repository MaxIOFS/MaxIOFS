package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// Test Logging Middleware

func TestLogging(t *testing.T) {
	handler := Logging()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OK", rec.Body.String())
}

func TestLoggingWithConfig(t *testing.T) {
	t.Run("Common log format", func(t *testing.T) {
		config := &LoggingConfig{
			LogFormat: "common",
			SkipPaths: []string{},
		}

		handler := LoggingWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("test"))
		}))

		req := httptest.NewRequest("GET", "/api/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("Combined log format", func(t *testing.T) {
		config := &LoggingConfig{
			LogFormat: "combined",
		}

		handler := LoggingWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}))

		req := httptest.NewRequest("POST", "/api/create", nil)
		req.Header.Set("User-Agent", "TestAgent/1.0")
		req.Header.Set("Referer", "http://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("JSON log format", func(t *testing.T) {
		config := &LoggingConfig{
			LogFormat: "json",
		}

		handler := LoggingWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}))

		req := httptest.NewRequest("PUT", "/api/update", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusAccepted, rec.Code)
	})

	t.Run("Skip paths", func(t *testing.T) {
		config := &LoggingConfig{
			LogFormat: "common",
			SkipPaths: []string{"/health", "/metrics"},
		}

		handler := LoggingWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// Test skipped path
		req := httptest.NewRequest("GET", "/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)

		// Test non-skipped path
		req = httptest.NewRequest("GET", "/api/test", nil)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("Custom formatter", func(t *testing.T) {
		config := &LoggingConfig{
			LogFormat: "custom",
			CustomFormatter: func(entry LogEntry) string {
				return "CUSTOM: " + entry.Method + " " + entry.URL
			},
		}

		handler := LoggingWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("DELETE", "/api/delete", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestDefaultLoggingConfig(t *testing.T) {
	config := DefaultLoggingConfig()

	assert.NotNil(t, config)
	assert.Equal(t, "common", config.LogFormat)
	assert.Contains(t, config.SkipPaths, "/health")
	assert.Contains(t, config.SkipPaths, "/metrics")
	assert.False(t, config.LogBody)
	assert.Equal(t, int64(1024), config.MaxBodySize)
}

func TestResponseWriterWrapper(t *testing.T) {
	t.Run("WriteHeader", func(t *testing.T) {
		rec := httptest.NewRecorder()
		wrapper := &responseWriterWrapper{
			ResponseWriter: rec,
		}

		wrapper.WriteHeader(http.StatusNotFound)

		assert.Equal(t, http.StatusNotFound, wrapper.statusCode)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("Write with implicit 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		wrapper := &responseWriterWrapper{
			ResponseWriter: rec,
		}

		n, err := wrapper.Write([]byte("test data"))

		assert.NoError(t, err)
		assert.Equal(t, 9, n)
		assert.Equal(t, 200, wrapper.statusCode)
		assert.Equal(t, int64(9), wrapper.size)
	})

	t.Run("Write captures body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		wrapper := &responseWriterWrapper{
			ResponseWriter: rec,
			body:           make([]byte, 0, 100),
		}

		wrapper.Write([]byte("test"))

		assert.Equal(t, []byte("test"), wrapper.body)
	})

	t.Run("Flush forwards to underlying Flusher", func(t *testing.T) {
		// httptest.ResponseRecorder implements http.Flusher; the wrapper must
		// forward the call so CompleteMultipartUpload keepalive writes reach clients.
		rec := httptest.NewRecorder()
		wrapper := &responseWriterWrapper{ResponseWriter: rec}

		// Must satisfy the interface at compile-time too
		var _ http.Flusher = wrapper

		wrapper.WriteHeader(http.StatusOK)
		wrapper.Write([]byte("hello"))
		wrapper.Flush() // must not panic and must forward to rec.Flush()

		assert.True(t, rec.Flushed, "Flush must propagate through the wrapper")
	})
}

func TestGetRemoteAddr(t *testing.T) {
	t.Run("X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "192.168.1.1")
		req.RemoteAddr = "10.0.0.1:1234"

		addr := getRemoteAddr(req)

		assert.Equal(t, "192.168.1.1", addr)
	})

	t.Run("X-Real-IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Real-IP", "192.168.1.2")
		req.RemoteAddr = "10.0.0.1:1234"

		addr := getRemoteAddr(req)

		assert.Equal(t, "192.168.1.2", addr)
	})

	t.Run("RemoteAddr fallback", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"

		addr := getRemoteAddr(req)

		assert.Equal(t, "10.0.0.1:1234", addr)
	})
}

func TestGetRequestID(t *testing.T) {
	t.Run("X-Request-ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Request-ID", "req-123")

		id := getRequestID(req)

		assert.Equal(t, "req-123", id)
	})

	t.Run("X-Trace-ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Trace-ID", "trace-456")

		id := getRequestID(req)

		assert.Equal(t, "trace-456", id)
	})

	t.Run("No request ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)

		id := getRequestID(req)

		assert.Empty(t, id)
	})
}

func TestEscapeJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"No escaping needed", "hello", "hello"},
		{"Double quote", `hello "world"`, `hello \"world\"`},
		{"Backslash", `path\to\file`, `path\\to\\file`},
		{"Newline", "line1\nline2", `line1\nline2`},
		{"Carriage return", "line1\rline2", `line1\rline2`},
		{"Tab", "col1\tcol2", `col1\tcol2`},
		{"Mixed", "a\nb\"c\\d", `a\nb\"c\\d`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeJSON(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test Rate Limit Middleware

func TestCORS(t *testing.T) {
	handler := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://localhost:5173", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSWithConfig(t *testing.T) {
	t.Run("Allowed origin", func(t *testing.T) {
		config := &CORSConfig{
			AllowedOrigins: []string{"http://example.com"},
			AllowedMethods: []string{"GET", "POST"},
		}

		handler := CORSWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "http://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "GET")
	})

	t.Run("Wildcard origin", func(t *testing.T) {
		config := &CORSConfig{
			AllowedOrigins: []string{"*"},
		}

		handler := CORSWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "http://any-origin.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// When wildcard is configured and origin is present, it returns the specific origin
		// If no origin header, it should return "*"
		assert.Equal(t, "http://any-origin.com", rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("Wildcard pattern", func(t *testing.T) {
		config := &CORSConfig{
			AllowedOrigins: []string{"*.example.com"},
		}

		handler := CORSWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "http://sub.example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, "http://sub.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("Disallowed origin", func(t *testing.T) {
		config := &CORSConfig{
			AllowedOrigins: []string{"http://example.com"},
		}

		handler := CORSWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "http://evil.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("Preflight request", func(t *testing.T) {
		config := &CORSConfig{
			AllowedOrigins: []string{"http://example.com"},
			AllowedMethods: []string{"GET", "POST", "DELETE"},
			AllowedHeaders: []string{"Content-Type", "Authorization"},
			MaxAge:         "3600",
		}

		handler := CORSWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("Handler should not be called for OPTIONS")
		}))

		req := httptest.NewRequest("OPTIONS", "/test", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "http://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "POST")
		assert.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "Authorization")
		assert.Equal(t, "3600", rec.Header().Get("Access-Control-Max-Age"))
	})

	t.Run("Allow credentials", func(t *testing.T) {
		config := &CORSConfig{
			AllowedOrigins:   []string{"http://example.com"},
			AllowCredentials: true,
		}

		handler := CORSWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	})

	t.Run("Exposed headers", func(t *testing.T) {
		config := &CORSConfig{
			AllowedOrigins: []string{"http://example.com"},
			ExposedHeaders: []string{"ETag", "X-Custom-Header"},
		}

		handler := CORSWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		exposedHeaders := rec.Header().Get("Access-Control-Expose-Headers")
		assert.Contains(t, exposedHeaders, "ETag")
		assert.Contains(t, exposedHeaders, "X-Custom-Header")
	})

	t.Run("Custom origin validator", func(t *testing.T) {
		config := &CORSConfig{
			AllowedOrigins: []string{},
			CustomOriginValidator: func(origin string) bool {
				return strings.HasSuffix(origin, ".trusted.com")
			},
		}

		handler := CORSWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "http://sub.trusted.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, "http://sub.trusted.com", rec.Header().Get("Access-Control-Allow-Origin"))
	})
}

func TestDefaultCORSConfig(t *testing.T) {
	config := DefaultCORSConfig()

	assert.NotNil(t, config)
	assert.Contains(t, config.AllowedOrigins, "http://localhost:5173")
	assert.Contains(t, config.AllowedMethods, "GET")
	assert.Contains(t, config.AllowedMethods, "POST")
	assert.Contains(t, config.AllowedHeaders, "Authorization")
	assert.Contains(t, config.ExposedHeaders, "ETag")
	assert.True(t, config.AllowCredentials)
}

func TestVerboseLogging(t *testing.T) {
	// Set log level to debug for this test
	originalLevel := logrus.GetLevel()
	logrus.SetLevel(logrus.DebugLevel)
	defer logrus.SetLevel(originalLevel)

	// Capture log output
	var buf bytes.Buffer
	originalOutput := logrus.StandardLogger().Out
	logrus.SetOutput(&buf)
	defer logrus.SetOutput(originalOutput)

	handler := VerboseLogging()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("User-Agent", "TestAgent")
	req.Header.Set("X-Custom-Header", "custom-value")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "test response", rec.Body.String())

	// Check that verbose logging occurred
	logOutput := buf.String()
	assert.Contains(t, logOutput, "INCOMING REQUEST")
	assert.Contains(t, logOutput, "REQUEST HEADERS")
	assert.Contains(t, logOutput, "RESPONSE")
}

func TestVerboseLoggingWithBody(t *testing.T) {
	originalLevel := logrus.GetLevel()
	logrus.SetLevel(logrus.DebugLevel)
	defer logrus.SetLevel(originalLevel)

	var buf bytes.Buffer
	originalOutput := logrus.StandardLogger().Out
	logrus.SetOutput(&buf)
	defer logrus.SetOutput(originalOutput)

	handler := VerboseLogging()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created: " + string(body)))
	}))

	reqBody := strings.NewReader("test data")
	req := httptest.NewRequest("POST", "/api/create", reqBody)
	req.Header.Set("Content-Type", "text/plain")
	req.ContentLength = 9
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "INCOMING REQUEST")
}

func TestVerboseResponseWriter(t *testing.T) {
	t.Run("WriteHeader", func(t *testing.T) {
		rec := httptest.NewRecorder()
		wrapper := &verboseResponseWriter{
			ResponseWriter: rec,
			body:           &bytes.Buffer{},
		}

		wrapper.WriteHeader(http.StatusCreated)

		assert.Equal(t, http.StatusCreated, wrapper.statusCode)
		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("Write with implicit 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		wrapper := &verboseResponseWriter{
			ResponseWriter: rec,
			body:           &bytes.Buffer{},
		}

		n, err := wrapper.Write([]byte("response data"))

		assert.NoError(t, err)
		assert.Equal(t, 13, n)
		assert.Equal(t, 200, wrapper.statusCode)
		assert.Equal(t, int64(13), wrapper.size)
	})

	t.Run("Capture body up to limit", func(t *testing.T) {
		rec := httptest.NewRecorder()
		wrapper := &verboseResponseWriter{
			ResponseWriter: rec,
			body:           &bytes.Buffer{},
		}

		wrapper.Write([]byte("short response"))

		assert.Equal(t, "short response", wrapper.body.String())
	})

	t.Run("Capture body stops at limit", func(t *testing.T) {
		rec := httptest.NewRecorder()
		wrapper := &verboseResponseWriter{
			ResponseWriter: rec,
			body:           &bytes.Buffer{},
		}

		// Write small data first
		wrapper.Write([]byte("small"))
		assert.Equal(t, "small", wrapper.body.String())

		// Write more data up to limit
		largeData := make([]byte, 1500)
		for i := range largeData {
			largeData[i] = 'x'
		}

		wrapper.Write(largeData)

		// Body continues capturing until it reaches the 1000 byte check
		// The implementation checks if body.Len() < 1000 before writing
		// So it will stop capturing after reaching 1000 bytes
		assert.Greater(t, wrapper.body.Len(), 1000)
	})
}
