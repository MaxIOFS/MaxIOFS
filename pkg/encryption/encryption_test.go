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

func TestEncryptionService(t *testing.T) {
	service := NewEncryptionService(DefaultEncryptionConfig())

	// Test data
	originalData := []byte("Test data for encryption service")

	// Test encryption
	encrypted, err := service.EncryptData(originalData)
	if err != nil {
		t.Fatalf("Failed to encrypt data: %v", err)
	}

	if encrypted.KeyID == "" {
		t.Error("KeyID should not be empty")
	}

	// Test decryption
	decrypted, err := service.DecryptData(encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt data: %v", err)
	}

	if !bytes.Equal(originalData, decrypted) {
		t.Errorf("Decrypted data doesn't match original")
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

func TestKeyManager(t *testing.T) {
	km := NewInMemoryKeyManager()

	// Test key storage
	testKey := []byte("test-key-32-bytes-long-for-aes256")
	err := km.StoreKey("test-key", testKey)
	if err != nil {
		t.Fatalf("Failed to store key: %v", err)
	}

	// Test key retrieval
	retrievedKey, err := km.GetKey("test-key")
	if err != nil {
		t.Fatalf("Failed to get key: %v", err)
	}

	if !bytes.Equal(testKey, retrievedKey) {
		t.Error("Retrieved key doesn't match stored key")
	}

	// Test default key
	defaultKey, keyID, err := km.GetDefaultKey()
	if err != nil {
		t.Fatalf("Failed to get default key: %v", err)
	}

	if keyID != "test-key" {
		t.Errorf("Expected default key ID 'test-key', got '%s'", keyID)
	}

	if !bytes.Equal(testKey, defaultKey) {
		t.Error("Default key doesn't match stored key")
	}

	// Test key listing
	keys, err := km.ListKeys()
	if err != nil {
		t.Fatalf("Failed to list keys: %v", err)
	}

	if len(keys) != 1 || keys[0] != "test-key" {
		t.Errorf("Expected keys ['test-key'], got %v", keys)
	}

	// Test key deletion
	err = km.DeleteKey("test-key")
	if err != nil {
		t.Fatalf("Failed to delete key: %v", err)
	}

	_, err = km.GetKey("test-key")
	if err == nil {
		t.Error("Expected error when getting deleted key")
	}
}