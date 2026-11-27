package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An object is renamed to a key of its bucket with its data and tags, by who
// may read it, write the new key and delete the old one; one under a legal
// hold or COMPLIANCE retention is not.
func TestAnObjectIsRenamedWithWhatItHolds(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	lockedBucket(t, s, "acme", "files")
	put := func(key string, headers http.Header) {
		_, err := s.objectManager.PutObject(ctx, "acme/files", key, bytes.NewReader([]byte("content of "+key)), headers)
		require.NoError(t, err)
	}
	read := func(key string) string {
		_, reader, err := s.objectManager.GetObject(ctx, "acme/files", key)
		if err != nil {
			return ""
		}
		defer reader.Close()
		content, err := io.ReadAll(reader)
		require.NoError(t, err)
		return string(content)
	}
	rename := func(key, body string, actions ...string) int {
		req := mux.SetURLVars(consoleRequestWith(tenantAdmin, body, actions...), map[string]string{"bucket": "files", "object": key})
		rec := httptest.NewRecorder()
		s.handleRenameObject(rec, req)
		return rec.Code
	}
	put("old", http.Header{"Content-Type": {"text/plain"}})
	put("bare", http.Header{})
	require.NoError(t, s.objectManager.SetObjectTagging(ctx, "acme/files", "old", &object.TagSet{Tags: []object.Tag{{Key: "env", Value: "prod"}}}))
	put("held", http.Header{"X-Amz-Object-Lock-Legal-Hold": {"ON"}})
	put("kept", http.Header{
		"X-Amz-Object-Lock-Mode":              {"COMPLIANCE"},
		"X-Amz-Object-Lock-Retain-Until-Date": {time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
	})

	rec := httptest.NewRecorder()
	s.handleRenameObject(rec, mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/", nil), map[string]string{"bucket": "files", "object": "old"}))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	for _, tc := range []struct {
		key, body string
		actions   []string
		status    int
	}{
		{"old", `{"newKey":"  "}`, []string{"s3:*"}, http.StatusBadRequest},
		{"old", `{"newKey":"old"}`, []string{"s3:*"}, http.StatusBadRequest},
		{"old", `not json`, []string{"s3:*"}, http.StatusBadRequest},
		{"old", `{"newKey":"new"}`, []string{"s3:GetObject", "s3:PutObject"}, http.StatusForbidden},
		{"nowhere", `{"newKey":"new"}`, []string{"s3:*"}, http.StatusNotFound},
		{"held", `{"newKey":"held-2"}`, []string{"s3:*"}, http.StatusForbidden},
		{"kept", `{"newKey":"kept-2"}`, []string{"s3:*"}, http.StatusForbidden},
	} {
		assert.Equal(t, tc.status, rename(tc.key, tc.body, tc.actions...), "%s %s", tc.key, tc.body)
	}
	assert.Equal(t, "content of old", read("old"), "refused renames change nothing")
	assert.Empty(t, read("held-2"))
	assert.Empty(t, read("kept-2"))

	require.Equal(t, http.StatusOK, rename("old", `{"newKey":" new "}`, "s3:*"))
	assert.Equal(t, "content of old", read("new"))
	assert.Empty(t, read("old"), "the old key is gone")
	renamed, err := s.objectManager.GetObjectMetadata(ctx, "acme/files", "new")
	require.NoError(t, err)
	assert.Equal(t, "text/plain", renamed.ContentType)
	require.Equal(t, http.StatusOK, rename("bare", `{"newKey":"bare-2"}`, "s3:*"), "an object without tags")
	assert.Equal(t, "content of bare", read("bare-2"))
	tags, err := s.objectManager.GetObjectTagging(ctx, "acme/files", "new")
	require.NoError(t, err)
	require.Len(t, tags.Tags, 1)
	assert.Equal(t, "prod", tags.Tags[0].Value)
}
