package cluster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// UserData represents user information to be synchronized
type UserData struct {
	ID                  string `json:"id"`
	Username            string `json:"username"`
	PasswordHash        string `json:"password_hash"`
	DisplayName         string `json:"display_name"`
	Email               string `json:"email"`
	Status              string `json:"status"`
	TenantID            string `json:"tenant_id"`
	Roles               string `json:"roles"`
	Policies            string `json:"policies"`
	Metadata            string `json:"metadata"`
	FailedLoginAttempts int    `json:"failed_login_attempts"`
	LockedUntil         int64  `json:"locked_until"`
	LastFailedLogin     int64  `json:"last_failed_login"`
	ThemePreference     string `json:"theme_preference"`
	LanguagePreference  string `json:"language_preference"`
	AuthProvider        string `json:"auth_provider"`
	ExternalID          string `json:"external_id"`
	CreatedAt           int64  `json:"created_at"`
	UpdatedAt           int64  `json:"updated_at"`
}

// UserSyncManager handles automatic user synchronization between cluster nodes
type UserSyncManager struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	stopChan       chan struct{}
	log            *logrus.Entry
}

// NewUserSyncManager creates a new user sync manager
func NewUserSyncManager(db *sql.DB, clusterManager *Manager) *UserSyncManager {
	return &UserSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewProxyClient(),
		stopChan:       make(chan struct{}),
		log:            logrus.WithField("component", "user-sync"),
	}
}

// Start begins the user synchronization loop
func (m *UserSyncManager) Start(ctx context.Context) {
	// Get sync interval from config
	intervalStr, err := GetGlobalConfig(ctx, m.db, "user_sync_interval_seconds")
	if err != nil {
		m.log.WithError(err).Warn("Failed to get user sync interval, using default 30s")
		intervalStr = "30"
	}

	interval, err := strconv.Atoi(intervalStr)
	if err != nil {
		m.log.WithError(err).Warn("Invalid user sync interval, using default 30s")
		interval = 30
	}

	// Check if auto user sync is enabled
	enabledStr, err := GetGlobalConfig(ctx, m.db, "auto_user_sync_enabled")
	if err != nil || enabledStr != "true" {
		m.log.Info("Automatic user synchronization is disabled")
		return
	}

	m.log.WithField("interval_seconds", interval).Info("Starting user synchronization manager")

	go m.syncLoop(ctx, time.Duration(interval)*time.Second)
}

// syncLoop runs the synchronization loop
func (m *UserSyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.syncAllUsers(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("User sync loop stopped")
			return
		case <-m.stopChan:
			m.log.Info("User sync loop stopped")
			return
		case <-ticker.C:
			m.syncAllUsers(ctx)
		}
	}
}

// syncAllUsers synchronizes all users to all healthy nodes
func (m *UserSyncManager) syncAllUsers(ctx context.Context) {
	// Check if cluster is enabled
	if !m.clusterManager.IsClusterEnabled() {
		return
	}

	// Get local node ID
	localNodeID, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get local node ID")
		return
	}

	// Get all healthy nodes (excluding self)
	nodes, err := m.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get healthy nodes")
		return
	}

	// Filter out local node
	var targetNodes []*Node
	for _, node := range nodes {
		if node.ID != localNodeID {
			targetNodes = append(targetNodes, node)
		}
	}

	if len(targetNodes) == 0 {
		m.log.Debug("No target nodes for user synchronization")
		return
	}

	// Get all users from local database
	users, err := m.listLocalUsers(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list local users")
		return
	}

	m.log.WithFields(logrus.Fields{
		"user_count": len(users),
		"node_count": len(targetNodes),
	}).Debug("Starting user synchronization")

	// Sync each user to each target node
	for _, user := range users {
		for _, node := range targetNodes {
			if err := m.syncUserToNode(ctx, user, node, localNodeID); err != nil {
				m.log.WithFields(logrus.Fields{
					"user_id":  user.ID,
					"username": user.Username,
					"node_id":  node.ID,
					"error":    err,
				}).Warn("Failed to sync user to node")
			}
		}
	}
}

