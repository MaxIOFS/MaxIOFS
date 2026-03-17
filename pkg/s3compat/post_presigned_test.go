package s3compat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPresignedPostEnv builds a test env with the HandlePresignedPost route registered.
func setupPresignedPostEnv(t *testing.T) *s3TestEnv {
	env := setupCompleteS3Environment(t)
	// The base test router doesn't include the POST presigned route — add it here.
	// It must come AFTER the ?delete route already registered.
	env.router.HandleFunc("/{bucket}", env.handler.HandlePresignedPost).Methods("POST")
	return env
}

// buildPostPresignedRequest constructs a multipart/form-data request for POST presigned upload.
// It generates and signs the policy using V4 signing.
func buildPostPresignedRequest(
	t *testing.T,
	bucketName string,
	objectKey string,
	fileContent []byte,
	contentType string,
	accessKey string,
	secretKey string,
	conditions []interface{},
	extraFields map[string]string,
) *http.Request {
	t.Helper()

	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	dateTime := now.Format("20060102T150405Z")
	region := "us-east-1"
	service := "s3"
	credential := fmt.Sprintf("%s/%s/%s/%s/aws4_request", accessKey, dateStamp, region, service)
	expiration := now.Add(15 * time.Minute).UTC().Format(time.RFC3339)

	// Build policy conditions — always include bucket and key
	allConditions := []interface{}{
		map[string]string{"bucket": bucketName},
		map[string]string{"key": objectKey},
		map[string]string{"x-amz-algorithm": "AWS4-HMAC-SHA256"},
		map[string]string{"x-amz-credential": credential},
		map[string]string{"x-amz-date": dateTime},
	}
	if contentType != "" {
		allConditions = append(allConditions, map[string]string{"content-type": contentType})
	}
	allConditions = append(allConditions, conditions...)

	policyDoc := map[string]interface{}{
		"expiration": expiration,
		"conditions": allConditions,
	}
	policyJSON, err := json.Marshal(policyDoc)
	require.NoError(t, err)
	policyB64 := base64.StdEncoding.EncodeToString(policyJSON)

	// Sign: calculateSignatureV4(policyB64, secretKey, dateStamp, region, service)
	kDate := hmacSHA256Test([]byte("AWS4"+secretKey), []byte(dateStamp))
	kRegion := hmacSHA256Test(kDate, []byte(region))
	kService := hmacSHA256Test(kRegion, []byte(service))
	kSigning := hmacSHA256Test(kService, []byte("aws4_request"))
	// The handler calls hmacSHA256(kSigning, []byte(policyB64)) and hex-encodes it
	sig := hmacSHA256Test(kSigning, []byte(policyB64))
	hexSig := fmt.Sprintf("%x", sig)

	// Build multipart body
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	fields := map[string]string{
		"key":              objectKey,
		"x-amz-algorithm": "AWS4-HMAC-SHA256",
		"x-amz-credential": credential,
		"x-amz-date":       dateTime,
		"policy":           policyB64,
		"x-amz-signature":  hexSig,
	}
	if contentType != "" {
		fields["Content-Type"] = contentType
	}
	for k, v := range extraFields {
		fields[k] = v
	}

	for k, v := range fields {
		require.NoError(t, mw.WriteField(k, v))
	}

	// Write file field
	fw, err := mw.CreateFormFile("file", objectKey)
	require.NoError(t, err)
	_, err = io.Copy(fw, bytes.NewReader(fileContent))
	require.NoError(t, err)

	require.NoError(t, mw.Close())

	req := httptest.NewRequest("POST", "/"+bucketName, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// TestPresignedPost_BasicUpload_204 tests a POST presigned upload that returns 204 (default).
func TestPresignedPost_BasicUpload_204(t *testing.T) {
	env := setupPresignedPostEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "post-presigned-bucket"
	objectKey := "uploaded.txt"
	content := []byte("hello from POST presigned upload")

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	req := buildPostPresignedRequest(t, bucketName, objectKey, content, "text/plain", env.accessKey, env.secretKey, nil, nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code, "Default status should be 204")

	// Verify object was stored
	bucketPath := env.tenantID + "/" + bucketName
	obj, _, err := env.objectManager.GetObject(ctx, bucketPath, objectKey)
	require.NoError(t, err, "Object should exist after POST presigned upload")
	assert.Equal(t, int64(len(content)), obj.Size)
}

// TestPresignedPost_StatusCode_200 tests success_action_status=200.
func TestPresignedPost_StatusCode_200(t *testing.T) {
	env := setupPresignedPostEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "post-presigned-200"
	objectKey := "result200.txt"
	content := []byte("status 200 test")

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	req := buildPostPresignedRequest(t, bucketName, objectKey, content, "", env.accessKey, env.secretKey, nil,
		map[string]string{"success_action_status": "200"})
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "success_action_status=200 should return 200")
}

// TestPresignedPost_StatusCode_201 tests success_action_status=201 returns XML body.
func TestPresignedPost_StatusCode_201(t *testing.T) {
	env := setupPresignedPostEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "post-presigned-201"
	objectKey := "result201.txt"
	content := []byte("status 201 test")

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	req := buildPostPresignedRequest(t, bucketName, objectKey, content, "", env.accessKey, env.secretKey, nil,
		map[string]string{"success_action_status": "201"})
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code, "success_action_status=201 should return 201")
	// 201 should return XML body with Location, Bucket, Key, ETag
	body := w.Body.String()
	assert.Contains(t, body, "<Bucket>"+bucketName+"</Bucket>", "201 response should contain bucket in XML")
	assert.Contains(t, body, "<Key>"+objectKey+"</Key>", "201 response should contain key in XML")
}

