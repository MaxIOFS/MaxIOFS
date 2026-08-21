package bgwork

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Shutdown closes the stores right after stopping the components, and Pebble
// panics on use after close, so Stop must not return while a goroutine is still
// running.
func TestBgWorker_StopWaitsForTheGoroutine(t *testing.T) {
	var w Worker
	var mu sync.Mutex
	finished := false

	w.Spawn(func() {
		<-w.Stopped()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		finished = true
		mu.Unlock()
	})

	w.Stop()

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, finished, "Stop returned while the worker was still running")
}

// Two shutdown paths can reach the same component.
func TestBgWorker_StopIsIdempotentUnderConcurrency(t *testing.T) {
	var w Worker
	w.Spawn(func() { <-w.Stopped() })

	require.NotPanics(t, func() {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); w.Stop() }()
		}
		wg.Wait()
	})
}

// A goroutine started while the shutdown is already waiting would be missed by
// the WaitGroup, so spawn refuses instead.
func TestBgWorker_SpawnAfterStopIsRefused(t *testing.T) {
	var w Worker
	w.Stop()

	ran := make(chan struct{})
	started := w.Spawn(func() { close(ran) })

	assert.False(t, started, "spawn accepted work after the component had stopped")
	select {
	case <-ran:
		t.Fatal("the refused work ran anyway")
	case <-time.After(50 * time.Millisecond):
	}
}

// The zero value has to be usable: components embed this without a constructor,
// and a nil channel would block every loop forever and panic on close.
func TestBgWorker_ZeroValueIsUsable(t *testing.T) {
	var w Worker

	require.NotNil(t, w.Stopped())
	require.NotPanics(t, func() { w.Stop() })

	select {
	case <-w.Stopped():
	default:
		t.Fatal("the stop signal never fired on a zero-value worker")
	}
}
