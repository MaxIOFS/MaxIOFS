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
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sendSigned sends a request signed with the environment's key, with headers
// set before it is signed.
func (env *s3TestEnv) sendSigned(method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Host = "localhost"
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	signRequestV4(req, env.accessKey, env.secretKey, "us-east-1", "s3")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	return w
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
