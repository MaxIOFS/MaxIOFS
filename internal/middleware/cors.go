package middleware

import (
	"net/http"
	"os"
	"strings"
)

// CORSConfig holds CORS configuration options
type CORSConfig struct {
	// AllowedOrigins is a list of allowed origins. Use "*" for wildcard
	AllowedOrigins []string
	// AllowedMethods is a list of allowed HTTP methods
	AllowedMethods []string
	// AllowedHeaders is a list of allowed headers
	AllowedHeaders []string
	// ExposedHeaders is a list of headers exposed to the client
	ExposedHeaders []string
	// MaxAge indicates how long the browser can cache preflight responses
	MaxAge string
	// AllowCredentials indicates whether credentials are allowed
	AllowCredentials bool
	// CustomOriginValidator is a custom function to validate origins
	CustomOriginValidator func(origin string) bool
}

// DefaultCORSConfig returns the default CORS configuration for S3 compatibility
func DefaultCORSConfig() *CORSConfig {
	// Get allowed origins from environment variable or use defaults
	allowedOrigins := []string{
		"http://localhost:5173", // Vite dev server
	}
	if envOrigins := os.Getenv("MAXIOFS_ALLOWED_ORIGINS"); envOrigins != "" {
		allowedOrigins = strings.Split(envOrigins, ",")
	}

	return &CORSConfig{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{
			"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS",
		},
		AllowedHeaders: []string{
			"Accept",
			"Accept-Language",
			"Content-Language",
			"Content-Type",
			"Authorization",
			// AWS S3 request headers — listed explicitly because browsers do not expand globs (BUG-07)
			"X-Amz-Date",
			"X-Amz-Content-Sha256",
			"X-Amz-Security-Token",
			"X-Amz-User-Agent",
			"X-Amz-Target",
			"X-Amz-Acl",
			"X-Amz-Copy-Source",
			"X-Amz-Copy-Source-Range",
			"X-Amz-Copy-Source-If-Match",
			"X-Amz-Copy-Source-If-None-Match",
			"X-Amz-Copy-Source-If-Modified-Since",
			"X-Amz-Copy-Source-If-Unmodified-Since",
			"X-Amz-Server-Side-Encryption",
			"X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id",
			"X-Amz-Server-Side-Encryption-Customer-Algorithm",
			"X-Amz-Server-Side-Encryption-Customer-Key",
			"X-Amz-Server-Side-Encryption-Customer-Key-Md5",
			"X-Amz-Storage-Class",
			"X-Amz-Website-Redirect-Location",
			"X-Amz-Metadata-Directive",
			"X-Amz-Mfa",
			"X-Amz-Tagging",
			"X-Amz-Object-Lock-Mode",
			"X-Amz-Object-Lock-Retain-Until-Date",
			"X-Amz-Object-Lock-Legal-Hold",
			"X-Amz-Checksum-Algorithm",
			"X-Amz-Checksum-Crc32",
			"X-Amz-Checksum-Crc32c",
			"X-Amz-Checksum-Sha1",
			"X-Amz-Checksum-Sha256",
			"X-Amz-Expected-Bucket-Owner",
			"X-Amz-Sdk-Checksum-Algorithm",
			"X-Requested-With",
			"Cache-Control",
			"Expires",
			"If-Match",
			"If-Modified-Since",
			"If-None-Match",
			"If-Unmodified-Since",
			"Range",
		},
		ExposedHeaders: []string{
			"ETag",
			// AWS S3 response headers — listed explicitly (BUG-07)
			"x-amz-request-id",
			"x-amz-id-2",
			"x-amz-version-id",
			"x-amz-delete-marker",
			"x-amz-storage-class",
			"x-amz-server-side-encryption",
			"x-amz-server-side-encryption-aws-kms-key-id",
			"x-amz-server-side-encryption-customer-algorithm",
			"x-amz-server-side-encryption-customer-key-md5",
			"x-amz-website-redirect-location",
			"x-amz-tagging-count",
			"x-amz-object-lock-mode",
			"x-amz-object-lock-retain-until-date",
			"x-amz-object-lock-legal-hold",
			"x-amz-checksum-crc32",
			"x-amz-checksum-crc32c",
			"x-amz-checksum-sha1",
			"x-amz-checksum-sha256",
			"x-amz-expiration",
			"x-amz-restore",
			"x-amz-replication-status",
			"Content-Length",
			"Content-Range",
			"Content-Type",
			"Date",
			"Last-Modified",
			"Server",
		},
		MaxAge:           "3600",
		AllowCredentials: true,
	}
}

// RestrictiveCORSConfig returns a more restrictive CORS configuration
// CORS returns a middleware that handles CORS headers with default configuration
func CORS() func(http.Handler) http.Handler {
	return CORSWithConfig(DefaultCORSConfig())
}

// CORSWithConfig returns a CORS middleware with custom configuration
func CORSWithConfig(config *CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			if origin != "" && config.isOriginAllowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else if config.hasWildcardOrigin() {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}

			// Set other CORS headers
			if len(config.AllowedMethods) > 0 {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
			}

			if len(config.AllowedHeaders) > 0 {
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
			}

			if len(config.ExposedHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(config.ExposedHeaders, ", "))
			}

			if config.MaxAge != "" {
				w.Header().Set("Access-Control-Max-Age", config.MaxAge)
			}

			// Per RFC 6454 / CORS spec: Access-Control-Allow-Credentials must
			// not be set to "true" when the origin is a wildcard (*), because
			// browsers reject that combination for credentialed requests.
			// We also guard against the reflected-origin case where the config
			// contains "*" as an entry (isOriginAllowed returns true for any
			// origin, so the origin gets reflected — that is equally dangerous).
			if config.AllowCredentials && !config.hasWildcardOrigin() {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isOriginAllowed checks if the given origin is allowed
func (c *CORSConfig) isOriginAllowed(origin string) bool {
	// Check custom validator first
	if c.CustomOriginValidator != nil {
		return c.CustomOriginValidator(origin)
	}

	// Check against allowed origins list
	for _, allowedOrigin := range c.AllowedOrigins {
		if allowedOrigin == "*" {
			return true
		}
		if allowedOrigin == origin {
			return true
		}
		// Support simple wildcard patterns like *.example.com
		if strings.HasPrefix(allowedOrigin, "*.") {
			domain := strings.TrimPrefix(allowedOrigin, "*.")
			if strings.HasSuffix(origin, "."+domain) || origin == domain {
				return true
			}
		}
	}

	return false
}

// hasWildcardOrigin checks if wildcard origin is configured
func (c *CORSConfig) hasWildcardOrigin() bool {
	for _, origin := range c.AllowedOrigins {
		if origin == "*" {
			return true
		}
	}
	return false
}
