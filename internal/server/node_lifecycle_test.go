package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/config"
	idpkg "github.com/maxiofs/maxiofs/internal/idp"
	"github.com/maxiofs/maxiofs/internal/replication"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asGlobalAdmin gives a console request the identity of a global administrator.
func asGlobalAdmin(r *http.Request, vars map[string]string) *http.Request {
	r = r.WithContext(context.WithValue(r.Context(), "user", &auth.User{ID: "admin", Username: "admin", Roles: []string{auth.RoleAdmin}}))
	return mux.SetURLVars(r, vars)
}

func holdsNode(s *Server, id string) bool {
	_, err := s.clusterManager.GetNode(context.Background(), id)
	return err == nil
}

// addNodeEverywhere gives both nodes of the pair a third node.
func addNodeEverywhere(t *testing.T, p *haPair, id string) {
	t.Helper()
	for _, s := range []*Server{p.a, p.b} {
		require.NoError(t, s.clusterManager.AddNode(context.Background(), &cluster.Node{
			ID: id, Name: id, Endpoint: "https://127.0.0.1:9", NodeToken: "tok", Region: "us-east-1", Priority: 100, Metadata: "{}", ChangedAt: 1,
		}))
		_, err := s.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusHealthy, id)
		require.NoError(t, err)
	}
}

// A node removed on one node is removed on every node and stays removed: a
// node that still holds an earlier copy of it does not bring it back.
func TestARemovedNodeIsRemovedEverywhere(t *testing.T) {
	p := newHAPair(t)
	addNodeEverywhere(t, p, "gone")
	// b stamped its copy after a's: every node stamps the copy it adds.
	_, err := p.b.db.Exec(`UPDATE cluster_nodes SET changed_at = ? WHERE id = 'gone'`, time.Now().Unix()+60)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	p.a.handleRemoveClusterNode(w, asGlobalAdmin(httptest.NewRequest(http.MethodDelete, "/api/v1/cluster/nodes/gone", nil), map[string]string{"nodeId": "gone"}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, holdsNode(p.a, "gone"))
	assert.Eventually(t, func() bool { return !holdsNode(p.b, "gone") }, 10*time.Second, 20*time.Millisecond,
		"the other node removes it")

	w = httptest.NewRecorder()
	p.a.handleReceiveNodeListSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/node-list-sync", cluster.NodeList{
		Nodes: []*cluster.JoinPackageNode{{ID: "gone", Name: "gone", Endpoint: "https://127.0.0.1:9", NodeToken: "tok", Priority: 100, ChangedAt: 1}},
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, holdsNode(p.a, "gone"), "an earlier copy does not bring it back")
	p.b.globalConfigSyncMgr.SyncNow(context.Background())
	assert.False(t, holdsNode(p.a, "gone"))
}

