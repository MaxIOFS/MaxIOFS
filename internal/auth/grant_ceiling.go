package auth

import (
	"fmt"
	"sort"
	"strings"
)

// FirstNotHeld is the first permission documents allow that the set does not
// hold itself, within its own account, or "" when it holds them all: who
// grants a permission must hold it. A wildcard on part of the actions, s3:Get*,
// is held when every action of the catalogue it covers is; a wildcard on all
// of them, * or s3:*, only as that wildcard, so a list of actions never makes
// it up. A Deny restricts and is never refused.
func (p *PolicySet) FirstNotHeld(documents []string) string {
	for _, raw := range documents {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		policy, err := ParseIAMPolicy(raw, IAMMaxManagedPolicyBytes)
		if err != nil {
			return "a policy that does not parse"
		}
		for _, st := range policy.Statement {
			if st.Effect != EffectAllow {
				continue
			}
			for _, action := range policyStringValues(st.Action) {
				for _, resource := range policyStringValues(st.Resource) {
					if !p.holds(action, resource) {
						return action
					}
				}
			}
		}
	}
	return ""
}

// holds reports whether the set holds action on resource, a wildcard on part
// of the actions as the actions it covers.
func (p *PolicySet) holds(action, resource string) bool {
	if p.AllowsOwnAccount(action, resource) {
		return true
	}
	if !strings.Contains(action, "*") || action == "*" || strings.EqualFold(action, "s3:*") {
		return false
	}
	for _, covered := range expandWildcardActions([]string{action}) {
		if !p.AllowsOwnAccount(covered, resource) {
			return false
		}
	}
	return true
}

// SelectionDocuments are the documents a selection grants, as
// SetUserPermissions writes them.
func SelectionDocuments(permissions *UserPermissions) []string {
	if permissions == nil {
		return nil
	}
	var documents []string
	if len(permissions.Global) > 0 {
		documents = append(documents, allowDocument(permissions.Global))
	}
	for _, grant := range permissions.Buckets {
		if document := bucketGrantDocument(grant.Bucket, grant.Actions); document != "" {
			documents = append(documents, document)
		}
	}
	return documents
}

// BucketLevelDocument is the document a bucket permission level grants.
func BucketLevelDocument(bucketName, level string) string {
	return bucketGrantDocument(bucketName, levelActions(level))
}

// GrantedByRoles are the documents roles give a user of tenantID.
func (am *authManager) GrantedByRoles(roles []string, tenantID string) ([]string, error) {
	if tenantID == "" {
		var documents []string
		for _, role := range roles {
			roleDocs, err := am.store.IAMEffectiveDocuments(IAMTargetRole, role)
			if err != nil {
				return nil, err
			}
			documents = append(documents, roleDocs...)
		}
		return documents, nil
	}
	return am.store.EffectivePolicyDocumentsInTenant("", roles, tenantID)
}

// GrantedByGroup are the documents a group gives its members.
func (am *authManager) GrantedByGroup(groupID string) ([]string, error) {
	return am.store.IAMEffectiveDocuments(IAMTargetGroup, groupID)
}

// heldOnBucket is what documents allow on a bucket and its objects, action by
// action: those of the catalogue, and any other one they name outright.
func heldOnBucket(documents []string, bucketName string) []string {
	candidates := map[string]bool{ActionGetBucketQuota: true, ActionPutBucketQuota: true}
	for _, permission := range permissionCatalog {
		if permission.ResourceScoped {
			candidates[permission.Action] = true
		}
	}
	for _, action := range ActionsFromDocuments(documents) {
		if isResourceScopedAction(action) && !strings.ContainsAny(action, "*?") {
			candidates[action] = true
		}
	}

	bucketARN := "arn:aws:s3:::" + bucketName
	var held []string
	for action := range candidates {
		if EvaluateIAMDocuments(documents, action, bucketARN) || EvaluateIAMDocuments(documents, action, bucketARN+"/*") {
			held = append(held, action)
		}
	}
	sort.Strings(held)
	return held
}

// withoutDocument is documents without one occurrence of document.
func withoutDocument(documents []string, document string) []string {
	for i, d := range documents {
		if d == document {
			return append(append([]string{}, documents[:i]...), documents[i+1:]...)
		}
	}
	return documents
}

// GrantBucketOwnerPolicy records a bucket's owner as a policy on that owner:
// on the bucket, what the owner's own permissions let it do there, and no
// more. Creating a bucket gives its creator nothing it does not hold, and a
// permission held only on another bucket does not reach this one. The owner
// policy being rewritten does not vouch for itself.
func (s *SQLiteStore) GrantBucketOwnerPolicy(bucketName, ownerType, ownerID string) error {
	if ownerType != "" && ownerType != "user" {
		return fmt.Errorf("bucket owner must be a user, got %q", ownerType)
	}
	owner, err := s.GetUserByID(ownerID)
	if err != nil {
		return err
	}

	name := "owner-" + bucketName
	current, err := s.GetIAMInlinePolicy(IAMTargetUser, ownerID, name)
	if err != nil && !isNoSuchEntity(err) {
		return err
	}
	documents, err := s.EffectivePolicyDocumentsInTenant(ownerID, owner.Roles, owner.TenantID)
	if err != nil {
		return err
	}
	if current != nil {
		documents = withoutDocument(documents, current.Document)
	}

	document := bucketGrantDocument(bucketName, heldOnBucket(documents, bucketName))
	switch {
	case current != nil && current.Document == document:
		return nil
	case document == "" && current == nil:
		return nil
	case document == "":
		return s.DeleteIAMInlinePolicy(IAMTargetUser, ownerID, name)
	}
	return s.PutIAMInlinePolicy(IAMTargetUser, ownerID, name, document)
}

// RecutBucketOwnerPolicies rewrites every bucket owner policy as
// GrantBucketOwnerPolicy writes a new one, and returns how many changed: those
// of earlier releases gave the owner everything on its bucket.
func (s *SQLiteStore) RecutBucketOwnerPolicies() (int, error) {
	rows, err := s.db.Query(`SELECT target_id, name, document FROM iam_inline_policies
		WHERE target_type = ? AND name LIKE 'owner-%'`, IAMTargetUser)
	if err != nil {
		return 0, err
	}
	type ownerPolicy struct{ owner, name, document string }
	var policies []ownerPolicy
	for rows.Next() {
		var p ownerPolicy
		if err := rows.Scan(&p.owner, &p.name, &p.document); err != nil {
			rows.Close() //nolint:errcheck
			return 0, err
		}
		policies = append(policies, p)
	}
	rows.Close() //nolint:errcheck
	if err := rows.Err(); err != nil {
		return 0, err
	}

	changed := 0
	var failed []string
	for _, p := range policies {
		if err := s.GrantBucketOwnerPolicy(strings.TrimPrefix(p.name, "owner-"), "user", p.owner); err != nil {
			failed = append(failed, p.name+" of "+p.owner+": "+err.Error())
			continue
		}
		after, err := s.GetIAMInlinePolicy(IAMTargetUser, p.owner, p.name)
		if err != nil || after.Document != p.document {
			changed++
		}
	}
	if len(failed) > 0 {
		return changed, fmt.Errorf("%d owner policies were not rewritten: %s", len(failed), strings.Join(failed, "; "))
	}
	return changed, nil
}

// RecutBucketOwnerPolicies rewrites every bucket owner policy to what its owner
// holds.
func (am *authManager) RecutBucketOwnerPolicies() (int, error) {
	return am.store.RecutBucketOwnerPolicies()
}
