package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// A bucket's CORS rules answer a browser in the tenant the bucket is in: a
// preflight carries no credentials. One "*" may stand anywhere in an allowed
// origin, as in AWS.
func TestBucketCORSAnswersABrowserForATenantsBucket(t *testing.T) {
	dir, err := os.MkdirTemp("", "maxiofs-cors-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: dir, Logger: logrus.StandardLogger()})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.CreateBucket(context.Background(), &metadata.BucketMetadata{Name: "web", TenantID: "t1", OwnerID: "u"}))

	mockBucket := new(MockBucketManager)
	mockMetrics := new(MockMetricsManager)
	mockMetrics.On("RecordAuthAttempt", mock.Anything, mock.Anything, mock.Anything).Maybe()
	h := NewHandler(mockBucket, new(MockObjectManager), new(MockAuthManager), mockMetrics, store, nil,
		"http://localhost:8080", "http://localhost:8081", ":8081", dir, nil)
	mockBucket.On("GetCORS", mock.Anything, "t1", "web").Return(&bucket.CORSConfig{CORSRules: []bucket.CORSRule{
		{AllowedOrigins: []string{"https://app.example"}, AllowedMethods: []string{"GET", "PUT"}, AllowedHeaders: []string{"x-amz-*", "Content-Type"}},
		{AllowedOrigins: []string{"https://*.example.org"}, AllowedMethods: []string{"GET"}},
		{AllowedOrigins: []string{"start*end"}, AllowedMethods: []string{"GET"}},
	}}, nil)
	mockBucket.On("GetCORS", mock.Anything, "", "web").Return(nil, bucket.ErrBucketNotFound).Maybe()

	mw := h.BucketCORSMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	allowed := func(method, origin, requestMethod string) string {
		r := httptest.NewRequest(method, "/web/page.html", nil)
		r.Header.Set("Origin", origin)
		if requestMethod != "" {
			r.Header.Set("Access-Control-Request-Method", requestMethod)
		}
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, r)
		return w.Header().Get("Access-Control-Allow-Origin")
	}

	assert.Equal(t, "https://app.example", allowed(http.MethodOptions, "https://app.example", "PUT"), "a preflight")
	assert.Equal(t, "https://app.example", allowed(http.MethodGet, "https://app.example", ""))
	assert.Equal(t, "https://a.example.org", allowed(http.MethodGet, "https://a.example.org", ""))
	assert.Equal(t, "startXend", allowed(http.MethodGet, "startXend", ""))
	assert.Empty(t, allowed(http.MethodGet, "https://evil.example", ""))
	assert.Empty(t, allowed(http.MethodOptions, "https://a.example.org", "PUT"), "a method its rule does not allow")

	// A preflight's headers are allowed by the rule, without case, and named back.
	preflight := func(headers string) http.Header {
		r := httptest.NewRequest(http.MethodOptions, "/web/page.html", nil)
		r.Header.Set("Origin", "https://app.example")
		r.Header.Set("Access-Control-Request-Method", "PUT")
		r.Header.Set("Access-Control-Request-Headers", headers)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, r)
		return w.Header()
	}
	got := preflight("X-Amz-Date, content-type")
	assert.Equal(t, "https://app.example", got.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "x-amz-date, content-type", got.Get("Access-Control-Allow-Headers"))
	assert.Empty(t, preflight("authorization").Get("Access-Control-Allow-Origin"), "a header no rule allows")
}

// A preflight no rule allows is refused as AWS refuses it: 403, and 400 when it
// names no origin or no method.
func TestAPreflightNoRuleAllowsIsRefused(t *testing.T) {
	h, _, _, _ := setupTestHandler()
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	send := func(origin, method string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodOptions, "/web/page.html", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if method != "" {
			r.Header.Set("Access-Control-Request-Method", method)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	w := send("https://app.example", "PUT")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "<Code>AccessForbidden</Code>")
	assert.Equal(t, http.StatusBadRequest, send("", "PUT").Code)
	assert.Equal(t, http.StatusBadRequest, send("https://app.example", "").Code)
}
