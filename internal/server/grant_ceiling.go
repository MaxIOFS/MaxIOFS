package server

import (
	"context"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/auth"
)

// grantBeyondCaller is the first permission of documents the caller does not
// hold itself, or "". An administrator of a tenant grants within what it
// holds; a global administrator holds everything.
func (s *Server) grantBeyondCaller(r *http.Request, documents []string) string {
	caller := s.getAuthUser(r)
	if caller == nil {
		return "anything"
	}
	if s.isGlobalAdmin(caller) {
		return ""
	}
	set, ok := auth.PolicySetFromContext(r.Context())
	if !ok || set.UserID != caller.ID {
		resolver, can := s.authManager.(interface {
			ResolvePolicySet(ctx context.Context, user *auth.User) (*auth.PolicySet, error)
		})
		if !can {
			return "anything"
		}
		resolved, err := resolver.ResolvePolicySet(r.Context(), caller)
		if err != nil {
			return "anything"
		}
		set = resolved
	}
	return set.FirstNotHeld(documents)
}

// refuseBeyondCaller answers 403 when documents grant what the caller does not
// hold, and reports whether it did.
func (s *Server) refuseBeyondCaller(w http.ResponseWriter, r *http.Request, documents []string) bool {
	action := s.grantBeyondCaller(r, documents)
	if action == "" {
		return false
	}
	s.writeError(w, "You cannot grant "+action+": you do not hold it yourself", http.StatusForbidden)
	return true
}

// addedPermissions is what next grants that current does not hold: what a
// grant adds, the only part held to the caller's ceiling. What a user already
// had stays, whoever edits its permissions.
func addedPermissions(next, current *auth.UserPermissions) *auth.UserPermissions {
	held := map[string]bool{}
	onBucket := map[string]map[string]bool{}
	if current != nil {
		for _, action := range current.Global {
			held[action] = true
		}
		for _, grant := range current.Buckets {
			if onBucket[grant.Bucket] == nil {
				onBucket[grant.Bucket] = map[string]bool{}
			}
			for _, action := range grant.Actions {
				onBucket[grant.Bucket][action] = true
			}
		}
	}

	added := &auth.UserPermissions{Global: []string{}, Buckets: []auth.BucketGrant{}}
	for _, action := range next.Global {
		if !held[action] {
			added.Global = append(added.Global, action)
		}
	}
	for _, grant := range next.Buckets {
		var kept []string
		for _, action := range grant.Actions {
			if !held[action] && !onBucket[grant.Bucket][action] {
				kept = append(kept, action)
			}
		}
		if len(kept) > 0 {
			added.Buckets = append(added.Buckets, auth.BucketGrant{Bucket: grant.Bucket, Actions: kept})
		}
	}
	return added
}

// documentsOfRoles are the documents roles give a user of tenantID; nil when
// they cannot be read, which the ceiling refuses.
func (s *Server) documentsOfRoles(roles []string, tenantID string) ([]string, bool) {
	source, ok := s.authManager.(interface {
		GrantedByRoles(roles []string, tenantID string) ([]string, error)
	})
	if !ok {
		return nil, false
	}
	documents, err := source.GrantedByRoles(roles, tenantID)
	return documents, err == nil
}

// newRoles are the roles of next that current does not have.
func newRoles(next, current []string) []string {
	had := map[string]bool{}
	for _, role := range current {
		had[role] = true
	}
	var added []string
	for _, role := range next {
		if !had[role] {
			added = append(added, role)
		}
	}
	return added
}

// refuseRolesBeyondCaller answers 403 when the roles a request gives grant
// what the caller does not hold, and reports whether it did.
func (s *Server) refuseRolesBeyondCaller(w http.ResponseWriter, r *http.Request, roles []string, tenantID string) bool {
	if len(roles) == 0 {
		return false
	}
	if caller := s.getAuthUser(r); caller != nil && s.isGlobalAdmin(caller) {
		return false
	}
	documents, ok := s.documentsOfRoles(roles, tenantID)
	if !ok {
		s.writeError(w, "The permissions of the roles could not be read", http.StatusInternalServerError)
		return true
	}
	return s.refuseBeyondCaller(w, r, documents)
}
