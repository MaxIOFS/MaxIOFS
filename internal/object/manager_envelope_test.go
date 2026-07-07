package object

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/maxiofs/maxiofs/pkg/encryption"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envelopeTestKey is the fixed master key used to simulate a pre-envelope
// deployment (the former config.yaml encryption_key = KEK version 1).
const envelopeTestKey = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"

// setupManagerWithConfigKey builds a manager the way a legacy deployment
// would: encryption_key in config, no DB-backed KEK provider.
func setupManagerWithConfigKey(t *testing.T) (*objectManager, storage.Backend, metadata.Store) {
	t.Helper()
	tempDir := t.TempDir()

	backend, err := storage.NewFilesystemBackend(storage.Config{Root: tempDir})
	require.NoError(t, err)

	metaStore, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: filepath.Join(tempDir, "metadata"),
		Logger:  logrus.StandardLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { metaStore.Close() })

	cfg := config.StorageConfig{
		Backend:       "filesystem",
		Root:          tempDir,
		EncryptionKey: envelopeTestKey,
	}
	om := NewManager(backend, metaStore, cfg).(*objectManager)
	return om, backend, metaStore
}

// TestLegacyDirectEncryptedObjectStillDecrypts simulates an object written by
// a pre-envelope version (encrypted directly with the config master key, no
// wrapped DEK in the sidecar) and verifies the new multi-format reader
// decrypts it with KEK version 1.
func TestLegacyDirectEncryptedObjectStillDecrypts(t *testing.T) {
	ctx := context.Background()
	om, backend, metaStore := setupManagerWithConfigKey(t)

	bucketName := "legacy-bucket"
	key := "legacy-object.txt"
	content := []byte("object written before envelope encryption existed")

	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	// Encrypt the content EXACTLY as the old code did: directly with the
	// master key from config (no DEK, no wrapped-dek metadata).
	masterKey, err := hex.DecodeString(envelopeTestKey)
	require.NoError(t, err)

	encryptor := encryption.NewAESGCMEncryptor(encryption.DefaultEncryptionConfig())
	var ciphertext bytes.Buffer
	_, err = encryptor.EncryptStream(bytes.NewReader(content), &ciphertext, masterKey)
	require.NoError(t, err)

	md5sum := md5.Sum(content)
	legacyMeta := map[string]string{
		"original-size":                          fmt.Sprintf("%d", len(content)),
		"original-etag":                          hex.EncodeToString(md5sum[:]),
		"encrypted":                              "true",
		"x-amz-server-side-encryption":           "AES256",
		"x-amz-server-side-encryption-algorithm": "AES-256-GCM-STREAM",
		"content-type":                           "text/plain",
	}
	objectPath := om.getObjectPath(bucketName, key)
	require.NoError(t, backend.Put(ctx, objectPath, &ciphertext, legacyMeta))

	// Sanity: the sidecar must NOT contain a wrapped DEK (legacy format).
	storedMeta, err := backend.GetMetadata(ctx, objectPath)
	require.NoError(t, err)
	require.Empty(t, storedMeta["wrapped-dek"])

	// The new reader must decrypt it via KEK version 1.
	_, reader, err := om.GetObject(ctx, bucketName, key)
	require.NoError(t, err)
	defer reader.Close()

	readBack, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, readBack, "legacy direct-encrypted object must decrypt with KEK v1")
}

// TestPlaintextObjectStillServed simulates an object written by a deployment
// that ran without any encryption key: stored as plaintext, no encrypted flag.
// It must be served as-is.
func TestPlaintextObjectStillServed(t *testing.T) {
	ctx := context.Background()
	om, backend, metaStore := setupManagerWithConfigKey(t)

	bucketName := "plaintext-bucket"
	key := "plaintext-object.txt"
	content := []byte("object stored in plaintext by a key-less deployment")

	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	md5sum := md5.Sum(content)
	plainMeta := map[string]string{
		"size":         fmt.Sprintf("%d", len(content)),
		"etag":         hex.EncodeToString(md5sum[:]),
		"content-type": "text/plain",
	}
	objectPath := om.getObjectPath(bucketName, key)
	require.NoError(t, backend.Put(ctx, objectPath, bytes.NewReader(content), plainMeta))

	_, reader, err := om.GetObject(ctx, bucketName, key)
	require.NoError(t, err)
	defer reader.Close()

	readBack, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, readBack, "plaintext object must be served as-is")
}

