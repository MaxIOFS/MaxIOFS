package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A folder's size counts the objects under it and not its folder markers; a
// bucket's versions are listed with their delete markers, and the list says it
// was cut only when more are left.
func TestFoldersAndVersionsAreCounted(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "listed", tenantAdmin.ID))
	require.NoError(t, s.bucketManager.SetVersioning(ctx, "acme", "listed", &bucket.VersioningConfig{Status: "Enabled"}))
	for key, content := range map[string]string{"docs/": "", "docs/a": "12345", "docs/b": "123", "docs/empty": "", "other/c": "1"} {
		_, err := s.objectManager.PutObject(ctx, "acme/listed", key, bytes.NewReader([]byte(content)), http.Header{})
		require.NoError(t, err)
	}
	_, err := s.objectManager.DeleteObject(ctx, "acme/listed", "other/c", false)
	require.NoError(t, err)
	call := func(handler http.HandlerFunc, query string, actions ...string) *httptest.ResponseRecorder {
		req := mux.SetURLVars(consoleRequestWith(tenantAdmin, "", actions...), map[string]string{"bucket": "listed"})
		req.URL.RawQuery = query
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	size := consoleData[map[string]int64](t, call(s.handleFolderSize, "prefix=docs/", "s3:*"), http.StatusOK)
	assert.Equal(t, map[string]int64{"size": 8, "count": 3}, size, "an empty file counts, a folder marker does not")
	assert.Equal(t, http.StatusForbidden, call(s.handleFolderSize, "prefix=docs/").Code)

	type listing struct {
		Versions []struct {
			Key            string `json:"key"`
			IsDeleteMarker bool   `json:"isDeleteMarker"`
		} `json:"versions"`
		IsTruncated bool `json:"isTruncated"`
	}
	all := consoleData[listing](t, call(s.handleListBucketVersions, "", "s3:*"), http.StatusOK)
	require.Len(t, all.Versions, 6, "five objects and a delete marker")
	assert.False(t, all.IsTruncated)
	markers := 0
	for _, v := range all.Versions {
		if v.IsDeleteMarker {
			markers++
			assert.Equal(t, "other/c", v.Key)
		}
	}
	assert.Equal(t, 1, markers)
	assert.False(t, consoleData[listing](t, call(s.handleListBucketVersions, "maxKeys=6", "s3:*"), http.StatusOK).IsTruncated,
		"six of six is all of them")
	for _, query := range []string{"maxKeys=0", "maxKeys=-1", "maxKeys=many"} {
		got := consoleData[listing](t, call(s.handleListBucketVersions, query, "s3:*"), http.StatusOK)
		assert.Len(t, got.Versions, 6, query)
		assert.False(t, got.IsTruncated, query)
	}
	one := consoleData[listing](t, call(s.handleListBucketVersions, "maxKeys=1", "s3:*"), http.StatusOK)
	assert.Len(t, one.Versions, 1)
	assert.True(t, one.IsTruncated)
	cut := consoleData[listing](t, call(s.handleListBucketVersions, "maxKeys=2", "s3:*"), http.StatusOK)
	assert.Len(t, cut.Versions, 2)
	assert.True(t, cut.IsTruncated)
	only := consoleData[listing](t, call(s.handleListBucketVersions, "prefix=docs/", "s3:*"), http.StatusOK)
	assert.Len(t, only.Versions, 4)
	assert.Equal(t, http.StatusForbidden, call(s.handleListBucketVersions, "").Code)
}
