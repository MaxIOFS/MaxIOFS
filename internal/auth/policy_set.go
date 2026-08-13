package auth

import (
	"context"
	"strings"
)

// PolicySet is a user's complete permissions, resolved once. TenantID is the
// account the principal belongs to.
type PolicySet struct {
	UserID    string
	TenantID  string
	Documents []string
	Actions   []string
}

// AccessRequest is one authorization question: a principal asking to perform an
// action on a resource owned by some tenant.
//
// ResourceTenant is required rather than optional because a tenant is an AWS
// account and an S3 ARN cannot express one: bucket names are global, so
// `arn:aws:s3:::backups` names a bucket called that in every tenant at once.
// AWS resolves the owning account before evaluating, and so does this. An empty
// ResourceTenant is not "unknown" — it names the shared namespace, the buckets
// that belong to no tenant.
type AccessRequest struct {
	Action   string
	Resource string
	Owner    ResourceOwner
}

// ResourceOwner names the account a resource belongs to.
//
// It is a type rather than a string because the two meanings a string would
// carry are not the same: an empty tenant is the shared namespace — the buckets
// that belong to no tenant, which every principal may be granted — while an
// unset field means nobody resolved the owner at all. Left as a string those
// two are identical, so forgetting to fill it in opened the boundary instead of
// closing it. The zero value here is "not stated", and it is refused.
type ResourceOwner struct {
	tenantID string
	stated   bool
}

// OwnedBy names the tenant a resource belongs to. An empty tenant is the shared
// namespace, stated deliberately.
func OwnedBy(tenantID string) ResourceOwner {
	return ResourceOwner{tenantID: tenantID, stated: true}
}

// Allows reports whether the set permits the request.
//
// The account boundary is enforced here rather than by each caller. It is not
// expressible in a policy document, so leaving it to the call sites meant every
// new one had to remember it, and the ones that forgot read across tenants.
func (p *PolicySet) Allows(req AccessRequest) bool {
	if p == nil {
		return false
	}
	// An owner nobody resolved is not a resource anybody may reach.
	if !req.Owner.stated {
		return false
	}
	if !p.mayReachAccount(req) {
		return false
	}
	return EvaluateIAMDocuments(p.Documents, req.Action, req.Resource)
}

// AllowsOwnAccount answers for a resource that belongs to no tenant — the
// shared namespace, or a permission that names no bucket at all.
func (p *PolicySet) AllowsOwnAccount(action, resource string) bool {
	if p == nil {
		return false
	}
	return p.Allows(AccessRequest{Action: action, Resource: resource, Owner: OwnedBy(p.TenantID)})
}

// mayReachAccount decides whether the principal may touch the resource's
// account at all, before any policy is consulted.
//
// Same account, or a resource in the shared namespace: yes. Otherwise only a
// super administrator, and only to read — the audit view of another tenant.
func (p *PolicySet) mayReachAccount(req AccessRequest) bool {
	if req.Owner.tenantID == "" || p.TenantID == req.Owner.tenantID {
		return true
	}
	// A principal that belongs to an account never leaves it, whatever its own
	// policies say. Reaching into another account is not something an identity
	// policy can grant itself — that is what makes the boundary a boundary.
	if p.TenantID != "" {
		return false
	}
	if !ReadOnlyAuditAction(req.Action) {
		return false
	}
	return EvaluateIAMDocuments(p.Documents, ActionSuperAdmin, "*")
}

// AllowsAnywhere reports whether the set permits an action on any resource at
func (p *PolicySet) AllowsAnywhere(action string) bool {
	if p == nil {
		return false
	}

	for _, permitted := range p.Actions {
		if stsWildcardMatch(strings.ToLower(permitted), strings.ToLower(action)) {
			return true
		}
	}

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

// ResolvePolicySet resolves a user's complete permissions.
func (am *authManager) ResolvePolicySet(ctx context.Context, user *User) (*PolicySet, error) {
	if user == nil {
		return nil, ErrUserNotFound
	}
	return am.buildPolicySetFor(user.ID, user.Roles, user.TenantID)
}

// buildPolicySet resolves both views for a caller holding only an identifier,
// so the principal's account comes from the stored row.
func (am *authManager) buildPolicySet(userID string, roles []string) (*PolicySet, error) {
	tenantID := ""
	if user, err := am.store.GetUserByID(userID); err == nil && user != nil {
		tenantID = user.TenantID
		if len(roles) == 0 {
			roles = user.Roles
		}
	}
	return am.buildPolicySetFor(userID, roles, tenantID)
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

	// tenantID is the principal's account, taken as given: a caller holding the
	// user knows it, and an empty value means the shared namespace rather than
	// "unresolved". Guessing it from the stored row would override a caller
	// that deliberately asks about a tenant-less identity.
	return &PolicySet{UserID: userID, TenantID: tenantID, Documents: documents, Actions: actions}, nil
}

// ReadOnlyAuditAction reports whether an action only observes. It is the set a
// super administrator may use across an account boundary.
func ReadOnlyAuditAction(action string) bool {
	switch action {
	case ActionListAllMyBuckets,
		ActionListBucket,
		ActionListBucketVersions,
		ActionListBucketMultipartUploads,
		ActionListMultipartUploadParts,
		ActionGetBucketLocation,
		ActionGetBucketVersioning,
		ActionGetBucketPolicy,
		ActionGetBucketLifecycle,
		ActionGetBucketCORS,
		ActionGetBucketTagging,
		ActionGetBucketAcl,
		ActionGetObject,
		ActionGetObjectVersion,
		ActionGetObjectAcl,
		ActionGetObjectTagging,
		ActionGetObjectRetention,
		ActionGetObjectLegalHold:
		return true
	}
	return false
}
