package server

import (
	"net"
	"net/http"
	"strings"

	"github.com/maxiofs/maxiofs/internal/bucket"
)

// policyRequestFacts records what bucket policy conditions test about a
// request: the client address, as getClientIP gives it, and whether the
// request came over TLS, or reached a trusted proxy over HTTPS.
func (s *Server) policyRequestFacts(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		facts := bucket.RequestFacts{
			SourceIP: getClientIP(r, s.config.TrustedProxies),
			SecureTransport: r.TLS != nil || (fromTrustedProxy(r, s.config.TrustedProxies) &&
				strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")),
		}
		next.ServeHTTP(w, r.WithContext(bucket.WithRequestFacts(r.Context(), facts)))
	})
}

// fromTrustedProxy reports whether the direct peer of the request is a proxy
// whose forwarding headers are honoured.
func fromTrustedProxy(r *http.Request, trustedProxies []string) bool {
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && isIPTrusted(ip, trustedProxies)
}
