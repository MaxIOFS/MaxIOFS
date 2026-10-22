package s3compat

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"hash/crc32"
	"hash/crc64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
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
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))

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

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))

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

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))

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

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))

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

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))

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

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))

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

func b64(sum []byte) string { return base64.StdEncoding.EncodeToString(sum) }

func md5Of(data []byte) string    { s := md5.Sum(data); return b64(s[:]) }
func sha256Of(data []byte) string { s := sha256.Sum256(data); return b64(s[:]) }
func crc32cOf(data []byte) string {
	return b64(binary.BigEndian.AppendUint32(nil, crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli))))
}
func crc32Of(data []byte) string {
	return b64(binary.BigEndian.AppendUint32(nil, crc32.ChecksumIEEE(data)))
}
func crc64nvmeOf(data []byte) string {
	return b64(binary.BigEndian.AppendUint64(nil, crc64.Checksum(data, crc64.MakeTable(0x9a6c9329ac4bc9b5))))
}

// awsChunked is data in one aws-chunked chunk, then the trailer.
func awsChunked(data []byte, trailer string) []byte {
	return []byte(fmt.Sprintf("%x\r\n%s\r\n0\r\n%s\r\n\r\n", len(data), data, trailer))
}

// A write is checked against every digest it carries: Content-MD5, a
// checksum in a header or in the trailer of an aws-chunked body, and the
// payload hash it was signed with. One that does not match is refused and
// nothing is stored; a checksum that matches is kept and given back.
func TestS3AWriteIsCheckedAgainstTheDigestsItCarries(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := t.Context()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "sums", env.userID))
	data := []byte("the bytes that matter")
	other := []byte("other bytes")
	stored := func(key string) bool {
		_, err := env.objectManager.GetObjectMetadata(ctx, env.tenantID+"/sums", key)
		return err == nil
	}
	chunkedHeaders := func(trailer string) map[string]string {
		return map[string]string{
			"Content-Encoding":             "aws-chunked",
			"X-Amz-Decoded-Content-Length": fmt.Sprint(len(data)),
			"X-Amz-Content-Sha256":         "STREAMING-UNSIGNED-PAYLOAD-TRAILER",
			"X-Amz-Trailer":                trailer,
		}
	}

	for i, c := range []struct {
		name, code string
		body       []byte
		headers    map[string]string
	}{
		{"a wrong Content-MD5", "BadDigest", data, map[string]string{"Content-MD5": md5Of(other)}},
		{"a Content-MD5 that is no digest", "InvalidDigest", data, map[string]string{"Content-MD5": "not a digest"}},
		{"a wrong SHA256 the SDK names", "BadDigest", data, map[string]string{"X-Amz-Sdk-Checksum-Algorithm": "SHA256", "X-Amz-Checksum-Sha256": sha256Of(other)}},
		{"a wrong CRC32C alone", "BadDigest", data, map[string]string{"X-Amz-Checksum-Crc32c": crc32cOf(other)}},
		{"a wrong CRC64NVME", "BadDigest", data, map[string]string{"X-Amz-Checksum-Crc64nvme": crc64nvmeOf(other)}},
		{"a wrong CRC32 in the trailer", "BadDigest", awsChunked(data, "x-amz-checksum-crc32:"+crc32Of(other)), chunkedHeaders("x-amz-checksum-crc32")},
		{"a body other than the one signed", "XAmzContentSHA256Mismatch", data, map[string]string{"X-Amz-Content-Sha256": "forced:" + hex.EncodeToString(func() []byte { s := sha256.Sum256(other); return s[:] }())}},
	} {
		t.Run(c.name, func(t *testing.T) {
			key := fmt.Sprintf("refused-%d", i)
			w := env.sendSigned("PUT", "/sums/"+key, c.body, c.headers)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "<Code>"+c.code+"</Code>")
			assert.False(t, stored(key), "nothing is stored")
		})
	}

	w := env.sendSigned("PUT", "/sums/right", data, map[string]string{
		"Content-MD5": md5Of(data), "X-Amz-Sdk-Checksum-Algorithm": "SHA256", "X-Amz-Checksum-Sha256": sha256Of(data),
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	head := env.sendSigned("HEAD", "/sums/right", nil, map[string]string{"X-Amz-Checksum-Mode": "ENABLED"})
	assert.Equal(t, sha256Of(data), head.Header().Get("X-Amz-Checksum-Sha256"))

	w = env.sendSigned("PUT", "/sums/computed", data, map[string]string{"X-Amz-Sdk-Checksum-Algorithm": "CRC32C"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	head = env.sendSigned("HEAD", "/sums/computed", nil, map[string]string{"X-Amz-Checksum-Mode": "ENABLED"})
	assert.Equal(t, crc32cOf(data), head.Header().Get("X-Amz-Checksum-Crc32c"), "a checksum the SDK names without its value is computed and kept")

	w = env.sendSigned("PUT", "/sums/nvme", data, map[string]string{"X-Amz-Checksum-Crc64nvme": crc64nvmeOf(data)})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	head = env.sendSigned("HEAD", "/sums/nvme", nil, map[string]string{"X-Amz-Checksum-Mode": "ENABLED"})
	assert.Equal(t, crc64nvmeOf(data), head.Header().Get("X-Amz-Checksum-Crc64nvme"))
	req := httptest.NewRequest("GET", "/sums/nvme?attributes", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "sums", "object": "nvme"})
	req = req.WithContext(context.WithValue(req.Context(), "user", &auth.User{ID: env.userID, TenantID: env.tenantID, Roles: []string{"admin"}}))
	req.Header.Set("x-amz-object-attributes", "Checksum")
	attributes := httptest.NewRecorder()
	env.handler.GetObjectAttributes(attributes, req)
	assert.Contains(t, attributes.Body.String(), "<ChecksumCRC64NVME>"+crc64nvmeOf(data)+"</ChecksumCRC64NVME>")

	w = env.sendSigned("PUT", "/sums/trailed", awsChunked(data, "x-amz-checksum-crc32:"+crc32Of(data)), chunkedHeaders("x-amz-checksum-crc32"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := env.sendSigned("GET", "/sums/trailed", nil, map[string]string{"X-Amz-Checksum-Mode": "ENABLED"})
	assert.Equal(t, data, got.Body.Bytes())
	assert.Equal(t, crc32Of(data), got.Header().Get("X-Amz-Checksum-Crc32"))
}

// A read of a range gives no checksum: the object's is not the one of the
// bytes sent, and an SDK that checks it would refuse them.
func TestS3ARangeIsReadWithoutTheObjectChecksum(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "ranged", env.userID))
	data := []byte("0123456789abcdef")
	w := env.sendSigned("PUT", "/ranged/k", data, map[string]string{"X-Amz-Checksum-Crc32": crc32Of(data)})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	got := env.sendSigned("GET", "/ranged/k", nil, map[string]string{"X-Amz-Checksum-Mode": "ENABLED", "Range": "bytes=2-5"})
	require.Equal(t, http.StatusPartialContent, got.Code, got.Body.String())
	assert.Equal(t, "2345", got.Body.String())
	assert.Empty(t, got.Header().Get("X-Amz-Checksum-Crc32"))

	got = env.sendSigned("GET", "/ranged/k", nil, map[string]string{"X-Amz-Checksum-Mode": "ENABLED"})
	assert.Equal(t, crc32Of(data), got.Header().Get("X-Amz-Checksum-Crc32"), "a whole read gives it")
}

// A body is aws-chunked only when its headers say so: one that merely starts
// like a chunk is stored as it is.
func TestS3ABodyThatLooksChunkedIsStoredAsItIs(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "plain", env.userID))
	data := []byte("10;chunk-signature=0123456789abcdef\r\nthis is a log line, not a chunk\r\n")
	w := env.sendSigned("PUT", "/plain/log.txt", data, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := env.sendSigned("GET", "/plain/log.txt", nil, nil)
	assert.Equal(t, data, got.Body.Bytes())
}

// A part is checked against the digests it carries; one that does not match
// is refused and the part it would replace is kept.
func TestS3APartIsCheckedAgainstTheDigestsItCarries(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "parts", env.userID))
	created := env.sendSigned("POST", "/parts/big?uploads", nil, nil)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var upload InitiateMultipartUploadResult
	require.NoError(t, xml.Unmarshal(created.Body.Bytes(), &upload))
	part := "/parts/big?partNumber=1&uploadId=" + upload.UploadId
	first, second := []byte("first part"), []byte("second try")

	w := env.sendSigned("PUT", part, first, map[string]string{"Content-MD5": md5Of(first)})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for name, headers := range map[string]map[string]string{
		"a wrong Content-MD5": {"Content-MD5": md5Of(first)},
		"a wrong SHA256":      {"X-Amz-Sdk-Checksum-Algorithm": "SHA256", "X-Amz-Checksum-Sha256": sha256Of(first)},
		"a wrong CRC32C":      {"X-Amz-Checksum-Crc32c": crc32cOf(first)},
	} {
		w := env.sendSigned("PUT", part, second, headers)
		assert.Equal(t, http.StatusBadRequest, w.Code, name)
		assert.Contains(t, w.Body.String(), "<Code>BadDigest</Code>", name)
	}
	listed := env.sendSigned("GET", "/parts/big?uploadId="+upload.UploadId, nil, nil)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	sum := md5.Sum(first)
	assert.Contains(t, listed.Body.String(), hex.EncodeToString(sum[:]), "the first part is kept")

	w = env.sendSigned("PUT", part, second, map[string]string{"X-Amz-Checksum-Crc32c": crc32cOf(second)})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// signedChunks is a body in aws-chunked chunks, each signed after the one
