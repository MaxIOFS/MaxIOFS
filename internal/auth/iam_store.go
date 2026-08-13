package auth

// SQLite persistence for the IAM entities declared in iam_types.go.

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// --- managed policies ---

// CreateIAMPolicy stores a new managed policy together with its first version.
// The policy and its version are written in one transaction: a policy row whose
// default version does not exist would fail every evaluation that reaches it.
func (s *SQLiteStore) CreateIAMPolicy(policy *IAMPolicy, document string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRow(`SELECT 1 FROM iam_policies WHERE name = ?`, policy.Name).Scan(&exists)
	if err == nil {
		return fmt.Errorf("%w: policy %q", ErrIAMEntityExists, policy.Name)
	}
	if err != sql.ErrNoRows {
		return err
	}

	if _, err := tx.Exec(`
		INSERT INTO iam_policies (name, arn, path, description, default_version_id, is_builtin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, policy.Name, policy.ARN, policy.Path, nullString(policy.Description),
		policy.DefaultVersionID, boolToInt(policy.IsBuiltin), policy.CreatedAt, policy.UpdatedAt); err != nil {
		return fmt.Errorf("failed to create iam policy: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO iam_policy_versions (policy_name, version_id, document, created_at)
		VALUES (?, ?, ?, ?)
	`, policy.Name, policy.DefaultVersionID, document, policy.CreatedAt); err != nil {
		return fmt.Errorf("failed to create iam policy version: %w", err)
	}

	return tx.Commit()
}

// GetIAMPolicy returns a managed policy with Document resolved to its default
// version's document.
func (s *SQLiteStore) GetIAMPolicy(name string) (*IAMPolicy, error) {
	var p IAMPolicy
	var description sql.NullString
	var isBuiltin int
	var document sql.NullString

	err := s.db.QueryRow(`
		SELECT p.name, p.arn, p.path, p.description, p.default_version_id, p.is_builtin,
		       p.created_at, p.updated_at, v.document
		FROM iam_policies p
		LEFT JOIN iam_policy_versions v
		  ON v.policy_name = p.name AND v.version_id = p.default_version_id
		WHERE p.name = ?
	`, name).Scan(&p.Name, &p.ARN, &p.Path, &description, &p.DefaultVersionID, &isBuiltin,
		&p.CreatedAt, &p.UpdatedAt, &document)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: policy %q", ErrIAMNoSuchEntity, name)
	}
	if err != nil {
		return nil, err
	}

	if description.Valid {
		p.Description = description.String
	}
	p.IsBuiltin = isBuiltin == 1

	if document.Valid {
		p.Document = document.String
		return &p, nil
	}

	if err := s.repairDefaultVersion(&p); err != nil {
		return nil, fmt.Errorf("policy %q has no usable version: %w", name, err)
	}
	return &p, nil
}

// repairDefaultVersion points a policy at its newest surviving version.
func (s *SQLiteStore) repairDefaultVersion(p *IAMPolicy) error {
	var versionID, document string
	err := s.db.QueryRow(`
		SELECT version_id, document FROM iam_policy_versions
		WHERE policy_name = ?
		ORDER BY created_at DESC, version_id DESC
		LIMIT 1
	`, p.Name).Scan(&versionID, &document)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: no versions at all", ErrIAMNoSuchEntity)
	}
	if err != nil {
		return err
	}

	if _, err := s.db.Exec(`UPDATE iam_policies SET default_version_id = ? WHERE name = ?`,
		versionID, p.Name); err != nil {
		return err
	}
	p.DefaultVersionID = versionID
	p.Document = document
	return nil
}

