package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A bucket is deleted empty by who may delete it; only a global
// administrator deletes one with objects, by force, or one with Object Lock;
// nobody deletes one an object holds by COMPLIANCE retention.
func TestABucketIsDeletedOnlyWhenNothingKeepsIt(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	admin := storedUser(t, s, "admin")
	remove := func(user *auth.User, name, query string, actions ...string) *httptest.ResponseRecorder {
		req := mux.SetURLVars(consoleRequestWith(user, "", actions...), map[string]string{"bucket": name})
		req.URL.RawQuery = query
		rec := httptest.NewRecorder()
		s.handleDeleteBucket(rec, req)
		return rec
	}
	exists := func(tenantID, name string) bool {
		_, err := s.bucketManager.GetBucketInfo(ctx, tenantID, name)
		return err == nil
	}
	put := func(bucketPath, key string, headers http.Header) {
		_, err := s.objectManager.PutObject(ctx, bucketPath, key, bytes.NewReader([]byte("kept")), headers)
		require.NoError(t, err)
	}
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "acme-empty", tenantAdmin.ID))
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "acme-full", tenantAdmin.ID))
	put("acme/acme-full", "a", http.Header{})
	lockedBucket(t, s, "acme", "acme-locked")
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "global-full", ""))
	put("global-full", "a", http.Header{})
	lockedBucket(t, s, "", "global-worm")
	put("global-worm", "kept", http.Header{
		"X-Amz-Object-Lock-Mode":              {"COMPLIANCE"},
		"X-Amz-Object-Lock-Retain-Until-Date": {time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)},
	})

	rec := httptest.NewRecorder()
	s.handleDeleteBucket(rec, mux.SetURLVars(httptest.NewRequest(http.MethodDelete, "/", nil), map[string]string{"bucket": "acme-empty"}))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	for _, tc := range []struct {
		user    *auth.User
		name    string
		query   string
		actions []string
		status  int
	}{
		{tenantAdmin, "acme-empty", "", nil, http.StatusForbidden},
		{tenantAdmin, "acme-full", "force=true", []string{"s3:*"}, http.StatusForbidden},
		{tenantAdmin, "acme-full", "", []string{"s3:*"}, http.StatusConflict},
		{tenantAdmin, "acme-locked", "", []string{"s3:*"}, http.StatusForbidden},
		{tenantAdmin, "nowhere", "", []string{"s3:*"}, http.StatusNotFound},
		{admin, "global-full", "", []string{"s3:*"}, http.StatusConflict},
		{admin, "global-worm", "force=true", []string{"s3:*"}, http.StatusConflict},
	} {
		assert.Equal(t, tc.status, remove(tc.user, tc.name, tc.query, tc.actions...).Code, "%s %s", tc.name, tc.query)
	}
	for tenantID, name := range map[string]string{"acme": "acme-full", "": "global-worm"} {
		assert.True(t, exists(tenantID, name), "%s is kept", name)
	}
	assert.True(t, exists("acme", "acme-locked"))

	assert.Equal(t, http.StatusNoContent, remove(tenantAdmin, "acme-empty", "", "s3:*").Code)
	assert.False(t, exists("acme", "acme-empty"))
	assert.Equal(t, http.StatusNoContent, remove(admin, "global-full", "force=true", "s3:*").Code)
	assert.False(t, exists("", "global-full"))
}
