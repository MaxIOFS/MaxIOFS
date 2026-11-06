package auth

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

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

	handler := APIRateLimitMiddleware(settings, limiter)(
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
	handler := APIRateLimitMiddleware(fixedSettings{enabled: true, rate: 1}, limiter)(
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

func TestAPIRateLimit_ZeroMeansNoLimit(t *testing.T) {
	rec := rateLimited(t, fixedSettings{enabled: true, rate: 0}, 50)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAPIRateLimit_DisabledLetsEverythingThrough(t *testing.T) {
	rec := rateLimited(t, fixedSettings{enabled: false, rate: 1}, 50)
	assert.Equal(t, http.StatusOK, rec.Code)
}
