package auth

import (
	"testing"
	"time"
)

// TestLoginRateLimiter_AllowLogin tests basic allow login functionality
func TestLoginRateLimiter_AllowLogin(t *testing.T) {
	limiter := NewLoginRateLimiter(3, 60)

	testIP := "192.168.1.100"

	// First attempt should be allowed
	if !limiter.AllowLogin(testIP) {
		t.Error("First attempt should be allowed")
	}

	// Record failed attempts
	limiter.RecordFailedAttempt(testIP)
	limiter.RecordFailedAttempt(testIP)
	limiter.RecordFailedAttempt(testIP)

	// Fourth attempt should be denied (limit is 3)
	if limiter.AllowLogin(testIP) {
		t.Error("Fourth attempt should be denied after 3 failures")
	}
}

// TestLoginRateLimiter_AllowLogin_MultipleIPs tests rate limiting per IP
func TestLoginRateLimiter_AllowLogin_MultipleIPs(t *testing.T) {
	limiter := NewLoginRateLimiter(2, 60)

	ip1 := "10.0.0.1"
	ip2 := "10.0.0.2"

	// Block IP1
	limiter.RecordFailedAttempt(ip1)
	limiter.RecordFailedAttempt(ip1)

	// IP1 should be blocked
	if limiter.AllowLogin(ip1) {
		t.Error("IP1 should be blocked after 2 failures")
	}

	// IP2 should still be allowed (independent tracking)
	if !limiter.AllowLogin(ip2) {
		t.Error("IP2 should be allowed (different IP)")
	}
}

// TestLoginRateLimiter_WindowExpiry tests that the time window expires
func TestLoginRateLimiter_WindowExpiry(t *testing.T) {
	// Short window for testing (2 seconds)
	limiter := NewLoginRateLimiter(2, 2)

	testIP := "192.168.1.200"

	// Block the IP
	limiter.RecordFailedAttempt(testIP)
	limiter.RecordFailedAttempt(testIP)

	// Should be blocked
	if limiter.AllowLogin(testIP) {
		t.Error("IP should be blocked after 2 failures")
	}

	// Wait for window to expire
	time.Sleep(2100 * time.Millisecond)

	// Should be allowed again after window expires
	if !limiter.AllowLogin(testIP) {
		t.Error("IP should be allowed after window expires")
	}
}

// TestLoginRateLimiter_RecordFailedAttempt tests recording failed attempts
func TestLoginRateLimiter_RecordFailedAttempt(t *testing.T) {
	limiter := NewLoginRateLimiter(5, 60)

	testIP := "172.16.0.1"

	// Record multiple failed attempts
	for i := 0; i < 3; i++ {
		limiter.RecordFailedAttempt(testIP)
	}

	// Verify attempts are tracked
	attempts := limiter.GetAttempts(testIP)
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

// TestLoginRateLimiter_RecordFailedAttempt_WindowReset tests window reset behavior
func TestLoginRateLimiter_RecordFailedAttempt_WindowReset(t *testing.T) {
	// Short window for testing
	limiter := NewLoginRateLimiter(3, 1)

	testIP := "192.168.2.1"

	// Record 2 attempts
	limiter.RecordFailedAttempt(testIP)
	limiter.RecordFailedAttempt(testIP)

	if limiter.GetAttempts(testIP) != 2 {
		t.Error("Should have 2 attempts")
	}

	// Wait for window to expire
	time.Sleep(1100 * time.Millisecond)

	// Record new attempt after window expiry - should reset counter
	limiter.RecordFailedAttempt(testIP)

	attempts := limiter.GetAttempts(testIP)
	if attempts != 1 {
		t.Errorf("Expected 1 attempt after window reset, got %d", attempts)
	}
}

// TestLoginRateLimiter_ResetIP tests resetting rate limit for an IP
func TestLoginRateLimiter_ResetIP(t *testing.T) {
	limiter := NewLoginRateLimiter(2, 60)

	testIP := "10.1.1.1"

	// Block the IP
	limiter.RecordFailedAttempt(testIP)
	limiter.RecordFailedAttempt(testIP)

	// Verify it's blocked
	if limiter.AllowLogin(testIP) {
		t.Error("IP should be blocked")
	}

	// Reset the IP
	limiter.ResetIP(testIP)

	// Should be allowed now
	if !limiter.AllowLogin(testIP) {
		t.Error("IP should be allowed after reset")
	}

	// Attempts should be zero
	if limiter.GetAttempts(testIP) != 0 {
		t.Error("Attempts should be 0 after reset")
	}
}

// TestLoginRateLimiter_ResetIP_NonExistent tests resetting non-existent IP
func TestLoginRateLimiter_ResetIP_NonExistent(t *testing.T) {
	limiter := NewLoginRateLimiter(5, 60)

	// Reset IP that was never tracked - should not panic
	limiter.ResetIP("10.10.10.10")

	// Should still work normally
	if !limiter.AllowLogin("10.10.10.10") {
		t.Error("Should allow login for never-tracked IP after reset")
	}
}

// TestLoginRateLimiter_GetAttempts tests getting attempt count
func TestLoginRateLimiter_GetAttempts(t *testing.T) {
	limiter := NewLoginRateLimiter(10, 60)

	testIP := "192.168.100.1"

	tests := []struct {
		name            string
		recordAttempts  int
		expectedCount   int
	}{
		{
			name:            "Zero attempts",
			recordAttempts:  0,
			expectedCount:   0,
		},
		{
			name:            "One attempt",
			recordAttempts:  1,
			expectedCount:   1,
		},
		{
			name:            "Five attempts",
			recordAttempts:  5,
			expectedCount:   5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use unique IP for each test
			ip := testIP + tt.name

			// Record attempts
			for i := 0; i < tt.recordAttempts; i++ {
				limiter.RecordFailedAttempt(ip)
			}

			// Get attempts
			attempts := limiter.GetAttempts(ip)
			if attempts != tt.expectedCount {
				t.Errorf("Expected %d attempts, got %d", tt.expectedCount, attempts)
			}
		})
	}
}

