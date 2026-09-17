package object

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Permissions are evaluated on the key the caller named, so that key is the one
// that must be deleted. Resolving "folder" to "folder/" inside the manager let a
// request authorized for one object remove a different one.
func TestDeleteObject_DeletesOnlyTheKeyItWasGiven(t *testing.T) {
	ctx := context.Background()
	om, _, metaStore := setupManagerWithConfigKey(t)

	bucketName := "delete-fidelity-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	_, err := om.PutObject(ctx, bucketName, "folder/", bytes.NewReader(nil), http.Header{})
	require.NoError(t, err)

	_, _ = om.DeleteObject(ctx, bucketName, "folder", false)

	marker, err := metaStore.GetObject(ctx, bucketName, "folder/")
	require.NoError(t, err, "deleting \"folder\" must not remove \"folder/\"")
	assert.NotNil(t, marker)

	_, err = om.DeleteObject(ctx, bucketName, "folder/", false)
	require.NoError(t, err)
	_, err = metaStore.GetObject(ctx, bucketName, "folder/")
	assert.Error(t, err, "naming the marker explicitly must delete it")
}