// before it from the seed signature of the request, then the final empty
// chunk; tamper changes the bytes of the second chunk after it is signed.
func (env *s3TestEnv) signedChunks(req *http.Request, chunks [][]byte, tamper bool) []byte {
	return env.signedChunksWithTrailer(req, chunks, tamper, "", false, "")
}

// signedChunksWithTrailer is signedChunks ending with trailer, signed after the
// last chunk and parted from its signature by separator; tamperTrailer changes
// its value after it is signed.
func (env *s3TestEnv) signedChunksWithTrailer(req *http.Request, chunks [][]byte, tamper bool, trailer string, tamperTrailer bool, separator string) []byte {
	auth := req.Header.Get("Authorization")
	previous := auth[strings.LastIndex(auth, "Signature=")+len("Signature="):]
	amzDate := req.Header.Get("X-Amz-Date")
	scope := amzDate[:8] + "/us-east-1/s3/aws4_request"
	kDate := hmacSHA256Test([]byte("AWS4"+env.secretKey), []byte(amzDate[:8]))
	key := hmacSHA256Test(hmacSHA256Test(hmacSHA256Test(kDate, []byte("us-east-1")), []byte("s3")), []byte("aws4_request"))
	empty := sha256.Sum256(nil)
	var body bytes.Buffer
	for i, chunk := range append(chunks, nil) {
		sum := sha256.Sum256(chunk)
		toSign := "AWS4-HMAC-SHA256-PAYLOAD\n" + amzDate + "\n" + scope + "\n" + previous + "\n" +
			hex.EncodeToString(empty[:]) + "\n" + hex.EncodeToString(sum[:])
		previous = hex.EncodeToString(hmacSHA256Test(key, []byte(toSign)))
		sent := chunk
		if tamper && i == 1 {
			sent = bytes.ToUpper(chunk)
		}
		fmt.Fprintf(&body, "%x;chunk-signature=%s\r\n%s\r\n", len(chunk), previous, sent)
	}
	if trailer == "" {
		return body.Bytes()
	}
	sum := sha256.Sum256([]byte(trailer + "\n"))
	toSign := "AWS4-HMAC-SHA256-TRAILER\n" + amzDate + "\n" + scope + "\n" + previous + "\n" + hex.EncodeToString(sum[:])
	signature := hex.EncodeToString(hmacSHA256Test(key, []byte(toSign)))
	if tamperTrailer {
		trailer += "x"
	}
	// The last chunk ends with the trailers, not with an empty line.
	out := bytes.TrimSuffix(body.Bytes(), []byte("\r\n"))
	return append(out, []byte(trailer+separator+"x-amz-trailer-signature:"+signature+"\r\n\r\n")...)
}

