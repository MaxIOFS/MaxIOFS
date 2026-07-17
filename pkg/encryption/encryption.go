package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
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
	Algorithm string            `json:"algorithm"`
	IV        []byte            `json:"iv"`
	KeyID     string            `json:"key_id,omitempty"`
	Size      int64             `json:"size"`
	Checksum  string            `json:"checksum,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	BlockSize int               `json:"block_size"`
	Padding   string            `json:"padding,omitempty"`
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
		KeySource:           "server",
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

// gcmStreamChunkSize is the plaintext chunk size used for AES-256-GCM streaming.
// Each chunk is independently authenticated; 64 KB gives a good balance between
// memory use and overhead (16-byte GCM tag per chunk = 0.024% expansion).
const gcmStreamChunkSize = 64 * 1024

// EncryptStream encrypts a data stream using AES-256-GCM in a chunked fashion.
//
// Stream format written to dst:
//   - 12 bytes : base nonce (randomly generated)
//   - For every 64 KB chunk of plaintext:
//     4 bytes (big-endian uint32) : length of the following ciphertext+tag
//     N+16 bytes                  : GCM-sealed ciphertext including authentication tag
//
// Each chunk uses a derived nonce: baseNonce XOR (chunkIndex in last 4 bytes),
// preventing chunk reordering or substitution attacks.
func (e *aesGCMEncryptor) EncryptStream(src io.Reader, dst io.Writer, key []byte) (*EncryptionMetadata, error) {
	if len(key) != 32 {
		key = e.DeriveKey(key, []byte("maxiofs-salt"))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate 12-byte random base nonce and write it first.
	baseNonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := rand.Read(baseNonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	if _, err := dst.Write(baseNonce); err != nil {
		return nil, fmt.Errorf("failed to write nonce: %w", err)
	}

	buf := make([]byte, gcmStreamChunkSize)
	var totalSize int64
	var chunkIdx uint32

	for {
		n, readErr := io.ReadFull(src, buf)
		if n > 0 {
			// Per-chunk nonce: copy base nonce, XOR last 4 bytes with chunk index.
			chunkNonce := make([]byte, gcm.NonceSize())
			copy(chunkNonce, baseNonce)
			chunkNonce[8] ^= byte(chunkIdx >> 24)
			chunkNonce[9] ^= byte(chunkIdx >> 16)
			chunkNonce[10] ^= byte(chunkIdx >> 8)
			chunkNonce[11] ^= byte(chunkIdx)

			ciphertext := gcm.Seal(nil, chunkNonce, buf[:n], nil)

			// Write 4-byte big-endian length prefix then ciphertext+tag.
			l := uint32(len(ciphertext))
			lenBuf := [4]byte{byte(l >> 24), byte(l >> 16), byte(l >> 8), byte(l)}
			if _, err := dst.Write(lenBuf[:]); err != nil {
				return nil, fmt.Errorf("failed to write chunk length: %w", err)
			}
			if _, err := dst.Write(ciphertext); err != nil {
				return nil, fmt.Errorf("failed to write chunk: %w", err)
			}

			totalSize += int64(n)
			chunkIdx++
		}

		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("failed to read input: %w", readErr)
		}
	}

	return &EncryptionMetadata{
		Algorithm: "AES-256-GCM-STREAM",
		IV:        baseNonce,
		Size:      totalSize,
		BlockSize: gcmStreamChunkSize,
		Metadata:  make(map[string]string),
	}, nil
}

// DecryptStream decrypts a stream previously encrypted by EncryptStream.
//
// If metadata.Algorithm is "AES-256-CTR" (or empty) the legacy AES-CTR path is
// used for backward compatibility with objects encrypted before this fix.
// All other values (including "AES-256-GCM-STREAM") use the chunked GCM path.
func (e *aesGCMEncryptor) DecryptStream(src io.Reader, dst io.Writer, key []byte, metadata *EncryptionMetadata) error {
	if len(key) != 32 {
		key = e.DeriveKey(key, []byte("maxiofs-salt"))
	}

	// Route to the legacy CTR path for objects encrypted before Bug #21 fix.
	if metadata == nil || metadata.Algorithm == "" || metadata.Algorithm == "AES-256-CTR" {
		return e.decryptCTRStream(src, dst, key)
	}

	return e.decryptGCMStream(src, dst, key)
}

// decryptCTRStream is the legacy AES-CTR decryption path kept for objects
// encrypted before the introduction of authenticated streaming (Bug #21 fix).
// Format: 16-byte IV followed by CTR-mode ciphertext.
func (e *aesGCMEncryptor) decryptCTRStream(src io.Reader, dst io.Writer, key []byte) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	iv := make([]byte, aes.BlockSize) // 16 bytes
	if _, err := io.ReadFull(src, iv); err != nil {
		return fmt.Errorf("failed to read CTR IV: %w", err)
	}

	stream := cipher.NewCTR(block, iv)
	reader := &cipher.StreamReader{S: stream, R: src}
	if _, err := io.Copy(dst, reader); err != nil {
		return fmt.Errorf("failed to decrypt CTR stream: %w", err)
	}

	return nil
}

// decryptGCMStream decrypts a stream written by the chunked AES-GCM EncryptStream.
// Format: 12-byte base nonce, then repeating [4-byte len][ciphertext+tag] chunks.
func (e *aesGCMEncryptor) decryptGCMStream(src io.Reader, dst io.Writer, key []byte) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to create GCM: %w", err)
	}

	baseNonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(src, baseNonce); err != nil {
		return fmt.Errorf("failed to read base nonce: %w", err)
	}

	var chunkIdx uint32
	for {
		var lenBuf [4]byte
		_, err := io.ReadFull(src, lenBuf[:])
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read chunk length: %w", err)
		}

		chunkLen := int(lenBuf[0])<<24 | int(lenBuf[1])<<16 | int(lenBuf[2])<<8 | int(lenBuf[3])
		ciphertext := make([]byte, chunkLen)
		if _, err := io.ReadFull(src, ciphertext); err != nil {
			return fmt.Errorf("failed to read chunk %d: %w", chunkIdx, err)
		}

		chunkNonce := make([]byte, gcm.NonceSize())
		copy(chunkNonce, baseNonce)
		chunkNonce[8] ^= byte(chunkIdx >> 24)
		chunkNonce[9] ^= byte(chunkIdx >> 16)
		chunkNonce[10] ^= byte(chunkIdx >> 8)
		chunkNonce[11] ^= byte(chunkIdx)

		plaintext, err := gcm.Open(nil, chunkNonce, ciphertext, nil)
		if err != nil {
			return fmt.Errorf("chunk %d: authentication failed — object may be corrupted or tampered", chunkIdx)
		}
		if _, err := dst.Write(plaintext); err != nil {
			return fmt.Errorf("failed to write decrypted chunk: %w", err)
		}
		chunkIdx++
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

// DeriveKey derives a 256-bit AES key from password and salt using PBKDF2-SHA256.
// SEC-06: 310,000 iterations (OWASP 2023 minimum for PBKDF2-SHA256).
// The caller is responsible for providing a unique, random salt per usage context.
func (e *aesGCMEncryptor) DeriveKey(password, salt []byte) []byte {
	iterations := e.config.KeyDerivationRounds
	if iterations < 310000 {
		iterations = 310000 // enforce minimum regardless of config
	}
	return pbkdf2.Key(password, salt, iterations, 32, sha256.New)
}
