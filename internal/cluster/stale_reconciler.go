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

	"github.com/sirupsen/logrus"
)

// ReconcileMode describes the nature of the stale period.
type ReconcileMode int

const (
	// ModeOffline means the node was unreachable but made no local writes
	// during the stale period.  Only tombstones need to be propagated
	// immediately; entity data will arrive through the normal periodic sync
	// from healthy peers once the stale flag is cleared.
	ModeOffline ReconcileMode = iota

	// ModePartition means the node was isolated but continued accepting
	// writes.  A full LWW push of locally-newer entities is performed before
	// clearing the stale flag.
	ModePartition
)

func (m ReconcileMode) String() string {
	if m == ModePartition {
		return "partition"
	}
	return "offline"
}

// StaleReconciler runs once when a stale node reconnects to the cluster.
// It pushes locally-newer entities to each reachable peer, syncs tombstones
// bidirectionally, and finally clears the is_stale flag so normal periodic
// sync resumes.
type StaleReconciler struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	log            *logrus.Entry
}

// NewStaleReconciler creates a StaleReconciler bound to the given db and manager.
func NewStaleReconciler(db *sql.DB, mgr *Manager) *StaleReconciler {
	return &StaleReconciler{
		db:             db,
		clusterManager: mgr,
		proxyClient:    NewProxyClient(mgr.GetTLSConfig()),
		log:            logrus.WithField("component", "stale-reconciler"),
	}
}

// Reconcile performs the full stale-node reconciliation sequence.
// It is safe to call even when the node is not stale — it detects that and
// returns immediately.
func (r *StaleReconciler) Reconcile(ctx context.Context) error {
	localNodeID, err := r.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return fmt.Errorf("get local node ID: %w", err)
	}

	stale, err := r.isStale(ctx, localNodeID)
	if err != nil {
		return fmt.Errorf("check stale flag: %w", err)
	}
	if !stale {
		r.log.Debug("Node is not stale; skipping reconciliation")
		return nil
	}

	mode, err := r.detectMode(ctx, localNodeID)
	if err != nil {
		return fmt.Errorf("detect reconcile mode: %w", err)
	}

	r.log.WithFields(logrus.Fields{
		"node_id": localNodeID,
		"mode":    mode,
	}).Info("Starting stale-node reconciliation")

	nodeToken, err := r.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("get local node token: %w", err)
	}

	nodes, err := r.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		return fmt.Errorf("get healthy nodes: %w", err)
	}

	var peers []*Node
	for _, n := range nodes {
		if n.ID != localNodeID {
			peers = append(peers, n)
		}
	}

	if len(peers) == 0 {
		r.log.Warn("No reachable peers during reconciliation; will retry on next health cycle")
		return nil
	}

	// Build local snapshot once — reused for every peer.
	local, err := BuildLocalSnapshot(ctx, localNodeID, r.db)
	if err != nil {
		return fmt.Errorf("build local snapshot: %w", err)
	}

	// Process each peer; individual failures are logged and don't abort others.
	for _, peer := range peers {
		if err := r.reconcileWithPeer(ctx, mode, local, peer, localNodeID, nodeToken); err != nil {
			r.log.WithFields(logrus.Fields{
				"peer_id":   peer.ID,
				"peer_name": peer.Name,
				"error":     err,
			}).Warn("Reconciliation with peer failed; continuing with next peer")
		}
	}

	// Clear the stale flag so normal sync managers include this node again.
	if err := r.clearStaleFlag(ctx, localNodeID); err != nil {
		return fmt.Errorf("clear stale flag: %w", err)
	}

	r.log.WithField("node_id", localNodeID).Info("Stale-node reconciliation completed successfully")
	return nil
}

// ── Internal helpers ────────────────────────────────────────────────────────

// isStale checks whether this node is currently marked stale in the DB.
func (r *StaleReconciler) isStale(ctx context.Context, nodeID string) (bool, error) {
	var stale bool
	err := r.db.QueryRowContext(ctx,
		"SELECT is_stale FROM cluster_nodes WHERE id = ?", nodeID).Scan(&stale)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return stale, err
}

