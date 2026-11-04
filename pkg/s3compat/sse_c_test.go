package s3compat

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sendSSEC sends a signed request with headers, over HTTPS when secure.
func (env *s3TestEnv) sendSSEC(method, path string, body []byte, headers map[string]string, secure bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Host = "localhost"
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	signRequestV4(req, env.accessKey, env.secretKey, "us-east-1", "s3")
	if secure {
		req = req.WithContext(bucket.WithRequestFacts(req.Context(), bucket.RequestFacts{SecureTransport: true}))
	}
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	return w
}

// customerKeyHeaders are the headers that give key, under prefix.
func customerKeyHeaders(prefix string, key []byte) map[string]string {
	sum := md5.Sum(key)
	return map[string]string{
		prefix + sseCAlgorithmHeader: "AES256",
		prefix + sseCKeyHeader:       base64.StdEncoding.EncodeToString(key),
		prefix + sseCKeyMD5Header:    base64.StdEncoding.EncodeToString(sum[:]),
	}
}

func joinHeaders(sets ...map[string]string) map[string]string {
	all := map[string]string{}
	for _, set := range sets {
		for k, v := range set {
			all[k] = v
		}
	}
	return all
}

const allowCustomerKeysXML = `<ServerSideEncryptionConfiguration><Rule>` +
	`<ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault>` +
	`<BlockedEncryptionTypes><EncryptionType>NONE</EncryptionType></BlockedEncryptionTypes>` +
	`</Rule></ServerSideEncryptionConfiguration>`

// customerKeyBucket makes a bucket that takes writes with SSE-C.
func (env *s3TestEnv) customerKeyBucket(t *testing.T, name string) {
	t.Helper()
	require.NoError(t, env.bucketManager.CreateBucket(context.Background(), env.tenantID, name, env.userID))
	w := env.sendSigned("PUT", "/"+name+"?encryption", []byte(allowCustomerKeysXML), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

var (
	keyA = bytes.Repeat([]byte{0xA1}, 32)
	keyB = bytes.Repeat([]byte{0xB2}, 32)
)

func TestBucketEncryptionBlocksCustomerKeysUnlessAllowed(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(context.Background(), env.tenantID, "enc", env.userID))

	blocked := func() string {
		w := env.sendSigned("GET", "/enc?encryption", nil, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var cfg sseConfiguration
		require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &cfg))
		require.NotNil(t, cfg.Rule.BlockedEncryptionTypes)
		return strings.Join(cfg.Rule.BlockedEncryptionTypes.EncryptionType, ",")
	}
	assert.Equal(t, "SSE-C", blocked(), "a new bucket blocks SSE-C")

	w := env.sendSigned("PUT", "/enc?encryption", []byte(allowCustomerKeysXML), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "NONE", blocked())

	// A configuration that names no blocked type blocks SSE-C again.
	w = env.sendSigned("PUT", "/enc?encryption", []byte(`<ServerSideEncryptionConfiguration><Rule>`+
		`<ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault>`+
		`</Rule></ServerSideEncryptionConfiguration>`), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "SSE-C", blocked())

	// AWS's own example: a rule that only blocks SSE-C.
	env.sendSigned("PUT", "/enc?encryption", []byte(allowCustomerKeysXML), nil)
	w = env.sendSigned("PUT", "/enc?encryption", []byte(`<ServerSideEncryptionConfiguration><Rule>`+
		`<BlockedEncryptionTypes><EncryptionType>SSE-C</EncryptionType></BlockedEncryptionTypes>`+
		`</Rule></ServerSideEncryptionConfiguration>`), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "SSE-C", blocked())

	for _, body := range []string{"<EncryptionType>SSE-KMS</EncryptionType>",
		"<EncryptionType>NONE</EncryptionType><EncryptionType>SSE-C</EncryptionType>"} {
		w = env.sendSigned("PUT", "/enc?encryption", []byte(`<ServerSideEncryptionConfiguration><Rule>`+
			`<BlockedEncryptionTypes>`+body+`</BlockedEncryptionTypes></Rule></ServerSideEncryptionConfiguration>`), nil)
		assert.Equal(t, http.StatusBadRequest, w.Code, body)
	}

	env.sendSigned("PUT", "/enc?encryption", []byte(allowCustomerKeysXML), nil)
	w = env.sendSigned("DELETE", "/enc?encryption", nil, nil)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	assert.Equal(t, "SSE-C", blocked(), "deleting the configuration blocks SSE-C again")
}