// ListIAMPolicies returns every managed policy, documents included.
func (s *SQLiteStore) ListIAMPolicies() ([]*IAMPolicy, error) {
	rows, err := s.db.Query(`
		SELECT p.name, p.arn, p.path, p.description, p.default_version_id, p.is_builtin,
		       p.created_at, p.updated_at, v.document
		FROM iam_policies p
		LEFT JOIN iam_policy_versions v
		  ON v.policy_name = p.name AND v.version_id = p.default_version_id
		ORDER BY p.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*IAMPolicy
	for rows.Next() {
		var p IAMPolicy
		var description, document sql.NullString
		var isBuiltin int
		if err := rows.Scan(&p.Name, &p.ARN, &p.Path, &description, &p.DefaultVersionID,
			&isBuiltin, &p.CreatedAt, &p.UpdatedAt, &document); err != nil {
			return nil, err
		}
		if description.Valid {
			p.Description = description.String
		}
		p.IsBuiltin = isBuiltin == 1
		if document.Valid {
			p.Document = document.String
		}
		out = append(out, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, p := range out {
		if p.Document == "" {
			_ = s.repairDefaultVersion(p)
		}
	}
	return out, nil
}

// DeleteIAMPolicy removes a managed policy, its versions and its attachments.
func (s *SQLiteStore) DeleteIAMPolicy(name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var isBuiltin int
	err = tx.QueryRow(`SELECT is_builtin FROM iam_policies WHERE name = ?`, name).Scan(&isBuiltin)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: policy %q", ErrIAMNoSuchEntity, name)
	}
	if err != nil {
		return err
	}
	if isBuiltin == 1 {
		return fmt.Errorf("%w: policy %q is built in", ErrIAMInvalidInput, name)
	}

	var attached int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM iam_policy_attachments WHERE policy_name = ?`, name).Scan(&attached); err != nil {
		return err
	}
	if attached > 0 {
		return fmt.Errorf("%w: policy %q is attached to %d entities", ErrIAMDeleteConflict, name, attached)
	}

	if _, err := tx.Exec(`DELETE FROM iam_policy_versions WHERE policy_name = ?`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM iam_policies WHERE name = ?`, name); err != nil {
		return err
	}
	return tx.Commit()
}

// --- policy versions ---

// CreateIAMPolicyVersion adds a version and optionally makes it the default.
func (s *SQLiteStore) CreateIAMPolicyVersion(policyName, document string, setDefault bool) (*IAMPolicyVersion, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var currentDefault string
	err = tx.QueryRow(`SELECT default_version_id FROM iam_policies WHERE name = ?`, policyName).Scan(&currentDefault)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: policy %q", ErrIAMNoSuchEntity, policyName)
	}
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(`SELECT version_id FROM iam_policy_versions WHERE policy_name = ?`, policyName)
	if err != nil {
		return nil, err
	}
	count, highest := 0, 0
	for rows.Next() {
		var vid string
		if err := rows.Scan(&vid); err != nil {
			rows.Close()
			return nil, err
		}
		count++
		if n, err := strconv.Atoi(strings.TrimPrefix(vid, "v")); err == nil && n > highest {
			highest = n
		}
	}
	rows.Close()

	if count >= IAMMaxPolicyVersions {
		return nil, fmt.Errorf("%w: policy %q already has %d versions", ErrIAMLimitExceeded, policyName, count)
	}

	now := time.Now().Unix()
	version := &IAMPolicyVersion{
		PolicyName: policyName,
		VersionID:  "v" + strconv.Itoa(highest+1),
		Document:   document,
		IsDefault:  setDefault,
		CreatedAt:  now,
	}

	if _, err := tx.Exec(`
		INSERT INTO iam_policy_versions (policy_name, version_id, document, created_at)
		VALUES (?, ?, ?, ?)
	`, version.PolicyName, version.VersionID, version.Document, now); err != nil {
		return nil, err
	}

	if setDefault {
		if _, err := tx.Exec(`UPDATE iam_policies SET default_version_id = ?, updated_at = ? WHERE name = ?`,
			version.VersionID, now, policyName); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return version, nil
}

// GetIAMPolicyVersion returns one version of a managed policy.
func (s *SQLiteStore) GetIAMPolicyVersion(policyName, versionID string) (*IAMPolicyVersion, error) {
	var v IAMPolicyVersion
	var defaultVersion string
	err := s.db.QueryRow(`
		SELECT v.policy_name, v.version_id, v.document, v.created_at, p.default_version_id
		FROM iam_policy_versions v
		JOIN iam_policies p ON p.name = v.policy_name
		WHERE v.policy_name = ? AND v.version_id = ?
	`, policyName, versionID).Scan(&v.PolicyName, &v.VersionID, &v.Document, &v.CreatedAt, &defaultVersion)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: policy %q version %q", ErrIAMNoSuchEntity, policyName, versionID)
	}
	if err != nil {
		return nil, err
	}
	v.IsDefault = v.VersionID == defaultVersion
	return &v, nil
}

// ListIAMPolicyVersions returns every version of a managed policy, newest first.
func (s *SQLiteStore) ListIAMPolicyVersions(policyName string) ([]*IAMPolicyVersion, error) {
	var defaultVersion string
	err := s.db.QueryRow(`SELECT default_version_id FROM iam_policies WHERE name = ?`, policyName).Scan(&defaultVersion)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: policy %q", ErrIAMNoSuchEntity, policyName)
	}
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`
		SELECT policy_name, version_id, document, created_at
		FROM iam_policy_versions
		WHERE policy_name = ?
		ORDER BY created_at DESC, version_id DESC
	`, policyName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*IAMPolicyVersion
	for rows.Next() {
		var v IAMPolicyVersion
		if err := rows.Scan(&v.PolicyName, &v.VersionID, &v.Document, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.IsDefault = v.VersionID == defaultVersion
		out = append(out, &v)
	}
	return out, rows.Err()
}

// DeleteIAMPolicyVersion removes a non-default version.
func (s *SQLiteStore) DeleteIAMPolicyVersion(policyName, versionID string) error {
	var defaultVersion string
	err := s.db.QueryRow(`SELECT default_version_id FROM iam_policies WHERE name = ?`, policyName).Scan(&defaultVersion)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: policy %q", ErrIAMNoSuchEntity, policyName)
	}
	if err != nil {
		return err
	}
	if versionID == defaultVersion {
		return fmt.Errorf("%w: cannot delete the default version of %q", ErrIAMDeleteConflict, policyName)
	}

	res, err := s.db.Exec(`DELETE FROM iam_policy_versions WHERE policy_name = ? AND version_id = ?`, policyName, versionID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: policy %q version %q", ErrIAMNoSuchEntity, policyName, versionID)
	}
	return nil
}

// SetDefaultIAMPolicyVersion points a policy at an existing version.
func (s *SQLiteStore) SetDefaultIAMPolicyVersion(policyName, versionID string) error {
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM iam_policy_versions WHERE policy_name = ? AND version_id = ?`,
		policyName, versionID).Scan(&exists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: policy %q version %q", ErrIAMNoSuchEntity, policyName, versionID)
	}
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE iam_policies SET default_version_id = ?, updated_at = ? WHERE name = ?`,
		versionID, time.Now().Unix(), policyName)
	return err
}

// --- attachments ---

// AttachIAMPolicy attaches a managed policy to a user, group or role.
func (s *SQLiteStore) AttachIAMPolicy(policyName, targetType, targetID string) error {
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM iam_policies WHERE name = ?`, policyName).Scan(&exists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: policy %q", ErrIAMNoSuchEntity, policyName)
	}
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO iam_policy_attachments (policy_name, target_type, target_id, attached_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(policy_name, target_type, target_id) DO NOTHING
	`, policyName, targetType, targetID, time.Now().Unix())
	return err
}

// DetachIAMPolicy removes an attachment. Detaching something that is not
// attached is not an error: the caller's intent — "this policy must not apply
// to this identity" — is satisfied either way.
func (s *SQLiteStore) DetachIAMPolicy(policyName, targetType, targetID string) error {
	_, err := s.db.Exec(`
		DELETE FROM iam_policy_attachments
		WHERE policy_name = ? AND target_type = ? AND target_id = ?
	`, policyName, targetType, targetID)
	return err
}

// ListAttachedIAMPolicyNames returns the managed policies attached to a target.
func (s *SQLiteStore) ListAttachedIAMPolicyNames(targetType, targetID string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT policy_name FROM iam_policy_attachments
		WHERE target_type = ? AND target_id = ?
		ORDER BY policy_name
	`, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// CountIAMPolicyAttachments returns how many entities a managed policy is
// attached to. Reported in IAM responses, where a client reads it to know
// whether the policy can be deleted.
func (s *SQLiteStore) CountIAMPolicyAttachments(policyName string) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM iam_policy_attachments WHERE policy_name = ?`, policyName).Scan(&n)
	return n, err
}

// --- inline policies ---

// PutIAMInlinePolicy creates or replaces an inline policy on a target.
func (s *SQLiteStore) PutIAMInlinePolicy(targetType, targetID, name, document string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`
		INSERT INTO iam_inline_policies (target_type, target_id, name, document, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(target_type, target_id, name) DO UPDATE SET
			document   = excluded.document,
			updated_at = excluded.updated_at
	`, targetType, targetID, name, document, now, now)
	return err
}

// GetIAMInlinePolicy returns one inline policy.
func (s *SQLiteStore) GetIAMInlinePolicy(targetType, targetID, name string) (*IAMInlinePolicy, error) {
	var p IAMInlinePolicy
	err := s.db.QueryRow(`
		SELECT target_type, target_id, name, document, created_at, updated_at
		FROM iam_inline_policies
		WHERE target_type = ? AND target_id = ? AND name = ?
	`, targetType, targetID, name).Scan(&p.TargetType, &p.TargetID, &p.Name, &p.Document, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: inline policy %q", ErrIAMNoSuchEntity, name)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListIAMInlinePolicies returns every inline policy on a target.
func (s *SQLiteStore) ListIAMInlinePolicies(targetType, targetID string) ([]*IAMInlinePolicy, error) {
	rows, err := s.db.Query(`
		SELECT target_type, target_id, name, document, created_at, updated_at
		FROM iam_inline_policies
		WHERE target_type = ? AND target_id = ?
		ORDER BY name
	`, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*IAMInlinePolicy
	for rows.Next() {
		var p IAMInlinePolicy
		if err := rows.Scan(&p.TargetType, &p.TargetID, &p.Name, &p.Document, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// DeleteIAMInlinePolicy removes one inline policy.
func (s *SQLiteStore) DeleteIAMInlinePolicy(targetType, targetID, name string) error {
	res, err := s.db.Exec(`
		DELETE FROM iam_inline_policies
		WHERE target_type = ? AND target_id = ? AND name = ?
	`, targetType, targetID, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: inline policy %q", ErrIAMNoSuchEntity, name)
	}
	return nil
}

// DeleteIAMPoliciesForTarget drops every inline policy and attachment belonging
// to a target. Called when the target itself is deleted, so a later entity that
// reuses the name cannot inherit the dead one's permissions.
func (s *SQLiteStore) DeleteIAMPoliciesForTarget(targetType, targetID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM iam_inline_policies WHERE target_type = ? AND target_id = ?`,
		targetType, targetID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM iam_policy_attachments WHERE target_type = ? AND target_id = ?`,
		targetType, targetID); err != nil {
		return err
	}
	return tx.Commit()
}

// --- roles ---

// CreateIAMRole stores a new role.
func (s *SQLiteStore) CreateIAMRole(role *IAMRole) error {
	_, err := s.db.Exec(`
		INSERT INTO iam_roles (name, arn, path, description, assume_role_policy, max_session_duration, tenant_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, role.Name, role.ARN, role.Path, nullString(role.Description), role.AssumeRolePolicy,
		role.MaxSessionDuration, nullString(role.TenantID), role.CreatedAt, role.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("%w: role %q", ErrIAMEntityExists, role.Name)
		}
		return fmt.Errorf("failed to create iam role: %w", err)
	}
	return nil
}

