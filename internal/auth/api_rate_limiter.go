package auth

import (
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// apiRateBucket holds a sliding-window token bucket for a single key (user/IP).
type apiRateBucket struct {
	tokens   float64
	lastFill time.Time
}

// APIRateLimiter enforces per-user (by access key or user ID) request rate limiting
// for the S3 API using a token-bucket algorithm.
type APIRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*apiRateBucket
}

// NewAPIRateLimiter creates a new API rate limiter.
func NewAPIRateLimiter() *APIRateLimiter {
	rl := &APIRateLimiter{
		buckets: make(map[string]*apiRateBucket),
	}
	go rl.cleanupLoop()
	return rl
}

// Allow checks and consumes one token for the given key at the given ratePerSecond.
// Returns true if the request is allowed.
func (rl *APIRateLimiter) Allow(key string, ratePerSecond int) bool {
	if ratePerSecond <= 0 {
		return true // disabled
	}
	rate := float64(ratePerSecond)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		b = &apiRateBucket{tokens: rate, lastFill: now}
		rl.buckets[key] = b
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * rate
	if b.tokens > rate {
		b.tokens = rate // cap at burst = 1 second of tokens
	}
	b.lastFill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// cleanupLoop removes stale buckets every minute.
func (rl *APIRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for k, b := range rl.buckets {
			if time.Since(b.lastFill) > 2*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

// APIRateLimitMiddleware returns a Gorilla Mux middleware that enforces per-user
// S3 API rate limiting based on the security.ratelimit_api_per_second setting.
// The limit is read from settings on every request for hot-reload support.
// It identifies users by the Authorization header access key, or falls back to remote IP.
func APIRateLimitMiddleware(sm SettingsManager, rl *APIRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if rate limiting is enabled
			if enabled, err := sm.GetBool("security.ratelimit_enabled"); err == nil && !enabled {
				next.ServeHTTP(w, r)
				return
			}

			ratePerSecond := 100 // default
			if v, err := sm.GetInt("security.ratelimit_api_per_second"); err == nil && v > 0 {
				ratePerSecond = v
			}

			// Identify user: prefer Authorization header value (access key), fall back to IP
			key := r.Header.Get("Authorization")
			if key == "" {
				key = r.RemoteAddr
			}
			// Use just the first 64 chars to keep map keys small
			if len(key) > 64 {
				key = key[:64]
			}

			if !rl.Allow(key, ratePerSecond) {
				logrus.WithFields(logrus.Fields{
					"key":  key[:min(len(key), 20)],
					"rate": ratePerSecond,
				}).Warn("S3 API rate limit exceeded")
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
