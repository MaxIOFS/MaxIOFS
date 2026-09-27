package s3compat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tenantQuotaSetter interface {
	SetAuthManager(am interface {
		IncrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error
		DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error
		CheckTenantStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) error
	})
}

// A tenant quota refusal inside the write — after the handler's own check let
// an unknown length through — answers 403 QuotaExceeded, not 500.
func TestPutRefusedByTenantQuotaInsideTheWrite(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()
	setter, ok := env.objectManager.(tenantQuotaSetter)
	require.True(t, ok)
	setter.SetAuthManager(env.authManager)
	tenant, err := env.authManager.GetTenant(ctx, env.tenantID)
	require.NoError(t, err)
	tenant.MaxStorageBytes = 16
	require.NoError(t, env.authManager.UpdateTenant(ctx, tenant))

	req, w := env.makeS3Request("PUT", "/quota-put", nil)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	req, w = env.makeS3Request("PUT", "/quota-put/k", bytes.Repeat([]byte("x"), 32))
	req.ContentLength = -1
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "<Code>QuotaExceeded</Code>")
}

// A copy into a bucket that is full answers 403 QuotaExceeded, not 500.
func TestCopyRefusedByBucketQuota(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	for _, b := range []string{"copy-src", "copy-full"} {
		req, w := env.makeS3Request("PUT", "/"+b, nil)
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	req, w := env.makeS3Request("PUT", "/copy-src/big", bytes.Repeat([]byte("x"), 64))
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	full, err := env.metadataStore.GetBucket(context.Background(), env.tenantID, "copy-full")
	require.NoError(t, err)
	full.Quota = &metadata.BucketQuota{MaxSizeBytes: 10}
	require.NoError(t, env.metadataStore.UpdateBucket(context.Background(), full))

	req, w = env.makeS3Request("PUT", "/copy-full/big", nil)
	req.Header.Set("x-amz-copy-source", "/copy-src/big")
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "<Code>QuotaExceeded</Code>")
}

// Only a quota refusal is QuotaExceeded; a failure to check the quota is not.
func TestQuotaExceededIsOnlyAQuotaRefusal(t *testing.T) {
	assert.True(t, isQuotaExceeded(object.ErrBucketQuotaExceeded))
	assert.True(t, isQuotaExceeded(fmt.Errorf("storage quota exceeded: %w",
		fmt.Errorf("%w: 20/16 bytes", auth.ErrStorageQuotaExceeded))))
	assert.False(t, isQuotaExceeded(fmt.Errorf("storage quota exceeded: %w",
		fmt.Errorf("failed to get tenant: %w", errors.New("database is locked")))))
}
