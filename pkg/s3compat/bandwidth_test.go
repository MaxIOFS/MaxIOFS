package s3compat

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bandwidth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A tenant's bandwidth cap paces every S3 transfer to its buckets: a PUT, a
// part of a multipart upload, a GET, a range GET and a browser form upload. The form upload
// was read at full speed.
func TestTenantBandwidthCapsS3Transfers(t *testing.T) {
	env := setupPresignedPostEnv(t)
	defer env.cleanup()
	ctx := context.Background()
	env.handler.SetBandwidthManager(bandwidth.NewManager())

	const perSecond = 512 * 1024
	tenant, err := env.authManager.GetTenant(ctx, env.tenantID)
	require.NoError(t, err)
	tenant.MaxBandwidthBytesPerSec = perSecond
	require.NoError(t, env.authManager.UpdateTenant(ctx, tenant))
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "paced", env.userID))

	// 1 MiB against 512 KiB/s with a burst of one second: at least one second.
	payload := bytes.Repeat([]byte("x"), 2*perSecond)
	const atLeast = 700 * time.Millisecond
	timed := func(req *http.Request) (time.Duration, *httptest.ResponseRecorder) {
		t.Helper()
		time.Sleep(time.Second) // the burst refills
		w := httptest.NewRecorder()
		start := time.Now()
		env.router.ServeHTTP(w, req)
		return time.Since(start), w
	}

	t.Run("PUT", func(t *testing.T) {
		req, _ := env.makeS3Request("PUT", "/paced/put", payload)
		took, w := timed(req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.GreaterOrEqual(t, took, atLeast)
	})

	t.Run("part", func(t *testing.T) {
		req, w := env.makeS3Request("POST", "/paced/parts?uploads", nil)
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var created struct {
			UploadId string `xml:"UploadId"`
		}
		require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &created))
		req, _ = env.makeS3Request("PUT", "/paced/parts?partNumber=1&uploadId="+created.UploadId, payload)
		took, w := timed(req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.GreaterOrEqual(t, took, atLeast)
	})

	t.Run("GET", func(t *testing.T) {
		req, _ := env.makeS3Request("GET", "/paced/put", nil)
		took, w := timed(req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, len(payload), w.Body.Len())
		assert.GreaterOrEqual(t, took, atLeast)
	})

	t.Run("range GET", func(t *testing.T) {
		req, _ := env.makeS3Request("GET", "/paced/put", nil)
		req.Header.Set("Range", "bytes=0-1048575")
		took, w := timed(req)
		require.Equal(t, http.StatusPartialContent, w.Code, w.Body.String())
		require.Equal(t, 1048576, w.Body.Len())
		assert.GreaterOrEqual(t, took, atLeast)
	})

	t.Run("form upload", func(t *testing.T) {
		req := buildPostPresignedRequest(t, "paced", "form", payload, "", env.accessKey, env.secretKey, nil, nil)
		took, w := timed(req)
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
		assert.GreaterOrEqual(t, took, atLeast)
	})
}
