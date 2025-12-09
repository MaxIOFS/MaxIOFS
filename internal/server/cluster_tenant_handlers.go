package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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

	now := time.Now()

	if exists {
		// Update existing tenant
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
			now,
			tenant.ID,
		)
		if err != nil {
			return fmt.Errorf("failed to update tenant: %w", err)
		}

		logrus.WithField("tenant_id", tenant.ID).Debug("Updated existing tenant")
	} else {
		// Insert new tenant (preserve original created_at from source)
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
			now,
		)
		if err != nil {
			// Check if it's a unique constraint violation (race condition)
			if err == sql.ErrNoRows || (err != nil && err.Error() == "UNIQUE constraint failed: tenants.id") {
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
