package auth

// The resolved permission set of a user, and the single place a decision is
// made from it.
//
// Everything a user may do lives in one list of policy documents, produced by
// EffectivePolicyDocuments from what the database already holds: their roles,
// their bucket permissions, their overrides and any policy attached to them.
// The capability check and the bucket-access check are two questions asked of
// that one list — not two mechanisms that have to agree.
//
// The set is resolved once per request and carried on the context. Without
// that, replacing two indexed lookups with a translation of the whole
// permission model would mean re-reading five tables on every check.

import (
	"context"
	"strings"
)

// PolicySet is a user's complete permissions, resolved once.
//
// Documents are the scoped grants — what may be done and where. Actions are the
// same permissions seen along their other dimension: what kinds of operation
// the user may perform at all, regardless of target. Both come from the same
// resolution pass over the same rows; they are two views of one set, not two
// sets that have to agree.
type PolicySet struct {
	UserID    string
	Documents []string
	Actions   []string
}

// Allows reports whether the set permits an action on a resource.
func (p *PolicySet) Allows(action, resource string) bool {
	if p == nil {
		return false
	}
	return EvaluateIAMDocuments(p.Documents, action, resource)
}

// AllowsAnywhere reports whether the set permits an action on any resource at
// all. This is the question a capability used to answer: not "may they upload
// here" but "may they upload".
//
// Each candidate resource is judged against the WHOLE set, never against the
// document it came from. Evaluating documents one at a time would let an Allow
// in one satisfy the question while a Deny in another — which must win — is
// never consulted.
func (p *PolicySet) AllowsAnywhere(action string) bool {
	if p == nil {
		return false
	}

	for _, permitted := range p.Actions {
		if stsWildcardMatch(strings.ToLower(permitted), strings.ToLower(action)) {
			return true
		}
	}

	// A policy attached directly to the user can permit an action their roles
	// never mentioned. Those are scoped documents, so the resources they name
	// are probed against the whole set — a Deny anywhere still wins.
	seen := make(map[string]bool)
	for _, doc := range p.Documents {
		for _, resource := range allowedResourcesFor(doc, action) {
			probe := probeResource(resource)
			if seen[probe] {
				continue
			}
			seen[probe] = true

			if EvaluateIAMDocuments(p.Documents, action, probe) {
				return true
			}
		}
	}
	return false
}

// probeResource turns a resource pattern into a concrete ARN the evaluator can
// judge. A pattern is not a resource: "arn:aws:s3:::data/*" matches keys, so a
// key is what has to be tested against the Deny statements in the set.
func probeResource(resource string) string {
	if resource == "*" {
		return "arn:aws:s3:::probe/probe"
	}
	if strings.HasSuffix(resource, "*") {
		return strings.TrimSuffix(resource, "*") + "probe"
	}
	return resource
}

// allowedResourcesFor returns the resource patterns a document allows an action
// on, which is how "anywhere" is answered without knowing the bucket up front.
func allowedResourcesFor(document, action string) []string {
	policy, err := ParseIAMPolicy(document, IAMMaxManagedPolicyBytes)
	if err != nil {
		return nil
	}

	var resources []string
	for _, st := range policy.Statement {
		if st.Effect != EffectAllow || !stsActionMatches(st.Action, action) {
			continue
		}
		resources = append(resources, policyStringValues(st.Resource)...)
	}
	return resources
}

type policySetContextKey struct{}

// WithPolicySet carries a resolved set on the request context.
func WithPolicySet(ctx context.Context, set *PolicySet) context.Context {
	return context.WithValue(ctx, policySetContextKey{}, set)
}

// PolicySetFromContext returns the set resolved for this request, if any.
func PolicySetFromContext(ctx context.Context) (*PolicySet, bool) {
	set, ok := ctx.Value(policySetContextKey{}).(*PolicySet)
	return set, ok && set != nil
}

// resolvePolicySet returns the user's permissions, preferring the set already
// resolved for this request.
func (am *authManager) resolvePolicySet(ctx context.Context, userID string, roles []string) (*PolicySet, error) {
	if set, ok := PolicySetFromContext(ctx); ok && set.UserID == userID {
		return set, nil
	}

	// No set on the context: a console handler, an internal caller, or a code
	// path that runs outside a request. Resolve it directly.
	if len(roles) == 0 {
		user, err := am.store.GetUserByID(userID)
		if err != nil {
			return nil, err
		}
		roles = user.Roles
	}

	return am.buildPolicySet(userID, roles)
}

// ResolvePolicySet resolves a user's complete permissions. Exported so the
// request middleware can resolve once and put the result on the context.
//
// The tenant comes from the resolved user rather than from the users table: a
// role session, and any identity assembled for a request, carries its tenant
// without necessarily having a row to read it back from.
func (am *authManager) ResolvePolicySet(ctx context.Context, user *User) (*PolicySet, error) {
	if user == nil {
		return nil, ErrUserNotFound
	}
	return am.buildPolicySetFor(user.ID, user.Roles, user.TenantID)
}

// buildPolicySet resolves both views in one pass, reading the tenant from the
// user's stored row.
func (am *authManager) buildPolicySet(userID string, roles []string) (*PolicySet, error) {
	return am.buildPolicySetFor(userID, roles, "")
}

// buildPolicySetFor resolves both views, with the tenant given explicitly.
// An empty tenantID means "look it up", which is what a caller holding only an
// identifier can do.
func (am *authManager) buildPolicySetFor(userID string, roles []string, tenantID string) (*PolicySet, error) {
	documents, err := am.store.EffectivePolicyDocumentsInTenant(userID, roles, tenantID)
	if err != nil {
		return nil, err
	}
	actions, err := am.store.EffectiveActionsInTenant(userID, roles, tenantID)
	if err != nil {
		return nil, err
	}
	return &PolicySet{UserID: userID, Documents: documents, Actions: actions}, nil
}
