package middleware

import (
	"net/http"
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
	return &CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{
			"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS",
		},
		AllowedHeaders: []string{
			"Accept",
			"Accept-Language",
			"Content-Language",
			"Content-Type",
			"Authorization",
			"X-Amz-*",
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
			"x-amz-*",
			"Content-Length",
			"Content-Range",
			"Content-Type",
			"Date",
			"Last-Modified",
			"Server",
		},
		MaxAge:           "3600",
		AllowCredentials: false,
	}
}

// RestrictiveCORSConfig returns a more restrictive CORS configuration
func RestrictiveCORSConfig() *CORSConfig {
	return &CORSConfig{
		AllowedOrigins: []string{}, // No origins allowed by default
		AllowedMethods: []string{"GET", "HEAD", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept",
			"Content-Type",
			"Authorization",
		},
		ExposedHeaders: []string{
			"Content-Length",
			"Content-Type",
		},
		MaxAge:           "1800",
		AllowCredentials: false,
	}
}

// DisabledCORSConfig returns a configuration that disables CORS
func DisabledCORSConfig() *CORSConfig {
	return &CORSConfig{
		AllowedOrigins: []string{},
		AllowedMethods: []string{},
		AllowedHeaders: []string{},
		ExposedHeaders: []string{},
		MaxAge:         "0",
	}
}

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

			if config.AllowCredentials {
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