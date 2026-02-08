package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	// RequestsPerSecond defines the maximum number of requests per second
	RequestsPerSecond float64
	// BurstSize defines the maximum burst size
	BurstSize int
	// WindowSize defines the time window for rate limiting
	WindowSize time.Duration
	// KeyExtractor extracts the key for rate limiting (IP, user ID, etc.)
	KeyExtractor func(*http.Request) string
	// OnRateLimitExceeded is called when rate limit is exceeded
	OnRateLimitExceeded func(http.ResponseWriter, *http.Request, string)
	// SkipPaths contains paths that should not be rate limited
	SkipPaths []string
	// Store is the storage backend for rate limit data
	Store RateLimitStore
}

// RateLimitStore defines the interface for rate limit storage
type RateLimitStore interface {
	// Allow checks if a request is allowed and updates counters
	Allow(key string, limit float64, window time.Duration) (bool, time.Duration, error)
	// Reset resets the counter for a key
	Reset(key string) error
	// Cleanup removes expired entries
	Cleanup() error
}

// TokenBucket represents a token bucket for rate limiting
type TokenBucket struct {
	tokens     float64
	capacity   float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

// InMemoryRateLimitStore implements RateLimitStore using in-memory storage
type InMemoryRateLimitStore struct {
	buckets map[string]*TokenBucket
	mu      sync.RWMutex
}

// NewInMemoryRateLimitStore creates a new in-memory rate limit store
func NewInMemoryRateLimitStore() *InMemoryRateLimitStore {
	store := &InMemoryRateLimitStore{
		buckets: make(map[string]*TokenBucket),
	}

	// Start cleanup routine
	go store.cleanupRoutine()

	return store
}

// Allow implements RateLimitStore.Allow
func (s *InMemoryRateLimitStore) Allow(key string, limit float64, window time.Duration) (bool, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bucket, exists := s.buckets[key]
	if !exists {
		bucket = &TokenBucket{
			tokens:     limit,
			capacity:   limit,
			refillRate: limit / window.Seconds(),
			lastRefill: time.Now(),
		}
		s.buckets[key] = bucket
	}

	return bucket.allow(), time.Duration(0), nil
}

// Reset implements RateLimitStore.Reset
func (s *InMemoryRateLimitStore) Reset(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.buckets, key)
	return nil
}

// Cleanup implements RateLimitStore.Cleanup
func (s *InMemoryRateLimitStore) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for key, bucket := range s.buckets {
		bucket.mu.Lock()
		// Remove buckets that haven't been used for more than 1 hour
		if now.Sub(bucket.lastRefill) > time.Hour {
			delete(s.buckets, key)
		}
		bucket.mu.Unlock()
	}

	return nil
}

// cleanupRoutine runs periodic cleanup
func (s *InMemoryRateLimitStore) cleanupRoutine() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.Cleanup()
	}
}

// allow checks if a token is available and consumes it
func (tb *TokenBucket) allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()

	// Refill tokens
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	tb.lastRefill = now

	// Check if token is available
	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}

	return false
}

// DefaultRateLimitConfig returns the default rate limiting configuration
func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		RequestsPerSecond: 100.0, // 100 requests per second
		BurstSize:         200,   // Allow burst of 200 requests
		WindowSize:        time.Second,
		KeyExtractor:      IPKeyExtractor,
		OnRateLimitExceeded: func(w http.ResponseWriter, r *http.Request, key string) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		},
		SkipPaths: []string{"/health", "/metrics"},
		Store:     NewInMemoryRateLimitStore(),
	}
}

// StrictRateLimitConfig returns a strict rate limiting configuration
func StrictRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		RequestsPerSecond: 10.0, // 10 requests per second
		BurstSize:         20,   // Allow burst of 20 requests
		WindowSize:        time.Second,
		KeyExtractor:      IPKeyExtractor,
		OnRateLimitExceeded: func(w http.ResponseWriter, r *http.Request, key string) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
		},
		SkipPaths: []string{"/health"},
		Store:     NewInMemoryRateLimitStore(),
	}
}

