package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An object's tags are set from the console as S3 takes them, and read back;
// tags S3 refuses are refused as the request's fault.
func TestObjectTagsAreSetAsS3TakesThem(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "tagged", tenantAdmin.ID))
	_, err := s.objectManager.PutObject(ctx, "acme/tagged", "doc", bytes.NewReader([]byte("x")), http.Header{})
	require.NoError(t, err)
	call := func(handler http.HandlerFunc, key, body string, actions ...string) *httptest.ResponseRecorder {
		req := mux.SetURLVars(consoleRequestWith(tenantAdmin, body, actions...), map[string]string{"bucket": "tagged", "object": key})
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}
	tagsOf := func(tags ...string) string {
		set := object.TagSet{}
		for i := 0; i+1 < len(tags); i += 2 {
			set.Tags = append(set.Tags, object.Tag{Key: tags[i], Value: tags[i+1]})
		}
		body, err := json.Marshal(set)
		require.NoError(t, err)
		return string(body)
	}

	require.Equal(t, http.StatusOK, call(s.handleSetObjectTags, "doc", tagsOf("env", "prod", "team", "ops"), "s3:*").Code)
	rec := call(s.handleGetObjectTags, "doc", "", "s3:*")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"prod"`)

	ten := []string{}
	for i := range 10 {
		ten = append(ten, strings.Repeat("k", 127)+string(rune('a'+i)), strings.Repeat("v", 256))
	}
	assert.Equal(t, http.StatusOK, call(s.handleSetObjectTags, "doc", tagsOf(ten...), "s3:*").Code,
		"ten tags, keys of 128 characters and values of 256 are taken")
	require.Equal(t, http.StatusOK, call(s.handleSetObjectTags, "doc", tagsOf("env", "prod", "team", "ops"), "s3:*").Code)
	eleven := []string{}
	for i := range 11 {
		eleven = append(eleven, strings.Repeat("k", i+1), "v")
	}
	for name, body := range map[string]string{
		"eleven tags":      tagsOf(eleven...),
		"an empty key":     tagsOf("", "v"),
		"a key too long":   tagsOf(strings.Repeat("k", 129), "v"),
		"a value too long": tagsOf("k", strings.Repeat("v", 257)),
		"a key twice":      tagsOf("k", "a", "k", "b"),
		"not json":         `not json`,
	} {
		assert.Equal(t, http.StatusBadRequest, call(s.handleSetObjectTags, "doc", body, "s3:*").Code, name)
	}
	rec = call(s.handleGetObjectTags, "doc", "", "s3:*")
	assert.Contains(t, rec.Body.String(), `"prod"`, "refused tags change nothing")
	assert.Equal(t, http.StatusNotFound, call(s.handleSetObjectTags, "nowhere", tagsOf("k", "v"), "s3:*").Code)
	assert.Equal(t, http.StatusNotFound, call(s.handleGetObjectTags, "nowhere", "", "s3:*").Code)
	assert.Equal(t, http.StatusForbidden, call(s.handleSetObjectTags, "doc", tagsOf("k", "v")).Code)
}
