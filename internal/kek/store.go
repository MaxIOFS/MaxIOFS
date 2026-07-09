// Package kek manages the Key Encryption Key (KEK) used for envelope
// encryption. The KEK lives in the SQLite database (encryption_keys table,
// created by migration 16) and wraps each object's per-object DEK.
//
// Bootstrap resolves the KEK on startup with the following priority:
//  1. A current KEK already exists in the DB → use it.
//  2. No KEK in the DB but config.yaml provides storage.encryption_key →
//     seed the DB with it as KEK version 1 (migration path: existing
//     objects were encrypted directly with this key, so it MUST become
//     KEK-v1 or they would stop decrypting).
//  3. Neither → generate a fresh random 32-byte KEK and persist it as
//     version 1 (self-provisioning for deployments started with only
//     --data-dir and no config file).
//
// After bootstrap the DB is the source of truth; the config value is only
// ever a seed.
package kek

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Provider exposes the KEK material needed by the object manager to wrap
// and unwrap per-object DEKs. Implemented by Store (DB-backed) and by the
// ephemeral in-memory provider used in tests.
type Provider interface {
	// CurrentKEK returns the key used to wrap DEKs for new objects.
	CurrentKEK() (key []byte, version int)
	// KEKByVersion returns the key for a specific version (needed to
	// unwrap DEKs of objects written before a rotation).
	KEKByVersion(version int) ([]byte, error)
	// IsClusterShared reports whether a KEK version is shared across all
	// cluster nodes — objects wrapped with a shared version can be
	// replicated as ciphertext (the destination can unwrap the DEK).
	IsClusterShared(version int) bool
}

// Store is the SQLite-backed KEK provider. All versions are loaded into
// memory at bootstrap; the table is tiny (one row per rotation).
type Store struct {
	db            *sql.DB
	mu            sync.RWMutex
	keys          map[int][]byte
	clusterShared map[int]bool
	current       int
}

// Bootstrap resolves the KEK (see package doc for the priority order) and
// returns a Store with every key version loaded.
func Bootstrap(db *sql.DB, configKeyHex string) (*Store, error) {
	s := &Store{db: db, keys: make(map[int][]byte), clusterShared: make(map[int]bool)}

	if err := s.loadAll(); err != nil {
		return nil, fmt.Errorf("failed to load encryption keys: %w", err)
	}

	// Case 1: KEK already in the DB — the config key is not consulted at all;
	// it only serves as a one-time seed the very first boot.
	if s.current != 0 {
		logrus.WithField("kek_version", s.current).Info("Encryption KEK loaded from database")
		return s, nil
	}

	// Case 2: seed from config.yaml (existing deployments with a config key).
	if configKeyHex != "" {
		key, err := decodeKeyHex(configKeyHex)
		if err != nil {
			return nil, fmt.Errorf("invalid storage.encryption_key: %w. Generate a secure key with: openssl rand -hex 32", err)
		}
		if err := s.insertKey(1, key, true, false); err != nil {
			return nil, fmt.Errorf("failed to seed KEK from config: %w", err)
		}
		logrus.Info("✅ Encryption KEK seeded into database from config.yaml (keep the config file as a backup of the key)")
		return s, nil
	}

	// Case 3: self-provision a fresh random KEK.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate KEK: %w", err)
	}
	if err := s.insertKey(1, key, true, false); err != nil {
		return nil, fmt.Errorf("failed to persist generated KEK: %w", err)
	}
	logrus.Info("✅ Encryption KEK generated and persisted in database (download the recovery bundle from Settings and store it outside this system)")
	return s, nil
}

// CurrentKEK returns the current KEK and its version.
func (s *Store) CurrentKEK() ([]byte, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.keys[s.current], s.current
}

// KEKByVersion returns the KEK for a specific version.
func (s *Store) KEKByVersion(version int) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.keys[version]
	if !ok {
		return nil, fmt.Errorf("encryption key version %d not found", version)
	}
	return key, nil
}

// IsClusterShared reports whether a KEK version is shared cluster-wide.
func (s *Store) IsClusterShared(version int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clusterShared[version]
}

// ClusterSharedKeys returns every cluster-shared KEK version (for the join
// package). The current marker reflects the store's current version.
func (s *Store) ClusterSharedKeys() []KeyRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]KeyRecord, 0, len(s.keys))
	for version, key := range s.keys {
		if !s.clusterShared[version] {
			continue
		}
		records = append(records, KeyRecord{
			Version:   version,
			KeyHex:    hex.EncodeToString(key),
			IsCurrent: version == s.current,
		})
	}
	return records
}

