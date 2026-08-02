package cluster

// Cluster synchronization for IAM entities.
//
// Policies, roles, attachments and inline policies are few, small and change
// rarely, so each cycle pushes the complete local set to every peer. The
// receiver upserts rather than replaces: a wholesale replace would let a batch
// from one node erase an entity another node created seconds earlier, and the
// two would then undo each other on every tick without ever converging.
//
// Edits converge on updated_at — the newest write wins. Deletions travel as
// tombstones in the shared cluster_deletion_log, which is what stops a node
// that has not heard about a delete from resurrecting the entity on its next
// push.

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

// IAMPolicyData is a managed policy with all of its versions.
type IAMPolicyData struct {
	Name             string                 `json:"name"`
	ARN              string                 `json:"arn"`
	Path             string                 `json:"path"`
	Description      string                 `json:"description,omitempty"`
	DefaultVersionID string                 `json:"default_version_id"`
	IsBuiltin        bool                   `json:"is_builtin"`
	CreatedAt        int64                  `json:"created_at"`
	UpdatedAt        int64                  `json:"updated_at"`
	Versions         []*IAMPolicyVersionRow `json:"versions"`
}

type IAMPolicyVersionRow struct {
	VersionID string `json:"version_id"`
	Document  string `json:"document"`
	CreatedAt int64  `json:"created_at"`
}

type IAMRoleData struct {
	Name               string `json:"name"`
	ARN                string `json:"arn"`
	Path               string `json:"path"`
	Description        string `json:"description,omitempty"`
	AssumeRolePolicy   string `json:"assume_role_policy"`
	MaxSessionDuration int    `json:"max_session_duration"`
	TenantID           string `json:"tenant_id,omitempty"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

type IAMAttachmentData struct {
	PolicyName string `json:"policy_name"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	AttachedAt int64  `json:"attached_at"`
}

type IAMInlinePolicyData struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Name       string `json:"name"`
	Document   string `json:"document"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// IAMSyncPayload is the batch body sent to each peer. Deletions are keyed by
// entity type so one message carries every kind of tombstone.
type IAMSyncPayload struct {
	Policies    []*IAMPolicyData       `json:"policies"`
	Roles       []*IAMRoleData         `json:"roles"`
	Attachments []*IAMAttachmentData   `json:"attachments"`
	Inline      []*IAMInlinePolicyData `json:"inline_policies"`
	Deletions   map[string][]string    `json:"deletions,omitempty"`
}

// IAMSyncManager replicates IAM entities across cluster nodes.
type IAMSyncManager struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	stopChan       chan struct{}
	log            *logrus.Entry
}

// NewIAMSyncManager creates a new IAM sync manager.
func NewIAMSyncManager(db *sql.DB, clusterManager *Manager) *IAMSyncManager {
	return &IAMSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewDynamicProxyClient(clusterManager.GetTLSConfig),
		stopChan:       make(chan struct{}),
		log:            logrus.WithField("component", "iam-sync"),
	}
}

// Start begins the periodic synchronization loop. It shares the access-key
// interval and enable flag: IAM entities decide what a key may do, so having
// the two replicate on different schedules would only create windows where a
// credential exists on a node that does not yet know its permissions.
func (m *IAMSyncManager) Start(ctx context.Context) {
	m.proxyClient = NewDynamicProxyClient(m.clusterManager.GetTLSConfig)

	interval := 30
	if intervalStr, err := GetGlobalConfig(ctx, m.db, "access_key_sync_interval_seconds"); err == nil {
		if v, convErr := strconv.Atoi(intervalStr); convErr == nil && v > 0 {
			interval = v
		}
	}

	enabledStr, err := GetGlobalConfig(ctx, m.db, "auto_access_key_sync_enabled")
	if err != nil || enabledStr != "true" {
		m.log.Info("Automatic IAM synchronization is disabled")
		return
	}

	m.log.WithField("interval_seconds", interval).Info("Starting IAM synchronization manager")
	go m.syncLoop(ctx, time.Duration(interval)*time.Second)
}

// Stop halts the synchronization loop.
func (m *IAMSyncManager) Stop() {
	close(m.stopChan)
}

// TriggerSync pushes current state to all peers immediately, so an identity
// created through the IAM API can use its credentials on any node right away
// instead of waiting out the interval behind a load balancer.
func (m *IAMSyncManager) TriggerSync(ctx context.Context) {
	go m.syncAll(context.WithoutCancel(ctx))
}

func (m *IAMSyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	m.syncAll(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("IAM sync loop stopped")
			return
		case <-m.stopChan:
			m.log.Info("IAM sync loop stopped")
			return
		case <-ticker.C:
			m.syncAll(ctx)
		}
	}
}

func (m *IAMSyncManager) syncAll(ctx context.Context) {
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

	var targets []*Node
	for _, node := range nodes {
		if node.ID != localNodeID {
			targets = append(targets, node)
		}
	}
	if len(targets) == 0 {
		return
	}

	payload, err := m.buildPayload(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to build IAM sync payload")
		return
	}
	if payload == nil {
		return
	}

	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get node token for IAM sync")
		return
	}

	for _, node := range targets {
		if err := m.sendToNode(ctx, payload, node, localNodeID, nodeToken); err != nil {
			m.log.WithFields(logrus.Fields{"node_id": node.ID, "error": err}).
				Warn("Failed to sync IAM entities to node")
		}
	}
}

// buildPayload collects the local IAM state. Returns nil when there is nothing
// worth sending.
func (m *IAMSyncManager) buildPayload(ctx context.Context) (*IAMSyncPayload, error) {
	payload := &IAMSyncPayload{Deletions: map[string][]string{}}

	policies, err := m.listPolicies(ctx)
	if err != nil {
		return nil, err
	}
	payload.Policies = policies

	if payload.Roles, err = m.listRoles(ctx); err != nil {
		return nil, err
	}
	if payload.Attachments, err = m.listAttachments(ctx); err != nil {
		return nil, err
	}
	if payload.Inline, err = m.listInlinePolicies(ctx); err != nil {
		return nil, err
	}

	for _, entityType := range []string{
		EntityTypeIAMPolicy, EntityTypeIAMRole, EntityTypeIAMInlinePolicy, EntityTypeIAMAttachment,
	} {
		entries, err := ListDeletions(ctx, m.db, entityType)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			payload.Deletions[entityType] = append(payload.Deletions[entityType], entry.EntityID)
		}
	}

	if len(payload.Policies) == 0 && len(payload.Roles) == 0 &&
		len(payload.Attachments) == 0 && len(payload.Inline) == 0 && len(payload.Deletions) == 0 {
		return nil, nil
	}
	return payload, nil
}

func (m *IAMSyncManager) listPolicies(ctx context.Context) ([]*IAMPolicyData, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT name, arn, path, description, default_version_id, is_builtin, created_at, updated_at
		FROM iam_policies
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query iam policies: %w", err)
	}
	defer rows.Close()

	var policies []*IAMPolicyData
	for rows.Next() {
		var p IAMPolicyData
		var description sql.NullString
		var isBuiltin int
		if err := rows.Scan(&p.Name, &p.ARN, &p.Path, &description, &p.DefaultVersionID,
			&isBuiltin, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if description.Valid {
			p.Description = description.String
		}
		p.IsBuiltin = isBuiltin == 1
		policies = append(policies, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, p := range policies {
		versions, err := m.listPolicyVersions(ctx, p.Name)
		if err != nil {
			return nil, err
		}
		p.Versions = versions
	}
	return policies, nil
}

func (m *IAMSyncManager) listPolicyVersions(ctx context.Context, policyName string) ([]*IAMPolicyVersionRow, error) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT version_id, document, created_at FROM iam_policy_versions WHERE policy_name = ?`, policyName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []*IAMPolicyVersionRow
	for rows.Next() {
		var v IAMPolicyVersionRow
		if err := rows.Scan(&v.VersionID, &v.Document, &v.CreatedAt); err != nil {
			return nil, err
		}
		versions = append(versions, &v)
	}
	return versions, rows.Err()
}

