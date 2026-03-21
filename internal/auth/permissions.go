package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// Permission levels
const (
	PermissionLevelRead  = "read"
	PermissionLevelWrite = "write"
	PermissionLevelAdmin = "admin"
)

// PermissionManager defines the interface for bucket permission management
type PermissionManager interface {
	GrantBucketAccess(ctx context.Context, bucketName, userID, tenantID, permissionLevel, grantedBy string, expiresAt int64) error
	GrantGroupBucketAccess(ctx context.Context, bucketName, groupID, permissionLevel, grantedBy string, expiresAt int64) error
	RevokeBucketAccess(ctx context.Context, bucketName, userID, tenantID string) error
	RevokeGroupBucketAccess(ctx context.Context, bucketName, groupID string) error
	CheckBucketAccess(ctx context.Context, bucketName, userID string) (bool, string, error)
	ListBucketPermissions(ctx context.Context, bucketName string) ([]*BucketPermission, error)
	ListUserBucketPermissions(ctx context.Context, userID string) ([]*BucketPermission, error)
}

// GrantBucketAccess grants access to a bucket for a user or tenant
func (s *SQLiteStore) GrantBucketAccess(bucketName, userID, tenantID, permissionLevel, grantedBy string, expiresAt int64) error {
	// Validate permission level
	if permissionLevel != PermissionLevelRead && permissionLevel != PermissionLevelWrite && permissionLevel != PermissionLevelAdmin {
		return fmt.Errorf("invalid permission level: %s", permissionLevel)
	}

	// Must have either userID or tenantID, not both
	if (userID == "" && tenantID == "") || (userID != "" && tenantID != "") {
		return fmt.Errorf("must specify either userID or tenantID, not both")
	}

	permissionID := GeneratePermissionID()
	now := time.Now().Unix()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check if permission already exists
	var existingID string
	err = tx.QueryRow(`
		SELECT id FROM bucket_permissions
		WHERE bucket_name = ? AND (user_id = ? OR tenant_id = ?)
	`, bucketName, nullString(userID), nullString(tenantID)).Scan(&existingID)

	if err == nil {
		// Permission exists, update it
		_, err = tx.Exec(`
			UPDATE bucket_permissions
			SET permission_level = ?, granted_by = ?, granted_at = ?, expires_at = ?
			WHERE id = ?
		`, permissionLevel, grantedBy, now, nullInt64(expiresAt), existingID)
	} else if err == sql.ErrNoRows {
		// Permission doesn't exist, create it
		_, err = tx.Exec(`
			INSERT INTO bucket_permissions (id, bucket_name, user_id, tenant_id, permission_level, granted_by, granted_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, permissionID, bucketName, nullString(userID), nullString(tenantID), permissionLevel, grantedBy, now, nullInt64(expiresAt))
	}

	if err != nil {
		return fmt.Errorf("failed to grant bucket access: %w", err)
	}

	return tx.Commit()
}

// RevokeBucketAccess revokes access to a bucket for a user or tenant
func (s *SQLiteStore) RevokeBucketAccess(bucketName, userID, tenantID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		DELETE FROM bucket_permissions
		WHERE bucket_name = ? AND (user_id = ? OR tenant_id = ?)
	`, bucketName, nullString(userID), nullString(tenantID))

	if err != nil {
		return fmt.Errorf("failed to revoke bucket access: %w", err)
	}

	return tx.Commit()
}

// CheckBucketAccess checks if a user has access to a bucket and returns the permission level.
// Evaluation order: user → user's groups → user's tenant.
func (s *SQLiteStore) CheckBucketAccess(bucketName, userID string) (bool, string, error) {
	// Single query: best matching permission across user, groups, and tenant.
	// Permission precedence (highest first): admin > write > read.
	// We use MAX() on an ordered mapping via CASE to pick the strongest grant.
	var permissionLevel sql.NullString
	var expiresAt sql.NullInt64

	err := s.db.QueryRow(`
		SELECT permission_level, expires_at
		FROM bucket_permissions
		WHERE bucket_name = ?
		  AND (
		    user_id = ?
		    OR group_id IN (SELECT group_id FROM group_members WHERE user_id = ?)
		    OR tenant_id = (SELECT tenant_id FROM users WHERE id = ?)
		  )
		  AND (expires_at IS NULL OR expires_at = 0 OR expires_at > ?)
		ORDER BY CASE permission_level
		    WHEN 'admin' THEN 3
		    WHEN 'write' THEN 2
		    WHEN 'read'  THEN 1
		    ELSE 0
		END DESC
		LIMIT 1
	`, bucketName, userID, userID, userID, time.Now().Unix()).Scan(&permissionLevel, &expiresAt)

	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to check bucket access: %w", err)
	}
	if !permissionLevel.Valid {
		return false, "", nil
	}
	return true, permissionLevel.String, nil
}