// sendChunked sends chunks as a signed aws-chunked body.
func (env *s3TestEnv) sendChunked(method, path string, chunks [][]byte, tamper bool) *httptest.ResponseRecorder {
	size := 0
	for _, c := range chunks {
		size += len(c)
	}
	req := httptest.NewRequest(method, path, nil)
	req.Host = "localhost"
	req.Header.Set("Content-Encoding", "aws-chunked")
	req.Header.Set("X-Amz-Decoded-Content-Length", fmt.Sprint(size))
	req.Header.Set("X-Amz-Content-Sha256", "STREAMING-AWS4-HMAC-SHA256-PAYLOAD")
	signRequestV4(req, env.accessKey, env.secretKey, "us-east-1", "s3")
	body := env.signedChunks(req, chunks, tamper)
	req.Body = nopCloser{bytes.NewReader(body)}
	req.ContentLength = int64(len(body))
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	return w
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

// A body sent in signed chunks is checked chunk by chunk against the signature
// chained from the request's: a chunk changed on the way is refused with
// SignatureDoesNotMatch and nothing is stored, as an object or as a part.
func TestS3SignedChunksAreChecked(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "chunks", env.userID))
	chunks := [][]byte{[]byte("first chunk of the object "), []byte("second chunk of the object")}

	w := env.sendChunked("PUT", "/chunks/k", chunks, false)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := env.sendSigned("GET", "/chunks/k", nil, nil)
	assert.Equal(t, string(bytes.Join(chunks, nil)), got.Body.String())

	w = env.sendChunked("PUT", "/chunks/tampered", chunks, true)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "<Code>SignatureDoesNotMatch</Code>")
	_, err := env.objectManager.GetObjectMetadata(t.Context(), env.tenantID+"/chunks", "tampered")
	assert.Error(t, err, "nothing is stored")

	created := env.sendSigned("POST", "/chunks/part?uploads", nil, nil)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var upload InitiateMultipartUploadResult
	require.NoError(t, xml.Unmarshal(created.Body.Bytes(), &upload))
	w = env.sendChunked("PUT", "/chunks/part?partNumber=1&uploadId="+upload.UploadId, chunks, true)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	listed := env.sendSigned("GET", "/chunks/part?uploadId="+upload.UploadId, nil, nil)
	assert.NotContains(t, listed.Body.String(), "<PartNumber>1</PartNumber>", "the part is not stored")
	w = env.sendChunked("PUT", "/chunks/part?partNumber=1&uploadId="+upload.UploadId, chunks, false)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// The trailers of a body sent in signed chunks are checked against their own
