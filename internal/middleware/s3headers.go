package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
)

// ConsoleHeaders adds security headers to all console (web UI) responses.
// Surrogate-Control: no-store tells nginx/Varnish proxy caches not to store
// the response. Cache-Control: no-store is the browser-side equivalent for
// the HTML entry point and API responses; individual asset handlers override
// this for content-hashed immutable files.
func ConsoleHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Surrogate-Control", "no-store")
			h.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
			h.Set("Pragma", "no-cache")
			h.Set("X-Accel-Expires", "0")
			h.Set("X-Frame-Options", "SAMEORIGIN")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Xss-Protection", "1; mode=block")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy",
				"default-src 'self'; "+
					"script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+
					"style-src 'self' 'unsafe-inline'; "+
					"img-src 'self' data: blob:; "+
					"font-src 'self' data:; "+
					"connect-src 'self'; "+
					"frame-ancestors 'self';")
			next.ServeHTTP(w, r)
		})
	}
}

// S3HeadersMiddleware adds S3-compatible headers to ALL responses
// This ensures VEEAM and other S3 clients receive proper headers even on auth errors
func S3Headers() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Add S3 headers BEFORE processing the request
			// This ensures they're present even if middleware chain returns early
			addS3Headers(w)

			next.ServeHTTP(w, r)
		})
	}
}

// addS3Headers adds all S3-compatible response headers
func addS3Headers(w http.ResponseWriter) {
	// X-Amz-Request-Id: 16 character hex string (like MinIO)
	w.Header().Set("X-Amz-Request-Id", generateRequestID())

	// X-Amz-Id-2: 64 character hex string (host ID)
	w.Header().Set("X-Amz-Id-2", generateHostID())

	// Server header - identify as MaxIOFS
	w.Header().Set("Server", "MaxIOFS")

	// Accept ranges for partial content support
	w.Header().Set("Accept-Ranges", "bytes")

	// Security headers (same as MinIO)
	w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Xss-Protection", "1; mode=block")

	// Vary headers for proper caching
	w.Header().Set("Vary", "Origin, Accept-Encoding")
}

// generateRequestID generates a 16 character hex request ID
func generateRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

// generateHostID generates a base64-encoded host ID (like AWS S3 and MinIO)
func generateHostID() string {
	b := make([]byte, 48) // 48 bytes → 64-char base64
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}
