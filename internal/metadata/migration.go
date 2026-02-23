package metadata

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/cockroachdb/pebble"
	"github.com/sirupsen/logrus"
)

const (
	migrationBatchSize = 10_000
	badgerKeyRegistry  = "KEYREGISTRY" // file present only in BadgerDB directories
)

// MigrateFromBadgerIfNeeded checks whether a BadgerDB exists at
// {dataDir}/metadata/ and, if so, migrates all its data to Pebble.
//
// Migration is transparent: on success the caller can open a PebbleStore at
// the same dataDir and all data will be available.  If migration fails the
// BadgerDB directory is left untouched so the caller can retry or investigate.
//
// Migration flow:
//  1. Detect KEYREGISTRY file → BadgerDB present
//  2. Open BadgerDB (read-only)
//  3. Open Pebble in metadata_pebble/ (temporary)
//  4. Iterate all BadgerDB keys → write to Pebble in batches of 10 000
//  5. Close both databases
//  6. Rename metadata/ → metadata_badger_backup_{ts}/
//  7. Rename metadata_pebble/ → metadata/
func MigrateFromBadgerIfNeeded(dataDir string, logger *logrus.Logger) error {
	metaDir := filepath.Join(dataDir, "metadata")
	keyRegistry := filepath.Join(metaDir, badgerKeyRegistry)

	// Check whether this is a BadgerDB directory
	if _, err := os.Stat(keyRegistry); os.IsNotExist(err) {
		return nil // nothing to migrate — either fresh install or already on Pebble
	} else if err != nil {
		return fmt.Errorf("failed to check metadata directory: %w", err)
	}

	logger.Info("BadgerDB metadata detected; starting migration to Pebble…")

	pebbleTmpDir := filepath.Join(dataDir, "metadata_pebble")

	// Clean up any leftover temp dir from a previous failed attempt
	if err := os.RemoveAll(pebbleTmpDir); err != nil {
		return fmt.Errorf("failed to clean up previous migration attempt: %w", err)
	}

	migrated, err := runMigration(metaDir, pebbleTmpDir, logger)
	if err != nil {
		// Migration failed — remove the incomplete Pebble directory so the
		// BadgerDB is the only thing at metadata/ and can be retried next start.
		_ = os.RemoveAll(pebbleTmpDir)
		return fmt.Errorf("migration failed after %d keys: %w", migrated, err)
	}

	// Swap directories atomically (best-effort — OS rename is atomic on POSIX;
	// on Windows it may not be fully atomic but is safe enough for this use case)
	backupName := fmt.Sprintf("metadata_badger_backup_%s", time.Now().Format("20060102_150405"))

	// If a previous backup already exists, use a unique name
	backupDir := filepath.Join(dataDir, backupName)
	if _, err := os.Stat(backupDir); err == nil {
		backupDir = filepath.Join(dataDir, backupName+"_2")
	}

	if err := os.Rename(metaDir, backupDir); err != nil {
		_ = os.RemoveAll(pebbleTmpDir)
		return fmt.Errorf("failed to rename BadgerDB directory: %w", err)
	}

	if err := os.Rename(pebbleTmpDir, metaDir); err != nil {
		// Undo: try to restore BadgerDB directory
		_ = os.Rename(backupDir, metaDir)
		return fmt.Errorf("failed to rename Pebble directory: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"migrated_keys": migrated,
		"backup_dir":    backupDir,
	}).Info("Migration to Pebble complete")

	return nil
}

// runMigration performs the actual data copy from BadgerDB to Pebble.
// Returns the number of keys successfully migrated.
func runMigration(badgerDir, pebbleDir string, logger *logrus.Logger) (int64, error) {
	// ── Open BadgerDB (read-only) ──────────────────────────────────────────────
	badgerLogger := newBadgerLogger(logger)
	badgerOpts := badger.DefaultOptions(badgerDir).
		WithLogger(badgerLogger).
		WithNumVersionsToKeep(1)

	bdb, err := badger.Open(badgerOpts)
	if err != nil {
		return 0, fmt.Errorf("failed to open BadgerDB for migration: %w", err)
	}
	defer bdb.Close() //nolint:errcheck

	// ── Open Pebble (write destination) ───────────────────────────────────────
	if err := os.MkdirAll(pebbleDir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create Pebble migration directory: %w", err)
	}

	cache := pebble.NewCache(256 << 20)
	defer cache.Unref()

	pdb, err := pebble.Open(pebbleDir, &pebble.Options{
		Cache:  cache,
		Levels: []pebble.LevelOptions{{Compression: pebble.SnappyCompression}},
		Logger: &pebbleLogger{logger: logger},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to open Pebble for migration: %w", err)
	}
	defer pdb.Close() //nolint:errcheck

	// ── Iterate BadgerDB and write to Pebble in batches ───────────────────────
	var totalKeys int64
	batch := pdb.NewBatch()

	err = bdb.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		opts.PrefetchSize = 256

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			key := item.KeyCopy(nil)

			var writeErr error
			valErr := item.Value(func(val []byte) error {
				valCopy := make([]byte, len(val))
				copy(valCopy, val)

				writeErr = batch.Set(key, valCopy, nil)
				return nil
			})
			if valErr != nil {
				return fmt.Errorf("failed to read BadgerDB value for key %q: %w", key, valErr)
			}
			if writeErr != nil {
				return fmt.Errorf("failed to write key %q to Pebble batch: %w", key, writeErr)
			}

			totalKeys++

			if totalKeys%migrationBatchSize == 0 {
				if err := batch.Commit(pebble.NoSync); err != nil {
					return fmt.Errorf("failed to commit Pebble batch at key %d: %w", totalKeys, err)
				}
				batch.Close() //nolint:errcheck
				batch = pdb.NewBatch()
				logger.WithField("keys_migrated", totalKeys).Info("Migration progress")
			}
		}
		return nil
	})
	if err != nil {
		batch.Close() //nolint:errcheck
		return totalKeys, err
	}

	// Commit the final (possibly partial) batch
	if err := batch.Commit(pebble.Sync); err != nil {
		batch.Close() //nolint:errcheck
		return totalKeys, fmt.Errorf("failed to commit final Pebble batch: %w", err)
	}
	batch.Close() //nolint:errcheck

	// Force a sync so all data is durable before we swap directories
	if err := pdb.Flush(); err != nil {
		return totalKeys, fmt.Errorf("failed to flush Pebble after migration: %w", err)
	}

	return totalKeys, nil
}
