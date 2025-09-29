package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// EncryptionConfig holds encryption configuration
type EncryptionConfig struct {
	// Algorithm specifies the encryption algorithm (AES-256-GCM, AES-256-CBC)
	Algorithm string
	// KeySource specifies where keys come from (server, customer, kms)
	KeySource string
	// MasterKey is the server-side master key (when using server-side encryption)
	MasterKey []byte
	// KeyDerivationRounds for PBKDF2 (default: 10000)
	KeyDerivationRounds int
}

// Encryptor defines the interface for encryption operations
type Encryptor interface {
	// Encrypt encrypts data with the given key
	Encrypt(data []byte, key []byte) (*EncryptedData, error)
	// Decrypt decrypts encrypted data with the given key
	Decrypt(encryptedData *EncryptedData, key []byte) ([]byte, error)
	// EncryptStream encrypts data from reader to writer
	EncryptStream(src io.Reader, dst io.Writer, key []byte) (*EncryptionMetadata, error)
	// DecryptStream decrypts data from reader to writer
	DecryptStream(src io.Reader, dst io.Writer, key []byte, metadata *EncryptionMetadata) error
	// GenerateKey generates a new encryption key
	GenerateKey() ([]byte, error)
	// DeriveKey derives a key from password and salt
	DeriveKey(password, salt []byte) []byte
}

// EncryptedData represents encrypted data with metadata
type EncryptedData struct {
	Data      []byte            `json:"data"`
	IV        []byte            `json:"iv"`
	Algorithm string            `json:"algorithm"`
	KeyID     string            `json:"key_id,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// EncryptionMetadata contains metadata about encrypted streams
type EncryptionMetadata struct {
	Algorithm   string            `json:"algorithm"`
	IV          []byte            `json:"iv"`
	KeyID       string            `json:"key_id,omitempty"`
	Size        int64             `json:"size"`
	Checksum    string            `json:"checksum,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	BlockSize   int               `json:"block_size"`
	Padding     string            `json:"padding,omitempty"`
}

// KeyManager defines the interface for key management
type KeyManager interface {
	// GetKey retrieves a key by ID
	GetKey(keyID string) ([]byte, error)
	// StoreKey stores a key with the given ID
	StoreKey(keyID string, key []byte) error
	// DeleteKey deletes a key by ID
	DeleteKey(keyID string) error
	// ListKeys lists all available key IDs
	ListKeys() ([]string, error)
	// RotateKey rotates a key (creates new version)
	RotateKey(keyID string) ([]byte, error)
	// GetDefaultKey gets the default encryption key
	GetDefaultKey() ([]byte, string, error)
}

// aesGCMEncryptor implements AES-GCM encryption
type aesGCMEncryptor struct {
	config *EncryptionConfig
}

// NewAESGCMEncryptor creates a new AES-GCM encryptor
func NewAESGCMEncryptor(config *EncryptionConfig) Encryptor {
	if config == nil {
		config = DefaultEncryptionConfig()
	}
	return &aesGCMEncryptor{config: config}
}

// DefaultEncryptionConfig returns default encryption configuration
func DefaultEncryptionConfig() *EncryptionConfig {
	return &EncryptionConfig{
		Algorithm:           "AES-256-GCM",
		KeySource:          "server",
		KeyDerivationRounds: 10000,
	}
}

// S3CompatibleEncryptionConfig returns S3-compatible encryption configuration
func S3CompatibleEncryptionConfig() *EncryptionConfig {
	return &EncryptionConfig{
		Algorithm:           "AES-256-GCM",
		KeySource:          "customer",
		KeyDerivationRounds: 10000,
	}
}

