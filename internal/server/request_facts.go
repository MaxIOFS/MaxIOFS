package server

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
)

// policyRequestFacts records what bucket policy conditions test about a
// request: the client address, as getClientIP gives it, and whether the
// request came over TLS, or reached a trusted proxy over HTTPS.
func (s *Server) policyRequestFacts(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		facts := bucket.RequestFacts{
			SourceIP: getClientIP(r, s.proxyTrust()),
			SecureTransport: r.TLS != nil || (fromTrustedProxy(r, s.proxyTrust()) &&
				strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")),
		}
		next.ServeHTTP(w, r.WithContext(bucket.WithRequestFacts(r.Context(), facts)))
	})
}

// withForwardedFacts records, for a request another node forwarded and
// signed, what that node found of it: its client's address, and whether it
// came over HTTPS.
func withForwardedFacts(ctx context.Context, r *http.Request) context.Context {
	facts := bucket.RequestFactsFrom(ctx)
	if ip := net.ParseIP(r.Header.Get(cluster.ClientIPHeader)); ip != nil {
		facts.SourceIP = ip.String()
	}
	facts.SecureTransport = r.Header.Get(cluster.SecureTransportHeader) == "true"
	return bucket.WithRequestFacts(ctx, facts)
}

// fromTrustedProxy reports whether the direct peer of the request is a proxy
// whose forwarding headers are honoured.
func fromTrustedProxy(r *http.Request, trust proxyTrust) bool {
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && isIPTrusted(ip, trust)
}
