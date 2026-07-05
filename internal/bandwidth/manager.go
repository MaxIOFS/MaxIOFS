// Package bandwidth provides per-tenant aggregate transfer throttling.
//
// A tenant's configured cap (bytes/second, combined upload + download) is
// enforced by a single shared token-bucket rate limiter per tenant, so all of
// that tenant's concurrent transfers draw from one budget. Throttling slows
// transfers (io.Reader.Read blocks until tokens are available); it never rejects
// a request, so legitimate bursts are smoothed rather than failed.
package bandwidth

import (
	"context"
	"io"
	"sync"

	"golang.org/x/time/rate"
)

// throttleChunk bounds how many bytes each Read delivers before waiting, so a
// single WaitN never exceeds the limiter burst and throttling stays smooth.
const throttleChunk = 32 * 1024

// Manager holds one shared rate limiter per tenant. Safe for concurrent use.
type Manager struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter // tenantID -> shared limiter
	rates    map[string]int64         // tenantID -> configured bytes/sec (change detection)
}

// NewManager creates an empty bandwidth manager.
func NewManager() *Manager {
	return &Manager{
		limiters: make(map[string]*rate.Limiter),
		rates:    make(map[string]int64),
	}
}

// burstFor returns the token-bucket burst for a given rate: at least one chunk
// so WaitN(chunk) never fails, and otherwise one second of data to allow a
// short initial burst up to the configured rate.
func burstFor(bytesPerSec int64) int {
	if bytesPerSec < throttleChunk {
		return throttleChunk
	}
	return int(bytesPerSec)
}

// Limiter returns the shared limiter for a tenant given its current cap in
// bytes/sec. Returns nil when there is no throttling to apply (no tenant or
// unlimited), so callers skip wrapping. When the cap changes, the existing
// limiter's rate is updated in place (hot update) so new transfers — and any
// in-flight transfer that shares this limiter — pick up the new rate.
func (m *Manager) Limiter(tenantID string, bytesPerSec int64) *rate.Limiter {
	if tenantID == "" || bytesPerSec <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	lim, ok := m.limiters[tenantID]
	if !ok {
		lim = rate.NewLimiter(rate.Limit(bytesPerSec), burstFor(bytesPerSec))
		m.limiters[tenantID] = lim
		m.rates[tenantID] = bytesPerSec
		return lim
	}
	if m.rates[tenantID] != bytesPerSec {
		lim.SetLimit(rate.Limit(bytesPerSec))
		lim.SetBurst(burstFor(bytesPerSec))
		m.rates[tenantID] = bytesPerSec
	}
	return lim
}

// Remove drops a tenant's limiter (e.g. on tenant deletion). Optional — limiters
// are tiny and few, so this is only housekeeping.
func (m *Manager) Remove(tenantID string) {
	m.mu.Lock()
	delete(m.limiters, tenantID)
	delete(m.rates, tenantID)
	m.mu.Unlock()
}

// ThrottleReader wraps r so bytes are delivered no faster than the limiter
// allows. Returns r unchanged when limiter is nil (no throttling). The context
// cancels the wait if the client disconnects.
func ThrottleReader(ctx context.Context, r io.Reader, limiter *rate.Limiter) io.Reader {
	if limiter == nil || r == nil {
		return r
	}
	return &throttledReader{r: r, limiter: limiter, ctx: ctx}
}

type throttledReader struct {
	r       io.Reader
	limiter *rate.Limiter
	ctx     context.Context
}

func (t *throttledReader) Read(p []byte) (int, error) {
	if len(p) > throttleChunk {
		p = p[:throttleChunk]
	}
	n, err := t.r.Read(p)
	if n > 0 {
		// Spend n tokens (bytes); blocks until the tenant budget allows them.
		if werr := t.limiter.WaitN(t.ctx, n); werr != nil {
			return n, werr
		}
	}
	return n, err
}