// ListBucketPermissions returns all permissions for a bucket
func (s *SQLiteStore) ListBucketPermissions(bucketName string) ([]*BucketPermission, error) {
	rows, err := s.db.Query(`
		SELECT id, bucket_name, user_id, tenant_id, group_id, permission_level, granted_by, granted_at, expires_at
		FROM bucket_permissions
		WHERE bucket_name = ?
		ORDER BY granted_at DESC
	`, bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to list bucket permissions: %w", err)
	}
	defer rows.Close()

	var permissions []*BucketPermission
	for rows.Next() {
		var perm BucketPermission
		var userID, tenantID, groupID sql.NullString
		var expiresAt sql.NullInt64

		err := rows.Scan(
			&perm.ID,
			&perm.BucketName,
			&userID,
			&tenantID,
			&groupID,
			&perm.PermissionLevel,
			&perm.GrantedBy,
			&perm.GrantedAt,
			&expiresAt,
		)
		if err != nil {
			logrus.WithError(err).Error("Failed to scan permission row")
			continue
		}

		if userID.Valid {
			perm.UserID = userID.String
		}
		if tenantID.Valid {
			perm.TenantID = tenantID.String
		}
		if groupID.Valid {
			perm.GroupID = groupID.String
		}
		if expiresAt.Valid {
			perm.ExpiresAt = expiresAt.Int64
		}

		permissions = append(permissions, &perm)
	}

	return permissions, nil
}

// ListUserBucketPermissions returns all bucket permissions that apply to a user:
// direct user grants + group grants + tenant grants.
func (s *SQLiteStore) ListUserBucketPermissions(userID string) ([]*BucketPermission, error) {
	rows, err := s.db.Query(`
		SELECT id, bucket_name, user_id, tenant_id, group_id, permission_level, granted_by, granted_at, expires_at
		FROM bucket_permissions
		WHERE user_id = ?
		   OR group_id IN (SELECT group_id FROM group_members WHERE user_id = ?)
		   OR tenant_id = (SELECT tenant_id FROM users WHERE id = ?)
		ORDER BY granted_at DESC
	`, userID, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list user bucket permissions: %w", err)
	}
	defer rows.Close()

	var permissions []*BucketPermission
	for rows.Next() {
		var perm BucketPermission
		var uid, tid, gid sql.NullString
		var expiresAt sql.NullInt64

		err := rows.Scan(
			&perm.ID,
			&perm.BucketName,
			&uid,
			&tid,
			&gid,
			&perm.PermissionLevel,
			&perm.GrantedBy,
			&perm.GrantedAt,
			&expiresAt,
		)
		if err != nil {
			logrus.WithError(err).Error("Failed to scan permission row")
			continue
		}

		if uid.Valid {
			perm.UserID = uid.String
		}
		if tid.Valid {
			perm.TenantID = tid.String
		}
		if gid.Valid {
			perm.GroupID = gid.String
		}
		if expiresAt.Valid {
			perm.ExpiresAt = expiresAt.Int64
		}

		permissions = append(permissions, &perm)
	}

	return permissions, nil
}

// GrantGroupBucketAccess grants access to a bucket for a group
func (s *SQLiteStore) GrantGroupBucketAccess(bucketName, groupID, permissionLevel, grantedBy string, expiresAt int64) error {
	if permissionLevel != PermissionLevelRead && permissionLevel != PermissionLevelWrite && permissionLevel != PermissionLevelAdmin {
		return fmt.Errorf("invalid permission level: %s", permissionLevel)
	}

	permissionID := GeneratePermissionID()
	now := time.Now().Unix()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existingID string
	err = tx.QueryRow(`SELECT id FROM bucket_permissions WHERE bucket_name = ? AND group_id = ?`,
		bucketName, groupID).Scan(&existingID)

	if err == nil {
		_, err = tx.Exec(`UPDATE bucket_permissions SET permission_level = ?, granted_by = ?, granted_at = ?, expires_at = ? WHERE id = ?`,
			permissionLevel, grantedBy, now, nullInt64(expiresAt), existingID)
	} else if err == sql.ErrNoRows {
		_, err = tx.Exec(`
			INSERT INTO bucket_permissions (id, bucket_name, group_id, permission_level, granted_by, granted_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, permissionID, bucketName, groupID, permissionLevel, grantedBy, now, nullInt64(expiresAt))
	}
	if err != nil {
		return fmt.Errorf("failed to grant group bucket access: %w", err)
	}
	return tx.Commit()
}

// RevokeGroupBucketAccess revokes a group's access to a bucket
func (s *SQLiteStore) RevokeGroupBucketAccess(bucketName, groupID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM bucket_permissions WHERE bucket_name = ? AND group_id = ?`, bucketName, groupID)
	if err != nil {
		return fmt.Errorf("failed to revoke group bucket access: %w", err)
	}
	return tx.Commit()
}

// GrantGroupBucketAccess — authManager wrapper
func (m *authManager) GrantGroupBucketAccess(ctx context.Context, bucketName, groupID, permissionLevel, grantedBy string, expiresAt int64) error {
	return m.store.GrantGroupBucketAccess(bucketName, groupID, permissionLevel, grantedBy, expiresAt)
}

// RevokeGroupBucketAccess — authManager wrapper
func (m *authManager) RevokeGroupBucketAccess(ctx context.Context, bucketName, groupID string) error {
	return m.store.RevokeGroupBucketAccess(bucketName, groupID)
}

// GeneratePermissionID generates a unique permission ID
func GeneratePermissionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "perm-" + hex.EncodeToString(b)
}

// nullInt64 returns interface{} for optional int64 fields
func nullInt64(i int64) interface{} {
	if i == 0 {
		return nil
	}
	return i
}
