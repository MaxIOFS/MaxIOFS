package idp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	secret := "my-super-secret-encryption-key-32chars"

	tests := []struct {
		name      string
		plaintext string
	}{
		{"simple password", "MyLDAPBindPassword123!"},
		{"special characters", "p@$$w0rd!#%^&*()_+-=[]{}|;':\",./<>?"},
		{"unicode", "contraseña-日本語-пароль"},
		{"long string", strings.Repeat("a", 1000)},
		{"single char", "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := Encrypt(tt.plaintext, secret)
			require.NoError(t, err)
			assert.NotEmpty(t, encrypted)
			assert.NotEqual(t, tt.plaintext, encrypted, "encrypted should differ from plaintext")

			decrypted, err := Decrypt(encrypted, secret)
			require.NoError(t, err)
			assert.Equal(t, tt.plaintext, decrypted)
		})
	}
}

func TestEncryptDecrypt_EmptyString(t *testing.T) {
	secret := "test-secret"

	encrypted, err := Encrypt("", secret)
	require.NoError(t, err)
	assert.Empty(t, encrypted, "encrypting empty string should return empty")

	decrypted, err := Decrypt("", secret)
	require.NoError(t, err)
	assert.Empty(t, decrypted, "decrypting empty string should return empty")
}

func TestDecrypt_WrongKey(t *testing.T) {
	plaintext := "secret-password"
	secret1 := "correct-key-for-encryption-32ch"
	secret2 := "wrong-key-for-decryption-32char"

	encrypted, err := Encrypt(plaintext, secret1)
	require.NoError(t, err)

	_, err = Decrypt(encrypted, secret2)
	assert.Error(t, err, "decrypting with wrong key should fail")
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	plaintext := "secret-password"
	secret := "test-secret-key"

	encrypted, err := Encrypt(plaintext, secret)
	require.NoError(t, err)

	// Tamper with the base64 string
	tampered := encrypted[:len(encrypted)-4] + "XXXX"
	_, err = Decrypt(tampered, secret)
	assert.Error(t, err, "decrypting tampered ciphertext should fail")
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	_, err := Decrypt("not-valid-base64!!!", "secret")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "base64")
}

func TestDecrypt_CiphertextTooShort(t *testing.T) {
	// Base64 of a very short byte slice (shorter than nonce)
	_, err := Decrypt("YWJj", "secret") // "abc" in base64
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
}

func TestEncrypt_DifferentOutputEachTime(t *testing.T) {
	secret := "test-secret"
	plaintext := "same-input"

	enc1, err := Encrypt(plaintext, secret)
	require.NoError(t, err)

	enc2, err := Encrypt(plaintext, secret)
	require.NoError(t, err)

	assert.NotEqual(t, enc1, enc2, "each encryption should use a unique nonce")
}

func TestDeriveKey_Deterministic(t *testing.T) {
	key1 := deriveKey("my-secret")
	key2 := deriveKey("my-secret")
	assert.Equal(t, key1, key2, "same input should produce same key")
	assert.Len(t, key1, 32, "derived key should be 32 bytes")
}

func TestDeriveKey_DifferentInputs(t *testing.T) {
	key1 := deriveKey("secret-a")
	key2 := deriveKey("secret-b")
	assert.NotEqual(t, key1, key2, "different inputs should produce different keys")
}
