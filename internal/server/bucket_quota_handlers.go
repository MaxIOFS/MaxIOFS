package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// bucketQuotaResponse is the JSON shape returned by the quota endpoints. It
// reports the configured limits (null when no quota is set) alongside the
// bucket's current usage so the console can render a usage/limit bar without a
// second request.
type bucketQuotaResponse struct {
	Quota *bucketQuotaPayload `json:"quota"`
	Usage bucketQuotaUsage    `json:"usage"`
}

type bucketQuotaPayload struct {
	MaxSizeBytes   int64 `json:"maxSizeBytes"`
	MaxObjectCount int64 `json:"maxObjectCount"`
}

type bucketQuotaUsage struct {
	TotalSize   int64 `json:"totalSize"`
	ObjectCount int64 `json:"objectCount"`
}

// resolveBucketQuotaTenant resolves the tenant scope for a bucket quota request,
// honoring a ?tenantId= override only for global admins (same rule as the other
// bucket config handlers).
func (s *Server) resolveBucketQuotaTenant(r *http.Request, currentUser *auth.User) string {
	tenantID := currentUser.TenantID
	queryTenantID := r.URL.Query().Get("tenantId")
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && currentUser.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}
	return tenantID
}

// handleGetBucketQuota returns the per-bucket storage quota and current usage.
// GET /api/v1/buckets/{bucket}/quota
func (s *Server) handleGetBucketQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	currentUser, ok := auth.GetUserFromContext(ctx)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	tenantID := s.resolveBucketQuotaTenant(r, currentUser)

	info, err := s.bucketManager.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
			return
		}
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := bucketQuotaResponse{
		Usage: bucketQuotaUsage{TotalSize: info.TotalSize, ObjectCount: info.ObjectCount},
	}
	if info.Quota != nil {
		resp.Quota = &bucketQuotaPayload{
			MaxSizeBytes:   info.Quota.MaxSizeBytes,
			MaxObjectCount: info.Quota.MaxObjectCount,
		}
	}
	s.writeJSON(w, resp)
}

// handlePutBucketQuota sets or updates the per-bucket storage quota.
// PUT /api/v1/buckets/{bucket}/quota
// Body: {"maxSizeBytes": <int64>, "maxObjectCount": <int64>}  (0 = unlimited for that field)
// Setting both fields to 0 clears the quota entirely.
func (s *Server) handlePutBucketQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	currentUser, ok := auth.GetUserFromContext(ctx)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}
	if !s.requireCapability(w, r, auth.CapBucketConfigure, "You do not have permission to configure buckets") {
		return
	}

	tenantID := s.resolveBucketQuotaTenant(r, currentUser)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var req struct {
		MaxSizeBytes   int64 `json:"maxSizeBytes"`
		MaxObjectCount int64 `json:"maxObjectCount"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.MaxSizeBytes < 0 || req.MaxObjectCount < 0 {
		s.writeError(w, "Quota limits cannot be negative", http.StatusBadRequest)
		return
	}

	// Both limits zero means "no quota" — clear it rather than persisting an
	// all-zero quota that would read as unlimited anyway.
	var quota *metadata.BucketQuota
	if req.MaxSizeBytes > 0 || req.MaxObjectCount > 0 {
		quota = &metadata.BucketQuota{
			MaxSizeBytes:   req.MaxSizeBytes,
			MaxObjectCount: req.MaxObjectCount,
		}
	}

	if err := s.bucketManager.SetQuota(ctx, tenantID, bucketName, quota); err != nil {
		if err == bucket.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
			return
		}
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":         bucketName,
		"tenant_id":      tenantID,
		"maxSizeBytes":   req.MaxSizeBytes,
		"maxObjectCount": req.MaxObjectCount,
		"cleared":        quota == nil,
	}).Info("Bucket storage quota updated")

	// Return the fresh state (quota + usage) so the UI can refresh in one round-trip.
	info, err := s.bucketManager.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		s.writeJSON(w, map[string]interface{}{"success": true})
		return
	}
	resp := bucketQuotaResponse{
		Usage: bucketQuotaUsage{TotalSize: info.TotalSize, ObjectCount: info.ObjectCount},
	}
	if info.Quota != nil {
		resp.Quota = &bucketQuotaPayload{
			MaxSizeBytes:   info.Quota.MaxSizeBytes,
			MaxObjectCount: info.Quota.MaxObjectCount,
		}
	}
	s.writeJSON(w, resp)
}

// handleDeleteBucketQuota removes the per-bucket storage quota.
// DELETE /api/v1/buckets/{bucket}/quota
func (s *Server) handleDeleteBucketQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	currentUser, ok := auth.GetUserFromContext(ctx)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}
	if !s.requireCapability(w, r, auth.CapBucketConfigure, "You do not have permission to configure buckets") {
		return
	}

	tenantID := s.resolveBucketQuotaTenant(r, currentUser)

	if err := s.bucketManager.DeleteQuota(ctx, tenantID, bucketName); err != nil {
		if err == bucket.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
			return
		}
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":    bucketName,
		"tenant_id": tenantID,
	}).Info("Bucket storage quota removed")

	s.writeJSON(w, map[string]interface{}{"success": true})
}
