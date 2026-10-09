package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// handleReceiveAccessKeySync handles incoming access key synchronization from other nodes
// POST /api/internal/cluster/access-key-sync
func (s *Server) handleReceiveAccessKeySync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get source node ID from context (set by auth middleware)
	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse access key data from JSON body
	var accessKeyData struct {
		AccessKeyID     string `json:"access_key_id"`
		SecretAccessKey string `json:"secret_access_key"`
		UserID          string `json:"user_id"`
		Status          string `json:"status"`
		CreatedAt       int64  `json:"created_at"`
		LastUsed        *int64 `json:"last_used,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&accessKeyData); err != nil {
		logrus.WithError(err).Error("Failed to decode access key data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"access_key_id":  accessKeyData.AccessKeyID,
		"user_id":        accessKeyData.UserID,
	}).Info("Receiving access key from synchronization")

	// Skip if this entity has been deleted (tombstone exists)
	if hasDeletion, _ := cluster.HasDeletion(ctx, s.db, cluster.EntityTypeAccessKey, accessKeyData.AccessKeyID); hasDeletion {
		logrus.WithField("access_key_id", accessKeyData.AccessKeyID).Debug("Skipping sync for deleted access key (tombstone exists)")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Skipped (deleted)"})
		return
	}

	// Upsert access key in database (INSERT OR REPLACE)
	query := `
		INSERT OR REPLACE INTO access_keys
		(access_key_id, secret_access_key, user_id, status, created_at, last_used)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		accessKeyData.AccessKeyID,
		accessKeyData.SecretAccessKey,
		accessKeyData.UserID,
		accessKeyData.Status,
		accessKeyData.CreatedAt,
		accessKeyData.LastUsed,
	)

	if err != nil {
		logrus.WithError(err).Error("Failed to store access key")
		http.Error(w, fmt.Sprintf("Failed to store access key: %v", err), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"access_key_id": accessKeyData.AccessKeyID,
		"user_id":       accessKeyData.UserID,
	}).Info("Access key synchronized successfully")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Access key synchronized successfully",
	})
}

// handleReceiveBucketPermissionSync handles incoming bucket permission synchronization from other nodes
// POST /api/internal/cluster/bucket-permission-sync
func (s *Server) handleReceiveBucketPermissionSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get source node ID from context (set by auth middleware)
	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse permission data from JSON body
	var permissionData struct {
		ID              string  `json:"id"`
		BucketName      string  `json:"bucket_name"`
		BucketTenantID  string  `json:"bucket_tenant_id"`
		UserID          *string `json:"user_id,omitempty"`
		TenantID        *string `json:"tenant_id,omitempty"`
		PermissionLevel string  `json:"permission_level"`
		GrantedBy       string  `json:"granted_by"`
		GrantedAt       int64   `json:"granted_at"`
		ExpiresAt       *int64  `json:"expires_at,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&permissionData); err != nil {
		logrus.WithError(err).Error("Failed to decode permission data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"permission_id":  permissionData.ID,
		"bucket":         permissionData.BucketName,
	}).Info("Receiving bucket permission from synchronization")

	// Skip if this entity has been deleted (tombstone exists)
	if hasDeletion, _ := cluster.HasDeletion(ctx, s.db, cluster.EntityTypeBucketPermission, permissionData.ID); hasDeletion {
		logrus.WithField("permission_id", permissionData.ID).Debug("Skipping sync for deleted bucket permission (tombstone exists)")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Skipped (deleted)"})
		return
	}

	// Upsert permission in database (INSERT OR REPLACE)
	query := `
		INSERT OR REPLACE INTO bucket_permissions
		(id, bucket_name, bucket_tenant_id, user_id, tenant_id, permission_level, granted_by, granted_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		permissionData.ID,
		permissionData.BucketName,
		permissionData.BucketTenantID,
		permissionData.UserID,
		permissionData.TenantID,
		permissionData.PermissionLevel,
		permissionData.GrantedBy,
		permissionData.GrantedAt,
		permissionData.ExpiresAt,
	)

	if err != nil {
		logrus.WithError(err).Error("Failed to store bucket permission")
		http.Error(w, fmt.Sprintf("Failed to store permission: %v", err), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"permission_id": permissionData.ID,
		"bucket":        permissionData.BucketName,
	}).Info("Bucket permission synchronized successfully")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Bucket permission synchronized successfully",
	})
}

