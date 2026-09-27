package s3compat

import (
	"net/http"
	"testing"

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
