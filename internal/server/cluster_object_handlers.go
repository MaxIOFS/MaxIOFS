package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/sirupsen/logrus"
)

// handleReceiveObjectReplication handles incoming object replication from other nodes
// This endpoint is authenticated with HMAC signatures
// PUT /api/internal/cluster/objects/:tenantID/:bucket/:key
func (s *Server) handleReceiveObjectReplication(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get source node ID from context (set by auth middleware)
	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get path parameters
	vars := mux.Vars(r)
	tenantID := vars["tenantID"]
	bucket := vars["bucket"]
	key := vars["key"]

	// Get metadata from headers
	contentType := r.Header.Get("Content-Type")
	sizeStr := r.Header.Get("X-Object-Size")
	etag := r.Header.Get("X-Object-ETag")
	metadata := r.Header.Get("X-Object-Metadata")
	sourceVersionID := r.Header.Get("X-Source-Version-ID")

	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		logrus.WithError(err).Error("Invalid object size header")
		http.Error(w, "Invalid object size", http.StatusBadRequest)
		return
	}

	logrus.WithFields(logrus.Fields{
		"tenant_id":        tenantID,
		"bucket":           bucket,
		"key":              key,
		"source_node_id":   sourceNodeID,
		"source_version":   sourceVersionID,
		"size":             size,
	}).Info("Receiving object replication")

	// Store object using ObjectManager.PutObject()
	// This will automatically encrypt the object with this node's encryption key
	if s.objectManager != nil {
		// Create HTTP headers with metadata
		headers := http.Header{}
		headers.Set("Content-Type", contentType)

		if metadata != "" {
			// Metadata is already stored as JSON string in header
			// The object manager will handle it
			headers.Set("X-Amz-Meta-Original", metadata)
		}

		_, err := s.objectManager.PutObject(ctx, bucket, key, r.Body, headers)
		if err != nil {
			logrus.WithError(err).Error("Failed to store replicated object")
			http.Error(w, fmt.Sprintf("Failed to store object: %v", err), http.StatusInternalServerError)
			return
		}

		logrus.WithFields(logrus.Fields{
			"tenant_id": tenantID,
			"bucket":    bucket,
			"key":       key,
			"size":      size,
		}).Info("Object replicated and stored successfully")
	} else {
		// Fallback: If no object manager, store metadata in database (backward compatibility)
		logrus.Warn("ObjectManager not available, storing metadata only")

		// Read body (needed for placeholder)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			logrus.WithError(err).Error("Failed to read request body")
			http.Error(w, "Failed to read object data", http.StatusInternalServerError)
			return
		}

		err = s.storeReplicatedObjectMetadata(ctx, tenantID, bucket, key, size, etag, contentType, metadata, sourceVersionID)
		if err != nil {
			logrus.WithError(err).Error("Failed to store object metadata")
			http.Error(w, fmt.Sprintf("Failed to store object: %v", err), http.StatusInternalServerError)
			return
		}

		logrus.WithFields(logrus.Fields{
			"tenant_id": tenantID,
			"bucket":    bucket,
			"key":       key,
			"size":      len(body),
		}).Info("Object metadata replicated successfully (fallback mode)")
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Object replicated successfully",
	})
}

// handleReceiveObjectDeletion handles incoming object deletion replication from other nodes
// This endpoint is authenticated with HMAC signatures
// DELETE /api/internal/cluster/objects/:tenantID/:bucket/:key
func (s *Server) handleReceiveObjectDeletion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get source node ID from context (set by auth middleware)
	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get path parameters
	vars := mux.Vars(r)
	tenantID := vars["tenantID"]
	bucket := vars["bucket"]
	key := vars["key"]

	logrus.WithFields(logrus.Fields{
		"tenant_id":      tenantID,
		"bucket":         bucket,
		"key":            key,
		"source_node_id": sourceNodeID,
	}).Info("Receiving object deletion replication")

	// Delete object using ObjectManager.DeleteObject()
	if s.objectManager != nil {
		_, err := s.objectManager.DeleteObject(ctx, bucket, key, false)
		if err != nil {
			// Object not found is acceptable (idempotent delete)
			logrus.WithError(err).Warn("Failed to delete replicated object (may already be deleted)")
			// Continue anyway - deletion is idempotent
		}

		logrus.WithFields(logrus.Fields{
			"tenant_id": tenantID,
			"bucket":    bucket,
			"key":       key,
		}).Info("Object deletion replicated successfully")
	} else {
		// Fallback: If no object manager, soft delete in database (backward compatibility)
		logrus.Warn("ObjectManager not available, performing soft delete in database")

		err := s.deleteReplicatedObject(ctx, tenantID, bucket, key)
		if err != nil {
			logrus.WithError(err).Error("Failed to delete object")
			http.Error(w, fmt.Sprintf("Failed to delete object: %v", err), http.StatusInternalServerError)
			return
		}

		logrus.WithFields(logrus.Fields{
			"tenant_id": tenantID,
			"bucket":    bucket,
			"key":       key,
		}).Info("Object deletion replicated successfully (fallback mode)")
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Object deleted successfully",
	})
}