// detectMode returns ModePartition when last_local_write_at is set (the node
// accepted writes during isolation), otherwise ModeOffline.
func (r *StaleReconciler) detectMode(ctx context.Context, nodeID string) (ReconcileMode, error) {
	var lastWriteAt sql.NullTime
	err := r.db.QueryRowContext(ctx,
		"SELECT last_local_write_at FROM cluster_nodes WHERE id = ?", nodeID).Scan(&lastWriteAt)
	if err != nil {
		return ModeOffline, err
	}
	if lastWriteAt.Valid {
		return ModePartition, nil
	}
	return ModeOffline, nil
}

// reconcileWithPeer performs the full reconciliation sequence against one peer.
func (r *StaleReconciler) reconcileWithPeer(ctx context.Context, mode ReconcileMode, local *StateSnapshot, peer *Node, localNodeID, nodeToken string) error {
	log := r.log.WithFields(logrus.Fields{
		"peer_id":   peer.ID,
		"peer_name": peer.Name,
		"mode":      mode,
	})

	remote, err := r.fetchRemoteSnapshot(ctx, peer, localNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("fetch remote snapshot: %w", err)
	}

	log.WithFields(logrus.Fields{
		"remote_tenants":    len(remote.Tenants),
		"remote_users":      len(remote.Users),
		"remote_tombstones": len(remote.Tombstones),
	}).Debug("Fetched remote snapshot")

	// ModePartition: push locally-newer entities to the peer.
	// ModeOffline: skip — peer already has authoritative data; normal sync
	// will deliver it once the stale flag is cleared.
	if mode == ModePartition {
		if err := r.pushNewerEntities(ctx, local, remote, peer, localNodeID, nodeToken); err != nil {
			return fmt.Errorf("push newer entities: %w", err)
		}
	}

	// Always sync tombstones in both directions.
	if err := r.pushTombstonesToPeer(ctx, local.Tombstones, remote.Tombstones, peer, localNodeID, nodeToken); err != nil {
		return fmt.Errorf("push tombstones to peer: %w", err)
	}
	if err := r.applyRemoteTombstones(ctx, remote.Tombstones, local.Tombstones, localNodeID); err != nil {
		return fmt.Errorf("apply remote tombstones: %w", err)
	}

	return nil
}

// fetchRemoteSnapshot calls GET /api/internal/cluster/state-snapshot on peer.
func (r *StaleReconciler) fetchRemoteSnapshot(ctx context.Context, peer *Node, localNodeID, nodeToken string) (*StateSnapshot, error) {
	url := fmt.Sprintf("%s/api/internal/cluster/state-snapshot", peer.Endpoint)
	req, err := r.proxyClient.CreateAuthenticatedRequest(ctx, "GET", url, nil, localNodeID, nodeToken)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := r.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var snap StateSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	return &snap, nil
}

