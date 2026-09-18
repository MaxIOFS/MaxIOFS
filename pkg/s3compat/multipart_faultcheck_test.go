package s3compat

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type faultHTTPResult struct {
	code int
	etag string
	body []byte
	err  error
}

type faultGateReader struct {
	r         *bytes.Reader
	remaining int
	started   chan struct{}
	release   <-chan struct{}
	ctx       context.Context
	once      sync.Once
}

func (r *faultGateReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		r.once.Do(func() { close(r.started) })
		select {
		case <-r.release:
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		}
	} else if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= n
	return n, err
}

func TestFaultMultipartHTTP(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "wire-test", env.userID))
	srv := httptest.NewServer(env.router)
	defer srv.Close()
	client := &http.Client{Timeout: 30 * time.Second}
	request := func(ctx context.Context, method, path string, body []byte) *http.Request {
		r, err := http.NewRequestWithContext(ctx, method, srv.URL+path, bytes.NewReader(body))
		require.NoError(t, err)
		signRequestV4(r, env.accessKey, env.secretKey, "us-east-1", "s3")
		return r
	}
	do := func(r *http.Request) faultHTTPResult {
		resp, err := client.Do(r)
		if err != nil {
			return faultHTTPResult{err: err}
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return faultHTTPResult{code: resp.StatusCode, etag: resp.Header.Get("ETag"), body: b, err: err}
	}
	check := func(t *testing.T, r faultHTTPResult, code int) {
		t.Helper()
		require.NoError(t, r.err)
		require.Equal(t, code, r.code, string(r.body))
	}
	create := func(t *testing.T, key string) string {
		r := do(request(t.Context(), "POST", "/wire-test/"+key+"?uploads", nil))
		check(t, r, 200)
		var u InitiateMultipartUploadResult
		require.NoError(t, xml.Unmarshal(r.body, &u))
		return "/wire-test/" + key + "?uploadId=" + u.UploadId
	}
	complete := func(t *testing.T, path string, etags ...string) {
		body := "<CompleteMultipartUpload>"
		for i, tag := range etags {
			body += fmt.Sprintf("<Part><PartNumber>%d</PartNumber><ETag>%s</ETag></Part>", i+1, tag)
		}
		body += "</CompleteMultipartUpload>"
		r := do(request(t.Context(), "POST", path, []byte(body)))
		check(t, r, 200)
		require.Contains(t, string(r.body), "CompleteMultipartUploadResult")
	}
	a, b := bytes.Repeat([]byte("a"), 8<<20), bytes.Repeat([]byte("b"), 8<<20)

	t.Run("different-parts", func(t *testing.T) {
		path := create(t, "parallel")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		release := make(chan struct{})
		var releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		defer unblock()
		started := make(chan struct{})
		r := request(ctx, "PUT", path+"&partNumber=1", a)
		r.Body = io.NopCloser(&faultGateReader{r: bytes.NewReader(a), remaining: 64 << 10, started: started, release: release, ctx: ctx})
		first := make(chan faultHTTPResult, 1)
		go func() { first <- do(r) }()
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("body did not start")
		}
		second := do(request(t.Context(), "PUT", path+"&partNumber=2", b))
		check(t, second, 200)
		unblock()
		finished := <-first
		check(t, finished, 200)
		complete(t, path, finished.etag, second.etag)
		got := do(request(t.Context(), "GET", "/wire-test/parallel", nil))
		check(t, got, 200)
		require.Equal(t, append(append([]byte{}, a...), b...), got.body)
	})

	t.Run("same-part", func(t *testing.T) {
		path := create(t, "same")
		r1 := request(t.Context(), "PUT", path+"&partNumber=1", a)
		r2 := request(t.Context(), "PUT", path+"&partNumber=1", b)
		results := make(chan faultHTTPResult, 2)
		go func() { results <- do(r1) }()
		go func() { results <- do(r2) }()
		one, two := <-results, <-results
		check(t, one, 200)
		check(t, two, 200)
		listed := do(request(t.Context(), "GET", path, nil))
		check(t, listed, 200)
		var parts struct {
			Parts []struct {
				ETag string `xml:"ETag"`
			} `xml:"Part"`
		}
		require.NoError(t, xml.Unmarshal(listed.body, &parts))
		require.Len(t, parts.Parts, 1)
		complete(t, path, parts.Parts[0].ETag)
		got := do(request(t.Context(), "GET", "/wire-test/same", nil))
		check(t, got, 200)
		require.True(t, bytes.Equal(got.body, a) || bytes.Equal(got.body, b))
		require.Equal(t, fmt.Sprintf("%x", md5.Sum(got.body)), strings.Trim(parts.Parts[0].ETag, "\""))
	})

	t.Run("abort-disconnected-part", func(t *testing.T) {
		path := create(t, "aborted")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		release := make(chan struct{})
		started := make(chan struct{})
		r := request(ctx, "PUT", path+"&partNumber=1", a)
		r.Body = io.NopCloser(&faultGateReader{r: bytes.NewReader(a), remaining: 64 << 10, started: started, release: release, ctx: ctx})
		pending := make(chan faultHTTPResult, 1)
		go func() { pending <- do(r) }()
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("body did not start")
		}
		abortReq := request(t.Context(), "DELETE", path, nil)
		aborted := make(chan faultHTTPResult, 1)
		go func() { aborted <- do(abortReq) }()
		cancel()
		require.Error(t, (<-pending).err)
		check(t, <-aborted, 204)
		check(t, do(request(t.Context(), "GET", path, nil)), 404)
		check(t, do(request(t.Context(), "GET", "/wire-test/aborted", nil)), 404)
	})
}
