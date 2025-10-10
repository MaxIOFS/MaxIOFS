package middleware

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// VerboseLogging returns a middleware that logs EVERYTHING
func VerboseLogging() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Log incoming request with ALL details
			logrus.WithFields(logrus.Fields{
				"method":         r.Method,
				"url":            r.URL.String(),
				"path":           r.URL.Path,
				"query":          r.URL.RawQuery,
				"proto":          r.Proto,
				"remote_addr":    r.RemoteAddr,
				"host":           r.Host,
				"user_agent":     r.Header.Get("User-Agent"),
				"referer":        r.Header.Get("Referer"),
				"content_type":   r.Header.Get("Content-Type"),
				"content_length": r.ContentLength,
			}).Info("📥 INCOMING REQUEST")

			// Log ALL headers
			logrus.Info("📋 REQUEST HEADERS:")
			for name, values := range r.Header {
				for _, value := range values {
					logrus.Infof("  %s: %s", name, value)
				}
			}

			// Read and log request body (for small requests)
			var requestBody []byte
			if r.Body != nil && r.ContentLength > 0 && r.ContentLength < 10000 {
				requestBody, _ = io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewBuffer(requestBody))

				if len(requestBody) > 0 {
					bodyStr := string(requestBody)
					if len(bodyStr) > 500 {
						bodyStr = bodyStr[:500] + "..."
					}
					logrus.WithField("body", bodyStr).Info("📝 REQUEST BODY")
				}
			}

			// Wrap response writer to capture response
			rw := &verboseResponseWriter{
				ResponseWriter: w,
				body:           &bytes.Buffer{},
			}

			// Process request
			next.ServeHTTP(rw, r)

			// Calculate duration
			duration := time.Since(start)

			// Log response
			logrus.WithFields(logrus.Fields{
				"status":      rw.statusCode,
				"size":        rw.size,
				"duration_ms": duration.Milliseconds(),
			}).Info("📤 RESPONSE")

			// Log response headers
			logrus.Info("📋 RESPONSE HEADERS:")
			for name, values := range rw.Header() {
				for _, value := range values {
					logrus.Infof("  %s: %s", name, value)
				}
			}

			// Log response body (if small)
			if rw.body.Len() > 0 && rw.body.Len() < 1000 {
				bodyStr := rw.body.String()
				if !strings.Contains(rw.Header().Get("Content-Type"), "image") &&
					!strings.Contains(rw.Header().Get("Content-Type"), "octet-stream") {
					logrus.WithField("body", bodyStr).Info("📝 RESPONSE BODY")
				}
			}

			logrus.WithField("duration", duration).Info("✅ REQUEST COMPLETED")
			logrus.Info("=" + strings.Repeat("=", 80))
		})
	}
}

type verboseResponseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int64
	body       *bytes.Buffer
}

func (rw *verboseResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *verboseResponseWriter) Write(b []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = 200
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.size += int64(n)

	// Capture body for logging
	if rw.body.Len() < 1000 {
		rw.body.Write(b)
	}

	return n, err
}
