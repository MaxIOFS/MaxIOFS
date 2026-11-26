package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// consoleRequest is what a console session of the user id sends a node.
func consoleRequest(t *testing.T, s *Server, id, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	user, err := s.authManager.GetUser(context.Background(), id)
	require.NoError(t, err)
	token, err := s.authManager.GenerateJWT(context.Background(), user)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.consoleServer.Handler.ServeHTTP(rec, req)
	return rec
}

// A configuration change made on a node that does not coordinate is made by
// the coordinator, elected by the nodes, and answered as the coordinator
// answers it; while there is none, it is refused, to be tried again.
func TestAChangeIsMadeByTheElectedCoordinator(t *testing.T) {
	c := newHACluster(t, 2, 1)
	a, b := c.nodes[0], c.nodes[1]
	ctx := context.Background()
	for _, node := range c.nodes {
		require.NoError(t, node.leaderMgr.EnsureSchema())
	}

	rec := consoleRequest(t, b.Server, routingAdminID, http.MethodPost, "/api/v1/groups", `{"name":"before"}`)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	assert.Equal(t, "5", rec.Header().Get("Retry-After"))

	// Only a campaigns: b votes, through its cluster port.
	a.leaderMgr.Start(ctx)
	t.Cleanup(a.leaderMgr.Stop)
	require.Eventually(t, a.leaderMgr.IsLeader, 20*time.Second, 50*time.Millisecond, "a is elected")
	assert.Equal(t, a.id, b.leaderMgr.LeaderID(ctx), "b granted a the lease")

	rec = consoleRequest(t, b.Server, routingAdminID, http.MethodPost, "/api/v1/groups", `{"name":"ops","displayName":"Ops"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "application/json", rec.Result().Header.Get("Content-Type"))
	var created struct {
		Data struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "ops", created.Data.Name)
	made, err := a.authManager.GetGroup(ctx, created.Data.ID)
	require.NoError(t, err, "the coordinator made it")
	assert.Equal(t, "Ops", made.DisplayName)
	_, err = a.authManager.GetGroupByName(ctx, "before", "")
	assert.Error(t, err, "a change refused for want of a coordinator is not made later")

	// The coordinator's refusals reach the user as it gives them.
	rec = consoleRequest(t, b.Server, routingAdminID, http.MethodPost, "/api/v1/groups", `{"name":"ops"}`)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	// A body over the console's limit is refused as the coordinator refuses
	// it; one no limit bounds, over what is forwarded.
	rec = consoleRequest(t, b.Server, routingAdminID, http.MethodPost, "/api/v1/groups",
		`{"name":"`+strings.Repeat("x", consoleJSONBodyLimitBytes)+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = consoleRequest(t, b.Server, routingAdminID, http.MethodDelete, "/api/v1/groups/"+created.Data.ID,
		strings.Repeat("x", maxForwardedBody+1))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	_, err = a.authManager.GetGroup(ctx, created.Data.ID)
	assert.NoError(t, err, "a refused deletion deletes nothing")

	// The coordinator, asked directly, takes only a forwarded write with its path.
	forwarded := func(s *Server, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(nil))
		req = req.WithContext(context.WithValue(req.Context(), "cluster_node_id", "peer"))
		rec := httptest.NewRecorder()
		s.handleCoordinatorWrite(rec, req)
		return rec
	}
	assert.Equal(t, http.StatusBadRequest, forwarded(a.Server, "/api/internal/cluster/console-write").Code)
	rec = forwarded(b.Server, "/api/internal/cluster/console-write/api/v1/groups")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "b does not coordinate")
	assert.Equal(t, "5", rec.Header().Get("Retry-After"))
}

// A vote is asked by a node of the cluster, of a candidate in a term.
func TestALeaseIsAskedByANodeOfTheCluster(t *testing.T) {
	s := newClusterTestNode(t)
	require.NoError(t, s.leaderMgr.EnsureSchema())
	ask := func(node string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/internal/cluster/leader-lease", strings.NewReader(body))
		if node != "" {
			req = req.WithContext(context.WithValue(req.Context(), "cluster_node_id", node))
		}
		rec := httptest.NewRecorder()
		s.handleLeaderLease(rec, req)
		return rec
	}

	assert.Equal(t, http.StatusUnauthorized, ask("", `{"candidate_id":"peer","term":1}`).Code)
	for _, body := range []string{`not json`, `{"term":1}`, `{"candidate_id":"peer"}`, `{"candidate_id":"peer","term":-1}`} {
		assert.Equal(t, http.StatusBadRequest, ask("peer", body).Code, body)
	}
	rec := ask("peer", `{"candidate_id":"peer","term":1}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var answer cluster.LeaseResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	assert.True(t, answer.Granted)
	assert.Equal(t, "peer", s.leaderMgr.LiveLeaseHolder(context.Background()))
	require.NoError(t, json.Unmarshal(ask("other", `{"candidate_id":"other","term":1}`).Body.Bytes(), &answer))
	assert.False(t, answer.Granted, "one vote per term")
}