// TestLoginRateLimiter_GetAttempts_NonExistent tests getting attempts for unknown IP
func TestLoginRateLimiter_GetAttempts_NonExistent(t *testing.T) {
	limiter := NewLoginRateLimiter(5, 60)

	// Get attempts for IP that was never tracked
	attempts := limiter.GetAttempts("1.2.3.4")

	if attempts != 0 {
		t.Errorf("Expected 0 attempts for unknown IP, got %d", attempts)
	}
}

// TestLoginRateLimiter_GetAttempts_ExpiredWindow tests expired window returns zero
func TestLoginRateLimiter_GetAttempts_ExpiredWindow(t *testing.T) {
	// Short window for testing
	limiter := NewLoginRateLimiter(5, 1)

	testIP := "192.168.50.1"

	// Record attempts
	limiter.RecordFailedAttempt(testIP)
	limiter.RecordFailedAttempt(testIP)
	limiter.RecordFailedAttempt(testIP)

	// Verify attempts exist
	if limiter.GetAttempts(testIP) != 3 {
		t.Error("Should have 3 attempts")
	}

	// Wait for window to expire
	time.Sleep(1100 * time.Millisecond)

	// GetAttempts should return 0 for expired window
	attempts := limiter.GetAttempts(testIP)
	if attempts != 0 {
		t.Errorf("Expected 0 attempts after window expiry, got %d", attempts)
	}
}

// TestLoginRateLimiter_Cleanup tests the cleanup function
func TestLoginRateLimiter_Cleanup(t *testing.T) {
	// Short window for testing
	limiter := NewLoginRateLimiter(5, 2)

	// Record attempts for multiple IPs
	ips := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	for _, ip := range ips {
		limiter.RecordFailedAttempt(ip)
	}

	// Verify all IPs have attempts
	for _, ip := range ips {
		if limiter.GetAttempts(ip) != 1 {
			t.Errorf("IP %s should have 1 attempt", ip)
		}
	}

	// Wait for window to expire
	time.Sleep(2100 * time.Millisecond)

	// Call cleanup manually
	limiter.cleanup()

	// After cleanup, all expired entries should be removed
	// GetAttempts should return 0 (entries deleted)
	limiter.mu.RLock()
	mapSize := len(limiter.attempts)
	limiter.mu.RUnlock()

	if mapSize != 0 {
		t.Errorf("Expected cleanup to remove all expired entries, but %d remain", mapSize)
	}
}

