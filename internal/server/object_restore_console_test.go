package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A version is restored as the object's latest; a delete marker with a version
// behind it is removed. A version that is not a delete marker is never removed
// as one, whatever the request says it is.
func TestAVersionIsRestoredAndOnlyAMarkerIsRemoved(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "history", tenantAdmin.ID))
	require.NoError(t, s.bucketManager.SetVersioning(ctx, "acme", "history", &bucket.VersioningConfig{Status: "Enabled"}))
	put := func(content string) string {
		written, err := s.objectManager.PutObject(ctx, "acme/history", "doc", bytes.NewReader([]byte(content)), http.Header{})
		require.NoError(t, err)
		return written.VersionID
	}
	latest := func() string {
		_, reader, err := s.objectManager.GetObject(ctx, "acme/history", "doc")
		if err != nil {
			return ""
		}
		defer reader.Close()
		content, err := io.ReadAll(reader)
		require.NoError(t, err)
		return string(content)
	}
	versionExists := func(id string) bool {
		versions, err := s.objectManager.GetObjectVersions(ctx, "acme/history", "doc")
		require.NoError(t, err)
		for _, v := range versions {
			if v.VersionID == id {
				return true
			}
		}
		return false
	}
	restore := func(user *auth.User, body string, actions ...string) *httptest.ResponseRecorder {
		req := mux.SetURLVars(consoleRequestWith(user, body, actions...), map[string]string{"bucket": "history", "object": "doc"})
		rec := httptest.NewRecorder()
		s.handleRestoreObjectVersion(rec, req)
		return rec
	}

	first, second := put("first"), put("second")
	rec := httptest.NewRecorder()
	s.handleRestoreObjectVersion(rec, mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/", nil), map[string]string{"bucket": "history", "object": "doc"}))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, http.StatusBadRequest, restore(tenantAdmin, `{}`, "s3:*").Code)
	assert.Equal(t, http.StatusForbidden, restore(tenantAdmin, `{"versionId":"`+first+`"}`).Code)
	assert.Equal(t, http.StatusForbidden, restore(tenantAdmin, `{"versionId":"`+first+`"}`, "s3:GetObjectVersion").Code, "read, not write")
	assert.Equal(t, http.StatusForbidden, restore(tenantAdmin, `{"versionId":"`+first+`"}`, "s3:PutObject").Code, "write, not read")
	assert.Equal(t, http.StatusNotFound, restore(tenantAdmin, `{"versionId":"no-such-version","isDeleteMarker":true}`, "s3:*").Code)

	rec = restore(tenantAdmin, `{"versionId":"`+first+`","isDeleteMarker":true}`, "s3:*")
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a version with data is not a delete marker: %s", rec.Body.String())
	assert.True(t, versionExists(first), "it is kept")

	require.Equal(t, http.StatusOK, restore(tenantAdmin, `{"versionId":"`+first+`"}`, "s3:*").Code)
	assert.Equal(t, "first", latest(), "the version restored is the latest")
	assert.True(t, versionExists(second))
	assert.Equal(t, http.StatusNotFound, restore(tenantAdmin, `{"versionId":"no-such-version"}`, "s3:*").Code)

	marker, err := s.objectManager.DeleteObject(ctx, "acme/history", "doc", false)
	require.NoError(t, err)
	assert.Empty(t, latest(), "deleted")
	require.Equal(t, http.StatusOK, restore(tenantAdmin, `{"versionId":"`+marker+`","isDeleteMarker":true}`, "s3:*").Code)
	assert.Equal(t, "first", latest(), "the marker removed, the object is back")
	assert.False(t, versionExists(marker))

	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "lonely", tenantAdmin.ID))
	require.NoError(t, s.bucketManager.SetVersioning(ctx, "acme", "lonely", &bucket.VersioningConfig{Status: "Enabled"}))
	alone, err := s.objectManager.DeleteObject(ctx, "acme/lonely", "doc", false)
	require.NoError(t, err)
	req := mux.SetURLVars(consoleRequestWith(tenantAdmin, `{"versionId":"`+alone+`","isDeleteMarker":true}`, "s3:*"),
		map[string]string{"bucket": "lonely", "object": "doc"})
	rec = httptest.NewRecorder()
	s.handleRestoreObjectVersion(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code, "nothing behind the marker")
}
