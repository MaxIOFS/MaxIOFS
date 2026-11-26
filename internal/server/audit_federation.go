package server

import (
	"cmp"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// auditTotalHeader carries how many logs of a node match a federated query.
// A node of an earlier version sends none.
const auditTotalHeader = "X-Maxiofs-Audit-Total"

// peerAuditDefault is how many logs a node gives a peer that does not say how
// many: what a node of an earlier version was given.
const peerAuditDefault = 100

// auditLogFilters reads the filters and the page of an audit log query.
func auditLogFilters(q url.Values) *audit.AuditLogFilters {
	filters := &audit.AuditLogFilters{
		TenantID:     q.Get("tenant_id"),
		UserID:       q.Get("user_id"),
		EventType:    q.Get("event_type"),
		ResourceType: q.Get("resource_type"),
		Action:       q.Get("action"),
		Status:       q.Get("status"),
		StartDate:    queryInt64(q, "start_date"),
		EndDate:      queryInt64(q, "end_date"),
		Page:         1,
		PageSize:     50,
	}
	// Bounded so that the logs up to the end of a page are counted in an int.
	if page, err := strconv.Atoi(q.Get("page")); err == nil && page > 0 {
		filters.Page = min(page, math.MaxInt32)
	}
	if size, err := strconv.Atoi(q.Get("page_size")); err == nil && size > 0 {
		filters.PageSize = min(size, 100)
	}
	return filters
}

func queryInt64(q url.Values, key string) int64 {
	v, err := strconv.ParseInt(q.Get(key), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// auditFilterQuery asks a peer for its newest logs that match filters.
func auditFilterQuery(filters *audit.AuditLogFilters, newest int) url.Values {
	q := url.Values{"limit": {strconv.Itoa(newest)}}
	for key, value := range map[string]string{
		"tenant_id": filters.TenantID, "user_id": filters.UserID, "event_type": filters.EventType,
		"resource_type": filters.ResourceType, "action": filters.Action, "status": filters.Status,
	} {
		if value != "" {
			q.Set(key, value)
		}
	}
	for key, value := range map[string]int64{"start_date": filters.StartDate, "end_date": filters.EndDate} {
		if value != 0 {
			q.Set(key, strconv.FormatInt(value, 10))
		}
	}
	return q
}

// auditLogs is the page filters asks of this node's log, or of the cluster's.
func (s *Server) auditLogs(ctx context.Context, filters *audit.AuditLogFilters) ([]*audit.AuditLog, int, error) {
	if s.clusterManager != nil && s.clusterManager.IsClusterEnabled() {
		return s.federatedAuditLogs(ctx, filters)
	}
	return s.auditManager.GetLogs(ctx, filters)
}

// federatedAuditLogs is the page filters asks of the logs of every healthy
// node, newest first, and how many match in all. Each node gives its newest
// logs up to the end of the page; a node that does not answer adds none.
func (s *Server) federatedAuditLogs(ctx context.Context, filters *audit.AuditLogFilters) ([]*audit.AuditLog, int, error) {
	newest := filters.Page * filters.PageSize
	logs, total, err := s.auditManager.NewestLogs(ctx, *filters, newest)
	if err != nil {
		return nil, 0, err
	}
	for _, peer := range s.peerAuditLogs(ctx, auditFilterQuery(filters, newest)) {
		logs = append(logs, peer.logs...)
		total += peer.total
	}
	// Stable: logs of one second and one ID keep the order of their nodes.
	slices.SortStableFunc(logs, func(a, b *audit.AuditLog) int {
		return cmp.Or(cmp.Compare(b.Timestamp, a.Timestamp), cmp.Compare(b.ID, a.ID))
	})
	start := min((filters.Page-1)*filters.PageSize, len(logs))
	end := min(start+filters.PageSize, len(logs))
	return append([]*audit.AuditLog{}, logs[start:end]...), total, nil
}

// peerAudit is a peer's answer to a federated audit query.
type peerAudit struct {
	logs  []*audit.AuditLog
	total int
}

// peerAuditLogs asks every other healthy node for the logs query names, and
// gives the answers in the order of the nodes.
func (s *Server) peerAuditLogs(ctx context.Context, query url.Values) []peerAudit {
	clusterCfg, err := s.clusterManager.GetConfig(ctx)
	if err != nil {
		return nil
	}
	localNodeID, err := s.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return nil
	}
	nodes, err := s.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		return nil
	}
	client := cluster.NewDynamicProxyClient(s.clusterManager.GetTLSConfig)
	answers := make([]peerAudit, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		if node.ID != localNodeID {
			wg.Go(func() { answers[i] = askPeerAuditLogs(ctx, client, clusterCfg, node, query) })
		}
	}
	wg.Wait()
	return answers
}

// askPeerAuditLogs is node's answer to query, or nothing.
func askPeerAuditLogs(ctx context.Context, client *cluster.ProxyClient, cfg *cluster.ClusterConfig, node *cluster.Node, query url.Values) peerAudit {
	logger := logrus.WithField("node_id", node.ID)
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := client.CreateAuthenticatedRequest(reqCtx, http.MethodGet,
		node.Endpoint+"/api/internal/cluster/audit-logs?"+query.Encode(), nil, cfg.NodeID, cfg.ClusterToken)
	if err != nil {
		logger.WithError(err).Warn("audit federation: failed to create request")
		return peerAudit{}
	}
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		logger.WithError(err).Warn("audit federation: request failed")
		return peerAudit{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.WithField("status", resp.StatusCode).Warn("audit federation: unexpected status")
		return peerAudit{}
	}
	var logs []*audit.AuditLog
	if err := json.NewDecoder(resp.Body).Decode(&logs); err != nil {
		logger.WithError(err).Warn("audit federation: failed to decode response")
		return peerAudit{}
	}
	total, err := strconv.Atoi(resp.Header.Get(auditTotalHeader))
	if err != nil {
		// A node of an earlier version: what it sent is all that is known.
		total = len(logs)
	}
	return peerAudit{logs: logs, total: total}
}

// handleGetLocalAuditLogs answers a peer's federated audit query with this
// node's newest logs that match it, and how many match.
// GET /api/internal/cluster/audit-logs?limit=<n>
func (s *Server) handleGetLocalAuditLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.auditManager == nil {
		json.NewEncoder(w).Encode([]*audit.AuditLog{}) //nolint:errcheck
		return
	}
	q := r.URL.Query()
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit <= 0 {
		limit = peerAuditDefault
	}
	logs, total, err := s.auditManager.NewestLogs(r.Context(), *auditLogFilters(q), limit)
	if err != nil {
		http.Error(w, "failed to retrieve audit logs", http.StatusInternalServerError)
		return
	}
	w.Header().Set(auditTotalHeader, strconv.Itoa(total))
	json.NewEncoder(w).Encode(logs) //nolint:errcheck
}
