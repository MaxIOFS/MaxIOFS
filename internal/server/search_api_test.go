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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSearchObjects(t *testing.T) {
	server := getSharedServer()

	testCtx := context.Background()
	tenantID := "test-tenant-search"
	bucketName := "test-bucket-search"

	cleanupTestData(t, tenantID, bucketName)

	// Create tenant
	tenant := &auth.Tenant{
		ID:              tenantID,
		Name:            "Test Tenant Search",
		Status:          "active",
		MaxStorageBytes: 1000000000,
		MaxBuckets:      100,
		MaxAccessKeys:   10,
	}
	err := server.authManager.CreateTenant(testCtx, tenant)
	require.NoError(t, err)

	// Create bucket
	err = server.bucketManager.CreateBucket(testCtx, tenantID, bucketName, "")
	require.NoError(t, err)

	// Upload objects with different content types
	headers := http.Header{}
	headers.Set("Content-Type", "image/jpeg")
	_, err = server.objectManager.PutObject(testCtx, tenantID+"/"+bucketName, "photo.jpg",
		bytes.NewReader([]byte("fake jpeg image data here")), headers)
	require.NoError(t, err)

	headers = http.Header{}
	headers.Set("Content-Type", "image/png")
	_, err = server.objectManager.PutObject(testCtx, tenantID+"/"+bucketName, "logo.png",
		bytes.NewReader([]byte("fake png data")), headers)
	require.NoError(t, err)

	headers = http.Header{}
	headers.Set("Content-Type", "text/plain")
	_, err = server.objectManager.PutObject(testCtx, tenantID+"/"+bucketName, "readme.txt",
		bytes.NewReader([]byte("hello world")), headers)
	require.NoError(t, err)

	headers = http.Header{}
	headers.Set("Content-Type", "application/pdf")
	_, err = server.objectManager.PutObject(testCtx, tenantID+"/"+bucketName, "document.pdf",
		bytes.NewReader(bytes.Repeat([]byte("x"), 5000)), headers)
	require.NoError(t, err)

	t.Run("should require authentication", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/buckets/"+bucketName+"/objects/search", nil)
		req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})

		rr := httptest.NewRecorder()
		server.handleSearchObjects(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("should return all objects without filters", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/api/v1/buckets/"+bucketName+"/objects/search", nil, tenantID, "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})

		rr := httptest.NewRecorder()
		server.handleSearchObjects(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.True(t, response["success"].(bool))

		data := response["data"].(map[string]interface{})
		objects := data["objects"].([]interface{})
		assert.Len(t, objects, 4)
	})

	t.Run("should filter by content_type", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/api/v1/buckets/"+bucketName+"/objects/search?content_type=image/", nil, tenantID, "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})

		rr := httptest.NewRecorder()
		server.handleSearchObjects(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		data := response["data"].(map[string]interface{})
		objects := data["objects"].([]interface{})
		assert.Len(t, objects, 2, "Should return 2 image objects")
	})

	t.Run("should filter by min_size", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/api/v1/buckets/"+bucketName+"/objects/search?min_size=100", nil, tenantID, "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})

		rr := httptest.NewRecorder()
		server.handleSearchObjects(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		data := response["data"].(map[string]interface{})
		objects := data["objects"].([]interface{})
		assert.Len(t, objects, 1, "Should return 1 object with size >= 100")
	})

	t.Run("should handle non-existent bucket", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/api/v1/buckets/nonexistent/objects/search", nil, tenantID, "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"bucket": "nonexistent"})

		rr := httptest.NewRecorder()
		server.handleSearchObjects(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("should combine content_type and size filters", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/api/v1/buckets/"+bucketName+"/objects/search?content_type=image/&min_size=20", nil, tenantID, "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})

		rr := httptest.NewRecorder()
		server.handleSearchObjects(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		data := response["data"].(map[string]interface{})
		objects := data["objects"].([]interface{})
		assert.GreaterOrEqual(t, len(objects), 1, "Should return images with size >= 20")
	})
}
