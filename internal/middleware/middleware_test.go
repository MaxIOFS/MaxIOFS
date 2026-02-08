package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestVerboseLoggingConfig(t *testing.T) {
	config := VerboseLoggingConfig()

	assert.NotNil(t, config)
	assert.Equal(t, "json", config.LogFormat)
	assert.Empty(t, config.SkipPaths)
	assert.True(t, config.LogBody)
	assert.Equal(t, int64(4096), config.MaxBodySize)
}

func TestS3LoggingMiddleware(t *testing.T) {
	handler := S3LoggingMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/bucket/object", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
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

func TestRateLimit(t *testing.T) {
	handler := RateLimit()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRateLimitWithConfig(t *testing.T) {
	t.Run("Allow requests within limit", func(t *testing.T) {
		store := NewInMemoryRateLimitStore()
		config := &RateLimitConfig{
			RequestsPerSecond: 10.0,
			WindowSize:        time.Second,
			KeyExtractor:      IPKeyExtractor,
			Store:             store,
			OnRateLimitExceeded: func(w http.ResponseWriter, r *http.Request, key string) {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			},
		}

		handler := RateLimitWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("X-RateLimit-Limit"), "10")
	})

	t.Run("Block requests exceeding limit", func(t *testing.T) {
		store := NewInMemoryRateLimitStore()
		config := &RateLimitConfig{
			RequestsPerSecond: 2.0,
			WindowSize:        time.Second,
			KeyExtractor:      IPKeyExtractor,
			Store:             store,
			OnRateLimitExceeded: func(w http.ResponseWriter, r *http.Request, key string) {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			},
		}

		handler := RateLimitWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.2:1234"

		// First 2 requests should succeed
		for i := 0; i < 2; i++ {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code, "Request %d should succeed", i+1)
		}

		// Third request should be rate limited
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		assert.Equal(t, "0", rec.Header().Get("X-RateLimit-Remaining"))
	})

	t.Run("Skip paths", func(t *testing.T) {
		store := NewInMemoryRateLimitStore()
		config := &RateLimitConfig{
			RequestsPerSecond: 1.0,
			WindowSize:        time.Second,
			KeyExtractor:      IPKeyExtractor,
			SkipPaths:         []string{"/health"},
			Store:             store,
		}

		handler := RateLimitWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// Multiple requests to skipped path should all succeed
		for i := 0; i < 5; i++ {
			req := httptest.NewRequest("GET", "/health", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code)
		}
	})

	t.Run("Custom rate limit exceeded handler", func(t *testing.T) {
		store := NewInMemoryRateLimitStore()
		config := &RateLimitConfig{
			RequestsPerSecond: 1.0,
			WindowSize:        time.Second,
			KeyExtractor:      IPKeyExtractor,
			Store:             store,
			OnRateLimitExceeded: func(w http.ResponseWriter, r *http.Request, key string) {
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte("Custom rate limit message"))
			},
		}

		handler := RateLimitWithConfig(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.3:1234"

		// First request succeeds
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)

		// Second request blocked with custom message
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		assert.Contains(t, rec.Body.String(), "Custom rate limit message")
	})
}

func TestTokenBucket(t *testing.T) {
	t.Run("Allow request with available token", func(t *testing.T) {
		bucket := &TokenBucket{
			tokens:     5.0,
			capacity:   5.0,
			refillRate: 1.0,
			lastRefill: time.Now(),
		}

		allowed := bucket.allow()

		assert.True(t, allowed)
		assert.Equal(t, 4.0, bucket.tokens)
	})

	t.Run("Block request without available token", func(t *testing.T) {
		bucket := &TokenBucket{
			tokens:     0.0,
			capacity:   5.0,
			refillRate: 1.0,
			lastRefill: time.Now(),
		}

		allowed := bucket.allow()

		assert.False(t, allowed)
		assert.InDelta(t, 0.0, bucket.tokens, 0.001) // Allow small tolerance for floating point precision
	})

	t.Run("Refill tokens over time", func(t *testing.T) {
		bucket := &TokenBucket{
			tokens:     0.0,
			capacity:   5.0,
			refillRate: 2.0, // 2 tokens per second
			lastRefill: time.Now().Add(-time.Second),
		}

		allowed := bucket.allow()

		assert.True(t, allowed)
		assert.InDelta(t, 1.0, bucket.tokens, 0.1) // Should have ~1 token left after consuming 1
	})

	t.Run("Cap tokens at capacity", func(t *testing.T) {
		bucket := &TokenBucket{
			tokens:     3.0,
			capacity:   5.0,
			refillRate: 10.0,
			lastRefill: time.Now().Add(-time.Second),
		}

		bucket.allow()

		// Should cap at capacity (5.0), not exceed it
		assert.LessOrEqual(t, bucket.tokens, 5.0)
	})
}

