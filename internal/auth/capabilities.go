package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
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
	CapConsoleAccess  = "console:access"
	CapKeysManageOwn  = "keys:manage_own"
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

// EffectiveCapability describes a single capability for a user, including its source.
type EffectiveCapability struct {
	Capability string `json:"capability"`
	Granted    bool   `json:"granted"`
	// Source: "role" (from role default) or "override" (admin-set override).
	Source string `json:"source"`
}

// HasCapability returns true if the user identified by userID+roles has the given capability.
// Resolution order:
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

// GetEffectiveCapabilities returns the full capability matrix for a user with source annotation.
func (s *SQLiteStore) GetEffectiveCapabilities(userID string, roles []string) ([]EffectiveCapability, error) {
	// Load all user overrides in one query.
	rows, err := s.db.Query(
		`SELECT capability, granted FROM user_capability_overrides WHERE user_id = ?`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load overrides: %w", err)
	}
	defer rows.Close()

	overrides := make(map[string]bool)
	for rows.Next() {
		var cap string
		var g bool
		if err := rows.Scan(&cap, &g); err != nil {
			return nil, err
		}
		overrides[cap] = g
	}

	isAdmin := false
	for _, r := range roles {
		if r == "admin" {
			isAdmin = true
			break
		}
	}

	// Load role defaults for the user's roles.
	roleDefaults := make(map[string]bool)
	for _, role := range roles {
		rrows, err := s.db.Query(`SELECT capability FROM role_capabilities WHERE role = ?`, role)
		if err != nil {
			return nil, fmt.Errorf("failed to load role capabilities: %w", err)
		}
		for rrows.Next() {
			var cap string
			if err := rrows.Scan(&cap); err != nil {
				rrows.Close()
				return nil, err
			}
			roleDefaults[cap] = true
		}
		rrows.Close()
	}

	result := make([]EffectiveCapability, 0, len(AllCapabilities))
	for _, cap := range AllCapabilities {
		ec := EffectiveCapability{Capability: cap}

		if g, overridden := overrides[cap]; overridden {
			ec.Granted = g
			ec.Source = "override"
		} else if isAdmin || roleDefaults[cap] {
			ec.Granted = true
			ec.Source = "role"
		} else {
			ec.Granted = false
			ec.Source = "role"
		}
		result = append(result, ec)
	}
	return result, nil
}

// SetCapabilityOverride creates or updates a per-user capability override.
func (s *SQLiteStore) SetCapabilityOverride(userID, capability, grantedBy string, granted bool) error {
	id := generateCapabilityID()
	now := time.Now().Unix()
	_, err := s.db.Exec(`
		INSERT INTO user_capability_overrides (id, user_id, capability, granted, granted_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, capability) DO UPDATE SET
			granted    = excluded.granted,
			granted_by = excluded.granted_by,
			created_at = excluded.created_at
	`, id, userID, capability, boolToInt(granted), grantedBy, now)
	return err
}

// DeleteCapabilityOverride removes a per-user override, reverting to role default.
func (s *SQLiteStore) DeleteCapabilityOverride(userID, capability string) error {
	_, err := s.db.Exec(
		`DELETE FROM user_capability_overrides WHERE user_id = ? AND capability = ?`,
		userID, capability,
	)
	return err
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

// GetAllRoleCapabilities returns the full role→capabilities map.
func (s *SQLiteStore) GetAllRoleCapabilities() (map[string][]string, error) {
	rows, err := s.db.Query(`SELECT role, capability FROM role_capabilities ORDER BY role, capability`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var role, cap string
		if err := rows.Scan(&role, &cap); err != nil {
			return nil, err
		}
		result[role] = append(result[role], cap)
	}
	return result, nil
}

// SetRoleCapabilities replaces the full capability set for a role atomically.
func (s *SQLiteStore) SetRoleCapabilities(role string, capabilities []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM role_capabilities WHERE role = ?`, role); err != nil {
		return err
	}
	for _, cap := range capabilities {
		if _, err := tx.Exec(
			`INSERT INTO role_capabilities (role, capability) VALUES (?, ?)`, role, cap,
		); err != nil {
			return fmt.Errorf("failed to insert capability %s: %w", cap, err)
		}
	}
	return tx.Commit()
}

// --- authManager context helper ---

// HasCapability resolves the capability for the user in context. Always returns true for admin users.
func HasCapabilityInContext(ctx context.Context, store interface {
	HasCapability(userID string, roles []string, capability string) (bool, error)
}, capability string) bool {
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return false
	}
	allowed, err := store.HasCapability(user.ID, user.Roles, capability)
	if err != nil {
		return false
	}
	return allowed
}

func generateCapabilityID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "cap-" + hex.EncodeToString(b)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
