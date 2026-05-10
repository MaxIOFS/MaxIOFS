package server

import (
	"bytes"
	"context"
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

