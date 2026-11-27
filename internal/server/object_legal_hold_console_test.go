package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A legal hold is put on and taken off an object of a bucket with Object Lock;
// a bucket without it has none to put.
func TestALegalHoldIsSetInALockedBucket(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	lockedBucket(t, s, "acme", "locked")
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "plain", tenantAdmin.ID))
	for _, bucketPath := range []string{"acme/locked", "acme/plain"} {
		_, err := s.objectManager.PutObject(ctx, bucketPath, "doc", bytes.NewReader([]byte("x")), http.Header{})
		require.NoError(t, err)
	}
	hold := func(bucketName, key, body string, actions ...string) int {
		req := mux.SetURLVars(consoleRequestWith(tenantAdmin, body, actions...), map[string]string{"bucket": bucketName, "object": key})
		rec := httptest.NewRecorder()
		s.handlePutObjectLegalHold(rec, req)
		return rec.Code
	}
	held := func() string {
		config, err := s.objectManager.GetObjectLegalHold(ctx, "acme/locked", "doc")
		require.NoError(t, err)
		if config == nil {
			return ""
		}
		return config.Status
	}

	rec := httptest.NewRecorder()
	s.handlePutObjectLegalHold(rec, mux.SetURLVars(httptest.NewRequest(http.MethodPut, "/", nil), map[string]string{"bucket": "locked", "object": "doc"}))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, http.StatusForbidden, hold("locked", "doc", `{"status":"ON"}`))
	assert.Equal(t, http.StatusBadRequest, hold("locked", "doc", `{"status":"on"}`, "s3:*"))
	assert.Equal(t, http.StatusBadRequest, hold("locked", "doc", `not json`, "s3:*"))
	assert.Equal(t, http.StatusBadRequest, hold("plain", "doc", `{"status":"ON"}`, "s3:*"), "no Object Lock")
	assert.Equal(t, http.StatusNotFound, hold("locked", "nowhere", `{"status":"ON"}`, "s3:*"))

	require.Equal(t, http.StatusOK, hold("locked", "doc", `{"status":"ON"}`, "s3:*"))
	assert.Equal(t, "ON", held())
	require.Equal(t, http.StatusOK, hold("locked", "doc", `{"status":"OFF"}`, "s3:*"))
	assert.Equal(t, "OFF", held())
}
