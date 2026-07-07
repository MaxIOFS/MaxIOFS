package object

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// putPlaintextObject writes an object directly to storage the way a key-less
// pre-envelope deployment did: raw bytes + sidecar without encryption fields,
// plus the Pebble metadata entry.
func putPlaintextObject(t *testing.T, om *objectManager, metaStore metadata.Store, bucket, key string, content []byte) {
	t.Helper()
	ctx := context.Background()

	md5sum := md5.Sum(content)
	sidecar := map[string]string{
		"size":         fmt.Sprintf("%d", len(content)),
		"etag":         hex.EncodeToString(md5sum[:]),
		"content-type": "text/plain",
	}
	require.NoError(t, om.storage.Put(ctx, om.getObjectPath(bucket, key), bytes.NewReader(content), sidecar))

	require.NoError(t, metaStore.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket:      bucket,
		Key:         key,
		Size:        int64(len(content)),
		ETag:        hex.EncodeToString(md5sum[:]),
		ContentType: "text/plain",
	}))
}

func TestEncryptExistingObject_ConvertsPlaintext(t *testing.T) {
	ctx := context.Background()
	om, backend, metaStore := setupManagerWithConfigKey(t)

	bucketName := "migrate-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucketName, OwnerID: "u"}))

	content := bytes.Repeat([]byte("plaintext object to migrate "), 5000) // ~140 KB, multiple chunks
	putPlaintextObject(t, om, metaStore, bucketName, "legacy.txt", content)

	converted, skipped, err := om.EncryptExistingObject(ctx, bucketName, "legacy.txt")
	require.NoError(t, err)
	assert.Equal(t, 1, converted)
	assert.Equal(t, 0, skipped)

	// Sidecar now carries the envelope; disk bytes are ciphertext.
	meta, err := backend.GetMetadata(ctx, om.getObjectPath(bucketName, "legacy.txt"))
	require.NoError(t, err)
	assert.Equal(t, "true", meta["encrypted"])
	assert.NotEmpty(t, meta["wrapped-dek"])
	assert.Equal(t, fmt.Sprintf("%d", len(content)), meta["original-size"])

	raw, _, err := backend.Get(ctx, om.getObjectPath(bucketName, "legacy.txt"))
	require.NoError(t, err)
	rawBytes, _ := io.ReadAll(raw)
	raw.Close()
	assert.NotEqual(t, content, rawBytes)

	// And it reads back through the normal path.
	obj, reader, err := om.GetObject(ctx, bucketName, "legacy.txt")
	require.NoError(t, err)
	defer reader.Close()
	readBack, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, readBack)
	assert.Equal(t, int64(len(content)), obj.Size)
}

func TestEncryptExistingObject_SkipsAlreadyEncrypted(t *testing.T) {
	ctx := context.Background()
	om, _, metaStore := setupManagerWithConfigKey(t)

	bucketName := "skip-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucketName, OwnerID: "u"}))

	// Normal PutObject → already envelope-encrypted.
	_, err := om.PutObject(ctx, bucketName, "new.txt", bytes.NewReader([]byte("already encrypted")), http.Header{})
	require.NoError(t, err)

	converted, skipped, err := om.EncryptExistingObject(ctx, bucketName, "new.txt")
	require.NoError(t, err)
	assert.Equal(t, 0, converted)
	assert.Equal(t, 1, skipped)

	// Idempotent: still reads fine.
	_, reader, err := om.GetObject(ctx, bucketName, "new.txt")
	require.NoError(t, err)
	defer reader.Close()
	readBack, _ := io.ReadAll(reader)
	assert.Equal(t, []byte("already encrypted"), readBack)
}

func TestEncryptExistingObject_SkipsFolderAndMissing(t *testing.T) {
	ctx := context.Background()
	om, _, metaStore := setupManagerWithConfigKey(t)

	bucketName := "marker-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucketName, OwnerID: "u"}))

	// Folder marker.
	converted, skipped, err := om.EncryptExistingObject(ctx, bucketName, "folder/")
	require.NoError(t, err)
	assert.Equal(t, 0, converted)
	assert.Equal(t, 1, skipped)

	// Missing object (no file on disk).
	converted, skipped, err = om.EncryptExistingObject(ctx, bucketName, "does-not-exist.txt")
	require.NoError(t, err)
	assert.Equal(t, 0, converted)
	assert.Equal(t, 1, skipped)
}

