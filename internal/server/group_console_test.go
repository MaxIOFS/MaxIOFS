package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// consoleData decodes the data of a console answer with status.
func consoleData[T any](t *testing.T, rec *httptest.ResponseRecorder, status int) T {
	t.Helper()
	require.Equal(t, status, rec.Code, rec.Body.String())
	assert.Equal(t, "application/json", rec.Result().Header.Get("Content-Type"), "as sent")
	var body struct {
		Success bool `json:"success"`
		Data    T    `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.True(t, body.Success)
	return body.Data
}

type groupList struct {
	Groups []*auth.Group `json:"groups"`
	Total  int           `json:"total"`
}

func groupNames(list groupList) []string {
	names := []string{}
	for _, group := range list.Groups {
		names = append(names, group.Name+"@"+group.TenantID)
	}
	return names
}

func consoleUserOf(t *testing.T, s *Server, id, tenantID, role string) *auth.User {
	t.Helper()
	now := time.Now().Unix()
	require.NoError(t, s.authManager.CreateUser(context.Background(), &auth.User{ID: id, Username: id, TenantID: tenantID,
		Status: auth.UserStatusActive, Roles: []string{role}, CreatedAt: now, UpdatedAt: now}))
	user, err := s.authManager.GetUser(context.Background(), id)
	require.NoError(t, err)
	return user
}

// A tenant's administrator makes, sees and changes its tenant's groups and no
// other; a global administrator, any.
func TestGroupsAreKeptByWhoAdministersThem(t *testing.T) {
	s := newClusterTestNode(t)
	tenantAdmin, member := groupDeleteFixture(t, s)
	admin, err := s.authManager.GetUser(context.Background(), "admin")
	require.NoError(t, err)
	create := func(user *auth.User, body string) *httptest.ResponseRecorder {
		return consoleAs(user, s.handleCreateGroup, http.MethodPost, nil, body)
	}
	list := func(user *auth.User, query string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/groups?"+query, nil)
		s.handleListGroups(rec, req.WithContext(context.WithValue(req.Context(), "user", user)))
		return rec
	}

	made := consoleData[auth.Group](t, create(tenantAdmin, `{"name":" ops ","displayName":"Ops","tenantId":"other"}`), http.StatusCreated)
	assert.Equal(t, "ops", made.Name)
	assert.Equal(t, "acme", made.TenantID, "a tenant's administrator makes groups of its tenant")
	assert.NotEmpty(t, made.ID)
	assert.Equal(t, "other", consoleData[auth.Group](t, create(admin, `{"name":"ops","tenantId":"other"}`), http.StatusCreated).TenantID)
	for _, tc := range []struct {
		user   *auth.User
		body   string
		status int
	}{
		{tenantAdmin, `{"name":"ops"}`, http.StatusConflict},
		{tenantAdmin, `{"name":"  "}`, http.StatusBadRequest},
		{tenantAdmin, `not json`, http.StatusBadRequest},
		{member, `{"name":"mine"}`, http.StatusForbidden},
		{nil, `{"name":"mine"}`, http.StatusForbidden},
	} {
		assert.Equal(t, tc.status, create(tc.user, tc.body).Code, tc.body)
	}

	for _, tc := range []struct {
		user  *auth.User
		query string
		want  []string
	}{
		{tenantAdmin, "", []string{"g-acme@acme", "ops@acme"}},
		{tenantAdmin, "scope=global&tenantId=other", []string{"g-acme@acme", "ops@acme"}},
		{admin, "", []string{"g-acme@acme", "ops@acme", "g-other@other", "ops@other", "g-global@"}},
		{admin, "scope=global", []string{"g-global@"}},
		{admin, "tenantId=other", []string{"g-other@other", "ops@other"}},
	} {
		listed := consoleData[groupList](t, list(tc.user, tc.query), http.StatusOK)
		assert.ElementsMatch(t, tc.want, groupNames(listed), "%s %s", tc.user.ID, tc.query)
		assert.Equal(t, len(tc.want), listed.Total)
	}
	assert.Equal(t, http.StatusUnauthorized, list(nil, "").Code)
	assert.Equal(t, http.StatusForbidden, list(member, "").Code)

	get := func(user *auth.User, id string) *httptest.ResponseRecorder {
		return consoleAs(user, s.handleGetGroup, http.MethodGet, map[string]string{"group": id}, "")
	}
	update := func(user *auth.User, id, body string) *httptest.ResponseRecorder {
		return consoleAs(user, s.handleUpdateGroup, http.MethodPut, map[string]string{"group": id}, body)
	}
	assert.Equal(t, "g-acme", consoleData[auth.Group](t, get(tenantAdmin, "g-acme"), http.StatusOK).ID)
	assert.Equal(t, "g-other", consoleData[auth.Group](t, get(admin, "g-other"), http.StatusOK).ID)
	changed := consoleData[auth.Group](t, update(tenantAdmin, "g-acme", `{"displayName":"Acme team","description":"Everyone"}`), http.StatusOK)
	assert.Equal(t, "Acme team", changed.DisplayName)
	stored, err := s.authManager.GetGroup(context.Background(), "g-acme")
	require.NoError(t, err)
	assert.Equal(t, "Acme team", stored.DisplayName)
	assert.Equal(t, "Everyone", stored.Description)
	for _, rec := range []*httptest.ResponseRecorder{
		get(tenantAdmin, "g-other"), get(tenantAdmin, "g-global"), get(member, "g-acme"), get(nil, "g-acme"),
		update(tenantAdmin, "g-other", `{"displayName":"x"}`), update(member, "g-acme", `{"displayName":"x"}`),
	} {
		assert.Equal(t, http.StatusForbidden, rec.Code)
	}
	assert.Equal(t, http.StatusNotFound, get(tenantAdmin, "g-none").Code)
	assert.Equal(t, http.StatusNotFound, update(tenantAdmin, "g-none", `{}`).Code)
	assert.Equal(t, http.StatusBadRequest, update(tenantAdmin, "g-acme", `not json`).Code)
	stored, err = s.authManager.GetGroup(context.Background(), "g-other")
	require.NoError(t, err)
	assert.Empty(t, stored.DisplayName, "refused changes change nothing")
}

// A group's members are listed, added and removed by who administers the
// group, and are users of its tenant.
func TestAGroupsMembersAreKeptByWhoAdministersIt(t *testing.T) {
	s := newClusterTestNode(t)
	tenantAdmin, member := groupDeleteFixture(t, s)
	colleague := consoleUserOf(t, s, "acme-colleague", "acme", auth.RoleUser)
	consoleUserOf(t, s, "other-user", "other", auth.RoleUser)
	members := func(user *auth.User, group string) *httptest.ResponseRecorder {
		return consoleAs(user, s.handleListGroupMembers, http.MethodGet, map[string]string{"group": group}, "")
	}
	add := func(user *auth.User, group, body string) *httptest.ResponseRecorder {
		return consoleAs(user, s.handleAddGroupMember, http.MethodPost, map[string]string{"group": group}, body)
	}
	remove := func(user *auth.User, group, userID string) *httptest.ResponseRecorder {
		return consoleAs(user, s.handleRemoveGroupMember, http.MethodDelete, map[string]string{"group": group, "user": userID}, "")
	}
	memberIDs := func() []string {
		listed := consoleData[struct {
			Members []*auth.GroupMember `json:"members"`
			Total   int                 `json:"total"`
		}](t, members(tenantAdmin, "g-acme"), http.StatusOK)
		ids := []string{}
		for _, m := range listed.Members {
			ids = append(ids, m.UserID)
		}
		assert.Len(t, ids, listed.Total)
		return ids
	}

	assert.Equal(t, []string{member.ID}, memberIDs())
	consoleData[map[string]string](t, add(tenantAdmin, "g-acme", `{"userId":"`+colleague.ID+`"}`), http.StatusOK)
	assert.ElementsMatch(t, []string{member.ID, colleague.ID}, memberIDs())
	for _, tc := range []struct {
		rec    *httptest.ResponseRecorder
		status int
	}{
		{add(tenantAdmin, "g-acme", `{"userId":"other-user"}`), http.StatusForbidden},
		{add(tenantAdmin, "g-other", `{"userId":"other-user"}`), http.StatusForbidden},
		{add(member, "g-acme", `{"userId":"`+colleague.ID+`"}`), http.StatusForbidden},
		{add(tenantAdmin, "g-acme", `{"userId":"ghost"}`), http.StatusNotFound},
		{add(tenantAdmin, "g-none", `{"userId":"`+colleague.ID+`"}`), http.StatusNotFound},
		{add(tenantAdmin, "g-acme", `{}`), http.StatusBadRequest},
		{add(tenantAdmin, "g-acme", `not json`), http.StatusBadRequest},
		{members(tenantAdmin, "g-other"), http.StatusForbidden},
		{members(member, "g-acme"), http.StatusForbidden},
		{members(tenantAdmin, "g-none"), http.StatusNotFound},
		{remove(tenantAdmin, "g-other", "other-user"), http.StatusForbidden},
		{remove(member, "g-acme", colleague.ID), http.StatusForbidden},
		{remove(tenantAdmin, "g-none", colleague.ID), http.StatusNotFound},
	} {
		assert.Equal(t, tc.status, tc.rec.Code, tc.rec.Body.String())
	}
	assert.ElementsMatch(t, []string{member.ID, colleague.ID}, memberIDs(), "refused requests change nothing")

	consoleData[map[string]string](t, remove(tenantAdmin, "g-acme", member.ID), http.StatusOK)
	assert.Equal(t, []string{colleague.ID}, memberIDs())
}

// A user sees the groups it is in; another user's, only who administers it.
func TestAUsersGroupsAreSeenByItOrWhoAdministersIt(t *testing.T) {
	s := newClusterTestNode(t)
	tenantAdmin, member := groupDeleteFixture(t, s)
	other := consoleUserOf(t, s, "other-user", "other", auth.RoleUser)
	require.NoError(t, s.authManager.AddGroupMember(context.Background(), "g-other", other.ID, "admin"))
	admin, err := s.authManager.GetUser(context.Background(), "admin")
	require.NoError(t, err)
	groupsOf := func(user *auth.User, id string) *httptest.ResponseRecorder {
		return consoleAs(user, s.handleListUserGroups, http.MethodGet, map[string]string{"user": id}, "")
	}

	assert.Equal(t, []string{"g-acme@acme"}, groupNames(consoleData[groupList](t, groupsOf(member, member.ID), http.StatusOK)))
	assert.Equal(t, []string{"g-acme@acme"}, groupNames(consoleData[groupList](t, groupsOf(tenantAdmin, member.ID), http.StatusOK)))
	assert.Equal(t, []string{"g-other@other"}, groupNames(consoleData[groupList](t, groupsOf(admin, other.ID), http.StatusOK)))
	assert.Equal(t, http.StatusUnauthorized, groupsOf(nil, member.ID).Code)
	assert.Equal(t, http.StatusForbidden, groupsOf(member, tenantAdmin.ID).Code)
	assert.Equal(t, http.StatusForbidden, groupsOf(tenantAdmin, other.ID).Code, "a user of another tenant")
	assert.Equal(t, http.StatusNotFound, groupsOf(admin, "ghost").Code)
}
