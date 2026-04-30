package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// ── Rename ────────────────────────────────────────────────────────────────────

// handleRenameObject implements POST /buckets/{bucket}/objects/{object:.*}/rename
// Body: { "newKey": "path/to/new-name.txt" }
//
// Rename is implemented as: copy the object data + metadata to the new key,
// then delete the original.  Tags are also copied (best-effort).
// Renaming is blocked for objects under COMPLIANCE retention or active Legal Hold.
func (s *Server) handleRenameObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.requireCapability(w, r, auth.CapObjectUpload, "You do not have permission to upload objects") {
		return
	}
	if !s.requireCapability(w, r, auth.CapObjectDelete, "You do not have permission to delete objects") {
		return
	}

	var req struct {
		NewKey string `json:"newKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.NewKey) == "" {
		s.writeError(w, "Invalid request: newKey is required", http.StatusBadRequest)
		return
	}
	req.NewKey = strings.TrimSpace(req.NewKey)
	if req.NewKey == objectKey {
		s.writeError(w, "New key is the same as the current key", http.StatusBadRequest)
		return
	}

	tenantID := user.TenantID
	if q := r.URL.Query().Get("tenantId"); q != "" && auth.IsAdminUser(r.Context()) && user.TenantID == "" {
		tenantID = q
	}
	bucketPath := bucketName
	if tenantID != "" {
		bucketPath = tenantID + "/" + bucketName
	}

	// 1. Fetch source metadata + open data stream
	srcObj, reader, err := s.objectManager.GetObject(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			s.writeError(w, "Object not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	defer reader.Close()

	// 2. Block rename if the object is protected
	if srcObj.LegalHold != nil && srcObj.LegalHold.Status == "ON" {
		s.writeError(w, "Cannot rename: object has an active Legal Hold", http.StatusForbidden)
		return
	}
	if srcObj.Retention != nil && srcObj.Retention.Mode == "COMPLIANCE" && srcObj.Retention.RetainUntilDate.After(time.Now()) {
		s.writeError(w, "Cannot rename: object is under COMPLIANCE retention that has not expired", http.StatusForbidden)
		return
	}

	// 3. Reconstruct the original HTTP headers so PutObject preserves all metadata
	headers := make(http.Header)
	ct := srcObj.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	headers.Set("Content-Type", ct)
	headers.Set("Content-Length", strconv.FormatInt(srcObj.Size, 10))
	if srcObj.ContentDisposition != "" {
		headers.Set("Content-Disposition", srcObj.ContentDisposition)
	}
	if srcObj.ContentEncoding != "" {
		headers.Set("Content-Encoding", srcObj.ContentEncoding)
	}
	if srcObj.CacheControl != "" {
		headers.Set("Cache-Control", srcObj.CacheControl)
	}
	if srcObj.ContentLanguage != "" {
		headers.Set("Content-Language", srcObj.ContentLanguage)
	}
	for k, v := range srcObj.Metadata {
		headers.Set("X-Amz-Meta-"+k, v)
	}

	// 4. Write to the new key
	if _, err = s.objectManager.PutObject(r.Context(), bucketPath, req.NewKey, reader, headers); err != nil {
		s.writeError(w, fmt.Sprintf("Failed to write object at new key: %v", err), http.StatusInternalServerError)
		return
	}

	// 5. Copy tags (best-effort; failure does not abort the rename)
	if tags, tagErr := s.objectManager.GetObjectTagging(r.Context(), bucketPath, objectKey); tagErr == nil && tags != nil && len(tags.Tags) > 0 {
		_ = s.objectManager.SetObjectTagging(r.Context(), bucketPath, req.NewKey, tags)
	}

	// 6. Delete the source key
	if _, err = s.objectManager.DeleteObject(r.Context(), bucketPath, objectKey, false); err != nil {
		// Data is already at the new key; log but do not return an error to the client
		logrus.WithError(err).WithField("key", objectKey).Warn("rename: failed to delete source object after successful copy")
	}

	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     tenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeObjectUploaded,
		ResourceType: audit.ResourceTypeObject,
		ResourceID:   req.NewKey,
		ResourceName: req.NewKey,
		Action:       audit.ActionUpdate,
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
		UserAgent:    r.Header.Get("User-Agent"),
		Details: map[string]interface{}{
			"bucket":  bucketName,
			"old_key": objectKey,
			"new_key": req.NewKey,
		},
	})

	s.writeJSON(w, map[string]string{"newKey": req.NewKey})
}

// ── Object Tags ───────────────────────────────────────────────────────────────

// handleGetObjectTags implements GET /buckets/{bucket}/objects/{object:.*}/tags
func (s *Server) handleGetObjectTags(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	_, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tenantID := s.resolveTenantID(r)
	bucketPath := buildBucketPath(tenantID, bucketName)

	tags, err := s.objectManager.GetObjectTagging(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			s.writeError(w, "Object not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if tags == nil {
		tags = &object.TagSet{Tags: []object.Tag{}}
	}
	s.writeJSON(w, tags)
}

// handleSetObjectTags implements PUT /buckets/{bucket}/objects/{object:.*}/tags
// Body: { "tags": [{ "key": "env", "value": "prod" }] }
func (s *Server) handleSetObjectTags(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.requireCapability(w, r, auth.CapObjectManageTags, "You do not have permission to manage object tags") {
		return
	}

	var tags object.TagSet
	if err := json.NewDecoder(r.Body).Decode(&tags); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tenantID := user.TenantID
	if q := r.URL.Query().Get("tenantId"); q != "" && auth.IsAdminUser(r.Context()) && user.TenantID == "" {
		tenantID = q
	}
	bucketPath := buildBucketPath(tenantID, bucketName)

	if err := s.objectManager.SetObjectTagging(r.Context(), bucketPath, objectKey, &tags); err != nil {
		if err == object.ErrObjectNotFound {
			s.writeError(w, "Object not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     tenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeObjectUploaded,
		ResourceType: audit.ResourceTypeObject,
		ResourceID:   objectKey,
		ResourceName: objectKey,
		Action:       audit.ActionUpdate,
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
		UserAgent:    r.Header.Get("User-Agent"),
		Details: map[string]interface{}{
			"bucket":    bucketName,
			"tag_count": len(tags.Tags),
		},
	})

	s.writeJSON(w, nil)
}

// ── Folder Size ───────────────────────────────────────────────────────────────

// handleFolderSize implements GET /buckets/{bucket}/folder-size?prefix=folder/
// Returns the total size and object count for all objects under the given prefix.
func (s *Server) handleFolderSize(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	prefix := r.URL.Query().Get("prefix")

	_, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	tenantID := s.resolveTenantID(r)
	bucketPath := buildBucketPath(tenantID, bucketName)

	var totalSize int64
	var totalCount int64
	marker := ""

	for {
		result, err := s.objectManager.ListObjects(r.Context(), bucketPath, prefix, "", marker, 1000)
		if err != nil {
			s.writeError(w, fmt.Sprintf("Failed to list objects: %v", err), http.StatusInternalServerError)
			return
		}
		for _, obj := range result.Objects {
			if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
				continue
			}
			totalSize += obj.Size
			totalCount++
		}
		if !result.IsTruncated {
			break
		}
		marker = result.NextMarker
	}

	s.writeJSON(w, map[string]int64{
		"size":  totalSize,
		"count": totalCount,
	})
}

// ── Bucket Versions ──────────────────────────────────────────────────────────

// handleListBucketVersions implements GET /buckets/{bucket}/versions?prefix=&maxKeys=
// Returns all object versions and delete markers for the bucket (or a prefix within it).
func (s *Server) handleListBucketVersions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	_, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	tenantID := s.resolveTenantID(r)
	bucketPath := buildBucketPath(tenantID, bucketName)

	prefix := r.URL.Query().Get("prefix")
	maxKeys := 5000
	if v := r.URL.Query().Get("maxKeys"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxKeys = n
		}
	}

	metaVersions, err := s.metadataStore.ListAllObjectVersions(r.Context(), bucketPath, prefix, maxKeys)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to list versions: %v", err), http.StatusInternalServerError)
		return
	}

	type versionItem struct {
		Key            string `json:"key"`
		VersionID      string `json:"versionId"`
		IsLatest       bool   `json:"isLatest"`
		IsDeleteMarker bool   `json:"isDeleteMarker"`
		Size           int64  `json:"size"`
		ETag           string `json:"etag"`
		LastModified   string `json:"lastModified"`
		StorageClass   string `json:"storageClass,omitempty"`
	}

	items := make([]versionItem, 0, len(metaVersions))
	for _, v := range metaVersions {
		items = append(items, versionItem{
			Key:            v.Key,
			VersionID:      v.VersionID,
			IsLatest:       v.IsLatest,
			IsDeleteMarker: v.Size == 0 && v.ETag == "",
			Size:           v.Size,
			ETag:           v.ETag,
			LastModified:   v.LastModified.Format(time.RFC3339),
			StorageClass:   v.StorageClass,
		})
	}

	s.writeJSON(w, map[string]interface{}{
		"versions":    items,
		"isTruncated": len(items) == maxKeys,
	})
}

// handleRestoreObjectVersion implements POST /buckets/{bucket}/objects/{object:.*}/restore
// Body: { "versionId": "...", "isDeleteMarker": false }
//
// For delete markers: removes the marker so the previous real version becomes current.
// For content versions: copies the specified version to a new PUT, making it the latest.
func (s *Server) handleRestoreObjectVersion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.requireCapability(w, r, auth.CapObjectManageVersions, "You do not have permission to manage object versions") {
		return
	}

	var req struct {
		VersionID      string `json:"versionId"`
		IsDeleteMarker bool   `json:"isDeleteMarker"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.VersionID) == "" {
		s.writeError(w, "Invalid request: versionId is required", http.StatusBadRequest)
		return
	}

	tenantID := user.TenantID
	if q := r.URL.Query().Get("tenantId"); q != "" && auth.IsAdminUser(r.Context()) && user.TenantID == "" {
		tenantID = q
	}
	bucketPath := buildBucketPath(tenantID, bucketName)

	if req.IsDeleteMarker {
		// Removing a delete marker exposes the previous real version as the latest
		if err := s.objectManager.DeleteObjectVersion(r.Context(), bucketPath, objectKey, req.VersionID); err != nil {
			s.writeError(w, fmt.Sprintf("Failed to remove delete marker: %v", err), http.StatusInternalServerError)
			return
		}
		s.logAuditEvent(r.Context(), &audit.AuditEvent{
			TenantID:     tenantID,
			UserID:       user.ID,
			Username:     user.Username,
			EventType:    audit.EventTypeObjectDeleted,
			ResourceType: audit.ResourceTypeObject,
			ResourceID:   objectKey,
			ResourceName: objectKey,
			Action:       audit.ActionDelete,
			Status:       audit.StatusSuccess,
			IPAddress:    getClientIP(r, s.config.TrustedProxies),
			UserAgent:    r.Header.Get("User-Agent"),
			Details: map[string]interface{}{
				"bucket":         bucketName,
				"key":            objectKey,
				"removed_marker": req.VersionID,
			},
		})
		s.writeJSON(w, map[string]string{"status": "delete marker removed"})
		return
	}

	// Copy the specified version to a new PUT → it becomes the new latest version
	srcObj, reader, err := s.objectManager.GetObject(r.Context(), bucketPath, objectKey, req.VersionID)
	if err != nil {
		if err == object.ErrObjectNotFound {
			s.writeError(w, "Version not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	defer reader.Close()

	headers := make(http.Header)
	ct := srcObj.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	headers.Set("Content-Type", ct)
	headers.Set("Content-Length", strconv.FormatInt(srcObj.Size, 10))
	if srcObj.ContentDisposition != "" {
		headers.Set("Content-Disposition", srcObj.ContentDisposition)
	}
	if srcObj.ContentEncoding != "" {
		headers.Set("Content-Encoding", srcObj.ContentEncoding)
	}
	if srcObj.CacheControl != "" {
		headers.Set("Cache-Control", srcObj.CacheControl)
	}
	if srcObj.ContentLanguage != "" {
		headers.Set("Content-Language", srcObj.ContentLanguage)
	}
	for k, v := range srcObj.Metadata {
		headers.Set("X-Amz-Meta-"+k, v)
	}

	if _, err = s.objectManager.PutObject(r.Context(), bucketPath, objectKey, reader, headers); err != nil {
		s.writeError(w, fmt.Sprintf("Failed to restore version: %v", err), http.StatusInternalServerError)
		return
	}

	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     tenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeObjectUploaded,
		ResourceType: audit.ResourceTypeObject,
		ResourceID:   objectKey,
		ResourceName: objectKey,
		Action:       audit.ActionUpdate,
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
		UserAgent:    r.Header.Get("User-Agent"),
		Details: map[string]interface{}{
			"bucket":           bucketName,
			"key":              objectKey,
			"restored_version": req.VersionID,
		},
	})

	s.writeJSON(w, map[string]string{"status": "restored"})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// resolveTenantID returns the effective tenant ID for the request.
// Global admins may override it via the tenantId query parameter.
func (s *Server) resolveTenantID(r *http.Request) string {
	user, _ := auth.GetUserFromContext(r.Context())
	tenantID := user.TenantID
	if q := r.URL.Query().Get("tenantId"); q != "" && auth.IsAdminUser(r.Context()) && user.TenantID == "" {
		tenantID = q
	}
	return tenantID
}

// buildBucketPath constructs the internal bucket path from tenantID and bucket name.
func buildBucketPath(tenantID, bucketName string) string {
	if tenantID != "" {
		return tenantID + "/" + bucketName
	}
	return bucketName
}
