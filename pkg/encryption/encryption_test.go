package encryption

import (
	"bytes"
	"strings"
	"testing"
)

func TestAESGCMEncryption(t *testing.T) {
	encryptor := NewAESGCMEncryptor(DefaultEncryptionConfig())

	// Test data
	originalData := []byte("Hello, World! This is a test message for encryption.")
	key, err := encryptor.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Test encryption
	encrypted, err := encryptor.Encrypt(originalData, key)
	if err != nil {
		t.Fatalf("Failed to encrypt data: %v", err)
	}

	if len(encrypted.Data) == 0 {
		t.Fatal("Encrypted data is empty")
	}

	if encrypted.Algorithm != "AES-256-GCM" {
		t.Errorf("Expected algorithm AES-256-GCM, got %s", encrypted.Algorithm)
	}

	// Test decryption
	decrypted, err := encryptor.Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Failed to decrypt data: %v", err)
	}

	if !bytes.Equal(originalData, decrypted) {
		t.Errorf("Decrypted data doesn't match original. Expected: %s, Got: %s", originalData, decrypted)
	}
}

func TestStreamEncryption(t *testing.T) {
	encryptor := NewAESGCMEncryptor(DefaultEncryptionConfig())
	key, _ := encryptor.GenerateKey()

	// Test data
	originalData := "This is a test for stream encryption with a longer message to test streaming capabilities."
	src := strings.NewReader(originalData)
	var encrypted bytes.Buffer

	// Test stream encryption
	metadata, err := encryptor.EncryptStream(src, &encrypted, key)
	if err != nil {
		t.Fatalf("Failed to encrypt stream: %v", err)
	}

	if metadata.Size != int64(len(originalData)) {
		t.Errorf("Expected size %d, got %d", len(originalData), metadata.Size)
	}

	// Test stream decryption
	var decrypted bytes.Buffer
	err = encryptor.DecryptStream(&encrypted, &decrypted, key, metadata)
	if err != nil {
		t.Fatalf("Failed to decrypt stream: %v", err)
	}

	if decrypted.String() != originalData {
		t.Errorf("Decrypted stream doesn't match original")
	}
}

