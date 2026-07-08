package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHAReceiveRawPut exercises the receiving side of ciphertext replication:
// an object written locally (envelope, cluster-shared KEK) is read raw and
// re-ingested through the HA raw endpoint under a different key — as if it
// arrived from a peer node sharing the same KEK. The replica copy must be
// byte-identical ciphertext and decrypt on read.
func TestHAReceiveRawPut(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()

	// Make the current KEK cluster-shared (what the first join does).
	_, err := server.kekStore.EnsureClusterKey()
	require.NoError(t, err)

	bucketName := "raw-ha-bucket"
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: bucketName, OwnerID: "admin",
	}))
	cleanupTestData(t, "", bucketName)

	content := bytes.Repeat([]byte("ha raw replication payload "), 2000)
	_, err = server.objectManager.PutObject(ctx, bucketName, "source.bin", bytes.NewReader(content), http.Header{})
	require.NoError(t, err)

	raw, ok := server.objectManager.(object.RawObjectAccessor)
	require.True(t, ok)

	reader, sidecar, metaObj, err := raw.GetObjectRaw(ctx, bucketName, "source.bin", "")
	require.NoError(t, err)
	rawBytes, err := io.ReadAll(reader)
	reader.Close()
	require.NoError(t, err)
	require.True(t, raw.CanReplicateRaw(sidecar), "object must be wrapped with the cluster-shared KEK")

	// Re-ingest as a raw replica under a different key (simulating the peer).
	metaObj.Key = "replicated.bin"
	sidecarJSON, _ := json.Marshal(sidecar)
	metaJSON, _ := json.Marshal(metaObj)

	req := httptest.NewRequest("PUT", "/api/internal/ha/objects/replicated.bin", bytes.NewReader(rawBytes))
	req = mux.SetURLVars(req, map[string]string{"key": "replicated.bin"})
	req.Header.Set(cluster.HABucketHeader, bucketName)
	req.Header.Set(cluster.HARawHeader, "true")
	req.Header.Set(cluster.HARawSidecarHeader, base64.StdEncoding.EncodeToString(sidecarJSON))
	req.Header.Set(cluster.HARawObjectMetaHeader, base64.StdEncoding.EncodeToString(metaJSON))

	w := httptest.NewRecorder()
	server.handleHAReceivePut(w, req)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	// The replica stores identical ciphertext…
	replicaRaw, replicaSidecar, _, err := raw.GetObjectRaw(ctx, bucketName, "replicated.bin", "")
	require.NoError(t, err)
	replicaBytes, _ := io.ReadAll(replicaRaw)
	replicaRaw.Close()
	assert.Equal(t, rawBytes, replicaBytes, "replica ciphertext must be byte-identical (no re-encryption)")
	assert.Equal(t, sidecar["wrapped-dek"], replicaSidecar["wrapped-dek"])

	// …and serves the decrypted content.
	obj, dec, err := server.objectManager.GetObject(ctx, bucketName, "replicated.bin")
	require.NoError(t, err)
	readBack, _ := io.ReadAll(dec)
	dec.Close()
	assert.Equal(t, content, readBack)
	assert.Equal(t, int64(len(content)), obj.Size)
}

// TestHAReceiveRawPut_RejectsUnknownKEK: a raw transfer wrapped with a KEK
// version this node does not share must be declined with 412 so the primary
// falls back to the legacy path.
func TestHAReceiveRawPut_RejectsUnknownKEK(t *testing.T) {
	server := getSharedServer()

	sidecar := map[string]string{
		"encrypted": "true", "wrapped-dek": "abcd", "wrapped-dek-iv": "0011", "kek-version": "99",
	}
	metaObj := &metadata.ObjectMetadata{Bucket: "any", Key: "k", Size: 4, ETag: "e"}
	sidecarJSON, _ := json.Marshal(sidecar)
	metaJSON, _ := json.Marshal(metaObj)

	req := httptest.NewRequest("PUT", "/api/internal/ha/objects/k", bytes.NewReader([]byte("data")))
	req = mux.SetURLVars(req, map[string]string{"key": "k"})
	req.Header.Set(cluster.HABucketHeader, "any")
	req.Header.Set(cluster.HARawHeader, "true")
	req.Header.Set(cluster.HARawSidecarHeader, base64.StdEncoding.EncodeToString(sidecarJSON))
	req.Header.Set(cluster.HARawObjectMetaHeader, base64.StdEncoding.EncodeToString(metaJSON))

	w := httptest.NewRecorder()
	server.handleHAReceivePut(w, req)
	assert.Equal(t, http.StatusPreconditionFailed, w.Code)
}
