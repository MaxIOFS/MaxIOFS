package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An object is shared once: sharing it again gives the share it has. The link
// is the object's address on the S3 API.
func TestAnObjectIsSharedOnceAtItsS3Address(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, member, _, _ := userUpdateFixture(t, s)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "files", tenantAdmin.ID))
	_, err := s.objectManager.PutObject(ctx, "acme/files", "a b", bytes.NewReader([]byte("x")), http.Header{})
	require.NoError(t, err)
	_, err = s.authManager.GenerateAccessKey(ctx, tenantAdmin.ID)
	require.NoError(t, err)
	s.config.PublicAPIURL = "https://s3.example.com"
	type answer struct {
		ID        string     `json:"id"`
		URL       string     `json:"url"`
		ExpiresAt *time.Time `json:"expiresAt"`
		Existing  bool       `json:"existing"`
		IsExpired *bool      `json:"isExpired"`
	}
	share := func(user *auth.User, bucketName, body string, actions ...string) *httptest.ResponseRecorder {
		req := mux.SetURLVars(consoleRequestWith(user, body, actions...), map[string]string{"bucket": bucketName, "object": "a b"})
		rec := httptest.NewRecorder()
		s.handleShareObject(rec, req)
		return rec
	}

	rec := httptest.NewRecorder()
	s.handleShareObject(rec, mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/", nil), map[string]string{"bucket": "files", "object": "a b"}))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, http.StatusForbidden, share(tenantAdmin, "files", `{"expiresIn":60}`).Code)
	assert.Equal(t, http.StatusNotFound, share(tenantAdmin, "nowhere", `{"expiresIn":60}`, "s3:*").Code)
	assert.Equal(t, http.StatusBadRequest, share(member, "files", `{"expiresIn":60}`, "s3:*").Code, "no access key")

	first := consoleData[answer](t, share(tenantAdmin, "files", `{"expiresIn":60}`, "s3:*"), http.StatusOK)
	assert.False(t, first.Existing)
	require.NotNil(t, first.IsExpired)
	assert.False(t, *first.IsExpired)
	assert.Equal(t, "https://s3.example.com/files/a%20b", first.URL)
	require.NotNil(t, first.ExpiresAt)
	assert.WithinDuration(t, time.Now().Add(time.Minute), *first.ExpiresAt, 10*time.Second)
	again := consoleData[answer](t, share(tenantAdmin, "files", `{"expiresIn":3600}`, "s3:*"), http.StatusOK)
	assert.True(t, again.Existing)
	assert.Equal(t, first.ID, again.ID)
	assert.Equal(t, first.URL, again.URL)
}

// Without a public S3 URL configured, a share link and a presigned URL point
// at the request's host on the S3 port, never at the console's.
func TestConsoleLinksPointAtTheS3Port(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "files", tenantAdmin.ID))
	_, err := s.objectManager.PutObject(ctx, "acme/files", "doc", bytes.NewReader([]byte("x")), http.Header{})
	require.NoError(t, err)
	_, err = s.authManager.GenerateAccessKey(ctx, tenantAdmin.ID)
	require.NoError(t, err)
	s.config.PublicAPIURL = ""
	s.config.Listen = "0.0.0.0:9000"
	link := func(handler http.HandlerFunc, host string, secure ...bool) string {
		req := mux.SetURLVars(consoleRequestWith(tenantAdmin, `{"expiresIn":60}`, "s3:*"), map[string]string{"bucket": "files", "object": "doc"})
		req.Host = host
		if len(secure) > 0 {
			req.TLS = &tls.ConnectionState{}
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		return consoleData[struct {
			URL string `json:"url"`
		}](t, rec, http.StatusOK).URL
	}

	for _, host := range []string{"console.example.com:8081", "console.example.com"} {
		assert.Equal(t, "http://console.example.com:9000/files/doc", link(s.handleShareObject, host), host)
		require.NoError(t, s.shareManager.DeleteBucketShares(ctx, "files", "acme"))
		assert.True(t, strings.HasPrefix(link(s.handleGeneratePresignedURL, host), "http://console.example.com:9000/files/doc?"), host)
	}
	require.NoError(t, s.shareManager.DeleteBucketShares(ctx, "files", "acme"))
	assert.Equal(t, "https://console.example.com:9000/files/doc", link(s.handleShareObject, "console.example.com", true), "over TLS")
}
