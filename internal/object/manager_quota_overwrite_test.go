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

// Regression test: a quota-rejected overwrite must leave the existing object
// completely intact. The quota check used to run AFTER the store step, so on
// a non-versioned bucket the original object had already been replaced on
// disk and the rejection cleanup deleted it — destroying the only copy.
func TestQuotaRejectedOverwriteKeepsOriginalObject(t *testing.T) {
	ctx := context.Background()
	om, _, metaStore := setupManagerWithConfigKey(t)

	bucketName := "quota-bucket"
	key := "victim.txt"
	original := []byte("original content that must survive the rejected overwrite")

	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
		Quota:   &metadata.BucketQuota{MaxSizeBytes: 100},
	}))

	// First write fits within the 100-byte quota.
	require.LessOrEqual(t, len(original), 100)
	_, err := om.PutObject(ctx, bucketName, key, bytes.NewReader(original), http.Header{})
	require.NoError(t, err)

	// Overwrite attempt that exceeds the quota (delta pushes past MaxSizeBytes).
	oversized := bytes.Repeat([]byte("x"), 300)
	_, err = om.PutObject(ctx, bucketName, key, bytes.NewReader(oversized), http.Header{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBucketQuotaExceeded)

	// The original object must still exist and serve its original bytes.
	obj, reader, err := om.GetObject(ctx, bucketName, key)
	require.NoError(t, err, "original object was destroyed by the rejected overwrite")
	defer reader.Close()
	readBack, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, original, readBack)
	assert.Equal(t, int64(len(original)), obj.Size)
}

// Same scenario against the tenant quota path: the tenant quota rejection must
// also fire before the store step.
func TestTenantQuotaRejectedOverwriteKeepsOriginalObject(t *testing.T) {
	ctx := context.Background()
	om, _, metaStore := setupManagerWithConfigKey(t)
	om.SetAuthManager(rejectingAuthManager{})

	bucketPath := "tenant-a/quota-bucket"
	key := "victim.txt"
	original := []byte("tenant object that must survive")

	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     "quota-bucket",
		TenantID: "tenant-a",
		OwnerID:  "user-1",
	}))

	_, err := om.PutObject(ctx, bucketPath, key, bytes.NewReader(original), http.Header{})
	require.NoError(t, err)

	// Any growing overwrite is rejected by the tenant quota stub.
	oversized := bytes.Repeat([]byte("y"), 500)
	_, err = om.PutObject(ctx, bucketPath, key, bytes.NewReader(oversized), http.Header{})
	require.Error(t, err)

	_, reader, err := om.GetObject(ctx, bucketPath, key)
	require.NoError(t, err, "original object was destroyed by the rejected overwrite")
	defer reader.Close()
	readBack, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, original, readBack)
}

// rejectingAuthManager rejects quota increases above 100 bytes (so the small
// initial write succeeds and the oversized overwrite is rejected).
type rejectingAuthManager struct{}

func (rejectingAuthManager) CheckTenantStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) error {
	if additionalBytes > 100 {
		return ErrBucketQuotaExceeded // any error works; reuse a package sentinel
	}
	return nil
}

func (rejectingAuthManager) IncrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	return nil
}

func (rejectingAuthManager) DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	return nil
}
