package metadata

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	pebblev1 "github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/v2"
	"github.com/sirupsen/logrus"
)

// PebbleV2SentinelFile is written inside the metadata directory after a
// successful Pebble v2 open (or after a v1→v2 migration).  Its presence tells
// MigrateFromPebbleV1IfNeeded that the directory is already in v2 format.
const PebbleV2SentinelFile = "PEBBLE_FORMAT_V2"

// MigrateFromPebbleV1IfNeeded checks whether the metadata directory contains a
// Pebble v1 database (identified by the absence of PebbleV2SentinelFile) and,
// if so, migrates every key-value pair to a new Pebble v2 database.
//
// Migration flow:
//  1. If metadata/ does not exist  → fresh install, nothing to do.
//  2. If metadata/PEBBLE_FORMAT_V2 exists → already v2, nothing to do.
//  3. Otherwise metadata/ is assumed to be v1 format:
//     a. Open v1 DB (read-only).
//     b. Write all k/v pairs into metadata_pebblev2/ (v2 format).
//     c. Rename metadata/ → metadata_pebblev1_backup_{ts}/
//     d. Rename metadata_pebblev2/ → metadata/
//     e. Write PEBBLE_FORMAT_V2 sentinel.
//
// Recovery: if a previous run renamed metadata/ but crashed before renaming
// metadata_pebblev2/, the orphaned tmp directory is promoted on the next start.
func MigrateFromPebbleV1IfNeeded(dataDir string, logger *logrus.Logger) error {
	metaDir := filepath.Join(dataDir, "metadata")
	sentinelPath := filepath.Join(metaDir, PebbleV2SentinelFile)
	v2TmpDir := filepath.Join(dataDir, "metadata_pebblev2")

	// ── Recovery path ─────────────────────────────────────────────────────────
	// A previous run may have renamed metadata/ → backup but then crashed before
	// renaming metadata_pebblev2/ → metadata/.
	if _, err := os.Stat(v2TmpDir); err == nil {
		if _, err := os.Stat(metaDir); os.IsNotExist(err) {
			logger.Info("Recovering incomplete Pebble v1→v2 migration: promoting tmp directory…")
			if err := os.Rename(v2TmpDir, metaDir); err != nil {
				return fmt.Errorf("failed to recover Pebble v2 migration: %w", err)
			}
			_ = os.WriteFile(filepath.Join(metaDir, PebbleV2SentinelFile), []byte("v2\n"), 0644)
			logger.Info("Pebble v1→v2 migration recovery complete")
			return nil
		}
		// Both metadata/ and metadata_pebblev2/ exist → leftover from a failed
		// attempt where v1 data is still intact. Remove the partial tmp dir.
		_ = os.RemoveAll(v2TmpDir)
	}

	// ── Skip conditions ───────────────────────────────────────────────────────
	if _, err := os.Stat(metaDir); os.IsNotExist(err) {
		return nil // fresh install — NewPebbleStore will create a v2 DB
	}
	if _, err := os.Stat(sentinelPath); err == nil {
		return nil // already Pebble v2
	}

	// ── Migrate ───────────────────────────────────────────────────────────────
	logger.Info("Pebble v1 metadata format detected; migrating to v2 (this may take a moment)…")

	migrated, err := runPebbleV1toV2Migration(metaDir, v2TmpDir, logger)
	if err != nil {
		_ = os.RemoveAll(v2TmpDir)
		return fmt.Errorf("Pebble v1→v2 migration failed after %d keys: %w", migrated, err)
	}

	// ── Swap directories ──────────────────────────────────────────────────────
	backupDir := filepath.Join(dataDir,
		fmt.Sprintf("metadata_pebblev1_backup_%s", time.Now().Format("20060102_150405")))

	if err := os.Rename(metaDir, backupDir); err != nil {
		_ = os.RemoveAll(v2TmpDir)
		return fmt.Errorf("failed to rename v1 metadata directory: %w", err)
	}
	if err := os.Rename(v2TmpDir, metaDir); err != nil {
		// Best-effort restore
		_ = os.Rename(backupDir, metaDir)
		return fmt.Errorf("failed to promote v2 metadata directory: %w", err)
	}

	// Write sentinel so future starts skip this check
	_ = os.WriteFile(filepath.Join(metaDir, PebbleV2SentinelFile), []byte("v2\n"), 0644)

	logger.WithFields(logrus.Fields{
		"migrated_keys": migrated,
		"backup_dir":    backupDir,
	}).Info("Pebble v1→v2 migration complete")

	return nil
}

// runPebbleV1toV2Migration reads every key from the v1 database and writes it
// into a new v2 database at v2Dir.  Returns the number of keys migrated.
func runPebbleV1toV2Migration(v1Dir, v2Dir string, logger *logrus.Logger) (int64, error) {
	// ── Open v1 (read-only) ───────────────────────────────────────────────────
	v1db, err := pebblev1.Open(v1Dir, &pebblev1.Options{
		ReadOnly: true,
		Logger:   &pebbleLogger{logger: logger}, // satisfies pebblev1.Logger (Infof+Fatalf)
	})
	if err != nil {
		return 0, fmt.Errorf("failed to open Pebble v1 database: %w", err)
	}
	defer v1db.Close() //nolint:errcheck

	// ── Open v2 (write destination) ───────────────────────────────────────────
	if err := os.MkdirAll(v2Dir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create v2 migration directory: %w", err)
	}

	v2cache := pebble.NewCache(256 << 20)
	defer v2cache.Unref()

	v2db, err := pebble.Open(v2Dir, &pebble.Options{
		Cache:  v2cache,
		Levels: [7]pebble.LevelOptions{},
		Logger: &pebbleLogger{logger: logger},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to open Pebble v2 migration database: %w", err)
	}
	defer v2db.Close() //nolint:errcheck

	// ── Iterate v1 → write to v2 in batches ──────────────────────────────────
	iter, err := v1db.NewIter(nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create v1 iterator: %w", err)
	}
	defer iter.Close() //nolint:errcheck

	const batchSize = 10_000
	var totalKeys int64
	batch := v2db.NewBatch()

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		val := iter.Value()

		keyCopy := make([]byte, len(key))
		copy(keyCopy, key)
		valCopy := make([]byte, len(val))
		copy(valCopy, val)

		if err := batch.Set(keyCopy, valCopy, nil); err != nil {
			batch.Close() //nolint:errcheck
			return totalKeys, fmt.Errorf("failed to stage key in v2 batch: %w", err)
		}
		totalKeys++

		if totalKeys%batchSize == 0 {
			if err := batch.Commit(pebble.NoSync); err != nil {
				batch.Close() //nolint:errcheck
				return totalKeys, fmt.Errorf("failed to commit v2 batch at key %d: %w", totalKeys, err)
			}
			batch.Close() //nolint:errcheck
			batch = v2db.NewBatch()
			logger.WithField("keys_migrated", totalKeys).Info("Pebble v1→v2 migration progress")
		}
	}

	if err := iter.Error(); err != nil {
		batch.Close() //nolint:errcheck
		return totalKeys, fmt.Errorf("v1 iterator error: %w", err)
	}

	// Commit final batch
	if err := batch.Commit(pebble.Sync); err != nil {
		batch.Close() //nolint:errcheck
		return totalKeys, fmt.Errorf("failed to commit final v2 batch: %w", err)
	}
	batch.Close() //nolint:errcheck

	if err := v2db.Flush(); err != nil {
		return totalKeys, fmt.Errorf("failed to flush v2 database after migration: %w", err)
	}

	return totalKeys, nil
}