func TestEncryptExistingObject_MultipartETagPreserved(t *testing.T) {
	ctx := context.Background()
	om, backend, metaStore := setupManagerWithConfigKey(t)

	bucketName := "mpetag-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucketName, OwnerID: "u"}))

	// Plaintext multipart object: sidecar etag has the "<md5>-<N>" format,
	// which is NOT the MD5 of the content.
	content := []byte("assembled multipart content")
	mpETag := "0123456789abcdef0123456789abcdef-3"
	sidecar := map[string]string{
		"size":         fmt.Sprintf("%d", len(content)),
		"etag":         mpETag,
		"content-type": "application/octet-stream",
	}
	require.NoError(t, backend.Put(ctx, om.getObjectPath(bucketName, "multi.bin"), bytes.NewReader(content), sidecar))
	// filesystem.Put overwrites etag with the content MD5 — force the
	// multipart-format value back the way completeMultipartUpload does.
	stored, err := backend.GetMetadata(ctx, om.getObjectPath(bucketName, "multi.bin"))
	require.NoError(t, err)
	stored["etag"] = mpETag
	require.NoError(t, backend.SetMetadata(ctx, om.getObjectPath(bucketName, "multi.bin"), stored))

	converted, _, err := om.EncryptExistingObject(ctx, bucketName, "multi.bin")
	require.NoError(t, err)
	assert.Equal(t, 1, converted)

	meta, err := backend.GetMetadata(ctx, om.getObjectPath(bucketName, "multi.bin"))
	require.NoError(t, err)
	assert.Equal(t, mpETag, meta["original-etag"], "multipart ETag format must be preserved")

	_, reader, err := om.GetObject(ctx, bucketName, "multi.bin")
	require.NoError(t, err)
	defer reader.Close()
	readBack, _ := io.ReadAll(reader)
	assert.Equal(t, content, readBack)
}

func TestEncryptExistingObject_ConvertsAllVersions(t *testing.T) {
	ctx := context.Background()
	om, backend, metaStore := setupManagerWithConfigKey(t)

	bucketName := "versioned-migrate-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: bucketName, OwnerID: "u",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
	}))

	// Two plaintext versions written directly (pre-envelope deployment).
	for i, body := range [][]byte{[]byte("version one plaintext"), []byte("version two plaintext")} {
		versionID := fmt.Sprintf("100000000000000000%d.abcdef1%d", i, i)
		path := om.getVersionedObjectPath(bucketName, "doc.txt", versionID)
		md5sum := md5.Sum(body)
		sidecar := map[string]string{
			"size": fmt.Sprintf("%d", len(body)), "etag": hex.EncodeToString(md5sum[:]), "content-type": "text/plain",
		}
		require.NoError(t, backend.Put(ctx, path, bytes.NewReader(body), sidecar))

		metaObj := &metadata.ObjectMetadata{
			Bucket: bucketName, Key: "doc.txt", VersionID: versionID,
			Size: int64(len(body)), ETag: hex.EncodeToString(md5sum[:]), ContentType: "text/plain",
		}
		version := &metadata.ObjectVersion{
			VersionID: versionID, IsLatest: i == 1, Key: "doc.txt",
			Size: int64(len(body)), ETag: hex.EncodeToString(md5sum[:]),
		}
		require.NoError(t, metaStore.PutObjectVersion(ctx, metaObj, version))
	}

	converted, _, err := om.EncryptExistingObject(ctx, bucketName, "doc.txt")
	require.NoError(t, err)
	assert.Equal(t, 2, converted, "both stored versions must be converted")

	// Latest version reads back decrypted.
	_, reader, err := om.GetObject(ctx, bucketName, "doc.txt")
	require.NoError(t, err)
	readBack, _ := io.ReadAll(reader)
	reader.Close()
	assert.Equal(t, []byte("version two plaintext"), readBack)

	// Specific older version too.
	_, reader, err = om.GetObject(ctx, bucketName, "doc.txt", "1000000000000000000.abcdef10")
	require.NoError(t, err)
	readBack, _ = io.ReadAll(reader)
	reader.Close()
	assert.Equal(t, []byte("version one plaintext"), readBack)
}
