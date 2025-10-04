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
	RevokeBucketAccess(ctx context.Context, bucketName, userID, tenantID string) error
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

// CheckBucketAccess checks if a user has access to a bucket and returns the permission level
func (s *SQLiteStore) CheckBucketAccess(bucketName, userID string) (bool, string, error) {
	var permissionLevel string
	var expiresAt sql.NullInt64

	// Check user permission
	err := s.db.QueryRow(`
		SELECT permission_level, expires_at FROM bucket_permissions
		WHERE bucket_name = ? AND user_id = ?
	`, bucketName, userID).Scan(&permissionLevel, &expiresAt)

	if err == nil {
		// Check if permission has expired
		if expiresAt.Valid && expiresAt.Int64 > 0 && time.Now().Unix() > expiresAt.Int64 {
			return false, "", nil
		}
		return true, permissionLevel, nil
	}

	if err != sql.ErrNoRows {
		return false, "", fmt.Errorf("failed to check bucket access: %w", err)
	}

	// Check tenant permission
	var tenantID sql.NullString
	err = s.db.QueryRow(`SELECT tenant_id FROM users WHERE id = ?`, userID).Scan(&tenantID)
	if err != nil {
		return false, "", fmt.Errorf("failed to get user tenant: %w", err)
	}

	if tenantID.Valid && tenantID.String != "" {
		err = s.db.QueryRow(`
			SELECT permission_level, expires_at FROM bucket_permissions
			WHERE bucket_name = ? AND tenant_id = ?
		`, bucketName, tenantID.String).Scan(&permissionLevel, &expiresAt)

		if err == nil {
			// Check if permission has expired
			if expiresAt.Valid && expiresAt.Int64 > 0 && time.Now().Unix() > expiresAt.Int64 {
				return false, "", nil
			}
			return true, permissionLevel, nil
		}

		if err != sql.ErrNoRows {
			return false, "", fmt.Errorf("failed to check tenant bucket access: %w", err)
		}
	}

	return false, "", nil
}

// ListBucketPermissions returns all permissions for a bucket
func (s *SQLiteStore) ListBucketPermissions(bucketName string) ([]*BucketPermission, error) {
	rows, err := s.db.Query(`
		SELECT id, bucket_name, user_id, tenant_id, permission_level, granted_by, granted_at, expires_at
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
		var userID, tenantID sql.NullString
		var expiresAt sql.NullInt64

		err := rows.Scan(
			&perm.ID,
			&perm.BucketName,
			&userID,
			&tenantID,
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
		if expiresAt.Valid {
			perm.ExpiresAt = expiresAt.Int64
		}

		permissions = append(permissions, &perm)
	}

	return permissions, nil
}

// ListUserBucketPermissions returns all bucket permissions for a user
func (s *SQLiteStore) ListUserBucketPermissions(userID string) ([]*BucketPermission, error) {
	// Get user's tenant
	var tenantID sql.NullString
	err := s.db.QueryRow(`SELECT tenant_id FROM users WHERE id = ?`, userID).Scan(&tenantID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get user tenant: %w", err)
	}

	query := `
		SELECT id, bucket_name, user_id, tenant_id, permission_level, granted_by, granted_at, expires_at
		FROM bucket_permissions
		WHERE user_id = ?`

	if tenantID.Valid && tenantID.String != "" {
		query += ` OR tenant_id = ?`
	}

	query += ` ORDER BY granted_at DESC`

	var rows *sql.Rows
	if tenantID.Valid && tenantID.String != "" {
		rows, err = s.db.Query(query, userID, tenantID.String)
	} else {
		rows, err = s.db.Query(query, userID)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list user bucket permissions: %w", err)
	}
	defer rows.Close()

	var permissions []*BucketPermission
	for rows.Next() {
		var perm BucketPermission
		var uid, tid sql.NullString
		var expiresAt sql.NullInt64

		err := rows.Scan(
			&perm.ID,
			&perm.BucketName,
			&uid,
			&tid,
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
		if expiresAt.Valid {
			perm.ExpiresAt = expiresAt.Int64
		}

		permissions = append(permissions, &perm)
	}

	return permissions, nil
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
