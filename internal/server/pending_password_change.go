package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/maxiofs/maxiofs/internal/auth"
)

// PasswordChangeRequiredCode is what the console keys on to route the user to
// the password form instead of showing an error.
const PasswordChangeRequiredCode = "PASSWORD_CHANGE_REQUIRED"

// pendingPasswordChangeMiddleware makes the obligation real. An administrator
// who hands out a password marks the account, and until the user replaces it
// the session can do nothing else — the console honours this by showing the
// password form, but the refusal does not depend on the console doing so.
func (s *Server) pendingPasswordChangeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		user, ok := auth.GetUserFromContext(r.Context())
		if !ok || user == nil || !user.MustChangePassword {
			next.ServeHTTP(w, r)
			return
		}

		if passwordChangeAllowsPath(r, user.ID) {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "You must set a new password before using the console.",
			"code":  PasswordChangeRequiredCode,
		})
	})
}

// passwordChangeAllowsPath reports whether a request is one of the few an
// account with a pending password change may still make: ending the session,
// refreshing it, reading who it is, reading the rules the new password has to
// satisfy, and setting that password.
func passwordChangeAllowsPath(r *http.Request, userID string) bool {
	path := consoleRelativePath(r.URL.Path)

	switch {
	case strings.HasPrefix(path, "/auth/"):
		return true
	case path == "/health" || path == "/version":
		return true
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/settings"):
		return true
	case r.Method == http.MethodPut && path == "/users/"+userID+"/password":
		return true
	}
	return false
}

// consoleRelativePath strips the API version prefix so the rules above read as
// the routes they guard.
func consoleRelativePath(urlPath string) string {
	const v1 = "/api/v1"
	if idx := strings.Index(urlPath, v1); idx >= 0 {
		rel := urlPath[idx+len(v1):]
		if rel == "" {
			return "/"
		}
		return rel
	}
	return urlPath
}
