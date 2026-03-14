package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/sirupsen/logrus"
)

// handleGetBucketWebsite returns the static website hosting configuration.
// GET /api/v1/buckets/{bucket}/website
func (s *Server) handleGetBucketWebsite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	currentUser, ok := auth.GetUserFromContext(ctx)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := currentUser.TenantID
	isGlobalAdmin := auth.IsAdminUser(ctx) && currentUser.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	websiteCfg, err := s.bucketManager.GetWebsite(ctx, tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
			return
		}
		if err == bucket.ErrWebsiteNotFound {
			s.writeError(w, "Website hosting not configured", http.StatusNotFound)
			return
		}
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type websiteResponse struct {
		IndexDocument string `json:"indexDocument"`
		ErrorDocument string `json:"errorDocument,omitempty"` // optional; when set, returned so UI shows it
	}
	s.writeJSON(w, websiteResponse{
		IndexDocument: websiteCfg.IndexDocument,
		ErrorDocument: websiteCfg.ErrorDocument,
	})
}

// handlePutBucketWebsite saves or updates the static website hosting configuration.
// PUT /api/v1/buckets/{bucket}/website
func (s *Server) handlePutBucketWebsite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	currentUser, ok := auth.GetUserFromContext(ctx)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := currentUser.TenantID
	isGlobalAdmin := auth.IsAdminUser(ctx) && currentUser.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var req struct {
		IndexDocument string `json:"indexDocument"`
		ErrorDocument string `json:"errorDocument"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.IndexDocument == "" {
		s.writeError(w, "indexDocument is required", http.StatusBadRequest)
		return
	}

	cfg := &bucket.WebsiteConfig{
		IndexDocument: req.IndexDocument,
		ErrorDocument: req.ErrorDocument,
	}

	if err := s.bucketManager.SetWebsite(ctx, tenantID, bucketName, cfg); err != nil {
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
		"index_document": req.IndexDocument,
	}).Info("Bucket website hosting configuration saved")

	s.writeJSON(w, map[string]interface{}{
		"success":       true,
		"indexDocument": cfg.IndexDocument,
		"errorDocument": cfg.ErrorDocument,
	})
}

// handleDeleteBucketWebsite removes the static website hosting configuration.
// DELETE /api/v1/buckets/{bucket}/website
func (s *Server) handleDeleteBucketWebsite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	currentUser, ok := auth.GetUserFromContext(ctx)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := currentUser.TenantID
	isGlobalAdmin := auth.IsAdminUser(ctx) && currentUser.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	if err := s.bucketManager.DeleteWebsite(ctx, tenantID, bucketName); err != nil {
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
	}).Info("Bucket website hosting configuration deleted")

	s.writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Website hosting disabled successfully",
	})
}
