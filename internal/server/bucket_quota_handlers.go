package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// bucketQuotaResponse is the JSON shape returned by the quota endpoints.
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

func (s *Server) authorizeBucketQuota(w http.ResponseWriter, r *http.Request, bucketName, action string) (string, bool) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || user == nil {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return "", false
	}
	tenantID := s.resolveConsoleBucketTenantID(r, bucketName, user)
	set, ok := auth.PolicySetFromContext(r.Context())
	if !ok || set.UserID != user.ID {
		resolver, exists := s.authManager.(interface {
			ResolvePolicySet(context.Context, *auth.User) (*auth.PolicySet, error)
		})
		if exists {
			set, _ = resolver.ResolvePolicySet(r.Context(), user)
		} else {
			set = nil
		}
	}
	if !set.Allows(auth.AccessRequest{Action: action, Resource: consoleBucketARN(bucketName), Owner: auth.OwnedBy(tenantID),
		BucketPolicy: bucket.DecideBucketPolicy(r.Context(), s.bucketManager, tenantID, action, consoleBucketARN(bucketName),
			bucket.PolicyPrincipals(user))}) {
		s.writeError(w, "You do not have permission to access this bucket's quota", http.StatusForbidden)
		return "", false
	}
	return tenantID, true
}

// handleGetBucketQuota returns the per-bucket storage quota and current usage.
// GET /api/v1/buckets/{bucket}/quota
func (s *Server) handleGetBucketQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	tenantID, ok := s.authorizeBucketQuota(w, r, bucketName, auth.ActionGetBucketQuota)
	if !ok {
		return
	}

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
func (s *Server) handlePutBucketQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	tenantID, ok := s.authorizeBucketQuota(w, r, bucketName, auth.ActionPutBucketQuota)
	if !ok {
		return
	}

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

	// No bucket of a tenant, whoever owns it, is given a size quota larger than
	// the space assigned to the tenant: the tenant quota is the hard ceiling.
	if req.MaxSizeBytes > 0 {
		info, err := s.bucketManager.GetBucketInfo(ctx, tenantID, bucketName)
		if errors.Is(err, bucket.ErrBucketNotFound) {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
			return
		}
		if err != nil {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := s.withinTenantStorage(ctx, info.TenantID, req.MaxSizeBytes); err != nil {
			s.writeRefusal(w, err)
			return
		}
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
		if errors.Is(err, bucket.ErrBucketNotFound) {
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

	tenantID, ok := s.authorizeBucketQuota(w, r, bucketName, auth.ActionPutBucketQuota)
	if !ok {
		return
	}

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

// withinTenantStorage refuses a bucket quota larger than the storage of the
// bucket's tenant: the tenant quota is the hard ceiling.
func (s *Server) withinTenantStorage(ctx context.Context, tenantID string, maxSizeBytes int64) error {
	if tenantID == "" {
		return nil
	}
	tenant, err := s.authManager.GetTenant(ctx, tenantID)
	if err != nil {
		return errors.New("Failed to retrieve tenant information")
	}
	if tenant.MaxStorageBytes > 0 && maxSizeBytes > tenant.MaxStorageBytes {
		return refused(http.StatusBadRequest, fmt.Sprintf(
			"Bucket quota (%d bytes) cannot exceed the tenant's storage quota (%d bytes)", maxSizeBytes, tenant.MaxStorageBytes))
	}
	return nil
}
