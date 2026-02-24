package middleware

import (
	"fmt"
	"net/http"
)

// MaintenanceModeS3 returns middleware that blocks S3 write operations when maintenance
// mode is active. isEnabled is called on every request so changes take effect immediately
// without restarting the server.
func MaintenanceModeS3(isEnabled func() bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Safe (read-only) methods are always allowed.
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			if isEnabled() {
				w.Header().Set("Content-Type", "application/xml")
				w.Header().Set("Retry-After", "3600")
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+
					`<Error><Code>ServiceUnavailable</Code>`+
					`<Message>Server is in maintenance mode. Only read operations are allowed.</Message>`+
					`</Error>`)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