// GetIAMRole returns a role by name.
func (s *SQLiteStore) GetIAMRole(name string) (*IAMRole, error) {
	return s.scanIAMRole(s.db.QueryRow(`
		SELECT name, arn, path, description, assume_role_policy, max_session_duration, tenant_id, created_at, updated_at
		FROM iam_roles WHERE name = ?
	`, name), name)
}

// GetIAMRoleByARN resolves the RoleArn an AssumeRole caller sent.
func (s *SQLiteStore) GetIAMRoleByARN(arn string) (*IAMRole, error) {
	return s.scanIAMRole(s.db.QueryRow(`
		SELECT name, arn, path, description, assume_role_policy, max_session_duration, tenant_id, created_at, updated_at
		FROM iam_roles WHERE arn = ?
	`, arn), arn)
}

func (s *SQLiteStore) scanIAMRole(row *sql.Row, ident string) (*IAMRole, error) {
	var r IAMRole
	var description, tenantID, trust sql.NullString
	err := row.Scan(&r.Name, &r.ARN, &r.Path, &description, &trust,
		&r.MaxSessionDuration, &tenantID, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: role %q", ErrIAMNoSuchEntity, ident)
	}
	if err != nil {
		return nil, err
	}
	if description.Valid {
		r.Description = description.String
	}
	if trust.Valid {
		r.AssumeRolePolicy = trust.String
	}
	if tenantID.Valid {
		r.TenantID = tenantID.String
	}
	return &r, nil
}

