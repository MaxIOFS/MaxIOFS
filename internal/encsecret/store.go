// Package encsecret keeps the secret the server encrypts the credentials it
// stores with: identity provider secrets, replication destination keys and
// share link keys. It lives in the database, as the KEK does, so the same
// secret is used every start whatever the configuration says, and every node
// of a cluster holds the cluster's.
package encsecret

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Reencrypter rewrites, inside tx, the credentials one component stores
// encrypted with from as encrypted with to. A credential encrypted with neither
// is left as it is and named in the result. With from equal to to nothing is
// written: it only names what cannot be decrypted.
type Reencrypter func(ctx context.Context, tx *sql.Tx, from, to string) (unreadable []string, err error)

// Store holds the secret in memory, loaded from the encryption_secret table.
type Store struct {
	db           *sql.DB
	mu           sync.RWMutex
	secret       string
	writeMu      sync.Mutex
	reencrypters []Reencrypter
}

// Bootstrap loads the secret. The first time, it stores configured, or else
// the first non-empty fallback, or else a random secret. Afterwards the
// configuration is not used; a configured secret that differs is reported.
func Bootstrap(db *sql.DB, configured string, fallbacks ...string) (*Store, error) {
	s := &Store{db: db}
	var stored string
	err := db.QueryRow(`SELECT secret FROM encryption_secret WHERE id = 1`).Scan(&stored)
	switch {
	case err == nil:
		s.secret = stored
		if configured != "" && configured != stored {
			logrus.Warn("auth.encryption_secret differs from the encryption secret in the database, which is the one used")
		}
		return s, nil
	case !errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("failed to read the encryption secret: %w", err)
	}

	seed := configured
	for _, f := range fallbacks {
		if seed == "" {
			seed = f
		}
	}
	if seed == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("failed to generate the encryption secret: %w", err)
		}
		seed = hex.EncodeToString(b)
	}
	if _, err := db.Exec(`INSERT INTO encryption_secret (id, secret, updated_at) VALUES (1, ?, ?)`, seed, time.Now().Unix()); err != nil {
		return nil, fmt.Errorf("failed to store the encryption secret: %w", err)
	}
	s.secret = seed
	logrus.Info("Encryption secret for stored credentials persisted in the database")
	return s, nil
}

// SetReencrypters names the components whose credentials Adopt and Check
// cover.
func (s *Store) SetReencrypters(r ...Reencrypter) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.reencrypters = r
}

// Current returns the secret.
func (s *Store) Current() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.secret
}

// Fingerprint identifies the secret without revealing it, so two nodes can
// tell whether they hold the same one.
func (s *Store) Fingerprint() string {
	return Fingerprint(s.Current())
}

// Fingerprint identifies secret without revealing it.
func Fingerprint(secret string) string {
	sum := sha256.Sum256([]byte("maxiofs encryption secret\x00" + secret))
	return hex.EncodeToString(sum[:8])
}

// Adopt replaces the secret with secret, rewriting every stored credential in
// the same transaction, and returns the credentials no secret decrypts.
func (s *Store) Adopt(ctx context.Context, secret string) ([]string, error) {
	if secret == "" {
		return nil, fmt.Errorf("the encryption secret cannot be empty")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	current := s.Current()
	if secret == current {
		return nil, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	var unreadable []string
	for _, r := range s.reencrypters {
		names, err := r(ctx, tx, current, secret)
		if err != nil {
			return nil, fmt.Errorf("failed to re-encrypt stored credentials: %w", err)
		}
		unreadable = append(unreadable, names...)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE encryption_secret SET secret = ?, updated_at = ? WHERE id = 1`,
		secret, time.Now().Unix()); err != nil {
		return nil, fmt.Errorf("failed to store the encryption secret: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.secret = secret
	s.mu.Unlock()
	return unreadable, nil
}

// Check names the stored credentials the secret does not decrypt.
func (s *Store) Check(ctx context.Context) ([]string, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	current := s.Current()
	var unreadable []string
	for _, r := range s.reencrypters {
		names, err := r(ctx, tx, current, current)
		if err != nil {
			return nil, err
		}
		unreadable = append(unreadable, names...)
	}
	return unreadable, nil
}