func TestInMemoryRateLimitStore(t *testing.T) {
	t.Run("Allow first request", func(t *testing.T) {
		store := NewInMemoryRateLimitStore()

		allowed, retryAfter, err := store.Allow("user1", 10.0, time.Second)

		assert.NoError(t, err)
		assert.True(t, allowed)
		assert.Equal(t, time.Duration(0), retryAfter)
	})

	t.Run("Reset key", func(t *testing.T) {
		store := NewInMemoryRateLimitStore()

		store.Allow("user2", 1.0, time.Second)
		store.Allow("user2", 1.0, time.Second) // Consume token

		err := store.Reset("user2")
		assert.NoError(t, err)

		// After reset, should allow again
		allowed, _, _ := store.Allow("user2", 1.0, time.Second)
		assert.True(t, allowed)
	})

	t.Run("Cleanup old entries", func(t *testing.T) {
		store := NewInMemoryRateLimitStore()

		// Create a bucket with old lastRefill
		store.buckets["old-user"] = &TokenBucket{
			tokens:     5.0,
			capacity:   5.0,
			refillRate: 1.0,
			lastRefill: time.Now().Add(-2 * time.Hour),
		}

		err := store.Cleanup()
		assert.NoError(t, err)

		// Old bucket should be removed
		store.mu.RLock()
		_, exists := store.buckets["old-user"]
		store.mu.RUnlock()
		assert.False(t, exists)
	})
}

func TestDefaultRateLimitConfig(t *testing.T) {
	config := DefaultRateLimitConfig()

	assert.NotNil(t, config)
	assert.Equal(t, 100.0, config.RequestsPerSecond)
	assert.Equal(t, 200, config.BurstSize)
	assert.Equal(t, time.Second, config.WindowSize)
	assert.NotNil(t, config.KeyExtractor)
	assert.NotNil(t, config.OnRateLimitExceeded)
	assert.Contains(t, config.SkipPaths, "/health")
	assert.NotNil(t, config.Store)
}

func TestStrictRateLimitConfig(t *testing.T) {
	config := StrictRateLimitConfig()

	assert.Equal(t, 10.0, config.RequestsPerSecond)
	assert.Equal(t, 20, config.BurstSize)
}

func TestGenerousRateLimitConfig(t *testing.T) {
	config := GenerousRateLimitConfig()

	assert.Equal(t, 1000.0, config.RequestsPerSecond)
	assert.Equal(t, 2000, config.BurstSize)
}

