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

	"github.com/maxiofs/maxiofs/internal/auth"
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

	// A lease naming a node b does not know is no coordinator to reach.
	now := time.Now().Unix()
	_, err := b.db.Exec(`INSERT OR REPLACE INTO cluster_leader (id, leader_id, term, granted_at, expires_at) VALUES (1, 'gone', 1, ?, ?)`, now, now+60)
	require.NoError(t, err)
	rec = consoleRequest(t, b.Server, routingAdminID, http.MethodPost, "/api/v1/groups", `{"name":"before"}`)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "not reachable")
	_, err = b.db.Exec(`DELETE FROM cluster_leader`)
	require.NoError(t, err)

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
	var made *auth.Group
	made, err = a.authManager.GetGroup(ctx, created.Data.ID)
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
	rec = consoleRequest(t, b.Server, routingAdminID, http.MethodDelete, "/api/v1/groups/"+created.Data.ID,
		strings.Repeat("x", maxForwardedBody))
	assert.Equal(t, http.StatusOK, rec.Code, "a body of the limit is forwarded")
	_, err = a.authManager.GetGroup(ctx, created.Data.ID)
	assert.Error(t, err, "the coordinator deleted it")

	// The coordinator, asked directly, takes only a forwarded write with its path.
	forwarded := func(s *Server, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(nil))
		req = req.WithContext(context.WithValue(req.Context(), "cluster_node_id", "peer"))
		rec := httptest.NewRecorder()
		s.handleCoordinatorWrite(rec, req)
		return rec
	}
	assert.Equal(t, http.StatusBadRequest, forwarded(a.Server, "/api/internal/cluster/console-write").Code)

	// The coordinator replays the query the user sent with the path.
	require.NoError(t, a.authManager.CreateGroup(ctx, &auth.Group{ID: "g-replay", Name: "replay", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}))
	token, err := a.authManager.GenerateJWT(ctx, &auth.User{ID: routingAdminID, Username: routingAdminID, Roles: []string{auth.RoleAdmin}})
	require.NoError(t, err)
	replayed := func(query string) []string {
		req := httptest.NewRequest(http.MethodGet, "/api/internal/cluster/console-write/api/v1/groups?"+query, nil)
		req.Header.Set("X-MaxIOFS-Forwarded-Authorization", "Bearer "+token)
		req = req.WithContext(context.WithValue(req.Context(), "cluster_node_id", b.id))
		rec := httptest.NewRecorder()
		a.handleCoordinatorWrite(rec, req)
		return groupNames(consoleData[groupList](t, rec, http.StatusOK))
	}
	assert.NotEmpty(t, replayed(""))
	assert.Empty(t, replayed("tenantId=nope"))
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

// What the user sent beside the body, its type and its tenant, reaches the
// coordinator as the user sent it; nothing it did not send is made up.
func TestTheCoordinatorGetsTheHeadersTheUserSent(t *testing.T) {
	user := httptest.NewRequest(http.MethodPost, "/api/v1/groups", nil)
	user.Header.Set("Authorization", "Bearer user-token")
	user.Header.Set("Content-Type", "application/json")
	user.Header.Set("X-Tenant-ID", "acme")
	forwarded := httptest.NewRequest(http.MethodPost, "/api/internal/cluster/console-write/api/v1/groups", nil)
	carryUser(forwarded, user)
	replay := replayOf(forwarded, "/api/v1/groups", nil)
	assert.Equal(t, "Bearer user-token", replay.Header.Get("Authorization"))
	assert.Equal(t, "application/json", replay.Header.Get("Content-Type"))
	assert.Equal(t, "acme", replay.Header.Get("X-Tenant-ID"))

	bare := httptest.NewRequest(http.MethodPost, "/api/internal/cluster/console-write/api/v1/groups", nil)
	carryUser(bare, httptest.NewRequest(http.MethodPost, "/api/v1/groups", nil))
	replay = replayOf(bare, "/api/v1/groups", nil)
	for _, header := range []string{"Authorization", "Content-Type", "X-Tenant-ID"} {
		assert.Empty(t, replay.Header.Values(header), header)
	}
}
