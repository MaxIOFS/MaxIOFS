package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleLeaderLease answers a candidate asking for this node's vote.
// POST /api/internal/cluster/leader-lease
func (s *Server) handleLeaderLease(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if _, ok := ctx.Value("cluster_node_id").(string); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if s.leaderMgr == nil {
		http.Error(w, "Leader election not available", http.StatusServiceUnavailable)
		return
	}

	var req cluster.LeaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.CandidateID == "" || req.Term <= 0 {
		http.Error(w, "A candidate and a term are required", http.StatusBadRequest)
		return
	}

	answer := s.leaderMgr.GrantLease(ctx, &req)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(answer)
}

// requireCoordinator lets a configuration write proceed on this node, or
func (s *Server) requireCoordinator(w http.ResponseWriter, r *http.Request) bool {
	// Without a cluster there is nobody to disagree with.
	if s.leaderMgr == nil || s.clusterManager == nil || !s.clusterManager.IsClusterEnabled() {
		return true
	}
	if s.leaderMgr.IsLeader() {
		return true
	}

	if r.Header.Get(forwardedHeader) == "true" {
		w.Header().Set("Retry-After", "5")
		s.writeError(w,
			"The cluster coordinator changed while this request was in flight. Try again.",
			http.StatusServiceUnavailable)
		return false
	}

	logrus.WithFields(logrus.Fields{
		"path":   r.URL.Path,
		"method": r.Method,
	}).Debug("Forwarding a configuration change to the coordinator")

	s.forwardToCoordinator(w, r)
	return false
}

// coordinatorMiddleware gates the console's configuration writes.
func (s *Server) coordinatorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		if coordinatorExemptPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		if !s.requireCoordinator(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// coordinatorExemptPath reports whether a mutating request is not a
// configuration change, and so may be served by any node.
func coordinatorExemptPath(path string) bool {
	exempt := []string{
		"/auth/login",
		"/auth/logout",
		"/auth/refresh",
		"/auth/2fa",

		// Node-to-node traffic carries its own coordination, and gating it
		// would stop the cluster from healing itself.
		"/api/internal/",

		// Object and bucket data, which is owned per bucket rather than
		// globally, and needs no arbitration.
		"/objects",
		"/upload",
		"/download",
	}

	for _, prefix := range exempt {
		if strings.HasPrefix(path, prefix) {
			return true
		}
		if hasPathSegment(path, prefix) {
			return true
		}
	}
	return false
}

// hasPathSegment reports whether a path contains the given "/name" as a whole
func hasPathSegment(path, segment string) bool {
	name := strings.TrimPrefix(segment, "/")
	for _, part := range strings.Split(path, "/") {
		if part == name {
			return true
		}
	}
	return false
}
