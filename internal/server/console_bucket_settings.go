package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
)

// auditBucketSetting records a change user made to a setting of a bucket.
func (s *Server) auditBucketSetting(r *http.Request, user *auth.User, tenantID, bucketName, eventType, action string, details map[string]interface{}) {
	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		EventType:    eventType,
		Action:       action,
		Status:       audit.StatusSuccess,
		UserID:       user.ID,
		Username:     user.Username,
		TenantID:     tenantID,
		ResourceType: audit.ResourceTypeBucket,
		ResourceID:   bucketName,
		ResourceName: bucketName,
		IPAddress:    getClientIP(r, s.proxyTrust()),
		UserAgent:    r.UserAgent(),
		Details:      details,
	})
}

// defaultRetentionRequest is a default retention the console sets.
type defaultRetentionRequest struct {
	Mode  string `json:"mode"`  // GOVERNANCE or COMPLIANCE
	Days  int    `json:"days"`  // Retention period in days
	Years int    `json:"years"` // Retention period in years
}

// days is the period asked, in days.
func (req *defaultRetentionRequest) days() int {
	if req.Years > 0 {
		return req.Years * 365
	}
	return req.Days
}

// handlePutObjectLockConfiguration sets the default retention of a bucket
// with Object Lock on. The console keeps its mode, and only lengthens it.
func (s *Server) handlePutObjectLockConfiguration(w http.ResponseWriter, r *http.Request) {
	bucketName := mux.Vars(r)["bucket"]
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}
	if !s.requireConsoleBucketS3Action(w, r, bucketName, auth.ActionPutBucketObjectLockConfiguration,
		"You do not have permission to configure this bucket") {
		return
	}
	tenantID := consoleBucketTenant(r, user)

	var req defaultRetentionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := bucket.ValidateDefaultRetention(req.Mode, req.Days, req.Years); err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := s.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if errors.Is(err, bucket.ErrBucketNotFound) {
		s.writeError(w, "Bucket not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := lengthenDefaultRetention(info, &req); err != nil {
		s.writeRefusal(w, err)
		return
	}
	if err := s.bucketManager.UpdateBucket(r.Context(), tenantID, bucketName, info); err != nil {
		s.writeError(w, "Failed to update Object Lock configuration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.auditBucketSetting(r, user, tenantID, bucketName, "bucket_object_lock_configured", audit.ActionUpdate,
		map[string]interface{}{"mode": req.Mode, "days": req.Days, "years": req.Years})
	s.writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Object Lock configuration updated successfully",
		"config":  map[string]interface{}{"mode": req.Mode, "days": req.Days, "years": req.Years},
	})
}

// lengthenDefaultRetention gives info the default retention req asks: of a
// bucket with Object Lock on, in the mode it has, and no shorter.
func lengthenDefaultRetention(info *bucket.Bucket, req *defaultRetentionRequest) error {
	if info.ObjectLock == nil || !info.ObjectLock.ObjectLockEnabled {
		return refused(http.StatusBadRequest, "Object Lock is not enabled for this bucket")
	}
	if info.ObjectLock.Rule == nil {
		info.ObjectLock.Rule = &bucket.ObjectLockRule{}
	}
	current := info.ObjectLock.Rule.DefaultRetention
	if current != nil {
		if current.Mode != req.Mode {
			return refused(http.StatusBadRequest, fmt.Sprintf("Object Lock mode cannot be changed (current: %s)", current.Mode))
		}
		if days := retentionDaysOf(current); req.days() < days {
			return refused(http.StatusBadRequest, fmt.Sprintf(
				"Retention period can only be increased (current: %d days, requested: %d days)", days, req.days()))
		}
	}
	retention := &bucket.DefaultRetention{Mode: req.Mode}
	if req.Years > 0 {
		retention.Years = &req.Years
	} else {
		retention.Days = &req.Days
	}
	info.ObjectLock.Rule.DefaultRetention = retention
	return nil
}

// retentionDaysOf is a default retention's period, in days.
func retentionDaysOf(retention *bucket.DefaultRetention) int {
	switch {
	case retention.Years != nil:
		return *retention.Years * 365
	case retention.Days != nil:
		return *retention.Days
	default:
		return 0
	}
}
