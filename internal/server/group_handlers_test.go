package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleAddGroupMemberRejectsDifferentTenantScope(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Unix()
	db := server.groupDeleteDB()
	require.NotNil(t, db)

	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{
		ID:          "tenant-a",
		Name:        "tenant-a",
		DisplayName: "Tenant A",
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}))
	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{
		ID:          "tenant-b",
		Name:        "tenant-b",
		DisplayName: "Tenant B",
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}))
	require.NoError(t, server.authManager.CreateUser(ctx, &auth.User{
		ID:        "user-a",
		Username:  "user-a",
		Password:  "unused",
		Roles:     []string{"user"},
		Status:    "active",
		TenantID:  "tenant-a",
		CreatedAt: now,
	}))
	require.NoError(t, server.authManager.CreateUser(ctx, &auth.User{
		ID:        "user-b",
		Username:  "user-b",
		Password:  "unused",
		Roles:     []string{"user"},
		Status:    "active",
		TenantID:  "tenant-b",
		CreatedAt: now,
	}))
	require.NoError(t, server.authManager.CreateGroup(ctx, &auth.Group{
		ID:          "group-a",
		Name:        "group-a",
		DisplayName: "Group A",
		TenantID:    "tenant-a",
		CreatedAt:   now,
		UpdatedAt:   now,
	}))

	admin := &auth.User{ID: "admin", Username: "admin", Roles: []string{"admin"}, Status: "active"}
	body, err := json.Marshal(map[string]string{"userId": "user-b"})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/api/v1/groups/group-a/members", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"group": "group-a"})
	req = req.WithContext(context.WithValue(req.Context(), "user", admin))
	rr := httptest.NewRecorder()

	server.handleAddGroupMember(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)

	members, err := server.authManager.ListGroupMembers(ctx, "group-a")
	require.NoError(t, err)
	assert.Empty(t, members)
}

func TestDeleteGroupAndRecordTombstoneCleansPoliciesAtomically(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Unix()
	db := server.groupDeleteDB()
	require.NotNil(t, db)

	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{
		ID:              "tenant-a",
		Name:            "tenant-a",
		Status:          "active",
		MaxStorageBytes: 1 << 30,
		MaxBuckets:      10,
		MaxAccessKeys:   10,
		CreatedAt:       now,
		UpdatedAt:       now,
	}))
	require.NoError(t, server.authManager.CreateUser(ctx, &auth.User{
		ID:        "member-a",
		Username:  "member-a",
		Status:    "active",
		TenantID:  "tenant-a",
		Roles:     []string{"user"},
		CreatedAt: now,
		UpdatedAt: now,
	}))
	require.NoError(t, server.authManager.CreateGroup(ctx, &auth.Group{
		ID:          "group-delete",
		Name:        "group-delete",
		DisplayName: "Delete Me",
		TenantID:    "tenant-a",
		CreatedAt:   now,
		UpdatedAt:   now,
	}))
	require.NoError(t, server.authManager.AddGroupMember(ctx, "group-delete", "member-a", "admin"))
	_, err := db.ExecContext(ctx, `
		INSERT INTO bucket_permissions (id, bucket_name, bucket_tenant_id, group_id, permission_level, granted_by, granted_at)
		VALUES ('perm-group-delete', 'bucket-a', 'tenant-a', 'group-delete', 'read', 'admin', ?)
	`, now)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO iam_inline_policies (target_type, target_id, name, document, created_at, updated_at)
		VALUES (?, 'group-delete', 'inline', '{}', ?, ?)
	`, auth.IAMTargetGroup, now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO iam_policy_attachments (policy_name, target_type, target_id, attached_at)
		VALUES ('ReadOnlyAccess', ?, 'group-delete', ?)
	`, auth.IAMTargetGroup, now)
	require.NoError(t, err)

	rows, err := server.deleteGroupAndRecordTombstone(ctx, "group-delete", "node-a")
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)

	assertTableCount(t, db, `SELECT COUNT(*) FROM groups WHERE id = 'group-delete'`, 0)
	assertTableCount(t, db, `SELECT COUNT(*) FROM group_members WHERE group_id = 'group-delete'`, 0)
	assertTableCount(t, db, `SELECT COUNT(*) FROM bucket_permissions WHERE group_id = 'group-delete'`, 0)
	assertTableCount(t, db, `SELECT COUNT(*) FROM iam_inline_policies WHERE target_type = 'group' AND target_id = 'group-delete'`, 0)
	assertTableCount(t, db, `SELECT COUNT(*) FROM iam_policy_attachments WHERE target_type = 'group' AND target_id = 'group-delete'`, 0)
	assertTableCount(t, db, `SELECT COUNT(*) FROM cluster_deletion_log WHERE entity_type = 'group' AND entity_id = 'group-delete'`, 1)
}

func assertTableCount(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string, expected int) {
	t.Helper()
	var got int
	require.NoError(t, db.QueryRowContext(context.Background(), query).Scan(&got))
	assert.Equal(t, expected, got)
}
