package object

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingPutStore wraps a real store but fails every object-metadata save,
// simulating a Pebble write failure (disk full, I/O error).
type failingPutStore struct {
	metadata.Store
	failPuts bool
}

func (f *failingPutStore) PutObject(ctx context.Context, obj *metadata.ObjectMetadata) error {
	if f.failPuts {
		return fmt.Errorf("simulated pebble write failure")
	}
	return f.Store.PutObject(ctx, obj)
}

func (f *failingPutStore) PutObjectVersion(ctx context.Context, obj *metadata.ObjectMetadata, version *metadata.ObjectVersion) error {
	if f.failPuts {
		return fmt.Errorf("simulated pebble write failure")
	}
	return f.Store.PutObjectVersion(ctx, obj, version)
}

// A metadata save failure must fail the PUT (no silent 200 for a write that
func TestPutObjectFailsWhenMetadataSaveFails(t *testing.T) {
	ctx := context.Background()
	om, backend, metaStore := setupManagerWithConfigKey(t)

	bucketName := "metafail-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	failing := &failingPutStore{Store: metaStore, failPuts: true}
	om.metadataStore = failing

	_, err := om.PutObject(ctx, bucketName, "victim.txt",
		bytes.NewReader([]byte("payload")), http.Header{})
	require.Error(t, err, "a failed metadata save must fail the write")
	assert.Contains(t, err.Error(), "metadata")

	// The data file stays on disk (recover-CLI territory, never deleted).
	exists, err := backend.Exists(ctx, bucketName+"/victim.txt")
	require.NoError(t, err)
	assert.True(t, exists, "the stored data file must be kept")

	// With the store healthy again, the same write succeeds.
	failing.failPuts = false
	obj, err := om.PutObject(ctx, bucketName, "victim.txt",
		bytes.NewReader([]byte("payload")), http.Header{})
	require.NoError(t, err)
	assert.Equal(t, int64(len("payload")), obj.Size)
}

// Same contract for the raw (ciphertext) replica write path.
func TestPutObjectRawFailsWhenMetadataSaveFails(t *testing.T) {
	ctx := context.Background()
	om, _, metaStore := setupManagerWithConfigKey(t)

	bucketName := "metafail-raw-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	om.metadataStore = &failingPutStore{Store: metaStore, failPuts: true}

	metaObj := &metadata.ObjectMetadata{
		Bucket: bucketName, Key: "replica.txt",
		Size: 4, ETag: "aabbccddeeff00112233445566778899",
	}
	err := om.PutObjectRaw(ctx, bucketName, "replica.txt",
		bytes.NewReader([]byte("data")), map[string]string{"size": "4"}, metaObj)
	require.Error(t, err, "a failed replica metadata save must fail the transfer")
}

func TestCompleteMultipartUploadMetadataFailurePreservesPreviousObject(t *testing.T) {
	ctx := context.Background()
	om, _, metaStore := setupManagerWithConfigKey(t)

	bucketName := "metafail-mpu-bucket"
	key := "victim.txt"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	_, err := om.PutObject(ctx, bucketName, key,
		bytes.NewReader([]byte("previous payload")), http.Header{"Content-Type": []string{"text/plain"}})
	require.NoError(t, err)

	upload, err := om.CreateMultipartUpload(ctx, bucketName, key, http.Header{"Content-Type": []string{"text/plain"}})
	require.NoError(t, err)
	part, err := om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader([]byte("replacement payload")))
	require.NoError(t, err)

	failing := &failingPutStore{Store: metaStore, failPuts: true}
	om.metadataStore = failing
	_, err = om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*part})
	require.Error(t, err)

	failing.failPuts = false
	_, reader, err := om.GetObject(ctx, bucketName, key)
	require.NoError(t, err)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, "previous payload", string(data))
}

// kekOutage stops handing out a current KEK once armed, which is what a KEK
// store outage or a rotation window looks like to the encryption step.
type kekOutage struct {
	inner kek.Provider
	down  bool
}

func (k *kekOutage) CurrentKEK() ([]byte, int) {
	if k.down {
		return nil, 0
	}
	return k.inner.CurrentKEK()
}
func (k *kekOutage) KEKByVersion(v int) ([]byte, error) { return k.inner.KEKByVersion(v) }
func (k *kekOutage) IsClusterShared(v int) bool         { return k.inner.IsClusterShared(v) }

// The combine writes over the live object, so a failure after it must put the
// previous object back rather than delete the path it landed on.
func TestCompleteMultipartUploadEncryptionFailurePreservesPreviousObject(t *testing.T) {
	ctx := context.Background()
	om, _, metaStore := setupManagerWithConfigKey(t)

	bucketName := "mpu-encfail-bucket"
	key := "victim.txt"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	_, err := om.PutObject(ctx, bucketName, key,
		bytes.NewReader([]byte("previous payload")), http.Header{"Content-Type": []string{"text/plain"}})
	require.NoError(t, err)

	upload, err := om.CreateMultipartUpload(ctx, bucketName, key, http.Header{"Content-Type": []string{"text/plain"}})
	require.NoError(t, err)
	part, err := om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader([]byte("replacement payload")))
	require.NoError(t, err)

	outage := &kekOutage{inner: om.kekProvider}
	om.kekProvider = outage
	outage.down = true

	_, err = om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*part})
	require.Error(t, err)

	outage.down = false
	_, reader, err := om.GetObject(ctx, bucketName, key)
	require.NoError(t, err, "the object that was already there must still be readable")
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, "previous payload", string(data))
}