// Encrypt encrypts data using AES-GCM
func (e *aesGCMEncryptor) Encrypt(data []byte, key []byte) (*EncryptedData, error) {
	// Validate key length (must be 32 bytes for AES-256)
	if len(key) != 32 {
		derivedKey := e.DeriveKey(key, []byte("maxiofs-salt"))
		key = derivedKey
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random IV
	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	// Encrypt data
	ciphertext := gcm.Seal(nil, iv, data, nil)

	return &EncryptedData{
		Data:      ciphertext,
		IV:        iv,
		Algorithm: e.config.Algorithm,
		Metadata:  make(map[string]string),
	}, nil
}

// Decrypt decrypts data using AES-GCM
func (e *aesGCMEncryptor) Decrypt(encryptedData *EncryptedData, key []byte) ([]byte, error) {
	// Validate key length
	if len(key) != 32 {
		derivedKey := e.DeriveKey(key, []byte("maxiofs-salt"))
		key = derivedKey
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt data
	plaintext, err := gcm.Open(nil, encryptedData.IV, encryptedData.Data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data: %w", err)
	}

	return plaintext, nil
}

// EncryptStream encrypts data from reader to writer
func (e *aesGCMEncryptor) EncryptStream(src io.Reader, dst io.Writer, key []byte) (*EncryptionMetadata, error) {
	// Validate key length
	if len(key) != 32 {
		derivedKey := e.DeriveKey(key, []byte("maxiofs-salt"))
		key = derivedKey
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// Generate random IV
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	// Create CTR mode for streaming
	stream := cipher.NewCTR(block, iv)

	// Write IV to destination first
	if _, err := dst.Write(iv); err != nil {
		return nil, fmt.Errorf("failed to write IV: %w", err)
	}

	// Create stream writer
	writer := &cipher.StreamWriter{S: stream, W: dst}

	// Copy data while encrypting
	size, err := io.Copy(writer, src)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt stream: %w", err)
	}

	metadata := &EncryptionMetadata{
		Algorithm: e.config.Algorithm,
		IV:        iv,
		Size:      size,
		BlockSize: aes.BlockSize,
		Metadata:  make(map[string]string),
	}

	return metadata, nil
}

// DecryptStream decrypts data from reader to writer
func (e *aesGCMEncryptor) DecryptStream(src io.Reader, dst io.Writer, key []byte, metadata *EncryptionMetadata) error {
	// Validate key length
	if len(key) != 32 {
		derivedKey := e.DeriveKey(key, []byte("maxiofs-salt"))
		key = derivedKey
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// Read IV from source first (it was written there during encryption)
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(src, iv); err != nil {
		return fmt.Errorf("failed to read IV: %w", err)
	}

	// Create CTR mode for streaming
	stream := cipher.NewCTR(block, iv)

	// Create stream reader
	reader := &cipher.StreamReader{S: stream, R: src}

	// Copy data while decrypting
	if _, err := io.Copy(dst, reader); err != nil {
		return fmt.Errorf("failed to decrypt stream: %w", err)
	}

	return nil
}

// GenerateKey generates a new 256-bit encryption key
func (e *aesGCMEncryptor) GenerateKey() ([]byte, error) {
	key := make([]byte, 32) // 256 bits
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}
	return key, nil
}

// DeriveKey derives a key from password using SHA-256
func (e *aesGCMEncryptor) DeriveKey(password, salt []byte) []byte {
	// Simple key derivation using SHA-256
	// In production, you'd want to use PBKDF2, scrypt, or Argon2
	hash := sha256.New()
	hash.Write(password)
	hash.Write(salt)
	return hash.Sum(nil)
}

// inMemoryKeyManager implements KeyManager using in-memory storage
type inMemoryKeyManager struct {
	keys       map[string][]byte
	defaultKey string
}

// NewInMemoryKeyManager creates a new in-memory key manager
func NewInMemoryKeyManager() KeyManager {
	return &inMemoryKeyManager{
		keys: make(map[string][]byte),
	}
}

// GetKey retrieves a key by ID
func (km *inMemoryKeyManager) GetKey(keyID string) ([]byte, error) {
	key, exists := km.keys[keyID]
	if !exists {
		return nil, errors.New("key not found")
	}
	return key, nil
}

// StoreKey stores a key with the given ID
func (km *inMemoryKeyManager) StoreKey(keyID string, key []byte) error {
	if len(key) == 0 {
		return errors.New("key cannot be empty")
	}
	km.keys[keyID] = make([]byte, len(key))
	copy(km.keys[keyID], key)

	// Set as default if it's the first key
	if km.defaultKey == "" {
		km.defaultKey = keyID
	}

	return nil
}

// DeleteKey deletes a key by ID
func (km *inMemoryKeyManager) DeleteKey(keyID string) error {
	if _, exists := km.keys[keyID]; !exists {
		return errors.New("key not found")
	}
	delete(km.keys, keyID)

	// Clear default if this was the default key
	if km.defaultKey == keyID {
		km.defaultKey = ""
	}

	return nil
}

// ListKeys lists all available key IDs
func (km *inMemoryKeyManager) ListKeys() ([]string, error) {
	keys := make([]string, 0, len(km.keys))
	for keyID := range km.keys {
		keys = append(keys, keyID)
	}
	return keys, nil
}

// RotateKey rotates a key (creates new version)
func (km *inMemoryKeyManager) RotateKey(keyID string) ([]byte, error) {
	// Generate new key
	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		return nil, fmt.Errorf("failed to generate new key: %w", err)
	}

	// Store new key with versioned ID
	newKeyID := fmt.Sprintf("%s-v%d", keyID, len(km.keys)+1)
	if err := km.StoreKey(newKeyID, newKey); err != nil {
		return nil, err
	}

	return newKey, nil
}

// GetDefaultKey gets the default encryption key
func (km *inMemoryKeyManager) GetDefaultKey() ([]byte, string, error) {
	if km.defaultKey == "" {
		return nil, "", errors.New("no default key set")
	}

	key, err := km.GetKey(km.defaultKey)
	if err != nil {
		return nil, "", err
	}

	return key, km.defaultKey, nil
}

// EncryptionService combines encryptor and key manager
type EncryptionService struct {
	encryptor  Encryptor
	keyManager KeyManager
	config     *EncryptionConfig
}

// NewEncryptionService creates a new encryption service
func NewEncryptionService(config *EncryptionConfig) *EncryptionService {
	if config == nil {
		config = DefaultEncryptionConfig()
	}

	var encryptor Encryptor
	switch config.Algorithm {
	case "AES-256-GCM":
		encryptor = NewAESGCMEncryptor(config)
	default:
		encryptor = NewAESGCMEncryptor(config)
	}

	return &EncryptionService{
		encryptor:  encryptor,
		keyManager: NewInMemoryKeyManager(),
		config:     config,
	}
}

// EncryptData encrypts data using the default key
func (es *EncryptionService) EncryptData(data []byte) (*EncryptedData, error) {
	key, keyID, err := es.keyManager.GetDefaultKey()
	if err != nil {
		// Generate and store a default key
		key, err = es.encryptor.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("failed to generate key: %w", err)
		}
		keyID = "default"
		if err := es.keyManager.StoreKey(keyID, key); err != nil {
			return nil, fmt.Errorf("failed to store key: %w", err)
		}
	}

	encryptedData, err := es.encryptor.Encrypt(data, key)
	if err != nil {
		return nil, err
	}

	encryptedData.KeyID = keyID
	return encryptedData, nil
}