func TestCustomerKeyWriteRefusedWhereBlocked(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(context.Background(), env.tenantID, "plain", env.userID))

	w := env.sendSSEC("PUT", "/plain/a.txt", []byte("secret"), customerKeyHeaders("", keyA), true)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "AccessDenied")

	head := env.sendSigned("HEAD", "/plain/a.txt", nil, nil)
	assert.Equal(t, http.StatusNotFound, head.Code, "a refused write stores nothing")
}

func TestCustomerKeyNeedsHTTPS(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.customerKeyBucket(t, "ssec")

	w := env.sendSSEC("PUT", "/ssec/a.txt", []byte("secret"), customerKeyHeaders("", keyA), false)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "secure connection")
}

func TestCustomerKeyHeaderErrors(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.customerKeyBucket(t, "ssec")

	cases := map[string]map[string]string{
		"algorithm": joinHeaders(customerKeyHeaders("", keyA), map[string]string{sseCAlgorithmHeader: "AES128"}),
		"short key": customerKeyHeaders("", keyA[:16]),
		"key MD5": joinHeaders(customerKeyHeaders("", keyA),
			map[string]string{sseCKeyMD5Header: customerKeyHeaders("", keyB)[sseCKeyMD5Header]}),
		"no key MD5":        {sseCAlgorithmHeader: "AES256", sseCKeyHeader: base64.StdEncoding.EncodeToString(keyA)},
		"no key":            {sseCAlgorithmHeader: "AES256"},
		"with SSE-S3 too":   joinHeaders(customerKeyHeaders("", keyA), map[string]string{"x-amz-server-side-encryption": "AES256"}),
		"key not in base64": joinHeaders(customerKeyHeaders("", keyA), map[string]string{sseCKeyHeader: "not base64!"}),
	}
	for name, headers := range cases {
		w := env.sendSSEC("PUT", "/ssec/a.txt", []byte("secret"), headers, true)
		assert.Equal(t, http.StatusBadRequest, w.Code, name+": "+w.Body.String())
	}
	head := env.sendSigned("HEAD", "/ssec/a.txt", nil, nil)
	assert.Equal(t, http.StatusNotFound, head.Code, "no refused write stored anything")
}