// GenerousRateLimitConfig returns a generous rate limiting configuration
func GenerousRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		RequestsPerSecond: 1000.0, // 1000 requests per second
		BurstSize:         2000,   // Allow burst of 2000 requests
		WindowSize:        time.Second,
		KeyExtractor:      IPKeyExtractor,
		OnRateLimitExceeded: func(w http.ResponseWriter, r *http.Request, key string) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		},
		SkipPaths: []string{"/health", "/metrics"},
		Store:     NewInMemoryRateLimitStore(),
	}
}

// RateLimit returns a rate limiting middleware with default configuration
func RateLimit() func(http.Handler) http.Handler {
	return RateLimitWithConfig(DefaultRateLimitConfig())
}

// RateLimitWithConfig returns a rate limiting middleware with custom configuration
func RateLimitWithConfig(config *RateLimitConfig) func(http.Handler) http.Handler {
	if config.Store == nil {
		config.Store = NewInMemoryRateLimitStore()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if path should be skipped
			for _, skipPath := range config.SkipPaths {
				if r.URL.Path == skipPath {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Extract key for rate limiting
			key := config.KeyExtractor(r)

			// Check rate limit
			allowed, retryAfter, err := config.Store.Allow(key, config.RequestsPerSecond, config.WindowSize)
			if err != nil {
				// Log error but don't block request
				next.ServeHTTP(w, r)
				return
			}

			if !allowed {
				// Set rate limit headers
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.0f", config.RequestsPerSecond))
				w.Header().Set("X-RateLimit-Remaining", "0")
				if retryAfter > 0 {
					w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
				}

				// Call rate limit exceeded handler
				config.OnRateLimitExceeded(w, r, key)
				return
			}

			// Set rate limit headers for successful requests
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.0f", config.RequestsPerSecond))

			next.ServeHTTP(w, r)
		})
	}
}

// Key extraction functions

// TrustedProxies holds additional trusted proxy IPs/CIDRs beyond private networks.
// By default, all RFC 1918 private networks (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
// and loopback (127.0.0.0/8, ::1) are trusted automatically.
// Add entries here only for public IPs that act as proxies (e.g., Cloudflare ranges).
var TrustedProxies []string

// privateNetworks contains RFC 1918 private ranges + loopback.
// Connections from these IPs are assumed to come from internal infrastructure
// (load balancers, reverse proxies, Docker networks, etc.)
var privateNetworks []*net.IPNet

func init() {
	privateCIDRs := []string{
		"127.0.0.0/8",    // Loopback
		"10.0.0.0/8",     // RFC 1918 Class A
		"172.16.0.0/12",  // RFC 1918 Class B
		"192.168.0.0/16", // RFC 1918 Class C
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
	}
	for _, cidr := range privateCIDRs {
		_, network, _ := net.ParseCIDR(cidr)
		privateNetworks = append(privateNetworks, network)
	}
}

// IPKeyExtractor extracts the real client IP address as the rate limiting key.
// Trusts X-Forwarded-For/X-Real-IP when the request comes from a private network
// or an explicitly trusted proxy — no configuration needed for standard deployments.
func IPKeyExtractor(r *http.Request) string {
	remoteIP := stripPort(r.RemoteAddr)

	// Trust proxy headers if the direct connection is from a private/trusted source
	if isTrustedProxy(remoteIP) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// X-Forwarded-For is comma-separated: client, proxy1, proxy2
			// The first IP is the original client
			parts := strings.SplitN(xff, ",", 2)
			clientIP := strings.TrimSpace(parts[0])
			if clientIP != "" {
				return clientIP
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	return remoteIP
}

// stripPort removes the port from an address like "192.168.1.1:12345"
func stripPort(addr string) string {
	// Handle IPv6 addresses like "[::1]:8080"
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		// Make sure we're not stripping part of an IPv6 address
		if bracketIdx := strings.LastIndex(addr, "]"); bracketIdx != -1 {
			if idx > bracketIdx {
				return addr[:idx]
			}
			return addr
		}
		return addr[:idx]
	}
	return addr
}