func (m *IAMSyncManager) listRoles(ctx context.Context) ([]*IAMRoleData, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT name, arn, path, description, assume_role_policy, max_session_duration, tenant_id, created_at, updated_at
		FROM iam_roles
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query iam roles: %w", err)
	}
	defer rows.Close()

	var roles []*IAMRoleData
	for rows.Next() {
		var r IAMRoleData
		var description, tenantID sql.NullString
		if err := rows.Scan(&r.Name, &r.ARN, &r.Path, &description, &r.AssumeRolePolicy,
			&r.MaxSessionDuration, &tenantID, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if description.Valid {
			r.Description = description.String
		}
		if tenantID.Valid {
			r.TenantID = tenantID.String
		}
		roles = append(roles, &r)
	}
	return roles, rows.Err()
}

func (m *IAMSyncManager) listAttachments(ctx context.Context) ([]*IAMAttachmentData, error) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT policy_name, target_type, target_id, attached_at FROM iam_policy_attachments`)
	if err != nil {
		return nil, fmt.Errorf("failed to query iam attachments: %w", err)
	}
	defer rows.Close()

	var out []*IAMAttachmentData
	for rows.Next() {
		var a IAMAttachmentData
		if err := rows.Scan(&a.PolicyName, &a.TargetType, &a.TargetID, &a.AttachedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (m *IAMSyncManager) listInlinePolicies(ctx context.Context) ([]*IAMInlinePolicyData, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT target_type, target_id, name, document, created_at, updated_at FROM iam_inline_policies
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query iam inline policies: %w", err)
	}
	defer rows.Close()

	var out []*IAMInlinePolicyData
	for rows.Next() {
		var p IAMInlinePolicyData
		if err := rows.Scan(&p.TargetType, &p.TargetID, &p.Name, &p.Document, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (m *IAMSyncManager) sendToNode(ctx context.Context, payload *IAMSyncPayload, node *Node, sourceNodeID, nodeToken string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal iam payload: %w", err)
	}

	url := fmt.Sprintf("%s/api/internal/cluster/iam-sync", node.Endpoint)
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

// SyncToNode pushes current state to a single node, used to bring a freshly
// joined node up to date without waiting for the ticker.
func (m *IAMSyncManager) SyncToNode(ctx context.Context, node *Node) {
	if !m.clusterManager.IsClusterEnabled() {
		return
	}

	localNodeID, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		m.log.WithError(err).Error("SyncToNode(iam): failed to get local node ID")
		return
	}
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		m.log.WithError(err).Error("SyncToNode(iam): failed to get node token")
		return
	}
	payload, err := m.buildPayload(ctx)
	if err != nil || payload == nil {
		return
	}

	if err := m.sendToNode(ctx, payload, node, localNodeID, nodeToken); err != nil {
		m.log.WithFields(logrus.Fields{"node_id": node.ID, "error": err}).
			Warn("SyncToNode(iam): failed to push IAM entities")
	}
}