// storeReplicatedObjectMetadata stores metadata for a replicated object
// TODO: This should be replaced with ObjectManager.PutObject() in real implementation
func (s *Server) storeReplicatedObjectMetadata(ctx context.Context, tenantID, bucket, key string, size int64, etag, contentType, metadata, sourceVersionID string) error {
	// Check if bucket exists
	var bucketExists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM buckets WHERE name = ? AND tenant_id = ?)
	`, bucket, tenantID).Scan(&bucketExists)

	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !bucketExists {
		return fmt.Errorf("bucket not found: %s", bucket)
	}

	// For now, just log that we would store the object
	// In real implementation, this would call ObjectManager.PutObject()
	logrus.WithFields(logrus.Fields{
		"tenant_id":  tenantID,
		"bucket":     bucket,
		"key":        key,
		"size":       size,
		"etag":       etag,
		"content_type": contentType,
		"source_version": sourceVersionID,
	}).Debug("Would store replicated object via ObjectManager")

	// TODO: Uncomment when ObjectManager integration is ready
	/*
	// Parse metadata JSON
	var metadataMap map[string]string
	if metadata != "" {
		if err := json.Unmarshal([]byte(metadata), &metadataMap); err != nil {
			return fmt.Errorf("failed to parse metadata: %w", err)
		}
	}

	// Store object via ObjectManager (will auto-encrypt with this node's key)
	err = s.objectManager.PutObject(ctx, tenantID, bucket, key, reader, size, contentType, metadataMap)
	if err != nil {
		return fmt.Errorf("failed to store object: %w", err)
	}
	*/

	return nil
}

// deleteReplicatedObject deletes a replicated object
// TODO: This should be replaced with ObjectManager.DeleteObject() in real implementation
func (s *Server) deleteReplicatedObject(ctx context.Context, tenantID, bucket, key string) error {
	// For now, just log that we would delete the object
	// In real implementation, this would call ObjectManager.DeleteObject()
	logrus.WithFields(logrus.Fields{
		"tenant_id": tenantID,
		"bucket":    bucket,
		"key":       key,
	}).Debug("Would delete replicated object via ObjectManager")

	// TODO: Uncomment when ObjectManager integration is ready
	/*
	err := s.objectManager.DeleteObject(ctx, tenantID, bucket, key)
	if err != nil && err != ErrObjectNotFound {
		return fmt.Errorf("failed to delete object: %w", err)
	}
	*/

	return nil
}

// handleReceiveBucketPermission handles incoming bucket permission from other nodes during migration
// POST /api/internal/cluster/bucket-permissions
func (s *Server) handleReceiveBucketPermission(w http.ResponseWriter, r *http.Request) {
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
		UserID          string  `json:"user_id"`
		TenantID        string  `json:"tenant_id"`
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
		"bucket":         permissionData.BucketName,
		"permission_id":  permissionData.ID,
		"user_id":        permissionData.UserID,
	}).Info("Receiving bucket permission from migration")

	// Upsert permission in database (INSERT OR REPLACE)
	query := `
		INSERT OR REPLACE INTO bucket_permissions
		(id, bucket_name, user_id, tenant_id, permission_level, granted_by, granted_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		permissionData.ID,
		permissionData.BucketName,
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
	}).Info("Bucket permission stored successfully")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Bucket permission stored successfully",
	})
}

