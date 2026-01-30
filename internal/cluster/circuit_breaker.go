package cluster

import (
	"errors"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	// ErrCircuitOpen is returned when the circuit breaker is open
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	// StateClosed means requests are allowed
	StateClosed CircuitBreakerState = iota
	// StateOpen means requests are blocked
	StateOpen
	// StateHalfOpen means we're testing if the service recovered
	StateHalfOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern for node communication
type CircuitBreaker struct {
	// failure threshold before opening circuit
	failureThreshold int
	// success threshold to close circuit from half-open
	successThreshold int
	// timeout before attempting recovery (half-open)
	timeout time.Duration
	// current state
	state CircuitBreakerState
	// failure count
	failures int
	// success count in half-open state
	successes int
	// last failure time
	lastFailureTime time.Time
	// mutex for thread safety
	mu sync.RWMutex
	// logger
	log *logrus.Entry
	// node identifier
	nodeID string
}

// NewCircuitBreaker creates a new circuit breaker
// failureThreshold: number of failures before opening circuit
// successThreshold: number of successes to close circuit from half-open
// timeout: duration before attempting recovery
func NewCircuitBreaker(nodeID string, failureThreshold, successThreshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		timeout:          timeout,
		state:            StateClosed,
		failures:         0,
		successes:        0,
		log:              logrus.WithFields(logrus.Fields{"component": "circuit_breaker", "node_id": nodeID}),
		nodeID:           nodeID,
	}
}

// Call executes a function with circuit breaker protection
func (cb *CircuitBreaker) Call(fn func() error) error {
	if !cb.allowRequest() {
		cb.log.WithField("state", cb.state.String()).Debug("Circuit breaker blocked request")
		return ErrCircuitOpen
	}

	// Execute the function
	err := fn()

	// Record result
	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

// allowRequest checks if a request should be allowed
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// Check if timeout has elapsed
		if time.Since(cb.lastFailureTime) > cb.timeout {
			// Transition to half-open
			cb.log.Info("Circuit breaker transitioning from open to half-open")
			cb.state = StateHalfOpen
			cb.successes = 0
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

// recordFailure records a failed request
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.failureThreshold {
			cb.log.WithField("failures", cb.failures).Warn("Circuit breaker opening due to failures")
			cb.state = StateOpen
			cb.failures = 0
		}
	case StateHalfOpen:
		// Any failure in half-open state reopens the circuit
		cb.log.Warn("Circuit breaker reopening from half-open after failure")
		cb.state = StateOpen
		cb.failures = 0
		cb.successes = 0
	}
}

// recordSuccess records a successful request
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Reset failure count on success in closed state
	if cb.state == StateClosed {
		cb.failures = 0
		return
	}

	// In half-open state, count successes
	if cb.state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.log.WithField("successes", cb.successes).Info("Circuit breaker closing after successful recovery")
			cb.state = StateClosed
			cb.failures = 0
			cb.successes = 0
		}
	}
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns current circuit breaker statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"node_id":            cb.nodeID,
		"state":              cb.state.String(),
		"failures":           cb.failures,
		"successes":          cb.successes,
		"failure_threshold":  cb.failureThreshold,
		"success_threshold":  cb.successThreshold,
		"timeout_seconds":    cb.timeout.Seconds(),
		"last_failure_time":  cb.lastFailureTime.Unix(),
		"time_until_retry":   cb.getTimeUntilRetry().Seconds(),
	}
}

// getTimeUntilRetry calculates time until next retry attempt
func (cb *CircuitBreaker) getTimeUntilRetry() time.Duration {
	if cb.state != StateOpen {
		return 0
	}

	elapsed := time.Since(cb.lastFailureTime)
	remaining := cb.timeout - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.log.Info("Circuit breaker manually reset")
	cb.state = StateClosed
	cb.failures = 0
	cb.successes = 0
}

// CircuitBreakerManager manages circuit breakers for multiple nodes
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	mu       sync.RWMutex
	// default configuration
	failureThreshold int
	successThreshold int
	timeout          time.Duration
	log              *logrus.Entry
}

// NewCircuitBreakerManager creates a new circuit breaker manager
func NewCircuitBreakerManager(failureThreshold, successThreshold int, timeout time.Duration) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers:         make(map[string]*CircuitBreaker),
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		timeout:          timeout,
		log:              logrus.WithField("component", "circuit_breaker_manager"),
	}
}

// GetBreaker gets or creates a circuit breaker for a node
func (cbm *CircuitBreakerManager) GetBreaker(nodeID string) *CircuitBreaker {
	cbm.mu.RLock()
	breaker, exists := cbm.breakers[nodeID]
	cbm.mu.RUnlock()

	if exists {
		return breaker
	}

	// Create new breaker
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	// Double-check after acquiring write lock
	breaker, exists = cbm.breakers[nodeID]
	if exists {
		return breaker
	}

	breaker = NewCircuitBreaker(nodeID, cbm.failureThreshold, cbm.successThreshold, cbm.timeout)
	cbm.breakers[nodeID] = breaker
	cbm.log.WithField("node_id", nodeID).Debug("Created new circuit breaker for node")

	return breaker
}

// RemoveBreaker removes a circuit breaker for a node
func (cbm *CircuitBreakerManager) RemoveBreaker(nodeID string) {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	delete(cbm.breakers, nodeID)
	cbm.log.WithField("node_id", nodeID).Debug("Removed circuit breaker for node")
}

// GetAllStats returns statistics for all circuit breakers
func (cbm *CircuitBreakerManager) GetAllStats() map[string]interface{} {
	cbm.mu.RLock()
	defer cbm.mu.RUnlock()

	stats := make(map[string]interface{})
	for nodeID, breaker := range cbm.breakers {
		stats[nodeID] = breaker.GetStats()
	}

	return stats
}

// ResetAll resets all circuit breakers
func (cbm *CircuitBreakerManager) ResetAll() {
	cbm.mu.RLock()
	defer cbm.mu.RUnlock()

	for _, breaker := range cbm.breakers {
		breaker.Reset()
	}

	cbm.log.Info("Reset all circuit breakers")
}