// ListIAMRoles returns every role.
func (s *SQLiteStore) ListIAMRoles() ([]*IAMRole, error) {
	rows, err := s.db.Query(`
		SELECT name, arn, path, description, assume_role_policy, max_session_duration, tenant_id, created_at, updated_at
		FROM iam_roles ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*IAMRole
	for rows.Next() {
		var r IAMRole
		var description, tenantID, trust sql.NullString
		if err := rows.Scan(&r.Name, &r.ARN, &r.Path, &description, &trust,
			&r.MaxSessionDuration, &tenantID, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if description.Valid {
			r.Description = description.String
		}
		if trust.Valid {
			r.AssumeRolePolicy = trust.String
		}
		if tenantID.Valid {
			r.TenantID = tenantID.String
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// UpdateIAMRole rewrites a role's mutable fields.
func (s *SQLiteStore) UpdateIAMRole(role *IAMRole) error {
	res, err := s.db.Exec(`
		UPDATE iam_roles
		SET description = ?, assume_role_policy = ?, max_session_duration = ?, updated_at = ?
		WHERE name = ?
	`, nullString(role.Description), role.AssumeRolePolicy, role.MaxSessionDuration,
		time.Now().Unix(), role.Name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: role %q", ErrIAMNoSuchEntity, role.Name)
	}
	return nil
}

// DeleteIAMRole removes a role along with its policies.
func (s *SQLiteStore) DeleteIAMRole(name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`DELETE FROM iam_roles WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: role %q", ErrIAMNoSuchEntity, name)
	}
	if _, err := tx.Exec(`DELETE FROM iam_inline_policies WHERE target_type = ? AND target_id = ?`,
		IAMTargetRole, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM iam_policy_attachments WHERE target_type = ? AND target_id = ?`,
		IAMTargetRole, name); err != nil {
		return err
	}
	return tx.Commit()
}

// --- effective documents ---

// IAMEffectiveDocuments returns every policy document that applies to a target:
// its inline policies plus the default version of each attached managed policy.
// Order is irrelevant — evaluation is Deny-wins over the whole set.
func (s *SQLiteStore) IAMEffectiveDocuments(targetType, targetID string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT document FROM iam_inline_policies WHERE target_type = ? AND target_id = ?
		UNION ALL
		SELECT v.document
		FROM iam_policy_attachments a
		JOIN iam_policies p ON p.name = a.policy_name
		JOIN iam_policy_versions v ON v.policy_name = p.name AND v.version_id = p.default_version_id
		WHERE a.target_type = ? AND a.target_id = ?
	`, targetType, targetID, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

// IAMEffectiveDocumentsForUser returns the documents that apply to a user:
// their own, plus those of every group they belong to. Group membership is how
// AWS scales permissions across identities and Veeam's flow relies on it.
func (s *SQLiteStore) IAMEffectiveDocumentsForUser(userID string) ([]string, error) {
	docs, err := s.IAMEffectiveDocuments(IAMTargetUser, userID)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`SELECT group_id FROM group_members WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	var groupIDs []string
	for rows.Next() {
		var gid string
		if err := rows.Scan(&gid); err != nil {
			rows.Close()
			return nil, err
		}
		groupIDs = append(groupIDs, gid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, gid := range groupIDs {
		groupDocs, err := s.IAMEffectiveDocuments(IAMTargetGroup, gid)
		if err != nil {
			return nil, err
		}
		docs = append(docs, groupDocs...)
	}
	return docs, nil
}

// --- built-in policies ---

// EnsureBuiltinIAMPolicies seeds the shipped policies. It only inserts what is
// missing: an operator who edited a built-in document keeps their edit across
// restarts, which would not be true if this rewrote them every boot.
func (s *SQLiteStore) EnsureBuiltinIAMPolicies() error {
	now := time.Now().Unix()
	for _, b := range builtinIAMPolicies {
		var exists int
		err := s.db.QueryRow(`SELECT 1 FROM iam_policies WHERE name = ?`, b.Name).Scan(&exists)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		policy := &IAMPolicy{
			Name:             b.Name,
			ARN:              IAMPolicyARN(b.Name),
			Path:             "/",
			Description:      b.Description,
			DefaultVersionID: "v1",
			IsBuiltin:        true,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := s.CreateIAMPolicy(policy, b.Document); err != nil {
			return fmt.Errorf("failed to seed built-in policy %s: %w", b.Name, err)
		}
	}
	return nil
}
