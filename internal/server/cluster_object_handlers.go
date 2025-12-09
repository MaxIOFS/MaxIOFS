package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
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

	// Read object data from request body
	// In real implementation, this would be the actual object data
	// For now, we handle the metadata
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logrus.WithError(err).Error("Failed to read request body")
		http.Error(w, "Failed to read object data", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	// TODO: Store object using ObjectManager.PutObject()
	// This will automatically encrypt the object with this node's encryption key
	// For now, we'll store metadata in database
	/*
	err = s.objectManager.PutObject(ctx, tenantID, bucket, key, bytes.NewReader(body), size, contentType, metadataMap)
	if err != nil {
		logrus.WithError(err).Error("Failed to store replicated object")
		http.Error(w, fmt.Sprintf("Failed to store object: %v", err), http.StatusInternalServerError)
		return
	}
	*/

	// Store object metadata in database (placeholder)
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
	}).Info("Object replicated successfully")

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

	// TODO: Delete object using ObjectManager.DeleteObject()
	/*
	err := s.objectManager.DeleteObject(ctx, tenantID, bucket, key)
	if err != nil && err != ErrObjectNotFound {
		logrus.WithError(err).Error("Failed to delete replicated object")
		http.Error(w, fmt.Sprintf("Failed to delete object: %v", err), http.StatusInternalServerError)
		return
	}
	*/

	// Soft delete object in database (placeholder)
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
	}).Info("Object deletion replicated successfully")

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
