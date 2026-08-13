package s3compat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startUpload initiates a multipart upload and returns its id.
func startUpload(t *testing.T, env *coverageTestEnv, bucketName, objectKey string) string {
	t.Helper()
	upload, err := env.objectManager.CreateMultipartUpload(
		context.Background(), env.tenantID+"/"+bucketName, objectKey, http.Header{})
	require.NoError(t, err)
	require.NotEmpty(t, upload.UploadID)
	return upload.UploadID
}

// requestFor builds a multipart request against one key carrying another
// upload's id — the confused-deputy shape.
func requestFor(method, bucketName, objectKey, uploadID, body string) *http.Request {
	req := httptest.NewRequest(method,
		"/"+bucketName+"/"+objectKey+"?uploadId="+uploadID, strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"bucket": bucketName, "object": objectKey})
	return req
}

func TestMultipartOwnership_ForeignUploadIsRefused(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "mpown", env.userID))

	// The victim's upload, and a key the caller legitimately holds.
	victimUpload := startUpload(t, env, "mpown", "victim/secret.txt")

	caller := &auth.User{ID: env.userID, TenantID: env.tenantID, Roles: []string{"user"}}

	cases := []struct {
		name   string
		method string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{"upload a part into it", http.MethodPut, "data", env.handler.UploadPart},
		{"list its parts", http.MethodGet, "", env.handler.ListParts},
		{"abort it", http.MethodDelete, "", env.handler.AbortMultipartUpload},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := requestFor(tc.method, "mpown", "attacker/mine.txt", victimUpload, tc.body)
			if tc.method == http.MethodPut {
				q := req.URL.Query()
				q.Set("partNumber", "1")
				req.URL.RawQuery = q.Encode()
			}
			req = req.WithContext(setUserInContext(req.Context(), caller))

			w := httptest.NewRecorder()
			tc.call(w, req)

			assert.NotEqual(t, http.StatusOK, w.Code,
				"an upload belonging to another key must not be reachable")
			assert.Contains(t, w.Body.String(), "NoSuchUpload",
				"and the refusal must not reveal that it exists elsewhere")
		})
	}

	// The victim's upload must still be there: aborting it was refused.
	uploads, err := env.objectManager.ListMultipartUploads(ctx, env.tenantID+"/mpown")
	require.NoError(t, err)
	found := false
	for _, u := range uploads {
		if u.UploadID == victimUpload {
			found = true
		}
	}
	assert.True(t, found, "the refused abort must not have destroyed the upload")
}

// TestMultipartOwnership_CompleteKeepsItsContract: CompleteMultipartUpload
func TestMultipartOwnership_CompleteKeepsItsContract(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "mpc", env.userID))
	victimUpload := startUpload(t, env, "mpc", "victim/secret.txt")

	caller := &auth.User{ID: env.userID, TenantID: env.tenantID, Roles: []string{"user"}}

	body := `<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>"x"</ETag></Part></CompleteMultipartUpload>`
	req := requestFor(http.MethodPost, "mpc", "attacker/mine.txt", victimUpload, body)
	req.Header.Set("Content-Type", "application/xml")
	req = req.WithContext(setUserInContext(req.Context(), caller))

	w := httptest.NewRecorder()
	env.handler.CompleteMultipartUpload(w, req)

	assert.Equal(t, http.StatusOK, w.Code,
		"the always-200 contract holds for a refusal too")
	assert.Contains(t, w.Body.String(), "NoSuchUpload")

	// Nothing was assembled at the victim's destination.
	_, err := env.objectManager.GetObjectMetadata(ctx, env.tenantID+"/mpc", "victim/secret.txt")
	assert.Error(t, err, "the refused completion must not have written the object")
}

// TestMultipartOwnership_TheOwnersOwnUploadStillWorks — the guard must not
// break the operation it protects.
func TestMultipartOwnership_TheOwnersOwnUploadStillWorks(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "mpok", env.userID))
	uploadID := startUpload(t, env, "mpok", "mine.txt")

	owner := &auth.User{ID: env.userID, TenantID: env.tenantID, Roles: []string{"user"}}

	req := requestFor(http.MethodGet, "mpok", "mine.txt", uploadID, "")
	req = req.WithContext(setUserInContext(req.Context(), owner))
	w := httptest.NewRecorder()
	env.handler.ListParts(w, req)

	assert.Equal(t, http.StatusOK, w.Code,
		"the upload's own key must still reach it")
}
