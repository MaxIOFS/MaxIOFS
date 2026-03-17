package s3compat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChecksumCRC32_PutAndGet uploads an object with x-amz-checksum-algorithm: CRC32
// and verifies the server computes, stores, and returns the checksum.
func TestChecksumCRC32_PutAndGet(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "checksum-bucket"
	objectKey := "file.bin"
	content := []byte("hello checksum CRC32")

	// Create bucket
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	// Calculate expected CRC32 checksum
	h := crc32.NewIEEE()
	h.Write(content)
	expectedChecksum := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// PUT with checksum algorithm header
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, content)
	req.Header.Set("x-amz-checksum-algorithm", "CRC32")
	env.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "PUT should succeed")
	assert.NotEmpty(t, w.Header().Get("ETag"), "Should return ETag")
	assert.Equal(t, "CRC32", w.Header().Get("x-amz-checksum-algorithm"), "PUT response should echo algorithm")
	assert.Equal(t, expectedChecksum, w.Header().Get("x-amz-checksum-crc32"), "PUT response should return computed CRC32")

	// GET and verify checksum headers
	req2, w2 := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey, nil)
	env.router.ServeHTTP(w2, req2)

	require.Equal(t, http.StatusOK, w2.Code, "GET should succeed")
	assert.Equal(t, "CRC32", w2.Header().Get("x-amz-checksum-algorithm"), "GET response should return algorithm")
	assert.Equal(t, expectedChecksum, w2.Header().Get("x-amz-checksum-crc32"), "GET response should return stored CRC32")

	// HEAD and verify checksum headers
	req3, w3 := env.makeS3Request("HEAD", "/"+bucketName+"/"+objectKey, nil)
	env.router.ServeHTTP(w3, req3)

	require.Equal(t, http.StatusOK, w3.Code, "HEAD should succeed")
	assert.Equal(t, "CRC32", w3.Header().Get("x-amz-checksum-algorithm"), "HEAD response should return algorithm")
	assert.Equal(t, expectedChecksum, w3.Header().Get("x-amz-checksum-crc32"), "HEAD response should return stored CRC32")
}

// TestChecksumSHA256_PutAndGet uploads with SHA256 and verifies round-trip.
func TestChecksumSHA256_PutAndGet(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "checksum-sha256-bucket"
	objectKey := "data.bin"
	content := []byte("hello checksum SHA256 verification")

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	// Calculate expected SHA256
	raw := sha256.Sum256(content)
	expectedChecksum := base64.StdEncoding.EncodeToString(raw[:])

	// PUT with SHA256 algorithm
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, content)
	req.Header.Set("x-amz-checksum-algorithm", "SHA256")
	env.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "PUT should succeed with SHA256 checksum")
	assert.Equal(t, "SHA256", w.Header().Get("x-amz-checksum-algorithm"))
	assert.Equal(t, expectedChecksum, w.Header().Get("x-amz-checksum-sha256"))

	// GET should return the same checksum
	req2, w2 := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey, nil)
	env.router.ServeHTTP(w2, req2)

	require.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, expectedChecksum, w2.Header().Get("x-amz-checksum-sha256"), "GET should return stored SHA256")
}

// TestChecksumCRC32C_PutAndGet uploads with CRC32C and verifies round-trip.
func TestChecksumCRC32C_PutAndGet(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "checksum-crc32c-bucket"
	objectKey := "crc32c.bin"
	content := []byte("data for CRC32C checksum test")

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	// Calculate expected CRC32C
	h := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	h.Write(content)
	expectedChecksum := base64.StdEncoding.EncodeToString(h.Sum(nil))

	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, content)
	req.Header.Set("x-amz-checksum-algorithm", "CRC32C")
	env.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "CRC32C", w.Header().Get("x-amz-checksum-algorithm"))
	assert.Equal(t, expectedChecksum, w.Header().Get("x-amz-checksum-crc32c"))
}

// TestChecksum_ClientProvided_Correct verifies that a correct client-provided checksum is accepted.
func TestChecksum_ClientProvided_Correct(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "checksum-validation-bucket"
	objectKey := "validated.bin"
	content := []byte("content for client-provided checksum")

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	h := crc32.NewIEEE()
	h.Write(content)
	correctChecksum := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// Send PUT with algorithm header AND pre-computed checksum value
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, content)
	req.Header.Set("x-amz-checksum-algorithm", "CRC32")
	req.Header.Set("x-amz-checksum-crc32", correctChecksum)
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Correct client-provided checksum should be accepted")
}

// TestChecksum_ClientProvided_Wrong verifies that a wrong client-provided checksum is rejected.
func TestChecksum_ClientProvided_Wrong(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "checksum-reject-bucket"
	objectKey := "bad.bin"
	content := []byte("content for wrong checksum test")

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	// Send incorrect checksum
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, content)
	req.Header.Set("x-amz-checksum-algorithm", "CRC32")
	req.Header.Set("x-amz-checksum-crc32", "AAAAAAAAAA==") // wrong value
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "Wrong checksum should be rejected with 400")
}

// TestChecksum_NoAlgorithm_NoHeader verifies that objects uploaded without a checksum
// algorithm do not receive checksum response headers.
func TestChecksum_NoAlgorithm_NoHeader(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "no-checksum-bucket"
	objectKey := "plain.txt"
	content := []byte("plain upload without checksum")

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, content)
	// No x-amz-checksum-algorithm header
	env.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("x-amz-checksum-algorithm"), "No checksum header should be set")
	assert.Empty(t, w.Header().Get("x-amz-checksum-crc32"), "No CRC32 header should be set")
	assert.Empty(t, w.Header().Get("x-amz-checksum-sha256"), "No SHA256 header should be set")

	// Verify GET also has no checksum headers
	req2, w2 := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey, nil)
	env.router.ServeHTTP(w2, req2)

	require.Equal(t, http.StatusOK, w2.Code)
	assert.Empty(t, w2.Header().Get("x-amz-checksum-algorithm"), "GET should not return checksum header for plain upload")
}

// makeS3RequestWithHeaders creates and signs an S3 request, then applies extra headers.
// The extra headers are applied BEFORE signing so they are included in the signature.
func (env *s3TestEnv) makeS3RequestWithHeaders(method, path string, body []byte, extraHeaders map[string]string) (*http.Request, *httptest.ResponseRecorder) {
	var reqBody bytes.Reader
	if body != nil {
		reqBody = *bytes.NewReader(body)
	}

	req := httptest.NewRequest(method, path, &reqBody)
	req.Host = "localhost"

	// Apply extra headers before signing
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	signRequestV4(req, env.accessKey, env.secretKey, "us-east-1", "s3")
	return req, httptest.NewRecorder()
}
