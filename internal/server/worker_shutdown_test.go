package server

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Shutdown closes the metadata store, and Pebble panics when it is used after
// close. So every background worker has to be finished before that happens —
// waiting for them is not tidiness, it is what keeps a shutdown from taking the
// process down with it.
func TestGoWorker_ShutdownWaitsForWorkersToReturn(t *testing.T) {
	s := &Server{}

	var finished atomic.Bool
	release := make(chan struct{})

	s.goWorker("slow worker", func() {
		<-release
		finished.Store(true)
	})

	waited := make(chan struct{})
	go func() {
		s.workers.Wait()
		close(waited)
	}()

	select {
	case <-waited:
		t.Fatal("the wait returned while a worker was still running")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case <-waited:
		assert.True(t, finished.Load(), "the worker finished before the wait returned")
	case <-time.After(5 * time.Second):
		t.Fatal("the wait never returned after the worker finished")
	}
}

// A worker that never stops must not keep the process alive for ever; the wait
// is bounded and the stores are closed regardless.
func TestGoWorker_TracksEveryWorker(t *testing.T) {
	s := &Server{}
	var started atomic.Int32
	release := make(chan struct{})

	for i := 0; i < 5; i++ {
		s.goWorker("worker", func() {
			started.Add(1)
			<-release
		})
	}

	require.Eventually(t, func() bool { return started.Load() == 5 },
		2*time.Second, 10*time.Millisecond)

	close(release)
	s.workers.Wait() // returns only once all five have returned
}
