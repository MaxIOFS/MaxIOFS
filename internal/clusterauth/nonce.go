package clusterauth

import (
	"sync"
	"time"
)

// NonceCache remembers the nonces seen inside the signature's own time window,
// so a captured request cannot be replayed while its timestamp is still valid.
// Entries older than the window can be forgotten: the timestamp check refuses
// them before the nonce is ever consulted.
type NonceCache struct {
	window time.Duration

	mu     sync.Mutex
	seen   map[string]time.Time
	swept  time.Time
	sweepe time.Duration
}

func NewNonceCache(window time.Duration) *NonceCache {
	return &NonceCache{
		window: window,
		seen:   make(map[string]time.Time),
		swept:  time.Now(),
		sweepe: window,
	}
}

// Observe records a nonce and reports whether it is the first time it is seen.
func (c *NonceCache) Observe(key string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if now.Sub(c.swept) >= c.sweepe {
		for k, at := range c.seen {
			if now.Sub(at) > c.window {
				delete(c.seen, k)
			}
		}
		c.swept = now
	}

	if at, ok := c.seen[key]; ok && now.Sub(at) <= c.window {
		return false
	}
	c.seen[key] = now
	return true
}