// pushNewerEntities iterates every entity type and pushes those whose local
// timestamp is strictly greater than the peer's (or absent from the peer).
func (r *StaleReconciler) pushNewerEntities(ctx context.Context, local, remote *StateSnapshot, peer *Node, localNodeID, nodeToken string) error {
	idx := buildStampIndex(remote)

	for _, stamp := range newerStamps(local.Tenants, idx["tenants"]) {
		if err := r.pushTenant(ctx, stamp.ID, peer, localNodeID, nodeToken); err != nil {
			r.log.WithFields(logrus.Fields{"tenant_id": stamp.ID, "error": err}).Warn("Failed to push tenant to peer")
		}
	}

	for _, stamp := range newerStamps(local.Users, idx["users"]) {
		if err := r.pushUser(ctx, stamp.ID, peer, localNodeID, nodeToken); err != nil {
			r.log.WithFields(logrus.Fields{"user_id": stamp.ID, "error": err}).Warn("Failed to push user to peer")
		}
	}

	for _, stamp := range newerStamps(local.AccessKeys, idx["access_keys"]) {
		if err := r.pushAccessKey(ctx, stamp.ID, peer, localNodeID, nodeToken); err != nil {
			r.log.WithFields(logrus.Fields{"access_key_id": stamp.ID, "error": err}).Warn("Failed to push access key to peer")
		}
	}

	for _, stamp := range newerStamps(local.BucketPermissions, idx["bucket_permissions"]) {
		if err := r.pushBucketPermission(ctx, stamp.ID, peer, localNodeID, nodeToken); err != nil {
			r.log.WithFields(logrus.Fields{"permission_id": stamp.ID, "error": err}).Warn("Failed to push bucket permission to peer")
		}
	}

	for _, stamp := range newerStamps(local.IDPProviders, idx["idp_providers"]) {
		if err := r.pushIDPProvider(ctx, stamp.ID, peer, localNodeID, nodeToken); err != nil {
			r.log.WithFields(logrus.Fields{"idp_id": stamp.ID, "error": err}).Warn("Failed to push IDP provider to peer")
		}
	}

	for _, stamp := range newerStamps(local.GroupMappings, idx["group_mappings"]) {
		if err := r.pushGroupMapping(ctx, stamp.ID, peer, localNodeID, nodeToken); err != nil {
			r.log.WithFields(logrus.Fields{"mapping_id": stamp.ID, "error": err}).Warn("Failed to push group mapping to peer")
		}
	}

	return nil
}

// buildStampIndex returns map[entityType][id] → updatedAt for quick lookup.
func buildStampIndex(snap *StateSnapshot) map[string]map[string]int64 {
	idx := map[string]map[string]int64{
		"tenants":            {},
		"users":              {},
		"access_keys":        {},
		"bucket_permissions": {},
		"idp_providers":      {},
		"group_mappings":     {},
	}
	for _, s := range snap.Tenants           { idx["tenants"][s.ID]            = s.UpdatedAt }
	for _, s := range snap.Users             { idx["users"][s.ID]              = s.UpdatedAt }
	for _, s := range snap.AccessKeys        { idx["access_keys"][s.ID]        = s.UpdatedAt }
	for _, s := range snap.BucketPermissions { idx["bucket_permissions"][s.ID] = s.UpdatedAt }
	for _, s := range snap.IDPProviders      { idx["idp_providers"][s.ID]      = s.UpdatedAt }
	for _, s := range snap.GroupMappings     { idx["group_mappings"][s.ID]     = s.UpdatedAt }
	return idx
}

// newerStamps returns the local stamps that are strictly newer than (or absent
// from) the corresponding entry in the remote index.
func newerStamps(local []EntityStamp, remoteByID map[string]int64) []EntityStamp {
	var out []EntityStamp
	for _, s := range local {
		remoteTS, exists := remoteByID[s.ID]
		if !exists || s.UpdatedAt > remoteTS {
			out = append(out, s)
		}
	}
	return out
}

// ── Tombstone sync ──────────────────────────────────────────────────────────

// pushTombstonesToPeer sends local tombstones that are absent or newer on the
// peer, using the existing bulk deletion-log-sync endpoint.
func (r *StaleReconciler) pushTombstonesToPeer(ctx context.Context, local, remote []*DeletionEntry, peer *Node, localNodeID, nodeToken string) error {
	remoteTS := make(map[string]int64, len(remote))
	for _, e := range remote {
		remoteTS[e.EntityType+":"+e.EntityID] = e.DeletedAt
	}

	var toSend []*DeletionEntry
	for _, e := range local {
		if rTS, exists := remoteTS[e.EntityType+":"+e.EntityID]; !exists || e.DeletedAt > rTS {
			toSend = append(toSend, e)
		}
	}
	if len(toSend) == 0 {
		return nil
	}

	if err := r.postToNode(ctx, peer, "/api/internal/cluster/deletion-log-sync", toSend, localNodeID, nodeToken); err != nil {
		return err
	}

	r.log.WithFields(logrus.Fields{"count": len(toSend), "peer_id": peer.ID}).
		Debug("Pushed tombstones to peer")
	return nil
}

