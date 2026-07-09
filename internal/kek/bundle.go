package kek

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

// Recovery bundle: an export of every KEK version, encrypted under an admin
// passphrase, meant to be stored OUTSIDE the system. It is the disaster-
// recovery path for the "database lost, filesystem intact" scenario — with
// the bundle (plus its passphrase) every envelope-encrypted object on disk
// remains decryptable.
//
// File format (JSON): header in the clear (format, KDF parameters, salt,
// nonce) + AES-256-GCM ciphertext of the key list. The encryption key is
// derived from the passphrase with PBKDF2-SHA256.

const (
	bundleFormat     = "maxiofs-kek-bundle-v1"
	bundleIterations = 310000 // OWASP 2023 minimum for PBKDF2-SHA256
	// MinBundlePassphraseLen is the minimum accepted passphrase length.
	MinBundlePassphraseLen = 8

	settingBundleDownloadedAt = "encryption.recovery_bundle_downloaded_at"
)

// KeyRecord is one KEK version inside a recovery bundle.
type KeyRecord struct {
	Version   int    `json:"version"`
	KeyHex    string `json:"key_hex"`
	IsCurrent bool   `json:"is_current"`
}

// bundleFile is the on-disk JSON structure of a recovery bundle.
type bundleFile struct {
	Format     string `json:"format"`
	CreatedAt  int64  `json:"created_at"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// ExportBundle serialises every KEK version encrypted under the passphrase.
func (s *Store) ExportBundle(passphrase string) ([]byte, error) {
	if len(passphrase) < MinBundlePassphraseLen {
		return nil, fmt.Errorf("passphrase must be at least %d characters", MinBundlePassphraseLen)
	}

	s.mu.RLock()
	records := make([]KeyRecord, 0, len(s.keys))
	for version, key := range s.keys {
		records = append(records, KeyRecord{
			Version:   version,
			KeyHex:    hex.EncodeToString(key),
			IsCurrent: version == s.current,
		})
	}
	s.mu.RUnlock()

	if len(records) == 0 {
		return nil, fmt.Errorf("no encryption keys to export")
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Version < records[j].Version })

	plaintext, err := json.Marshal(records)
	if err != nil {
		return nil, fmt.Errorf("failed to serialise keys: %w", err)
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	derivedKey := pbkdf2.Key([]byte(passphrase), salt, bundleIterations, 32, sha256.New)
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	bundle := bundleFile{
		Format:     bundleFormat,
		CreatedAt:  time.Now().Unix(),
		KDF:        "pbkdf2-sha256",
		Iterations: bundleIterations,
		Salt:       hex.EncodeToString(salt),
		Nonce:      hex.EncodeToString(nonce),
		Ciphertext: hex.EncodeToString(ciphertext),
	}
	return json.MarshalIndent(bundle, "", "  ")
}

// DecryptBundle opens a recovery bundle with the passphrase and returns the
// key records it contains. Used by the offline recovery tooling.
func DecryptBundle(data []byte, passphrase string) ([]KeyRecord, error) {
	var bundle bundleFile
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("not a valid recovery bundle: %w", err)
	}
	if bundle.Format != bundleFormat {
		return nil, fmt.Errorf("unsupported bundle format %q", bundle.Format)
	}

	salt, err := hex.DecodeString(bundle.Salt)
	if err != nil {
		return nil, fmt.Errorf("corrupt bundle salt: %w", err)
	}
	nonce, err := hex.DecodeString(bundle.Nonce)
	if err != nil {
		return nil, fmt.Errorf("corrupt bundle nonce: %w", err)
	}
	ciphertext, err := hex.DecodeString(bundle.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("corrupt bundle ciphertext: %w", err)
	}

	iterations := bundle.Iterations
	if iterations < bundleIterations {
		iterations = bundleIterations
	}
	derivedKey := pbkdf2.Key([]byte(passphrase), salt, iterations, 32, sha256.New)

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("wrong passphrase or corrupt bundle")
	}

	var records []KeyRecord
	if err := json.Unmarshal(plaintext, &records); err != nil {
		return nil, fmt.Errorf("corrupt bundle contents: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("bundle contains no keys")
	}
	for _, r := range records {
		if _, err := decodeKeyHex(r.KeyHex); err != nil {
			return nil, fmt.Errorf("bundle key version %d is corrupt: %w", r.Version, err)
		}
	}
	return records, nil
}

// MarkBundleDownloaded records (in system_settings) that the admin has
// downloaded the recovery bundle — the console banner disappears after this.
func (s *Store) MarkBundleDownloaded() error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`
		INSERT INTO system_settings (key, value, type, category, description, editable, created_at, updated_at)
		VALUES (?, ?, 'string', 'security', 'Unix timestamp of the last encryption recovery bundle download', 0, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, settingBundleDownloadedAt, fmt.Sprintf("%d", now), now, now)
	return err
}

// resetBundleDownloaded clears the download marker (used after a rotation:
// the previously downloaded bundle lacks the new key, so the banner must
// prompt for a fresh download).
func (s *Store) resetBundleDownloaded() error {
	_, err := s.db.Exec(`DELETE FROM system_settings WHERE key = ?`, settingBundleDownloadedAt)
	return err
}

// BundleDownloadedAt returns the Unix timestamp of the last bundle download,
// or 0 if it has never been downloaded.
func (s *Store) BundleDownloadedAt() (int64, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM system_settings WHERE key = ?`, settingBundleDownloadedAt).Scan(&value)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var ts int64
	if _, err := fmt.Sscanf(value, "%d", &ts); err != nil {
		return 0, nil
	}
	return ts, nil
}
