package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A search filters a bucket's objects by type, size, date and tags; a filter
// it cannot read is refused, never dropped to answer every object.
func TestASearchFiltersOrIsRefused(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "found", tenantAdmin.ID))
	for key, put := range map[string]struct {
		contentType string
		size        int
	}{"a.txt": {"text/plain", 5}, "b.png": {"image/png", 2000}, "dir/c.txt": {"text/plain", 10}} {
		_, err := s.objectManager.PutObject(ctx, "acme/found", key, bytes.NewReader(bytes.Repeat([]byte("x"), put.size)),
			http.Header{"Content-Type": {put.contentType}})
		require.NoError(t, err)
	}
	require.NoError(t, s.objectManager.SetObjectTagging(ctx, "acme/found", "a.txt", &object.TagSet{Tags: []object.Tag{{Key: "env", Value: "prod"}}}))
	search := func(query url.Values, actions ...string) *httptest.ResponseRecorder {
		req := mux.SetURLVars(consoleRequestWith(tenantAdmin, "", actions...), map[string]string{"bucket": "found"})
		req.URL.RawQuery = query.Encode()
		rec := httptest.NewRecorder()
		s.handleSearchObjects(rec, req)
		return rec
	}
	type found struct {
		Objects []struct {
			Key string `json:"key"`
		} `json:"objects"`
		CommonPrefixes []string `json:"commonPrefixes"`
	}
	keys := func(query url.Values) []string {
		answer := consoleData[found](t, search(query, "s3:*"), http.StatusOK)
		listed := []string{}
		for _, o := range answer.Objects {
			listed = append(listed, o.Key)
		}
		sort.Strings(listed)
		return append(listed, answer.CommonPrefixes...)
	}
	past, future := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

	for want, query := range map[string]url.Values{
		"[a.txt b.png dir/c.txt]": {},
		"[b.png]":                 {"content_type": {"image/"}},
		"[a.txt dir/c.txt]":       {"content_type": {"text/"}, "max_size": {"5000"}, "modified_after": {past}},
		"[a.txt]":                 {"max_size": {"5"}},
		"[b.png dir/c.txt]":       {"min_size": {"6"}},
		"[a.txt b.png dir/]":      {"delimiter": {"/"}},
		"[]":                      {"modified_after": {future}},
		"[dir/c.txt]":             {"prefix": {"dir/"}, "modified_before": {future}},
	} {
		assert.Equal(t, want, "["+joinKeys(keys(query))+"]", "%v", query)
	}
	assert.Equal(t, []string{"a.txt"}, keys(url.Values{"tag": {"env:prod"}}))
	assert.Empty(t, keys(url.Values{"delimiter": {"/"}, "modified_before": {past}}), "a folder with nothing that passes is not listed")
	for _, maxKeys := range []string{"0", "-1", "many"} {
		assert.Len(t, keys(url.Values{"max_keys": {maxKeys}}), 3, maxKeys)
	}
	assert.Len(t, keys(url.Values{"max_keys": {"1"}}), 1)

	for name, query := range map[string]url.Values{
		"a size of a fraction": {"min_size": {"307.2"}},
		"a size of no number":  {"max_size": {"big"}},
		"a date of no time":    {"modified_after": {"2026-01-01"}},
		"a date of nothing":    {"modified_before": {"soon"}},
		"a tag of no value":    {"tag": {"env"}},
	} {
		assert.Equal(t, http.StatusBadRequest, search(query, "s3:*").Code, name)
	}
	assert.Equal(t, http.StatusForbidden, search(url.Values{}).Code)
}

func joinKeys(keys []string) string {
	joined := ""
	for i, k := range keys {
		if i > 0 {
			joined += " "
		}
		joined += k
	}
	return joined
}