// DecryptData decrypts data using the specified key ID
func (es *EncryptionService) DecryptData(encryptedData *EncryptedData) ([]byte, error) {
	keyID := encryptedData.KeyID
	if keyID == "" {
		keyID = "default"
	}

	key, err := es.keyManager.GetKey(keyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", keyID, err)
	}

	return es.encryptor.Decrypt(encryptedData, key)
}

// EncryptStream encrypts a stream using the default key
func (es *EncryptionService) EncryptStream(src io.Reader, dst io.Writer) (*EncryptionMetadata, error) {
	key, keyID, err := es.keyManager.GetDefaultKey()
	if err != nil {
		// Generate and store a default key
		key, err = es.encryptor.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("failed to generate key: %w", err)
		}
		keyID = "default"
		if err := es.keyManager.StoreKey(keyID, key); err != nil {
			return nil, fmt.Errorf("failed to store key: %w", err)
		}
	}

	metadata, err := es.encryptor.EncryptStream(src, dst, key)
	if err != nil {
		return nil, err
	}

	metadata.KeyID = keyID
	return metadata, nil
}

// DecryptStream decrypts a stream using the specified key ID
func (es *EncryptionService) DecryptStream(src io.Reader, dst io.Writer, metadata *EncryptionMetadata) error {
	keyID := metadata.KeyID
	if keyID == "" {
		keyID = "default"
	}

	key, err := es.keyManager.GetKey(keyID)
	if err != nil {
		return fmt.Errorf("failed to get key %s: %w", keyID, err)
	}

	return es.encryptor.DecryptStream(src, dst, key, metadata)
}

// GetKeyManager returns the key manager
func (es *EncryptionService) GetKeyManager() KeyManager {
	return es.keyManager
}

// GetEncryptor returns the encryptor
func (es *EncryptionService) GetEncryptor() Encryptor {
	return es.encryptor
}