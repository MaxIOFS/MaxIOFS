package auth

// The assignable-permission catalogue, and reading or writing a user's
// permissions as a selection rather than as a document.
//
// An operator picks permissions from a list — by group or one action at a time,
// globally or on a particular bucket. The policy document is what that
// selection is stored as; nobody has to write or read one.

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// CatalogPermission is one assignable action from the catalogue.
type CatalogPermission struct {
	Action         string `json:"action"`
	Group          string `json:"group"`
	Label          string `json:"label"`
	Description    string `json:"description,omitempty"`
	ResourceScoped bool   `json:"resourceScoped"`
}

// PermissionGroup bundles the actions the console offers together, so granting
// "read" is one click and granting a single action is still possible.
type PermissionGroup struct {
	Name        string              `json:"name"`
	Permissions []CatalogPermission `json:"permissions"`
}

// BucketGrant is the set of actions a user holds on one bucket.
type BucketGrant struct {
	Bucket  string   `json:"bucket"`
	Actions []string `json:"actions"`
}

// UserPermissions is a user's complete selection: what they may do anywhere,
// and what they may do on each bucket they were given.
type UserPermissions struct {
	Global  []string      `json:"global"`
	Buckets []BucketGrant `json:"buckets"`
}

// ListPermissionCatalog returns every assignable permission, grouped.
func (s *SQLiteStore) ListPermissionCatalog() ([]PermissionGroup, error) {
	rows, err := s.db.Query(`
		SELECT action, action_group, label, description, resource_scoped
		FROM iam_permissions
		ORDER BY display_order
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var order []string
	byGroup := make(map[string][]CatalogPermission)

	for rows.Next() {
		var p CatalogPermission
		var description sql.NullString
		var scoped int
		if err := rows.Scan(&p.Action, &p.Group, &p.Label, &description, &scoped); err != nil {
			return nil, err
		}
		if description.Valid {
			p.Description = description.String
		}
		p.ResourceScoped = scoped == 1

		if _, seen := byGroup[p.Group]; !seen {
			order = append(order, p.Group)
		}
		byGroup[p.Group] = append(byGroup[p.Group], p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	groups := make([]PermissionGroup, 0, len(order))
	for _, name := range order {
		groups = append(groups, PermissionGroup{Name: name, Permissions: byGroup[name]})
	}
	return groups, nil
}

// IsResourceScopedAction reports whether an action from the catalogue names a
// bucket. Used to keep a global selection from containing actions that would be
// unreachable without one, and the reverse.
func (s *SQLiteStore) IsResourceScopedAction(action string) (bool, error) {
	var scoped int
	err := s.db.QueryRow(
		`SELECT resource_scoped FROM iam_permissions WHERE action = ?`, action).Scan(&scoped)
	if err == sql.ErrNoRows {
		return false, fmt.Errorf("%w: unknown permission %q", ErrIAMInvalidInput, action)
	}
	if err != nil {
		return false, err
	}
	return scoped == 1, nil
}

// GetUserPermissions reads back what a user may do, so the console can show the
// boxes that are ticked.
//
// It resolves their EFFECTIVE permissions, not only the ones attached directly
// to them: most of what a user can do comes from their role, and a screen that
// showed only the direct ones would tell an administrator they have nothing.
func (s *SQLiteStore) GetUserPermissions(userID string) (*UserPermissions, error) {
	var roles []string
	if user, err := s.GetUserByID(userID); err == nil {
		roles = user.Roles
	}

	documents, err := s.EffectivePolicyDocuments(userID, roles)
	if err != nil {
		return nil, err
	}

	result := &UserPermissions{Global: []string{}, Buckets: []BucketGrant{}}
	byBucket := make(map[string]map[string]bool)
	global := make(map[string]bool)

	for _, document := range documents {
		policy, err := ParseIAMPolicy(document, IAMMaxManagedPolicyBytes)
		if err != nil {
			continue
		}
		for _, st := range policy.Statement {
			if st.Effect != EffectAllow {
				continue
			}
			actions := policyStringValues(st.Action)
			for _, resource := range policyStringValues(st.Resource) {
				bucket := bucketFromResource(resource)
				if bucket == "" {
					for _, action := range expandWildcardActions(actions) {
						global[action] = true
					}
					continue
				}
				if byBucket[bucket] == nil {
					byBucket[bucket] = make(map[string]bool)
				}
				for _, action := range expandWildcardActions(actions) {
					byBucket[bucket][action] = true
				}
			}
		}
	}

	result.Global = sortedKeys(global)
	for bucket, actions := range byBucket {
		result.Buckets = append(result.Buckets, BucketGrant{Bucket: bucket, Actions: sortedKeys(actions)})
	}
	sort.Slice(result.Buckets, func(i, j int) bool {
		return result.Buckets[i].Bucket < result.Buckets[j].Bucket
	})
	return result, nil
}

// SetUserPermissions replaces a user's selection with the one given.
//
// It rewrites every policy this screen owns and leaves the rest alone, so a
// policy attached by the IAM API or shared from a role is not silently dropped
// by someone saving an unrelated change in the console.
func (s *SQLiteStore) SetUserPermissions(userID string, permissions *UserPermissions) error {
	if permissions == nil {
		return fmt.Errorf("%w: no permissions given", ErrIAMInvalidInput)
	}

	for _, action := range permissions.Global {
		if _, err := s.IsResourceScopedAction(action); err != nil {
			return err
		}
	}
	for _, grant := range permissions.Buckets {
		if grant.Bucket == "" {
			return fmt.Errorf("%w: a bucket grant needs a bucket", ErrIAMInvalidInput)
		}
		for _, action := range grant.Actions {
			if _, err := s.IsResourceScopedAction(action); err != nil {
				return err
			}
		}
	}

	existing, err := s.ListIAMInlinePolicies(IAMTargetUser, userID)
	if err != nil {
		return err
	}
	for _, inline := range existing {
		if inline.Name == selectionPolicyName || strings.HasPrefix(inline.Name, bucketPolicyPrefix) {
			if err := s.DeleteIAMInlinePolicy(IAMTargetUser, userID, inline.Name); err != nil && !isNoSuchEntity(err) {
				return err
			}
		}
	}

	if len(permissions.Global) > 0 {
		if err := s.PutIAMInlinePolicy(IAMTargetUser, userID,
			selectionPolicyName, allowDocument(permissions.Global)); err != nil {
			return err
		}
	}

	for _, grant := range permissions.Buckets {
		if len(grant.Actions) == 0 {
			continue
		}
		document := bucketGrantDocument(grant.Bucket, grant.Actions)
		if document == "" {
			continue
		}
		if err := s.PutIAMInlinePolicy(IAMTargetUser, userID,
			bucketPolicyName(grant.Bucket), document); err != nil {
			return err
		}
	}
	return nil
}

// expandWildcardActions turns a wildcard into the permissions it covers, so a
// screen of checkboxes can show what "everything" means. An administrator holds
// a single "*" statement; without this the boxes would all be empty next to a
// user who can do anything at all.
func expandWildcardActions(actions []string) []string {
	var out []string
	for _, action := range actions {
		if !strings.Contains(action, "*") {
			out = append(out, action)
			continue
		}
		for _, permission := range permissionCatalog {
			if stsWildcardMatch(strings.ToLower(action), strings.ToLower(permission.Action)) {
				out = append(out, permission.Action)
			}
		}
	}
	return out
}

// selectionPolicyName holds the permissions a user has everywhere.
const selectionPolicyName = "permissions"

// bucketPolicyPrefix marks the policies that hold a per-bucket selection.
const bucketPolicyPrefix = "bucket-"

// bucketFromResource extracts the bucket an ARN names, or "" when the resource
// is not a bucket at all.
func bucketFromResource(resource string) string {
	if resource == "*" {
		return ""
	}
	trimmed := strings.TrimPrefix(resource, "arn:aws:s3:::")
	if trimmed == resource {
		return ""
	}
	trimmed = strings.TrimSuffix(trimmed, "/*")
	if trimmed == "" || trimmed == "*" {
		return ""
	}
	return trimmed
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// --- manager pass-throughs ---
//
// The console reaches these through the Manager it already holds. Without them
// the store's methods are invisible to it and every call fails as "not
// available", which is exactly what an interface assertion against the wrong
// type looks like from the outside.

// ListPermissionCatalog returns every assignable permission, grouped.
func (am *authManager) ListPermissionCatalog() ([]PermissionGroup, error) {
	return am.store.ListPermissionCatalog()
}

// GetUserPermissions returns a user's selection.
func (am *authManager) GetUserPermissions(userID string) (*UserPermissions, error) {
	return am.store.GetUserPermissions(userID)
}

// SetUserPermissions replaces a user's selection.
func (am *authManager) SetUserPermissions(userID string, permissions *UserPermissions) error {
	return am.store.SetUserPermissions(userID, permissions)
}

// GrantBucketOwnerPolicy records a bucket's owner as a policy on that owner.
func (am *authManager) GrantBucketOwnerPolicy(bucketName, ownerType, ownerID string) error {
	return am.store.GrantBucketOwnerPolicy(bucketName, ownerType, ownerID)
}

// RevokeBucketPolicies removes every policy naming a bucket.
func (am *authManager) RevokeBucketPolicies(bucketName string) error {
	return am.store.RevokeBucketPolicies(bucketName)
}

// HasPermission reports whether a user's policies allow an action anywhere.
//
// For the administration permissions, which name no bucket, "anywhere" is the
// only question there is.
func (am *authManager) HasPermission(userID string, roles []string, action string) bool {
	return am.HasPermissionInTenant(userID, roles, "", action)
}

// HasPermissionInTenant is the same with the tenant supplied by the caller, for
// identities that carry one without having a row to read it back from.
func (am *authManager) HasPermissionInTenant(userID string, roles []string, tenantID, action string) bool {
	set, err := am.buildPolicySetFor(userID, roles, tenantID)
	if err != nil {
		return false
	}
	return set.AllowsAnywhere(action)
}

// HasExactPermission reports whether an action is named outright by one of the
// user's policies. A wildcard does not satisfy it.
func (am *authManager) HasExactPermission(userID string, roles []string, action string) bool {
	return am.HasExactPermissionInTenant(userID, roles, "", action)
}

// HasExactPermissionInTenant is the same with the tenant supplied by the
// caller.
func (am *authManager) HasExactPermissionInTenant(userID string, roles []string, tenantID, action string) bool {
	documents, err := am.store.EffectivePolicyDocumentsInTenant(userID, roles, tenantID)
	if err != nil {
		return false
	}

	for _, raw := range documents {
		policy, err := ParseIAMPolicy(raw, IAMMaxManagedPolicyBytes)
		if err != nil {
			continue
		}
		for _, st := range policy.Statement {
			if st.Effect != EffectAllow {
				continue
			}
			for _, named := range policyStringValues(st.Action) {
				if strings.EqualFold(named, action) {
					return true
				}
			}
		}
	}
	return false
}

// CountDirectPolicies returns how many policies are attached to each of the
// given users, in one query per kind rather than two per user.
//
// The console lists every user at once; asking per user turned a single screen
// into two queries per row.
func (s *SQLiteStore) CountDirectPolicies(userIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(userIDs))
	if len(userIDs) == 0 {
		return counts, nil
	}

	wanted := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		wanted[id] = true
	}

	for _, query := range []string{
		`SELECT target_id, COUNT(*) FROM iam_inline_policies WHERE target_type = ? GROUP BY target_id`,
		`SELECT target_id, COUNT(*) FROM iam_policy_attachments WHERE target_type = ? GROUP BY target_id`,
	} {
		rows, err := s.db.Query(query, IAMTargetUser)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var targetID string
			var count int
			if err := rows.Scan(&targetID, &count); err != nil {
				rows.Close()
				return nil, err
			}
			if wanted[targetID] {
				counts[targetID] += count
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return counts, nil
}

// CountDirectPolicies is the manager's view of the same count.
func (am *authManager) CountDirectPolicies(userIDs []string) (map[string]int, error) {
	return am.store.CountDirectPolicies(userIDs)
}
