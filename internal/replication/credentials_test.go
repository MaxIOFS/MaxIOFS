package replication

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// encryptCredential / decryptCredential round-trip
// ---------------------------------------------------------------------------

func TestCredentials_RoundTrip(t *testing.T) {
	plaintext := "super-secret-key-123"
	key := "my-encryption-passphrase"

	encrypted, err := encryptCredential(plaintext, key)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(encrypted, credentialEncryptionPrefix))
	assert.NotEqual(t, plaintext, encrypted)

	decrypted, err := decryptCredential(encrypted, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestCredentials_RoundTrip_DifferentValues(t *testing.T) {
	key := "test-key"
	values := []string{
		"short",
		"AKIAIOSFODNN7EXAMPLE",
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		strings.Repeat("x", 512),
	}
	for _, v := range values {
		enc, err := encryptCredential(v, key)
		require.NoError(t, err)
		dec, err := decryptCredential(enc, key)
		require.NoError(t, err)
		assert.Equal(t, v, dec)
	}
}

// Each call should produce a different ciphertext (random nonce)
func TestCredentials_Encrypt_RandomNonce(t *testing.T) {
	plaintext := "same-value"
	key := "same-key"

	enc1, err := encryptCredential(plaintext, key)
	require.NoError(t, err)
	enc2, err := encryptCredential(plaintext, key)
	require.NoError(t, err)

	assert.NotEqual(t, enc1, enc2, "each encryption should produce a distinct ciphertext")
}

// ---------------------------------------------------------------------------
// Empty / passthrough cases
// ---------------------------------------------------------------------------

func TestCredentials_EmptyKey_ReturnsPlaintext(t *testing.T) {
	enc, err := encryptCredential("secret", "")
	require.NoError(t, err)
	assert.Equal(t, "secret", enc)

	dec, err := decryptCredential("secret", "")
	require.NoError(t, err)
	assert.Equal(t, "secret", dec)
}

func TestCredentials_EmptyPlaintext_ReturnsEmpty(t *testing.T) {
	enc, err := encryptCredential("", "some-key")
	require.NoError(t, err)
	assert.Equal(t, "", enc)
}

func TestCredentials_EmptyStored_ReturnsEmpty(t *testing.T) {
	dec, err := decryptCredential("", "some-key")
	require.NoError(t, err)
	assert.Equal(t, "", dec)
}

// ---------------------------------------------------------------------------
// Legacy plaintext migration
// ---------------------------------------------------------------------------

func TestCredentials_Decrypt_LegacyPlaintext(t *testing.T) {
	// Values without the "enc1:" prefix are legacy — returned as-is
	legacy := "plaintext-secret-no-prefix"
	dec, err := decryptCredential(legacy, "any-key")
	require.NoError(t, err)
	assert.Equal(t, legacy, dec)
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestCredentials_Decrypt_WrongKey(t *testing.T) {
	enc, err := encryptCredential("secret", "correct-key")
	require.NoError(t, err)

	_, err = decryptCredential(enc, "wrong-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt")
}

func TestCredentials_Decrypt_CorruptBase64(t *testing.T) {
	corrupt := credentialEncryptionPrefix + "!!!not-valid-base64!!!"
	_, err := decryptCredential(corrupt, "some-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base64")
}

func TestCredentials_Decrypt_TooShort(t *testing.T) {
	import64 := credentialEncryptionPrefix + "dGVzdA==" // "test" in base64 — too short for nonce+ciphertext
	_, err := decryptCredential(import64, "some-key")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// deriveCredentialKey
// ---------------------------------------------------------------------------

func TestDeriveCredentialKey_Length(t *testing.T) {
	key := deriveCredentialKey("any-passphrase")
	assert.Len(t, key, 32, "AES-256 key must be 32 bytes")
}

func TestDeriveCredentialKey_Deterministic(t *testing.T) {
	k1 := deriveCredentialKey("passphrase")
	k2 := deriveCredentialKey("passphrase")
	assert.Equal(t, k1, k2)
}

func TestDeriveCredentialKey_DifferentInputs(t *testing.T) {
	k1 := deriveCredentialKey("passphrase-a")
	k2 := deriveCredentialKey("passphrase-b")
	assert.NotEqual(t, k1, k2)
}
