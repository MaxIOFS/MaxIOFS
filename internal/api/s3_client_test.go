package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A browser is sent to the console, except where S3 serves it: a shared object,
// and a form upload to a bucket, whose signature is in the form.
func TestABrowserFormUploadReachesS3(t *testing.T) {
	h := &Handler{}
	browser := func(method, target, contentType string) *http.Request {
		r := httptest.NewRequest(method, target, strings.NewReader("x"))
		r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Firefox/140.0")
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		return r
	}
	form := "multipart/form-data; boundary=----geckoformboundary1"

	assert.True(t, h.isS3Client(browser(http.MethodPost, "/uploads", form)), "a form upload to a bucket")
	assert.True(t, h.isS3Client(browser(http.MethodPost, "/uploads/", form)))
	assert.True(t, h.isS3Client(browser(http.MethodPost, "/", form)), "a form upload to a virtual-hosted bucket")
	assert.True(t, h.isS3Client(browser(http.MethodGet, "/uploads/doc.txt", "")), "a shared object")

	assert.False(t, h.isS3Client(browser(http.MethodGet, "/", "")), "the console's address typed on the S3 port")
	assert.False(t, h.isS3Client(browser(http.MethodGet, "/uploads", "")))
	assert.False(t, h.isS3Client(browser(http.MethodPost, "/uploads", "text/plain")))
	assert.False(t, h.isS3Client(browser(http.MethodPost, "/uploads/doc.txt", form)), "a form is posted to a bucket")
}