// EnsureClusterKey guarantees a cluster-shared KEK exists and is current,
// creating one (next free version) the first time a node joins the cluster.
// Returns all cluster-shared keys ready to be embedded in the join package.
func (s *Store) EnsureClusterKey() ([]KeyRecord, error) {
	if records := s.ClusterSharedKeys(); len(records) > 0 {
		return records, nil
	}

	s.mu.RLock()
	next := 1
	for version := range s.keys {
		if version >= next {
			next = version + 1
		}
	}
	s.mu.RUnlock()

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate cluster KEK: %w", err)
	}
	// Insert non-current first, then move the marker atomically (insertKey
	// alone would leave two rows flagged is_current).
	if err := s.insertKey(next, key, false, true); err != nil {
		return nil, fmt.Errorf("failed to persist cluster KEK: %w", err)
	}
	if err := s.markCurrent(next); err != nil {
		return nil, fmt.Errorf("failed to mark cluster KEK current: %w", err)
	}
	logrus.WithField("kek_version", next).Info("✅ Cluster-shared encryption KEK created (new objects on every node will use it)")
	return s.ClusterSharedKeys(), nil
}

// AdoptClusterKeys merges the cluster-shared keys received in a join package.
// A version that exists locally with different key material is a hard
// conflict (this node's objects reference it) and rejects the join — the
// operator must recover or empty the node first. The record marked current
// becomes this node's current KEK.
func (s *Store) AdoptClusterKeys(records []KeyRecord) error {
	if len(records) == 0 {
		return nil
	}

	// Validate everything before touching the DB.
	type parsedRecord struct {
		version int
		key     []byte
		current bool
	}
	parsed := make([]parsedRecord, 0, len(records))
	currentVersion := 0
	s.mu.RLock()
	for _, r := range records {
		key, err := decodeKeyHex(r.KeyHex)
		if err != nil {
			s.mu.RUnlock()
			return fmt.Errorf("cluster key version %d is malformed: %w", r.Version, err)
		}
		if local, exists := s.keys[r.Version]; exists && hex.EncodeToString(local) != r.KeyHex {
			s.mu.RUnlock()
			return fmt.Errorf("encryption key version %d already exists on this node with different material — "+
				"this node holds objects wrapped with its own key v%d and cannot join without recovery", r.Version, r.Version)
		}
		if r.IsCurrent {
			currentVersion = r.Version
		}
		parsed = append(parsed, parsedRecord{version: r.Version, key: key, current: r.IsCurrent})
	}
	s.mu.RUnlock()
	if currentVersion == 0 {
		return fmt.Errorf("cluster key set has no current version")
	}

	for _, p := range parsed {
		if err := s.upsertClusterKey(p.version, p.key); err != nil {
			return fmt.Errorf("failed to store cluster key v%d: %w", p.version, err)
		}
	}
	if err := s.markCurrent(currentVersion); err != nil {
		return fmt.Errorf("failed to mark cluster key v%d current: %w", currentVersion, err)
	}
	logrus.WithField("kek_version", currentVersion).Info("✅ Cluster encryption KEKs adopted from join package")
	return nil
}

// Rotate creates a fresh KEK as the next free version and makes it current.
// Existing versions are kept: objects wrapped with them stay decryptable and
// the background worker re-wraps their DEKs to the new version over time.
// Old versions are deliberately never deleted — they cost nothing, the
// recovery bundle includes them, and removing one would orphan any file that
// still references it (e.g. sidecar-only objects outside the metadata index).
//
// clusterShared must be true when the node is part of a cluster, so the new
// key can be distributed to peers and keep raw (ciphertext) replication
// working for new objects.
func (s *Store) Rotate(clusterShared bool) (int, error) {
	s.mu.RLock()
	next := 1
	for version := range s.keys {
		if version >= next {
			next = version + 1
		}
	}
	s.mu.RUnlock()

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return 0, fmt.Errorf("failed to generate new KEK: %w", err)
	}
	if err := s.insertKey(next, key, false, clusterShared); err != nil {
		return 0, fmt.Errorf("failed to persist new KEK: %w", err)
	}
	if err := s.markCurrent(next); err != nil {
		return 0, fmt.Errorf("failed to mark new KEK current: %w", err)
	}

	// The existing recovery bundle does not contain the new key — clear the
	// downloaded flag so the console banner prompts a fresh download.
	if err := s.resetBundleDownloaded(); err != nil {
		logrus.WithError(err).Warn("KEK rotated but failed to reset the recovery-bundle flag")
	}

	logrus.WithFields(logrus.Fields{"kek_version": next, "cluster_shared": clusterShared}).
		Info("✅ Encryption KEK rotated — new objects wrap with the new version; the worker re-wraps existing DEKs")
	return next, nil
}

