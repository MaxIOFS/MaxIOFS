package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsoleObjectWithACustomerKey: the console marks an object encrypted
// with a customer key and answers 409 to a download of it; an object of the
// same bucket written without one downloads.
func TestConsoleObjectWithACustomerKey(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	tenantID, bucketName := "test-tenant-ssec", "test-bucket-ssec"
	cleanupTestData(t, tenantID, bucketName)
	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{
		ID: tenantID, Name: "SSE-C", Status: "active", MaxStorageBytes: 1 << 30, MaxBuckets: 10, MaxAccessKeys: 10,
	}))
	require.NoError(t, server.bucketManager.CreateBucket(ctx, tenantID, bucketName, ""))
	path := tenantID + "/" + bucketName
	key := bytes.Repeat([]byte{0x77}, object.CustomerKeySize)
	_, err := server.objectManager.PutObject(object.WithWriteKey(ctx, key), path, "secret.txt", bytes.NewReader([]byte("secret")), http.Header{})
	require.NoError(t, err)
	_, err = server.objectManager.PutObject(ctx, path, "plain.txt", bytes.NewReader([]byte("plain")), http.Header{})
	require.NoError(t, err)

	get := func(objectKey string, asJSON bool) *httptest.ResponseRecorder {
		req := createAuthenticatedRequest("GET", "/api/v1/buckets/"+bucketName+"/objects/"+objectKey, nil, tenantID, "user-1", false)
		if asJSON {
			req.Header.Set("Accept", "application/json")
		}
		req = mux.SetURLVars(req, map[string]string{"bucket": bucketName, "object": objectKey})
		rr := httptest.NewRecorder()
		server.handleGetObject(rr, req)
		return rr
	}

	download := get("secret.txt", false)
	assert.Equal(t, http.StatusConflict, download.Code, download.Body.String())
	assert.NotContains(t, download.Body.String(), "secret\"")

	plain := get("plain.txt", false)
	require.Equal(t, http.StatusOK, plain.Code)
	assert.Equal(t, "plain", plain.Body.String())

	for objectKey, marked := range map[string]bool{"secret.txt": true, "plain.txt": false} {
		meta := get(objectKey, true)
		require.Equal(t, http.StatusOK, meta.Code)
		var response struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(meta.Body.Bytes(), &response))
		assert.Equal(t, marked, response.Data["customerKey"] == true, objectKey)
	}
}
