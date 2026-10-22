package s3compat

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
