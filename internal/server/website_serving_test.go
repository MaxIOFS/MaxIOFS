package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A website serves its pages, its folders' index documents and its error
// document. A routing rule redirects the keys it names; one on an error code
// only those that get it.
func TestAWebsiteServesItsPagesAndRedirects(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	userUpdateFixture(t, s)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "site", ""))
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "plain", ""))
	require.NoError(t, s.bucketManager.SetWebsite(ctx, "acme", "site", &bucket.WebsiteConfig{
		IndexDocument: "index.html",
		ErrorDocument: "error.html",
		RoutingRules: []bucket.WebsiteRoutingRule{
			{Condition: bucket.WebsiteRoutingCondition{KeyPrefixEquals: "old/"},
				Redirect: bucket.WebsiteRoutingRedirect{ReplaceKeyPrefixWith: "new/", HTTPRedirectCode: "302"}},
			{Condition: bucket.WebsiteRoutingCondition{HTTPErrorCodeReturnedEquals: "404", KeyPrefixEquals: "gone/"},
				Redirect: bucket.WebsiteRoutingRedirect{HostName: "archive.example.com", Protocol: "http"}},
			{Condition: bucket.WebsiteRoutingCondition{KeyPrefixEquals: "noop/"}},
			{Condition: bucket.WebsiteRoutingCondition{KeyPrefixEquals: "one/"}, Redirect: bucket.WebsiteRoutingRedirect{ReplaceKeyWith: "index.html"}},
			{Condition: bucket.WebsiteRoutingCondition{KeyPrefixEquals: "host/"}, Redirect: bucket.WebsiteRoutingRedirect{HostName: "new.example.com"}},
			{Condition: bucket.WebsiteRoutingCondition{KeyPrefixEquals: "plain/"}, Redirect: bucket.WebsiteRoutingRedirect{Protocol: "http"}},
		},
	}))
	for key, content := range map[string]string{"index.html": "home", "docs/index.html": "docs", "error.html": "oops", "gone/here.html": "still here", "both": "object", "both/index.html": "folder"} {
		_, err := s.objectManager.PutObject(ctx, "acme/site", key, bytes.NewReader([]byte(content)), http.Header{"Content-Type": {"text/html"}})
		require.NoError(t, err)
	}
	visit := func(method, bucketName, urlPath string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleWebsiteRequest(rec, httptest.NewRequest(method, urlPath, nil), bucketName)
		return rec
	}

	for urlPath, page := range map[string]string{"/": "home", "/docs/": "docs", "/docs": "docs", "/gone/here.html": "still here", "/both": "object", "/both/": "folder"} {
		rec := visit(http.MethodGet, "site", urlPath)
		assert.Equal(t, http.StatusOK, rec.Code, urlPath)
		assert.Equal(t, page, rec.Body.String(), urlPath)
	}
	home := visit(http.MethodGet, "site", "/")
	assert.Equal(t, "text/html", home.Header().Get("Content-Type"))
	assert.Regexp(t, `^".+"$`, home.Header().Get("ETag"))
	head := visit(http.MethodHead, "site", "/")
	assert.Equal(t, http.StatusOK, head.Code)
	assert.Empty(t, head.Body.String())
	assert.Equal(t, "4", head.Header().Get("Content-Length"))

	moved := visit(http.MethodGet, "site", "/old/a.html")
	assert.Equal(t, http.StatusFound, moved.Code)
	assert.Equal(t, "https://example.com/new/a.html", moved.Header().Get("Location"))
	assert.NotContains(t, moved.Body.String(), "oops", "nothing is written after a redirect")
	for urlPath, location := range map[string]string{
		"/one/x":   "https://example.com/index.html",
		"/host/x":  "https://new.example.com/host/x",
		"/plain/x": "http://example.com/plain/x",
	} {
		assert.Equal(t, location, visit(http.MethodGet, "site", urlPath).Header().Get("Location"), urlPath)
	}
	assert.Equal(t, http.StatusNotFound, visit(http.MethodGet, "site", "/noop/x").Code, "a rule that says nowhere redirects nothing")
	gone := visit(http.MethodGet, "site", "/gone/missing.html")
	assert.Equal(t, http.StatusMovedPermanently, gone.Code)
	assert.Equal(t, "http://archive.example.com/gone/missing.html", gone.Header().Get("Location"))

	missing := visit(http.MethodGet, "site", "/missing")
	assert.Equal(t, http.StatusNotFound, missing.Code)
	assert.Equal(t, "oops", missing.Body.String())
	// AWS's own example: what is not found goes to a folder of reports.
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "lost", ""))
	require.NoError(t, s.bucketManager.SetWebsite(ctx, "acme", "lost", &bucket.WebsiteConfig{IndexDocument: "index.html",
		RoutingRules: []bucket.WebsiteRoutingRule{{Condition: bucket.WebsiteRoutingCondition{HTTPErrorCodeReturnedEquals: "404"},
			Redirect: bucket.WebsiteRoutingRedirect{ReplaceKeyPrefixWith: "report-404/"}}}}))
	reported := visit(http.MethodGet, "lost", "/page.html")
	assert.Equal(t, http.StatusMovedPermanently, reported.Code)
	assert.Equal(t, "https://example.com/report-404/page.html", reported.Header().Get("Location"), "never the same key again")
	assert.Equal(t, http.StatusForbidden, visit(http.MethodGet, "plain", "/").Code, "no website")
	assert.Equal(t, http.StatusForbidden, visit(http.MethodGet, "nowhere", "/").Code)
}