// syncUserToNode synchronizes a single user to a target node
func (m *UserSyncManager) syncUserToNode(ctx context.Context, user *UserData, node *Node, sourceNodeID string) error {
	// Compute checksum for user data
	checksum := m.computeUserChecksum(user)

	// Check if user is already synced with same checksum
	needsSync, err := m.needsSynchronization(ctx, user.ID, node.ID, checksum)
	if err != nil {
		return fmt.Errorf("failed to check sync status: %w", err)
	}

	if !needsSync {
		m.log.WithFields(logrus.Fields{
			"user_id":  user.ID,
			"username": user.Username,
			"node_id":  node.ID,
		}).Debug("User already synchronized, skipping")
		return nil
	}

	// Get node token for authentication
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	// Send user data to remote node
	if err := m.sendUserToNode(ctx, user, node, sourceNodeID, nodeToken); err != nil {
		return fmt.Errorf("failed to send user data: %w", err)
	}

	// Update sync status
	if err := m.updateSyncStatus(ctx, user.ID, sourceNodeID, node.ID, checksum); err != nil {
		m.log.WithError(err).Warn("Failed to update sync status")
	}

	m.log.WithFields(logrus.Fields{
		"user_id":   user.ID,
		"username":  user.Username,
		"node_id":   node.ID,
		"node_name": node.Name,
	}).Info("User synchronized successfully")

	return nil
}

// sendUserToNode sends user data to a target node via HMAC-authenticated request
func (m *UserSyncManager) sendUserToNode(ctx context.Context, user *UserData, node *Node, sourceNodeID, nodeToken string) error {
	// Marshal user data
	userData, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user data: %w", err)
	}

	// Build URL
	url := fmt.Sprintf("%s/api/internal/cluster/user-sync", node.Endpoint)

	// Create authenticated request
	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(userData), sourceNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := m.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// listLocalUsers retrieves all users from the local database
func (m *UserSyncManager) listLocalUsers(ctx context.Context) ([]*UserData, error) {
	query := `
		SELECT id, username, password_hash, display_name, email, status,
		       COALESCE(tenant_id, ''), COALESCE(roles, ''), COALESCE(policies, ''),
		       COALESCE(metadata, ''), failed_login_attempts, locked_until,
		       last_failed_login, theme_preference, language_preference,
		       COALESCE(auth_provider, 'local'), COALESCE(external_id, ''),
		       created_at, updated_at
		FROM users
		WHERE status != 'deleted'
	`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	var users []*UserData
	for rows.Next() {
		user := &UserData{}
		err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.PasswordHash,
			&user.DisplayName,
			&user.Email,
			&user.Status,
			&user.TenantID,
			&user.Roles,
			&user.Policies,
			&user.Metadata,
			&user.FailedLoginAttempts,
			&user.LockedUntil,
			&user.LastFailedLogin,
			&user.ThemePreference,
			&user.LanguagePreference,
			&user.AuthProvider,
			&user.ExternalID,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, user)
	}

	return users, nil
}

// computeUserChecksum computes a checksum for user data to detect changes
func (m *UserSyncManager) computeUserChecksum(user *UserData) string {
	// Create a string representation of relevant user fields
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%d|%d|%s|%s|%s|%s|%d",
		user.Username,
		user.PasswordHash,
		user.DisplayName,
		user.Email,
		user.Status,
		user.TenantID,
		user.Roles,
		user.Policies,
		user.Metadata,
		user.FailedLoginAttempts,
		user.LockedUntil,
		user.ThemePreference,
		user.LanguagePreference,
		user.AuthProvider,
		user.ExternalID,
		user.UpdatedAt,
	)

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// needsSynchronization checks if a user needs to be synced to a node
func (m *UserSyncManager) needsSynchronization(ctx context.Context, userID, nodeID, checksum string) (bool, error) {
	query := `
		SELECT user_checksum FROM cluster_user_sync
		WHERE user_id = ? AND destination_node_id = ?
	`

	var existingChecksum string
	err := m.db.QueryRowContext(ctx, query, userID, nodeID).Scan(&existingChecksum)
	if err == sql.ErrNoRows {
		return true, nil // Never synced before
	}
	if err != nil {
		return false, err
	}

	return existingChecksum != checksum, nil
}

// updateSyncStatus updates the user sync status in the database
func (m *UserSyncManager) updateSyncStatus(ctx context.Context, userID, sourceNodeID, destNodeID, checksum string) error {
	now := time.Now().Unix()

	query := `
		INSERT INTO cluster_user_sync (id, user_id, source_node_id, destination_node_id, user_checksum, status, last_sync_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'synced', ?, ?, ?)
		ON CONFLICT(user_id, destination_node_id) DO UPDATE SET
			user_checksum = excluded.user_checksum,
			last_sync_at = excluded.last_sync_at,
			updated_at = excluded.updated_at
	`

	id := fmt.Sprintf("%s-%s", userID, destNodeID)
	_, err := m.db.ExecContext(ctx, query, id, userID, sourceNodeID, destNodeID, checksum, now, now, now)
	return err
}

// Stop stops the user sync manager
func (m *UserSyncManager) Stop() {
	close(m.stopChan)
}
