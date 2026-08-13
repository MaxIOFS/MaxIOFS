package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routerForTokenTests mirrors the console's object routes, names the two that
// a token may authorise, and reports what the middleware would have decided.
func routerForTokenTests(s *Server, seen *string, ok *bool) *mux.Router {
	router := mux.NewRouter()

	record := func(w http.ResponseWriter, r *http.Request) {
		*seen, *ok = s.downloadTokenResource(r)
	}

	base := "/api/v1/buckets/{bucket}/objects/{object:.*}"
	router.HandleFunc(base+"/acl", record).Methods("GET")
	router.HandleFunc(base+"/tags", record).Methods("GET")
	router.HandleFunc(base+"/versions", record).Methods("GET")
	router.HandleFunc(base+"/legal-hold", record).Methods("GET")
	router.HandleFunc(base+"/download-token", record).Methods("POST")
	router.HandleFunc("/api/v1/buckets/{bucket}/download-zip-token", record).Methods("POST")

	router.HandleFunc("/api/v1/buckets/{bucket}/download-zip", record).
		Methods("GET").Name(routeFolderDownload)
	router.HandleFunc(base, record).Methods("GET").Name(routeObjectDownload)
	router.HandleFunc(base, record).Methods("PUT", "DELETE")

	return router
}

// resourceFor dispatches a request and returns what a token would have to name.
func resourceFor(t *testing.T, s *Server, method, path string) (string, bool) {
	t.Helper()
	var seen string
	var ok bool
	routerForTokenTests(s, &seen, &ok).ServeHTTP(
		httptest.NewRecorder(), httptest.NewRequest(method, path, nil))
	return seen, ok
}

func TestDownloadToken_OnlyTheTwoDownloadRoutes(t *testing.T) {
	s := getSharedServer()

	t.Run("the object download is accepted", func(t *testing.T) {
		resource, ok := resourceFor(t, s, http.MethodGet,
			"/api/v1/buckets/backups/objects/report.pdf")
		require.True(t, ok)
		assert.Equal(t, downloadResource("", "backups", "report.pdf", ""), resource)
	})

	t.Run("the folder archive is accepted", func(t *testing.T) {
		resource, ok := resourceFor(t, s, http.MethodGet,
			"/api/v1/buckets/backups/download-zip?prefix=folder/")
		require.True(t, ok)
		assert.Equal(t, downloadZipResource("", "backups", "folder/"), resource)
	})

	t.Run("tenant and version travel with the object", func(t *testing.T) {
		resource, ok := resourceFor(t, s, http.MethodGet,
			"/api/v1/buckets/backups/objects/report.pdf?tenantId=acme&versionId=v7")
		require.True(t, ok)
		assert.Equal(t, downloadResource("acme", "backups", "report.pdf", "v7"), resource)
	})

	t.Run("a key containing slashes still matches", func(t *testing.T) {
		resource, ok := resourceFor(t, s, http.MethodGet,
			"/api/v1/buckets/backups/objects/2026/07/report.pdf")
		require.True(t, ok)
		assert.Equal(t, downloadResource("", "backups", "2026/07/report.pdf", ""), resource)
	})

	// The sibling routes. Each has the download path as a prefix, and the one
	// that used to slip through is the key named after the route itself.
	for _, sibling := range []struct{ name, method, path string }{
		{"object ACL", http.MethodGet, "/api/v1/buckets/backups/objects/report.pdf/acl"},
		{"a key literally named acl", http.MethodGet, "/api/v1/buckets/backups/objects/acl/acl"},
		{"a key literally named tags", http.MethodGet, "/api/v1/buckets/backups/objects/tags/tags"},
		{"a key literally named versions", http.MethodGet, "/api/v1/buckets/backups/objects/versions/versions"},
		{"a key literally named legal-hold", http.MethodGet, "/api/v1/buckets/backups/objects/legal-hold/legal-hold"},
		{"minting another object token", http.MethodPost, "/api/v1/buckets/backups/objects/report.pdf/download-token"},
		{"minting a folder token", http.MethodPost, "/api/v1/buckets/backups/download-zip-token"},
		{"writing over the object", http.MethodPut, "/api/v1/buckets/backups/objects/report.pdf"},
		{"deleting the object", http.MethodDelete, "/api/v1/buckets/backups/objects/report.pdf"},
	} {
		t.Run("refused: "+sibling.name, func(t *testing.T) {
			_, ok := resourceFor(t, s, sibling.method, sibling.path)
			assert.False(t, ok, "a download token must not reach %s", sibling.name)
		})
	}
}

// TestDownloadResource_CannotBeSpelledTwoWays: the parts are joined with a NUL
// so no combination produces another's string, and the leading kind keeps an
// object token from redeeming as a folder archive.
func TestDownloadResource_CannotBeSpelledTwoWays(t *testing.T) {
	assert.NotEqual(t,
		downloadResource("", "a/b", "c", ""),
		downloadResource("", "a", "b/c", ""))

	assert.NotEqual(t,
		downloadResource("t", "b", "k", ""),
		downloadResource("", "t", "b", "k"))

	assert.NotEqual(t,
		downloadResource("", "backups", "folder/", ""),
		downloadZipResource("", "backups", "folder/"),
		"reading one object is not archiving everything under a prefix")
}
