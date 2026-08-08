package server

// Where a download token is accepted.
//
// The middleware treats it as a credential, so the question these answer is
// not "does the download work" but "what else does this open". Only GET on the
// object-download route may be authorised by one; every sibling route lives
// under the same path prefix, so a prefix check would have handed a token good
// for reading a file the power to rewrite its ACL.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
)

// requestForTarget builds a request the way mux would deliver it, with the
// route variables already populated.
func requestForTarget(method, path, bucket, object string, query string) *http.Request {
	url := path
	if query != "" {
		url += "?" + query
	}
	r := httptest.NewRequest(method, url, nil)
	return mux.SetURLVars(r, map[string]string{"bucket": bucket, "object": object})
}

func TestDownloadRequestTarget_OnlyTheDownloadRoute(t *testing.T) {
	s := &Server{}

	t.Run("the download itself is accepted", func(t *testing.T) {
		r := requestForTarget(http.MethodGet, "/api/v1/buckets/backups/objects/report.pdf",
			"backups", "report.pdf", "")
		tenant, bucket, key, version, ok := s.downloadRequestTarget(r)
		assert.True(t, ok)
		assert.Equal(t, "", tenant)
		assert.Equal(t, "backups", bucket)
		assert.Equal(t, "report.pdf", key)
		assert.Equal(t, "", version)
	})

	t.Run("tenant and version travel with it", func(t *testing.T) {
		r := requestForTarget(http.MethodGet, "/api/v1/buckets/backups/objects/report.pdf",
			"backups", "report.pdf", "tenantId=acme&versionId=v7")
		tenant, _, _, version, ok := s.downloadRequestTarget(r)
		assert.True(t, ok)
		assert.Equal(t, "acme", tenant)
		assert.Equal(t, "v7", version)
	})

	t.Run("a key containing slashes still matches", func(t *testing.T) {
		r := requestForTarget(http.MethodGet, "/api/v1/buckets/backups/objects/2026/07/report.pdf",
			"backups", "2026/07/report.pdf", "")
		_, _, key, _, ok := s.downloadRequestTarget(r)
		assert.True(t, ok)
		assert.Equal(t, "2026/07/report.pdf", key)
	})

	t.Run("a key needing escaping still matches", func(t *testing.T) {
		const key = "informe final.pdf"
		r := requestForTarget(http.MethodGet, "/api/v1/buckets/backups/objects/informe%20final.pdf",
			"backups", key, "")
		_, _, got, _, ok := s.downloadRequestTarget(r)
		assert.True(t, ok)
		assert.Equal(t, key, got)
	})

	// The sibling routes. Every one of these has the download path as a prefix,
	// which is exactly why the check is not a prefix check.
	for _, sibling := range []struct {
		name   string
		method string
		path   string
	}{
		{"object ACL", http.MethodGet, "/api/v1/buckets/backups/objects/report.pdf/acl"},
		{"versions", http.MethodGet, "/api/v1/buckets/backups/objects/report.pdf/versions"},
		{"tags", http.MethodGet, "/api/v1/buckets/backups/objects/report.pdf/tags"},
		{"legal hold", http.MethodGet, "/api/v1/buckets/backups/objects/report.pdf/legal-hold"},
		{"minting another token", http.MethodPost, "/api/v1/buckets/backups/objects/report.pdf/download-token"},
	} {
		t.Run("refused: "+sibling.name, func(t *testing.T) {
			r := requestForTarget(sibling.method, sibling.path, "backups", "report.pdf", "")
			_, _, _, _, ok := s.downloadRequestTarget(r)
			assert.False(t, ok, "a download token must not reach %s", sibling.name)
		})
	}

	t.Run("refused: writing over the object", func(t *testing.T) {
		r := requestForTarget(http.MethodPut, "/api/v1/buckets/backups/objects/report.pdf",
			"backups", "report.pdf", "")
		_, _, _, _, ok := s.downloadRequestTarget(r)
		assert.False(t, ok)
	})

	t.Run("refused: deleting the object", func(t *testing.T) {
		r := requestForTarget(http.MethodDelete, "/api/v1/buckets/backups/objects/report.pdf",
			"backups", "report.pdf", "")
		_, _, _, _, ok := s.downloadRequestTarget(r)
		assert.False(t, ok)
	})
}

// TestDownloadResource_CannotBeSpelledTwoWays: the parts are joined with a NUL
// so no combination of tenant, bucket and key produces another combination's
// string. With a "/" separator, bucket "a/b" key "c" and bucket "a" key "b/c"
// would be the same resource, and a token for one would open the other.
func TestDownloadResource_CannotBeSpelledTwoWays(t *testing.T) {
	assert.NotEqual(t,
		downloadResource("", "a/b", "c", ""),
		downloadResource("", "a", "b/c", ""))

	assert.NotEqual(t,
		downloadResource("t", "b", "k", ""),
		downloadResource("", "t", "b", "k"))
}
