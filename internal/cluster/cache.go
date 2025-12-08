package cluster

import (
	"sync"
	"time"
)

// BucketLocationCache stores bucket-to-node mappings with TTL
type BucketLocationCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
}

type cacheEntry struct {
	nodeID    string
	expiresAt time.Time
}

// NewBucketLocationCache creates a new cache with the given TTL
func NewBucketLocationCache(ttl time.Duration) *BucketLocationCache {
	cache := &BucketLocationCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
	}

	// Start background cleanup goroutine
	go cache.cleanupExpired()

	return cache
}

// Get retrieves a node ID for a bucket from the cache
// Returns empty string if not found or expired
func (c *BucketLocationCache) Get(bucket string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[bucket]
	if !exists {
		return ""
	}

	// Check if expired
	if time.Now().After(entry.expiresAt) {
		return ""
	}

	return entry.nodeID
}

// Set stores a bucket-to-node mapping in the cache
func (c *BucketLocationCache) Set(bucket, nodeID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[bucket] = &cacheEntry{
		nodeID:    nodeID,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// Delete removes a bucket from the cache
func (c *BucketLocationCache) Delete(bucket string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, bucket)
}

// Clear removes all entries from the cache
func (c *BucketLocationCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*cacheEntry)
}

// Size returns the number of entries in the cache
func (c *BucketLocationCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

// cleanupExpired periodically removes expired entries
func (c *BucketLocationCache) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for bucket, entry := range c.entries {
			if now.After(entry.expiresAt) {
				delete(c.entries, bucket)
			}
		}
		c.mu.Unlock()
	}
}

// GetStats returns cache statistics
func (c *BucketLocationCache) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	expired := 0
	now := time.Now()
	for _, entry := range c.entries {
		if now.After(entry.expiresAt) {
			expired++
		}
	}

	return map[string]interface{}{
		"total_entries":   len(c.entries),
		"expired_entries": expired,
		"valid_entries":   len(c.entries) - expired,
		"ttl_seconds":     c.ttl.Seconds(),
	}
}
