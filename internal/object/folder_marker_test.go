package object

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

func TestFolderMarkerDoesNotDestroySameNamedObject(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket = "folder-conflict"
	const payload = "PAYLOAD-THAT-MATTERS"

	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucket, OwnerID: "u"}))

	_, err := om.PutObject(ctx, bucket, "report", bytes.NewReader([]byte(payload)), http.Header{})
	require.NoError(t, err)

	_, err = om.PutObject(ctx, bucket, "report/", bytes.NewReader(nil), http.Header{})
	require.Error(t, err)

	obj, rc, err := om.GetObject(ctx, bucket, "report")
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, payload, string(got))
	require.Equal(t, int64(len(payload)), obj.Size)
}
