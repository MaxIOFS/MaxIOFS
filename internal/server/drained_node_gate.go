package server

import (
	"fmt"
	"net/http"
)

// drainedNodeGate answers every S3 request with 503 once this node, drained,
// holds no data, the health check included, so that load balancers stop
// sending it requests. The console still answers.
func (s *Server) drainedNodeGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.clusterManager == nil || !s.clusterManager.LocalDrainDone(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+
			`<Error><Code>ServiceUnavailable</Code>`+
			`<Message>This node is drained and serves no request; send it to another node of the cluster.</Message>`+
			`</Error>`)
	})
}
