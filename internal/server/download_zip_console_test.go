package server

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A folder is downloaded as a ZIP of its objects, without its folder markers,
// by who may read every one of them.
func TestAFolderIsZippedForWhoMayReadAllOfIt(t *testing.T) {
	s := newClusterTestNodeWith(t, func(cfg *config.Config) { cfg.Audit.Enable = true })
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "files", tenantAdmin.ID))
	for key, content := range map[string]string{"docs/": "", "docs/a.txt": "aaa", "docs/empty": "", "docs/sub/b.txt": "bb", "secret/c.txt": "c"} {
		_, err := s.objectManager.PutObject(ctx, "acme/files", key, bytes.NewReader([]byte(content)), http.Header{})
		require.NoError(t, err)
	}
	download := func(prefix, document string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/?prefix="+url.QueryEscape(prefix), nil)
		reqCtx := context.WithValue(req.Context(), "user", tenantAdmin)
		reqCtx = auth.WithPolicySet(reqCtx, &auth.PolicySet{UserID: tenantAdmin.ID, TenantID: "acme",
			Actions: []string{"s3:ListBucket", "s3:GetObject"}, Documents: []string{document}})
		rec := httptest.NewRecorder()
		s.handleDownloadZip(rec, mux.SetURLVars(req.WithContext(reqCtx), map[string]string{"bucket": "files"}))
		return rec
	}
	const all = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListBucket","s3:GetObject"],"Resource":"*"}]}`
	const docsOnly = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:ListBucket","Resource":"*"},` +
		`{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::files/docs/*"}]}`
	const noList = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`
	entries := func(rec *httptest.ResponseRecorder) map[string]string {
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		archive, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
		require.NoError(t, err)
		read := map[string]string{}
		for _, f := range archive.File {
			opened, err := f.Open()
			require.NoError(t, err)
			content, err := io.ReadAll(opened)
			require.NoError(t, err)
			read[f.Name] = string(content)
		}
		return read
	}

	rec := httptest.NewRecorder()
	s.handleDownloadZip(rec, mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/", nil), map[string]string{"bucket": "files"}))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, http.StatusForbidden, download("docs/", noList).Code)
	assert.Equal(t, http.StatusForbidden, download("", docsOnly).Code, "the bucket holds an object it may not read")
	assert.Equal(t, http.StatusNotFound, download("none/", all).Code)

	assert.Equal(t, map[string]string{"a.txt": "aaa", "empty": "", "sub/b.txt": "bb"}, entries(download("docs/", docsOnly)),
		"an empty file is in it, a folder marker is not")
	s.auditManager.Flush()
	logs, _, err := s.auditManager.GetLogs(ctx, &audit.AuditLogFilters{EventType: audit.EventTypeObjectDownloaded})
	require.NoError(t, err)
	require.Len(t, logs, 1, "a ZIP written is audited")
	assert.Equal(t, "docs.zip", logs[0].ResourceName)
	assert.EqualValues(t, 3, logs[0].Details["file_count"])
	assert.EqualValues(t, 5, logs[0].Details["total_size"])
	assert.Equal(t, map[string]string{"docs/a.txt": "aaa", "docs/empty": "", "docs/sub/b.txt": "bb", "secret/c.txt": "c"}, entries(download("", all)))
	assert.Contains(t, download("docs/", all).Header().Get("Content-Disposition"), `filename="docs.zip"`)
}

// A ZIP holds at most so many objects and so many bytes: as many as that is
// taken, one more is refused.
func TestAZipHoldsNoMoreThanItsLimits(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "files", tenantAdmin.ID))
	for key, content := range map[string]string{"two/a": "1234", "two/b": "5678", "three/a": "1", "three/b": "2", "three/c": "3"} {
		_, err := s.objectManager.PutObject(ctx, "acme/files", key, bytes.NewReader([]byte(content)), http.Header{})
		require.NoError(t, err)
	}
	folderOf := func(prefix string, limits zipLimits) (zipFolder, error) {
		return s.zipFolderOf(consoleRequestWith(tenantAdmin, "", "s3:*"), tenantAdmin, "files", "acme/files", prefix, limits)
	}

	folder, err := folderOf("two/", zipLimits{objects: 2, bytes: 8})
	require.NoError(t, err, "two objects of 8 bytes, at both limits")
	assert.Len(t, folder.entries, 2)
	assert.EqualValues(t, 8, folder.size)
	_, err = folderOf("two/", zipLimits{objects: 2, bytes: 7})
	assert.ErrorContains(t, err, "10 GB", "a byte past the limit")
	_, err = folderOf("three/", zipLimits{objects: 2, bytes: 8})
	assert.ErrorContains(t, err, "more than 2 objects", "an object past the limit")
}

// A drive letter is any of a-z and A-Z: those are written encoded, the
// characters around them are not.
func TestAZipEncodesEveryDriveLetter(t *testing.T) {
	for name, want := range map[string]string{
		"a:/x": "a%3A/x", "z:/x": "z%3A/x", "A:/x": "A%3A/x", "Z:/x": "Z%3A/x",
		"@:/x": "@:/x", "[:/x": "[:/x", "`:/x": "`:/x", "{:/x": "{:/x", "ab:/x": "ab:/x", "a": "a",
	} {
		assert.Equal(t, want, zipEntryName(name), name)
	}
}
