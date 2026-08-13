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

	"github.com/sirupsen/logrus"
)

// STSSessionData is a session as it travels between nodes. The secret is the
// encrypted form stored in the database; nodes share the JWT secret it is
// derived from, exactly as they already do for permanent access keys.
type STSSessionData struct {
	TempAccessKeyID string `json:"temp_access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
	UserID          string `json:"user_id"`
	SessionPolicy   string `json:"session_policy,omitempty"`

	RoleARN         string `json:"role_arn,omitempty"`
	RoleSessionName string `json:"role_session_name,omitempty"`
	PolicyMode      string `json:"policy_mode,omitempty"`

	CreatedAt int64 `json:"created_at"`
	ExpiresAt int64 `json:"expires_at"`
}

// STSSessionSyncPayload is the batch body sent to each peer.
type STSSessionSyncPayload struct {
	Sessions  []*STSSessionData `json:"sessions"`
	Deletions []string          `json:"deletions"`
}

// STSSessionSyncManager replicates temporary credentials across cluster nodes.
type STSSessionSyncManager struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	stopChan       chan struct{}
	log            *logrus.Entry
}

// NewSTSSessionSyncManager creates a new STS session sync manager.
func NewSTSSessionSyncManager(db *sql.DB, clusterManager *Manager) *STSSessionSyncManager {
	return &STSSessionSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewDynamicProxyClient(clusterManager.GetTLSConfig),
		stopChan:       make(chan struct{}),
		log:            logrus.WithField("component", "sts-session-sync"),
	}
}

// Start begins the periodic synchronization loop. It reuses the access-key
// sync interval and enable flag: temporary credentials are access keys with a
// shorter life, and splitting the knobs would only invite them to drift apart.
func (m *STSSessionSyncManager) Start(ctx context.Context) {

	interval := 30
	if intervalStr, err := GetGlobalConfig(ctx, m.db, "access_key_sync_interval_seconds"); err == nil {
		if v, convErr := strconv.Atoi(intervalStr); convErr == nil && v > 0 {
			interval = v
		}
	}

	enabledStr, err := GetGlobalConfig(ctx, m.db, "auto_access_key_sync_enabled")
	if err != nil || enabledStr != "true" {
		m.log.Info("Automatic STS session synchronization is disabled")
		return
	}

	m.log.WithField("interval_seconds", interval).Info("Starting STS session synchronization manager")
	go m.syncLoop(ctx, time.Duration(interval)*time.Second)
}

// Stop halts the synchronization loop.
func (m *STSSessionSyncManager) Stop() {
	close(m.stopChan)
}

// TriggerSync pushes the current state to all peers immediately. Called right
// after issuing or revoking a session so a client using the credential through
// a load balancer doesn't have to wait for the next tick.
func (m *STSSessionSyncManager) TriggerSync(ctx context.Context) {
	runDetached(ctx, m.syncAllSessions)
}

func (m *STSSessionSyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	m.syncAllSessions(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("STS session sync loop stopped")
			return
		case <-m.stopChan:
			m.log.Info("STS session sync loop stopped")
			return
		case <-ticker.C:
			m.syncAllSessions(ctx)
		}
	}
}

// syncAllSessions sends one batch to every healthy peer.
func (m *STSSessionSyncManager) syncAllSessions(ctx context.Context) {
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

	var targetNodes []*Node
	for _, node := range nodes {
		if node.ID != localNodeID {
			targetNodes = append(targetNodes, node)
		}
	}
	if len(targetNodes) == 0 {
		return
	}

	m.pruneStaleTombstones(ctx)

	sessions, err := m.listLocalSessions(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list local STS sessions")
		return
	}

	deletions, err := ListDeletions(ctx, m.db, EntityTypeSTSSession)
	if err != nil {
		m.log.WithError(err).Error("Failed to list STS session tombstones")
		return
	}
	deletedIDs := make([]string, 0, len(deletions))
	for _, d := range deletions {
		deletedIDs = append(deletedIDs, d.EntityID)
	}

	if len(sessions) == 0 && len(deletedIDs) == 0 {
		return
	}

	payload := &STSSessionSyncPayload{Sessions: sessions, Deletions: deletedIDs}

	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get node token for STS session sync")
		return
	}

	for _, node := range targetNodes {
		if err := m.sendToNode(ctx, payload, node, localNodeID, nodeToken); err != nil {
			m.log.WithFields(logrus.Fields{
				"node_id": node.ID,
				"error":   err,
			}).Warn("Failed to sync STS sessions to node")
		}
	}
}

// listLocalSessions returns every non-expired local session. Expired ones are
// never propagated — a peer would reject them anyway.
func (m *STSSessionSyncManager) listLocalSessions(ctx context.Context) ([]*STSSessionData, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT temp_access_key_id, secret_access_key, session_token, user_id, session_policy,
		       role_arn, role_session_name, policy_mode, created_at, expires_at
		FROM sts_sessions
		WHERE expires_at > ?
	`, time.Now().Unix())
	if err != nil {
		return nil, fmt.Errorf("failed to query sts sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*STSSessionData
	for rows.Next() {
		var s STSSessionData
		var policy, roleARN, roleSessionName sql.NullString
		if err := rows.Scan(&s.TempAccessKeyID, &s.SecretAccessKey, &s.SessionToken,
			&s.UserID, &policy, &roleARN, &roleSessionName, &s.PolicyMode,
			&s.CreatedAt, &s.ExpiresAt); err != nil {
			return nil, err
		}
		if policy.Valid {
			s.SessionPolicy = policy.String
		}
		if roleARN.Valid {
			s.RoleARN = roleARN.String
		}
		if roleSessionName.Valid {
			s.RoleSessionName = roleSessionName.String
		}
		sessions = append(sessions, &s)
	}
	return sessions, rows.Err()
}

// pruneStaleTombstones drops tombstones older than the longest possible session
// lifetime: past that point the session would have expired on every node on its
// own, so the tombstone can no longer change any outcome.
func (m *STSSessionSyncManager) pruneStaleTombstones(ctx context.Context) {
	cutoff := time.Now().Unix() - stsTombstoneRetentionSeconds
	if _, err := m.db.ExecContext(ctx, `
		DELETE FROM cluster_deletion_log
		WHERE entity_type = ? AND deleted_at < ?
	`, EntityTypeSTSSession, cutoff); err != nil {
		m.log.WithError(err).Warn("Failed to prune STS session tombstones")
	}
}

// stsTombstoneRetentionSeconds is the maximum session lifetime (12 h) plus a
// day of margin for clock skew and nodes that stay down for a while.
const stsTombstoneRetentionSeconds = 43200 + 86400

func (m *STSSessionSyncManager) sendToNode(ctx context.Context, payload *STSSessionSyncPayload, node *Node, sourceNodeID, nodeToken string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal sts session payload: %w", err)
	}

	url := fmt.Sprintf("%s/api/internal/cluster/sts-session-sync", node.Endpoint)
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

// SyncToNode pushes the current state to a single node, used to bootstrap a
// newly-joined node without waiting for the periodic ticker.
func (m *STSSessionSyncManager) SyncToNode(ctx context.Context, node *Node) {
	if !m.clusterManager.IsClusterEnabled() {
		return
	}

	localNodeID, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		m.log.WithError(err).Error("SyncToNode(sts): failed to get local node ID")
		return
	}
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		m.log.WithError(err).Error("SyncToNode(sts): failed to get node token")
		return
	}

	sessions, err := m.listLocalSessions(ctx)
	if err != nil {
		m.log.WithError(err).Error("SyncToNode(sts): failed to list local sessions")
		return
	}
	if len(sessions) == 0 {
		return
	}

	if err := m.sendToNode(ctx, &STSSessionSyncPayload{Sessions: sessions}, node, localNodeID, nodeToken); err != nil {
		m.log.WithFields(logrus.Fields{"node_id": node.ID, "error": err}).
			Warn("SyncToNode(sts): failed to push sessions")
	}
}
