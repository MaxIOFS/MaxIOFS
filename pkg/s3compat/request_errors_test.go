package s3compat

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taggingXML(tags ...string) []byte {
	var b strings.Builder
	b.WriteString("<Tagging><TagSet>")
	for i := 0; i+1 < len(tags); i += 2 {
		fmt.Fprintf(&b, "<Tag><Key>%s</Key><Value>%s</Value></Tag>", tags[i], tags[i+1])
	}
	b.WriteString("</TagSet></Tagging>")
	return []byte(b.String())
}

// A request S3 refuses is refused with S3's code, as the client's error: an
// SDK retries a 500. Nothing it would write is written.
func TestS3ARequestS3RefusesIsTheClientsError(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "errs", env.userID))
	w := env.sendSigned("PUT", "/errs/tagged", []byte("data"), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	eleven := make([]string, 0, 22)
	for i := range 11 {
		eleven = append(eleven, fmt.Sprintf("k%d", i), "v")
	}
	long := strings.Repeat("k", 1025)
	bigMeta := strings.Repeat("v", 2100)
	for _, c := range []struct {
		name, method, path string
		body               []byte
		headers            map[string]string
		status             int
		code               string
	}{
		{"a bucket name with capitals", "PUT", "/Bad_Name", nil, nil, 400, "InvalidBucketName"},
		{"a bucket name too short", "PUT", "/ab", nil, nil, 400, "InvalidBucketName"},
		{"a read from a bucket that is not there", "GET", "/nothere/k", nil, nil, 404, "NoSuchBucket"},
		{"eleven tags", "PUT", "/errs/tagged?tagging", taggingXML(eleven...), nil, 400, "InvalidTag"},
		{"a tag key given twice", "PUT", "/errs/tagged?tagging", taggingXML("a", "1", "a", "2"), nil, 400, "InvalidTag"},
		{"a tag key too long", "PUT", "/errs/tagged?tagging", taggingXML(strings.Repeat("k", 129), "v"), nil, 400, "InvalidTag"},
		{"a tag value too long", "PUT", "/errs/tagged?tagging", taggingXML("k", strings.Repeat("v", 257)), nil, 400, "InvalidTag"},
		{"eleven tags with a write", "PUT", "/errs/w-tags", []byte("x"), map[string]string{"X-Amz-Tagging": "a=1&b=2&c=3&d=4&e=5&f=6&g=7&h=8&i=9&j=10&k=11"}, 400, "InvalidTag"},
		{"a storage class S3 does not have", "PUT", "/errs/w-class", []byte("x"), map[string]string{"X-Amz-Storage-Class": "FROZEN"}, 400, "InvalidStorageClass"},
		{"an upload of a storage class S3 does not have", "POST", "/errs/u-class?uploads", nil, map[string]string{"X-Amz-Storage-Class": "FROZEN"}, 400, "InvalidStorageClass"},
		{"a copy to a storage class S3 does not have", "PUT", "/errs/c-class", nil, map[string]string{"X-Amz-Copy-Source": "/errs/tagged", "X-Amz-Storage-Class": "FROZEN"}, 400, "InvalidStorageClass"},
		{"user metadata over 2 KB", "PUT", "/errs/w-meta", []byte("x"), map[string]string{"X-Amz-Meta-Big": bigMeta}, 400, "MetadataTooLarge"},
		{"an upload with user metadata over 2 KB", "POST", "/errs/u-meta?uploads", nil, map[string]string{"X-Amz-Meta-Big": bigMeta}, 400, "MetadataTooLarge"},
		{"a copy given user metadata over 2 KB", "PUT", "/errs/c-meta", nil, map[string]string{"X-Amz-Copy-Source": "/errs/tagged", "X-Amz-Metadata-Directive": "REPLACE", "X-Amz-Meta-Big": bigMeta}, 400, "MetadataTooLarge"},
		{"a write of a key too long", "PUT", "/errs/" + long, []byte("x"), nil, 400, "KeyTooLongError"},
		{"a read of a key too long", "GET", "/errs/" + long, nil, nil, 400, "KeyTooLongError"},
		{"a delete of a key too long", "DELETE", "/errs/" + long, nil, nil, 400, "KeyTooLongError"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := env.sendSigned(c.method, c.path, c.body, c.headers)
			assert.Equal(t, c.status, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "<Code>"+c.code+"</Code>")
		})
	}

	listed := env.sendSigned("GET", "/errs/", nil, nil)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	for _, key := range []string{"w-tags", "w-class", "c-class", "w-meta", "c-meta"} {
		assert.NotContains(t, listed.Body.String(), "<Key>"+key+"</Key>", "nothing is written")
	}
	uploads := env.sendSigned("GET", "/errs?uploads", nil, nil)
	assert.NotContains(t, uploads.Body.String(), "<Key>u-", "no upload is started")
	got := env.sendSigned("GET", "/errs/tagged?tagging", nil, nil)
	assert.NotContains(t, got.Body.String(), "<Tag>", "the tags are as they were")
	buckets := env.sendSigned("GET", "/", nil, nil)
	assert.NotContains(t, buckets.Body.String(), "<Name>ab</Name>")

	// At the limits S3 takes them.
	w = env.sendSigned("PUT", "/errs/at-limits", []byte("x"), map[string]string{
		"X-Amz-Meta-Big": strings.Repeat("v", 2048-len("big")), "X-Amz-Storage-Class": "GLACIER_IR",
		"X-Amz-Tagging": "a=1&b=2&c=3&d=4&e=5&f=6&g=7&h=8&i=9&" + strings.Repeat("k", 128) + "=" + strings.Repeat("v", 256),
	})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A copy of an object onto itself that changes nothing is refused; one that
