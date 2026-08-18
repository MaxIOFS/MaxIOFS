package server

import (
	"context"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/auth"
)

func (s *Server) requireCapability(w http.ResponseWriter, r *http.Request, capability, message string) bool {
	if s.authManager == nil {
		return true
	}
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || user == nil {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	roles := user.Roles
	if len(roles) == 0 {
		roles = []string{"user"}
	}
	allowed, err := s.authManager.HasCapability(r.Context(), user.ID, roles, capability)
	if err == nil && allowed {
		return true
	}
	if message == "" {
		message = "Insufficient permissions"
	}
	s.writeError(w, message, http.StatusForbidden)
	return false
}

// containsAdminRole reports whether a role selection confers administration.
func containsAdminRole(roles []string) bool {
	for _, role := range roles {
		if role == auth.RoleAdmin || role == auth.RoleTenantAdmin {
			return true
		}
	}
	return false
}

func consoleBucketARN(bucketName string) string {
	return "arn:aws:s3:::" + bucketName
}

func consoleObjectARN(bucketName, objectKey string) string {
	return consoleBucketARN(bucketName) + "/" + objectKey
}

func (s *Server) resolveConsoleBucketTenantID(r *http.Request, bucketName string, user *auth.User) string {
	if s.metadataStore != nil {
		if meta, err := s.metadataStore.GetBucketByName(r.Context(), bucketName); err == nil && meta != nil {
			return meta.TenantID
		}
	}
	if q := r.URL.Query().Get("tenantId"); q != "" && auth.IsAdminUser(r.Context()) && user != nil && user.TenantID == "" {
		return q
	}
	if user != nil {
		return user.TenantID
	}
	return ""
}

func (s *Server) userCanPerformConsoleS3Action(r *http.Request, user *auth.User, bucketTenantID, action, resource string) bool {
	if user == nil {
		return false
	}
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if isGlobalAdmin {
		if bucketTenantID == "" {
			return true
		}
		return auth.ReadOnlyAuditAction(action)
	}
	if user.TenantID != bucketTenantID {
		return false
	}
	set, ok := auth.PolicySetFromContext(r.Context())
	if !ok || set.UserID != user.ID {
		resolver, ok := s.authManager.(interface {
			ResolvePolicySet(ctx context.Context, user *auth.User) (*auth.PolicySet, error)
		})
		if !ok {
			return false
		}
		var err error
		set, err = resolver.ResolvePolicySet(r.Context(), user)
		if err != nil {
			return false
		}
	}
	return set.Allows(auth.AccessRequest{Action: action, Resource: resource, Owner: auth.OwnedBy(bucketTenantID)})
}

func (s *Server) requireConsoleBucketS3Action(w http.ResponseWriter, r *http.Request, bucketName, action, message string) bool {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || user == nil {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	tenantID := s.resolveConsoleBucketTenantID(r, bucketName, user)
	if action == auth.ActionCreateBucket {
		tenantID = user.TenantID
	}
	if s.userCanPerformConsoleS3Action(r, user, tenantID, action, consoleBucketARN(bucketName)) {
		return true
	}
	if message == "" {
		message = "Insufficient permissions"
	}
	s.writeError(w, message, http.StatusForbidden)
	return false
}

func (s *Server) requireConsoleObjectS3Action(w http.ResponseWriter, r *http.Request, bucketName, objectKey, action, message string) bool {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || user == nil {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	tenantID := s.resolveConsoleBucketTenantID(r, bucketName, user)
	if s.userCanPerformConsoleS3Action(r, user, tenantID, action, consoleObjectARN(bucketName, objectKey)) {
		return true
	}
	if message == "" {
		message = "Insufficient permissions"
	}
	s.writeError(w, message, http.StatusForbidden)
	return false
}
