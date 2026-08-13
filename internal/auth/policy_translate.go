package auth

import (
	"database/sql"
	"fmt"
	"strings"
)

// capabilityDocument renders the actions a capability grants as an Allow (or
// Deny) statement over every resource.
func capabilityDocument(capability string, allow bool) string {
	var actions []string
	for _, policyName := range capabilityPolicies[capability] {
		if entry, ok := CatalogEntry(policyName); ok {
			actions = append(actions, entry.Actions...)
		}
	}
	if len(actions) == 0 {
		return ""
	}

	effect := EffectAllow
	if !allow {
		effect = EffectDeny
	}

	quoted := make([]string, 0, len(actions))
	for _, a := range actions {
		quoted = append(quoted, `"`+a+`"`)
	}
	return fmt.Sprintf(
		`{"Version":"2012-10-17","Statement":[{"Effect":%q,"Action":[%s],"Resource":["*"]}]}`,
		effect, strings.Join(quoted, ","))
}

// levelActions returns the S3 actions a bucket permission level allows, before
// it is narrowed by what the user's roles permit.
func levelActions(level string) []string {
	var policies []string
	switch level {
	case PermissionLevelRead:
		policies = []string{PolicyObjectRead}
	case PermissionLevelWrite:
		policies = []string{
			PolicyObjectRead, PolicyObjectWrite, PolicyObjectDelete,
			PolicyObjectManageTags, PolicyObjectManageVersions,
		}
	case PermissionLevelAdmin:
		policies = []string{
			PolicyObjectRead, PolicyObjectWrite, PolicyObjectDelete,
			PolicyObjectManageTags, PolicyObjectManageVersions,
			PolicyBucketConfigure, PolicyBucketManagePolicy, PolicyBucketDelete,
			PolicyObjectLockManage,
		}
	default:
		return nil
	}

	var actions []string
	for _, name := range policies {
		if entry, ok := CatalogEntry(name); ok {
			actions = append(actions, entry.Actions...)
		}
	}
	return actions
}

// bucketGrantDocument renders one bucket permission as a single statement over
func bucketGrantDocument(bucketName string, actions []string) string {
	if len(actions) == 0 {
		return ""
	}

	quoted := make([]string, 0, len(actions))
	for _, a := range actions {
		quoted = append(quoted, `"`+a+`"`)
	}
	return fmt.Sprintf(
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":[%s],`+
			`"Resource":["arn:aws:s3:::%s","arn:aws:s3:::%s/*"]}]}`,
		strings.Join(quoted, ","), bucketName, bucketName)
}

// isResourceScopedAction reports whether an action names a bucket or an object.
func isResourceScopedAction(action string) bool {
	return strings.HasPrefix(action, "s3:") || action == "*"
}

// splitRoleActions separates what a role permits into the globally-granted
// actions and the ones that only make sense against a bucket.
func splitRoleActions(actions []string) (global []string, scoped []string) {
	for _, a := range actions {
		if a == "*" {
			// Full access is both: everything, everywhere.
			return []string{"*"}, []string{"*"}
		}
		if isResourceScopedAction(a) {
			scoped = append(scoped, a)
		} else {
			global = append(global, a)
		}
	}
	return global, scoped
}

// intersectActions returns the actions present in both lists, preserving the
// order of the first. A wildcard on either side means the other side passes
// through unchanged.
func intersectActions(a, b []string) []string {
	if containsAction(a, "*") {
		return b
	}
	if containsAction(b, "*") {
		return a
	}

	present := make(map[string]bool, len(b))
	for _, action := range b {
		present[action] = true
	}

	var out []string
	for _, action := range a {
		if present[action] {
			out = append(out, action)
		}
	}
	return out
}

func containsAction(actions []string, want string) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}

// allowDocument renders an Allow over every resource.
func allowDocument(actions []string) string {
	if len(actions) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(actions))
	for _, a := range actions {
		quoted = append(quoted, `"`+a+`"`)
	}
	return fmt.Sprintf(
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":[%s],"Resource":["*"]}]}`,
		strings.Join(quoted, ","))
}

// --- request path: IAM only ---

// EffectivePolicyDocuments returns a user's permissions: the policies attached
func (s *SQLiteStore) EffectivePolicyDocuments(userID string, roles []string) ([]string, error) {
	return s.EffectivePolicyDocumentsInTenant(userID, roles, "")
}

// EffectivePolicyDocumentsInTenant is the same, with the tenant supplied by the
func (s *SQLiteStore) EffectivePolicyDocumentsInTenant(userID string, roles []string, tenantID string) ([]string, error) {
	documents, err := s.IAMEffectiveDocumentsForUser(userID)
	if err != nil {
		return nil, err
	}

	if tenantID == "" {
		tenantID, err = s.userTenantID(userID)
		if err != nil {
			return nil, err
		}
	}

	for _, role := range roles {
		roleDocs, err := s.IAMEffectiveDocuments(IAMTargetRole, role)
		if err != nil {
			return nil, err
		}

		if tenantID != "" && (role == RoleAdmin || role == RoleTenantAdmin) {
			documents = append(documents, tenantAdminDocument())
			documents = append(documents, withoutUnscopedFullAccess(roleDocs)...)
			continue
		}
		documents = append(documents, roleDocs...)
	}

	if tenantID != "" {
		tenantDocs, err := s.IAMEffectiveDocuments(IAMTargetTenant, tenantID)
		if err != nil {
			return nil, err
		}
		documents = append(documents, tenantDocs...)
	}

	return documents, nil
}

