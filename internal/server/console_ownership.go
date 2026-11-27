package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
)

// consoleBucketTenant is the tenant of the bucket a console request names: the
// caller's, or the one a global administrator names.
func consoleBucketTenant(r *http.Request, user *auth.User) string {
	if tenantID := r.URL.Query().Get("tenantId"); tenantID != "" && auth.IsAdminUser(r.Context()) && user.TenantID == "" {
		return tenantID
	}
	return user.TenantID
}

// handleGetBucketOwnership gives the bucket's Object Ownership; "" for a bucket
// without one, whose ACLs are on.
func (s *Server) handleGetBucketOwnership(w http.ResponseWriter, r *http.Request) {
	bucketName := mux.Vars(r)["bucket"]
	if !s.requireConsoleBucketS3Action(w, r, bucketName, auth.ActionGetBucketOwnership,
		"You do not have permission to read this bucket") {
		return
	}
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	cfg, err := s.bucketManager.GetOwnershipControls(r.Context(), consoleBucketTenant(r, user), bucketName)
	switch {
	case errors.Is(err, bucket.ErrBucketNotFound):
		s.writeError(w, "Bucket not found", http.StatusNotFound)
	case errors.Is(err, bucket.ErrOwnershipControlsNotFound):
		s.writeJSON(w, map[string]string{"objectOwnership": ""})
	case err != nil:
		s.writeError(w, err.Error(), http.StatusInternalServerError)
	default:
		s.writeJSON(w, map[string]string{"objectOwnership": cfg.ObjectOwnership})
	}
}

// handlePutBucketOwnership sets the bucket's Object Ownership. Turning ACLs off
// needs an ACL of the bucket that grants nothing to others, as in AWS.
func (s *Server) handlePutBucketOwnership(w http.ResponseWriter, r *http.Request) {
	bucketName := mux.Vars(r)["bucket"]
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}
	if !s.requireConsoleBucketS3Action(w, r, bucketName, auth.ActionPutBucketOwnership,
		"You do not have permission to configure this bucket") {
		return
	}
	tenantID := consoleBucketTenant(r, user)

	var req struct {
		ObjectOwnership string `json:"objectOwnership"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !bucket.ValidObjectOwnership(req.ObjectOwnership) {
		s.writeError(w, "objectOwnership must be BucketOwnerEnforced, BucketOwnerPreferred or ObjectWriter", http.StatusBadRequest)
		return
	}
	// ACLs are turned off only once the bucket's ACL grants nothing to others,
	// as AWS asks; an ACL that cannot be read may.
	if req.ObjectOwnership == bucket.ObjectOwnershipBucketOwnerEnforced {
		stored, err := s.bucketManager.GetBucketACL(r.Context(), tenantID, bucketName)
		if err != nil && !errors.Is(err, bucket.ErrBucketNotFound) {
			s.writeError(w, "The bucket's ACL could not be read: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if current, ok := stored.(*acl.ACL); ok && !current.OwnerOnly() {
			s.writeError(w, "The bucket's ACL grants access to others: make it private before turning ACLs off", http.StatusBadRequest)
			return
		}
	}

	err := s.bucketManager.SetOwnershipControls(r.Context(), tenantID, bucketName,
		&bucket.OwnershipControlsConfig{ObjectOwnership: req.ObjectOwnership})
	switch {
	case errors.Is(err, bucket.ErrBucketNotFound):
		s.writeError(w, "Bucket not found", http.StatusNotFound)
		return
	case err != nil:
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.auditBucketSetting(r, user, tenantID, bucketName, "bucket_ownership_controls_configured", audit.ActionUpdate,
		map[string]interface{}{"object_ownership": req.ObjectOwnership})
	s.writeJSON(w, map[string]interface{}{"success": true, "objectOwnership": req.ObjectOwnership})
}
