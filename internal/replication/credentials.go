package replication

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

const credentialEncryptionPrefix = "enc1:"

// encryptCredential encrypts a plaintext credential string using AES-256-GCM.
// The result is a base64-encoded blob prefixed with "enc1:" so it can be identified
// as an encrypted value when reading from the database.
// Returns the original plaintext unchanged if encryptionKey is empty.
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
// If the value does not carry the "enc1:" prefix (legacy plaintext), it is returned as-is,
// which allows gradual migration — existing rules remain readable until they are next updated.
// Returns the original value unchanged if encryptionKey is empty.
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

// deriveCredentialKey derives a 32-byte AES-256 key from an arbitrary-length passphrase
// using a single SHA-256 hash scoped to this purpose.
func deriveCredentialKey(passphrase string) []byte {
	h := sha256.New()
	h.Write([]byte("maxiofs-replication-credential-v1:"))
	h.Write([]byte(passphrase))
	return h.Sum(nil)
}