// TestPresignedPost_PolicyExpired rejects an expired policy.
func TestPresignedPost_PolicyExpired(t *testing.T) {
	env := setupPresignedPostEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "post-presigned-expired"
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	dateTime := now.Format("20060102T150405Z")
	credential := fmt.Sprintf("%s/%s/us-east-1/s3/aws4_request", env.accessKey, dateStamp)

	policyDoc := map[string]interface{}{
		"expiration": now.Add(-1 * time.Hour).UTC().Format(time.RFC3339), // already expired
		"conditions": []interface{}{
			map[string]string{"bucket": bucketName},
			map[string]string{"key": "test.txt"},
			map[string]string{"x-amz-algorithm": "AWS4-HMAC-SHA256"},
			map[string]string{"x-amz-credential": credential},
			map[string]string{"x-amz-date": dateTime},
		},
	}
	policyJSON, _ := json.Marshal(policyDoc)
	policyB64 := base64.StdEncoding.EncodeToString(policyJSON)

	kDate := hmacSHA256Test([]byte("AWS4"+env.secretKey), []byte(dateStamp))
	kRegion := hmacSHA256Test(kDate, []byte("us-east-1"))
	kService := hmacSHA256Test(kRegion, []byte("s3"))
	kSigning := hmacSHA256Test(kService, []byte("aws4_request"))
	sig := hmacSHA256Test(kSigning, []byte(policyB64))

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{
		"key": "test.txt", "x-amz-algorithm": "AWS4-HMAC-SHA256",
		"x-amz-credential": credential, "x-amz-date": dateTime,
		"policy": policyB64, "x-amz-signature": fmt.Sprintf("%x", sig),
	} {
		mw.WriteField(k, v) //nolint:errcheck
	}
	fw, _ := mw.CreateFormFile("file", "test.txt")
	fw.Write([]byte("data")) //nolint:errcheck
	mw.Close()

	req := httptest.NewRequest("POST", "/"+bucketName, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, "Expired policy should return 403")
	assert.Contains(t, w.Body.String(), "expired", strings.ToLower(w.Body.String()))
}

