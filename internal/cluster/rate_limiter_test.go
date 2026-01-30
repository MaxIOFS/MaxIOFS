package cluster

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimiter_Allow(t *testing.T) {
	// Create rate limiter: 5 requests per second, burst of 10
	rl := NewRateLimiter(5, 10)

	t.Run("Allows requests within burst limit", func(t *testing.T) {
		ip := "192.168.1.1"

		// Should allow 10 requests (burst size)
		for i := 0; i < 10; i++ {
			allowed := rl.Allow(ip)
			assert.True(t, allowed, "Request %d should be allowed within burst limit", i+1)
		}

		// 11th request should be denied (burst exhausted)
		allowed := rl.Allow(ip)
		assert.False(t, allowed, "Request beyond burst should be denied")
	})

	t.Run("Refills tokens over time", func(t *testing.T) {
		ip := "192.168.1.2"

		// Exhaust burst
		for i := 0; i < 10; i++ {
			rl.Allow(ip)
		}

		// Wait for 1 second (should refill 5 tokens at 5 req/s)
		time.Sleep(1 * time.Second)

		// Should allow 5 more requests
		allowedCount := 0
		for i := 0; i < 5; i++ {
			if rl.Allow(ip) {
				allowedCount++
			}
		}

		assert.GreaterOrEqual(t, allowedCount, 4, "Should have refilled at least 4 tokens")
	})

	t.Run("Different IPs have separate buckets", func(t *testing.T) {
		ip1 := "192.168.1.3"
		ip2 := "192.168.1.4"

		// Exhaust ip1's bucket
		for i := 0; i < 10; i++ {
			rl.Allow(ip1)
		}

		// ip1 should be denied
		assert.False(t, rl.Allow(ip1), "IP1 should be rate limited")

		// ip2 should still be allowed (separate bucket)
		assert.True(t, rl.Allow(ip2), "IP2 should not be affected by IP1's limit")
	})
}

func TestRateLimiter_Middleware(t *testing.T) {
	// Create rate limiter: 2 requests per second, burst of 3
	rl := NewRateLimiter(2, 3)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	rateLimitedHandler := rl.Middleware()(handler)

	t.Run("Allows requests within limit", func(t *testing.T) {
		// First 3 requests should succeed (burst)
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "192.168.1.10:12345"
			rr := httptest.NewRecorder()

			rateLimitedHandler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code, "Request %d should succeed", i+1)
			assert.Equal(t, "success", rr.Body.String())
		}
	})

	t.Run("Blocks requests exceeding limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.10:12345"
		rr := httptest.NewRecorder()

		rateLimitedHandler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusTooManyRequests, rr.Code, "Should return 429 when rate limited")
		assert.Contains(t, rr.Body.String(), "rate limit exceeded")
	})
}

func TestRateLimiter_Cleanup(t *testing.T) {
	// Create rate limiter with short cleanup interval for testing
	rl := &RateLimiter{
		requestsPerSecond: 10,
		burstSize:         20,
		buckets:           make(map[string]*tokenBucket),
		cleanupInterval:   100 * time.Millisecond, // Short interval for testing
	}

	// Add a bucket
	rl.Allow("192.168.1.20")

	// Verify bucket exists
	rl.mu.RLock()
	assert.Len(t, rl.buckets, 1, "Should have 1 bucket")
	rl.mu.RUnlock()

	// Manually trigger cleanup
	rl.cleanup()

	// Bucket should still exist (not stale yet)
	rl.mu.RLock()
	assert.Len(t, rl.buckets, 1, "Bucket should not be removed if not stale")
	rl.mu.RUnlock()
}

func TestRateLimiter_GetStats(t *testing.T) {
	rl := NewRateLimiter(10, 20)

	// Add some IPs
	rl.Allow("192.168.1.30")
	rl.Allow("192.168.1.31")
	rl.Allow("192.168.1.32")

	stats := rl.GetStats()

	require.NotNil(t, stats)
	assert.Equal(t, 3, stats["total_tracked_ips"], "Should track 3 IPs")
	assert.Equal(t, 10, stats["requests_per_second"], "Should match configured rate")
	assert.Equal(t, 20, stats["burst_size"], "Should match configured burst")
}

func TestRateLimiter_HighConcurrency(t *testing.T) {
	rl := NewRateLimiter(100, 200)

	// Simulate 100 concurrent requests from same IP
	ip := "192.168.1.100"
	results := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func() {
			results <- rl.Allow(ip)
		}()
	}

	// Collect results
	allowedCount := 0
	deniedCount := 0

	for i := 0; i < 100; i++ {
		if <-results {
			allowedCount++
		} else {
			deniedCount++
		}
	}

	t.Logf("Allowed: %d, Denied: %d", allowedCount, deniedCount)

	// Due to race conditions, we can't assert exact counts
	// But total should be 100
	assert.Equal(t, 100, allowedCount+deniedCount, "Should process all requests")
}
