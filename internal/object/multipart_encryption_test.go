package object

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// partOf is n bytes of a part, the marker repeated, at least the 5 MiB a part
// but the last has.
func partOf(marker string, n int) []byte {
	return bytes.Repeat([]byte(marker), n/len(marker)+1)[:n]
}

const minPart = 5 * 1024 * 1024

// storedPart is the bytes of a part as stored.
func storedPart(t *testing.T, om *objectManager, uploadID string, number int) ([]byte, map[string]string) {
	t.Helper()
	reader, meta, err := om.storage.GetPart(context.Background(), uploadID, number)
	require.NoError(t, err)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return data, meta
}

func readWith(t *testing.T, om *objectManager, ctx context.Context, bucket, key string) []byte {
	t.Helper()
	_, body, err := om.GetObject(ctx, bucket, key)
	require.NoError(t, err)
	defer body.Close()
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	return data
}

// TestPartsAreEncryptedAtRest: a part is stored encrypted with its upload's
// key; its ETag and size are those of what was sent.
func TestPartsAreEncryptedAtRest(t *testing.T) {
	om := customerKeyManager(t)
	ctx := context.Background()
	upload, err := om.CreateMultipartUpload(ctx, "ssec", "big", http.Header{})
	require.NoError(t, err)

	first, second := partOf("PART-ONE-MARKER-", minPart), partOf("PART-TWO-MARKER-", 1000)
	p1, err := om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(first))
	require.NoError(t, err)
	p2, err := om.UploadPart(ctx, upload.UploadID, 2, bytes.NewReader(second))
	require.NoError(t, err)

	sum := md5.Sum(first)
	assert.Equal(t, hex.EncodeToString(sum[:]), p1.ETag)
	assert.Equal(t, int64(len(first)), p1.Size)
	stored, meta := storedPart(t, om, upload.UploadID, 1)
	assert.Equal(t, "true", meta["encrypted"])
	assert.False(t, bytes.Contains(stored, []byte("PART-ONE-MARKER-")), "the part is not stored as sent")

	listed, err := om.ListParts(ctx, upload.UploadID)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, p1.ETag, listed[0].ETag)
	assert.Equal(t, int64(len(first)), listed[0].Size)

	obj, err := om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*p1, *p2})
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(obj.ETag, "-2"), obj.ETag)
	assert.Equal(t, append(first, second...), readWith(t, om, ctx, "ssec", "big"))
}

// TestUploadStartedBeforePartsWereEncrypted: an upload with no key of its own
// keeps storing its parts as sent, and completes.
func TestUploadStartedBeforePartsWereEncrypted(t *testing.T) {
	om := customerKeyManager(t)
	ctx := context.Background()
	require.NoError(t, om.metadataStore.CreateMultipartUpload(ctx, &metadata.MultipartUploadMetadata{
		UploadID: "old-upload", Bucket: "ssec", Key: "old", Initiated: time.Now(), OwnerID: "u",
	}))
	content := []byte("sent before the upgrade")
	part, err := om.UploadPart(ctx, "old-upload", 1, bytes.NewReader(content))
	require.NoError(t, err)
	stored, meta := storedPart(t, om, "old-upload", 1)
	assert.Empty(t, meta["encrypted"])
	assert.Equal(t, content, stored)

	_, err = om.CompleteMultipartUpload(ctx, "old-upload", []Part{*part})
	require.NoError(t, err)
	assert.Equal(t, content, readWith(t, om, ctx, "ssec", "old"))
}

// TestCustomerKeyUpload: an upload started with a customer key takes parts
// only with that key, completes without it, and is read only with it. The
// ETags are not of the content.
func TestCustomerKeyUpload(t *testing.T) {
	om := customerKeyManager(t)
	ctx := context.Background()
	withKey := WithWriteKey(ctx, customerKeyA)
	upload, err := om.CreateMultipartUpload(withKey, "ssec", "secret", http.Header{})
	require.NoError(t, err)

	first, second := partOf("SECRET-ONE-", minPart), partOf("SECRET-TWO-", 2000)
	_, err = om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(first))
	assert.ErrorIs(t, err, ErrCustomerKeyRequired)
	_, err = om.UploadPart(WithWriteKey(ctx, customerKeyB), upload.UploadID, 1, bytes.NewReader(first))
	assert.ErrorIs(t, err, ErrCustomerKeyMismatch)

	p1, err := om.UploadPart(withKey, upload.UploadID, 1, bytes.NewReader(first))
	require.NoError(t, err)
	p2, err := om.UploadPart(withKey, upload.UploadID, 2, bytes.NewReader(second))
	require.NoError(t, err)
	sum := md5.Sum(first)
	assert.NotEqual(t, hex.EncodeToString(sum[:]), p1.ETag, "a part's ETag is not its content's MD5")

	obj, err := om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*p1, *p2})
	require.NoError(t, err)
	assert.True(t, obj.CustomerEncrypted())
	assert.Empty(t, obj.SSEAlgorithm)
	assert.True(t, strings.HasSuffix(obj.ETag, "-2"), obj.ETag)

	_, _, err = om.GetObject(ctx, "ssec", "secret")
	assert.ErrorIs(t, err, ErrCustomerKeyRequired)
	assert.Equal(t, append(first, second...), readWith(t, om, WithReadKey(ctx, customerKeyA), "ssec", "secret"))

	// Neither the content's MD5 nor the KEK's wrap of the key is kept.
	whole := md5.Sum(append(first, second...))
	reader, sidecar, err := om.storage.Get(ctx, om.objectRef("ssec", "secret"))
	require.NoError(t, err)
	reader.Close()
	for field, value := range sidecar {
		assert.NotEqual(t, hex.EncodeToString(whole[:]), value, field)
	}
	assert.Empty(t, sidecar["wrapped-dek"])
	assert.Empty(t, sidecar["kek-version"])
	_, err = om.metadataStore.GetMultipartUpload(ctx, upload.UploadID)
	assert.Error(t, err, "the upload, with the KEK's wrap of its key, is gone")

	// A part with a key for an upload started without one is refused.
	plain, err := om.CreateMultipartUpload(ctx, "ssec", "plain", http.Header{})
	require.NoError(t, err)
	_, err = om.UploadPart(withKey, plain.UploadID, 1, bytes.NewReader([]byte("x")))
	assert.ErrorIs(t, err, ErrCustomerKeyNotApplicable)
}
