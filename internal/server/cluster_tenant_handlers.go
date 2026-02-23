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

// handleReceiveTenantSync handles incoming tenant synchronization requests from other nodes
// This endpoint is authenticated with HMAC signatures
func (s *Server) handleReceiveTenantSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get source node ID from context (set by auth middleware)
	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse tenant data from request body
	var tenantData struct {
		ID                  string            `json:"id"`
		Name                string            `json:"name"`
		DisplayName         string            `json:"display_name"`
		Description         string            `json:"description"`
		Status              string            `json:"status"`
		MaxAccessKeys       int               `json:"max_access_keys"`
		MaxStorageBytes     int64             `json:"max_storage_bytes"`
		CurrentStorageBytes int64             `json:"current_storage_bytes"`
		MaxBuckets          int               `json:"max_buckets"`
		CurrentBuckets      int               `json:"current_buckets"`
		Metadata            map[string]string `json:"metadata"`
		CreatedAt           time.Time         `json:"created_at"`
		UpdatedAt           time.Time         `json:"updated_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&tenantData); err != nil {
		logrus.WithError(err).Error("Failed to decode tenant data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	logrus.WithFields(logrus.Fields{
		"tenant_id":      tenantData.ID,
		"tenant_name":    tenantData.Name,
		"source_node_id": sourceNodeID,
	}).Info("Receiving tenant synchronization")

	// Skip if this entity has been deleted (tombstone exists)
	if hasDeletion, _ := cluster.HasDeletion(ctx, s.db, cluster.EntityTypeTenant, tenantData.ID); hasDeletion {
		logrus.WithField("tenant_id", tenantData.ID).Debug("Skipping sync for deleted tenant (tombstone exists)")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Skipped (deleted)"})
		return
	}

	// Upsert tenant in local database
	if err := s.upsertTenant(ctx, &tenantData); err != nil {
		logrus.WithError(err).WithField("tenant_id", tenantData.ID).Error("Failed to upsert tenant")
		http.Error(w, fmt.Sprintf("Failed to sync tenant: %v", err), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"tenant_id":   tenantData.ID,
		"tenant_name": tenantData.Name,
	}).Info("Tenant synchronized successfully")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Tenant synchronized successfully",
	})
}

// upsertTenant creates or updates a tenant in the local database
func (s *Server) upsertTenant(ctx context.Context, tenant *struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	DisplayName         string            `json:"display_name"`
	Description         string            `json:"description"`
	Status              string            `json:"status"`
	MaxAccessKeys       int               `json:"max_access_keys"`
	MaxStorageBytes     int64             `json:"max_storage_bytes"`
	CurrentStorageBytes int64             `json:"current_storage_bytes"`
	MaxBuckets          int               `json:"max_buckets"`
	CurrentBuckets      int               `json:"current_buckets"`
	Metadata            map[string]string `json:"metadata"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}) error {
	// Marshal metadata to JSON
	metadataJSON, err := json.Marshal(tenant.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Check if tenant exists
	var exists bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE id = ?)`, tenant.ID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check tenant existence: %w", err)
	}

	if exists {
		// LWW: only apply if the incoming record is strictly newer than the local one.
		var localUpdatedAt time.Time
		if err := s.db.QueryRowContext(ctx, `SELECT updated_at FROM tenants WHERE id = ?`, tenant.ID).Scan(&localUpdatedAt); err != nil {
			return fmt.Errorf("failed to read local tenant updated_at: %w", err)
		}
		if !tenant.UpdatedAt.After(localUpdatedAt) {
			logrus.WithField("tenant_id", tenant.ID).Debug("Skipping tenant update: local record is newer or equal (LWW)")
			return nil
		}

		// Update existing tenant — preserve source updated_at so all nodes agree on the timestamp.
		_, err = s.db.ExecContext(ctx, `
			UPDATE tenants SET
				name = ?,
				display_name = ?,
				description = ?,
				status = ?,
				max_access_keys = ?,
				max_storage_bytes = ?,
				current_storage_bytes = ?,
				max_buckets = ?,
				current_buckets = ?,
				metadata = ?,
				updated_at = ?
			WHERE id = ?
		`,
			tenant.Name,
			tenant.DisplayName,
			tenant.Description,
			tenant.Status,
			tenant.MaxAccessKeys,
			tenant.MaxStorageBytes,
			tenant.CurrentStorageBytes,
			tenant.MaxBuckets,
			tenant.CurrentBuckets,
			string(metadataJSON),
			tenant.UpdatedAt,
			tenant.ID,
		)
		if err != nil {
			return fmt.Errorf("failed to update tenant: %w", err)
		}

		logrus.WithField("tenant_id", tenant.ID).Debug("Updated existing tenant")
	} else {
		// Insert new tenant (preserve original created_at and updated_at from source)
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO tenants (
				id, name, display_name, description, status,
				max_access_keys, max_storage_bytes, current_storage_bytes,
				max_buckets, current_buckets, metadata, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			tenant.ID,
			tenant.Name,
			tenant.DisplayName,
			tenant.Description,
			tenant.Status,
			tenant.MaxAccessKeys,
			tenant.MaxStorageBytes,
			tenant.CurrentStorageBytes,
			tenant.MaxBuckets,
			tenant.CurrentBuckets,
			string(metadataJSON),
			tenant.CreatedAt,
			tenant.UpdatedAt,
		)
		if err != nil {
			// Check if it's a unique constraint violation (race condition)
			if err == sql.ErrNoRows || err.Error() == "UNIQUE constraint failed: tenants.id" {
				// Try update instead
				logrus.WithField("tenant_id", tenant.ID).Debug("Tenant created concurrently, updating instead")
				return s.upsertTenant(ctx, tenant) // Retry as update
			}
			return fmt.Errorf("failed to insert tenant: %w", err)
		}

		logrus.WithField("tenant_id", tenant.ID).Debug("Inserted new tenant")
	}

	return nil
}

// handleReceiveUserSync handles incoming user synchronization requests from other nodes
// This endpoint is authenticated with HMAC signatures
func (s *Server) handleReceiveUserSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get source node ID from context (set by auth middleware)
	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse user data from request body
	var userData struct {
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

	if err := json.NewDecoder(r.Body).Decode(&userData); err != nil {
		logrus.WithError(err).Error("Failed to decode user data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	logrus.WithFields(logrus.Fields{
		"user_id":        userData.ID,
		"username":       userData.Username,
		"source_node_id": sourceNodeID,
	}).Info("Receiving user synchronization")

	// Skip if this entity has been deleted (tombstone exists)
	if hasDeletion, _ := cluster.HasDeletion(ctx, s.db, cluster.EntityTypeUser, userData.ID); hasDeletion {
		logrus.WithField("user_id", userData.ID).Debug("Skipping sync for deleted user (tombstone exists)")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Skipped (deleted)"})
		return
	}

	// Upsert user in local database
	if err := s.upsertUser(ctx, &userData); err != nil {
		logrus.WithError(err).WithField("user_id", userData.ID).Error("Failed to upsert user")
		http.Error(w, fmt.Sprintf("Failed to sync user: %v", err), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"user_id":  userData.ID,
		"username": userData.Username,
	}).Info("User synchronized successfully")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "User synchronized successfully",
	})
}

// upsertUser creates or updates a user in the local database
func (s *Server) upsertUser(ctx context.Context, user *struct {
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
}) error {
	// Handle NULL values for tenant_id
	var tenantID sql.NullString
	if user.TenantID != "" {
		tenantID = sql.NullString{String: user.TenantID, Valid: true}
	}

	// Check if user exists
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)`, user.ID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check user existence: %w", err)
	}

	if exists {
		// LWW: only apply if the incoming record is strictly newer than the local one.
		var localUpdatedAt int64
		if err := s.db.QueryRowContext(ctx, `SELECT updated_at FROM users WHERE id = ?`, user.ID).Scan(&localUpdatedAt); err != nil {
			return fmt.Errorf("failed to read local user updated_at: %w", err)
		}
		if user.UpdatedAt <= localUpdatedAt {
			logrus.WithField("user_id", user.ID).Debug("Skipping user update: local record is newer or equal (LWW)")
			return nil
		}

		// Update existing user — preserve source updated_at so all nodes agree on the timestamp.
		_, err = s.db.ExecContext(ctx, `
			UPDATE users SET
				username = ?,
				password_hash = ?,
				display_name = ?,
				email = ?,
				status = ?,
				tenant_id = ?,
				roles = ?,
				policies = ?,
				metadata = ?,
				failed_login_attempts = ?,
				locked_until = ?,
				last_failed_login = ?,
				theme_preference = ?,
				language_preference = ?,
				auth_provider = ?,
				external_id = ?,
				updated_at = ?
			WHERE id = ?
		`,
			user.Username,
			user.PasswordHash,
			user.DisplayName,
			user.Email,
			user.Status,
			tenantID,
			user.Roles,
			user.Policies,
			user.Metadata,
			user.FailedLoginAttempts,
			user.LockedUntil,
			user.LastFailedLogin,
			user.ThemePreference,
			user.LanguagePreference,
			user.AuthProvider,
			user.ExternalID,
			user.UpdatedAt,
			user.ID,
		)
		if err != nil {
			return fmt.Errorf("failed to update user: %w", err)
		}

		logrus.WithField("user_id", user.ID).Debug("Updated existing user")
	} else {
		// Insert new user (preserve original created_at and updated_at from source)
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO users (
				id, username, password_hash, display_name, email, status, tenant_id,
				roles, policies, metadata, failed_login_attempts, locked_until,
				last_failed_login, theme_preference, language_preference,
				auth_provider, external_id, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			user.ID,
			user.Username,
			user.PasswordHash,
			user.DisplayName,
			user.Email,
			user.Status,
			tenantID,
			user.Roles,
			user.Policies,
			user.Metadata,
			user.FailedLoginAttempts,
			user.LockedUntil,
			user.LastFailedLogin,
			user.ThemePreference,
			user.LanguagePreference,
			user.AuthProvider,
			user.ExternalID,
			user.CreatedAt,
			user.UpdatedAt,
		)
		if err != nil {
			// Check if it's a unique constraint violation (race condition)
			if err == sql.ErrNoRows || err.Error() == "UNIQUE constraint failed: users.id" {
				// Try update instead
				logrus.WithField("user_id", user.ID).Debug("User created concurrently, updating instead")
				return s.upsertUser(ctx, user) // Retry as update
			}
			return fmt.Errorf("failed to insert user: %w", err)
		}

		logrus.WithField("user_id", user.ID).Debug("Inserted new user")
	}

	return nil
}

