package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// ── Rename ────────────────────────────────────────────────────────────────────

// handleRenameObject implements POST /buckets/{bucket}/objects/{object:.*}/rename
func (s *Server) handleRenameObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName, objectKey := vars["bucket"], vars["object"]
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	newKey, err := renameTarget(r, objectKey)
	if err != nil {
		s.writeRefusal(w, err)
		return
	}
	if !s.mayRename(w, r, bucketName, objectKey, newKey) {
		return
	}
	tenantID := consoleBucketTenant(r, user)
	if err := s.renameObject(r.Context(), buildBucketPath(tenantID, bucketName), objectKey, newKey); err != nil {
		s.writeRefusal(w, err)
		return
	}
	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     tenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeObjectUploaded,
		ResourceType: audit.ResourceTypeObject,
		ResourceID:   newKey,
		ResourceName: newKey,
		Action:       audit.ActionUpdate,
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r, s.proxyTrust()),
		UserAgent:    r.Header.Get("User-Agent"),
		Details:      map[string]interface{}{"bucket": bucketName, "old_key": objectKey, "new_key": newKey},
	})
	s.writeJSON(w, map[string]string{"newKey": newKey})
}

// renameTarget reads the key an object is renamed to: given, and not its own.
func renameTarget(r *http.Request, objectKey string) (string, error) {
	var req struct {
		NewKey string `json:"newKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.NewKey) == "" {
		return "", refused(http.StatusBadRequest, "Invalid request: newKey is required")
	}
	newKey := strings.TrimSpace(req.NewKey)
	if newKey == objectKey {
		return "", refused(http.StatusBadRequest, "New key is the same as the current key")
	}
	return newKey, nil
}

// mayRename checks the caller may read the object, write the new key and
// delete the old one.
func (s *Server) mayRename(w http.ResponseWriter, r *http.Request, bucketName, objectKey, newKey string) bool {
	return s.requireConsoleObjectS3Action(w, r, bucketName, objectKey, auth.ActionGetObject, "You do not have permission to read objects") &&
		s.requireConsoleObjectS3Action(w, r, bucketName, newKey, auth.ActionPutObject, "You do not have permission to upload objects") &&
		s.requireConsoleObjectS3Action(w, r, bucketName, objectKey, auth.ActionDeleteObject, "You do not have permission to delete objects")
}

// renameObject writes an object at a new key of its bucket, with its tags,
// and deletes it at the old one. One under a legal hold or an unexpired
// COMPLIANCE retention stays where it is.
func (s *Server) renameObject(ctx context.Context, bucketPath, objectKey, newKey string) error {
	source, reader, err := s.openConsoleObject(ctx, bucketPath, objectKey, "Object not found")
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := renameRefusal(source); err != nil {
		return err
	}
	if _, err := s.objectManager.PutObject(ctx, bucketPath, newKey, reader, rewriteHeaders(source)); err != nil {
		return refused(writeErrorStatus(err), "Failed to write object at new key: "+err.Error())
	}
	// Tags are copied when they can be; the rename stands without them.
	if tags, err := s.objectManager.GetObjectTagging(ctx, bucketPath, objectKey); err == nil && tags != nil && len(tags.Tags) > 0 {
		_ = s.objectManager.SetObjectTagging(ctx, bucketPath, newKey, tags)
	}
	// The data is already at the new key: a failed delete is logged, not answered.
	if _, err := s.objectManager.DeleteObject(ctx, bucketPath, objectKey, false); err != nil {
		logrus.WithError(err).WithField("key", objectKey).Warn("rename: failed to delete source object after successful copy")
	}
	return nil
}

// renameRefusal refuses to rename an object under a legal hold or an
// unexpired COMPLIANCE retention.
func renameRefusal(source *object.Object) error {
	if source.LegalHold != nil && source.LegalHold.Status == "ON" {
		return refused(http.StatusForbidden, "Cannot rename: object has an active Legal Hold")
	}
	if source.Retention != nil && source.Retention.Mode == "COMPLIANCE" && source.Retention.RetainUntilDate.After(time.Now()) {
		return refused(http.StatusForbidden, "Cannot rename: object is under COMPLIANCE retention that has not expired")
	}
	return nil
}

// openConsoleObject opens an object, or a version of it, with the refusal the
// console answers when it cannot.
func (s *Server) openConsoleObject(ctx context.Context, bucketPath, key, notFound string, versionID ...string) (*object.Object, io.ReadCloser, error) {
	opened, reader, err := s.objectManager.GetObject(ctx, bucketPath, key, versionID...)
	switch {
	case errors.Is(err, object.ErrObjectNotFound):
		return nil, nil, refused(http.StatusNotFound, notFound)
	case errors.Is(err, object.ErrCustomerKeyRequired):
		return nil, nil, refused(http.StatusConflict, errCustomerKeyConsole)
	case err != nil:
		return nil, nil, err
	}
	return opened, reader, nil
}

// ── Object Tags ───────────────────────────────────────────────────────────────

// handleGetObjectTags implements GET /buckets/{bucket}/objects/{object:.*}/tags
func (s *Server) handleGetObjectTags(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.requireConsoleObjectS3Action(w, r, bucketName, objectKey, auth.ActionGetObjectTagging, "You do not have permission to read object tags") {
		return
	}
	tenantID := consoleBucketTenant(r, user)
	bucketPath := buildBucketPath(tenantID, bucketName)

	tags, err := s.objectManager.GetObjectTagging(r.Context(), bucketPath, objectKey)
	if err != nil {
		if errors.Is(err, object.ErrObjectNotFound) {
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
	if !s.requireConsoleObjectS3Action(w, r, bucketName, objectKey, auth.ActionPutObjectTagging, "You do not have permission to manage object tags") {
		return
	}

	var tags object.TagSet
	if err := json.NewDecoder(r.Body).Decode(&tags); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	// The tags S3 takes, as S3 takes them.
	if err := object.ValidateTags(tags.Tags); err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	tenantID := consoleBucketTenant(r, user)
	bucketPath := buildBucketPath(tenantID, bucketName)

	if err := s.objectManager.SetObjectTagging(r.Context(), bucketPath, objectKey, &tags); err != nil {
		if errors.Is(err, object.ErrObjectNotFound) {
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
		IPAddress:    getClientIP(r, s.proxyTrust()),
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

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.requireConsoleBucketS3Action(w, r, bucketName, auth.ActionListBucket, "You do not have permission to list objects") {
		return
	}

	tenantID := consoleBucketTenant(r, user)
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

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.requireConsoleBucketS3Action(w, r, bucketName, auth.ActionListBucketVersions, "You do not have permission to list object versions") {
		return
	}

	tenantID := consoleBucketTenant(r, user)
	bucketPath := buildBucketPath(tenantID, bucketName)

	prefix := r.URL.Query().Get("prefix")
	maxKeys := 5000
	if v := r.URL.Query().Get("maxKeys"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxKeys = min(n, math.MaxInt32)
		}
	}

	// One more than asked tells whether more are left.
	metaVersions, err := s.metadataStore.ListAllObjectVersions(r.Context(), bucketPath, prefix, maxKeys+1)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to list versions: %v", err), http.StatusInternalServerError)
		return
	}
	truncated := len(metaVersions) > maxKeys
	metaVersions = metaVersions[:min(len(metaVersions), maxKeys)]

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
		"isTruncated": truncated,
	})
}

// handleRestoreObjectVersion implements POST /buckets/{bucket}/objects/{object:.*}/restore:
// a version is written again as the object's latest, or a delete marker with
// a version behind it is removed.
func (s *Server) handleRestoreObjectVersion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName, objectKey := vars["bucket"], vars["object"]
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
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
	if !s.mayRestoreVersion(w, r, bucketName, objectKey, req.IsDeleteMarker) {
		return
	}
	tenantID := consoleBucketTenant(r, user)
	bucketPath := buildBucketPath(tenantID, bucketName)

	eventType, action, status, detail := audit.EventTypeObjectUploaded, audit.ActionUpdate, "restored", "restored_version"
	var err error
	if req.IsDeleteMarker {
		eventType, action, status, detail = audit.EventTypeObjectDeleted, audit.ActionDelete, "delete marker removed", "removed_marker"
		err = s.removeDeleteMarker(r.Context(), bucketPath, objectKey, req.VersionID)
	} else {
		err = s.restoreVersion(r.Context(), bucketPath, objectKey, req.VersionID)
	}
	if err != nil {
		s.writeRefusal(w, err)
		return
	}
	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     tenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    eventType,
		ResourceType: audit.ResourceTypeObject,
		ResourceID:   objectKey,
		ResourceName: objectKey,
		Action:       action,
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r, s.proxyTrust()),
		UserAgent:    r.Header.Get("User-Agent"),
		Details:      map[string]interface{}{"bucket": bucketName, "key": objectKey, detail: req.VersionID},
	})
	s.writeJSON(w, map[string]string{"status": status})
}

// mayRestoreVersion checks the caller may remove a delete marker, or read a
// version and write it again.
func (s *Server) mayRestoreVersion(w http.ResponseWriter, r *http.Request, bucketName, objectKey string, deleteMarker bool) bool {
	if deleteMarker {
		return s.requireConsoleObjectS3Action(w, r, bucketName, objectKey, auth.ActionDeleteObjectVersion,
			"You do not have permission to manage object versions")
	}
	return s.requireConsoleObjectS3Action(w, r, bucketName, objectKey, auth.ActionGetObjectVersion,
		"You do not have permission to read object versions") &&
		s.requireConsoleObjectS3Action(w, r, bucketName, objectKey, auth.ActionPutObject,
			"You do not have permission to restore object versions")
}

// removeDeleteMarker removes a delete marker of an object with a version
// behind it. What the store holds decides whether the version is a marker: a
// version with data is never removed as one, and a marker with nothing behind
// it stays, or the object would simply stop existing.
func (s *Server) removeDeleteMarker(ctx context.Context, bucketPath, objectKey, versionID string) error {
	versions, err := s.objectManager.GetObjectVersions(ctx, bucketPath, objectKey)
	if err != nil {
		return fmt.Errorf("Failed to read object versions: %w", err)
	}
	var found, marker, behind bool
	for _, v := range versions {
		switch {
		case v.VersionID == versionID:
			found, marker = true, v.IsDeleteMarker
		case !v.IsDeleteMarker:
			behind = true
		}
	}
	switch {
	case !found:
		return refused(http.StatusNotFound, "Version not found")
	case !marker:
		return refused(http.StatusBadRequest, "The version is not a delete marker: restore it instead")
	case !behind:
		return refused(http.StatusConflict,
			"Nothing to restore: no earlier version exists behind this delete marker. Removing it would leave no object at all.")
	}
	if err := s.objectManager.DeleteObjectVersion(ctx, bucketPath, objectKey, versionID); err != nil {
		return fmt.Errorf("Failed to remove delete marker: %w", err)
	}
	return nil
}

// restoreVersion writes a version of an object again, as its latest.
func (s *Server) restoreVersion(ctx context.Context, bucketPath, objectKey, versionID string) error {
	source, reader, err := s.openConsoleObject(ctx, bucketPath, objectKey, "Version not found", versionID)
	if err != nil {
		return err
	}
	defer reader.Close()
	if _, err := s.objectManager.PutObject(ctx, bucketPath, objectKey, reader, rewriteHeaders(source)); err != nil {
		return refused(writeErrorStatus(err), "Failed to restore version: "+err.Error())
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// buildBucketPath constructs the internal bucket path from tenantID and bucket name.
func buildBucketPath(tenantID, bucketName string) string {
	if tenantID != "" {
		return tenantID + "/" + bucketName
	}
	return bucketName
}

// rewriteHeaders are the headers that write srcObj again as it is: its
// content type, size, stored headers and user metadata.
func rewriteHeaders(srcObj *object.Object) http.Header {
	headers := make(http.Header)
	ct := srcObj.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	headers.Set("Content-Type", ct)
	headers.Set("Content-Length", strconv.FormatInt(srcObj.Size, 10))
	srcObj.SetStoredHeaders(headers)
	for k, v := range srcObj.Metadata {
		headers.Set("X-Amz-Meta-"+k, v)
	}
	return headers
}

// isQuotaExceeded reports a write refused by a bucket or tenant quota. A
// failure to check the quota is not one.
func isQuotaExceeded(err error) bool {
	return errors.Is(err, object.ErrBucketQuotaExceeded) || errors.Is(err, auth.ErrStorageQuotaExceeded)
}

// writeErrorStatus is the HTTP status for a failed object write.
func writeErrorStatus(err error) int {
	switch {
	case isQuotaExceeded(err):
		return http.StatusForbidden
	case errors.Is(err, object.ErrNoRetentionConfiguration), errors.Is(err, object.ErrInvalidRetentionMode),
		errors.Is(err, object.ErrRetentionDateInPast), errors.Is(err, object.ErrInvalidLegalHoldStatus):
		return http.StatusBadRequest
	case object.IsDiskFull(err):
		return http.StatusInsufficientStorage
	case errors.Is(err, object.ErrBucketNotFound):
		return http.StatusNotFound
	case errors.Is(err, cluster.ErrClusterDegraded):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
