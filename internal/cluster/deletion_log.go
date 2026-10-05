package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/maxiofs/maxiofs/internal/bgwork"
	"io"
	"net/http"
	"strings"
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

	// EntityTypeClusterNode is a node removed from the cluster. A node joins
	// under a new ID every time, so its removal is final.
	EntityTypeClusterNode = "cluster_node"
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

// DeletedAfter is the time to record for the deletion of something last
// changed at lastChange (unix seconds, 0 when unknown): now, or the second
// after the change when that is later, so every node orders the deletion after
// the change.
func DeletedAfter(lastChange int64) int64 {
	now := time.Now().Unix()
	if lastChange >= now {
		return lastChange + 1
	}
	return now
}

// RecordDeletion records that an entity was deleted at deletedAt (unix
// seconds), the time of the deletion on the node that made it. Of two records
// of an entity the later is kept. A record that changes takes the next
// sequence number, the order in which the deletion log is sent to the other
// nodes. Accepts *sql.DB or *sql.Tx.
func RecordDeletion(ctx context.Context, q sqlQuerier, entityType, entityID, nodeID string, deletedAt int64) error {
	var seq int64
	if err := q.QueryRowContext(ctx,
		`UPDATE cluster_deletion_log_counter SET last_seq = last_seq + 1 WHERE id = 1 RETURNING last_seq`).Scan(&seq); err != nil {
		return fmt.Errorf("failed to number deletion: %w", err)
	}
	_, err := q.ExecContext(ctx, `
		INSERT INTO cluster_deletion_log (id, entity_type, entity_id, deleted_by_node_id, deleted_at, seq)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(entity_type, entity_id) DO UPDATE SET
			deleted_by_node_id = excluded.deleted_by_node_id,
			deleted_at = excluded.deleted_at,
			seq = excluded.seq
		WHERE excluded.deleted_at > cluster_deletion_log.deleted_at
	`, uuid.New().String(), entityType, entityID, nodeID, deletedAt, seq)
	if err != nil {
		return fmt.Errorf("failed to record deletion: %w", err)
	}
	return nil
}

// IAMInlinePolicyID names an inline policy in the deletion log.
func IAMInlinePolicyID(targetType, targetID, name string) string {
	return targetType + "/" + targetID + "/" + name
}

// IAMAttachmentID names a policy attachment in the deletion log.
func IAMAttachmentID(policyName, targetType, targetID string) string {
	return policyName + "/" + targetType + "/" + targetID
}