// handleReceiveTenantDeleteSync handles incoming tenant deletion from cluster sync
// POST /api/internal/cluster/tenant-delete-sync
func (s *Server) handleReceiveTenantDeleteSync(w http.ResponseWriter, r *http.Request) {
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
		logrus.WithError(err).Error("Failed to decode tenant deletion data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if deleteData.ID == "" {
		http.Error(w, "Missing tenant ID", http.StatusBadRequest)
		return
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"tenant_id":      deleteData.ID,
	}).Info("Receiving tenant deletion from synchronization")

	// Phase 4: Tombstone vs entity LWW.
	// If the local tenant was updated after the tombstone's deleted_at, the entity wins.
	if cluster.EntityIsNewerThanTombstone(ctx, s.db, cluster.EntityTypeTenant, deleteData.ID, deleteData.DeletedAt) {
		logrus.WithFields(logrus.Fields{
			"source_node_id": sourceNodeID,
			"tenant_id":      deleteData.ID,
		}).Info("Skipping tenant deletion: local entity was updated after tombstone (LWW)")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Skipped (entity is newer than tombstone)"})
		return
	}

	// Delete users belonging to this tenant first (cascade)
	_, err := s.db.ExecContext(ctx, `DELETE FROM access_keys WHERE user_id IN (SELECT id FROM users WHERE tenant_id = ?)`, deleteData.ID)
	if err != nil {
		logrus.WithError(err).Error("Failed to delete access keys for tenant users")
		http.Error(w, fmt.Sprintf("Failed to delete access keys: %v", err), http.StatusInternalServerError)
		return
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM users WHERE tenant_id = ?`, deleteData.ID)
	if err != nil {
		logrus.WithError(err).Error("Failed to delete users for tenant")
		http.Error(w, fmt.Sprintf("Failed to delete users: %v", err), http.StatusInternalServerError)
		return
	}

	// Delete the tenant
	result, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id = ?`, deleteData.ID)
	if err != nil {
		logrus.WithError(err).Error("Failed to delete tenant")
		http.Error(w, fmt.Sprintf("Failed to delete tenant: %v", err), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()

	// Record tombstone locally so this node doesn't re-sync the item
	if err := cluster.RecordDeletion(ctx, s.db, cluster.EntityTypeTenant, deleteData.ID, sourceNodeID); err != nil {
		logrus.WithError(err).WithField("tenant_id", deleteData.ID).Warn("Failed to record tenant deletion tombstone")
	}

	logrus.WithFields(logrus.Fields{
		"tenant_id":     deleteData.ID,
		"rows_affected": rowsAffected,
	}).Info("Tenant deletion synchronized successfully")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Tenant deleted successfully",
	})
}