func TestCustomerKeyRoundTrip(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.customerKeyBucket(t, "ssec")
	content := []byte("the customer's secret, kept under the customer's key")

	put := env.sendSSEC("PUT", "/ssec/doc.txt", content, customerKeyHeaders("", keyA), true)
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	assert.Equal(t, "AES256", put.Header().Get(sseCAlgorithmHeader))
	assert.Equal(t, customerKeyHeaders("", keyA)[sseCKeyMD5Header], put.Header().Get(sseCKeyMD5Header))
	assert.Empty(t, put.Header().Get("x-amz-server-side-encryption"), "SSE-C is not SSE-S3")
	sum := md5.Sum(content)
	assert.NotEqual(t, `"`+hex.EncodeToString(sum[:])+`"`, put.Header().Get("ETag"), "the ETag is not the content's MD5")

	get := env.sendSSEC("GET", "/ssec/doc.txt", nil, customerKeyHeaders("", keyA), true)
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	assert.Equal(t, content, get.Body.Bytes())
	assert.Equal(t, "AES256", get.Header().Get(sseCAlgorithmHeader))
	assert.Equal(t, put.Header().Get("ETag"), get.Header().Get("ETag"))

	ranged := env.sendSSEC("GET", "/ssec/doc.txt", nil, joinHeaders(customerKeyHeaders("", keyA), map[string]string{"Range": "bytes=4-9"}), true)
	require.Equal(t, http.StatusPartialContent, ranged.Code, ranged.Body.String())
	assert.Equal(t, content[4:10], ranged.Body.Bytes())

	noKey := env.sendSSEC("GET", "/ssec/doc.txt", nil, nil, true)
	assert.Equal(t, http.StatusBadRequest, noKey.Code, noKey.Body.String())
	assert.NotContains(t, noKey.Body.String(), string(content))

	wrongKey := env.sendSSEC("GET", "/ssec/doc.txt", nil, customerKeyHeaders("", keyB), true)
	assert.Equal(t, http.StatusForbidden, wrongKey.Code, wrongKey.Body.String())
	assert.NotContains(t, wrongKey.Body.String(), string(content))

	assert.Equal(t, http.StatusBadRequest, env.sendSSEC("HEAD", "/ssec/doc.txt", nil, nil, true).Code)
	assert.Equal(t, http.StatusForbidden, env.sendSSEC("HEAD", "/ssec/doc.txt", nil, customerKeyHeaders("", keyB), true).Code)
	head := env.sendSSEC("HEAD", "/ssec/doc.txt", nil, customerKeyHeaders("", keyA), true)
	require.Equal(t, http.StatusOK, head.Code)
	assert.Equal(t, "AES256", head.Header().Get(sseCAlgorithmHeader))

	// A key given for an object that has none is refused, as in S3.
	plain := env.sendSigned("PUT", "/ssec/plain.txt", []byte("plain"), nil)
	require.Equal(t, http.StatusOK, plain.Code)
	assert.Equal(t, http.StatusBadRequest, env.sendSSEC("GET", "/ssec/plain.txt", nil, customerKeyHeaders("", keyA), true).Code)
	assert.Equal(t, http.StatusBadRequest, env.sendSSEC("HEAD", "/ssec/plain.txt", nil, customerKeyHeaders("", keyA), true).Code)

	// The server cannot read it on its own.
	_, _, err := env.objectManager.GetObject(context.Background(), env.ssecPath(), "doc.txt")
	assert.ErrorIs(t, err, object.ErrCustomerKeyRequired)

	// Blocking SSE-C stops writes, not reads.
	require.Equal(t, http.StatusNoContent, env.sendSigned("DELETE", "/ssec?encryption", nil, nil).Code)
	again := env.sendSSEC("GET", "/ssec/doc.txt", nil, customerKeyHeaders("", keyA), true)
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	assert.Equal(t, content, again.Body.Bytes())
	assert.Equal(t, http.StatusForbidden, env.sendSSEC("PUT", "/ssec/doc2.txt", content, customerKeyHeaders("", keyA), true).Code)
}

// TestCustomerKeyContentIsNotStored: neither the content nor its MD5 is on
// disk, in the data or in the sidecar.
func TestCustomerKeyContentIsNotStored(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.customerKeyBucket(t, "ssec")
	content := []byte(strings.Repeat("PLAINTEXT-MARKER-", 4096))
	sum := md5.Sum(content)

	put := env.sendSSEC("PUT", "/ssec/big.bin", content, customerKeyHeaders("", keyA), true)
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())

	// Every file of the deployment: the object's data and sidecar, and the
	// metadata store with its entry.
	stored := 0
	require.NoError(t, filepath.WalkDir(env.tempDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // a file the store holds open
		}
		if len(data) >= len(content) {
			stored++
		}
		assert.False(t, bytes.Contains(data, []byte("PLAINTEXT-MARKER-")), path)
		assert.False(t, bytes.Contains(data, []byte(hex.EncodeToString(sum[:]))), path)
		assert.False(t, bytes.Contains(data, []byte(base64.StdEncoding.EncodeToString(keyA))), path)
		assert.False(t, bytes.Contains(data, keyA), path)
		return nil
	}))
	assert.NotZero(t, stored, "the object's data is among the files read")
}