// signature, as AWS documents them and as minio-go writes them, with an empty
// line before the signature: a checksum changed on the way is refused.
func TestS3SignedTrailersAreChecked(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "trailers", env.userID))
	data := []byte("the bytes of the object")
	send := func(key string, tamper bool, separator string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/trailers/"+key, nil)
		req.Host = "localhost"
		req.Header.Set("Content-Encoding", "aws-chunked")
		req.Header.Set("X-Amz-Decoded-Content-Length", fmt.Sprint(len(data)))
		req.Header.Set("X-Amz-Content-Sha256", "STREAMING-AWS4-HMAC-SHA256-PAYLOAD-TRAILER")
		req.Header.Set("X-Amz-Trailer", "x-amz-checksum-crc32")
		signRequestV4(req, env.accessKey, env.secretKey, "us-east-1", "s3")
		body := env.signedChunksWithTrailer(req, [][]byte{data}, false, "x-amz-checksum-crc32:"+crc32Of(data), tamper, separator)
		req.Body = nopCloser{bytes.NewReader(body)}
		req.ContentLength = int64(len(body))
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)
		return w
	}
	for name, separator := range map[string]string{"aws": "\r\n", "minio-go": "\n\r\n"} {
		w := send("signed-"+name, false, separator)
		require.Equal(t, http.StatusOK, w.Code, "%s: %s", name, w.Body.String())
		w = send("tampered-"+name, true, separator)
		assert.Equal(t, http.StatusForbidden, w.Code, "%s: %s", name, w.Body.String())
		assert.Contains(t, w.Body.String(), "<Code>SignatureDoesNotMatch</Code>")
	}
}

// A body sent in signed chunks that is cut short, ended by a last chunk whose
// signature was not made for it, is refused.
func TestS3ASignedBodyCutShortIsRefused(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "cut", env.userID))
	first := []byte("the first chunk, the only one that arrives")
	req := httptest.NewRequest("PUT", "/cut/k", nil)
	req.Host = "localhost"
	req.Header.Set("Content-Encoding", "aws-chunked")
	req.Header.Set("X-Amz-Decoded-Content-Length", fmt.Sprint(len(first)))
	req.Header.Set("X-Amz-Content-Sha256", "STREAMING-AWS4-HMAC-SHA256-PAYLOAD")
	signRequestV4(req, env.accessKey, env.secretKey, "us-east-1", "s3")
	whole := env.signedChunks(req, [][]byte{first, []byte("a chunk that is dropped")}, false)
	end := bytes.Index(whole, []byte("\r\n"+fmt.Sprintf("%x", len("a chunk that is dropped"))+";"))
	require.Positive(t, end)
	body := append(whole[:end+2:end+2], []byte("0;chunk-signature="+strings.Repeat("0", 64)+"\r\n\r\n")...)
	req.Body = nopCloser{bytes.NewReader(body)}
	req.ContentLength = int64(len(body))
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	_, err := env.objectManager.GetObjectMetadata(t.Context(), env.tenantID+"/cut", "k")
	assert.Error(t, err, "nothing is stored")
}