// changes its metadata, its class or its tags, or that copies an older version
// over it, is taken.
func TestS3ACopyOntoItselfMustChangeSomething(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "self", env.userID))
	w := env.sendSigned("PUT", "/self/k", []byte("data"), map[string]string{"X-Amz-Meta-A": "1"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	copyOf := map[string]string{"X-Amz-Copy-Source": "/self/k"}

	w = env.sendSigned("PUT", "/self/k", nil, copyOf)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "<Code>InvalidRequest</Code>")

	for name, headers := range map[string]map[string]string{
		"new metadata": {"X-Amz-Metadata-Directive": "REPLACE", "X-Amz-Meta-A": "2"},
		"a new class":  {"X-Amz-Storage-Class": "STANDARD_IA"},
		"new tags":     {"X-Amz-Tagging-Directive": "REPLACE", "X-Amz-Tagging": "t=1"},
		"encryption":   {"X-Amz-Server-Side-Encryption": "AES256"},
	} {
		headers["X-Amz-Copy-Source"] = "/self/k"
		w := env.sendSigned("PUT", "/self/k", nil, headers)
		assert.Equal(t, http.StatusOK, w.Code, "%s: %s", name, w.Body.String())
	}
	got := env.sendSigned("GET", "/self/k", nil, nil)
	assert.Equal(t, "data", got.Body.String(), "the data is kept")
	assert.Equal(t, []string{"2"}, got.Header()["x-amz-meta-a"])
}

// A part other than the last under 5 MB is refused when the upload is
// completed, before anything is written.
func TestS3APartOtherThanTheLastUnder5MBIsEntityTooSmall(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "small", env.userID))
	w := env.uploadInParts(t, "/small/k", nil, []byte("too small to come first"), []byte("last"))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "<Code>EntityTooSmall</Code>")
	got := env.sendSigned("HEAD", "/small/k", nil, nil)
	assert.Equal(t, http.StatusNotFound, got.Code, "nothing is written")

	w = env.uploadInParts(t, "/small/fits", nil, bytes.Repeat([]byte("x"), 5<<20), []byte("a last part may be small"))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "<Error>")
}

// A request signed with a wrong secret is told its signature does not match;
// one signed with a key that is not there is told the key is not there.
func TestS3AWrongSecretIsNotAnUnknownKey(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	for key, code := range map[string]string{env.accessKey: "SignatureDoesNotMatch", "AKIANOTANYONESKEY000": "InvalidAccessKeyId"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = "localhost"
		signRequestV4(req, key, "not-the-secret", "us-east-1", "s3")
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "<Code>"+code+"</Code>")
	}
}

// A read of a part of an object written in parts is that part, and tells how
// many parts there are; an object written whole is its only part.
func TestS3AReadOfAPartIsThatPart(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "byparts", env.userID))
	parts := [][]byte{bytes.Repeat([]byte("1"), 5<<20), bytes.Repeat([]byte("2"), 6<<20),
		bytes.Repeat([]byte("3"), 6<<20), []byte("the last part")}
	w := env.uploadInParts(t, "/byparts/k", nil, parts...)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "<Error>")
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	stored, err := env.objectManager.GetObjectMetadata(t.Context(), env.tenantID+"/byparts", "k")
	require.NoError(t, err)
	assert.Equal(t, []metadata.PartRun{{Size: 5 << 20, Count: 1}, {Size: 6 << 20, Count: 2}, {Size: int64(len(parts[3])), Count: 1}},
		stored.Parts, "parts of one size are kept as one run")

	start := 0
	for i, want := range parts {
		n := i + 1
		got := env.sendSigned("GET", fmt.Sprintf("/byparts/k?partNumber=%d", n), nil, nil)
		require.Equal(t, http.StatusPartialContent, got.Code, "part %d", n)
		assert.True(t, bytes.Equal(want, got.Body.Bytes()), "part %d", n)
		assert.Equal(t, "4", got.Header().Get("X-Amz-Mp-Parts-Count"), "part %d", n)
		assert.Equal(t, fmt.Sprintf("bytes %d-%d/%d", start, start+len(want)-1, total), got.Header().Get("Content-Range"), "part %d", n)
		head := env.sendSigned("HEAD", fmt.Sprintf("/byparts/k?partNumber=%d", n), nil, nil)
		require.Equal(t, http.StatusPartialContent, head.Code, "part %d", n)
		assert.Equal(t, fmt.Sprint(len(want)), head.Header().Get("Content-Length"), "part %d", n)
		assert.Equal(t, "4", head.Header().Get("X-Amz-Mp-Parts-Count"), "part %d", n)
		start += len(want)
	}
	got := env.sendSigned("GET", "/byparts/k?partNumber=5", nil, nil)
	assert.Equal(t, http.StatusRequestedRangeNotSatisfiable, got.Code, got.Body.String())
	assert.Contains(t, got.Body.String(), "<Code>InvalidPartNumber</Code>")
	got = env.sendSigned("GET", "/byparts/k?partNumber=1", nil, map[string]string{"Range": "bytes=0-1"})
	assert.Equal(t, http.StatusBadRequest, got.Code, got.Body.String())
	got = env.sendSigned("GET", "/byparts/k?partNumber=0", nil, nil)
	assert.Equal(t, http.StatusBadRequest, got.Code, got.Body.String())

	w = env.sendSigned("PUT", "/byparts/whole", []byte("written whole"), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got = env.sendSigned("GET", "/byparts/whole?partNumber=1", nil, nil)
	assert.Equal(t, "written whole", got.Body.String())
	assert.Empty(t, got.Header().Get("X-Amz-Mp-Parts-Count"))
	got = env.sendSigned("GET", "/byparts/whole?partNumber=2", nil, nil)
	assert.Equal(t, http.StatusRequestedRangeNotSatisfiable, got.Code, got.Body.String())
}
