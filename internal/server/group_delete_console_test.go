package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groupDeleteFixture is tenants acme and other, a group in each and one of no
// tenant, acme's administrator and one of its members.
func groupDeleteFixture(t *testing.T, s *Server) (tenantAdmin, member *auth.User) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Unix()
	for _, id := range []string{"acme", "other"} {
		require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: id, Name: id, Status: "active"}))
	}
	for id, tenantID := range map[string]string{"g-acme": "acme", "g-other": "other", "g-global": ""} {
		require.NoError(t, s.authManager.CreateGroup(ctx, &auth.Group{ID: id, Name: id, TenantID: tenantID, CreatedAt: now, UpdatedAt: now}))
	}
	user := func(id, role string) *auth.User {
		require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: id, Username: id, TenantID: "acme",
			Status: auth.UserStatusActive, Roles: []string{role}, CreatedAt: now, UpdatedAt: now}))
		stored, err := s.authManager.GetUser(ctx, id)
		require.NoError(t, err)
		return stored
	}
	tenantAdmin, member = user("acme-admin", auth.RoleTenantAdmin), user("acme-member", auth.RoleUser)
	require.NoError(t, s.authManager.AddGroupMember(ctx, "g-acme", member.ID, tenantAdmin.ID))
	return tenantAdmin, member
}

func deleteGroup(s *Server, user *auth.User, groupID string) int {
	rec := httptest.NewRecorder()
	s.handleDeleteGroup(rec, requestAs(user, map[string]string{"method": http.MethodDelete}, map[string]string{"group": groupID}, ""))
	return rec.Code
}

func groupExists(t *testing.T, s *Server, groupID string) bool {
	t.Helper()
	_, err := s.authManager.GetGroup(context.Background(), groupID)
	return err == nil
}

// A tenant's administrator deletes the groups of its tenant and no other; a
// global administrator deletes any.
func TestAGroupIsDeletedByWhoAdministersIt(t *testing.T) {
	s := newClusterTestNode(t)
	tenantAdmin, member := groupDeleteFixture(t, s)
	admin, err := s.authManager.GetUser(context.Background(), "admin")
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, deleteGroup(s, nil, "g-acme"))
	assert.Equal(t, http.StatusForbidden, deleteGroup(s, member, "g-acme"))
	assert.Equal(t, http.StatusNotFound, deleteGroup(s, tenantAdmin, "g-none"))
	assert.Equal(t, http.StatusForbidden, deleteGroup(s, tenantAdmin, "g-other"))
	assert.Equal(t, http.StatusForbidden, deleteGroup(s, tenantAdmin, "g-global"))
	for _, id := range []string{"g-acme", "g-other", "g-global"} {
		assert.True(t, groupExists(t, s, id), "refused requests delete nothing: %s", id)
	}

	assert.Equal(t, http.StatusOK, deleteGroup(s, tenantAdmin, "g-acme"))
	assert.False(t, groupExists(t, s, "g-acme"))
	groups, err := s.authManager.ListUserGroups(context.Background(), member.ID)
	require.NoError(t, err)
	assert.Empty(t, groups, "its members leave it")
	assert.Equal(t, http.StatusOK, deleteGroup(s, admin, "g-other"))
	assert.Equal(t, http.StatusOK, deleteGroup(s, admin, "g-global"))
	assert.False(t, groupExists(t, s, "g-other"))
	assert.False(t, groupExists(t, s, "g-global"))
	assertTableCount(t, s.groupDeleteDB(), `SELECT COUNT(*) FROM cluster_deletion_log WHERE entity_type = 'group'`, 0)
}

// In a cluster a deleted group is recorded, so the other nodes drop it too.
func TestAGroupDeletedInAClusterIsRecorded(t *testing.T) {
	s := newClusterTestNode(t)
	tenantAdmin, _ := groupDeleteFixture(t, s)
	_, err := s.clusterManager.InitializeCluster(context.Background(), "s", "us-east-1", "https://127.0.0.1:18082")
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, deleteGroup(s, tenantAdmin, "g-other"))
	require.Equal(t, http.StatusOK, deleteGroup(s, tenantAdmin, "g-acme"))
	assert.False(t, groupExists(t, s, "g-acme"))
	assertTableCount(t, s.groupDeleteDB(), `SELECT COUNT(*) FROM cluster_deletion_log WHERE entity_type = 'group' AND entity_id = 'g-acme'`, 1)
	assertTableCount(t, s.groupDeleteDB(), `SELECT COUNT(*) FROM cluster_deletion_log WHERE entity_type = 'group' AND entity_id = 'g-other'`, 0)
}
