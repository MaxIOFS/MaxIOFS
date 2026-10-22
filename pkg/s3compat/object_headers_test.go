package s3compat

import (
	"encoding/xml"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uploadInParts writes path in a multipart upload started with headers, one
// part for each of parts, and gives the answer to its completion.
func (env *s3TestEnv) uploadInParts(t *testing.T, path string, headers map[string]string, parts ...[]byte) *httptest.ResponseRecorder {
	t.Helper()
	created := env.sendSigned("POST", path+"?uploads", nil, headers)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var upload InitiateMultipartUploadResult
	require.NoError(t, xml.Unmarshal(created.Body.Bytes(), &upload))
	var complete strings.Builder
	complete.WriteString("<CompleteMultipartUpload>")
	for i, data := range parts {
		w := env.sendSigned("PUT", fmt.Sprintf("%s?partNumber=%d&uploadId=%s", path, i+1, upload.UploadId), data, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		fmt.Fprintf(&complete, "<Part><PartNumber>%d</PartNumber><ETag>%s</ETag></Part>", i+1, w.Header().Get("ETag"))
	}
	complete.WriteString("</CompleteMultipartUpload>")
	return env.sendSigned("POST", path+"?uploadId="+upload.UploadId, []byte(complete.String()), nil)
}

// An object gives back the headers it was written with, written whole, in
// parts or copied; one written in parts keeps the tags it was started with.
func TestS3AnObjectKeepsTheHeadersItWasWrittenWith(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "kept", env.userID))
	headers := map[string]string{
		"Content-Type":        "text/x-kept",
		"Cache-Control":       "max-age=60",
		"Content-Disposition": `attachment; filename="r.txt"`,
		"Content-Encoding":    "gzip",
		"Content-Language":    "es",
		"Expires":             "Tue, 01 Jan 2030 00:00:00 GMT",
	}
	keeps := func(path string) {
		t.Helper()
		for _, method := range []string{"HEAD", "GET"} {
			got := env.sendSigned(method, path, nil, nil)
			require.Equal(t, http.StatusOK, got.Code, got.Body.String())
			for name, value := range headers {
				assert.Equal(t, value, got.Header().Get(name), "%s %s: %s", method, path, name)
			}
			for name := range got.Header() {
				assert.NotContains(t, strings.ToLower(name), "x-amz-meta-", "%s %s: no user metadata was given", method, path)
			}
		}
	}

	w := env.sendSigned("PUT", "/kept/whole", []byte("written whole"), headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	keeps("/kept/whole")

	started := maps.Clone(headers)
	started["X-Amz-Tagging"] = "a=1&b=dos"
	w = env.uploadInParts(t, "/kept/parts", started, []byte("written in parts"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "<Error>")
	keeps("/kept/parts")
	got := env.sendSigned("GET", "/kept/parts?tagging", nil, nil)
	require.Equal(t, http.StatusOK, got.Code, got.Body.String())
	var tagging Tagging
	require.NoError(t, xml.Unmarshal(got.Body.Bytes(), &tagging))
	assert.ElementsMatch(t, []Tag{{Key: "a", Value: "1"}, {Key: "b", Value: "dos"}}, tagging.TagSet.Tags)

	w = env.sendSigned("PUT", "/kept/copied", nil, map[string]string{"X-Amz-Copy-Source": "/kept/parts"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	keeps("/kept/copied")
}

// A multipart upload started with tags S3 refuses is refused.
func TestS3AnUploadStartedWithBadTagsIsRefused(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "badtags", env.userID))
	w := env.sendSigned("POST", "/badtags/k?uploads", nil, map[string]string{"X-Amz-Tagging": "a=1&a=2"})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "<Code>InvalidTag</Code>")
}

// A signed read is sent with the headers it asks for in its response-*
// parameters; an anonymous one asking for them is refused, as S3 does: else
// anyone could have a public object served as a page.
func TestS3AReadIsSentWithTheHeadersItAsksFor(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "asked", env.userID))
	w := env.sendSigned("PUT", "/asked/k", []byte("data"), map[string]string{"Content-Type": "text/plain", "Cache-Control": "max-age=60"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	asked := map[string]string{
		"Content-Type":        "application/x-test",
		"Content-Disposition": `attachment; filename="k.bin"`,
		"Cache-Control":       "no-store",
		"Content-Language":    "fr",
		"Content-Encoding":    "identity",
		"Expires":             "Wed, 01 Jan 2031 00:00:00 GMT",
	}
	query := url.Values{}
	for name, value := range asked {
		query.Set("response-"+strings.ToLower(name), value)
	}
	for _, method := range []string{"GET", "HEAD"} {
		got := env.sendSigned(method, "/asked/k?"+query.Encode(), nil, nil)
		require.Equal(t, http.StatusOK, got.Code, got.Body.String())
		for name, value := range asked {
			assert.Equal(t, value, got.Header().Get(name), "%s: %s", method, name)
		}
	}
	got := env.sendSigned("GET", "/asked/k", nil, nil)
	assert.Equal(t, "text/plain", got.Header().Get("Content-Type"), "a read that asks nothing is sent the object's")
	assert.Equal(t, "max-age=60", got.Header().Get("Cache-Control"))

	acls := setupACLTestEnvironment(t)
	defer acls.cleanup()
	acls.handler.SetShareManager(noShareManager{})
	putAnonymousTestObject(t, acls, "avatars", true)
	req := httptest.NewRequest(http.MethodGet, "/avatars/09.png?response-content-type=text%2Fhtml", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "avatars", "object": "09.png"})
	rec := httptest.NewRecorder()
	acls.handler.GetObject(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<Code>InvalidRequest</Code>")
	assert.Equal(t, http.StatusOK, anonymousGet(acls.handler, "avatars", "09.png").Code, "an anonymous read that asks nothing is served")
}
