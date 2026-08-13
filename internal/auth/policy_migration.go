package auth

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// NeedsLegacyConversion reports whether the permission model has been converted
func (s *SQLiteStore) NeedsLegacyConversion() (bool, error) {
	var attachments int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM iam_policy_attachments WHERE target_type = ?`,
		IAMTargetRole).Scan(&attachments); err != nil {
		return false, err
	}
	return attachments == 0, nil
}

// ConvertLegacyPermissions converts the stored permission model into IAM
func (s *SQLiteStore) ConvertLegacyPermissions() error {
	if err := s.convertRoleGlobalActions(); err != nil {
		return fmt.Errorf("failed to convert role capabilities: %w", err)
	}

	users, err := s.ListUsers()
	if err != nil {
		return err
	}
	for _, user := range users {
		if err := s.convertUserPermissions(user); err != nil {
			return fmt.Errorf("failed to convert permissions for %s: %w", user.Username, err)
		}
	}
	return nil
}

// convertRoleGlobalActions attaches to each role the permissions that name no
func (s *SQLiteStore) convertRoleGlobalActions() error {
	rows, err := s.db.Query(`SELECT role, capability FROM role_capabilities`)
	if err != nil {
		return err
	}

	byRole := make(map[string]map[string]bool)
	for rows.Next() {
		var role, capability string
		if err := rows.Scan(&role, &capability); err != nil {
			rows.Close()
			return err
		}
		for _, name := range PoliciesForCapability(capability) {
			entry, ok := CatalogEntry(name)
			if !ok {
				continue
			}
			if global, _ := splitRoleActions(entry.Actions); len(global) == 0 {
				continue
			}
			if byRole[role] == nil {
				byRole[role] = make(map[string]bool)
			}
			byRole[role][name] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	byRole[RoleAdmin] = map[string]bool{PolicyFullAccess: true}

	for role := range byRole {
		if err := s.ensureAssignableRole(role); err != nil {
			return err
		}
	}

	for role, policies := range byRole {
		for name := range policies {
			if err := s.ensureCataloguePolicy(name); err != nil {
				return err
			}
			if err := s.AttachIAMPolicy(name, IAMTargetRole, role); err != nil {
				return err
			}
		}
	}
	return nil
}

// convertUserPermissions writes what one user could actually do: their role's
// operations crossed with the buckets they were granted, plus their explicit
// overrides.
func (s *SQLiteStore) convertUserPermissions(user *User) error {
	actions, err := s.legacyUserActions(user)
	if err != nil {
		return err
	}
	_, scoped := splitRoleActions(actions)
	if len(scoped) == 0 {
		return s.convertUserDenies(user)
	}

	// The admin role reached every bucket without a permission row, so its
	// grant is not scoped to anything.
	if containsAction(scoped, "*") {
		return s.convertUserDenies(user)
	}

	permissions, err := s.ListUserBucketPermissions(user.ID)
	if err != nil {
		return err
	}
	for _, permission := range permissions {
		if permission.ExpiresAt != 0 && permission.ExpiresAt <= time.Now().Unix() {
			continue
		}
		granted := intersectActions(levelActions(permission.PermissionLevel), scoped)
		document := bucketGrantDocument(permission.BucketName, granted)
		if document == "" {
			continue
		}
		if err := s.PutIAMInlinePolicy(IAMTargetUser, user.ID,
			"bucket-"+permission.BucketName, document); err != nil {
			return err
		}
	}

	return s.convertUserDenies(user)
}

// legacyUserActions returns the actions a user's roles permitted, widened by
// any capability explicitly granted to them.
func (s *SQLiteStore) legacyUserActions(user *User) ([]string, error) {
	var actions []string
	for _, role := range user.Roles {
		if role == RoleAdmin {
			return []string{"*"}, nil
		}
		capabilities, err := s.GetRoleCapabilities(role)
		if err != nil {
			return nil, err
		}
		for _, capability := range capabilities {
			for _, name := range PoliciesForCapability(capability) {
				if entry, ok := CatalogEntry(name); ok {
					actions = append(actions, entry.Actions...)
				}
			}
		}
	}

	overrides, err := s.ListUserCapabilityOverrides(user.ID)
	if err != nil {
		return nil, err
	}
	for _, override := range overrides {
		if !override.Granted {
			continue
		}
		for _, name := range PoliciesForCapability(override.Capability) {
			if entry, ok := CatalogEntry(name); ok {
				actions = append(actions, entry.Actions...)
			}
		}
	}
	return actions, nil
}

// convertUserDenies writes each revoked capability as a Deny, which keeps
// winning over whatever the roles and grants allow.
func (s *SQLiteStore) convertUserDenies(user *User) error {
	overrides, err := s.ListUserCapabilityOverrides(user.ID)
	if err != nil {
		return err
	}
	for _, override := range overrides {
		if override.Granted {
			continue
		}
		document := capabilityDocument(override.Capability, false)
		if document == "" {
			continue
		}
		name := "deny-" + strings.ReplaceAll(override.Capability, ":", "-")
		if err := s.PutIAMInlinePolicy(IAMTargetUser, user.ID, name, document); err != nil {
			return err
		}
	}
	return nil
}

// EnsureAssignableRoles makes sure every role users can hold exists as a role
func (s *SQLiteStore) EnsureAssignableRoles() error {
	rows, err := s.db.Query(`SELECT DISTINCT role FROM role_capabilities`)
	if err != nil {
		return err
	}

	names := map[string]bool{RoleAdmin: true, RoleTenantAdmin: true}
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			rows.Close()
			return err
		}
		names[role] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Roles a user already holds, in case one was never in the capability table.
	userRows, err := s.db.Query(`SELECT roles FROM users WHERE status != 'deleted'`)
	if err != nil {
		return err
	}
	for userRows.Next() {
		var rolesJSON string
		if err := userRows.Scan(&rolesJSON); err != nil {
			userRows.Close()
			return err
		}
		var held []string
		if err := json.Unmarshal([]byte(rolesJSON), &held); err == nil {
			for _, role := range held {
				if role != "" {
					names[role] = true
				}
			}
		}
	}
	userRows.Close()

	for role := range names {
		if err := s.ensureAssignableRole(role); err != nil {
			return err
		}
	}
	return nil
}

// ensureAssignableRole creates the role entity for a role users are assigned.
// It carries no trust policy: nobody assumes it, they hold it.
func (s *SQLiteStore) ensureAssignableRole(role string) error {
	if _, err := s.GetIAMRole(role); err == nil {
		return nil
	}
	now := time.Now().Unix()
	return s.CreateIAMRole(&IAMRole{
		Name:               role,
		ARN:                IAMRoleARN(role),
		Path:               "/",
		Description:        "Assigned to users directly",
		MaxSessionDuration: IAMDefaultMaxSessionDuration,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
}

// ensureCataloguePolicy creates a shipped policy if it is not stored yet, so an
// attachment always points at something that exists.
func (s *SQLiteStore) ensureCataloguePolicy(name string) error {
	if _, err := s.GetIAMPolicy(name); err == nil {
		return nil
	}

	entry, ok := CatalogEntry(name)
	if !ok {
		return fmt.Errorf("%w: unknown catalogue policy %q", ErrIAMInvalidInput, name)
	}

	now := time.Now().Unix()
	return s.CreateIAMPolicy(&IAMPolicy{
		Name:             entry.Name,
		ARN:              IAMPolicyARN(entry.Name),
		Path:             "/",
		Description:      entry.Description,
		DefaultVersionID: "v1",
		IsBuiltin:        true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, entry.Document())
}

// GrantBucketOwnerPolicy records a bucket's owner as a policy on that owner.
func (s *SQLiteStore) GrantBucketOwnerPolicy(bucketName, ownerType, ownerID string) error {
	if ownerType != "" && ownerType != "user" {
		return fmt.Errorf("bucket owner must be a user, got %q", ownerType)
	}
	targetType := IAMTargetUser

	entry, _ := CatalogEntry(PolicyFullAccess)
	document := bucketGrantDocument(bucketName, entry.Actions)
	if document == "" {
		return nil
	}
	return s.PutIAMInlinePolicy(targetType, ownerID, "owner-"+bucketName, document)
}

// RevokeBucketPolicies removes every policy naming a bucket, so deleting one
// does not leave grants behind that a later bucket of the same name inherits.
func (s *SQLiteStore) RevokeBucketPolicies(bucketName string) ([]InlinePolicyRef, error) {
	names := []string{"owner-" + bucketName, "bucket-" + bucketName}

	rows, err := s.db.Query(
		`SELECT target_type, target_id, name FROM iam_inline_policies WHERE name IN (?, ?)`,
		names[0], names[1])
	if err != nil {
		return nil, err
	}

	var revoked []InlinePolicyRef
	for rows.Next() {
		var ref InlinePolicyRef
		if err := rows.Scan(&ref.TargetType, &ref.TargetID, &ref.Name); err != nil {
			rows.Close()
			return nil, err
		}
		revoked = append(revoked, ref)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if _, err := s.db.Exec(
		`DELETE FROM iam_inline_policies WHERE name IN (?, ?)`,
		names[0], names[1]); err != nil {
		return nil, err
	}
	return revoked, nil
}

// InlinePolicyRef identifies one inline policy, which is what a tombstone needs
// to name.
type InlinePolicyRef struct {
	TargetType string
	TargetID   string
	Name       string
}
