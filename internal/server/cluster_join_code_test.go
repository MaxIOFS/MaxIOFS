package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openJoinCodeOn opens a join code on s, as its administrator does on its
// Cluster page, and gives it.
func openJoinCodeOn(t *testing.T, s *Server) string {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleOpenJoinCode(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/cluster/join-code", nil), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Data struct {
			JoinCode  string `json:"join_code"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data.JoinCode)
	assert.InDelta(t, time.Now().Add(joinCodeLife).Unix(), resp.Data.ExpiresAt, 5)
	return resp.Data.JoinCode
}

// addNodeWithCode adds b to a's cluster from a's console with code, as the
// Nodes page does, and gives a's answer.
func addNodeWithCode(t *testing.T, a *Server, endpoint, code string) *httptest.ResponseRecorder {
	t.Helper()
	if !a.clusterManager.IsClusterEnabled() {
		_, err := a.clusterManager.InitializeCluster(context.Background(), "a", "us-east-1", "https://127.0.0.1:18082")
		require.NoError(t, err)
	}
	body, err := json.Marshal(map[string]string{"endpoint": endpoint, "join_code": code})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	a.handleAddClusterNode(w, createAuthenticatedRequest(http.MethodPost, "/api/v1/cluster/nodes", bytes.NewReader(body), "", "admin", true))
	return w
}

// sealedJoin posts pkg to s's join endpoint sealed with code, as the node that
// adds s does, and gives s's answer.
func sealedJoin(t *testing.T, s *Server, code string, pkg any) *httptest.ResponseRecorder {
	t.Helper()
	plain, err := json.Marshal(pkg)
	require.NoError(t, err)
	salt := make([]byte, 16)
	_, err = rand.Read(salt)
	require.NoError(t, err)
	key, err := joinKey(normalizeJoinCode(code), salt)
	require.NoError(t, err)
	sealed, err := sealJoin(key, plain)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{
		"salt": base64.StdEncoding.EncodeToString(salt), "package": base64.StdEncoding.EncodeToString(sealed)})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	s.handleJoinCluster(w, httptest.NewRequest(http.MethodPost, "/api/v1/cluster/join", bytes.NewReader(body)))
	return w
}

// A node joins with the join code it opened, typed as its administrator may
// type it, and the code is good once.
func TestANodeJoinsWithTheCodeItOpened(t *testing.T) {
	a := newClusterTestNode(t)
	b := newClusterTestNodeWith(t, func(c *config.Config) { c.ClusterListen = "127.0.0.1:0" })
	consoleB := httptest.NewServer(b.consoleRouter)
	t.Cleanup(consoleB.Close)
	code := openJoinCodeOn(t, b)

	w := addNodeWithCode(t, a, consoleB.URL, " "+strings.ToLower(code)+" ")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, b.clusterManager.IsClusterEnabled())
	nodes, err := a.clusterManager.ListNodes(context.Background())
	require.NoError(t, err)
	assert.Len(t, nodes, 2)

	again := newClusterTestNode(t)
	w = addNodeWithCode(t, again, consoleB.URL, code)
	assert.Equal(t, http.StatusConflict, w.Code, "the code was used")
	assert.Contains(t, w.Body.String(), "no join code is open")
}

// A node with no join code open, or given another, joins no cluster; wrong
// codes close the open one.
func TestAJoinNeedsTheCodeTheNodeOpened(t *testing.T) {
	a := newClusterTestNode(t)
	b := newClusterTestNodeWith(t, func(c *config.Config) { c.ClusterListen = "127.0.0.1:0" })
	consoleB := httptest.NewServer(b.consoleRouter)
	t.Cleanup(consoleB.Close)
	other := newClusterTestNode(t)
	wrong := openJoinCodeOn(t, other)

	w := addNodeWithCode(t, a, consoleB.URL, wrong)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "no join code is open")

	code := openJoinCodeOn(t, b)
	for i := 0; i < joinCodeTries; i++ {
		w = addNodeWithCode(t, a, consoleB.URL, wrong)
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "does not match")
	}
	w = addNodeWithCode(t, a, consoleB.URL, code)
	assert.Equal(t, http.StatusConflict, w.Code, "wrong codes closed it")

	assert.False(t, b.clusterManager.IsClusterEnabled())
	nodes, err := a.clusterManager.ListNodes(context.Background())
	require.NoError(t, err)
	assert.Len(t, nodes, 1, "a alone")

	w = addNodeWithCode(t, a, consoleB.URL, "not-a-code")
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// A join code is open for joinCodeLife.
func TestAnExpiredJoinCodeIsRefused(t *testing.T) {
	b := newClusterTestNodeWith(t, func(c *config.Config) { c.ClusterListen = "127.0.0.1:0" })
	code := openJoinCodeOn(t, b)
	b.joinCode.mu.Lock()
	b.joinCode.expires = time.Now().Add(-time.Second)
	b.joinCode.mu.Unlock()

	w := sealedJoin(t, b, code, map[string]string{})
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "no join code is open")
	assert.False(t, b.clusterManager.IsClusterEnabled())
}

// The join package and the answer cross the network sealed: none of the
// cluster's secrets is readable on the way, over plain HTTP too.
func TestTheJoinCrossesTheNetworkSealed(t *testing.T) {
	a := newClusterTestNode(t)
	b := newClusterTestNodeWith(t, func(c *config.Config) { c.ClusterListen = "127.0.0.1:0" })
	var mu sync.Mutex
	var crossed bytes.Buffer
	// What crossed, as sent and with its base64 fields decoded.
	keep := func(raw []byte) {
		crossed.Write(raw)
		var fields map[string]any
		if json.Unmarshal(raw, &fields) == nil {
			for _, v := range fields {
				if text, ok := v.(string); ok {
					if decoded, err := base64.StdEncoding.DecodeString(text); err == nil {
						crossed.Write(decoded)
					}
				}
			}
		}
	}
	consoleB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := httptest.NewRecorder()
		b.consoleRouter.ServeHTTP(rec, r)
		mu.Lock()
		keep(body)
		keep(rec.Body.Bytes())
		mu.Unlock()
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	t.Cleanup(consoleB.Close)

	w := addNodeWithCode(t, a, consoleB.URL, openJoinCodeOn(t, b))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	config, err := a.clusterManager.GetConfig(context.Background())
	require.NoError(t, err)
	var jwtSecret string
	require.NoError(t, a.db.QueryRow(`SELECT value FROM system_settings WHERE key = ?`, "jwt_secret").Scan(&jwtSecret))

	mu.Lock()
	defer mu.Unlock()
	for name, secret := range map[string]string{
		"the CA's key":          "PRIVATE KEY",
		"the cluster token":     config.ClusterToken,
		"the JWT secret":        jwtSecret,
		"the encryption secret": a.encSecret.Current(),
	} {
		require.NotEmpty(t, secret, name)
		assert.NotContains(t, crossed.String(), secret, name)
	}
}

// An answer the node with the join code did not seal adds no node.
func TestAnAnswerNotSealedByTheNodeAddsNoNode(t *testing.T) {
	a := newClusterTestNode(t)
	impostor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sealed":"c2VhbGVkIGJ5IG5vYm9keQ==","success":true,"data":{"message":"Join accepted, cluster TLS ready"}}`))
	}))
	t.Cleanup(impostor.Close)

	w := addNodeWithCode(t, a, impostor.URL, openJoinCodeOn(t, newClusterTestNode(t)))
	assert.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	nodes, err := a.clusterManager.ListNodes(context.Background())
	require.NoError(t, err)
	assert.Len(t, nodes, 1, "a alone")
}

// Only a global administrator of a node in no cluster opens a join code.
func TestWhoOpensAJoinCode(t *testing.T) {
	s := newClusterTestNode(t)
	w := httptest.NewRecorder()
	s.handleOpenJoinCode(w, createAuthenticatedRequest(http.MethodPost, "/api/v1/cluster/join-code", nil, "tenant-1", "tenant-admin", true))
	assert.Equal(t, http.StatusForbidden, w.Code)

	_, err := s.clusterManager.InitializeCluster(context.Background(), "s", "us-east-1", "https://127.0.0.1:18082")
	require.NoError(t, err)
	w = httptest.NewRecorder()
	s.handleOpenJoinCode(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/cluster/join-code", nil), nil))
	assert.Equal(t, http.StatusConflict, w.Code)
}
