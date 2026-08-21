package cluster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Entity type constants for the deletion log
const (
	EntityTypeUser             = "user"
	EntityTypeTenant           = "tenant"
	EntityTypeAccessKey        = "access_key"
	EntityTypeBucketPermission = "bucket_permission"
	EntityTypeIDPProvider      = "idp_provider"
	EntityTypeGroupMapping     = "group_mapping"
	EntityTypeGroup            = "group"
	EntityTypeSTSSession       = "sts_session"

	EntityTypeIAMPolicy       = "iam_policy"
	EntityTypeIAMRole         = "iam_role"
	EntityTypeIAMInlinePolicy = "iam_inline_policy"
	EntityTypeIAMAttachment   = "iam_attachment"

	EntityTypeObject        = "object"
	EntityTypeObjectVersion = "object_version"
)

// DeletionEntry represents a tombstone in the cluster deletion log
type DeletionEntry struct {
	ID              string `json:"id"`
	EntityType      string `json:"entity_type"`
	EntityID        string `json:"entity_id"`
	DeletedByNodeID string `json:"deleted_by_node_id"`
	DeletedAt       int64  `json:"deleted_at"`
}

// ObjectTombstoneID returns a stable opaque ID for a bucket/key delete marker
// or non-versioned object delete.
func ObjectTombstoneID(bucket, key string) string {
	return encodeObjectTombstoneID(bucket, key)
}

// ObjectVersionTombstoneID returns a stable opaque ID for a permanent version delete.
func ObjectVersionTombstoneID(bucket, key, versionID string) string {
	return encodeObjectTombstoneID(bucket, key, versionID)
}

