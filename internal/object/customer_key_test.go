package object

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	customerKeyA = bytes.Repeat([]byte{0x5A}, CustomerKeySize)
	customerKeyB = bytes.Repeat([]byte{0x6B}, CustomerKeySize)
)

// putWithCustomerKey writes key in bucket with content, encrypted with
// customer key ck.
func putWithCustomerKey(t *testing.T, om *objectManager, bucket, key string, content []byte, ck []byte) *Object {
	t.Helper()
	obj, err := om.PutObject(WithWriteKey(context.Background(), ck), bucket, key, bytes.NewReader(content), http.Header{})
	require.NoError(t, err)
	return obj
}

func customerKeyManager(t *testing.T) *objectManager {
	t.Helper()
	om, store, cleanup := setupTestManagerWithStore(t)
	t.Cleanup(cleanup)
	require.NoError(t, store.CreateBucket(context.Background(), &metadata.BucketMetadata{Name: "ssec", OwnerID: "u"}))
	return om
}

func TestCustomerKeyRead(t *testing.T) {
	om := customerKeyManager(t)
	content := []byte("read only with the customer's key")
	obj := putWithCustomerKey(t, om, "ssec", "doc", content, customerKeyA)
	assert.True(t, obj.CustomerEncrypted())
	assert.Empty(t, obj.SSEAlgorithm, "SSE-C is not SSE-S3")
	assert.Empty(t, obj.ChecksumValue, "no digest of the content is kept")

	_, _, err := om.GetObject(context.Background(), "ssec", "doc")
	assert.ErrorIs(t, err, ErrCustomerKeyRequired)
	_, _, err = om.GetObject(WithReadKey(context.Background(), customerKeyB), "ssec", "doc")
	assert.ErrorIs(t, err, ErrCustomerKeyMismatch)

	got, body, err := om.GetObject(WithReadKey(context.Background(), customerKeyA), "ssec", "doc")
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	body.Close()
	require.NoError(t, err)
	assert.Equal(t, content, data)
	assert.Equal(t, obj.ETag, got.ETag)

	head, err := om.GetObjectMetadata(context.Background(), "ssec", "doc")
	require.NoError(t, err)
	assert.NoError(t, CheckCustomerKey(head, customerKeyA))
	assert.ErrorIs(t, CheckCustomerKey(head, nil), ErrCustomerKeyRequired)
	assert.ErrorIs(t, CheckCustomerKey(head, customerKeyB), ErrCustomerKeyMismatch)
}

// TestCustomerKeyCopyBetweenNodesIsRefused: a copy between nodes moves the
// bytes as stored; one made through a write with a customer key is refused.
func TestCustomerKeyCopyBetweenNodesIsRefused(t *testing.T) {
	om := customerKeyManager(t)
	ctx := WithWriteKey(WithReplicaCopy(context.Background()), customerKeyA)
	_, err := om.PutObject(ctx, "ssec", "doc", bytes.NewReader([]byte("x")), http.Header{})
	assert.Error(t, err)
}

// TestCustomerKeyIntegrity: the scrubber checks the bytes stored against the
// ETag without the key, and finds them changed.
func TestCustomerKeyIntegrity(t *testing.T) {
	om := customerKeyManager(t)
	putWithCustomerKey(t, om, "ssec", "doc", bytes.Repeat([]byte("data"), 50000), customerKeyA)

	result, err := om.VerifyObjectIntegrity(context.Background(), "ssec", "doc")
	require.NoError(t, err)
	assert.Equal(t, IntegrityOK, result.Status, "%+v", result)

	ref := om.objectRef("ssec", "doc")
	reader, sidecar, err := om.storage.Get(context.Background(), ref)
	require.NoError(t, err)
	stored, err := io.ReadAll(reader)
	reader.Close()
	require.NoError(t, err)
	stored[len(stored)/2] ^= 0xFF
	require.NoError(t, om.storage.Put(context.Background(), ref, bytes.NewReader(stored), sidecar))

	result, err = om.VerifyObjectIntegrity(context.Background(), "ssec", "doc")
	require.NoError(t, err)
	assert.Equal(t, IntegrityCorrupted, result.Status, "%+v", result)
}

// TestCustomerKeyMigrationLeavesItAlone: the encryption worker takes neither
// the KEK nor a rewrite to an object whose DEK a customer key wraps.
func TestCustomerKeyMigrationLeavesItAlone(t *testing.T) {
	om := customerKeyManager(t)
	content := []byte("left as written")
	obj := putWithCustomerKey(t, om, "ssec", "doc", content, customerKeyA)

	converted, skipped, err := om.EncryptExistingObject(context.Background(), "ssec", "doc")
	require.NoError(t, err)
	assert.Zero(t, converted)
	assert.Equal(t, 1, skipped)

	got, body, err := om.GetObject(WithReadKey(context.Background(), customerKeyA), "ssec", "doc")
	require.NoError(t, err)
	data, _ := io.ReadAll(body)
	body.Close()
	assert.Equal(t, content, data)
	assert.Equal(t, obj.ETag, got.ETag)
	assert.Equal(t, obj.CustomerDEK, got.CustomerDEK)
}