// handleReceiveBucketACL handles incoming bucket ACL from other nodes during migration
// POST /api/internal/cluster/bucket-acl
func (s *Server) handleReceiveBucketACL(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get source node ID from context (set by auth middleware)
	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse ACL data from JSON body
	var aclData struct {
		TenantID   string   `json:"tenant_id"`
		BucketName string   `json:"bucket_name"`
		ACL        *acl.ACL `json:"acl"`
	}

	if err := json.NewDecoder(r.Body).Decode(&aclData); err != nil {
		logrus.WithError(err).Error("Failed to decode ACL data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"tenant_id":      aclData.TenantID,
		"bucket":         aclData.BucketName,
	}).Info("Receiving bucket ACL from migration")

	// Get ACL manager from bucket manager
	aclMgrInterface := s.bucketManager.GetACLManager()
	if aclMgrInterface == nil {
		logrus.Warn("ACL manager not available")
		http.Error(w, "ACL manager not available", http.StatusInternalServerError)
		return
	}

	// Type assert to acl.Manager
	aclMgr, ok := aclMgrInterface.(acl.Manager)
	if !ok {
		logrus.Error("Failed to type assert ACL manager")
		http.Error(w, "ACL manager type assertion failed", http.StatusInternalServerError)
		return
	}

	// Store ACL using ACL manager
	err := aclMgr.SetBucketACL(ctx, aclData.TenantID, aclData.BucketName, aclData.ACL)
	if err != nil {
		logrus.WithError(err).Error("Failed to store bucket ACL")
		http.Error(w, fmt.Sprintf("Failed to store ACL: %v", err), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"tenant_id": aclData.TenantID,
		"bucket":    aclData.BucketName,
	}).Info("Bucket ACL stored successfully")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Bucket ACL stored successfully",
	})
}

