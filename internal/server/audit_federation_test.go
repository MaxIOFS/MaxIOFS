package server

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAuditedCluster is n nodes of one cluster, each keeping an audit log.
func newAuditedCluster(t *testing.T, n int) *haCluster {
	t.Helper()
	return newHAClusterWith(t, n, 1, func(cfg *config.Config) { cfg.Audit.Enable = true })
}

// seedAuditLogs writes logs of tenantID to the audit log of s, each named by
// its label and stamped with its time.
func seedAuditLogs(t *testing.T, s *Server, tenantID string, stamps map[string]int64) {
	t.Helper()
	db, err := sql.Open("sqlite", s.config.Audit.DBPath+"?_pragma=busy_timeout(10000)")
	require.NoError(t, err)
	defer db.Close()
	for label, at := range stamps {
		_, err := db.Exec(`INSERT INTO audit_logs (timestamp, tenant_id, user_id, username, event_type, resource_name, action, status, created_at)
			VALUES (?, ?, 'seed', 'seed', 'bucket_created', ?, 'create', 'success', ?)`, at, tenantID, label, at)
		require.NoError(t, err)
	}
}

// auditPage is the page of the audit log user is shown, by the labels of its
// logs, and the total it is told.
func auditPage(t *testing.T, s *Server, user *auth.User, query string) ([]string, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?"+query, nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", user))
	rec := httptest.NewRecorder()
	s.handleListAuditLogs(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response struct {
		Data struct {
			Logs  []*audit.AuditLog `json:"logs"`
			Total int               `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	labels := []string{}
	for _, log := range response.Data.Logs {
		labels = append(labels, log.ResourceName)
	}
	return labels, response.Data.Total
}

func stampsFrom(prefix string, first int64, count int) map[string]int64 {
	stamps := map[string]int64{}
	for i := range count {
		stamps[fmt.Sprintf("%s%03d", prefix, i)] = first + int64(i)
	}
	return stamps
}

// The audit log of a cluster is paged as one log, newest first, whichever node
// answers and however many logs each holds.
func TestTheClusterAuditLogIsPagedAsOne(t *testing.T) {
	c := newAuditedCluster(t, 2)
	a, b := c.nodes[0], c.nodes[1]
	admin, err := a.authManager.GetUser(context.Background(), "admin")
	require.NoError(t, err)
	seedAuditLogs(t, a.Server, "", map[string]int64{"a10": 10, "a8": 8, "a6": 6, "a4": 4, "a2": 2})
	seedAuditLogs(t, b.Server, "", map[string]int64{"b9": 9, "b7": 7, "b5": 5, "b3": 3, "b1": 1})

	for i, want := range [][]string{{"a10", "b9", "a8"}, {"b7", "a6", "b5"}, {"a4", "b3", "a2"}, {"b1"}, {}} {
		labels, total := auditPage(t, a.Server, admin, fmt.Sprintf("start_date=1&end_date=10&page=%d&page_size=3", i+1))
		assert.Equal(t, want, labels, "page %d", i+1)
		assert.Equal(t, 10, total, "page %d", i+1)
	}

	// More logs on a node than one page holds are all counted and all shown.
	seedAuditLogs(t, b.Server, "", stampsFrom("many", 1000, 130))
	labels, total := auditPage(t, a.Server, admin, "start_date=1000&page=1&page_size=100")
	assert.Len(t, labels, 100)
	assert.Equal(t, 130, total)
	labels, _ = auditPage(t, a.Server, admin, "start_date=1000&page=2&page_size=100")
	require.Len(t, labels, 30)
	assert.Equal(t, "many029", labels[0])

	// A node that does not answer leaves its logs out, not the page.
	b.down.Store(true)
	labels, total = auditPage(t, a.Server, admin, "start_date=1&end_date=10&page=1&page_size=3")
	assert.Equal(t, []string{"a10", "a8", "a6"}, labels)
	assert.Equal(t, 5, total)
}

// Logs of one second are shown once each across the pages, in the same order
// on every request.
func TestLogsOfOneSecondArePagedOnce(t *testing.T) {
	c := newAuditedCluster(t, 2)
	a, b := c.nodes[0], c.nodes[1]
	admin, err := a.authManager.GetUser(context.Background(), "admin")
	require.NoError(t, err)
	same := func(prefix string) map[string]int64 {
		stamps := map[string]int64{}
		for i := range 4 {
			stamps[fmt.Sprintf("%s%d", prefix, i)] = 50
		}
		return stamps
	}
	seedAuditLogs(t, a.Server, "", same("a"))
	seedAuditLogs(t, b.Server, "", same("b"))

	var shown []string
	for page := 1; page <= 3; page++ {
		first, total := auditPage(t, a.Server, admin, fmt.Sprintf("start_date=50&end_date=50&page=%d&page_size=3", page))
		again, _ := auditPage(t, a.Server, admin, fmt.Sprintf("start_date=50&end_date=50&page=%d&page_size=3", page))
		assert.Equal(t, first, again, "page %d", page)
		assert.Equal(t, 8, total)
		shown = append(shown, first...)
	}
	assert.ElementsMatch(t, []string{"a0", "a1", "a2", "a3", "b0", "b1", "b2", "b3"}, shown)
}

// The administrator of a tenant is shown its tenant's logs from every node,
// and only those.
func TestATenantAdministratorSeesItsTenantOnEveryNode(t *testing.T) {
	c := newAuditedCluster(t, 2)
	a, b := c.nodes[0], c.nodes[1]
	ctx := context.Background()
	require.NoError(t, a.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", Status: "active"}))
	now := time.Now().Unix()
	require.NoError(t, a.authManager.CreateUser(ctx, &auth.User{ID: "acme-admin", Username: "acme-admin", TenantID: "acme",
		Status: auth.UserStatusActive, Roles: []string{auth.RoleTenantAdmin}, CreatedAt: now, UpdatedAt: now}))
	tenantAdmin, err := a.authManager.GetUser(ctx, "acme-admin")
	require.NoError(t, err)
	seedAuditLogs(t, a.Server, "acme", map[string]int64{"a-acme": 20})
	seedAuditLogs(t, a.Server, "other", map[string]int64{"a-other": 21})
	seedAuditLogs(t, b.Server, "acme", map[string]int64{"b-acme": 22})
	seedAuditLogs(t, b.Server, "other", map[string]int64{"b-other": 23})
	seedAuditLogs(t, b.Server, "", map[string]int64{"b-global": 24})

	for _, query := range []string{"start_date=20&end_date=30", "start_date=20&end_date=30&tenant_id=other"} {
		labels, total := auditPage(t, a.Server, tenantAdmin, query)
		assert.Equal(t, []string{"b-acme", "a-acme"}, labels, query)
		assert.Equal(t, 2, total, query)
	}
}

// A node of an earlier version asks for no number of logs and is given what it
// was given before; answering, it sends no total and is counted by what it
// sends.
func TestANodeOfAnEarlierVersionIsStillFederated(t *testing.T) {
	c := newAuditedCluster(t, 1)
	node := c.nodes[0]
	seedAuditLogs(t, node.Server, "", stampsFrom("log", 1000, 130))
	rec := httptest.NewRecorder()
	node.handleGetLocalAuditLogs(rec, httptest.NewRequest(http.MethodGet, "/api/internal/cluster/audit-logs?start_date=1000", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var logs []*audit.AuditLog
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &logs))
	assert.Len(t, logs, peerAuditDefault)
	assert.Equal(t, strconv.Itoa(130), rec.Header().Get(auditTotalHeader))
	for _, limit := range []string{"0", "-1", "many"} {
		rec := httptest.NewRecorder()
		node.handleGetLocalAuditLogs(rec, httptest.NewRequest(http.MethodGet, "/api/internal/cluster/audit-logs?start_date=1000&limit="+limit, nil))
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &logs))
		assert.Len(t, logs, peerAuditDefault, "a limit of %s", limit)
	}
	rec = httptest.NewRecorder()
	node.handleGetLocalAuditLogs(rec, httptest.NewRequest(http.MethodGet, "/api/internal/cluster/audit-logs?start_date=1000&limit=1", nil))
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &logs))
	assert.Len(t, logs, 1, "a limit of 1")

	earlier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/internal/cluster/audit-logs", r.URL.Path)
		assert.Equal(t, "7", r.URL.Query().Get("limit"))
		_ = json.NewEncoder(w).Encode([]*audit.AuditLog{{ID: 1}, {ID: 2}})
	}))
	defer earlier.Close()
	client := cluster.NewDynamicProxyClient(func() *tls.Config { return nil })
	answer := askPeerAuditLogs(context.Background(), client, &cluster.ClusterConfig{NodeID: "a", ClusterToken: "token"},
		&cluster.Node{ID: "earlier", Endpoint: earlier.URL}, url.Values{"limit": {"7"}})
	assert.Len(t, answer.logs, 2)
	assert.Equal(t, 2, answer.total)
}

// A page asked as nothing, or as less than one, is the first; a page size
// likewise is the default, and at most 100.
func TestAnAuditPageOutOfRangeIsTheFirst(t *testing.T) {
	c := newAuditedCluster(t, 2)
	a, b := c.nodes[0], c.nodes[1]
	admin, err := a.authManager.GetUser(context.Background(), "admin")
	require.NoError(t, err)
	seedAuditLogs(t, a.Server, "", stampsFrom("a", 1000, 70))
	seedAuditLogs(t, b.Server, "", stampsFrom("b", 2000, 70))

	first, _ := auditPage(t, a.Server, admin, "start_date=1000&page=1&page_size=3")
	for _, page := range []string{"0", "-1", "x"} {
		labels, _ := auditPage(t, a.Server, admin, "start_date=1000&page_size=3&page="+page)
		assert.Equal(t, first, labels, "page %s", page)
	}
	for size, want := range map[string]int{"0": 50, "-1": 50, "x": 50, "1": 1, "1000": 100} {
		labels, total := auditPage(t, a.Server, admin, "start_date=1000&page_size="+size)
		assert.Len(t, labels, want, "page size %s", size)
		assert.Equal(t, 140, total)
	}
}

// The audit log is read by an administrator, of a server that keeps one.
func TestTheAuditLogIsReadByAnAdministrator(t *testing.T) {
	unaudited := newClusterTestNode(t)
	admin, err := unaudited.authManager.GetUser(context.Background(), "admin")
	require.NoError(t, err)
	list := func(s *Server, ctx context.Context) int {
		rec := httptest.NewRecorder()
		s.handleListAuditLogs(rec, httptest.NewRequest(http.MethodGet, "/audit-logs", nil).WithContext(ctx))
		return rec.Code
	}
	assert.Equal(t, http.StatusServiceUnavailable, list(unaudited, context.WithValue(context.Background(), "user", admin)))

	audited := newAuditedCluster(t, 1).nodes[0].Server
	plain := consoleUser(t, audited, "audit-plain", true)
	assert.Equal(t, http.StatusUnauthorized, list(audited, context.Background()))
	assert.Equal(t, http.StatusForbidden, list(audited, context.WithValue(context.Background(), "user", plain)))
}