// handleReceiveIDPProviderSync handles incoming IDP provider synchronization from other nodes
// POST /api/internal/cluster/idp-provider-sync
func (s *Server) handleReceiveIDPProviderSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get source node ID from context (set by auth middleware)
	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse IDP provider data from JSON body
	var providerData struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Type      string `json:"type"`
		TenantID  string `json:"tenant_id"`
		Status    string `json:"status"`
		Config    string `json:"config"`
		CreatedBy string `json:"created_by"`
		CreatedAt int64  `json:"created_at"`
		UpdatedAt int64  `json:"updated_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&providerData); err != nil {
		logrus.WithError(err).Error("Failed to decode IDP provider data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"provider_id":    providerData.ID,
		"provider_name":  providerData.Name,
		"provider_type":  providerData.Type,
	}).Info("Receiving IDP provider from synchronization")

	// Skip a copy older than a deletion this node recorded.
	if cluster.DeletionSupersedes(ctx, s.db, cluster.EntityTypeIDPProvider, providerData.ID, providerData.UpdatedAt) {
		logrus.WithField("provider_id", providerData.ID).Debug("Skipping sync for deleted IDP provider (tombstone exists)")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Skipped (deleted)"})
		return
	}

	// Handle NULL tenant_id
	var tenantID interface{}
	if providerData.TenantID != "" {
		tenantID = providerData.TenantID
	}

	// Check if provider exists
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identity_providers WHERE id = ?)`, providerData.ID).Scan(&exists)
	if err != nil {
		logrus.WithError(err).Error("Failed to check IDP provider existence")
		http.Error(w, fmt.Sprintf("Failed to check provider: %v", err), http.StatusInternalServerError)
		return
	}

	if exists {
		// LWW: only apply if the incoming record is strictly newer than the local one.
		var localUpdatedAt int64
		if err := s.db.QueryRowContext(ctx, `SELECT updated_at FROM identity_providers WHERE id = ?`, providerData.ID).Scan(&localUpdatedAt); err != nil {
			logrus.WithError(err).Error("Failed to read local IDP provider updated_at")
			http.Error(w, fmt.Sprintf("Failed to check provider timestamp: %v", err), http.StatusInternalServerError)
			return
		}
		if providerData.UpdatedAt <= localUpdatedAt {
			logrus.WithField("provider_id", providerData.ID).Debug("Skipping IDP provider update: local record is newer or equal (LWW)")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "IDP provider synchronized successfully"})
			return
		}

		// Update existing provider — preserve source updated_at so all nodes agree on the timestamp.
		_, err = s.db.ExecContext(ctx, `
			UPDATE identity_providers SET
				name = ?, type = ?, tenant_id = ?, status = ?, config = ?, updated_at = ?
			WHERE id = ?
		`,
			providerData.Name,
			providerData.Type,
			tenantID,
			providerData.Status,
			providerData.Config,
			providerData.UpdatedAt,
			providerData.ID,
		)
		if err != nil {
			logrus.WithError(err).Error("Failed to update IDP provider")
			http.Error(w, fmt.Sprintf("Failed to sync provider: %v", err), http.StatusInternalServerError)
			return
		}
		logrus.WithField("provider_id", providerData.ID).Debug("Updated existing IDP provider")
	} else {
		// Insert new provider — preserve source updated_at for cross-node consistency.
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO identity_providers (id, name, type, tenant_id, status, config, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			providerData.ID,
			providerData.Name,
			providerData.Type,
			tenantID,
			providerData.Status,
			providerData.Config,
			providerData.CreatedBy,
			providerData.CreatedAt,
			providerData.UpdatedAt,
		)
		if err != nil {
			logrus.WithError(err).Error("Failed to insert IDP provider")
			http.Error(w, fmt.Sprintf("Failed to sync provider: %v", err), http.StatusInternalServerError)
			return
		}
		logrus.WithField("provider_id", providerData.ID).Debug("Inserted new IDP provider")
	}

	logrus.WithFields(logrus.Fields{
		"provider_id":   providerData.ID,
		"provider_name": providerData.Name,
	}).Info("IDP provider synchronized successfully")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "IDP provider synchronized successfully",
	})
}