// applyRemoteTombstones applies peer tombstones that this node is missing or
// has an older version of.  LWW is honoured via EntityIsNewerThanTombstone.
func (r *StaleReconciler) applyRemoteTombstones(ctx context.Context, remote, local []*DeletionEntry, localNodeID string) error {
	localTS := make(map[string]int64, len(local))
	for _, e := range local {
		localTS[e.EntityType+":"+e.EntityID] = e.DeletedAt
	}

	applied := 0
	for _, e := range remote {
		key := e.EntityType + ":" + e.EntityID
		if lTS, exists := localTS[key]; exists && e.DeletedAt <= lTS {
			continue // already have an equal or newer tombstone
		}
		if EntityIsNewerThanTombstone(ctx, r.db, e.EntityType, e.EntityID, e.DeletedAt) {
			r.log.WithFields(logrus.Fields{
				"entity_type": e.EntityType,
				"entity_id":   e.EntityID,
			}).Debug("Skipping remote tombstone: local entity is newer (LWW)")
			continue
		}
		if err := RecordDeletion(ctx, r.db, e.EntityType, e.EntityID, e.DeletedByNodeID); err != nil {
			r.log.WithError(err).WithFields(logrus.Fields{
				"entity_type": e.EntityType,
				"entity_id":   e.EntityID,
			}).Warn("Failed to record remote tombstone locally")
		} else {
			applied++
		}
	}

	if applied > 0 {
		r.log.WithField("count", applied).Debug("Applied remote tombstones locally")
	}
	return nil
}

// clearStaleFlag resets is_stale = 0 and last_local_write_at = NULL.
func (r *StaleReconciler) clearStaleFlag(ctx context.Context, nodeID string) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE cluster_nodes SET is_stale = 0, last_local_write_at = NULL, updated_at = ? WHERE id = ?",
		time.Now(), nodeID)
	return err
}

// ── Per-entity push methods ─────────────────────────────────────────────────

// postToNode is a generic helper that marshals payload and POSTs it to the
// given path on the peer, authenticated with HMAC.
func (r *StaleReconciler) postToNode(ctx context.Context, peer *Node, path string, payload interface{}, localNodeID, nodeToken string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	url := fmt.Sprintf("%s%s", peer.Endpoint, path)
	req, err := r.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(body), localNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (r *StaleReconciler) pushTenant(ctx context.Context, id string, peer *Node, localNodeID, nodeToken string) error {
	var t TenantData
	var metadataJSON string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, display_name, description, status, max_access_keys,
		       max_storage_bytes, current_storage_bytes, max_buckets, current_buckets,
		       metadata, created_at, updated_at
		FROM tenants WHERE id = ?
	`, id).Scan(
		&t.ID, &t.Name, &t.DisplayName, &t.Description, &t.Status, &t.MaxAccessKeys,
		&t.MaxStorageBytes, &t.CurrentStorageBytes, &t.MaxBuckets, &t.CurrentBuckets,
		&metadataJSON, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil // deleted after snapshot was taken
	}
	if err != nil {
		return fmt.Errorf("query tenant %s: %w", id, err)
	}
	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &t.Metadata); err != nil {
			t.Metadata = make(map[string]string)
		}
	} else {
		t.Metadata = make(map[string]string)
	}
	return r.postToNode(ctx, peer, "/api/internal/cluster/tenant-sync", &t, localNodeID, nodeToken)
}

func (r *StaleReconciler) pushUser(ctx context.Context, id string, peer *Node, localNodeID, nodeToken string) error {
	var u UserData
	err := r.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, display_name, email, status,
		       COALESCE(tenant_id, ''), COALESCE(roles, ''), COALESCE(policies, ''),
		       COALESCE(metadata, ''), failed_login_attempts, locked_until,
		       last_failed_login, theme_preference, language_preference,
		       COALESCE(auth_provider, 'local'), COALESCE(external_id, ''),
		       created_at, updated_at
		FROM users WHERE id = ?
	`, id).Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Email, &u.Status,
		&u.TenantID, &u.Roles, &u.Policies, &u.Metadata,
		&u.FailedLoginAttempts, &u.LockedUntil, &u.LastFailedLogin,
		&u.ThemePreference, &u.LanguagePreference, &u.AuthProvider, &u.ExternalID,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query user %s: %w", id, err)
	}
	return r.postToNode(ctx, peer, "/api/internal/cluster/user-sync", &u, localNodeID, nodeToken)
}

