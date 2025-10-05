package auth

import (
	"sync"
	"time"
)

// RateLimitAttempt tracks login attempts from an IP address for rate limiting
type RateLimitAttempt struct {
	Count     int
	FirstTry  time.Time
	LastTry   time.Time
}

// LoginRateLimiter implements in-memory rate limiting for login attempts
type LoginRateLimiter struct {
	attempts map[string]*RateLimitAttempt
	mu       sync.RWMutex

	// Configuration
	maxAttempts   int
	windowSeconds int
}

// NewLoginRateLimiter creates a new rate limiter
// maxAttempts: maximum number of login attempts allowed
// windowSeconds: time window in seconds for counting attempts
func NewLoginRateLimiter(maxAttempts, windowSeconds int) *LoginRateLimiter {
	limiter := &LoginRateLimiter{
		attempts:      make(map[string]*RateLimitAttempt),
		maxAttempts:   maxAttempts,
		windowSeconds: windowSeconds,
	}

	// Start cleanup goroutine to remove old entries
	go limiter.cleanupLoop()

	return limiter
}

// AllowLogin checks if login attempt from IP is allowed
func (l *LoginRateLimiter) AllowLogin(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	attempt, exists := l.attempts[ip]

	if !exists {
		// First attempt from this IP
		l.attempts[ip] = &RateLimitAttempt{
			Count:    1,
			FirstTry: now,
			LastTry:  now,
		}
		return true
	}

	// Check if window has expired
	if now.Sub(attempt.FirstTry) > time.Duration(l.windowSeconds)*time.Second {
		// Reset window
		l.attempts[ip] = &RateLimitAttempt{
			Count:    1,
			FirstTry: now,
			LastTry:  now,
		}
		return true
	}

	// Check if limit exceeded
	if attempt.Count >= l.maxAttempts {
		return false
	}

	// Increment counter
	attempt.Count++
	attempt.LastTry = now
	return true
}

// RecordFailedAttempt records a failed login attempt
func (l *LoginRateLimiter) RecordFailedAttempt(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	attempt, exists := l.attempts[ip]

	if !exists {
		l.attempts[ip] = &RateLimitAttempt{
			Count:    1,
			FirstTry: now,
			LastTry:  now,
		}
		return
	}

	// Check if window has expired
	if now.Sub(attempt.FirstTry) > time.Duration(l.windowSeconds)*time.Second {
		// Reset window
		l.attempts[ip] = &RateLimitAttempt{
			Count:    1,
			FirstTry: now,
			LastTry:  now,
		}
		return
	}

	// Increment counter
	attempt.Count++
	attempt.LastTry = now
}

// ResetIP removes rate limit for an IP address
func (l *LoginRateLimiter) ResetIP(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

// GetAttempts returns the current attempt count for an IP
func (l *LoginRateLimiter) GetAttempts(ip string) int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	attempt, exists := l.attempts[ip]
	if !exists {
		return 0
	}

	// Check if window has expired
	if time.Since(attempt.FirstTry) > time.Duration(l.windowSeconds)*time.Second {
		return 0
	}

	return attempt.Count
}

// cleanupLoop periodically removes expired entries
func (l *LoginRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		l.cleanup()
	}
}

// cleanup removes expired entries from the map
func (l *LoginRateLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	windowDuration := time.Duration(l.windowSeconds) * time.Second

	for ip, attempt := range l.attempts {
		if now.Sub(attempt.LastTry) > windowDuration {
			delete(l.attempts, ip)
		}
	}
}
