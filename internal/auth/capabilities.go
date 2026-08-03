package auth

// What remains of the capability model: the reads the one-time conversion needs
// to turn an existing installation's permissions into IAM policies.
//
// Nothing here is consulted to authorize a request. Permissions are policies —
// see policy_set.go for how a decision is made, and policy_migration.go for how
// these rows become policies on the boot that upgrades an installation.

import (
	"database/sql"
	"fmt"
)

// Capability constants — service-level actions controlled independently of bucket_permissions.
const (
	// Bucket management
	CapBucketCreate       = "bucket:create"
	CapBucketDelete       = "bucket:delete"
	CapBucketConfigure    = "bucket:configure"
	CapBucketManagePolicy = "bucket:manage_policy"

	// Object operations
	CapObjectUpload         = "object:upload"
	CapObjectDownload       = "object:download"
	CapObjectDelete         = "object:delete"
	CapObjectManageTags     = "object:manage_tags"
	CapObjectManageVersions = "object:manage_versions"

	// Console & API access
	CapConsoleAccess = "console:access"
	CapKeysManageOwn = "keys:manage_own"

	// CapIAMManage gates the AWS IAM protocol surface: creating identities,
	// their credentials, policies and roles. It is the authority to hand out
	// access, so it is not granted by any role default — an administrator has
	// it through the admin safety net, and anyone else needs an explicit
	// override.
	CapIAMManage = "iam:manage"
)

// AllCapabilities lists every known capability in display order.
var AllCapabilities = []string{
	CapBucketCreate,
	CapBucketDelete,
	CapBucketConfigure,
	CapBucketManagePolicy,
	CapObjectUpload,
	CapObjectDownload,
	CapObjectDelete,
	CapObjectManageTags,
	CapObjectManageVersions,
	CapConsoleAccess,
	CapKeysManageOwn,
	CapIAMManage,
}

// CapabilityOverride represents a per-user capability override set by an admin.
type CapabilityOverride struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	Capability string `json:"capability"`
	Granted    bool   `json:"granted"` // true = explicit allow, false = explicit deny
	GrantedBy  string `json:"granted_by"`
	CreatedAt  int64  `json:"created_at"`
}

// HasCapability answers the question the pre-IAM model answered, and exists so
// the equivalence tests can compare it against what the policy evaluator
// decides. Nothing in production calls it: a request is authorized by
// authManager.HasCapability, which reads the user's policies.
//
// Resolution order (the old one, reproduced faithfully):
//  1. Explicit admin deny  → false (deny always wins)
//  2. Explicit admin grant → true
//  3. Role default         → true if the role includes this capability
//  4. role == "admin"      → true (safety net: admin always has everything)
//  5. → false
func (s *SQLiteStore) HasCapability(userID string, roles []string, capability string) (bool, error) {
	// Check user-level overrides first.
	var granted sql.NullBool
	err := s.db.QueryRow(
		`SELECT granted FROM user_capability_overrides WHERE user_id = ? AND capability = ?`,
		userID, capability,
	).Scan(&granted)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check capability override: %w", err)
	}
	if err == nil && granted.Valid {
		return granted.Bool, nil
	}

	// Admin role is always allowed regardless of role_capabilities table.
	for _, r := range roles {
		if r == "admin" {
			return true, nil
		}
	}

	// Check role defaults.
	for _, role := range roles {
		var exists int
		err := s.db.QueryRow(
			`SELECT 1 FROM role_capabilities WHERE role = ? AND capability = ?`,
			role, capability,
		).Scan(&exists)
		if err == nil && exists == 1 {
			return true, nil
		}
	}

	return false, nil
}

// ListUserCapabilityOverrides returns all explicit overrides for a user.
func (s *SQLiteStore) ListUserCapabilityOverrides(userID string) ([]*CapabilityOverride, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, capability, granted, granted_by, created_at
		FROM user_capability_overrides
		WHERE user_id = ?
		ORDER BY capability
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*CapabilityOverride
	for rows.Next() {
		o := &CapabilityOverride{}
		var grantedInt int
		if err := rows.Scan(&o.ID, &o.UserID, &o.Capability, &grantedInt, &o.GrantedBy, &o.CreatedAt); err != nil {
			return nil, err
		}
		o.Granted = grantedInt == 1
		list = append(list, o)
	}
	return list, nil
}

// --- Role capabilities ---

// GetRoleCapabilities returns all capabilities assigned to a role.
func (s *SQLiteStore) GetRoleCapabilities(role string) ([]string, error) {
	rows, err := s.db.Query(`SELECT capability FROM role_capabilities WHERE role = ? ORDER BY capability`, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var caps []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		caps = append(caps, c)
	}
	return caps, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
