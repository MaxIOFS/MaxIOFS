package server

// The vote endpoint, and the gate that keeps configuration writes on one node.
//
// Object traffic is untouched by any of this: every node serves reads and
// writes as before. What is gated is the control plane — creating users,
// editing policies, granting buckets — because two nodes writing the same
// entity converge on last-write-wins with one-second resolution, which can
// leave the cluster permanently disagreeing.

import (
	"encoding/json"
	"net/http"

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
// forwards it to the coordinator. It reports whether the caller should continue
// serving the request locally.
//
// Forwarding rather than refusing is the point: which node arbitrates is the
// cluster's business, not the user's. Somebody editing a policy saves it from
// whichever node they happened to open, and it works.
func (s *Server) requireCoordinator(w http.ResponseWriter, r *http.Request) bool {
	// Without a cluster there is nobody to disagree with.
	if s.leaderMgr == nil || s.clusterManager == nil || !s.clusterManager.IsClusterEnabled() {
		return true
	}
	if s.leaderMgr.IsLeader() {
		return true
	}

	// A request that has already been forwarded is not forwarded again. If it
	// arrived here it is because this node was the coordinator a moment ago and
	// no longer is; bouncing it onward could loop between nodes that disagree.
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
//
// It gates by method rather than by an explicit list of paths: every mutating
// console request is a configuration change, and a list would silently miss
// whatever endpoint gets added next. The exemptions are the requests that write
// nothing shared, or that must keep working precisely when the cluster is
// unhealthy.
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
		// Signing in must work on any node — requiring the coordinator would
		// lock everybody out during an election, which is when an operator is
		// most likely to be trying to log in.
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
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			return true
		}
		if containsSegment(path, prefix) {
			return true
		}
	}
	return false
}

// containsSegment reports whether a path contains the given segment, so
// "/api/v1/buckets/x/objects" matches the "/objects" exemption.
func containsSegment(path, segment string) bool {
	for i := 0; i+len(segment) <= len(path); i++ {
		if path[i:i+len(segment)] == segment {
			return true
		}
	}
	return false
}
