package s3compat

import (
	"bytes"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func completionResult(t *testing.T, w *httptest.ResponseRecorder) CompleteMultipartUploadResult {
	t.Helper()
	var result CompleteMultipartUploadResult
	require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &result), w.Body.String())
	return result
}

// A completion or an abort sent again once the upload is finished is answered
// as AWS answers it, for a client that lost the first answer: the same
// completion gets the same result and an abort gets 204. A completion of other
// parts, a completion after an abort and an upload that never existed are
// NoSuchUpload.
func TestMultipartFinishSentAgain(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "again", env.userID))

	doneID, complete := env.startUploadInParts(t, "/again/done", nil, bytes.Repeat([]byte("a"), minPartSize), []byte("tail"))
	first := env.sendSigned("POST", "/again/done?uploadId="+doneID, []byte(complete), nil)
	require.Equal(t, http.StatusOK, first.Code)
	result := completionResult(t, first)
	again := env.sendSigned("POST", "/again/done?uploadId="+doneID, []byte(complete), nil)
	assert.Equal(t, http.StatusOK, again.Code)
	assert.Equal(t, result, completionResult(t, again))

	firstPartOnly := complete[:strings.Index(complete, "<Part><PartNumber>2<")] + "</CompleteMultipartUpload>"
	noSuchUpload := func(w *httptest.ResponseRecorder, what string) {
		t.Helper()
		assert.Equal(t, http.StatusNotFound, w.Code, what)
		assert.Contains(t, w.Body.String(), "<Code>NoSuchUpload</Code>", what)
	}
	noSuchUpload(env.sendSigned("POST", "/again/done?uploadId="+doneID, []byte(firstPartOnly), nil), "other parts")
	noSuchUpload(env.sendSigned("POST", "/again/other?uploadId="+doneID, []byte(complete), nil), "another key")

	assert.Equal(t, http.StatusNoContent, env.sendSigned("DELETE", "/again/done?uploadId="+doneID, nil, nil).Code)
	assert.Equal(t, http.StatusOK, env.sendSigned("HEAD", "/again/done", nil, nil).Code, "an abort after the completion leaves the object")

	abortedID, abortedComplete := env.startUploadInParts(t, "/again/aborted", nil, []byte("x"))
	for range 2 {
		assert.Equal(t, http.StatusNoContent, env.sendSigned("DELETE", "/again/aborted?uploadId="+abortedID, nil, nil).Code)
	}
	noSuchUpload(env.sendSigned("POST", "/again/aborted?uploadId="+abortedID, []byte(abortedComplete), nil), "a completion after the abort")

	noSuchUpload(env.sendSigned("DELETE", "/again/never?uploadId=no-such-upload", nil, nil), "abort of an unknown upload")
	noSuchUpload(env.sendSigned("POST", "/again/never?uploadId=no-such-upload", []byte(complete), nil), "completion of an unknown upload")
}

// A completion is held to If-Match and If-None-Match as a PUT is: refused with
// 412 before anything is written when the object is not as they say, and with
// NoSuchKey for an If-Match on a key without an object. A refused upload can
// still be completed.
func TestCompleteMultipartUploadHonoursWriteConditions(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "conditions", env.userID))
	complete := func(path string, headers map[string]string) (*httptest.ResponseRecorder, string, string) {
		id, body := env.startUploadInParts(t, path, nil, []byte(path))
		return env.sendSigned("POST", path+"?uploadId="+id, []byte(body), headers), id, body
	}
	refused := func(w *httptest.ResponseRecorder, status int, code, what string) {
		t.Helper()
		assert.Equal(t, status, w.Code, what)
		assert.Contains(t, w.Body.String(), "<Code>"+code+"</Code>", what)
	}

	w, _, _ := complete("/conditions/k", map[string]string{"If-None-Match": "*"})
	require.Equal(t, http.StatusOK, w.Code)
	etag := completionResult(t, w).ETag

	w, id, body := complete("/conditions/k", map[string]string{"If-None-Match": "*"})
	refused(w, http.StatusPreconditionFailed, "PreconditionFailed", "If-None-Match on an existing object")
	w = env.sendSigned("POST", "/conditions/k?uploadId="+id, []byte(body), nil)
	require.Equal(t, http.StatusOK, w.Code, "the refused upload is still there")
	etag = completionResult(t, w).ETag

	w, _, _ = complete("/conditions/k", map[string]string{"If-Match": `"0123456789abcdef0123456789abcdef"`})
	refused(w, http.StatusPreconditionFailed, "PreconditionFailed", "If-Match on another ETag")
	w, _, _ = complete("/conditions/k", map[string]string{"If-Match": etag})
	assert.Equal(t, http.StatusOK, w.Code, "If-Match on the current ETag")
	w, _, _ = complete("/conditions/missing", map[string]string{"If-Match": etag})
	refused(w, http.StatusNotFound, "NoSuchKey", "If-Match on a key without an object")
}