// upsertClusterKey inserts (or re-marks) a key as cluster-shared.
func (s *Store) upsertClusterKey(version int, key []byte) error {
	_, err := s.db.Exec(`
		INSERT INTO encryption_keys (version, key_hex, is_current, created_at, cluster_shared)
		VALUES (?, ?, 0, ?, 1)
		ON CONFLICT(version) DO UPDATE SET cluster_shared = 1
	`, version, hex.EncodeToString(key), time.Now().Unix())
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.keys[version] = key
	s.clusterShared[version] = true
	s.mu.Unlock()
	return nil
}

// markCurrent atomically moves the is_current marker to the given version.
func (s *Store) markCurrent(version int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE encryption_keys SET is_current = 0`); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE encryption_keys SET is_current = 1 WHERE version = ?`, version); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.mu.Lock()
	s.current = version
	s.mu.Unlock()
	return nil
}

// loadAll reads every key version from the DB into memory.
func (s *Store) loadAll() error {
	rows, err := s.db.Query(`SELECT version, key_hex, is_current, cluster_shared FROM encryption_keys`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var version, isCurrent, clusterShared int
		var keyHex string
		if err := rows.Scan(&version, &keyHex, &isCurrent, &clusterShared); err != nil {
			return err
		}
		key, err := decodeKeyHex(keyHex)
		if err != nil {
			return fmt.Errorf("encryption key version %d is corrupt: %w", version, err)
		}
		s.keys[version] = key
		s.clusterShared[version] = clusterShared == 1
		if isCurrent == 1 {
			s.current = version
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Defensive: rows exist but none is marked current (should not happen).
	if len(s.keys) > 0 && s.current == 0 {
		return fmt.Errorf("encryption_keys table has %d key(s) but none is marked current", len(s.keys))
	}
	return nil
}

// insertKey persists a key version and registers it in memory.
func (s *Store) insertKey(version int, key []byte, current, clusterShared bool) error {
	isCurrent := 0
	if current {
		isCurrent = 1
	}
	isShared := 0
	if clusterShared {
		isShared = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO encryption_keys (version, key_hex, is_current, created_at, cluster_shared) VALUES (?, ?, ?, ?, ?)`,
		version, hex.EncodeToString(key), isCurrent, time.Now().Unix(), isShared,
	)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[version] = key
	s.clusterShared[version] = clusterShared
	if current {
		s.current = version
	}
	return nil
}

// decodeKeyHex validates and decodes a 64-hex-character (32-byte) key.
func decodeKeyHex(keyHex string) ([]byte, error) {
	if len(keyHex) != 64 {
		return nil, fmt.Errorf("got %d characters, expected 64 (32 bytes in hex)", len(keyHex))
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("not valid hex: %w", err)
	}
	return key, nil
}

// Ephemeral returns an in-memory Provider with a single random KEK.
// Used by tests and embedded scenarios where no SQLite DB is available.
// Data written with an ephemeral KEK is NOT decryptable after restart.
func Ephemeral() (Provider, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return &ephemeralProvider{key: key}, nil
}

// EphemeralFromKey returns an in-memory Provider using the given 32-byte key
// as KEK version 1. Used by tests that need a deterministic key.
func EphemeralFromKey(key []byte) (Provider, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("KEK must be 32 bytes, got %d", len(key))
	}
	return &ephemeralProvider{key: key}, nil
}

type ephemeralProvider struct {
	key []byte
}

func (p *ephemeralProvider) CurrentKEK() ([]byte, int) { return p.key, 1 }

func (p *ephemeralProvider) IsClusterShared(int) bool { return false }

func (p *ephemeralProvider) KEKByVersion(version int) ([]byte, error) {
	if version != 1 {
		return nil, fmt.Errorf("encryption key version %d not found", version)
	}
	return p.key, nil
}
