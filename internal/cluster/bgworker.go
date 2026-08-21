package cluster

import "sync"

// bgWorker gives a component a stop signal and a way to wait for the goroutines
// it started. Embed it and start every goroutine through spawn.
//
// The zero value works, so a component cannot be built half-initialised: there
// is no constructor to remember, and a struct that embeds this needs nothing
// added to its own constructor.
//
// spawn and Stop share one lock, so a goroutine cannot be registered while the
// shutdown is already waiting: spawn refuses after Stop instead of racing with
// it. Callers therefore never need to check whether the component is stopping.
//
// The channel is reachable only as a receive-only value, so the only way to
// signal a stop is Stop itself: the bare close that used to sit in each
// manager, never waiting and panicking on a second shutdown, no longer
// compiles.
type bgWorker struct {
	mu     sync.Mutex
	signal chan struct{}
	closed bool
	wg     sync.WaitGroup
}

// chanLocked returns the stop channel, creating it on first use.
// The caller must hold mu.
func (b *bgWorker) chanLocked() chan struct{} {
	if b.signal == nil {
		b.signal = make(chan struct{})
	}
	return b.signal
}

// stopped is the signal a component's loops select on.
func (b *bgWorker) stopped() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chanLocked()
}

// spawn runs fn in a goroutine that Stop will wait for. It reports whether the
// goroutine was started; false means the component is already stopping.
func (b *bgWorker) spawn(fn func()) bool {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return false
	}
	b.wg.Add(1)
	b.mu.Unlock()

	go func() {
		defer b.wg.Done()
		fn()
	}()
	return true
}

// Stop signals the goroutines and waits for them. Safe to call more than once
// and from several shutdown paths at the same time.
func (b *bgWorker) Stop() {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.chanLocked())
	}
	b.mu.Unlock()

	b.wg.Wait()
}
