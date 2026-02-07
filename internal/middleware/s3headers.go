package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

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

// generateHostID generates a 64 character hex host ID
func generateHostID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