// handleReceiveUserDeleteSync handles incoming user deletion from cluster sync
// POST /api/internal/cluster/user-delete-sync
func (s *Server) handleReceiveUserDeleteSync(w http.ResponseWriter, r *http.Request) {
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
		logrus.WithError(err).Error("Failed to decode user deletion data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if deleteData.ID == "" {
		http.Error(w, "Missing user ID", http.StatusBadRequest)
		return
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"user_id":        deleteData.ID,
	}).Info("Receiving user deletion from synchronization")

	// Phase 4: Tombstone vs entity LWW.
	if cluster.EntityIsNewerThanTombstone(ctx, s.db, cluster.EntityTypeUser, deleteData.ID, deleteData.DeletedAt) {
		logrus.WithFields(logrus.Fields{
			"source_node_id": sourceNodeID,
			"user_id":        deleteData.ID,
		}).Info("Skipping user deletion: local entity was updated after tombstone (LWW)")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Skipped (entity is newer than tombstone)"})
		return
	}

	// Delete access keys for this user first (cascade)
	_, err := s.db.ExecContext(ctx, `DELETE FROM access_keys WHERE user_id = ?`, deleteData.ID)
	if err != nil {
		logrus.WithError(err).Error("Failed to delete access keys for user")
		http.Error(w, fmt.Sprintf("Failed to delete access keys: %v", err), http.StatusInternalServerError)
		return
	}

	// Delete the user
	result, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, deleteData.ID)
	if err != nil {
		logrus.WithError(err).Error("Failed to delete user")
		http.Error(w, fmt.Sprintf("Failed to delete user: %v", err), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()

	// Record tombstone locally so this node doesn't re-sync the item
	if err := cluster.RecordDeletion(ctx, s.db, cluster.EntityTypeUser, deleteData.ID, sourceNodeID); err != nil {
		logrus.WithError(err).WithField("user_id", deleteData.ID).Warn("Failed to record user deletion tombstone")
	}

	logrus.WithFields(logrus.Fields{
		"user_id":       deleteData.ID,
		"rows_affected": rowsAffected,
	}).Info("User deletion synchronized successfully")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "User deleted successfully",
	})
}
