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

// =============================================================================
// SQLiteStore — Group CRUD
// =============================================================================

func (s *SQLiteStore) CreateGroup(group *Group) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO groups (id, name, display_name, description, tenant_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, group.ID, group.Name, group.DisplayName, group.Description,
		nullString(group.TenantID), group.CreatedAt, group.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create group: %w", err)
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetGroup(groupID string) (*Group, error) {
	var g Group
	var tenantID sql.NullString
	err := s.db.QueryRow(`
		SELECT id, name, display_name, description, tenant_id, created_at, updated_at
		FROM groups WHERE id = ?
	`, groupID).Scan(&g.ID, &g.Name, &g.DisplayName, &g.Description, &tenantID, &g.CreatedAt, &g.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	if tenantID.Valid {
		g.TenantID = tenantID.String
	}
	// Populate member count
	s.db.QueryRow(`SELECT COUNT(*) FROM group_members WHERE group_id = ?`, groupID).Scan(&g.MemberCount)
	return &g, nil
}

func (s *SQLiteStore) GetGroupByName(name, tenantID string) (*Group, error) {
	var g Group
	var tid sql.NullString
	var err error
	if tenantID == "" {
		err = s.db.QueryRow(`
			SELECT id, name, display_name, description, tenant_id, created_at, updated_at
			FROM groups WHERE name = ? AND tenant_id IS NULL
		`, name).Scan(&g.ID, &g.Name, &g.DisplayName, &g.Description, &tid, &g.CreatedAt, &g.UpdatedAt)
	} else {
		err = s.db.QueryRow(`
			SELECT id, name, display_name, description, tenant_id, created_at, updated_at
			FROM groups WHERE name = ? AND tenant_id = ?
		`, name, tenantID).Scan(&g.ID, &g.Name, &g.DisplayName, &g.Description, &tid, &g.CreatedAt, &g.UpdatedAt)
	}
	if err == sql.ErrNoRows {
		return nil, ErrGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	if tid.Valid {
		g.TenantID = tid.String
	}
	return &g, nil
}

func (s *SQLiteStore) UpdateGroup(group *Group) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE groups SET display_name = ?, description = ?, updated_at = ?
		WHERE id = ?
	`, group.DisplayName, group.Description, group.UpdatedAt, group.ID)
	if err != nil {
		return fmt.Errorf("failed to update group: %w", err)
	}
	return tx.Commit()
}

func (s *SQLiteStore) DeleteGroup(groupID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err = tx.Exec(`DELETE FROM group_members WHERE group_id = ?`, groupID); err != nil {
		return fmt.Errorf("failed to delete group members: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM bucket_permissions WHERE group_id = ?`, groupID); err != nil {
		return fmt.Errorf("failed to delete group permissions: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM groups WHERE id = ?`, groupID); err != nil {
		return fmt.Errorf("failed to delete group: %w", err)
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListGroups(tenantID string) ([]*Group, error) {
	var rows *sql.Rows
	var err error
	if tenantID == "" {
		rows, err = s.db.Query(`
			SELECT g.id, g.name, g.display_name, g.description, g.tenant_id, g.created_at, g.updated_at,
			       COUNT(gm.user_id) AS member_count
			FROM groups g
			LEFT JOIN group_members gm ON gm.group_id = g.id
			WHERE g.tenant_id IS NULL
			GROUP BY g.id ORDER BY g.name
		`)
	} else {
		rows, err = s.db.Query(`
			SELECT g.id, g.name, g.display_name, g.description, g.tenant_id, g.created_at, g.updated_at,
			       COUNT(gm.user_id) AS member_count
			FROM groups g
			LEFT JOIN group_members gm ON gm.group_id = g.id
			WHERE g.tenant_id = ?
			GROUP BY g.id ORDER BY g.name
		`, tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*Group
	for rows.Next() {
		var g Group
		var tid sql.NullString
		if err := rows.Scan(&g.ID, &g.Name, &g.DisplayName, &g.Description, &tid, &g.CreatedAt, &g.UpdatedAt, &g.MemberCount); err != nil {
			logrus.WithError(err).Error("Failed to scan group row")
			continue
		}
		if tid.Valid {
			g.TenantID = tid.String
		}
		groups = append(groups, &g)
	}
	return groups, nil
}

func (s *SQLiteStore) ListAllGroups() ([]*Group, error) {
	rows, err := s.db.Query(`
		SELECT g.id, g.name, g.display_name, g.description, g.tenant_id, g.created_at, g.updated_at,
		       COUNT(gm.user_id) AS member_count
		FROM groups g
		LEFT JOIN group_members gm ON gm.group_id = g.id
		GROUP BY g.id ORDER BY g.tenant_id IS NOT NULL, g.tenant_id, g.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*Group
	for rows.Next() {
		var g Group
		var tid sql.NullString
		if err := rows.Scan(&g.ID, &g.Name, &g.DisplayName, &g.Description, &tid, &g.CreatedAt, &g.UpdatedAt, &g.MemberCount); err != nil {
			logrus.WithError(err).Error("Failed to scan group row")
			continue
		}
		if tid.Valid {
			g.TenantID = tid.String
		}
		groups = append(groups, &g)
	}
	return groups, nil
}

// =============================================================================
// SQLiteStore — Group membership
// =============================================================================

func (s *SQLiteStore) AddGroupMember(groupID, userID, addedBy string) error {
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO group_members (group_id, user_id, added_at, added_by)
		VALUES (?, ?, ?, ?)
	`, groupID, userID, time.Now().Unix(), addedBy)
	if err != nil {
		return fmt.Errorf("failed to add group member: %w", err)
	}
	return nil
}

func (s *SQLiteStore) RemoveGroupMember(groupID, userID string) error {
	_, err := s.db.Exec(`DELETE FROM group_members WHERE group_id = ? AND user_id = ?`, groupID, userID)
	if err != nil {
		return fmt.Errorf("failed to remove group member: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListGroupMembers(groupID string) ([]*GroupMember, error) {
	rows, err := s.db.Query(`
		SELECT gm.group_id, gm.user_id, u.username, u.email, gm.added_at, gm.added_by
		FROM group_members gm
		JOIN users u ON u.id = gm.user_id
		WHERE gm.group_id = ?
		ORDER BY gm.added_at DESC
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []*GroupMember
	for rows.Next() {
		var m GroupMember
		var addedBy sql.NullString
		if err := rows.Scan(&m.GroupID, &m.UserID, &m.Username, &m.Email, &m.AddedAt, &addedBy); err != nil {
			continue
		}
		if addedBy.Valid {
			m.AddedBy = addedBy.String
		}
		members = append(members, &m)
	}
	return members, nil
}

func (s *SQLiteStore) ListUserGroups(userID string) ([]*Group, error) {
	rows, err := s.db.Query(`
		SELECT g.id, g.name, g.display_name, g.description, g.tenant_id, g.created_at, g.updated_at
		FROM groups g
		JOIN group_members gm ON gm.group_id = g.id
		WHERE gm.user_id = ?
		ORDER BY g.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*Group
	for rows.Next() {
		var g Group
		var tid sql.NullString
		if err := rows.Scan(&g.ID, &g.Name, &g.DisplayName, &g.Description, &tid, &g.CreatedAt, &g.UpdatedAt); err != nil {
			continue
		}
		if tid.Valid {
			g.TenantID = tid.String
		}
		groups = append(groups, &g)
	}
	return groups, nil
}

// =============================================================================
// authManager — wrapper methods delegating to store
// =============================================================================

func (m *authManager) CreateGroup(ctx context.Context, group *Group) error {
	return m.store.CreateGroup(group)
}

func (m *authManager) GetGroup(ctx context.Context, groupID string) (*Group, error) {
	return m.store.GetGroup(groupID)
}

func (m *authManager) GetGroupByName(ctx context.Context, name, tenantID string) (*Group, error) {
	return m.store.GetGroupByName(name, tenantID)
}

func (m *authManager) UpdateGroup(ctx context.Context, group *Group) error {
	return m.store.UpdateGroup(group)
}

func (m *authManager) DeleteGroup(ctx context.Context, groupID string) error {
	return m.store.DeleteGroup(groupID)
}

func (m *authManager) ListGroups(ctx context.Context, tenantID string) ([]*Group, error) {
	return m.store.ListGroups(tenantID)
}

func (m *authManager) ListAllGroups(ctx context.Context) ([]*Group, error) {
	return m.store.ListAllGroups()
}

func (m *authManager) AddGroupMember(ctx context.Context, groupID, userID, addedBy string) error {
	return m.store.AddGroupMember(groupID, userID, addedBy)
}

func (m *authManager) RemoveGroupMember(ctx context.Context, groupID, userID string) error {
	return m.store.RemoveGroupMember(groupID, userID)
}

func (m *authManager) ListGroupMembers(ctx context.Context, groupID string) ([]*GroupMember, error) {
	return m.store.ListGroupMembers(groupID)
}

func (m *authManager) ListUserGroups(ctx context.Context, userID string) ([]*Group, error) {
	return m.store.ListUserGroups(userID)
}

// GenerateGroupID generates a unique group ID
func GenerateGroupID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "grp-" + hex.EncodeToString(b)
}
