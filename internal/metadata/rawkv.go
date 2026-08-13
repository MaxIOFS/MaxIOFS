package metadata

import "context"

// RawKVStore provides low-level key-value access to the underlying storage engine.
type RawKVStore interface {
	// GetRaw retrieves a value by exact key. Returns ErrNotFound if absent.
	GetRaw(ctx context.Context, key string) ([]byte, error)

	// PutRaw stores a key-value pair.
	PutRaw(ctx context.Context, key string, value []byte) error

	// DeleteRaw removes a key. Returns ErrNotFound if absent.
	DeleteRaw(ctx context.Context, key string) error

	// RawBatch applies a set of writes and deletes atomically.
	// sets is a map of key → value; deletes is a list of keys to remove.
	RawBatch(ctx context.Context, sets map[string][]byte, deletes []string) error

	RawScan(ctx context.Context, prefix, startKey string, fn func(key string, val []byte) bool) error

	// RawGC triggers a garbage-collection pass if the engine supports it.
	// No-op on Pebble (which compacts automatically).
	RawGC() error
}
