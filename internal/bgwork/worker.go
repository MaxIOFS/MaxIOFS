// Package bgwork owns background goroutines for a component.
package bgwork

import "sync"

// Usable at its zero value. Spawn refuses once Stop has begun.
type Worker struct {
	mu     sync.Mutex
	signal chan struct{}
	closed bool
	wg     sync.WaitGroup
}

// chanLocked requires mu.
func (w *Worker) chanLocked() chan struct{} {
	if w.signal == nil {
		w.signal = make(chan struct{})
	}
	return w.signal
}

func (w *Worker) Stopped() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.chanLocked()
}

// Reports false when fn was not run because Stop had already begun.
func (w *Worker) Spawn(fn func()) bool {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return false
	}
	w.wg.Add(1)
	w.mu.Unlock()

	go func() {
		defer w.wg.Done()
		fn()
	}()
	return true
}

// Stop is idempotent and safe from concurrent callers.
func (w *Worker) Stop() {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.chanLocked())
	}
	w.mu.Unlock()

	w.wg.Wait()
}
