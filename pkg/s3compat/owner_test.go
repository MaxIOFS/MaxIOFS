package s3compat

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var uploadIDIn = regexp.MustCompile(`<UploadId>([^<]+)</UploadId>`)

// TestOwnersAreTheBucketOwner: ACLs and listings name the bucket's owner, by ID
// alone, as AWS names owners since November 2025.
func TestOwnersAreTheBucketOwner(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	require.Equal(t, http.StatusOK, env.sendSigned("PUT", "/owned", nil, map[string]string{"x-amz-object-ownership": "ObjectWriter"}).Code)
	require.Equal(t, http.StatusOK, env.sendSigned("PUT", "/owned/k", []byte("data"), nil).Code)
	w := env.sendSigned("POST", "/owned/mp?uploads", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	uploadID := uploadIDIn.FindStringSubmatch(w.Body.String())
	require.Len(t, uploadID, 2)

	for _, path := range []string{"/", "/owned?acl", "/owned/k?acl", "/owned/", "/owned/?list-type=2&fetch-owner=true",
		"/owned?versions", "/owned?uploads", "/owned/mp?uploadId=" + uploadID[1]} {
		w := env.sendSigned("GET", path, nil, nil)
		require.Equal(t, http.StatusOK, w.Code, path)
		assert.Contains(t, w.Body.String(), "<ID>"+env.userID+"</ID>", path)
		assert.NotContains(t, w.Body.String(), "<ID>maxiofs</ID>", path)
		assert.NotContains(t, w.Body.String(), "DisplayName", path)
	}
}

// TestABucketPolicyIsGivenBackAsItWasPut: GetBucketPolicy gives the document
// as it was put, its spacing included.
func TestABucketPolicyIsGivenBackAsItWasPut(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	require.Equal(t, http.StatusOK, env.sendSigned("PUT", "/spaced", nil, nil).Code)
	doc := `{"Version": "2012-10-17", "Statement": [{"Effect": "Deny", "Principal": "*", ` +
		`"Action": "s3:DeleteBucket", "Resource": "arn:aws:s3:::spaced"}]}`
	w := env.sendSigned("PUT", "/spaced?policy", []byte(doc), nil)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	w = env.sendSigned("GET", "/spaced?policy", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, doc, w.Body.String())
}

// TestACopyRangePastTheSourceIsNotSatisfiable: UploadPartCopy of a range past
// the end of its source is refused as InvalidRange, a malformed range as
// InvalidArgument.
func TestACopyRangePastTheSourceIsNotSatisfiable(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	require.Equal(t, http.StatusOK, env.sendSigned("PUT", "/ranges", nil, nil).Code)
	require.Equal(t, http.StatusOK, env.sendSigned("PUT", "/ranges/source", []byte("12345"), nil).Code)
	w := env.sendSigned("POST", "/ranges/dest?uploads", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	uploadID := uploadIDIn.FindStringSubmatch(w.Body.String())
	require.Len(t, uploadID, 2)

	copyPart := func(copyRange string) *httpResult {
		w := env.sendSigned("PUT", "/ranges/dest?partNumber=1&uploadId="+uploadID[1], nil, map[string]string{
			"x-amz-copy-source": "/ranges/source", "x-amz-copy-source-range": copyRange})
		return &httpResult{code: w.Code, body: w.Body.String()}
	}
	got := copyPart("bytes=0-21")
	assert.Equal(t, http.StatusRequestedRangeNotSatisfiable, got.code)
	assert.Contains(t, got.body, "<Code>InvalidRange</Code>")
	got = copyPart("bytes=3-1")
	assert.Equal(t, http.StatusBadRequest, got.code)
	assert.Contains(t, got.body, "<Code>InvalidArgument</Code>")
	assert.Equal(t, http.StatusOK, copyPart("bytes=0-4").code, "the whole source")
}

type httpResult struct {
	code int
	body string
}