func TestIPKeyExtractor(t *testing.T) {
	t.Run("Private network proxy trusts X-Forwarded-For", func(t *testing.T) {
		// 10.0.0.1 is RFC 1918 — automatically trusted as proxy
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.50")
		req.RemoteAddr = "10.0.0.1:1234"

		key := IPKeyExtractor(req)

		assert.Equal(t, "203.0.113.50", key)
	})

	t.Run("Private network proxy trusts X-Real-IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Real-IP", "203.0.113.51")
		req.RemoteAddr = "192.168.1.1:8080"

		key := IPKeyExtractor(req)

		assert.Equal(t, "203.0.113.51", key)
	})

	t.Run("Loopback trusts proxy headers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.52")
		req.RemoteAddr = "127.0.0.1:1234"

		key := IPKeyExtractor(req)

		assert.Equal(t, "203.0.113.52", key)
	})

	t.Run("172.16.x.x trusts proxy headers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "8.8.4.4")
		req.RemoteAddr = "172.17.0.1:3000"

		key := IPKeyExtractor(req)

		// Docker default bridge network (172.17.x.x) is RFC 1918 — trusted
		assert.Equal(t, "8.8.4.4", key)
	})

	t.Run("XFF first IP only from private proxy", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.60, 10.0.0.2, 10.0.0.3")
		req.RemoteAddr = "10.0.0.1:1234"

		key := IPKeyExtractor(req)

		assert.Equal(t, "203.0.113.60", key)
	})

	t.Run("Prefers XFF over X-Real-IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "1.2.3.4")
		req.Header.Set("X-Real-IP", "5.6.7.8")
		req.RemoteAddr = "10.0.0.1:1234"

		key := IPKeyExtractor(req)

		assert.Equal(t, "1.2.3.4", key)
	})

	t.Run("Public IP ignores X-Forwarded-For", func(t *testing.T) {
		// Direct connection from public IP — attacker sending forged XFF
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "8.8.8.8")
		req.RemoteAddr = "198.51.100.10:9999"

		key := IPKeyExtractor(req)

		// Must use RemoteAddr, NOT the forged header
		assert.Equal(t, "198.51.100.10", key)
		assert.NotEqual(t, "8.8.8.8", key)
	})

	t.Run("Public IP ignores X-Real-IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Real-IP", "8.8.8.8")
		req.RemoteAddr = "198.51.100.10:9999"

		key := IPKeyExtractor(req)

		assert.Equal(t, "198.51.100.10", key)
	})

	t.Run("Explicit trusted public proxy", func(t *testing.T) {
		// Cloudflare-style: public IP explicitly added to TrustedProxies
		oldProxies := TrustedProxies
		TrustedProxies = []string{"104.16.0.1"}
		defer func() { TrustedProxies = oldProxies }()

		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.99")
		req.RemoteAddr = "104.16.0.1:443"

		key := IPKeyExtractor(req)

		assert.Equal(t, "203.0.113.99", key)
	})

	t.Run("No proxy headers uses RemoteAddr", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.10:1234"

		key := IPKeyExtractor(req)

		assert.Equal(t, "203.0.113.10", key)
	})
}

func TestStripPort(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		expected string
	}{
		{"IPv4 with port", "192.168.1.1:8080", "192.168.1.1"},
		{"IPv4 without port", "192.168.1.1", "192.168.1.1"},
		{"IPv6 with port", "[::1]:8080", "[::1]"},
		{"IPv6 without port", "[::1]", "[::1]"},
		{"IPv6 full with port", "[2001:db8::1]:443", "[2001:db8::1]"},
		{"Hostname with port", "localhost:3000", "localhost"},
		{"Empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripPort(tt.addr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsTrustedProxy(t *testing.T) {
	t.Run("RFC 1918 private IPs are trusted", func(t *testing.T) {
		assert.True(t, isTrustedProxy("10.0.0.1"))
		assert.True(t, isTrustedProxy("10.255.255.255"))
		assert.True(t, isTrustedProxy("172.16.0.1"))
		assert.True(t, isTrustedProxy("172.31.255.255"))
		assert.True(t, isTrustedProxy("192.168.0.1"))
		assert.True(t, isTrustedProxy("192.168.255.255"))
	})

	t.Run("Loopback is trusted", func(t *testing.T) {
		assert.True(t, isTrustedProxy("127.0.0.1"))
		assert.True(t, isTrustedProxy("127.0.0.2"))
	})

	t.Run("Public IPs are NOT trusted by default", func(t *testing.T) {
		assert.False(t, isTrustedProxy("8.8.8.8"))
		assert.False(t, isTrustedProxy("203.0.113.1"))
		assert.False(t, isTrustedProxy("198.51.100.1"))
		assert.False(t, isTrustedProxy("104.16.0.1"))
	})

	t.Run("172.32.x.x is NOT private", func(t *testing.T) {
		// 172.16-31 is private, 172.32+ is public
		assert.False(t, isTrustedProxy("172.32.0.1"))
	})

	t.Run("Explicit trusted proxy overrides", func(t *testing.T) {
		oldProxies := TrustedProxies
		TrustedProxies = []string{"104.16.0.1", "104.16.0.2"}
		defer func() { TrustedProxies = oldProxies }()

		assert.True(t, isTrustedProxy("104.16.0.1"))
		assert.True(t, isTrustedProxy("104.16.0.2"))
		assert.False(t, isTrustedProxy("104.16.0.3"))
	})

	t.Run("Explicit trusted proxy with CIDR range", func(t *testing.T) {
		oldProxies := TrustedProxies
		TrustedProxies = []string{"104.16.0.0/12"}
		defer func() { TrustedProxies = oldProxies }()

		// IPs within the CIDR range
		assert.True(t, isTrustedProxy("104.16.0.1"))
		assert.True(t, isTrustedProxy("104.31.255.255"))

		// IPs outside the CIDR range
		assert.False(t, isTrustedProxy("104.32.0.1"))
		assert.False(t, isTrustedProxy("8.8.8.8"))
	})

	t.Run("Non-parseable IP is not trusted", func(t *testing.T) {
		assert.False(t, isTrustedProxy("not-an-ip"))
		assert.False(t, isTrustedProxy(""))
	})
}

func TestUserIDKeyExtractor(t *testing.T) {
	t.Run("With User-ID header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-User-ID", "user123")

		key := UserIDKeyExtractor(req)

		assert.Equal(t, "user:user123", key)
	})

	t.Run("Fallback to IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.168.1.1:1234"

		key := UserIDKeyExtractor(req)

		assert.Equal(t, "192.168.1.1", key)
	})
}

