package auth

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixedSettings struct {
	enabled bool
	rate    int
}

func (f fixedSettings) GetInt(string) (int, error)   { return f.rate, nil }
func (f fixedSettings) GetBool(string) (bool, error) { return f.enabled, nil }

func rateLimited(t *testing.T, settings fixedSettings, requests int) *httptest.ResponseRecorder {
	t.Helper()
	limiter := NewAPIRateLimiter()
	defer limiter.Stop()

	handler := APIRateLimitMiddleware(settings, limiter, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	var rec *httptest.ResponseRecorder
	for i := 0; i < requests; i++ {
		req := httptest.NewRequest(http.MethodPut, "/bucket/object", nil)
		req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AKIAEXAMPLE/20260914/us-east-1/s3/aws4_request")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
	return rec
}

func TestAPIRateLimit_AnswersSlowDownSoClientsRetry(t *testing.T) {
	rec := rateLimited(t, fixedSettings{enabled: true, rate: 2}, 5)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "1", rec.Header().Get("Retry-After"))
	assert.Equal(t, "application/xml", rec.Header().Get("Content-Type"))

	var doc struct {
		XMLName xml.Name `xml:"Error"`
		Code    string   `xml:"Code"`
	}
	require.NoError(t, xml.Unmarshal(rec.Body.Bytes(), &doc),
		"the body must be an S3 error document: %s", rec.Body.String())
	assert.Equal(t, "SlowDown", doc.Code)
}

// A HEAD answer has no body: its code goes in a header, or nothing could tell a
// SlowDown from a failure.
func TestAPIRateLimit_NamesSlowDownOnAHead(t *testing.T) {
	limiter := NewAPIRateLimiter()
	defer limiter.Stop()
	handler := APIRateLimitMiddleware(fixedSettings{enabled: true, rate: 1}, limiter, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	var rec *httptest.ResponseRecorder
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodHead, "/bucket/object", nil)
		req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AKIAEXAMPLE/20260914/us-east-1/s3/aws4_request")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "SlowDown", rec.Header().Get("x-amz-error-code"))
	assert.Empty(t, rec.Body.String())
}

// An anonymous client counts once however many connections it opens, and
// behind a proxy it is the client the proxy names.
func TestAPIRateLimit_AnonymousRequestsCountPerClient(t *testing.T) {
	limiter := NewAPIRateLimiter()
	defer limiter.Stop()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	direct := APIRateLimitMiddleware(fixedSettings{enabled: true, rate: 1}, limiter, nil)(ok)
	proxied := APIRateLimitMiddleware(fixedSettings{enabled: true, rate: 1}, limiter,
		func(r *http.Request) string { return r.Header.Get("X-Forwarded-For") })(ok)
	send := func(handler http.Handler, peer, forwarded string) int {
		req := httptest.NewRequest(http.MethodGet, "/bucket/object", nil)
		req.RemoteAddr = peer
		if forwarded != "" {
			req.Header.Set("X-Forwarded-For", forwarded)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusOK, send(direct, "10.0.0.1:1000", ""))
	assert.Equal(t, http.StatusServiceUnavailable, send(direct, "10.0.0.1:2000", ""), "another connection of the same client")

	assert.Equal(t, http.StatusOK, send(proxied, "127.0.0.1:1", "10.0.0.2"))
	assert.Equal(t, http.StatusOK, send(proxied, "127.0.0.1:2", "10.0.0.3"), "another client behind the same proxy")
	assert.Equal(t, http.StatusServiceUnavailable, send(proxied, "127.0.0.1:3", "10.0.0.2"))
}

// The warning of a request held back names whom: the access key of a signed
// one, the client of an anonymous one. Every signature starts the same way.
func TestAPIRateLimit_TheWarningNamesTheClient(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()

	rateLimited(t, fixedSettings{enabled: true, rate: 1}, 2)
	require.NotNil(t, hook.LastEntry())
	assert.Equal(t, "AKIAEXAMPLE", hook.LastEntry().Data["client"])

	limiter := NewAPIRateLimiter()
	defer limiter.Stop()
	handler := APIRateLimitMiddleware(fixedSettings{enabled: true, rate: 1}, limiter, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/bucket/object", nil)
		req.RemoteAddr = "10.0.0.9:4000"
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	assert.Equal(t, "10.0.0.9", hook.LastEntry().Data["client"])
}

func TestAPIRateLimit_ZeroMeansNoLimit(t *testing.T) {
	rec := rateLimited(t, fixedSettings{enabled: true, rate: 0}, 50)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAPIRateLimit_DisabledLetsEverythingThrough(t *testing.T) {
	rec := rateLimited(t, fixedSettings{enabled: false, rate: 1}, 50)
	assert.Equal(t, http.StatusOK, rec.Code)
}
