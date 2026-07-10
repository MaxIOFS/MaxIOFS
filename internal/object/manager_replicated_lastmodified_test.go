package object

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A replica write must record the PRIMARY's LastModified (delivered through
// the replication context), not the local receive time — otherwise the
// anti-entropy LWW comparison sees permanently divergent timestamps.
func TestReplicaWriteKeepsPrimaryLastModified(t *testing.T) {
	ctx := context.Background()
	om, _, metaStore := setupManagerWithConfigKey(t)

	bucketName := "replica-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	primaryTime := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	repCtx := WithReplicatedLastModified(ctx, primaryTime)

	obj, err := om.PutObject(repCtx, bucketName, "from-primary.txt",
		bytes.NewReader([]byte("replica payload")), http.Header{})
	require.NoError(t, err)
	assert.True(t, obj.LastModified.Equal(primaryTime),
		"returned object: expected primary LastModified %v, got %v", primaryTime, obj.LastModified)

	stored, err := metaStore.GetObject(ctx, bucketName, "from-primary.txt")
	require.NoError(t, err)
	assert.True(t, stored.LastModified.Equal(primaryTime),
		"stored metadata: expected primary LastModified %v, got %v", primaryTime, stored.LastModified)

	// A normal write (no replication context) still gets a current timestamp.
	before := time.Now().Add(-time.Minute)
	obj2, err := om.PutObject(ctx, bucketName, "local.txt",
		bytes.NewReader([]byte("local payload")), http.Header{})
	require.NoError(t, err)
	assert.True(t, obj2.LastModified.After(before),
		"local write: expected recent LastModified, got %v", obj2.LastModified)
}
