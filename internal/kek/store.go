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
}

// Store is the SQLite-backed KEK provider. All versions are loaded into
// memory at bootstrap; the table is tiny (one row per rotation).
type Store struct {
	db      *sql.DB
	mu      sync.RWMutex
	keys    map[int][]byte
	current int
}

// Bootstrap resolves the KEK (see package doc for the priority order) and
// returns a Store with every key version loaded.
func Bootstrap(db *sql.DB, configKeyHex string) (*Store, error) {
	s := &Store{db: db, keys: make(map[int][]byte)}

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
		if err := s.insertKey(1, key, true); err != nil {
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
	if err := s.insertKey(1, key, true); err != nil {
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

// loadAll reads every key version from the DB into memory.
func (s *Store) loadAll() error {
	rows, err := s.db.Query(`SELECT version, key_hex, is_current FROM encryption_keys`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var version, isCurrent int
		var keyHex string
		if err := rows.Scan(&version, &keyHex, &isCurrent); err != nil {
			return err
		}
		key, err := decodeKeyHex(keyHex)
		if err != nil {
			return fmt.Errorf("encryption key version %d is corrupt: %w", version, err)
		}
		s.keys[version] = key
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
func (s *Store) insertKey(version int, key []byte, current bool) error {
	isCurrent := 0
	if current {
		isCurrent = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO encryption_keys (version, key_hex, is_current, created_at) VALUES (?, ?, ?, ?)`,
		version, hex.EncodeToString(key), isCurrent, time.Now().Unix(),
	)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[version] = key
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

func (p *ephemeralProvider) KEKByVersion(version int) ([]byte, error) {
	if version != 1 {
		return nil, fmt.Errorf("encryption key version %d not found", version)
	}
	return p.key, nil
}