// withoutUnscopedFullAccess drops any document that allows everything
func withoutUnscopedFullAccess(documents []string) []string {
	kept := make([]string, 0, len(documents))
	for _, raw := range documents {
		if grantsEverythingEverywhere(raw) {
			continue
		}
		kept = append(kept, raw)
	}
	return kept
}

func grantsEverythingEverywhere(document string) bool {
	policy, err := ParseIAMPolicy(document, IAMMaxManagedPolicyBytes)
	if err != nil {
		return false
	}
	for _, st := range policy.Statement {
		if st.Effect != EffectAllow {
			continue
		}
		if containsAction(policyStringValues(st.Action), "*") &&
			containsAction(policyStringValues(st.Resource), "*") {
			return true
		}
	}
	return false
}

func tenantAdminDocument() string {
	return allowDocument([]string{
		ActionTenantAdmin,
		ActionConsoleAccess,
		ActionManageOwnKeys,
		ActionCreateBucket,
		ActionDeleteBucket,
		ActionGetBucketLocation,
		ActionListAllMyBuckets,
		ActionListBucket,
		ActionListBucketVersions,
		ActionGetBucketVersioning,
		ActionPutBucketVersioning,
		ActionGetBucketLifecycle,
		ActionPutBucketLifecycle,
		ActionDeleteBucketLifecycle,
		ActionGetBucketCORS,
		ActionPutBucketCORS,
		ActionDeleteBucketCORS,
		ActionGetBucketTagging,
		ActionPutBucketTagging,
		ActionDeleteBucketTagging,
		ActionGetBucketAcl,
		ActionPutBucketAcl,
		ActionGetBucketPolicy,
		ActionPutBucketPolicy,
		ActionDeleteBucketPolicy,
		ActionGetObject,
		ActionGetObjectVersion,
		ActionGetObjectAcl,
		ActionPutObject,
		ActionPutObjectAcl,
		ActionDeleteObject,
		ActionDeleteObjectVersion,
		ActionGetObjectTagging,
		ActionPutObjectTagging,
		ActionDeleteObjectTagging,
		ActionGetObjectRetention,
		ActionPutObjectRetention,
		ActionGetObjectLegalHold,
		ActionPutObjectLegalHold,
		ActionGetBucketObjectLockConfiguration,
		ActionPutBucketObjectLockConfiguration,
		ActionRestoreObject,
		ActionAbortMultipartUpload,
		ActionListMultipartUploadParts,
		ActionListBucketMultipartUploads,
	})
}

// EffectiveActionsInTenant returns every action a user's policies permit,
func (s *SQLiteStore) EffectiveActionsInTenant(userID string, roles []string, tenantID string) ([]string, error) {
	documents, err := s.EffectivePolicyDocumentsInTenant(userID, roles, tenantID)
	if err != nil {
		return nil, err
	}

	var allowed, denied []string
	for _, raw := range documents {
		policy, err := ParseIAMPolicy(raw, IAMMaxManagedPolicyBytes)
		if err != nil {
			continue
		}
		for _, st := range policy.Statement {
			if st.Effect == EffectDeny {
				denied = append(denied, policyStringValues(st.Action)...)
			} else {
				allowed = append(allowed, policyStringValues(st.Action)...)
			}
		}
	}

	if len(denied) == 0 {
		return allowed, nil
	}

	// A Deny removes the action even from a wildcard: an administrator whose
	// console access was revoked must lose it, and "*" would hand it back.
	kept := make([]string, 0, len(allowed))
	for _, action := range allowed {
		if action == "*" {
			for _, catalogued := range everyCatalogedAction() {
				if !actionMatchesAny(denied, catalogued) {
					kept = append(kept, catalogued)
				}
			}
			continue
		}
		if !actionMatchesAny(denied, action) {
			kept = append(kept, action)
		}
	}
	return kept, nil
}

// actionMatchesAny reports whether any pattern covers the action.
func actionMatchesAny(patterns []string, action string) bool {
	for _, pattern := range patterns {
		if stsWildcardMatch(strings.ToLower(pattern), strings.ToLower(action)) {
			return true
		}
	}
	return false
}

// everyCatalogedAction lists every action the catalogue can express, used to
// expand a wildcard when part of it has been denied.
func everyCatalogedAction() []string {
	var actions []string
	for _, entry := range PolicyCatalog {
		if entry.Name == PolicyFullAccess {
			continue
		}
		actions = append(actions, entry.Actions...)
	}
	return actions
}

// userTenantID reads the tenant a user belongs to, so tenant-wide policies
// reach them.
func (s *SQLiteStore) userTenantID(userID string) (string, error) {
	var tenantID sql.NullString
	err := s.db.QueryRow(`SELECT tenant_id FROM users WHERE id = ?`, userID).Scan(&tenantID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !tenantID.Valid {
		return "", nil
	}
	return tenantID.String, nil
}
