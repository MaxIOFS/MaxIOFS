// Package transfer bounds HTTP requests that carry object data by PROGRESS
package transfer

import (
	"context"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultStallTimeout is how long a transfer may move nothing before it is
const DefaultStallTimeout = 60 * time.Second

// Do sends req and cancels it if its transfer stops making progress for stall.
func Do(client *http.Client, req *http.Request, stall time.Duration) (*http.Response, error) {
	if stall <= 0 {
		return client.Do(req)
	}

	ctx, cancel := context.WithCancel(req.Context())
	w := newWatchdog(cancel, stall)

	req = req.WithContext(ctx)
	if req.Body != nil && req.Body != http.NoBody {
		req.Body = &progressBody{ReadCloser: req.Body, watchdog: w}
	}

	resp, err := client.Do(req)
	if err != nil {
		// Nothing will read a body, so the watchdog has to be released here or
		// it outlives the request it was watching.
		w.stop()
		cancel()
		return nil, err
	}

	resp.Body = &guardedBody{
		ReadCloser: &progressBody{ReadCloser: resp.Body, watchdog: w},
		watchdog:   w,
		cancel:     cancel,
	}
	return resp, nil
}

// Transport applies the same watch to every request carried by a RoundTripper.
type Transport struct {
	// Base is the underlying RoundTripper. nil means http.DefaultTransport.
	Base http.RoundTripper
	// Stall is how long a transfer may move nothing. Zero disables the watch.
	Stall time.Duration
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if t.Stall <= 0 {
		return base.RoundTrip(req)
	}

	ctx, cancel := context.WithCancel(req.Context())
	w := newWatchdog(cancel, t.Stall)

	// Cloned rather than mutated: a RoundTripper is not allowed to modify the
	// request it is given.
	outgoing := req.Clone(ctx)
	if outgoing.Body != nil && outgoing.Body != http.NoBody {
		outgoing.Body = &progressBody{ReadCloser: outgoing.Body, watchdog: w}

		if original := outgoing.GetBody; original != nil {
			outgoing.GetBody = func() (io.ReadCloser, error) {
				body, err := original()
				if err != nil {
					return nil, err
				}
				return &progressBody{ReadCloser: body, watchdog: w}, nil
			}
		}
	}

	resp, err := base.RoundTrip(outgoing)
	if err != nil {
		w.stop()
		cancel()
		return nil, err
	}

	resp.Body = &guardedBody{
		ReadCloser: &progressBody{ReadCloser: resp.Body, watchdog: w},
		watchdog:   w,
		cancel:     cancel,
	}
	return resp, nil
}

// watchdog cancels a request once no bytes have moved for the stall window.
type watchdog struct {
	lastNanos atomic.Int64
	cancel    context.CancelFunc
	done      chan struct{}
	once      sync.Once
}

func newWatchdog(cancel context.CancelFunc, stall time.Duration) *watchdog {
	w := &watchdog{cancel: cancel, done: make(chan struct{})}
	// Sending the headers counts as progress: the window measures silence
	// during the transfer, not the time spent establishing it.
	w.mark()
	go w.watch(stall)
	return w
}

func (w *watchdog) mark() {
	w.lastNanos.Store(time.Now().UnixNano())
}

func (w *watchdog) stop() {
	w.once.Do(func() { close(w.done) })
}

// watch polls rather than resetting a timer on every read: a fast transfer
// stamps the clock millions of times, and an atomic store is far cheaper than
// rearming a timer at that rate.
func (w *watchdog) watch(stall time.Duration) {
	interval := stall / 4
	if interval < 25*time.Millisecond {
		interval = 25 * time.Millisecond
	}
	if interval > 5*time.Second {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case now := <-ticker.C:
			if now.Sub(time.Unix(0, w.lastNanos.Load())) >= stall {
				w.cancel()
				return
			}
		}
	}
}

// progressBody stamps the watchdog whenever bytes actually move.
type progressBody struct {
	io.ReadCloser
	watchdog *watchdog
}

func (p *progressBody) Read(b []byte) (int, error) {
	n, err := p.ReadCloser.Read(b)
	if n > 0 {
		p.watchdog.mark()
	}
	return n, err
}

// guardedBody releases the watch when the caller is done with the response.
type guardedBody struct {
	io.ReadCloser
	watchdog *watchdog
	cancel   context.CancelFunc
}

func (g *guardedBody) Close() error {
	err := g.ReadCloser.Close()
	g.watchdog.stop()
	g.cancel()
	return err
}
