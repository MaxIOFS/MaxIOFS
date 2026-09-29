package replication

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sirupsen/logrus"
)

// ErrDecryptionFailed is returned when a stored credential cannot be decrypted or
// produces an implausibly short result (indicating a key rotation or data corruption).
var ErrDecryptionFailed = errors.New("credential decryption failed")

const credentialEncryptionPrefix = "enc1:"

// currentKey is the key key returns; none leaves credentials unencrypted.
func currentKey(key func() string) string {
	if key == nil {
		return ""
	}
	return key()
}

// encryptCredential encrypts a plaintext credential string using AES-256-GCM.
func encryptCredential(plaintext, encryptionKey string) (string, error) {
	if encryptionKey == "" || plaintext == "" {
		return plaintext, nil
	}

	// Derive a 32-byte AES-256 key from the passphrase using SHA-256
	keyBytes := deriveCredentialKey(encryptionKey)

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal appends ciphertext+tag to nonce
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	encoded := base64.StdEncoding.EncodeToString(ciphertext)
	return credentialEncryptionPrefix + encoded, nil
}

// decryptCredential decrypts a credential encrypted by encryptCredential.
func decryptCredential(stored, encryptionKey string) (string, error) {
	if encryptionKey == "" || stored == "" {
		return stored, nil
	}

	// Legacy plaintext value — not yet encrypted
	if len(stored) < len(credentialEncryptionPrefix) || stored[:len(credentialEncryptionPrefix)] != credentialEncryptionPrefix {
		return stored, nil
	}

	encoded := stored[len(credentialEncryptionPrefix):]
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to base64-decode encrypted credential: %w", err)
	}

	keyBytes := deriveCredentialKey(encryptionKey)

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("encrypted credential too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt credential (corrupt data or wrong key): %w", err)
	}

	return string(plaintext), nil
}

// decryptAndValidateCredential wraps decryptCredential with post-decryption sanity checks.
func decryptAndValidateCredential(stored, encryptionKey string) (string, error) {
	isEncrypted := len(stored) >= len(credentialEncryptionPrefix) && stored[:len(credentialEncryptionPrefix)] == credentialEncryptionPrefix

	result, err := decryptCredential(stored, encryptionKey)
	if err != nil {
		logrus.WithError(err).Error("Replication credential decryption failed — encryption key may have changed or data is corrupt")
		return "", ErrDecryptionFailed
	}
	if isEncrypted && len(result) < 8 {
		logrus.WithFields(logrus.Fields{
			"result_length": len(result),
		}).Error("Decrypted replication credential is empty or too short — encryption key may have rotated")
		return "", ErrDecryptionFailed
	}
	return result, nil
}

// deriveCredentialKey derives a 32-byte AES-256 key from an arbitrary-length passphrase
// using a single SHA-256 hash scoped to this purpose.
func deriveCredentialKey(passphrase string) []byte {
	h := sha256.New()
	h.Write([]byte("maxiofs-replication-credential-v1:"))
	h.Write([]byte(passphrase))
	return h.Sum(nil)
}

// ReencryptCredentials rewrites, inside tx, the destination keys encrypted
// with from as encrypted with to, and names the rules whose key neither
// decrypts. With from equal to to it only names them. A key stored before
// encryption is left as it is.
func ReencryptCredentials(ctx context.Context, tx *sql.Tx, from, to string) ([]string, error) {
	if from == "" || to == "" {
		return nil, fmt.Errorf("an encryption secret is required")
	}
	type stored struct{ id, source, endpoint, bucket, secret string }
	rows, err := tx.QueryContext(ctx,
		`SELECT id, source_bucket, destination_endpoint, destination_bucket, destination_secret_key FROM replication_rules`)
	if err != nil {
		return nil, err
	}
	var all []stored
	for rows.Next() {
		var r stored
		if err := rows.Scan(&r.id, &r.source, &r.endpoint, &r.bucket, &r.secret); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var unreadable []string
	for _, r := range all {
		if !strings.HasPrefix(r.secret, credentialEncryptionPrefix) {
			continue
		}
		plain, err := decryptCredential(r.secret, from)
		if err != nil {
			if _, err := decryptCredential(r.secret, to); err != nil {
				unreadable = append(unreadable, fmt.Sprintf("replication rule %s (%s to %s/%s)", r.id, r.source, r.endpoint, r.bucket))
			}
			continue
		}
		if from == to {
			continue
		}
		encrypted, err := encryptCredential(plain, to)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE replication_rules SET destination_secret_key = ? WHERE id = ?`, encrypted, r.id); err != nil {
			return nil, err
		}
	}
	return unreadable, nil
}
