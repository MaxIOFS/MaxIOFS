package s3compat

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Object-lock headers a multipart upload cannot honour are refused when the
// upload is created, with the code a PUT gets.
func TestCreateMultipartUploadRefusesObjectLockItCannotHonour(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	req, w := env.makeS3Request("PUT", "/mp-no-lock", nil)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	req, w = env.makeS3Request("POST", "/mp-no-lock/k?uploads", nil)
	req.Header.Set("x-amz-object-lock-legal-hold", "ON")
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "<Code>InvalidRequest</Code>")
}

// A canned ACL on a multipart upload comes from x-amz-acl only: user metadata
// named x-amz-acl stays user metadata and grants nothing.
func TestMultipartCannedACLComesOnlyFromTheACLHeader(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "mp-acl", env.userID))

	complete := func(t *testing.T, key string, header, value string) *object.ACL {
		t.Helper()
		req, w := env.makeS3Request("POST", "/mp-acl/"+key+"?uploads", nil)
		req.Header.Set(header, value)
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var created struct {
			UploadId string `xml:"UploadId"`
		}
		require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &created))

		req, w = env.makeS3Request("PUT", fmt.Sprintf("/mp-acl/%s?partNumber=1&uploadId=%s", key, created.UploadId), []byte("part"))
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		etag := w.Header().Get("ETag")

		body := fmt.Sprintf(`<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>`, etag)
		req, w = env.makeS3Request("POST", fmt.Sprintf("/mp-acl/%s?uploadId=%s", key, created.UploadId), []byte(body))
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "<Error>")

		acl, err := env.objectManager.GetObjectACL(ctx, env.tenantID+"/mp-acl", key)
		require.NoError(t, err)
		return acl
	}
	public := func(acl *object.ACL) bool {
		for _, g := range acl.Grants {
			if strings.HasSuffix(g.Grantee.URI, "/AllUsers") {
				return true
			}
		}
		return false
	}

	assert.False(t, public(complete(t, "meta", "x-amz-meta-x-amz-acl", "public-read")), "user metadata grants nothing")
	assert.True(t, public(complete(t, "header", "x-amz-acl", "public-read")), "the ACL header still applies")
}
