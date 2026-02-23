package metadata

import (
	"context"
	"fmt"

	badger "github.com/dgraph-io/badger/v4"
)

// ==================== RawKVStore implementation for BadgerStore ====================

// RawBatch applies writes and deletes atomically in a single BadgerDB transaction.
func (s *BadgerStore) RawBatch(ctx context.Context, sets map[string][]byte, deletes []string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		for k, v := range sets {
			if err := txn.Set([]byte(k), v); err != nil {
				return fmt.Errorf("batch set %q: %w", k, err)
			}
		}
		for _, k := range deletes {
			if err := txn.Delete([]byte(k)); err != nil && err != badger.ErrKeyNotFound {
				return fmt.Errorf("batch delete %q: %w", k, err)
			}
		}
		return nil
	})
}

// RawScan iterates all keys with the given prefix starting from startKey.
// fn receives copies; returning false stops the scan.
func (s *BadgerStore) RawScan(ctx context.Context, prefix, startKey string, fn func(key string, val []byte) bool) error {
	return s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(prefix)
		it := txn.NewIterator(opts)
		defer it.Close()

		var seek []byte
		if startKey != "" && startKey >= prefix {
			seek = []byte(startKey)
		} else {
			seek = []byte(prefix)
		}

		for it.Seek(seek); it.ValidForPrefix([]byte(prefix)); it.Next() {
			item := it.Item()
			keyCopy := string(item.KeyCopy(nil))
			var valCopy []byte
			err := item.Value(func(val []byte) error {
				valCopy = make([]byte, len(val))
				copy(valCopy, val)
				return nil
			})
			if err != nil {
				return err
			}
			if !fn(keyCopy, valCopy) {
				break
			}
		}
		return nil
	})
}

// RawGC runs BadgerDB value-log garbage collection.
func (s *BadgerStore) RawGC() error {
	return s.db.RunValueLogGC(0.5)
}

// compile-time interface check
var _ RawKVStore = (*BadgerStore)(nil)
