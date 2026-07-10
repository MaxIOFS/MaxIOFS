package object

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"

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
// would be invisible in listings). The stored data file is kept on disk: on a
// non-versioned overwrite it is the only copy, and sidecar-only files are what
// `maxiofs recover` reindexes.
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