func TestPathBasedKeyExtractor(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/users", nil)
	req.RemoteAddr = "192.168.1.1:1234"

	key := PathBasedKeyExtractor(req)

	assert.Contains(t, key, "192.168.1.1")
	assert.Contains(t, key, "/api/users")
}

func TestMethodBasedKeyExtractor(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/data", nil)
	req.RemoteAddr = "192.168.1.1:1234"

	key := MethodBasedKeyExtractor(req)

	assert.Contains(t, key, "192.168.1.1")
	assert.Contains(t, key, "POST")
}

func TestS3RateLimitingMiddleware(t *testing.T) {
	handler := S3RateLimitingMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/bucket/object", nil)
	req.Header.Set("X-Request-ID", "test-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestDifferentiatedRateLimitConfig(t *testing.T) {
	config := DifferentiatedRateLimitConfig()

	assert.NotNil(t, config)
	assert.NotNil(t, config.KeyExtractor)

	// Test key extraction for different methods
	getReq := httptest.NewRequest("GET", "/test", nil)
	getReq.RemoteAddr = "192.168.1.1:1234"
	getKey := config.KeyExtractor(getReq)
	assert.Contains(t, getKey, "read:")

	postReq := httptest.NewRequest("POST", "/test", nil)
	postReq.RemoteAddr = "192.168.1.1:1234"
	postKey := config.KeyExtractor(postReq)
	assert.Contains(t, postKey, "write:")

	deleteReq := httptest.NewRequest("DELETE", "/test", nil)
	deleteReq.RemoteAddr = "192.168.1.1:1234"
	deleteKey := config.KeyExtractor(deleteReq)
	assert.Contains(t, deleteKey, "delete:")
}

// Test CORS Middleware

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

func TestRestrictiveCORSConfig(t *testing.T) {
	config := RestrictiveCORSConfig()

	assert.Empty(t, config.AllowedOrigins)
	assert.NotContains(t, config.AllowedMethods, "POST")
	assert.False(t, config.AllowCredentials)
}

func TestDisabledCORSConfig(t *testing.T) {
	config := DisabledCORSConfig()

	assert.Empty(t, config.AllowedOrigins)
	assert.Empty(t, config.AllowedMethods)
	assert.Empty(t, config.AllowedHeaders)
	assert.Equal(t, "0", config.MaxAge)
}

// Test Verbose Logging Middleware

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
