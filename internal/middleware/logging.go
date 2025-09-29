package middleware

import (
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// Logging returns a middleware that logs HTTP requests
func Logging() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Create response writer wrapper to capture status code
			wrapped := &responseWriterWrapper{
				ResponseWriter: w,
				statusCode:     200,
			}

			// Process request
			next.ServeHTTP(wrapped, r)

			// Log request
			logrus.WithFields(logrus.Fields{
				"method":     r.Method,
				"path":       r.URL.Path,
				"status":     wrapped.statusCode,
				"duration":   time.Since(start),
				"remote_ip":  r.RemoteAddr,
				"user_agent": r.UserAgent(),
			}).Info("HTTP request")
		})
	}
}

// responseWriterWrapper wraps http.ResponseWriter to capture status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// TODO: Implement advanced logging features in Fase 2.3 - Middleware Implementation