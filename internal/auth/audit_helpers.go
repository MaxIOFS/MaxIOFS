package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/maxiofs/maxiofs/internal/audit"
)

// logAuditEvent is a helper function to log audit events
// It safely checks if audit manager is available before logging
func (am *authManager) logAuditEvent(ctx context.Context, event *audit.AuditEvent) {
	if am.auditManager == nil {
		return
	}

	_ = am.auditManager.LogEvent(ctx, event)
}

// extractIPFromRequest extracts the client IP address from HTTP request
func extractIPFromRequest(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP if there are multiple
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	// RemoteAddr format is "IP:port", we only want IP
	parts := strings.Split(r.RemoteAddr, ":")
	if len(parts) > 0 {
		return parts[0]
	}

	return r.RemoteAddr
}

// extractUserAgentFromRequest extracts the user agent from HTTP request
func extractUserAgentFromRequest(r *http.Request) string {
	return r.Header.Get("User-Agent")
}