// handleReceiveIDPProviderDeleteSync handles incoming IDP provider deletion from cluster sync
// POST /api/internal/cluster/idp-provider-delete-sync
func (s *Server) handleReceiveIDPProviderDeleteSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var deleteData struct {
		ID        string `json:"id"`
		DeletedAt int64  `json:"deleted_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&deleteData); err != nil {
		logrus.WithError(err).Error("Failed to decode IDP provider deletion data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if deleteData.ID == "" {
		http.Error(w, "Missing provider ID", http.StatusBadRequest)
		return
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"provider_id":    deleteData.ID,
	}).Info("Receiving IDP provider deletion from synchronization")

	if _, err := s.applyReceivedDeletion(ctx, cluster.EntityTypeIDPProvider, deleteData.ID, receivedDeletionTime(deleteData.DeletedAt), sourceNodeID); err != nil {
		logrus.WithError(err).WithField("id", deleteData.ID).Error("Failed to apply a received deletion")
		http.Error(w, "Failed to apply deletion: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// handleReceiveGroupMappingSync handles incoming IDP group mapping synchronization from other nodes
// POST /api/internal/cluster/group-mapping-sync
func (s *Server) handleReceiveGroupMappingSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get source node ID from context (set by auth middleware)
	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse group mapping data from JSON body
	var mappingData struct {
		ID                string `json:"id"`
		ProviderID        string `json:"provider_id"`
		ExternalGroup     string `json:"external_group"`
		ExternalGroupName string `json:"external_group_name"`
		Role              string `json:"role"`
		TenantID          string `json:"tenant_id"`
		AutoSync          bool   `json:"auto_sync"`
		LastSyncedAt      int64  `json:"last_synced_at"`
		CreatedAt         int64  `json:"created_at"`
		UpdatedAt         int64  `json:"updated_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&mappingData); err != nil {
		logrus.WithError(err).Error("Failed to decode group mapping data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"mapping_id":     mappingData.ID,
		"provider_id":    mappingData.ProviderID,
		"external_group": mappingData.ExternalGroupName,
	}).Info("Receiving group mapping from synchronization")

	// Skip a copy older than a deletion this node recorded.
	if cluster.DeletionSupersedes(ctx, s.db, cluster.EntityTypeGroupMapping, mappingData.ID, mappingData.UpdatedAt) {
		logrus.WithField("mapping_id", mappingData.ID).Debug("Skipping sync for deleted group mapping (tombstone exists)")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Skipped (deleted)"})
		return
	}

	// Handle NULL tenant_id
	var tenantID interface{}
	if mappingData.TenantID != "" {
		tenantID = mappingData.TenantID
	}

	// Handle NULL last_synced_at
	var lastSyncedAt interface{}
	if mappingData.LastSyncedAt > 0 {
		lastSyncedAt = mappingData.LastSyncedAt
	}

	// Check if mapping exists
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM idp_group_mappings WHERE id = ?)`, mappingData.ID).Scan(&exists)
	if err != nil {
		logrus.WithError(err).Error("Failed to check group mapping existence")
		http.Error(w, fmt.Sprintf("Failed to check mapping: %v", err), http.StatusInternalServerError)
		return
	}

	if exists {
		// LWW: only apply if the incoming record is strictly newer than the local one.
		var localUpdatedAt int64
		if err := s.db.QueryRowContext(ctx, `SELECT updated_at FROM idp_group_mappings WHERE id = ?`, mappingData.ID).Scan(&localUpdatedAt); err != nil {
			logrus.WithError(err).Error("Failed to read local group mapping updated_at")
			http.Error(w, fmt.Sprintf("Failed to check mapping timestamp: %v", err), http.StatusInternalServerError)
			return
		}
		if mappingData.UpdatedAt <= localUpdatedAt {
			logrus.WithField("mapping_id", mappingData.ID).Debug("Skipping group mapping update: local record is newer or equal (LWW)")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Group mapping synchronized successfully"})
			return
		}

		// Update existing mapping — preserve source updated_at so all nodes agree on the timestamp.
		_, err = s.db.ExecContext(ctx, `
			UPDATE idp_group_mappings SET
				provider_id = ?, external_group = ?, external_group_name = ?,
				role = ?, tenant_id = ?, auto_sync = ?, last_synced_at = ?, updated_at = ?
			WHERE id = ?
		`,
			mappingData.ProviderID,
			mappingData.ExternalGroup,
			mappingData.ExternalGroupName,
			mappingData.Role,
			tenantID,
			mappingData.AutoSync,
			lastSyncedAt,
			mappingData.UpdatedAt,
			mappingData.ID,
		)
		if err != nil {
			logrus.WithError(err).Error("Failed to update group mapping")
			http.Error(w, fmt.Sprintf("Failed to sync mapping: %v", err), http.StatusInternalServerError)
			return
		}
		logrus.WithField("mapping_id", mappingData.ID).Debug("Updated existing group mapping")
	} else {
		// Insert new mapping — preserve source updated_at for cross-node consistency.
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO idp_group_mappings (id, provider_id, external_group, external_group_name,
				role, tenant_id, auto_sync, last_synced_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			mappingData.ID,
			mappingData.ProviderID,
			mappingData.ExternalGroup,
			mappingData.ExternalGroupName,
			mappingData.Role,
			tenantID,
			mappingData.AutoSync,
			lastSyncedAt,
			mappingData.CreatedAt,
			mappingData.UpdatedAt,
		)
		if err != nil {
			logrus.WithError(err).Error("Failed to insert group mapping")
			http.Error(w, fmt.Sprintf("Failed to sync mapping: %v", err), http.StatusInternalServerError)
			return
		}
		logrus.WithField("mapping_id", mappingData.ID).Debug("Inserted new group mapping")
	}

	logrus.WithFields(logrus.Fields{
		"mapping_id":     mappingData.ID,
		"external_group": mappingData.ExternalGroupName,
	}).Info("Group mapping synchronized successfully")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Group mapping synchronized successfully",
	})
}

// handleReceiveGroupMappingDeleteSync handles incoming group mapping deletion from cluster sync
// POST /api/internal/cluster/group-mapping-delete-sync
func (s *Server) handleReceiveGroupMappingDeleteSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var deleteData struct {
		ID        string `json:"id"`
		DeletedAt int64  `json:"deleted_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&deleteData); err != nil {
		logrus.WithError(err).Error("Failed to decode group mapping deletion data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if deleteData.ID == "" {
		http.Error(w, "Missing mapping ID", http.StatusBadRequest)
		return
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"mapping_id":     deleteData.ID,
	}).Info("Receiving group mapping deletion from synchronization")

	if _, err := s.applyReceivedDeletion(ctx, cluster.EntityTypeGroupMapping, deleteData.ID, receivedDeletionTime(deleteData.DeletedAt), sourceNodeID); err != nil {
		logrus.WithError(err).WithField("id", deleteData.ID).Error("Failed to apply a received deletion")
		http.Error(w, "Failed to apply deletion: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// handleReceiveAccessKeyDeleteSync handles incoming access key deletion from cluster sync
// POST /api/internal/cluster/access-key-delete-sync
func (s *Server) handleReceiveAccessKeyDeleteSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var deleteData struct {
		ID        string `json:"id"`
		DeletedAt int64  `json:"deleted_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&deleteData); err != nil {
		logrus.WithError(err).Error("Failed to decode access key deletion data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if deleteData.ID == "" {
		http.Error(w, "Missing access key ID", http.StatusBadRequest)
		return
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"access_key_id":  deleteData.ID,
	}).Info("Receiving access key deletion from synchronization")

	if _, err := s.applyReceivedDeletion(ctx, cluster.EntityTypeAccessKey, deleteData.ID, receivedDeletionTime(deleteData.DeletedAt), sourceNodeID); err != nil {
		logrus.WithError(err).WithField("id", deleteData.ID).Error("Failed to apply a received deletion")
		http.Error(w, "Failed to apply deletion: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// handleReceiveBucketPermissionDeleteSync handles incoming bucket permission deletion from cluster sync
// POST /api/internal/cluster/bucket-permission-delete-sync
func (s *Server) handleReceiveBucketPermissionDeleteSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var deleteData struct {
		ID        string `json:"id"`
		DeletedAt int64  `json:"deleted_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&deleteData); err != nil {
		logrus.WithError(err).Error("Failed to decode bucket permission deletion data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if deleteData.ID == "" {
		http.Error(w, "Missing permission ID", http.StatusBadRequest)
		return
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"permission_id":  deleteData.ID,
	}).Info("Receiving bucket permission deletion from synchronization")

	if _, err := s.applyReceivedDeletion(ctx, cluster.EntityTypeBucketPermission, deleteData.ID, receivedDeletionTime(deleteData.DeletedAt), sourceNodeID); err != nil {
		logrus.WithError(err).WithField("id", deleteData.ID).Error("Failed to apply a received deletion")
		http.Error(w, "Failed to apply deletion: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// receivedDeletionTime is when a deletion another node sends was made, or now
// when an earlier release sent no time.
func receivedDeletionTime(deletedAt int64) int64 {
	if deletedAt > 0 {
		return deletedAt
	}
	return time.Now().Unix()
}

// handleReceiveDeletionLogSync handles incoming deletion log entries from other nodes
// POST /api/internal/cluster/deletion-log-sync
func (s *Server) handleReceiveDeletionLogSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var entries []*cluster.DeletionEntry
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		logrus.WithError(err).Error("Failed to decode deletion log entries")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"entry_count":    len(entries),
	}).Debug("Receiving deletion log entries from synchronization")

	recorded := 0
	for _, entry := range entries {
		if entry.DeletedAt <= 0 {
			continue
		}
		if _, err := s.applyReceivedDeletion(ctx, entry.EntityType, entry.EntityID, entry.DeletedAt, entry.DeletedByNodeID); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"entity_type": entry.EntityType,
				"entity_id":   entry.EntityID,
			}).Warn("Failed to apply a deletion log entry")
			http.Error(w, "Failed to apply deletion", http.StatusInternalServerError)
			return
		}
		recorded++
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"recorded":       recorded,
		"total":          len(entries),
	}).Debug("Deletion log entries synchronized")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"message":  "Deletion log entries synchronized",
		"recorded": recorded,
	})
}

