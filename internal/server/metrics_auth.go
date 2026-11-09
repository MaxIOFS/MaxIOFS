package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// bearerTokenIs reports whether r carries token as its bearer token. An empty
// token matches nothing.
func bearerTokenIs(r *http.Request, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	sent, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(sent)), []byte(token)) == 1
}
