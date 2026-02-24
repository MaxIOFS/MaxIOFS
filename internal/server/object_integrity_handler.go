package server

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// handleVerifyBucketIntegrity handles POST /buckets/{bucket}/verify-integrity
// Only global admins may call this endpoint.
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