// isTrustedProxy checks if the IP is a private network address or in the explicit trusted list
func isTrustedProxy(ip string) bool {
	// Check private networks (RFC 1918 + loopback)
	parsedIP := net.ParseIP(ip)
	if parsedIP != nil {
		for _, network := range privateNetworks {
			if network.Contains(parsedIP) {
				return true
			}
		}
	}

	// Check explicit trusted proxies list (for public proxy IPs like Cloudflare)
	for _, trusted := range TrustedProxies {
		// Support CIDR notation (e.g., "104.16.0.0/12")
		if strings.Contains(trusted, "/") {
			_, network, err := net.ParseCIDR(trusted)
			if err == nil && parsedIP != nil && network.Contains(parsedIP) {
				return true
			}
		} else if trusted == ip {
			return true
		}
	}
	return false
}

// UserIDKeyExtractor extracts the user ID as the rate limiting key
func UserIDKeyExtractor(r *http.Request) string {
	// This would typically extract user ID from context or JWT token
	// For MVP, fall back to IP address
	if userID := r.Header.Get("X-User-ID"); userID != "" {
		return "user:" + userID
	}
	return IPKeyExtractor(r)
}

// PathBasedKeyExtractor creates a key extractor that includes the path
func PathBasedKeyExtractor(r *http.Request) string {
	ip := IPKeyExtractor(r)
	return fmt.Sprintf("%s:%s", ip, r.URL.Path)
}

// MethodBasedKeyExtractor creates a key extractor that includes the HTTP method
func MethodBasedKeyExtractor(r *http.Request) string {
	ip := IPKeyExtractor(r)
	return fmt.Sprintf("%s:%s", ip, r.Method)
}

// S3RateLimitingMiddleware returns a rate limiting middleware specifically configured for S3 operations
func S3RateLimitingMiddleware() func(http.Handler) http.Handler {
	config := &RateLimitConfig{
		RequestsPerSecond: 500.0, // 500 requests per second for S3 operations
		BurstSize:         1000,  // Allow burst of 1000 requests
		WindowSize:        time.Second,
		KeyExtractor:      IPKeyExtractor,
		OnRateLimitExceeded: func(w http.ResponseWriter, r *http.Request, key string) {
			// S3-compatible error response
			w.Header().Set("Content-Type", "application/xml")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error>
    <Code>SlowDown</Code>
    <Message>Please reduce your request rate.</Message>
    <RequestId>` + r.Header.Get("X-Request-ID") + `</RequestId>
</Error>`))
		},
		SkipPaths: []string{"/health"},
		Store:     NewInMemoryRateLimitStore(),
	}

	return RateLimitWithConfig(config)
}

// DifferentiatedRateLimitConfig creates different rate limits for different operations
func DifferentiatedRateLimitConfig() *RateLimitConfig {
	store := NewInMemoryRateLimitStore()

	return &RateLimitConfig{
		RequestsPerSecond: 100.0, // Default rate
		BurstSize:         200,
		WindowSize:        time.Second,
		KeyExtractor: func(r *http.Request) string {
			ip := IPKeyExtractor(r)

			// Different limits for different operations
			switch r.Method {
			case "GET", "HEAD":
				return fmt.Sprintf("read:%s", ip)
			case "PUT", "POST":
				return fmt.Sprintf("write:%s", ip)
			case "DELETE":
				return fmt.Sprintf("delete:%s", ip)
			default:
				return ip
			}
		},
		OnRateLimitExceeded: func(w http.ResponseWriter, r *http.Request, key string) {
			http.Error(w, "Rate limit exceeded for "+r.Method+" operations", http.StatusTooManyRequests)
		},
		SkipPaths: []string{"/health", "/metrics"},
		Store:     store,
	}
}