func TestCustomerKeyCopy(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.customerKeyBucket(t, "ssec")
	content := []byte("copied under another key")
	require.Equal(t, http.StatusOK, env.sendSSEC("PUT", "/ssec/src.txt", content, customerKeyHeaders("", keyA), true).Code)

	copySource := map[string]string{"x-amz-copy-source": "/ssec/src.txt"}
	noSourceKey := env.sendSSEC("PUT", "/ssec/dst.txt", nil, joinHeaders(copySource, customerKeyHeaders("", keyB)), true)
	assert.Equal(t, http.StatusBadRequest, noSourceKey.Code, noSourceKey.Body.String())

	wrongSourceKey := env.sendSSEC("PUT", "/ssec/dst.txt", nil,
		joinHeaders(copySource, customerKeyHeaders(copySourcePrefix, keyB), customerKeyHeaders("", keyB)), true)
	assert.Equal(t, http.StatusForbidden, wrongSourceKey.Code, wrongSourceKey.Body.String())

	copied := env.sendSSEC("PUT", "/ssec/dst.txt", nil,
		joinHeaders(copySource, customerKeyHeaders(copySourcePrefix, keyA), customerKeyHeaders("", keyB)), true)
	require.Equal(t, http.StatusOK, copied.Code, copied.Body.String())
	assert.Equal(t, customerKeyHeaders("", keyB)[sseCKeyMD5Header], copied.Header().Get(sseCKeyMD5Header))

	get := env.sendSSEC("GET", "/ssec/dst.txt", nil, customerKeyHeaders("", keyB), true)
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	assert.Equal(t, content, get.Body.Bytes())
	assert.Equal(t, http.StatusForbidden, env.sendSSEC("GET", "/ssec/dst.txt", nil, customerKeyHeaders("", keyA), true).Code)

	// A copy without a destination key is encrypted as any other object.
	plain := env.sendSSEC("PUT", "/ssec/plain.txt", nil, joinHeaders(copySource, customerKeyHeaders(copySourcePrefix, keyA)), true)
	require.Equal(t, http.StatusOK, plain.Code, plain.Body.String())
	read := env.sendSigned("GET", "/ssec/plain.txt", nil, nil)
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	assert.Equal(t, content, read.Body.Bytes())
}

// TestCustomerKeyServerCannotDecryptWithKEK: the KEK does not open an object
// written with a customer key.
func TestCustomerKeyServerCannotDecryptWithKEK(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.customerKeyBucket(t, "ssec")
	require.Equal(t, http.StatusOK, env.sendSSEC("PUT", "/ssec/doc.txt", []byte("secret"), customerKeyHeaders("", keyA), true).Code)

	ctx := object.WithReadKey(context.Background(), keyB)
	_, _, err := env.objectManager.GetObject(ctx, env.ssecPath(), "doc.txt")
	assert.True(t, errors.Is(err, object.ErrCustomerKeyMismatch), "%v", err)

	_, body, err := env.objectManager.GetObject(object.WithReadKey(context.Background(), keyA), env.ssecPath(), "doc.txt")
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	body.Close()
	require.NoError(t, err)
	assert.Equal(t, "secret", string(data))
}

// ssecPath is the path of the bucket ssec in the store.
func (env *s3TestEnv) ssecPath() string {
	if env.tenantID == "" {
		return "ssec"
	}
	return env.tenantID + "/ssec"
}

// TestCustomerKeyIsNotLogged: the signature's log line gives the signed
// headers, the key among them, redacted.
func TestCustomerKeyIsNotLogged(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.customerKeyBucket(t, "ssec")

	var logged bytes.Buffer
	out, level := logrus.StandardLogger().Out, logrus.GetLevel()
	logrus.SetOutput(&logged)
	logrus.SetLevel(logrus.DebugLevel)
	defer func() {
		logrus.SetOutput(out)
		logrus.SetLevel(level)
	}()

	require.Equal(t, http.StatusOK, env.sendSSEC("PUT", "/ssec/doc.txt", []byte("secret"), customerKeyHeaders("", keyA), true).Code)
	require.Equal(t, http.StatusOK, env.sendSSEC("GET", "/ssec/doc.txt", nil, customerKeyHeaders("", keyA), true).Code)

	require.Contains(t, logged.String(), "Canonical request components", "the signature is logged")
	assert.Contains(t, logged.String(), "x-amz-server-side-encryption-customer-key:[redacted]")
	assert.NotContains(t, logged.String(), base64.StdEncoding.EncodeToString(keyA))
}