// TestEnvelopeRoundtripWithConfigKey verifies the full write→read cycle:
// new objects get a wrapped DEK under KEK v1 (the config key) and decrypt
// transparently. A second manager built from the same config key (simulated
// restart) must also decrypt them.
func TestEnvelopeRoundtripWithConfigKey(t *testing.T) {
	ctx := context.Background()
	om, backend, metaStore := setupManagerWithConfigKey(t)

	bucketName := "envelope-bucket"
	key := "envelope-object.bin"
	content := bytes.Repeat([]byte("envelope encryption test data "), 10000) // ~300 KB, multiple GCM chunks

	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	obj, err := om.PutObject(ctx, bucketName, key, bytes.NewReader(content), http.Header{})
	require.NoError(t, err)
	assert.Equal(t, "AES256", obj.SSEAlgorithm)

	// Envelope fields must be present in the sidecar.
	objectPath := om.getObjectPath(bucketName, key)
	storedMeta, err := backend.GetMetadata(ctx, objectPath)
	require.NoError(t, err)
	assert.Equal(t, "true", storedMeta["encrypted"])
	assert.NotEmpty(t, storedMeta["wrapped-dek"])
	assert.NotEmpty(t, storedMeta["wrapped-dek-iv"])
	assert.Equal(t, "1", storedMeta["kek-version"])

	// The raw bytes on disk must NOT be the plaintext.
	rawReader, _, err := backend.Get(ctx, objectPath)
	require.NoError(t, err)
	rawBytes, err := io.ReadAll(rawReader)
	rawReader.Close()
	require.NoError(t, err)
	assert.NotEqual(t, content, rawBytes, "on-disk bytes must be ciphertext")

	// Same manager decrypts.
	_, reader, err := om.GetObject(ctx, bucketName, key)
	require.NoError(t, err)
	readBack, err := io.ReadAll(reader)
	reader.Close()
	require.NoError(t, err)
	assert.Equal(t, content, readBack)

	// A fresh manager with the same config key (restart) also decrypts.
	cfg := config.StorageConfig{
		Backend:       "filesystem",
		Root:          om.config.Root,
		EncryptionKey: envelopeTestKey,
	}
	om2 := NewManager(backend, metaStore, cfg).(*objectManager)
	_, reader2, err := om2.GetObject(ctx, bucketName, key)
	require.NoError(t, err)
	readBack2, err := io.ReadAll(reader2)
	reader2.Close()
	require.NoError(t, err)
	assert.Equal(t, content, readBack2, "restarted manager must decrypt envelope objects")
}

// TestFolderMarkerIsNotEncrypted verifies that folder markers (keys ending
// in "/") are stored as plain directory markers without envelope metadata.
func TestFolderMarkerIsNotEncrypted(t *testing.T) {
	ctx := context.Background()
	om, backend, metaStore := setupManagerWithConfigKey(t)

	bucketName := "folder-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	obj, err := om.PutObject(ctx, bucketName, "my-folder/", bytes.NewReader(nil), http.Header{})
	require.NoError(t, err)
	assert.Empty(t, obj.SSEAlgorithm)

	storedMeta, err := backend.GetMetadata(ctx, om.getObjectPath(bucketName, "my-folder/"))
	require.NoError(t, err)
	assert.NotEqual(t, "true", storedMeta["encrypted"])
	assert.Empty(t, storedMeta["wrapped-dek"])
}

// TestUpdateObjectMetadataPreservesEnvelope verifies that editing user
// metadata cannot destroy the sidecar's encryption entries (which would make
// the object undecryptable).
func TestUpdateObjectMetadataPreservesEnvelope(t *testing.T) {
	ctx := context.Background()
	om, backend, metaStore := setupManagerWithConfigKey(t)

	bucketName := "meta-bucket"
	key := "meta-object.txt"
	content := []byte("metadata update must not break decryption")

	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	_, err := om.PutObject(ctx, bucketName, key, bytes.NewReader(content), http.Header{})
	require.NoError(t, err)

	// Update user metadata — including a malicious attempt to clear system keys.
	err = om.UpdateObjectMetadata(ctx, bucketName, key, map[string]string{
		"x-amz-meta-owner": "someone",
		"content-type":     "application/custom",
		"wrapped-dek":      "", // must be ignored
		"encrypted":        "false",
	})
	require.NoError(t, err)

	storedMeta, err := backend.GetMetadata(ctx, om.getObjectPath(bucketName, key))
	require.NoError(t, err)
	assert.Equal(t, "true", storedMeta["encrypted"], "system key must survive metadata update")
	assert.NotEmpty(t, storedMeta["wrapped-dek"], "wrapped DEK must survive metadata update")
	assert.Equal(t, "someone", storedMeta["x-amz-meta-owner"])
	assert.Equal(t, "application/custom", storedMeta["content-type"])

	// And the object still decrypts.
	_, reader, err := om.GetObject(ctx, bucketName, key)
	require.NoError(t, err)
	defer reader.Close()
	readBack, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, readBack)
}
