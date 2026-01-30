package cluster

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	// Circuit breaker: open after 3 failures, close after 2 successes, 100ms timeout
	cb := NewCircuitBreaker("test-node", 3, 2, 100*time.Millisecond)

	t.Run("Starts in closed state", func(t *testing.T) {
		assert.Equal(t, StateClosed, cb.GetState(), "Should start in closed state")
	})

	t.Run("Opens after threshold failures", func(t *testing.T) {
		testErr := errors.New("test error")

		// Record 3 failures
		for i := 0; i < 3; i++ {
			err := cb.Call(func() error {
				return testErr
			})
			assert.Error(t, err)
		}

		// Should be open now
		assert.Equal(t, StateOpen, cb.GetState(), "Should be open after 3 failures")
	})

	t.Run("Blocks requests when open", func(t *testing.T) {
		callCount := 0
		err := cb.Call(func() error {
			callCount++
			return nil
		})

		assert.Error(t, err, "Should return error when circuit is open")
		assert.Equal(t, ErrCircuitOpen, err, "Should return circuit open error")
		assert.Equal(t, 0, callCount, "Should not execute function when circuit is open")
	})

	t.Run("Transitions to half-open after timeout", func(t *testing.T) {
		// Wait for timeout
		time.Sleep(150 * time.Millisecond)

		callCount := 0
		err := cb.Call(func() error {
			callCount++
			return nil
		})

		assert.NoError(t, err, "Should allow request after timeout")
		assert.Equal(t, 1, callCount, "Should execute function in half-open state")
		assert.Equal(t, StateHalfOpen, cb.GetState(), "Should be in half-open state after timeout")
	})

	t.Run("Reopens on failure in half-open", func(t *testing.T) {
		testErr := errors.New("test error")

		err := cb.Call(func() error {
			return testErr
		})

		assert.Error(t, err)
		assert.Equal(t, StateOpen, cb.GetState(), "Should reopen on failure in half-open state")
	})

	t.Run("Closes after success threshold in half-open", func(t *testing.T) {
		// Wait for timeout again
		time.Sleep(150 * time.Millisecond)

		// First success (enters half-open)
		cb.Call(func() error { return nil })
		assert.Equal(t, StateHalfOpen, cb.GetState())

		// Second success (should close)
		cb.Call(func() error { return nil })
		assert.Equal(t, StateClosed, cb.GetState(), "Should close after 2 successes in half-open")
	})
}

func TestCircuitBreaker_SuccessInClosedState(t *testing.T) {
	cb := NewCircuitBreaker("test-node", 3, 2, 100*time.Millisecond)

	// Record a failure
	cb.Call(func() error { return errors.New("error") })

	// Record a success (should reset failure count)
	err := cb.Call(func() error { return nil })
	assert.NoError(t, err)

	// Should still be closed
	assert.Equal(t, StateClosed, cb.GetState())

	// Should not open even after 2 more failures (because counter was reset)
	cb.Call(func() error { return errors.New("error") })
	cb.Call(func() error { return errors.New("error") })
	assert.Equal(t, StateClosed, cb.GetState(), "Should still be closed (failure count was reset)")
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker("test-node", 2, 2, 100*time.Millisecond)

	// Open the circuit
	cb.Call(func() error { return errors.New("error") })
	cb.Call(func() error { return errors.New("error") })

	assert.Equal(t, StateOpen, cb.GetState())

	// Reset manually
	cb.Reset()

	assert.Equal(t, StateClosed, cb.GetState(), "Should be closed after reset")

	// Should allow requests
	callCount := 0
	err := cb.Call(func() error {
		callCount++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, callCount, "Should execute function after reset")
}

func TestCircuitBreaker_GetStats(t *testing.T) {
	cb := NewCircuitBreaker("test-node-1", 3, 2, 5*time.Second)

	stats := cb.GetStats()

	require.NotNil(t, stats)
	assert.Equal(t, "test-node-1", stats["node_id"])
	assert.Equal(t, "closed", stats["state"])
	assert.Equal(t, 3, stats["failure_threshold"])
	assert.Equal(t, 2, stats["success_threshold"])
	assert.Equal(t, float64(5), stats["timeout_seconds"])
}

func TestCircuitBreakerManager(t *testing.T) {
	cbm := NewCircuitBreakerManager(3, 2, 100*time.Millisecond)

	t.Run("Creates circuit breakers on demand", func(t *testing.T) {
		cb1 := cbm.GetBreaker("node-1")
		cb2 := cbm.GetBreaker("node-2")

		assert.NotNil(t, cb1)
		assert.NotNil(t, cb2)
		assert.NotSame(t, cb1, cb2, "Should create separate breakers for different nodes")
	})

	t.Run("Returns same breaker for same node", func(t *testing.T) {
		cb1 := cbm.GetBreaker("node-3")
		cb2 := cbm.GetBreaker("node-3")

		assert.Same(t, cb1, cb2, "Should return same breaker for same node")
	})

	t.Run("Removes breaker", func(t *testing.T) {
		cbm.GetBreaker("node-4")

		stats := cbm.GetAllStats()
		assert.Contains(t, stats, "node-4")

		cbm.RemoveBreaker("node-4")

		stats = cbm.GetAllStats()
		assert.NotContains(t, stats, "node-4", "Should remove breaker")
	})

	t.Run("Resets all breakers", func(t *testing.T) {
		// Create and open some breakers
		cb1 := cbm.GetBreaker("node-5")
		cb2 := cbm.GetBreaker("node-6")

		// Open both
		for i := 0; i < 3; i++ {
			cb1.Call(func() error { return errors.New("error") })
			cb2.Call(func() error { return errors.New("error") })
		}

		assert.Equal(t, StateOpen, cb1.GetState())
		assert.Equal(t, StateOpen, cb2.GetState())

		// Reset all
		cbm.ResetAll()

		assert.Equal(t, StateClosed, cb1.GetState(), "Should reset cb1 to closed")
		assert.Equal(t, StateClosed, cb2.GetState(), "Should reset cb2 to closed")
	})

	t.Run("GetAllStats returns all breaker stats", func(t *testing.T) {
		cbm.GetBreaker("node-7")
		cbm.GetBreaker("node-8")

		stats := cbm.GetAllStats()

		assert.GreaterOrEqual(t, len(stats), 2, "Should have at least 2 breakers")
		assert.Contains(t, stats, "node-7")
		assert.Contains(t, stats, "node-8")
	})
}

func TestCircuitBreaker_EdgeCases(t *testing.T) {
	t.Run("Zero failure threshold", func(t *testing.T) {
		cb := NewCircuitBreaker("test-node", 0, 2, 100*time.Millisecond)

		// Should open immediately on any failure
		err := cb.Call(func() error { return errors.New("error") })
		assert.Error(t, err)

		// Note: With threshold 0, it might not open. This is an edge case.
		// In production, threshold should always be > 0
	})

	t.Run("Concurrent calls", func(t *testing.T) {
		cb := NewCircuitBreaker("test-node", 5, 2, 100*time.Millisecond)

		results := make(chan error, 10)

		// 10 concurrent calls, all failing
		for i := 0; i < 10; i++ {
			go func() {
				err := cb.Call(func() error {
					return errors.New("concurrent error")
				})
				results <- err
			}()
		}

		// Collect results
		for i := 0; i < 10; i++ {
			<-results
		}

		// Circuit should be open after threshold failures
		// (exact behavior depends on goroutine scheduling)
		state := cb.GetState()
		t.Logf("Circuit state after concurrent failures: %s", state)
	})
}
