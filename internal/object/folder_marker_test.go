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

func TestFolderMarkerAndSameNamedObjectCoexist(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket = "folder-coexist"
	const payload = "PAYLOAD-THAT-MATTERS"

	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucket, OwnerID: "u"}))

	_, err := om.PutObject(ctx, bucket, "report", bytes.NewReader([]byte(payload)), http.Header{})
	require.NoError(t, err)

	_, err = om.PutObject(ctx, bucket, "report/", bytes.NewReader(nil), http.Header{})
	require.NoError(t, err)

	_, err = om.PutObject(ctx, bucket, "report/inside.txt", bytes.NewReader([]byte("child")), http.Header{})
	require.NoError(t, err)

	obj, rc, err := om.GetObject(ctx, bucket, "report")
	require.NoError(t, err)
	defer rc.Close()

	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, payload, string(body))
	require.Equal(t, int64(len(payload)), obj.Size)

	res, err := om.ListObjects(ctx, bucket, "", "", "", 100)
	require.NoError(t, err)
	keys := make([]string, 0, len(res.Objects))
	for _, o := range res.Objects {
		keys = append(keys, o.Key)
	}
	require.ElementsMatch(t, []string{"report", "report/", "report/inside.txt"}, keys)
}
