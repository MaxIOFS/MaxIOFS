package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

type iamPolicyView struct {
	Name        string `json:"name"`
	ARN         string `json:"arn"`
	Description string `json:"description,omitempty"`
	Document    string `json:"document"`
	VersionID   string `json:"versionId"`
	IsBuiltin   bool   `json:"isBuiltin"`
	AttachedTo  int    `json:"attachedTo"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type iamRoleView struct {
	Name               string   `json:"name"`
	ARN                string   `json:"arn"`
	Description        string   `json:"description,omitempty"`
	TrustPolicy        string   `json:"trustPolicy"`
	MaxSessionDuration int      `json:"maxSessionDuration"`
	TenantID           string   `json:"tenantId,omitempty"`
	Policies           []string `json:"policies"`
	CreatedAt          int64    `json:"createdAt"`
}

// iamManagerOrError resolves the IAM manager and checks the caller is a global
// admin, writing the error response itself when either fails.
func (s *Server) iamManagerOrError(w http.ResponseWriter, r *http.Request) (auth.IAMManager, bool) {
	user := s.getAuthUser(r)
	if user == nil || !s.isGlobalAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return nil, false
	}
	im, ok := s.authManager.(auth.IAMManager)
	if !ok {
		s.writeError(w, "IAM is not available on this server", http.StatusServiceUnavailable)
		return nil, false
	}
	return im, true
}

// writeIAMConsoleError maps the IAM errors onto HTTP status codes, so the
// console shows "already exists" or "still attached" rather than a generic
// failure the user cannot act on.
func (s *Server) writeIAMConsoleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrIAMNoSuchEntity):
		s.writeError(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, auth.ErrIAMEntityExists):
		s.writeError(w, err.Error(), http.StatusConflict)
	case errors.Is(err, auth.ErrIAMDeleteConflict), errors.Is(err, auth.ErrIAMLimitExceeded):
		s.writeError(w, err.Error(), http.StatusConflict)
	case errors.Is(err, auth.ErrIAMInvalidInput):
		s.writeError(w, err.Error(), http.StatusBadRequest)
	default:
		s.writeError(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleListIAMPolicies returns every managed policy with its current document.
func (s *Server) handleListIAMPolicies(w http.ResponseWriter, r *http.Request) {
	im, ok := s.iamManagerOrError(w, r)
	if !ok {
		return
	}

	policies, err := im.ListIAMPolicies(r.Context())
	if err != nil {
		s.writeIAMConsoleError(w, err)
		return
	}

	views := make([]iamPolicyView, 0, len(policies))
	for _, p := range policies {
		views = append(views, iamPolicyView{
			Name:        p.Name,
			ARN:         p.ARN,
			Description: p.Description,
			Document:    p.Document,
			VersionID:   p.DefaultVersionID,
			IsBuiltin:   p.IsBuiltin,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "policies": views})
}

// handleCreateIAMPolicy creates a managed policy, or adds a version to one that
// already exists. Editing from the console is the same operation AWS calls
// CreatePolicyVersion, so the previous document stays available to roll back to.
func (s *Server) handleCreateIAMPolicy(w http.ResponseWriter, r *http.Request) {
	im, ok := s.iamManagerOrError(w, r)
	if !ok {
		return
	}

	var req struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Document    json.RawMessage `json:"document"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	document := decodeIAMDocument(req.Document)
	if document == "" {
		s.writeError(w, "A policy document is required", http.StatusBadRequest)
		return
	}

	if existing, err := im.GetIAMPolicy(r.Context(), req.Name); err == nil && existing != nil {
		if _, err := im.CreateIAMPolicyVersion(r.Context(), req.Name, document, true); err != nil {
			s.writeIAMConsoleError(w, err)
			return
		}
	} else if _, err := im.CreateIAMPolicy(r.Context(), req.Name, "/", req.Description, document); err != nil {
		s.writeIAMConsoleError(w, err)
		return
	}

	s.afterIAMWrite(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// handleDeleteIAMPolicy removes a managed policy.
func (s *Server) handleDeleteIAMPolicy(w http.ResponseWriter, r *http.Request) {
	im, ok := s.iamManagerOrError(w, r)
	if !ok {
		return
	}

	name := mux.Vars(r)["name"]
	if err := im.DeleteIAMPolicy(r.Context(), name); err != nil {
		s.writeIAMConsoleError(w, err)
		return
	}

	s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMPolicy, name)
	s.afterIAMWrite(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// handleListIAMRoles returns every role with the policies attached to it.
func (s *Server) handleListIAMRoles(w http.ResponseWriter, r *http.Request) {
	im, ok := s.iamManagerOrError(w, r)
	if !ok {
		return
	}

	roles, err := im.ListIAMRoles(r.Context())
	if err != nil {
		s.writeIAMConsoleError(w, err)
		return
	}

	views := make([]iamRoleView, 0, len(roles))
	for _, role := range roles {
		view := iamRoleView{
			Name:               role.Name,
			ARN:                role.ARN,
			Description:        role.Description,
			TrustPolicy:        role.AssumeRolePolicy,
			MaxSessionDuration: role.MaxSessionDuration,
			TenantID:           role.TenantID,
			Policies:           []string{},
			CreatedAt:          role.CreatedAt,
		}
		if attached, err := im.ListAttachedIAMPolicies(r.Context(), auth.IAMTargetRole, role.Name); err == nil {
			for _, p := range attached {
				view.Policies = append(view.Policies, p.Name)
			}
		}
		if inline, err := im.ListIAMInlinePolicies(r.Context(), auth.IAMTargetRole, role.Name); err == nil {
			for _, p := range inline {
				view.Policies = append(view.Policies, p.Name+" (inline)")
			}
		}
		views = append(views, view)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "roles": views})
}

// handleCreateIAMRole creates a role, or replaces the trust policy of one that
// already exists.
func (s *Server) handleCreateIAMRole(w http.ResponseWriter, r *http.Request) {
	im, ok := s.iamManagerOrError(w, r)
	if !ok {
		return
	}

	var req struct {
		Name               string          `json:"name"`
		Description        string          `json:"description"`
		TrustPolicy        json.RawMessage `json:"trustPolicy"`
		Permissions        json.RawMessage `json:"permissions"`
		MaxSessionDuration int             `json:"maxSessionDuration"`
		TenantID           string          `json:"tenantId"`
		Policies           []string        `json:"policies"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// A trust policy is optional: without one the role is assigned to users,
	// with one it can also be assumed through AssumeRole.
	trust := decodeIAMDocument(req.TrustPolicy)

	if existing, err := im.GetIAMRole(r.Context(), req.Name); err == nil && existing != nil {
		if trust != "" {
			if err := im.UpdateIAMRoleTrustPolicy(r.Context(), req.Name, trust); err != nil {
				s.writeIAMConsoleError(w, err)
				return
			}
		}
	} else if _, err := im.CreateIAMRole(r.Context(), req.Name, "/", req.Description,
		trust, req.MaxSessionDuration, req.TenantID); err != nil {
		s.writeIAMConsoleError(w, err)
		return
	}

	// What the role may do is stored as an inline policy on it — the same shape
	// every other permission takes.
	if permissions := decodeIAMDocument(req.Permissions); permissions != "" {
		if err := im.PutIAMInlinePolicy(r.Context(), auth.IAMTargetRole, req.Name,
			"permissions", permissions); err != nil {
			s.writeIAMConsoleError(w, err)
			return
		}
	}

	for _, policyName := range req.Policies {
		if err := im.AttachIAMPolicy(r.Context(), policyName, auth.IAMTargetRole, req.Name); err != nil {
			s.writeIAMConsoleError(w, err)
			return
		}
	}

	s.afterIAMWrite(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// handleDeleteIAMRole removes a role. Sessions issued from it stop working
// immediately, because role permissions are resolved live.
func (s *Server) handleDeleteIAMRole(w http.ResponseWriter, r *http.Request) {
	im, ok := s.iamManagerOrError(w, r)
	if !ok {
		return
	}

	name := mux.Vars(r)["name"]
	inlineNames := s.iamInlinePolicyNames(r.Context(), im, auth.IAMTargetRole, name)
	attachedNames := s.iamAttachedPolicyNames(r.Context(), im, auth.IAMTargetRole, name)

	if err := im.DeleteIAMRole(r.Context(), name); err != nil {
		s.writeIAMConsoleError(w, err)
		return
	}

	s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMRole, name)
	for _, policyName := range inlineNames {
		s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMInlinePolicy,
			iamInlineTombstoneID(auth.IAMTargetRole, name, policyName))
	}
	for _, policyName := range attachedNames {
		s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMAttachment,
			iamAttachmentTombstoneID(policyName, auth.IAMTargetRole, name))
	}

	s.afterIAMWrite(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// decodeIAMDocument accepts a policy sent either as a JSON object or as a JSON
// string containing the document. The console editor produces the second and
// anything pasted from an AWS example produces the first.
func decodeIAMDocument(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	return string(raw)
}

// recordBucketOwnerPolicy writes the policy that gives a bucket's creator
func (s *Server) recordBucketOwnerPolicy(bucketName, tenantID, ownerID string, created bool) {
	store, ok := s.authManager.(interface {
		GrantBucketOwnerPolicy(bucketName, ownerType, ownerID string) error
		RevokeBucketPolicies(bucketName string) ([]auth.InlinePolicyRef, error)
	})
	if !ok {
		return
	}

	if !created {
		revoked, err := store.RevokeBucketPolicies(bucketName)
		if err != nil {
			logrus.WithError(err).WithField("bucket", bucketName).
				Warn("Failed to remove bucket policies after deletion")
			return
		}
		for _, ref := range revoked {
			s.recordIAMDeletion(context.Background(), cluster.EntityTypeIAMInlinePolicy,
				iamInlineTombstoneID(ref.TargetType, ref.TargetID, ref.Name))
		}
		return
	}

	if ownerID == "" {
		logrus.WithField("bucket", bucketName).
			Warn("Bucket created without an owner; no owner policy was written for it")
		return
	}

	if err := store.GrantBucketOwnerPolicy(bucketName, "user", ownerID); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"bucket": bucketName, "owner": ownerID,
		}).Warn("Failed to record bucket owner policy")
	}
}

// handleListPermissionCatalog returns every assignable permission, grouped, so
// the console can offer them as boxes to tick.
func (s *Server) handleListPermissionCatalog(w http.ResponseWriter, r *http.Request) {
	store, ok := s.authManager.(interface {
		ListPermissionCatalog() ([]auth.PermissionGroup, error)
	})
	if !ok {
		s.writeError(w, "Permission catalogue not available", http.StatusServiceUnavailable)
		return
	}

	groups, err := store.ListPermissionCatalog()
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "groups": groups})
}

// handleGetUserPermissions returns which permissions a user holds, globally and
// per bucket, so the console shows what is already ticked.
func (s *Server) handleGetUserPermissions(w http.ResponseWriter, r *http.Request) {
	store, ok := s.permissionStore(w, r)
	if !ok {
		return
	}

	targetID := mux.Vars(r)["id"]
	if !s.canAdministerUser(w, r, targetID) {
		return
	}

	permissions, err := store.GetUserPermissions(targetID)
	if err != nil {
		s.writeIAMConsoleError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "permissions": permissions})
}

// handleSetUserPermissions replaces a user's selection.
func (s *Server) handleSetUserPermissions(w http.ResponseWriter, r *http.Request) {
	store, ok := s.permissionStore(w, r)
	if !ok {
		return
	}

	var permissions auth.UserPermissions
	if err := json.NewDecoder(r.Body).Decode(&permissions); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if grantsAdministration(&permissions) && !s.isGlobalAdmin(s.getAuthUser(r)) {
		s.writeError(w, "Only a super administrator can grant administration permissions",
			http.StatusForbidden)
		return
	}

	userID := mux.Vars(r)["id"]
	if !s.canAdministerUser(w, r, userID) {
		return
	}
	if err := store.SetUserPermissions(userID, &permissions); err != nil {
		s.writeIAMConsoleError(w, err)
		return
	}

	s.afterIAMWrite(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// grantsAdministration reports whether a selection contains either of the
// administration permissions.
func grantsAdministration(permissions *auth.UserPermissions) bool {
	for _, action := range permissions.Global {
		if action == auth.ActionSuperAdmin || action == auth.ActionTenantAdmin {
			return true
		}
	}
	for _, grant := range permissions.Buckets {
		for _, action := range grant.Actions {
			if action == auth.ActionSuperAdmin || action == auth.ActionTenantAdmin {
				return true
			}
		}
	}
	return false
}

// permissionStore resolves the store and checks the caller may hand out
func (s *Server) canAdministerUser(w http.ResponseWriter, r *http.Request, targetUserID string) bool {
	caller := s.getAuthUser(r)
	if caller == nil {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return false
	}
	if s.isGlobalAdmin(caller) {
		return true
	}

	target, err := s.authManager.GetUser(r.Context(), targetUserID)
	if err != nil {
		if err == auth.ErrUserNotFound {
			s.writeError(w, "User not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return false
	}
	if target.TenantID != caller.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) permissionStore(w http.ResponseWriter, r *http.Request) (interface {
	GetUserPermissions(userID string) (*auth.UserPermissions, error)
	SetUserPermissions(userID string, permissions *auth.UserPermissions) error
}, bool) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return nil, false
	}

	store, ok := s.authManager.(interface {
		GetUserPermissions(userID string) (*auth.UserPermissions, error)
		SetUserPermissions(userID string, permissions *auth.UserPermissions) error
	})
	if !ok {
		s.writeError(w, "Permissions are not available on this server", http.StatusServiceUnavailable)
		return nil, false
	}
	return store, true
}
