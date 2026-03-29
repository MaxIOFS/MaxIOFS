package replication

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealObjectAdapter_CopyObject_Success(t *testing.T) {
	content := []byte("hello replication")
	om := &MockObjectManager{
		GetObjectFunc: func(ctx context.Context, tenantID, bucket, key string) (io.ReadCloser, int64, string, map[string]string, error) {
			assert.Equal(t, "tenant1", tenantID)
			assert.Equal(t, "src-bucket", bucket)
			assert.Equal(t, "file.txt", key)
			return io.NopCloser(bytes.NewReader(content)), int64(len(content)), "text/plain", nil, nil
		},
	}
	adapter := NewRealObjectAdapter(om)

	size, err := adapter.CopyObject(context.Background(), "src-bucket", "file.txt", "dst-bucket", "file.txt", "tenant1")
	require.NoError(t, err)
	assert.Equal(t, int64(len(content)), size)
}

func TestRealObjectAdapter_CopyObject_ObjectNotFound(t *testing.T) {
	om := &MockObjectManager{
		GetObjectFunc: func(ctx context.Context, tenantID, bucket, key string) (io.ReadCloser, int64, string, map[string]string, error) {
			return nil, 0, "", nil, fmt.Errorf("object not found")
		},
	}
	adapter := NewRealObjectAdapter(om)

	_, err := adapter.CopyObject(context.Background(), "src-bucket", "missing.txt", "dst-bucket", "missing.txt", "tenant1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get source object")
}

func TestRealObjectAdapter_DeleteObject_Success(t *testing.T) {
	adapter := NewRealObjectAdapter(&MockObjectManager{})

	// DeleteObject is a no-op validation — always succeeds
	err := adapter.DeleteObject(context.Background(), "bucket", "file.txt", "tenant1")
	assert.NoError(t, err)
}

func TestRealObjectAdapter_GetObjectMetadata_Success(t *testing.T) {
	expectedMeta := map[string]string{"x-custom": "value"}
	om := &MockObjectManager{
		GetObjectMetadataFunc: func(ctx context.Context, tenantID, bucket, key string) (int64, string, map[string]string, error) {
			return 512, "application/json", expectedMeta, nil
		},
	}
	adapter := NewRealObjectAdapter(om)

	meta, err := adapter.GetObjectMetadata(context.Background(), "bucket", "file.json", "tenant1")
	require.NoError(t, err)
	assert.Equal(t, expectedMeta, meta)
}

func TestRealObjectAdapter_GetObjectMetadata_Error(t *testing.T) {
	om := &MockObjectManager{
		GetObjectMetadataFunc: func(ctx context.Context, tenantID, bucket, key string) (int64, string, map[string]string, error) {
			return 0, "", nil, fmt.Errorf("metadata not found")
		},
	}
	adapter := NewRealObjectAdapter(om)

	_, err := adapter.GetObjectMetadata(context.Background(), "bucket", "missing.json", "tenant1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get object metadata")
}
