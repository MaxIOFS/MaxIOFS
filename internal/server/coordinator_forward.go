package server

// Forwarding configuration writes to the coordinator.
//
// One node writes configuration, so two nodes cannot edit the same entity at
// once. That constraint belongs to the cluster, not to the person using it: a
// user who opens the console on any node saves their change and it works. The
// node they reached forwards the request to the coordinator and returns its
// answer.
//
// The forward travels over the inter-node channel, which is already
// HMAC-authenticated. The user's own credential goes with it and the
// coordinator validates that itself — the forwarding node vouches for nothing,
// it only carries. A node that lied about who the user is would gain nothing,
// because the token still has to verify against the shared secret.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/sirupsen/logrus"
)

// forwardedHeader marks a request that has already been forwarded once, so a
// cluster that briefly disagrees about who coordinates cannot bounce a request
// between nodes.
const forwardedHeader = "X-MaxIOFS-Coordinator-Forward"

// maxForwardedBody bounds what will be buffered to forward. Configuration
// requests are small; object data never comes through here.
const maxForwardedBody = 8 << 20

// forwardToCoordinator sends a configuration write to the coordinator and
// copies its answer back. It reports whether the request was handled.
func (s *Server) forwardToCoordinator(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()

	leaderID := s.leaderMgr.LeaderID(ctx)
	if leaderID == "" {
		// An election is in progress. Seconds, not an outage — say so, and say
		// that retrying is the right move.
		w.Header().Set("Retry-After", "5")
		s.writeError(w,
			"The cluster is choosing a coordinator for configuration changes. Try again in a few seconds.",
			http.StatusServiceUnavailable)
		return true
	}

	node, err := s.clusterManager.GetNode(ctx, leaderID)
	if err != nil || node == nil {
		s.writeError(w,
			"The cluster coordinator is not reachable from this node. Try again in a few seconds.",
			http.StatusServiceUnavailable)
		return true
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxForwardedBody+1))
	if err != nil {
		s.writeError(w, "Could not read the request", http.StatusBadRequest)
		return true
	}
	if len(body) > maxForwardedBody {
		s.writeError(w, "Request too large to forward", http.StatusRequestEntityTooLarge)
		return true
	}
	_ = r.Body.Close()

	localNodeID, err := s.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		s.writeError(w, "Could not identify this node", http.StatusInternalServerError)
		return true
	}
	nodeToken, err := s.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		s.writeError(w, "Could not authenticate to the coordinator", http.StatusInternalServerError)
		return true
	}

	url := fmt.Sprintf("%s/api/internal/cluster/console-write%s", node.Endpoint, r.URL.RequestURI())

	forwardCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	proxy := s.clusterManager.ProxyClient()
	req, err := proxy.CreateAuthenticatedRequest(forwardCtx, r.Method, url, bytes.NewReader(body),
		localNodeID, nodeToken)
	if err != nil {
		s.writeError(w, "Could not build the forwarded request", http.StatusInternalServerError)
		return true
	}

	// The user's own credential and content type travel with the request; the
	// coordinator authenticates and authorizes it exactly as if the user had
	// reached that node directly.
	if authorization := r.Header.Get("Authorization"); authorization != "" {
		req.Header.Set("X-MaxIOFS-Forwarded-Authorization", authorization)
	}
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		req.Header.Set("X-MaxIOFS-Forwarded-Content-Type", contentType)
	}
	if tenant := r.Header.Get("X-Tenant-ID"); tenant != "" {
		req.Header.Set("X-MaxIOFS-Forwarded-Tenant", tenant)
	}
	req.Header.Set(forwardedHeader, "true")

	resp, err := proxy.DoAuthenticatedRequest(req)
	if err != nil {
		logrus.WithError(err).WithField("coordinator", leaderID).
			Warn("Could not reach the coordinator to apply a configuration change")
		w.Header().Set("Retry-After", "5")
		s.writeError(w,
			"The cluster coordinator did not answer. Your change was not applied; try again in a few seconds.",
			http.StatusServiceUnavailable)
		return true
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
	return true
}

// handleCoordinatorWrite executes a configuration write forwarded by another
// node. Reached over the inter-node channel, so the HMAC middleware has already
// established that a cluster member sent it.
// POST|PUT|DELETE /api/internal/cluster/console-write/{original path}
func (s *Server) handleCoordinatorWrite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if _, ok := ctx.Value("cluster_node_id").(string); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Only the coordinator applies these. A node that is not coordinator
	// refuses rather than forwarding again, which is what stops a request
	// bouncing around a cluster that momentarily disagrees.
	if s.leaderMgr == nil || !s.leaderMgr.IsLeader() {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "This node is no longer the coordinator", http.StatusServiceUnavailable)
		return
	}

	const prefix = "/api/internal/cluster/console-write"
	originalPath := r.URL.Path[len(prefix):]
	if originalPath == "" {
		http.Error(w, "Missing the original request path", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxForwardedBody+1))
	if err != nil {
		http.Error(w, "Could not read the forwarded request", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	target := originalPath
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	replay := httptest.NewRequest(r.Method, target, bytes.NewReader(body))
	// Restore what the user sent, so the console router authenticates and
	// authorizes the request as if they had reached this node themselves.
	if authorization := r.Header.Get("X-MaxIOFS-Forwarded-Authorization"); authorization != "" {
		replay.Header.Set("Authorization", authorization)
	}
	if contentType := r.Header.Get("X-MaxIOFS-Forwarded-Content-Type"); contentType != "" {
		replay.Header.Set("Content-Type", contentType)
	}
	if tenant := r.Header.Get("X-MaxIOFS-Forwarded-Tenant"); tenant != "" {
		replay.Header.Set("X-Tenant-ID", tenant)
	}
	replay.Header.Set(forwardedHeader, "true")
	replay = replay.WithContext(ctx)

	if s.consoleRouter == nil {
		http.Error(w, "Console routes are not available on this node", http.StatusServiceUnavailable)
		return
	}

	recorder := httptest.NewRecorder()
	s.consoleRouter.ServeHTTP(recorder, replay)

	for key, values := range recorder.Header() {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(recorder.Code)
	_, _ = w.Write(recorder.Body.Bytes())
}
