package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The website's own 404 page shows the requested path as text, never as markup.
func TestWebsiteNotFoundEscapesThePath(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	const b = "website-404-escape"
	require.NoError(t, server.bucketManager.CreateBucket(ctx, "", b, ""))
	require.NoError(t, server.bucketManager.SetWebsite(ctx, "", b, &bucket.WebsiteConfig{IndexDocument: "index.html"}))

	rr := httptest.NewRecorder()
	server.handleWebsiteRequest(rr, httptest.NewRequest(http.MethodGet, "/%3Cscript%3Ealert(1)%3C/script%3E", nil), b)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.NotContains(t, rr.Body.String(), "<script>")
	assert.Contains(t, rr.Body.String(), "&lt;script&gt;alert(1)&lt;/script&gt;")
}
