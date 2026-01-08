package encryption

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

// BenchmarkEncrypt_1KB benchmarks encrypting 1KB of data
func BenchmarkEncrypt_1KB(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	data := make([]byte, 1024)
	rand.Read(data)

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := encryptor.Encrypt(data, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncrypt_100KB benchmarks encrypting 100KB of data
func BenchmarkEncrypt_100KB(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	data := make([]byte, 100*1024)
	rand.Read(data)

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := encryptor.Encrypt(data, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncrypt_1MB benchmarks encrypting 1MB of data
func BenchmarkEncrypt_1MB(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	data := make([]byte, 1024*1024)
	rand.Read(data)

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := encryptor.Encrypt(data, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecrypt_1KB benchmarks decrypting 1KB of data
func BenchmarkDecrypt_1KB(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	// Encrypt data first
	data := make([]byte, 1024)
	rand.Read(data)
	encrypted, err := encryptor.Encrypt(data, key)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := encryptor.Decrypt(encrypted, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecrypt_100KB benchmarks decrypting 100KB of data
func BenchmarkDecrypt_100KB(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	// Encrypt data first
	data := make([]byte, 100*1024)
	rand.Read(data)
	encrypted, err := encryptor.Encrypt(data, key)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := encryptor.Decrypt(encrypted, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecrypt_1MB benchmarks decrypting 1MB of data
func BenchmarkDecrypt_1MB(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	// Encrypt data first
	data := make([]byte, 1024*1024)
	rand.Read(data)
	encrypted, err := encryptor.Encrypt(data, key)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := encryptor.Decrypt(encrypted, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncryptStream_1MB benchmarks stream encryption of 1MB
func BenchmarkEncryptStream_1MB(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	data := make([]byte, 1024*1024)
	rand.Read(data)

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(data)
		dst := &bytes.Buffer{}
		_, err := encryptor.EncryptStream(src, dst, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecryptStream_1MB benchmarks stream decryption of 1MB
func BenchmarkDecryptStream_1MB(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	// Encrypt data first
	data := make([]byte, 1024*1024)
	rand.Read(data)
	src := bytes.NewReader(data)
	encryptedBuf := &bytes.Buffer{}
	metadata, err := encryptor.EncryptStream(src, encryptedBuf, key)
	if err != nil {
		b.Fatal(err)
	}
	encryptedData := encryptedBuf.Bytes()

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(encryptedData)
		dst := &bytes.Buffer{}
		err := encryptor.DecryptStream(src, dst, key, metadata)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRoundTrip_100KB benchmarks full encrypt/decrypt cycle
func BenchmarkRoundTrip_100KB(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	data := make([]byte, 100*1024)
	rand.Read(data)

	b.SetBytes(int64(len(data) * 2)) // Count both encrypt and decrypt
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Encrypt
		encrypted, err := encryptor.Encrypt(data, key)
		if err != nil {
			b.Fatal(err)
		}

		// Decrypt
		_, err = encryptor.Decrypt(encrypted, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGenerateKey benchmarks key generation
func BenchmarkGenerateKey(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := encryptor.GenerateKey()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDeriveKey benchmarks key derivation
func BenchmarkDeriveKey(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	password := []byte("test-password")
	salt := make([]byte, 16)
	rand.Read(salt)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = encryptor.DeriveKey(password, salt)
	}
}

// BenchmarkConcurrentEncrypt benchmarks concurrent encryption operations
func BenchmarkConcurrentEncrypt(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	data := make([]byte, 10*1024)
	rand.Read(data)

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := encryptor.Encrypt(data, key)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkConcurrentDecrypt benchmarks concurrent decryption operations
func BenchmarkConcurrentDecrypt(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	// Pre-encrypt data once
	data := make([]byte, 10*1024)
	rand.Read(data)
	encrypted, err := encryptor.Encrypt(data, key)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := encryptor.Decrypt(encrypted, key)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkEncryptStream_10MB benchmarks stream encryption of 10MB
func BenchmarkEncryptStream_10MB(b *testing.B) {
	encryptor := NewAESGCMEncryptor(nil)
	key := make([]byte, 32)
	rand.Read(key)

	data := make([]byte, 10*1024*1024)
	rand.Read(data)

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(data)
		dst := io.Discard
		_, err := encryptor.EncryptStream(src, dst, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}
