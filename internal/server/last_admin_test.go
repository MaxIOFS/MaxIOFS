package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The deployment keeps one global administrator that can sign in: an inactive
// one does not count, and the last active one is neither deleted, by the
// console or the IAM API, nor deactivated.
func TestTheLastActiveGlobalAdministratorIsKept(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()

	second := &auth.User{ID: "second", Username: "second", Password: "Second-pass-12345!", Status: auth.UserStatusActive,
		Roles: []string{auth.RoleAdmin}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	require.NoError(t, s.authManager.CreateUser(ctx, second))
	admin, err := s.authManager.GetUserByUsername(ctx, "admin")
	require.NoError(t, err)

	call := func(handler http.HandlerFunc, method, userID, body string) int {
		req := createAuthenticatedRequest(method, "/api/v1/users/"+userID, strings.NewReader(body), "", "second", true)
		req = mux.SetURLVars(req, map[string]string{"user": userID})
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, call(s.handleUpdateUser, http.MethodPut, "second", `{"status":"inactive"}`),
		"one of two administrators is deactivated")

	assert.Equal(t, http.StatusConflict, call(s.handleDeleteUser, http.MethodDelete, admin.ID, ""),
		"the only active administrator is not deleted")
	assert.Equal(t, http.StatusConflict, call(s.handleUpdateUser, http.MethodPut, admin.ID, `{"status":"inactive"}`),
		"nor deactivated")
	assert.Equal(t, http.StatusConflict, call(s.handleUpdateUser, http.MethodPut, admin.ID, `{"roles":["user"]}`),
		"nor left without the role")

	iamRequest := createAuthenticatedRequest(http.MethodPost, "/", nil, "", "second", true)
	iamRequest.PostForm = url.Values{"UserName": {"admin"}}
	rec := httptest.NewRecorder()
	s.iamDeleteUser(rec, iamRequest, s.authManager.(auth.IAMManager), s.getAuthUser(iamRequest))
	assert.Equal(t, http.StatusConflict, rec.Code, "nor deleted by the IAM API: %s", rec.Body.String())

	kept, err := s.authManager.GetUser(ctx, admin.ID)
	require.NoError(t, err)
	assert.Equal(t, auth.UserStatusActive, kept.Status)

	assert.Equal(t, http.StatusNoContent, call(s.handleDeleteUser, http.MethodDelete, "second", ""),
		"the inactive administrator is deleted: it was not the one that signs in")
}