// handleHAReceivePut receives an HA-fanout PUT from the primary node.
// PUT /api/internal/ha/objects/{key:.*}
// Bucket path is in the X-HA-Bucket header; key may contain slashes.
func (s *Server) handleHAReceivePut(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := mux.Vars(r)["key"]
	bucketPath := r.Header.Get(cluster.HABucketHeader)

	if bucketPath == "" || key == "" {
		http.Error(w, "missing bucket or key", http.StatusBadRequest)
		return
	}

	ctx = cluster.WithHAReplicaContext(ctx)
	if cluster.VersionDeleted(ctx, s.db, bucketPath, key, r.Header.Get(cluster.HAObjectVersionHeader)) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if lm, ok := cluster.HALastModifiedFromHeader(r.Header); ok && r.Header.Get(cluster.HARawHeader) != "true" &&
		r.Header.Get(cluster.HAObjectVersionHeader) == "" && cluster.KeyDeletedAfter(ctx, s.db, bucketPath, key, lm) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Raw ciphertext transfer: store the encrypted bytes + sidecar + Pebble
	// metadata exactly as sent by the primary — no decrypt/re-encrypt.
	if r.Header.Get(cluster.HARawHeader) == "true" {
		s.handleHAReceiveRawPut(w, r, ctx, bucketPath, key)
		return
	}

	headers := r.Header.Clone()
	if _, err := s.objectManager.PutObject(cluster.ReplicaWriteContext(ctx, r.Header), bucketPath, key, r.Body, headers); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucketPath, "key": key}).
			Error("HA receive PUT failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleHAReceiveRawPut applies a raw ciphertext replica write. Responds 412
// when this node cannot decrypt the object's KEK version (the primary then
// falls back to the legacy transfer for this node).
func (s *Server) handleHAReceiveRawPut(w http.ResponseWriter, r *http.Request, ctx context.Context, bucketPath, key string) {
	raw, ok := s.objectManager.(object.RawObjectAccessor)
	if !ok {
		http.Error(w, "raw replication not supported", http.StatusPreconditionFailed)
		return
	}

	sidecarJSON, err := base64.StdEncoding.DecodeString(r.Header.Get(cluster.HARawSidecarHeader))
	if err != nil {
		http.Error(w, "invalid raw sidecar header", http.StatusBadRequest)
		return
	}
	var sidecar map[string]string
	if err := json.Unmarshal(sidecarJSON, &sidecar); err != nil {
		http.Error(w, "invalid raw sidecar payload", http.StatusBadRequest)
		return
	}

	metaJSON, err := base64.StdEncoding.DecodeString(r.Header.Get(cluster.HARawObjectMetaHeader))
	if err != nil {
		http.Error(w, "invalid raw object-meta header", http.StatusBadRequest)
		return
	}
	var metaObj metadata.ObjectMetadata
	if err := json.Unmarshal(metaJSON, &metaObj); err != nil {
		http.Error(w, "invalid raw object-meta payload", http.StatusBadRequest)
		return
	}
	if cluster.VersionDeleted(ctx, s.db, bucketPath, key, metaObj.VersionID) ||
		(metaObj.VersionID == "" && cluster.KeyDeletedAfter(ctx, s.db, bucketPath, key, metaObj.LastModified)) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Guard: this node must hold the cluster-shared KEK version that wraps the
	// object's DEK, or it could never serve reads for it.
	if !raw.CanReplicateRaw(sidecar) {
		http.Error(w, "cannot decrypt this KEK version on this node", http.StatusPreconditionFailed)
		return
	}

	if err := raw.PutObjectRaw(ctx, bucketPath, key, r.Body, sidecar, &metaObj); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucketPath, "key": key}).
			Error("HA receive raw PUT failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleHAReceiveDelete receives an HA-fanout DELETE from the primary node.
// DELETE /api/internal/ha/objects/{key:.*}
func (s *Server) handleHAReceiveDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := mux.Vars(r)["key"]
	bucketPath := r.Header.Get(cluster.HABucketHeader)

	if bucketPath == "" || key == "" {
		http.Error(w, "missing bucket or key", http.StatusBadRequest)
		return
	}

	ctx = cluster.WithHAReplicaContext(ctx)
	specificVersionID := r.Header.Get(cluster.HAObjectVersionHeader)
	deleteMarkerVersionID := r.Header.Get(cluster.HADeleteMarkerVersionHeader)
	if specificVersionID != "" && deleteMarkerVersionID != "" {
		http.Error(w, "conflicting version headers", http.StatusBadRequest)
		return
	}
	if cluster.VersionDeleted(ctx, s.db, bucketPath, key, deleteMarkerVersionID) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if deleteMarkerVersionID != "" {
		ctx = object.WithReplicatedVersionID(ctx, deleteMarkerVersionID)
		if lm, ok := cluster.HALastModifiedFromHeader(r.Header); ok {
			ctx = object.WithReplicatedLastModified(ctx, lm)
		}
		ctx = cluster.WithHAWrittenAt(ctx, r.Header)
	}

	var err error
	if specificVersionID != "" {
		_, err = s.objectManager.DeleteObject(ctx, bucketPath, key, false, specificVersionID)
	} else {
		_, err = s.objectManager.DeleteObject(ctx, bucketPath, key, false)
	}
	if status := haDeleteStatus(err); status != http.StatusNoContent {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucketPath, "key": key}).
			Warn("HA: a delete from another node was not applied")
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// haDeleteStatus tells the sender of a delete what came of it: done, when the
// key is gone here whether or not this delete removed it; refused, when its
// lock keeps it and sending it again changes nothing; or failed.
func haDeleteStatus(err error) int {
	var retention *object.RetentionError
	switch {
	case err == nil, errors.Is(err, object.ErrObjectNotFound), errors.Is(err, object.ErrBucketNotFound):
		return http.StatusNoContent
	case errors.As(err, &retention), errors.Is(err, object.ErrObjectUnderLegalHold),
		errors.Is(err, object.ErrObjectLocked), errors.Is(err, object.ErrRetentionPeriod):
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

// handleHABucketState applies what another node reports about a bucket: its
// configuration and ACL, or its deletion.
// POST /api/internal/cluster/ha/bucket-state
func (s *Server) handleHABucketState(w http.ResponseWriter, r *http.Request) {
	var st cluster.BucketState
	if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	err := s.bucketStateReceiver.Apply(cluster.WithHAReplicaContext(r.Context()), &st)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, cluster.ErrBucketNameTaken):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, cluster.ErrInvalidBucketState):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		logrus.WithError(err).Error("HA: failed to apply a bucket from another node")
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleHARowStates stores the rows another node sends, and answers with those
// it refused.
// POST /api/internal/cluster/ha/row-states
func (s *Server) handleHARowStates(w http.ResponseWriter, r *http.Request) {
	var batch cluster.RowStateBatch
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	refused, err := s.rowStates.Apply(r.Context(), batch.Rows)
	if err != nil {
		logrus.WithError(err).Error("HA: failed to store rows from another node")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cluster.RowStateResult{Refused: refused}) //nolint:errcheck
}

// handleHAReceiveMetadataOp receives a metadata-only operation fanout.
// POST /api/internal/ha/metadata-op
// Bucket path is in X-HA-Bucket header; body is JSON cluster.HAMetadataOp.
func (s *Server) handleHAReceiveMetadataOp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketPath := r.Header.Get(cluster.HABucketHeader)
	if bucketPath == "" {
		http.Error(w, "missing "+cluster.HABucketHeader+" header", http.StatusBadRequest)
		return
	}

	var op cluster.HAMetadataOp
	if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}

	ctx = cluster.WithHAReplicaContext(ctx)

	if err := s.applyHAMetadataOp(ctx, bucketPath, op); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucketPath, "op": op.Op, "key": op.Key}).
			Error("HA receive metadata-op failed")
		http.Error(w, err.Error(), metadataOpStatus(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// metadataOpStatus tells the sender whether a failed metadata change can ever
// apply here: a 4xx means it cannot and must not be retried; a 5xx may pass
// on a later attempt.
func metadataOpStatus(err error) int {
	var retention *object.RetentionError
	var syntax *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.Is(err, object.ErrObjectNotFound), errors.Is(err, object.ErrBucketNotFound):
		return http.StatusNotFound
	case errors.As(err, &syntax), errors.As(err, &typeErr):
		return http.StatusBadRequest
	case errors.As(err, &retention),
		errors.Is(err, object.ErrObjectUnderLegalHold),
		errors.Is(err, object.ErrCannotShortenCompliance),
		errors.Is(err, object.ErrCannotShortenGovernance),
		errors.Is(err, object.ErrNoRetentionConfiguration),
		errors.Is(err, object.ErrInvalidRetentionMode),
		errors.Is(err, object.ErrInvalidLegalHoldStatus):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// applyHAMetadataOp dispatches a received metadata operation to the object manager.
func (s *Server) applyHAMetadataOp(ctx context.Context, bucket string, op cluster.HAMetadataOp) error {
	om := s.objectManager
	switch op.Op {
	case "update-metadata":
		var m map[string]string
		if err := json.Unmarshal(op.Data, &m); err != nil {
			return err
		}
		return om.UpdateObjectMetadata(ctx, bucket, op.Key, m)

	case "set-tagging":
		var tags object.TagSet
		if err := json.Unmarshal(op.Data, &tags); err != nil {
			return err
		}
		if op.VersionID != "" {
			return om.SetObjectTagging(ctx, bucket, op.Key, &tags, op.VersionID)
		}
		return om.SetObjectTagging(ctx, bucket, op.Key, &tags)

	case "delete-tagging":
		if op.VersionID != "" {
			return om.DeleteObjectTagging(ctx, bucket, op.Key, op.VersionID)
		}
		return om.DeleteObjectTagging(ctx, bucket, op.Key)

	case "set-acl":
		var acl object.ACL
		if err := json.Unmarshal(op.Data, &acl); err != nil {
			return err
		}
		if op.VersionID != "" {
			return om.SetObjectACL(ctx, bucket, op.Key, &acl, op.VersionID)
		}
		return om.SetObjectACL(ctx, bucket, op.Key, &acl)

	case "set-retention":
		var cfg object.RetentionConfig
		if err := json.Unmarshal(op.Data, &cfg); err != nil {
			return err
		}
		if op.VersionID != "" {
			return om.SetObjectRetention(ctx, bucket, op.Key, &cfg, op.VersionID)
		}
		return om.SetObjectRetention(ctx, bucket, op.Key, &cfg)

	case "set-legal-hold":
		var cfg object.LegalHoldConfig
		if err := json.Unmarshal(op.Data, &cfg); err != nil {
			return err
		}
		if op.VersionID != "" {
			return om.SetObjectLegalHold(ctx, bucket, op.Key, &cfg, op.VersionID)
		}
		return om.SetObjectLegalHold(ctx, bucket, op.Key, &cfg)

	case "set-locations":
		var change object.LocationsChange
		if err := json.Unmarshal(op.Data, &change); err != nil {
			return err
		}
		writer, ok := cluster.ReplicaWriter(om)
		if !ok {
			return fmt.Errorf("this node keeps no entries")
		}
		return writer.SetLocations(ctx, bucket, op.Key, change)

	case "set-restore-status":
		var p struct {
			Status    string     `json:"status"`
			ExpiresAt *time.Time `json:"expires_at,omitempty"`
		}
		if err := json.Unmarshal(op.Data, &p); err != nil {
			return err
		}
		if op.VersionID != "" {
			return om.SetRestoreStatus(ctx, bucket, op.Key, p.Status, p.ExpiresAt, op.VersionID)
		}
		return om.SetRestoreStatus(ctx, bucket, op.Key, p.Status, p.ExpiresAt)

	default:
		logrus.WithField("op", op.Op).Warn("HA receive metadata-op: unknown operation, ignoring")
		return nil
	}
}

// handleHAGetObject serves an object to a rejoining stale replica.
// GET /api/internal/ha/objects/{key:.*}
// Bucket path is in the X-HA-Bucket query parameter (or header).
func (s *Server) handleHAGetObject(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	bucketPath := r.URL.Query().Get("bucket")
	if bucketPath == "" {
		bucketPath = r.Header.Get(cluster.HABucketHeader)
	}
	if bucketPath == "" || key == "" {
		http.Error(w, "missing bucket or key", http.StatusBadRequest)
		return
	}

	// Served from this node's data only: a node that does not hold it answers
	// 404 and the reader asks another.
	ctx := cluster.WithHAReplicaContext(r.Context())
	obj, reader, err := s.objectManager.GetObject(ctx, bucketPath, key, r.URL.Query().Get("versionId"))
	if err != nil {
		var elsewhere *object.DataElsewhereError
		if err == object.ErrObjectNotFound || errors.As(err, &elsewhere) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer reader.Close()
	offset, err := haRangeStart(r.Header.Get("Range"), obj.Size)
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// Forward all object metadata as headers so the receiver can reconstruct it.
	w.Header().Set("Content-Type", obj.ContentType)
	if obj.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", obj.ContentDisposition)
	}
	if obj.ContentEncoding != "" {
		w.Header().Set("Content-Encoding", obj.ContentEncoding)
	}
	if obj.CacheControl != "" {
		w.Header().Set("Cache-Control", obj.CacheControl)
	}
	if obj.ContentLanguage != "" {
		w.Header().Set("Content-Language", obj.ContentLanguage)
	}
	if obj.StorageClass != "" {
		w.Header().Set("x-amz-storage-class", obj.StorageClass)
	}
	if obj.VersionID != "" {
		w.Header().Set(cluster.HAObjectVersionHeader, obj.VersionID)
		w.Header().Set("x-amz-version-id", obj.VersionID)
	}
	if !obj.LastModified.IsZero() && obj.LastModified.Unix() > 0 {
		w.Header().Set(cluster.HALastModifiedHeader, fmt.Sprintf("%d", obj.LastModified.Unix()))
	}
	cluster.SetHAWrittenAt(w.Header(), obj.WrittenAt)
	if obj.ChecksumAlgorithm != "" && obj.ChecksumValue != "" {
		w.Header().Set("x-amz-checksum-algorithm", obj.ChecksumAlgorithm)
		w.Header().Set("x-amz-checksum-"+strings.ToLower(obj.ChecksumAlgorithm), obj.ChecksumValue)
	}
	for k, v := range obj.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}
	cluster.SetHAObjectLock(w.Header(), obj)
	cluster.SetHAAttributes(w.Header(), obj)
	w.Header().Set(cluster.HAETagHeader, obj.ETag)
	cluster.SetHALocations(w.Header(), obj)
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, reader, offset); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Size-offset))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		if obj.Size > 0 {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Size))
		}
		w.WriteHeader(http.StatusOK)
	}
	io.Copy(w, reader) //nolint:errcheck
}

