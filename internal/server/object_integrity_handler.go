package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// handleVerifyBucketIntegrity handles POST /buckets/{bucket}/verify-integrity
// Only global admins may call this endpoint.
//
// Rate limit: when marker is empty (first page of a new scan) the handler
// checks that at least minManualScanInterval has elapsed since the last manual
// scan for this bucket.  Continuation pages (marker != "") are always allowed.
func (s *Server) handleVerifyBucketIntegrity(w http.ResponseWriter, r *http.Request) {
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := len(user.Roles) > 0 && user.Roles[0] == "admin" && user.TenantID == ""
	if !isGlobalAdmin {
		s.writeError(w, "Forbidden: only global admins can run integrity verification", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Build the full bucket path used by the metadata store:
	// tenant buckets are stored as "tenantID/bucketName".
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID
	if queryTenantID != "" {
		tenantID = queryTenantID
	}
	bucketPath := bucketName
	if tenantID != "" {
		bucketPath = tenantID + "/" + bucketName
	}

	prefix := r.URL.Query().Get("prefix")
	marker := r.URL.Query().Get("marker")

	maxKeys := 1000
	if v := r.URL.Query().Get("maxKeys"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxKeys = n
		}
	}
	if maxKeys > 5000 {
		maxKeys = 5000
	}

	// Rate limit: only enforced on the first page of a new scan (marker == "").
	if marker == "" {
		if last := s.lastManualScanTime(r.Context(), bucketPath); !last.IsZero() {
			elapsed := time.Since(last)
			if elapsed < minManualScanInterval {
				remaining := (minManualScanInterval - elapsed).Round(time.Second)
				s.writeError(w, fmt.Sprintf(
					"Rate limit: the last manual scan was %.0f minutes ago. Please wait %s before starting a new scan.",
					elapsed.Minutes(), remaining.String(),
				), http.StatusTooManyRequests)
				return
			}
		}
	}

	om, ok := s.objectManager.(interface {
		VerifyBucketIntegrity(ctx context.Context, bucket, prefix, marker string, maxKeys int) (*object.BucketIntegrityReport, error)
	})
	if !ok {
		s.writeError(w, "Integrity verification not supported by this object manager", http.StatusInternalServerError)
		return
	}

	report, err := om.VerifyBucketIntegrity(r.Context(), bucketPath, prefix, marker, maxKeys)
	if err != nil {
		logrus.WithError(err).WithField("bucket", bucketPath).Error("Integrity verification failed")
		s.writeError(w, "Integrity verification failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Audit the check
	_ = s.auditManager.LogEvent(r.Context(), &audit.AuditEvent{
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeDataIntegrityCheck,
		ResourceType: audit.ResourceTypeBucket,
		ResourceID:   bucketName,
		ResourceName: bucketName,
		Action:       audit.ActionVerifyIntegrity,
		Status:       audit.StatusSuccess,
		Details: map[string]interface{}{
			"checked":   report.Checked,
			"corrupted": report.Corrupted,
			"skipped":   report.Skipped,
			"errors":    report.Errors,
			"duration":  report.Duration,
		},
	})

	s.writeJSON(w, report)
}

// handleGetIntegrityStatus handles GET /buckets/{bucket}/integrity-status.
// Returns the history of the last maxScanHistory scan results (newest first),
// or 404 if no scan has ever been recorded for this bucket.
func (s *Server) handleGetIntegrityStatus(w http.ResponseWriter, r *http.Request) {
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := len(user.Roles) > 0 && user.Roles[0] == "admin" && user.TenantID == ""
	if !isGlobalAdmin {
		s.writeError(w, "Forbidden: only global admins can view integrity status", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID
	if queryTenantID != "" {
		tenantID = queryTenantID
	}
	bucketPath := bucketName
	if tenantID != "" {
		bucketPath = tenantID + "/" + bucketName
	}

	history, err := s.getIntegrityHistory(r.Context(), bucketPath)
	if err != nil {
		logrus.WithError(err).WithField("bucket", bucketPath).Error("Failed to retrieve integrity history")
		s.writeError(w, "Failed to retrieve integrity history: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(history) == 0 {
		s.writeError(w, "No scan results found for this bucket", http.StatusNotFound)
		return
	}

	s.writeJSON(w, history)
}

// handleSaveIntegrityStatus handles POST /buckets/{bucket}/integrity-status.
// Accepts the frontend's accumulated manual scan result and persists it as
// a new history entry.
func (s *Server) handleSaveIntegrityStatus(w http.ResponseWriter, r *http.Request) {
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := len(user.Roles) > 0 && user.Roles[0] == "admin" && user.TenantID == ""
	if !isGlobalAdmin {
		s.writeError(w, "Forbidden: only global admins can save integrity status", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID
	if queryTenantID != "" {
		tenantID = queryTenantID
	}
	bucketPath := bucketName
	if tenantID != "" {
		bucketPath = tenantID + "/" + bucketName
	}

	var report object.BucketIntegrityReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		s.writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.saveIntegrityResult(r.Context(), bucketPath, &report, "manual")
	s.writeJSON(w, map[string]bool{"saved": true})
}
