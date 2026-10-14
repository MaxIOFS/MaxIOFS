package s3compat

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A bucket or tenant past its quota, as concurrent writes on several nodes can
// leave it: a write that grows usage is refused, a delete is not, even one that
// leaves usage over the limit, and an overwrite that shrinks an object is
// allowed. Once usage is under the limit again a write that fits is admitted.
func TestOverQuotaRefusesWritesButNotDeletes(t *testing.T) {
	for _, limit := range []string{"bucket", "tenant"} {
		t.Run(limit, func(t *testing.T) {
			env := setupCompleteS3Environment(t)
			defer env.cleanup()
			ctx := context.Background()
			setter, ok := env.objectManager.(tenantQuotaSetter)
			require.True(t, ok)
			setter.SetAuthManager(env.authManager)
			// The bucket's usage is kept as the server keeps it.
			counted, ok := env.objectManager.(interface {
				SetBucketManager(bm interface {
					IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
					DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
					AdjustBucketSize(ctx context.Context, tenantID, name string, sizeDelta int64) error
				})
			})
			require.True(t, ok)
			counted.SetBucketManager(env.bucketManager)

			send := func(method, path string, body []byte) (int, string) {
				t.Helper()
				req, w := env.makeS3Request(method, path, body)
				env.router.ServeHTTP(w, req)
				return w.Code, w.Body.String()
			}
			status, body := send("PUT", "/full", nil)
			require.Equal(t, http.StatusOK, status, body)
			for key, size := range map[string]int{"base": 800, "first": 300, "second": 300, "third": 300} {
				status, body := send("PUT", "/full/"+key, bytes.Repeat([]byte("x"), size))
				require.Equal(t, http.StatusOK, status, body)
			}

			// 1700 bytes stored against a limit of 1000.
			if limit == "bucket" {
				b, err := env.metadataStore.GetBucket(ctx, env.tenantID, "full")
				require.NoError(t, err)
				b.Quota = &metadata.BucketQuota{MaxSizeBytes: 1000}
				require.NoError(t, env.metadataStore.UpdateBucket(ctx, b))
			} else {
				tenant, err := env.authManager.GetTenant(ctx, env.tenantID)
				require.NoError(t, err)
				tenant.MaxStorageBytes = 1000
				require.NoError(t, env.authManager.UpdateTenant(ctx, tenant))
			}

			status, body = send("PUT", "/full/new", []byte("x"))
			require.Equal(t, http.StatusForbidden, status, body)
			assert.Contains(t, body, "<Code>QuotaExceeded</Code>")

			status, body = send("DELETE", "/full/first", nil)
			require.Equal(t, http.StatusNoContent, status, "a delete is allowed over the limit: %s", body)

			status, body = send("PUT", "/full/new", []byte("x"))
			require.Equal(t, http.StatusForbidden, status, "1400 bytes are still over the limit: %s", body)

			status, body = send("PUT", "/full/second", bytes.Repeat([]byte("y"), 100))
			require.Equal(t, http.StatusOK, status, "an overwrite that shrinks the object is allowed: %s", body)

			status, body = send("POST", "/full?delete", []byte(`<Delete><Object><Key>third</Key></Object></Delete>`))
			require.Equal(t, http.StatusOK, status, body)
			assert.Contains(t, body, "<Deleted>")
			assert.NotContains(t, body, "<Error>")

			// 900 bytes: a write of 100 fits, one more byte does not.
			status, body = send("PUT", "/full/new", bytes.Repeat([]byte("z"), 100))
			require.Equal(t, http.StatusOK, status, "under the limit again: %s", body)
			status, body = send("PUT", "/full/more", []byte("x"))
			require.Equal(t, http.StatusForbidden, status, body)
		})
	}
}