// haRangeStart is where a "bytes=N-" range another node asks for starts: 0
// without one. A start past the end is refused.
func haRangeStart(header string, size int64) (int64, error) {
	if header == "" {
		return 0, nil
	}
	spec, ok := strings.CutPrefix(header, "bytes=")
	start, rest, dash := strings.Cut(spec, "-")
	if !ok || !dash || rest != "" {
		return 0, fmt.Errorf("unsupported range %q", header)
	}
	offset, err := strconv.ParseInt(start, 10, 64)
	if err != nil || offset < 0 || (offset > 0 && offset >= size) {
		return 0, fmt.Errorf("unsatisfiable range %q", header)
	}
	return offset, nil
}

// handleHAGetObjectEntry serves this node's entry of an object, or of one of
// its versions.
// GET /api/internal/cluster/ha/object-entry?bucket=<path>&key=<key>&versionId=<id>
func (s *Server) handleHAGetObjectEntry(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	bucketPath, key := q.Get("bucket"), q.Get("key")
	if bucketPath == "" || key == "" {
		http.Error(w, "missing bucket or key", http.StatusBadRequest)
		return
	}
	writer, ok := cluster.ReplicaWriter(s.objectManager)
	if !ok {
		http.Error(w, "entries are not kept on this node", http.StatusNotImplemented)
		return
	}
	entry, err := writer.ObjectEntry(r.Context(), bucketPath, key, q.Get("versionId"))
	switch {
	case errors.Is(err, metadata.ErrObjectNotFound) || errors.Is(err, metadata.ErrVersionNotFound):
		http.Error(w, "not found", http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry) //nolint:errcheck
}

// handleHAReceiveObjectEntry stores the entry of an object whose data other
// nodes hold.
// PUT /api/internal/cluster/ha/object-entry
func (s *Server) handleHAReceiveObjectEntry(w http.ResponseWriter, r *http.Request) {
	var entry metadata.ObjectMetadata
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if entry.Bucket == "" || entry.Key == "" || len(entry.Locations) == 0 {
		http.Error(w, "an entry names its bucket, key and the nodes holding its data", http.StatusBadRequest)
		return
	}
	ctx := cluster.WithHAReplicaContext(r.Context())
	if cluster.VersionDeleted(ctx, s.db, entry.Bucket, entry.Key, entry.VersionID) ||
		(entry.VersionID == "" && cluster.KeyDeletedAfter(ctx, s.db, entry.Bucket, entry.Key, entry.LastModified)) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writer, ok := cluster.ReplicaWriter(s.objectManager)
	if !ok {
		http.Error(w, "entries are not stored on this node", http.StatusNotImplemented)
		return
	}
	err := writer.PutReplicaMetadata(ctx, &entry)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, object.ErrBucketNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": entry.Bucket, "key": entry.Key}).
			Error("HA: failed to store the entry of an object other nodes hold")
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleHAListMultipartUploads lists the multipart uploads of a bucket started
// on this node.
// GET /api/internal/cluster/ha/multipart-uploads?bucket=<path>
func (s *Server) handleHAListMultipartUploads(w http.ResponseWriter, r *http.Request) {
	bucketPath := r.URL.Query().Get("bucket")
	if bucketPath == "" {
		http.Error(w, "missing bucket", http.StatusBadRequest)
		return
	}
	uploads, err := s.objectManager.ListMultipartUploads(r.Context(), bucketPath)
	if err != nil && !errors.Is(err, object.ErrBucketNotFound) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if uploads == nil {
		uploads = []object.MultipartUpload{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"uploads": uploads}) //nolint:errcheck
}

// handleHAChecksumBatch returns the etag, size and times of each requested key
// and, when asked, the IDs of its versions and delete markers.
func (s *Server) handleHAChecksumBatch(w http.ResponseWriter, r *http.Request) {
	var req cluster.ChecksumRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Bucket == "" {
		http.Error(w, "missing bucket", http.StatusBadRequest)
		return
	}

	writer, holds := cluster.ReplicaWriter(s.objectManager)
	held := func(key, versionID string) (bool, error) {
		if !holds {
			return false, nil
		}
		return writer.HoldsData(r.Context(), req.Bucket, key, versionID)
	}
	entries := make([]cluster.ChecksumEntry, 0, len(req.Keys))
	for _, key := range req.Keys {
		entry := cluster.ChecksumEntry{Key: key}
		if req.Versions {
			var err error
			if entry.Versions, err = cluster.KeyVersionIDs(r.Context(), s.objectManager, req.Bucket, key); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for _, id := range entry.Versions {
				ok, err := held(key, id)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				if ok {
					entry.Held = append(entry.Held, id)
				}
				if holds {
					if version, err := writer.ObjectEntry(r.Context(), req.Bucket, key, id); err == nil && version.LocationsGen > 0 {
						if entry.Gens == nil {
							entry.Gens = make(map[string]int64)
						}
						entry.Gens[id] = version.LocationsGen
					}
				}
			}
		}
		if obj, err := s.objectManager.GetObjectMetadata(r.Context(), req.Bucket, key); err == nil && obj != nil {
			entry.Found = true
			entry.ETag = obj.ETag
			entry.Size = obj.Size
			entry.LastModified = obj.LastModified.Unix()
			entry.WrittenAt = obj.WrittenAt
			if req.Versions {
				entry.Locations, entry.LocationsGen = obj.Locations, obj.LocationsGen
				if entry.HoldsCurrent, err = held(key, ""); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}
		entries = append(entries, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cluster.ChecksumResponse{Entries: entries, Versions: req.Versions}) //nolint:errcheck
}

// handleGetLocalAuditLogs returns this node's audit logs as a flat JSON array.
// GET /api/internal/cluster/audit-logs
// Used by peer nodes to federate audit log queries.
func (s *Server) handleGetLocalAuditLogs(w http.ResponseWriter, r *http.Request) {
	if s.auditManager == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]*audit.AuditLog{}) //nolint:errcheck
		return
	}

	q := r.URL.Query()
	filters := &audit.AuditLogFilters{
		Page:     1,
		PageSize: 1000, // fetch a large batch; caller will paginate after merge
	}

	if v := q.Get("tenant_id"); v != "" {
		filters.TenantID = v
	}
	if v := q.Get("user_id"); v != "" {
		filters.UserID = v
	}
	if v := q.Get("event_type"); v != "" {
		filters.EventType = v
	}
	if v := q.Get("resource_type"); v != "" {
		filters.ResourceType = v
	}
	if v := q.Get("action"); v != "" {
		filters.Action = v
	}
	if v := q.Get("status"); v != "" {
		filters.Status = v
	}
	if v := q.Get("start_date"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			filters.StartDate = ts
		}
	}
	if v := q.Get("end_date"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			filters.EndDate = ts
		}
	}

	logs, _, err := s.auditManager.GetLogs(r.Context(), filters)
	if err != nil {
		http.Error(w, "failed to retrieve audit logs", http.StatusInternalServerError)
		return
	}
	if logs == nil {
		logs = []*audit.AuditLog{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs) //nolint:errcheck
}
