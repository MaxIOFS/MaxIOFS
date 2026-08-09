package transfer

// The distinction these tests exist for: slow is not the same as stalled.
//
// Everything here uses a short stall window so the suite stays fast; the window
// is a parameter precisely so the behaviour can be checked without waiting a
// real minute.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDo_SlowButProgressingIsNotCut is the case a total timeout got wrong: a
// transfer that takes far longer than the window but never stops moving.
//
// The server trickles for well over the stall window, in steps shorter than it.
// A total deadline of the same length would have killed this; progress must
// not.
func TestDo_SlowButProgressingIsNotCut(t *testing.T) {
	const (
		stall = 200 * time.Millisecond
		steps = 12
		gap   = 60 * time.Millisecond // < stall, so it never falls silent
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		for i := 0; i < steps; i++ {
			time.Sleep(gap)
			_, _ = w.Write([]byte("x"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	started := time.Now()
	resp, err := Do(server.Client(), req, stall)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err,
		"a transfer that keeps moving must not be cut, however long it takes")
	assert.Len(t, body, steps)
	assert.Greater(t, time.Since(started), stall,
		"the test is only meaningful if the transfer outlived the window")
}

// TestDo_StalledIsCut: headers arrive, then the peer goes silent. Without this
// the request would hang for as long as the peer holds the connection open.
func TestDo_StalledIsCut(t *testing.T) {
	const stall = 200 * time.Millisecond

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release // never writes another byte
	}))
	defer server.Close()
	defer close(release)

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := Do(server.Client(), req, stall)
	require.NoError(t, err, "the response begins normally; the silence comes after")
	defer resp.Body.Close()

	started := time.Now()
	_, err = io.ReadAll(resp.Body)
	assert.Error(t, err, "a transfer that has stopped moving must be cut")
	assert.Less(t, time.Since(started), 5*time.Second,
		"and cut promptly, not left to some other deadline")
}

// TestDo_UploadProgressCounts: the watch has to see the request body too, or a
// long upload to a healthy peer would be cancelled while it was working
// perfectly — the response has not started, so nothing else is moving.
func TestDo_UploadProgressCounts(t *testing.T) {
	const stall = 200 * time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// A body that takes well over the window to produce, in steps under it.
	body := &trickleReader{chunks: 12, gap: 60 * time.Millisecond}

	req, err := http.NewRequest(http.MethodPost, server.URL, body)
	require.NoError(t, err)

	resp, err := Do(server.Client(), req, stall)
	require.NoError(t, err, "an upload that keeps moving must not be cut")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestDo_ClosingTheBodyEndsTheWatch: the watchdog is a goroutine, and the only
// thing that releases it is closing the response body.
func TestDo_ClosingTheBodyEndsTheWatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "done")
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := Do(server.Client(), req, time.Second)
	require.NoError(t, err)

	guarded, ok := resp.Body.(*guardedBody)
	require.True(t, ok)

	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	select {
	case <-guarded.watchdog.done:
	case <-time.After(time.Second):
		t.Fatal("closing the response body must release the watchdog")
	}

	// Closing twice must not panic on a re-closed channel.
	_ = resp.Body.Close()
}

// TestDo_ZeroStallDisablesTheWatch keeps the escape hatch honest.
func TestDo_ZeroStallDisablesTheWatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := Do(server.Client(), req, 0)
	require.NoError(t, err)
	defer resp.Body.Close()

	_, ok := resp.Body.(*guardedBody)
	assert.False(t, ok, "no window means no watch and no wrapping")
}

// TestDo_CallerCancellationStillWorks: the watch adds a reason to cancel; it
// must not remove the caller's.
func TestDo_CallerCancellationStillWorks(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := Do(server.Client(), req, time.Minute)
	require.NoError(t, err)
	defer resp.Body.Close()

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err = io.ReadAll(resp.Body)
	assert.Error(t, err, "the caller's cancellation must still reach the request")
}

// TestTransport_WatchesWhatItCarries covers the path the AWS SDK uses, where
// the request is built by the library and the transport is the only hold.
func TestTransport_WatchesWhatItCarries(t *testing.T) {
	const stall = 200 * time.Millisecond

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	}))
	defer server.Close()
	defer close(release)

	client := &http.Client{Transport: &Transport{Stall: stall}}

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	started := time.Now()
	_, err = io.ReadAll(resp.Body)
	assert.Error(t, err)
	assert.Less(t, time.Since(started), 5*time.Second)
}

// trickleReader produces its chunks slowly, standing in for a body that comes
// from a disk or another node rather than from memory.
type trickleReader struct {
	chunks int
	gap    time.Duration
	sent   int
}

func (r *trickleReader) Read(b []byte) (int, error) {
	if r.sent >= r.chunks {
		return 0, io.EOF
	}
	time.Sleep(r.gap)
	r.sent++
	return strings.NewReader("x").Read(b)
}
