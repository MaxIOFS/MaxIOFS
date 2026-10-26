package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/maxiofs/maxiofs/internal/bgwork"
	"github.com/sirupsen/logrus"
)

// ConsoleSessionData is a console session as it travels between nodes; an
// ended one travels too, so every node refuses it. The provider's refresh
// token is the encrypted form stored in the database: the nodes share the
// secret it is encrypted with.
type ConsoleSessionData struct {
	ID              string `json:"id"`
	UserID          string `json:"user_id"`
	Method          string `json:"method"`
	ProviderID      string `json:"provider_id,omitempty"`
	IdPSubject      string `json:"idp_subject,omitempty"`
	IdPSession      string `json:"idp_session,omitempty"`
	IdPRefreshToken string `json:"idp_refresh_token,omitempty"`
	CreatedAt       int64  `json:"created_at"`
	RefreshedAt     int64  `json:"refreshed_at"`
	ExpiresAt       int64  `json:"expires_at"`
	RevokedAt       int64  `json:"revoked_at,omitempty"`
	RevokedReason   string `json:"revoked_reason,omitempty"`
	UpdatedAt       int64  `json:"updated_at"`
}

// ConsoleSessionSyncPayload is the batch body sent to each peer.
type ConsoleSessionSyncPayload struct {
	Sessions []*ConsoleSessionData `json:"sessions"`
}

// ConsoleSessionSyncManager replicates console sessions across the nodes: a
// session is renewed through whichever node a load balancer picks, and one
// ended on a node is refused by all of them.
type ConsoleSessionSyncManager struct {
	bgwork.Worker
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	log            *logrus.Entry
}

// NewConsoleSessionSyncManager creates a new console session sync manager.
func NewConsoleSessionSyncManager(db *sql.DB, clusterManager *Manager) *ConsoleSessionSyncManager {
	return &ConsoleSessionSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewDynamicProxyClient(clusterManager.GetTLSConfig),
		log:            logrus.WithField("component", "console-session-sync"),
	}
}

// Start begins the periodic synchronization loop, at the access-key sync
// interval: a sign-in is a credential like an access key.
func (m *ConsoleSessionSyncManager) Start(ctx context.Context) {
	interval := 30
	if intervalStr, err := GetGlobalConfig(ctx, m.db, "access_key_sync_interval_seconds"); err == nil {
		if v, convErr := strconv.Atoi(intervalStr); convErr == nil && v > 0 {
			interval = v
		}
	}
	m.log.WithField("interval_seconds", interval).Info("Starting console session synchronization manager")
	m.Spawn(func() { m.syncLoop(ctx, time.Duration(interval)*time.Second) })
}

// TriggerSync pushes the sessions to all peers now: a session made, renewed or
// ended has to be known by the next node a request reaches.
func (m *ConsoleSessionSyncManager) TriggerSync(ctx context.Context) {
	runDetached(&m.Worker, ctx, m.syncAll)
}

func (m *ConsoleSessionSyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	m.syncAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.Stopped():
			return
		case <-ticker.C:
			m.syncAll(ctx)
		}
	}
}

func (m *ConsoleSessionSyncManager) syncAll(ctx context.Context) {
	if !m.clusterManager.IsClusterEnabled() {
		return
	}
	localNodeID, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get local node ID")
		return
	}
	nodes, err := m.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get healthy nodes")
		return
	}
	sessions, err := m.listLocalSessions(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list local console sessions")
		return
	}
	if len(sessions) == 0 {
		return
	}
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get node token for console session sync")
		return
	}
	payload := &ConsoleSessionSyncPayload{Sessions: sessions}
	for _, node := range nodes {
		if node.ID == localNodeID {
			continue
		}
		if err := m.sendToNode(ctx, payload, node, localNodeID, nodeToken); err != nil {
			m.log.WithFields(logrus.Fields{"node_id": node.ID, "error": err}).Warn("Failed to sync console sessions to node")
		}
	}
}

// listLocalSessions returns every session that has not expired, ended ones
// included.
func (m *ConsoleSessionSyncManager) listLocalSessions(ctx context.Context) ([]*ConsoleSessionData, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, user_id, method, provider_id, idp_subject, idp_session, idp_refresh_token,
		       created_at, refreshed_at, expires_at, revoked_at, revoked_reason, updated_at
		FROM console_sessions
		WHERE expires_at > ?
	`, time.Now().Unix())
	if err != nil {
		return nil, fmt.Errorf("failed to query console sessions: %w", err)
	}
	defer rows.Close()
	var sessions []*ConsoleSessionData
	for rows.Next() {
		var s ConsoleSessionData
		if err := rows.Scan(&s.ID, &s.UserID, &s.Method, &s.ProviderID, &s.IdPSubject, &s.IdPSession, &s.IdPRefreshToken,
			&s.CreatedAt, &s.RefreshedAt, &s.ExpiresAt, &s.RevokedAt, &s.RevokedReason, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, &s)
	}
	return sessions, rows.Err()
}

func (m *ConsoleSessionSyncManager) sendToNode(ctx context.Context, payload *ConsoleSessionSyncPayload, node *Node, sourceNodeID, nodeToken string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal console session payload: %w", err)
	}
	url := fmt.Sprintf("%s/api/internal/cluster/console-session-sync", node.Endpoint)
	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(body), sourceNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// SyncToNode pushes the sessions to a node that just joined.
func (m *ConsoleSessionSyncManager) SyncToNode(ctx context.Context, node *Node) {
	if !m.clusterManager.IsClusterEnabled() {
		return
	}
	localNodeID, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		m.log.WithError(err).Error("SyncToNode(console sessions): failed to get local node ID")
		return
	}
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		m.log.WithError(err).Error("SyncToNode(console sessions): failed to get node token")
		return
	}
	sessions, err := m.listLocalSessions(ctx)
	if err != nil || len(sessions) == 0 {
		return
	}
	if err := m.sendToNode(ctx, &ConsoleSessionSyncPayload{Sessions: sessions}, node, localNodeID, nodeToken); err != nil {
		m.log.WithFields(logrus.Fields{"node_id": node.ID, "error": err}).Warn("SyncToNode(console sessions): failed to push sessions")
	}
}