// SplitIAMID splits an inline policy or attachment id at its first two
// slashes.
func SplitIAMID(id string) (string, string, string, bool) {
	first := strings.IndexByte(id, '/')
	if first < 0 {
		return "", "", "", false
	}
	second := strings.IndexByte(id[first+1:], '/')
	if second < 0 {
		return "", "", "", false
	}
	second += first + 1
	return id[:first], id[first+1 : second], id[second+1:], true
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

// VersionDeleted reports whether this node deleted the version or delete
// marker of key. A copy of it from a node that missed the delete is not stored.
func VersionDeleted(ctx context.Context, db *sql.DB, bucket, key, versionID string) bool {
	return versionID != "" && DeletionTime(ctx, db, EntityTypeObjectVersion, ObjectVersionTombstoneID(bucket, key, versionID)) > 0
}

// EntityUpdatedAt returns when this node's copy of an entity last changed
// (unix seconds), and false when the node does not hold it or its type keeps
// no such time (access keys, bucket permissions, STS sessions, objects).
func EntityUpdatedAt(ctx context.Context, q sqlQuerier, entityType, entityID string) (int64, bool) {
	var query string
	args := []any{entityID}
	switch entityType {
	case EntityTypeTenant:
		query = `SELECT updated_at FROM tenants WHERE id = ?`
	case EntityTypeUser:
		query = `SELECT updated_at FROM users WHERE id = ?`
	case EntityTypeIDPProvider:
		query = `SELECT updated_at FROM identity_providers WHERE id = ?`
	case EntityTypeGroupMapping:
		query = `SELECT updated_at FROM idp_group_mappings WHERE id = ?`
	case EntityTypeGroup:
		query = `SELECT updated_at FROM groups WHERE id = ?`
	case EntityTypeIAMPolicy:
		query = `SELECT updated_at FROM iam_policies WHERE name = ?`
	case EntityTypeIAMRole:
		query = `SELECT updated_at FROM iam_roles WHERE name = ?`
	case EntityTypeIAMInlinePolicy, EntityTypeIAMAttachment:
		a, b, c, ok := SplitIAMID(entityID)
		if !ok {
			return 0, false
		}
		args = []any{a, b, c}
		if entityType == EntityTypeIAMInlinePolicy {
			query = `SELECT updated_at FROM iam_inline_policies WHERE target_type = ? AND target_id = ? AND name = ?`
		} else {
			query = `SELECT attached_at FROM iam_policy_attachments WHERE policy_name = ? AND target_type = ? AND target_id = ?`
		}
	default:
		return 0, false
	}
	var at int64
	if err := q.QueryRowContext(ctx, query, args...).Scan(&at); err != nil {
		return 0, false
	}
	return at, true
}

// EntityIsNewerThanTombstone reports whether this node's copy of an entity
// changed at or after a deletion of it. At the same second the entity is kept:
// the node that deletes dates the deletion after the last change it knows.
// Types that keep no change time lose to any deletion.
func EntityIsNewerThanTombstone(ctx context.Context, q sqlQuerier, entityType, entityID string, deletedAt int64) bool {
	at, ok := EntityUpdatedAt(ctx, q, entityType, entityID)
	return ok && at >= deletedAt
}

// DeletionSupersedes reports whether this node recorded a deletion of an
// entity after changedAt, the change time of a copy another node sent: such a
// copy is not stored.
func DeletionSupersedes(ctx context.Context, db *sql.DB, entityType, entityID string, changedAt int64) bool {
	return DeletionTime(ctx, db, entityType, entityID) > changedAt
}

// CleanupOldDeletions forgets deletions older than maxAge that every member
// of the cluster has: each was sent every other node, and no catch-up that
// replays them is pending. A member that is away, for however long, keeps them
// until it is back or removed from the cluster.
func CleanupOldDeletions(ctx context.Context, db *sql.DB, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge).Unix()
	if since, ok := oldestUnsentChange(ctx, db); ok && since < cutoff {
		cutoff = since
	}
	result, err := db.ExecContext(ctx, `
		DELETE FROM cluster_deletion_log
		WHERE deleted_at < ?
		  AND seq <= COALESCE((
			SELECT MIN(COALESCE(d.delivered_seq, 0))
			FROM cluster_nodes n
			LEFT JOIN cluster_deletion_log_delivery d ON d.node_id = n.id
			WHERE n.id NOT IN (SELECT node_id FROM cluster_config)
		  ), 9223372036854775807)
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
			ForgetOldDeletions(ctx, db, maxAge)
		}
	}
}

// ForgetOldDeletions forgets the deletions older than maxAge: of entities and
// objects, of buckets and of rows.
func ForgetOldDeletions(ctx context.Context, db *sql.DB, maxAge time.Duration) {
	log := logrus.WithField("component", "deletion-log-cleanup")
	if count, err := CleanupOldDeletions(ctx, db, maxAge); err != nil {
		log.WithError(err).Error("Failed to cleanup old deletions")
	} else if count > 0 {
		log.WithField("count", count).Info("Cleaned up old deletion log entries")
	}
	if count, err := cleanupBucketTombstones(ctx, db, maxAge); err != nil {
		log.WithError(err).Error("Failed to clean up old bucket deletions")
	} else if count > 0 {
		log.WithField("count", count).Info("Cleaned up old bucket deletions")
	}
	if count, err := cleanupRowVersions(ctx, db, maxAge); err != nil {
		log.WithError(err).Error("Failed to clean up old row versions")
	} else if count > 0 {
		log.WithField("count", count).Info("Cleaned up old row versions")
	}
}

// StartDeletionLogCleanup starts a goroutine that periodically cleans up old tombstones.
func StartDeletionLogCleanup(ctx context.Context, db *sql.DB, interval, maxAge time.Duration) {
	go RunDeletionLogCleanup(ctx, db, interval, maxAge)
}

// DeletionLogSyncManager synchronizes tombstone entries to other cluster nodes
type DeletionLogSyncManager struct {
	bgwork.Worker
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
	m.Spawn(func() { m.syncLoop(ctx, 30*time.Second) })
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
		case <-m.Stopped():
			m.log.Info("Deletion log sync loop stopped")
			return
		case <-ticker.C:
			m.syncAllDeletions(ctx)
		}
	}
}

// SyncNow sends every healthy node the deletions it has not taken yet, now.
func (m *DeletionLogSyncManager) SyncNow(ctx context.Context) {
	m.syncAllDeletions(ctx)
}

// syncAllDeletions sends every healthy node the deletions it has not taken yet
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

	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get node token")
		return
	}

	for _, node := range targetNodes {
		if err := m.deliverTo(ctx, node, localNodeID, nodeToken); err != nil {
			m.log.WithFields(logrus.Fields{
				"node_id": node.ID,
				"error":   err,
			}).Warn("Failed to sync deletion log to node")
		}
	}
}

// deletionLogBatch is how many deletions one request carries.
const deletionLogBatch = 500

// deliverTo sends node the deletions recorded or changed here since the last
// ones it took, in the order they were recorded.
func (m *DeletionLogSyncManager) deliverTo(ctx context.Context, node *Node, sourceNodeID, nodeToken string) error {
	var delivered int64
	err := m.db.QueryRowContext(ctx,
		`SELECT delivered_seq FROM cluster_deletion_log_delivery WHERE node_id = ?`, node.ID).Scan(&delivered)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	for {
		entries, last, err := deletionsAfter(ctx, m.db, delivered, deletionLogBatch)
		if err != nil || len(entries) == 0 {
			return err
		}
		if err := m.syncToNode(ctx, entries, node, sourceNodeID, nodeToken); err != nil {
			return err
		}
		if _, err := m.db.ExecContext(ctx, `
			INSERT INTO cluster_deletion_log_delivery (node_id, delivered_seq) VALUES (?, ?)
			ON CONFLICT(node_id) DO UPDATE SET delivered_seq = excluded.delivered_seq`, node.ID, last); err != nil {
			return err
		}
		delivered = last
	}
}

// deletionsAfter returns up to limit deletions whose sequence number is above
// seq, in order, and the last number returned.
func deletionsAfter(ctx context.Context, db *sql.DB, seq int64, limit int) ([]*DeletionEntry, int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, entity_type, entity_id, deleted_by_node_id, deleted_at, seq
		FROM cluster_deletion_log WHERE seq > ? ORDER BY seq LIMIT ?`, seq, limit)
	if err != nil {
		return nil, seq, err
	}
	defer rows.Close()
	var entries []*DeletionEntry
	last := seq
	for rows.Next() {
		e := &DeletionEntry{}
		if err := rows.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.DeletedByNodeID, &e.DeletedAt, &last); err != nil {
			return nil, seq, err
		}
		entries = append(entries, e)
	}
	return entries, last, rows.Err()
}

// syncToNode sends deletion entries to a single node
func (m *DeletionLogSyncManager) syncToNode(ctx context.Context, entries []*DeletionEntry, node *Node, sourceNodeID, nodeToken string) error {
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
