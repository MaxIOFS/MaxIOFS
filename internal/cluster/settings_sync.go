package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/bgwork"
	"github.com/sirupsen/logrus"
)

// SettingRow is a row of system_settings.
type SettingRow struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Type        string `json:"type"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Editable    bool   `json:"editable"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// LoggingTargetRow is a row of logging_targets.
type LoggingTargetRow struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	Enabled       bool    `json:"enabled"`
	Protocol      string  `json:"protocol"`
	Host          string  `json:"host"`
	Port          int     `json:"port"`
	Tag           string  `json:"tag"`
	Format        string  `json:"format"`
	TLSEnabled    bool    `json:"tls_enabled"`
	TLSCert       *string `json:"tls_cert"`
	TLSKey        *string `json:"tls_key"`
	TLSCA         *string `json:"tls_ca"`
	TLSSkipVerify bool    `json:"tls_skip_verify"`
	FilterLevel   string  `json:"filter_level"`
	AuthToken     *string `json:"auth_token"`
	URL           *string `json:"url"`
	BatchSize     *int64  `json:"batch_size"`
	FlushInterval *int64  `json:"flush_interval"`
	CreatedAt     int64   `json:"created_at"`
	UpdatedAt     int64   `json:"updated_at"`
}

// SettingsSyncPayload is every setting and logging target of a node.
type SettingsSyncPayload struct {
	Settings       []SettingRow       `json:"settings"`
	LoggingTargets []LoggingTargetRow `json:"logging_targets"`
}

const settingsSyncPath = "/api/internal/cluster/settings-sync"

// SettingsSyncManager makes every node hold the same settings and logging
// targets: it sends all of them to the other nodes when they change, and
// every 30 seconds.
type SettingsSyncManager struct {
	bgwork.Worker
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	log            *logrus.Entry
}

// NewSettingsSyncManager creates the settings synchronization.
func NewSettingsSyncManager(db *sql.DB, clusterManager *Manager) *SettingsSyncManager {
	return &SettingsSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewDynamicProxyClient(clusterManager.GetTLSConfig),
		log:            logrus.WithField("component", "settings-sync"),
	}
}

// Start sends the settings now and every 30 seconds until ctx ends.
func (m *SettingsSyncManager) Start(ctx context.Context) {
	m.Spawn(func() {
		ticker := time.NewTicker(30 * time.Second)
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
	})
}

// TriggerSync sends the settings to every other node now.
func (m *SettingsSyncManager) TriggerSync(ctx context.Context) {
	runDetached(&m.Worker, ctx, m.syncAll)
}

// SyncToNode sends the settings to node, a node that just joined.
func (m *SettingsSyncManager) SyncToNode(ctx context.Context, node *Node) {
	if !m.clusterManager.IsClusterEnabled() {
		return
	}
	if err := m.sendTo(ctx, []*Node{node}); err != nil {
		m.log.WithError(err).WithField("node_id", node.ID).Warn("Failed to send the settings to a joining node")
	}
}

func (m *SettingsSyncManager) syncAll(ctx context.Context) {
	if !m.clusterManager.IsClusterEnabled() {
		return
	}
	nodes, err := m.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get healthy nodes")
		return
	}
	if err := m.sendTo(ctx, nodes); err != nil {
		m.log.WithError(err).Error("Failed to send the settings")
	}
}

// sendTo sends every setting and logging target to nodes, this one aside.
func (m *SettingsSyncManager) sendTo(ctx context.Context, nodes []*Node) error {
	localID, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return err
	}
	token, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return err
	}
	payload, err := m.buildPayload(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.ID == localID {
			continue
		}
		if err := m.post(ctx, node, body, localID, token); err != nil {
			m.log.WithError(err).WithField("node_id", node.ID).Warn("Failed to send the settings to a node")
		}
	}
	return nil
}

func (m *SettingsSyncManager) buildPayload(ctx context.Context) (*SettingsSyncPayload, error) {
	payload := &SettingsSyncPayload{}
	rows, err := m.db.QueryContext(ctx, `
		SELECT key, value, type, category, description, editable, created_at, updated_at FROM system_settings`)
	if err != nil {
		return nil, fmt.Errorf("read the settings: %w", err)
	}
	for rows.Next() {
		var st SettingRow
		var description sql.NullString
		if err := rows.Scan(&st.Key, &st.Value, &st.Type, &st.Category, &description, &st.Editable, &st.CreatedAt, &st.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		st.Description = description.String
		payload.Settings = append(payload.Settings, st)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = m.db.QueryContext(ctx, `
		SELECT id, name, type, enabled, protocol, host, port, tag, format,
		       tls_enabled, tls_cert, tls_key, tls_ca, tls_skip_verify,
		       filter_level, auth_token, url, batch_size, flush_interval, created_at, updated_at
		FROM logging_targets`)
	if err != nil {
		return nil, fmt.Errorf("read the logging targets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t LoggingTargetRow
		if err := rows.Scan(&t.ID, &t.Name, &t.Type, &t.Enabled, &t.Protocol, &t.Host, &t.Port, &t.Tag, &t.Format,
			&t.TLSEnabled, &t.TLSCert, &t.TLSKey, &t.TLSCA, &t.TLSSkipVerify,
			&t.FilterLevel, &t.AuthToken, &t.URL, &t.BatchSize, &t.FlushInterval, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		payload.LoggingTargets = append(payload.LoggingTargets, t)
	}
	return payload, rows.Err()
}

func (m *SettingsSyncManager) post(ctx context.Context, node *Node, body []byte, localID, token string) error {
	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, http.MethodPost, node.Endpoint+settingsSyncPath, bytes.NewReader(body), localID, token)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, answer)
	}
	return nil
}