func (r *StaleReconciler) pushAccessKey(ctx context.Context, accessKeyID string, peer *Node, localNodeID, nodeToken string) error {
	var k AccessKeyData
	var lastUsed sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT access_key_id, secret_access_key, user_id, status, created_at, last_used
		FROM access_keys WHERE access_key_id = ?
	`, accessKeyID).Scan(
		&k.AccessKeyID, &k.SecretAccessKey, &k.UserID, &k.Status, &k.CreatedAt, &lastUsed,
	)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query access key %s: %w", accessKeyID, err)
	}
	if lastUsed.Valid {
		k.LastUsed = &lastUsed.Int64
	}
	return r.postToNode(ctx, peer, "/api/internal/cluster/access-key-sync", &k, localNodeID, nodeToken)
}

func (r *StaleReconciler) pushBucketPermission(ctx context.Context, id string, peer *Node, localNodeID, nodeToken string) error {
	var p BucketPermissionData
	var userID, tenantID sql.NullString
	var expiresAt sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT id, bucket_name, user_id, tenant_id, permission_level, granted_by, granted_at, expires_at
		FROM bucket_permissions WHERE id = ?
	`, id).Scan(
		&p.ID, &p.BucketName, &userID, &tenantID,
		&p.PermissionLevel, &p.GrantedBy, &p.GrantedAt, &expiresAt,
	)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query bucket permission %s: %w", id, err)
	}
	if userID.Valid   { p.UserID   = &userID.String   }
	if tenantID.Valid { p.TenantID = &tenantID.String }
	if expiresAt.Valid { p.ExpiresAt = &expiresAt.Int64 }
	return r.postToNode(ctx, peer, "/api/internal/cluster/bucket-permission-sync", &p, localNodeID, nodeToken)
}

func (r *StaleReconciler) pushIDPProvider(ctx context.Context, id string, peer *Node, localNodeID, nodeToken string) error {
	var p IDPProviderData
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, type, COALESCE(tenant_id, ''), status, config, created_by, created_at, updated_at
		FROM identity_providers WHERE id = ?
	`, id).Scan(
		&p.ID, &p.Name, &p.Type, &p.TenantID, &p.Status, &p.Config,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query IDP provider %s: %w", id, err)
	}
	return r.postToNode(ctx, peer, "/api/internal/cluster/idp-provider-sync", &p, localNodeID, nodeToken)
}

func (r *StaleReconciler) pushGroupMapping(ctx context.Context, id string, peer *Node, localNodeID, nodeToken string) error {
	var gm GroupMappingData
	err := r.db.QueryRowContext(ctx, `
		SELECT id, provider_id, external_group, COALESCE(external_group_name, ''),
		       role, COALESCE(tenant_id, ''), auto_sync, COALESCE(last_synced_at, 0),
		       created_at, updated_at
		FROM idp_group_mappings WHERE id = ?
	`, id).Scan(
		&gm.ID, &gm.ProviderID, &gm.ExternalGroup, &gm.ExternalGroupName,
		&gm.Role, &gm.TenantID, &gm.AutoSync, &gm.LastSyncedAt,
		&gm.CreatedAt, &gm.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query group mapping %s: %w", id, err)
	}
	return r.postToNode(ctx, peer, "/api/internal/cluster/group-mapping-sync", &gm, localNodeID, nodeToken)
}