func DecodeObjectTombstoneID(id string) (bucket, key string, ok bool) {
	parts, ok := decodeObjectTombstoneID(id, 2)
	if !ok {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func DecodeObjectVersionTombstoneID(id string) (bucket, key, versionID string, ok bool) {
	parts, ok := decodeObjectTombstoneID(id, 3)
	if !ok {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func encodeObjectTombstoneID(parts ...string) string {
	data, _ := json.Marshal(parts)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeObjectTombstoneID(id string, expectedParts int) ([]string, bool) {
	data, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return nil, false
	}
	var parts []string
	if err := json.Unmarshal(data, &parts); err != nil || len(parts) != expectedParts {
		return nil, false
	}
	return parts, true
}

// sqlQuerier is satisfied by both *sql.DB and *sql.Tx, allowing the check and
// the insert to share a single implementation that can run inside a transaction.
type sqlQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// RecordDeletion inserts a tombstone into the deletion log.
// Uses INSERT OR REPLACE so re-recording the same entity is idempotent.
// Accepts *sql.DB or *sql.Tx so callers can run it inside a transaction.
func RecordDeletion(ctx context.Context, q sqlQuerier, entityType, entityID, nodeID string) error {
	id := uuid.New().String()
	now := time.Now().Unix()

	_, err := q.ExecContext(ctx, `
		INSERT INTO cluster_deletion_log (id, entity_type, entity_id, deleted_by_node_id, deleted_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(entity_type, entity_id) DO UPDATE SET
			deleted_by_node_id = excluded.deleted_by_node_id,
			deleted_at = excluded.deleted_at
	`, id, entityType, entityID, nodeID, now)

	if err != nil {
		return fmt.Errorf("failed to record deletion: %w", err)
	}
	return nil
}

// ListDeletions returns all tombstones for a given entity type
func ListDeletions(ctx context.Context, db *sql.DB, entityType string) ([]*DeletionEntry, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, entity_type, entity_id, deleted_by_node_id, deleted_at
		FROM cluster_deletion_log
		WHERE entity_type = ?
	`, entityType)
	if err != nil {
		return nil, fmt.Errorf("failed to list deletions: %w", err)
	}
	defer rows.Close()

	var entries []*DeletionEntry
	for rows.Next() {
		e := &DeletionEntry{}
		if err := rows.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.DeletedByNodeID, &e.DeletedAt); err != nil {
			return nil, fmt.Errorf("failed to scan deletion entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// HasDeletion checks if a tombstone exists for a given entity
func HasDeletion(ctx context.Context, db *sql.DB, entityType, entityID string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM cluster_deletion_log WHERE entity_type = ? AND entity_id = ?)
	`, entityType, entityID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check deletion: %w", err)
	}
	return exists, nil
}

// DeletionTime returns when the cluster recorded a delete for an entity, or 0
// if it holds no tombstone for it.
func DeletionTime(ctx context.Context, db *sql.DB, entityType, entityID string) int64 {
	var deletedAt int64
	err := db.QueryRowContext(ctx, `
		SELECT deleted_at FROM cluster_deletion_log WHERE entity_type = ? AND entity_id = ?
	`, entityType, entityID).Scan(&deletedAt)
	if err != nil {
		return 0
	}
	return deletedAt
}

// EntityIsNewerThanTombstone returns true when the locally stored entity was updated
func EntityIsNewerThanTombstone(ctx context.Context, q sqlQuerier, entityType, entityID string, deletedAt int64) bool {
	switch entityType {
	case EntityTypeTenant:
		var updatedAt time.Time
		if err := q.QueryRowContext(ctx, `SELECT updated_at FROM tenants WHERE id = ?`, entityID).Scan(&updatedAt); err != nil {
			return false // entity not found or error → tombstone wins
		}
		return updatedAt.Unix() > deletedAt

	case EntityTypeUser:
		var updatedAt int64
		if err := q.QueryRowContext(ctx, `SELECT updated_at FROM users WHERE id = ?`, entityID).Scan(&updatedAt); err != nil {
			return false
		}
		return updatedAt > deletedAt

	case EntityTypeIDPProvider:
		var updatedAt int64
		if err := q.QueryRowContext(ctx, `SELECT updated_at FROM identity_providers WHERE id = ?`, entityID).Scan(&updatedAt); err != nil {
			return false
		}
		return updatedAt > deletedAt

	case EntityTypeGroupMapping:
		var updatedAt int64
		if err := q.QueryRowContext(ctx, `SELECT updated_at FROM idp_group_mappings WHERE id = ?`, entityID).Scan(&updatedAt); err != nil {
			return false
		}
		return updatedAt > deletedAt

	case EntityTypeGroup:
		var updatedAt int64
		if err := q.QueryRowContext(ctx, `SELECT updated_at FROM groups WHERE id = ?`, entityID).Scan(&updatedAt); err != nil {
			return false
		}
		return updatedAt > deletedAt

	default:
		// AccessKey, BucketPermission and any future types without updated_at:
		// tombstone always wins.
		return false
	}
}

// CleanupOldDeletions removes tombstones older than the given duration
func CleanupOldDeletions(ctx context.Context, db *sql.DB, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge).Unix()
	result, err := db.ExecContext(ctx, `
		DELETE FROM cluster_deletion_log WHERE deleted_at < ?
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old deletions: %w", err)
	}
	return result.RowsAffected()
}

// RunDeletionLogCleanup periodically cleans up old tombstones until ctx is
// cancelled. It blocks so callers that own shutdown can run it under their own
// worker tracking and wait for it before closing the database.
func RunDeletionLogCleanup(ctx context.Context, db *sql.DB, interval, maxAge time.Duration) {
	log := logrus.WithField("component", "deletion-log-cleanup")
	log.WithFields(logrus.Fields{
		"interval": interval,
		"max_age":  maxAge,
	}).Info("Starting deletion log cleanup")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Deletion log cleanup stopped")
			return
		case <-ticker.C:
			count, err := CleanupOldDeletions(ctx, db, maxAge)
			if err != nil {
				log.WithError(err).Error("Failed to cleanup old deletions")
			} else if count > 0 {
				log.WithField("count", count).Info("Cleaned up old deletion log entries")
			}
		}
	}
}

// StartDeletionLogCleanup starts a goroutine that periodically cleans up old tombstones.
func StartDeletionLogCleanup(ctx context.Context, db *sql.DB, interval, maxAge time.Duration) {
	go RunDeletionLogCleanup(ctx, db, interval, maxAge)
}

// DeletionLogSyncManager synchronizes tombstone entries to other cluster nodes
type DeletionLogSyncManager struct {
	bgWorker
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	log            *logrus.Entry
}

// NewDeletionLogSyncManager creates a new deletion log sync manager
func NewDeletionLogSyncManager(db *sql.DB, clusterManager *Manager) *DeletionLogSyncManager {
	return &DeletionLogSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewDynamicProxyClient(clusterManager.GetTLSConfig),
		log:            logrus.WithField("component", "deletion-log-sync"),
	}
}

// Start begins the deletion log synchronization loop
func (m *DeletionLogSyncManager) Start(ctx context.Context) {
	m.log.Info("Starting deletion log synchronization manager")
	m.spawn(func() { m.syncLoop(ctx, 30*time.Second) })
}

// syncLoop runs the synchronization loop
func (m *DeletionLogSyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.syncAllDeletions(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("Deletion log sync loop stopped")
			return
		case <-m.stopped():
			m.log.Info("Deletion log sync loop stopped")
			return
		case <-ticker.C:
			m.syncAllDeletions(ctx)
		}
	}
}

// syncAllDeletions synchronizes all deletion log entries to all healthy nodes
func (m *DeletionLogSyncManager) syncAllDeletions(ctx context.Context) {
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

	// Get all deletion entries
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, entity_type, entity_id, deleted_by_node_id, deleted_at
		FROM cluster_deletion_log
	`)
	if err != nil {
		m.log.WithError(err).Error("Failed to list all deletion entries")
		return
	}
	defer rows.Close()

	var entries []*DeletionEntry
	for rows.Next() {
		e := &DeletionEntry{}
		if err := rows.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.DeletedByNodeID, &e.DeletedAt); err != nil {
			m.log.WithError(err).Error("Failed to scan deletion entry")
			continue
		}
		entries = append(entries, e)
	}

	if len(entries) == 0 {
		return
	}

	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get node token")
		return
	}

	// Compute a checksum of all entries to skip sync if nothing changed
	checksum := m.computeChecksum(entries)

	for _, node := range targetNodes {
		if err := m.syncToNode(ctx, entries, node, localNodeID, nodeToken, checksum); err != nil {
			m.log.WithFields(logrus.Fields{
				"node_id": node.ID,
				"error":   err,
			}).Warn("Failed to sync deletion log to node")
		}
	}
}

// computeChecksum computes a checksum of all deletion entries
func (m *DeletionLogSyncManager) computeChecksum(entries []*DeletionEntry) string {
	data := ""
	for _, e := range entries {
		data += fmt.Sprintf("%s|%s|%s|%d|", e.EntityType, e.EntityID, e.DeletedByNodeID, e.DeletedAt)
	}
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// syncToNode sends all deletion entries to a single node
func (m *DeletionLogSyncManager) syncToNode(ctx context.Context, entries []*DeletionEntry, node *Node, sourceNodeID, nodeToken, checksum string) error {
	payload, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal deletion entries: %w", err)
	}

	url := fmt.Sprintf("%s/api/internal/cluster/deletion-log-sync", node.Endpoint)

	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(payload), sourceNodeID, nodeToken)
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
