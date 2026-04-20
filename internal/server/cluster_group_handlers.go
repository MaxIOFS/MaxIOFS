package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// groupSyncPayload mirrors cluster.GroupData for incoming requests.
type groupSyncPayload struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	TenantID    string   `json:"tenant_id"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
	MemberIDs   []string `json:"member_ids"`
}

// handleReceiveGroupSync applies an incoming group + membership upsert from another node.
// POST /api/internal/cluster/group-sync
func (s *Server) handleReceiveGroupSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var data groupSyncPayload
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		logrus.WithError(err).Error("Failed to decode group sync data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if data.ID == "" || data.Name == "" {
		http.Error(w, "Missing group id or name", http.StatusBadRequest)
		return
	}

	logrus.WithFields(logrus.Fields{
		"group_id":       data.ID,
		"group_name":     data.Name,
		"member_count":   len(data.MemberIDs),
		"source_node_id": sourceNodeID,
	}).Info("Receiving group synchronization")

	// Skip if a tombstone exists for this group (deleted on another node).
	if hasDeletion, _ := cluster.HasDeletion(ctx, s.db, cluster.EntityTypeGroup, data.ID); hasDeletion {
		logrus.WithField("group_id", data.ID).Debug("Skipping sync for deleted group (tombstone exists)")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Skipped (deleted)"})
		return
	}

	if err := s.upsertGroupAndMembers(ctx, &data); err != nil {
		logrus.WithError(err).WithField("group_id", data.ID).Error("Failed to upsert group")
		http.Error(w, fmt.Sprintf("Failed to sync group: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Group synchronized successfully",
	})
}

// upsertGroupAndMembers writes the group row (LWW on updated_at) and replaces the
// group's membership set with the incoming MemberIDs.  Members that no longer exist
// locally as users are silently skipped — they will be picked up once the user sync
// catches up.
func (s *Server) upsertGroupAndMembers(ctx context.Context, g *groupSyncPayload) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var tenantID sql.NullString
	if g.TenantID != "" {
		tenantID = sql.NullString{String: g.TenantID, Valid: true}
	}

	var localUpdatedAt int64
	err = tx.QueryRowContext(ctx, `SELECT updated_at FROM groups WHERE id = ?`, g.ID).Scan(&localUpdatedAt)
	switch {
	case err == sql.ErrNoRows:
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO groups (id, name, display_name, description, tenant_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, g.ID, g.Name, g.DisplayName, g.Description, tenantID, g.CreatedAt, g.UpdatedAt); err != nil {
			return fmt.Errorf("insert group: %w", err)
		}
	case err != nil:
		return fmt.Errorf("read local updated_at: %w", err)
	default:
		// LWW: only overwrite if incoming is strictly newer than local.
		if g.UpdatedAt > localUpdatedAt {
			if _, err := tx.ExecContext(ctx, `
				UPDATE groups SET name = ?, display_name = ?, description = ?, tenant_id = ?, updated_at = ?
				WHERE id = ?
			`, g.Name, g.DisplayName, g.Description, tenantID, g.UpdatedAt, g.ID); err != nil {
				return fmt.Errorf("update group: %w", err)
			}
		}
	}

	// Replace membership set: delete all current members, then re-insert the incoming set.
	// Members that reference a non-existent user_id are skipped via INSERT OR IGNORE so the
	// foreign-key constraint doesn't abort the entire transaction.
	if _, err := tx.ExecContext(ctx, `DELETE FROM group_members WHERE group_id = ?`, g.ID); err != nil {
		return fmt.Errorf("clear members: %w", err)
	}

	now := time.Now().Unix()
	for _, userID := range g.MemberIDs {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)`, userID).Scan(&exists); err != nil {
			return fmt.Errorf("check user %s: %w", userID, err)
		}
		if !exists {
			// User not yet synced to this node — skip; will be reconciled on next group sync once user arrives.
			logrus.WithFields(logrus.Fields{
				"group_id": g.ID,
				"user_id":  userID,
			}).Debug("Skipping group member: user not present locally yet")
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO group_members (group_id, user_id, added_at, added_by)
			VALUES (?, ?, ?, ?)
		`, g.ID, userID, now, ""); err != nil {
			return fmt.Errorf("insert member %s: %w", userID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"group_id": g.ID,
		"members":  len(g.MemberIDs),
	}).Debug("Group upsert applied")
	return nil
}

// handleReceiveGroupDeleteSync applies an incoming group deletion tombstone.
// POST /api/internal/cluster/group-delete-sync
func (s *Server) handleReceiveGroupDeleteSync(w http.ResponseWriter, r *http.Request) {
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
		logrus.WithError(err).Error("Failed to decode group deletion data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if deleteData.ID == "" {
		http.Error(w, "Missing group ID", http.StatusBadRequest)
		return
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"group_id":       deleteData.ID,
	}).Info("Receiving group deletion from synchronization")

	if cluster.EntityIsNewerThanTombstone(ctx, s.db, cluster.EntityTypeGroup, deleteData.ID, deleteData.DeletedAt) {
		logrus.WithFields(logrus.Fields{
			"source_node_id": sourceNodeID,
			"group_id":       deleteData.ID,
		}).Info("Skipping group deletion: local entity was updated after tombstone (LWW)")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Skipped (entity is newer than tombstone)"})
		return
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("begin tx: %v", err), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM group_members WHERE group_id = ?`, deleteData.ID); err != nil {
		http.Error(w, fmt.Sprintf("delete members: %v", err), http.StatusInternalServerError)
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bucket_permissions WHERE group_id = ?`, deleteData.ID); err != nil {
		http.Error(w, fmt.Sprintf("delete bucket_permissions: %v", err), http.StatusInternalServerError)
		return
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, deleteData.ID)
	if err != nil {
		http.Error(w, fmt.Sprintf("delete group: %v", err), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, fmt.Sprintf("commit: %v", err), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()

	if err := cluster.RecordDeletion(ctx, s.db, cluster.EntityTypeGroup, deleteData.ID, sourceNodeID); err != nil {
		logrus.WithError(err).WithField("group_id", deleteData.ID).Warn("Failed to record group deletion tombstone")
	}

	logrus.WithFields(logrus.Fields{
		"group_id":      deleteData.ID,
		"rows_affected": rowsAffected,
	}).Info("Group deletion synchronized successfully")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Group deleted successfully",
	})
}