// TestLoginRateLimiter_Cleanup_PartialExpiry tests cleanup with mixed expired/active entries
func TestLoginRateLimiter_Cleanup_PartialExpiry(t *testing.T) {
	// Short window for testing
	limiter := NewLoginRateLimiter(5, 2)

	oldIP := "10.0.1.1"
	newIP := "10.0.1.2"

	// Record attempt for old IP
	limiter.RecordFailedAttempt(oldIP)

	// Wait for it to expire
	time.Sleep(2100 * time.Millisecond)

	// Record attempt for new IP (should not expire)
	limiter.RecordFailedAttempt(newIP)

	// Run cleanup
	limiter.cleanup()

	// Old IP should be removed
	limiter.mu.RLock()
	_, oldExists := limiter.attempts[oldIP]
	_, newExists := limiter.attempts[newIP]
	limiter.mu.RUnlock()

	if oldExists {
		t.Error("Old IP should be removed by cleanup")
	}

	if !newExists {
		t.Error("New IP should NOT be removed by cleanup")
	}
}

// TestLoginRateLimiter_ConcurrentAccess tests thread safety
func TestLoginRateLimiter_ConcurrentAccess(t *testing.T) {
	limiter := NewLoginRateLimiter(100, 60)

	done := make(chan bool)

	// Launch multiple goroutines accessing the limiter concurrently
	for i := 0; i < 10; i++ {
		go func(workerID int) {
			testIP := "192.168.0.1"

			for j := 0; j < 100; j++ {
				limiter.AllowLogin(testIP)
				limiter.RecordFailedAttempt(testIP)
				limiter.GetAttempts(testIP)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic - just verify it completes
	attempts := limiter.GetAttempts("192.168.0.1")
	if attempts < 0 {
		t.Error("Attempts should not be negative")
	}
}

// TestLoginRateLimiter_EdgeCases tests various edge cases
func TestLoginRateLimiter_EdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		maxAttempts  int
		windowSec    int
		shouldCreate bool
	}{
		{
			name:         "Zero max attempts",
			maxAttempts:  0,
			windowSec:    60,
			shouldCreate: true,
		},
		{
			name:         "Zero window",
			maxAttempts:  5,
			windowSec:    0,
			shouldCreate: true,
		},
		{
			name:         "Very large max attempts",
			maxAttempts:  1000000,
			windowSec:    3600,
			shouldCreate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewLoginRateLimiter(tt.maxAttempts, tt.windowSec)
			if limiter == nil {
				t.Error("Limiter should be created")
			}

			// Verify basic operations don't panic
			limiter.AllowLogin("test.ip")
			limiter.RecordFailedAttempt("test.ip")
			limiter.GetAttempts("test.ip")
		})
	}
}

// TestLoginRateLimiter_EmptyIP tests behavior with empty IP string
func TestLoginRateLimiter_EmptyIP(t *testing.T) {
	limiter := NewLoginRateLimiter(5, 60)

	// Empty IP should be treated as valid string key
	emptyIP := ""

	// Should not panic
	if !limiter.AllowLogin(emptyIP) {
		t.Error("Empty IP should be allowed initially")
	}

	limiter.RecordFailedAttempt(emptyIP)
	attempts := limiter.GetAttempts(emptyIP)

	if attempts != 1 {
		t.Errorf("Expected 1 attempt for empty IP, got %d", attempts)
	}

	limiter.ResetIP(emptyIP)

	if limiter.GetAttempts(emptyIP) != 0 {
		t.Error("Empty IP should be reset")
	}
}

// TestLoginRateLimiter_SequentialAttempts tests sequential attempt tracking
func TestLoginRateLimiter_SequentialAttempts(t *testing.T) {
	limiter := NewLoginRateLimiter(3, 60)

	testIP := "192.168.1.50"

	// First attempt
	if !limiter.AllowLogin(testIP) {
		t.Error("First attempt should be allowed")
	}
	limiter.RecordFailedAttempt(testIP)

	// Second attempt
	if !limiter.AllowLogin(testIP) {
		t.Error("Second attempt should be allowed")
	}
	limiter.RecordFailedAttempt(testIP)

	// Third attempt
	if !limiter.AllowLogin(testIP) {
		t.Error("Third attempt should be allowed")
	}
	limiter.RecordFailedAttempt(testIP)

	// Fourth attempt should be blocked
	if limiter.AllowLogin(testIP) {
		t.Error("Fourth attempt should be blocked")
	}

	// Verify count
	if limiter.GetAttempts(testIP) != 3 {
		t.Errorf("Expected 3 attempts, got %d", limiter.GetAttempts(testIP))
	}
}