// A node removed while it runs leaves the cluster the next time it reaches
// a node that removed it.
func TestARemovedNodeLeavesTheCluster(t *testing.T) {
	p := newHAPair(t)
	w := httptest.NewRecorder()
	p.a.handleRemoveClusterNode(w, asGlobalAdmin(httptest.NewRequest(http.MethodDelete, "/api/v1/cluster/nodes/"+p.bID, nil), map[string]string{"nodeId": p.bID}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, p.b.clusterManager.IsClusterEnabled())

	p.b.globalConfigSyncMgr.SyncNow(context.Background())
	assert.False(t, p.b.clusterManager.IsClusterEnabled(), "told it was removed, it leaves")
	assert.False(t, holdsNode(p.a, p.bID), "and is not added back")
}

// A node that leaves the cluster is removed from the others.
func TestALeavingNodeIsRemovedFromTheOthers(t *testing.T) {
	p := newHAPair(t)
	// a stamped its copy of b after b's own: every node stamps the copy it adds.
	_, err := p.a.db.Exec(`UPDATE cluster_nodes SET changed_at = ? WHERE id = ?`, time.Now().Unix()+60, p.bID)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	p.b.handleLeaveCluster(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/cluster/leave", nil), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, p.b.clusterManager.IsClusterEnabled())
	assert.False(t, holdsNode(p.a, p.bID))
	assert.True(t, p.a.clusterManager.RemovedFromCluster(context.Background(), p.bID))
}

// A drain on one node takes the node out of service on every node, and a
// drained node that answers stays out of service.
func TestADrainedNodeIsOutOfServiceEverywhere(t *testing.T) {
	p := newHAPair(t)
	addNodeEverywhere(t, p, "retiring")
	answers := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	}))
	t.Cleanup(answers.Close)
	_, err := p.b.db.Exec(`UPDATE cluster_nodes SET endpoint = ? WHERE id = 'retiring'`, answers.URL)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	p.a.handleDrainClusterNode(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes/retiring/drain", nil), map[string]string{"nodeId": "retiring"}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Eventually(t, func() bool {
		n, err := p.b.clusterManager.GetNode(context.Background(), "retiring")
		return err == nil && n.Drained && n.HealthStatus == cluster.HealthStatusDead
	}, 10*time.Second, 20*time.Millisecond, "the other node takes it out of service")

	st, err := p.b.clusterManager.CheckNodeHealth(context.Background(), "retiring")
	require.NoError(t, err)
	assert.Equal(t, cluster.HealthStatusDead, st.Status, "it answers and stays out")
	w = httptest.NewRecorder()
	p.b.handleGetClusterHA(w, asGlobalAdmin(httptest.NewRequest(http.MethodGet, "/api/v1/cluster/ha", nil), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"drained":true`, "the console shows it drained")

	// Draining the other node of a factor-2 pair would leave one copy.
	w = httptest.NewRecorder()
	p.a.handleDrainClusterNode(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes/"+p.bID+"/drain", nil), map[string]string{"nodeId": p.bID}))
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	n, err := p.a.clusterManager.GetNode(context.Background(), p.bID)
	require.NoError(t, err)
	assert.False(t, n.Drained)
}

// A cluster created or joined while the server runs replicates writes
// without a restart.
func TestAWriteReachesTheOtherNodeWithoutARestart(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "mirror", "admin"))
	_, err := p.a.objectManager.PutObject(ctx, "mirror", "k", strings.NewReader("payload"), http.Header{})
	require.NoError(t, err)
	assert.Equal(t, "payload", readBody(t, p.b, "mirror", "k"))

	_, err = p.a.objectManager.DeleteObject(ctx, "mirror", "k", false)
	require.NoError(t, err)
	_, err = p.b.metadataStore.GetObject(ctx, "mirror", "k")
	assert.Error(t, err)
}

// Outside a cluster the object manager stores and nothing else: no deletion
// is recorded for other nodes, and existing objects are still encrypted.
func TestOutsideAClusterObjectsAreOnlyStored(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "alone", "admin"))
	_, err := s.objectManager.PutObject(ctx, "alone", "k", strings.NewReader("payload"), http.Header{})
	require.NoError(t, err)

	logger := logrus.StandardLogger()
	warnings := new(logtest.Hook)
	formerHooks := logger.ReplaceHooks(logrus.LevelHooks{})
	formerLevel := logger.GetLevel()
	logger.AddHook(warnings)
	logger.SetLevel(logrus.WarnLevel)
	_, err = s.objectManager.DeleteObject(ctx, "alone", "k", false)
	logger.SetLevel(formerLevel)
	logger.ReplaceHooks(formerHooks)
	require.NoError(t, err)
	assert.Empty(t, warnings.AllEntries(), "a delete outside a cluster warns of nothing")
	assert.False(t, hasRow(t, s, `SELECT COUNT(*) FROM cluster_deletion_log`))

	_, ok := s.objectManager.(objectEncryptor)
	assert.True(t, ok, "the encryption of existing objects runs")
}

// addNodeThrough adds b to a's cluster from a's console, as the Nodes page
// does, and returns a's answer.
func addNodeThrough(t *testing.T, a, b *Server) *httptest.ResponseRecorder {
	t.Helper()
	consoleB := httptest.NewServer(b.consoleRouter)
	t.Cleanup(consoleB.Close)
	_, err := a.clusterManager.InitializeCluster(context.Background(), "a", "us-east-1", "https://127.0.0.1:18082")
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{"endpoint": consoleB.URL, "username": "admin", "password": "admin"})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	a.handleAddClusterNode(w, createAuthenticatedRequest(http.MethodPost, "/api/v1/cluster/nodes", strings.NewReader(string(body)), "", "admin", true))
	return w
}

// A node that holds data is not added to a cluster: it is told why and
// stays out of it, with what it holds.
func TestANodeHoldingDataIsNotAddedToACluster(t *testing.T) {
	a := newClusterTestNode(t)
	b := newClusterTestNodeWith(t, func(c *config.Config) { c.ClusterListen = "127.0.0.1:0" })
	require.NoError(t, b.bucketManager.CreateBucket(context.Background(), "", "held", "admin"))

	w := addNodeThrough(t, a, b)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "1 bucket")
	assert.False(t, b.clusterManager.IsClusterEnabled())
	assert.True(t, hasBucket(b, "", "held"))
	nodes, err := a.clusterManager.ListNodes(context.Background())
	require.NoError(t, err)
	assert.Len(t, nodes, 1, "a alone")
}

// Every kind of data a node can hold keeps it out of a cluster; a node with
// only its first administrator, settings and keys joins.
func TestOnlyANodeWithoutDataJoinsACluster(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	trust := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"*"},"Action":"sts:AssumeRole"}]}`
	document := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`
	for _, c := range []struct {
		held string
		hold func(t *testing.T, s *Server)
	}{
		{"1 bucket", func(t *testing.T, s *Server) {
			require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "held", "admin"))
		}},
		{"1 tenant", func(t *testing.T, s *Server) {
			require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "t", Name: "t", Status: "active"}))
		}},
		{"1 user besides the administrator", func(t *testing.T, s *Server) {
			require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "u", Username: "u",
				Status: auth.UserStatusActive, Roles: []string{auth.RoleUser}, CreatedAt: now, UpdatedAt: now}))
		}},
		{"1 access key", func(t *testing.T, s *Server) {
			_, err := s.authManager.GenerateAccessKey(ctx, "admin")
			require.NoError(t, err)
		}},
		{"1 group", func(t *testing.T, s *Server) {
			require.NoError(t, s.authManager.CreateGroup(ctx, &auth.Group{ID: "g", Name: "g", CreatedAt: now, UpdatedAt: now}))
		}},
		{"1 identity provider", func(t *testing.T, s *Server) {
			require.NoError(t, s.idpManager.CreateProvider(ctx, &idpkg.IdentityProvider{ID: "idp-1", Name: "corp",
				Type: idpkg.TypeLDAP, Status: idpkg.StatusActive, CreatedBy: "admin", CreatedAt: now, UpdatedAt: now,
				Config: idpkg.ProviderConfig{LDAP: &idpkg.LDAPConfig{Host: "ldap.example.com", Port: 389, Security: "none", BaseDN: "dc=example"}}}))
		}},
		{"1 IAM policy", func(t *testing.T, s *Server) {
			_, err := s.authManager.(auth.IAMManager).CreateIAMPolicy(ctx, "reader", "/", "", document, "")
			require.NoError(t, err)
		}},
		{"1 IAM role", func(t *testing.T, s *Server) {
			_, err := s.authManager.(auth.IAMManager).CreateIAMRole(ctx, "assumed", "/", "", trust, 3600, "")
			require.NoError(t, err)
		}},
		{"1 share", func(t *testing.T, s *Server) {
			_, err := s.shareManager.CreateShare(ctx, "b", "k", "", "AKID", "share-secret-key", "u", nil)
			require.NoError(t, err)
		}},
		{"1 replication rule", func(t *testing.T, s *Server) {
			require.NoError(t, s.replicationManager.CreateRule(ctx, &replication.ReplicationRule{ID: "rule-1", TenantID: "t",
				SourceBucket: "b", DestinationEndpoint: "https://dest.example.com", DestinationBucket: "d",
				DestinationAccessKey: "AK", DestinationSecretKey: "rule-secret-key", Enabled: true,
				Mode: replication.ModeRealTime, ConflictResolution: replication.ConflictLWW}))
		}},
	} {
		s := newClusterTestNode(t)
		c.hold(t, s)
		w := httptest.NewRecorder()
		s.handleJoinCluster(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/cluster/join", strings.NewReader("{}")), nil))
		assert.Equal(t, http.StatusConflict, w.Code, c.held)
		assert.Contains(t, w.Body.String(), c.held)
		assert.False(t, s.clusterManager.IsClusterEnabled(), c.held)
	}

	b := newClusterTestNodeWith(t, func(c *config.Config) { c.ClusterListen = "127.0.0.1:0" })
	w := addNodeThrough(t, newClusterTestNode(t), b)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, b.clusterManager.IsClusterEnabled())
}
