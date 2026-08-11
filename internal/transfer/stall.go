// Package transfer bounds HTTP requests that carry object data by PROGRESS
// rather than by elapsed time.
//
// A total deadline is the wrong instrument for a file transfer. http.Client's
// Timeout covers the response body, so any value it holds is a maximum object
// size wearing the clothes of a maximum duration: at 60 seconds an object
// bigger than a minute of bandwidth can never cross, however healthy both ends
// are, and it fails as a timeout — which reads like a network fault instead of
// the size limit it actually is.
//
// Removing the deadline outright trades that for the opposite failure: a peer
// that accepts a connection and then stops responding leaves the request
// hanging forever, holding a goroutine, a connection and whatever the handler
// is waiting on.
//
// What separates the two cases is not how long the transfer has taken but
// whether it is still moving. A 500 GB object that is progressing is healthy no
// matter how many hours it needs; a 1 KB object that has moved no bytes in a
// minute is not, whatever its size. So that is what is measured here: every
// byte read from the request or response body stamps the clock, and only the
// absence of bytes cancels the request.
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
// treated as dead.
//
// It is generous on purpose. The cost of cutting a live transfer is a failed
// object and a retry that begins again from zero, while the cost of waiting
// too long is one held connection — so the balance favours patience. A remote
// end that is merely slow still moves bytes; a minute of total silence is a
// different condition from slowness.
const DefaultStallTimeout = 60 * time.Second

// Do sends req and cancels it if its transfer stops making progress for stall.
//
// Progress is counted in both directions: bytes read from the request body
// while uploading, and bytes read from the response body while downloading.
// The returned response's Body must be closed as usual — that is what releases
// the watchdog, so leaving it open leaks a goroutine exactly as leaving a body
// unclosed already leaks a connection.
//
// A stall of zero or less disables the watchdog and sends the request as-is.
//
// The request body is wrapped, which means it is no longer rewindable, so
// net/http will not retry the request automatically. For a proxy that is the
// correct behaviour anyway: a body that has been streamed to a peer cannot be
// replayed.
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

	// From here the caller owns the response, and closing its body is what ends
	// the watch. Cancelling before that would truncate the very transfer this
	// exists to protect.
	resp.Body = &guardedBody{
		ReadCloser: &progressBody{ReadCloser: resp.Body, watchdog: w},
		watchdog:   w,
		cancel:     cancel,
	}
	return resp, nil
}

// Transport applies the same watch to every request carried by a RoundTripper.
//
// This is for clients that are handed to a library rather than called directly
// — the AWS SDK builds and sends its own requests, so the only place to reach
// them is the transport.
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

		// GetBody has to produce a wrapped reader too. net/http calls it to
		// replay a request on a connection that turned out to be stale, and an
		// unwrapped replay stops stamping the clock — so a long upload retried
		// that way looked silent and was cancelled while it was flowing.
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
