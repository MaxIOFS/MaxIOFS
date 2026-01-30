package cluster

import (
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// RateLimiter implements a token bucket rate limiter for cluster internal APIs
type RateLimiter struct {
	// requests per second allowed per IP
	requestsPerSecond int
	// burst size - max requests allowed at once
	burstSize int
	// map of IP -> bucket
	buckets map[string]*tokenBucket
	mu      sync.RWMutex
	// cleanup interval
	cleanupInterval time.Duration
	log             *logrus.Entry
}

// tokenBucket implements a token bucket for rate limiting
type tokenBucket struct {
	tokens         int
	maxTokens      int
	refillRate     int // tokens per second
	lastRefillTime time.Time
	mu             sync.Mutex
}

// NewRateLimiter creates a new rate limiter
// requestsPerSecond: max requests per second per IP
// burstSize: max burst requests allowed
func NewRateLimiter(requestsPerSecond, burstSize int) *RateLimiter {
	rl := &RateLimiter{
		requestsPerSecond: requestsPerSecond,
		burstSize:         burstSize,
		buckets:           make(map[string]*tokenBucket),
		cleanupInterval:   5 * time.Minute,
		log:               logrus.WithField("component", "rate_limiter"),
	}

	// Start cleanup goroutine to remove stale buckets
	go rl.cleanupLoop()

	return rl
}

// Allow checks if a request from the given IP should be allowed
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.RLock()
	bucket, exists := rl.buckets[ip]
	rl.mu.RUnlock()

	if !exists {
		// Create new bucket for this IP
		bucket = &tokenBucket{
			tokens:         rl.burstSize,
			maxTokens:      rl.burstSize,
			refillRate:     rl.requestsPerSecond,
			lastRefillTime: time.Now(),
		}

		rl.mu.Lock()
		rl.buckets[ip] = bucket
		rl.mu.Unlock()
	}

	return bucket.takeToken()
}

// takeToken attempts to take a token from the bucket
func (tb *tokenBucket) takeToken() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(tb.lastRefillTime)
	tokensToAdd := int(elapsed.Seconds() * float64(tb.refillRate))

	if tokensToAdd > 0 {
		tb.tokens += tokensToAdd
		if tb.tokens > tb.maxTokens {
			tb.tokens = tb.maxTokens
		}
		tb.lastRefillTime = now
	}

	// Try to take a token
	if tb.tokens > 0 {
		tb.tokens--
		return true
	}

	return false
}

// cleanupLoop periodically removes stale buckets
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		rl.cleanup()
	}
}

// cleanup removes buckets that haven't been used in a while
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	staleThreshold := 10 * time.Minute

	for ip, bucket := range rl.buckets {
		bucket.mu.Lock()
		timeSinceLastUse := now.Sub(bucket.lastRefillTime)
		bucket.mu.Unlock()

		if timeSinceLastUse > staleThreshold {
			delete(rl.buckets, ip)
			rl.log.WithField("ip", ip).Debug("Removed stale rate limit bucket")
		}
	}
}

// Middleware creates an HTTP middleware for rate limiting
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract client IP
			ip := r.RemoteAddr

			// Check if allowed
			if !rl.Allow(ip) {
				rl.log.WithFields(logrus.Fields{
					"ip":   ip,
					"path": r.URL.Path,
				}).Warn("Rate limit exceeded for cluster API")

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error": "rate limit exceeded", "message": "too many requests from this IP"}`))
				return
			}

			// Allow request to proceed
			next.ServeHTTP(w, r)
		})
	}
}

// GetStats returns current rate limiter statistics
func (rl *RateLimiter) GetStats() map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	return map[string]interface{}{
		"total_tracked_ips":   len(rl.buckets),
		"requests_per_second": rl.requestsPerSecond,
		"burst_size":          rl.burstSize,
	}
}