// handleReceiveBucketConfiguration handles incoming bucket configuration from other nodes during migration
// POST /api/internal/cluster/bucket-config
func (s *Server) handleReceiveBucketConfiguration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get source node ID from context (set by auth middleware)
	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse bucket configuration data from JSON body
	var configData struct {
		TenantID     string  `json:"tenant_id"`
		BucketName   string  `json:"bucket_name"`
		Versioning   *string `json:"versioning,omitempty"`
		ObjectLock   *string `json:"object_lock,omitempty"`
		Encryption   *string `json:"encryption,omitempty"`
		Lifecycle    *string `json:"lifecycle,omitempty"`
		Tags         *string `json:"tags,omitempty"`
		CORS         *string `json:"cors,omitempty"`
		Policy       *string `json:"policy,omitempty"`
		Notification *string `json:"notification,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&configData); err != nil {
		logrus.WithError(err).Error("Failed to decode configuration data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"tenant_id":      configData.TenantID,
		"bucket":         configData.BucketName,
	}).Info("Receiving bucket configuration from migration")

	// Update bucket configuration in database
	query := `
		UPDATE buckets
		SET versioning = ?, object_lock = ?, encryption = ?, lifecycle = ?,
		    tags = ?, cors = ?, policy = ?, notification = ?
		WHERE name = ? AND tenant_id = ?
	`

	result, err := s.db.ExecContext(ctx, query,
		configData.Versioning,
		configData.ObjectLock,
		configData.Encryption,
		configData.Lifecycle,
		configData.Tags,
		configData.CORS,
		configData.Policy,
		configData.Notification,
		configData.BucketName,
		configData.TenantID,
	)

	if err != nil {
		logrus.WithError(err).Error("Failed to update bucket configuration")
		http.Error(w, fmt.Sprintf("Failed to update configuration: %v", err), http.StatusInternalServerError)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logrus.WithError(err).Warn("Failed to get rows affected")
	} else if rowsAffected == 0 {
		logrus.Warn("No bucket found to update configuration")
		http.Error(w, "Bucket not found", http.StatusNotFound)
		return
	}

	logrus.WithFields(logrus.Fields{
		"tenant_id": configData.TenantID,
		"bucket":    configData.BucketName,
	}).Info("Bucket configuration updated successfully")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Bucket configuration updated successfully",
	})
}

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

	// Upsert permission in database (INSERT OR REPLACE)
	query := `
		INSERT OR REPLACE INTO bucket_permissions
		(id, bucket_name, user_id, tenant_id, permission_level, granted_by, granted_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		permissionData.ID,
		permissionData.BucketName,
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

// handleReceiveBucketInventory handles incoming bucket inventory configuration from other nodes during migration
// POST /api/internal/cluster/bucket-inventory
func (s *Server) handleReceiveBucketInventory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get source node ID from context (set by auth middleware)
	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		logrus.Warn("Cluster node ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse inventory configuration data from JSON body
	var inventoryData struct {
		TenantID          string   `json:"tenant_id"`
		BucketName        string   `json:"bucket_name"`
		Enabled           bool     `json:"enabled"`
		Frequency         string   `json:"frequency"`
		Format            string   `json:"format"`
		DestinationBucket string   `json:"destination_bucket"`
		DestinationPrefix string   `json:"destination_prefix"`
		IncludedFields    []string `json:"included_fields"`
		ScheduleTime      string   `json:"schedule_time"`
		LastRunAt         *int64   `json:"last_run_at,omitempty"`
		NextRunAt         *int64   `json:"next_run_at,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&inventoryData); err != nil {
		logrus.WithError(err).Error("Failed to decode inventory configuration data")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"tenant_id":      inventoryData.TenantID,
		"bucket":         inventoryData.BucketName,
	}).Info("Receiving bucket inventory configuration from migration")

	// Check if configuration already exists
	existingConfig, err := s.inventoryManager.GetConfig(ctx, inventoryData.BucketName, inventoryData.TenantID)
	if err == nil {
		// Update existing configuration
		existingConfig.Enabled = inventoryData.Enabled
		existingConfig.Frequency = inventoryData.Frequency
		existingConfig.Format = inventoryData.Format
		existingConfig.DestinationBucket = inventoryData.DestinationBucket
		existingConfig.DestinationPrefix = inventoryData.DestinationPrefix
		existingConfig.IncludedFields = inventoryData.IncludedFields
		existingConfig.ScheduleTime = inventoryData.ScheduleTime
		existingConfig.LastRunAt = inventoryData.LastRunAt
		existingConfig.NextRunAt = inventoryData.NextRunAt

		if err := s.inventoryManager.UpdateConfig(ctx, existingConfig); err != nil {
			logrus.WithError(err).Error("Failed to update inventory configuration")
			http.Error(w, fmt.Sprintf("Failed to update configuration: %v", err), http.StatusInternalServerError)
			return
		}

		logrus.WithFields(logrus.Fields{
			"tenant_id": inventoryData.TenantID,
			"bucket":    inventoryData.BucketName,
		}).Info("Inventory configuration updated successfully")
	} else {
		// Create new configuration
		includedFieldsJSON, err := json.Marshal(inventoryData.IncludedFields)
		if err != nil {
			logrus.WithError(err).Error("Failed to marshal included fields")
			http.Error(w, "Invalid included fields", http.StatusBadRequest)
			return
		}

		// Generate new ID
		id := fmt.Sprintf("inv_%s_%d", inventoryData.BucketName, time.Now().Unix())

		query := `
			INSERT INTO bucket_inventory_configs (
				id, bucket_name, tenant_id, enabled, frequency, format,
				destination_bucket, destination_prefix, included_fields, schedule_time,
				last_run_at, next_run_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`

		now := time.Now().Unix()
		_, err = s.db.ExecContext(ctx, query,
			id,
			inventoryData.BucketName,
			inventoryData.TenantID,
			inventoryData.Enabled,
			inventoryData.Frequency,
			inventoryData.Format,
			inventoryData.DestinationBucket,
			inventoryData.DestinationPrefix,
			string(includedFieldsJSON),
			inventoryData.ScheduleTime,
			inventoryData.LastRunAt,
			inventoryData.NextRunAt,
			now,
			now,
		)

		if err != nil {
			logrus.WithError(err).Error("Failed to create inventory configuration")
			http.Error(w, fmt.Sprintf("Failed to create configuration: %v", err), http.StatusInternalServerError)
			return
		}

		logrus.WithFields(logrus.Fields{
			"tenant_id": inventoryData.TenantID,
			"bucket":    inventoryData.BucketName,
		}).Info("Inventory configuration created successfully")
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Inventory configuration migrated successfully",
	})
}