// TestPresignedPost_WrongSignature rejects an invalid signature.
func TestPresignedPost_WrongSignature(t *testing.T) {
	env := setupPresignedPostEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "post-presigned-badsig"
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	dateTime := now.Format("20060102T150405Z")
	credential := fmt.Sprintf("%s/%s/us-east-1/s3/aws4_request", env.accessKey, dateStamp)

	policyDoc := map[string]interface{}{
		"expiration": now.Add(15 * time.Minute).UTC().Format(time.RFC3339),
		"conditions": []interface{}{
			map[string]string{"bucket": bucketName},
			map[string]string{"key": "test.txt"},
			map[string]string{"x-amz-algorithm": "AWS4-HMAC-SHA256"},
			map[string]string{"x-amz-credential": credential},
			map[string]string{"x-amz-date": dateTime},
		},
	}
	policyJSON, _ := json.Marshal(policyDoc)
	policyB64 := base64.StdEncoding.EncodeToString(policyJSON)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{
		"key": "test.txt", "x-amz-algorithm": "AWS4-HMAC-SHA256",
		"x-amz-credential": credential, "x-amz-date": dateTime,
		"policy": policyB64, "x-amz-signature": "badc0ffebadc0ffebadc0ffebadc0ffebadc0ffe",
	} {
		mw.WriteField(k, v) //nolint:errcheck
	}
	fw, _ := mw.CreateFormFile("file", "test.txt")
	fw.Write([]byte("data")) //nolint:errcheck
	mw.Close()

	req := httptest.NewRequest("POST", "/"+bucketName, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	// AWS returns 403 for POST policy signature mismatch, our handler uses 401 (SignatureDoesNotMatch)
	assert.True(t, w.Code == http.StatusForbidden || w.Code == http.StatusUnauthorized,
		"Bad signature should return 4xx (got %d)", w.Code)
}

// TestPresignedPost_ContentLengthRange_TooSmall tests content-length-range lower bound enforcement.
func TestPresignedPost_ContentLengthRange_TooSmall(t *testing.T) {
	env := setupPresignedPostEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "post-presigned-minsize"
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	// Policy requires at least 100 bytes but we upload 5 bytes
	conditions := []interface{}{
		[]interface{}{"content-length-range", 100, 1000},
	}
	content := []byte("small") // only 5 bytes

	req := buildPostPresignedRequest(t, bucketName, "small.txt", content, "", env.accessKey, env.secretKey, conditions, nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "File too small for content-length-range should return 400")
}

// TestPresignedPost_StartsWithCondition_Valid tests that starts-with condition passes.
func TestPresignedPost_StartsWithCondition_Valid(t *testing.T) {
	env := setupPresignedPostEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "post-presigned-startswith"
	objectKey := "images/photo.jpg"
	content := []byte("fake image data for test")

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	// starts-with condition: key must start with "images/"
	conditions := []interface{}{
		[]interface{}{"starts-with", "$key", "images/"},
	}

	req := buildPostPresignedRequest(t, bucketName, objectKey, content, "", env.accessKey, env.secretKey, conditions, nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code, "starts-with condition satisfied should return 204")
}

// TestPresignedPost_Redirect tests success_action_redirect redirects with correct params.
func TestPresignedPost_Redirect(t *testing.T) {
	env := setupPresignedPostEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "post-presigned-redirect"
	objectKey := "redir.txt"
	content := []byte("redirect test data")

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	req := buildPostPresignedRequest(t, bucketName, objectKey, content, "", env.accessKey, env.secretKey, nil,
		map[string]string{"success_action_redirect": "https://example.com/done"})
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code, "Should redirect with 303")
	loc := w.Header().Get("Location")
	assert.Contains(t, loc, "https://example.com/done", "Redirect should contain original URL")
	assert.Contains(t, loc, "bucket="+bucketName, "Redirect should contain bucket")
	assert.Contains(t, loc, "key="+objectKey, "Redirect should contain key")
	assert.Contains(t, loc, "etag=", "Redirect should contain etag")
}
