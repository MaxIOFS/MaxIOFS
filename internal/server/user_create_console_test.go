package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createUser(s *Server, caller *auth.User, body string) *httptest.ResponseRecorder {
	return consoleAs(caller, s.handleCreateUser, http.MethodPost, nil, body)
}

// A user is made by who administers users: a tenant's administrator in its
// tenant, with the roles it may give; a global administrator anywhere, the
// administrator role too. A local user has a password the policy takes; one
// of an identity provider needs none and is named by its email.
func TestAUserIsMadeByWhoAdministersUsers(t *testing.T) {
	s := newClusterTestNode(t)
	tenantAdmin, member, _, _ := userUpdateFixture(t, s)
	admin := storedUser(t, s, "admin")
	const password = "Carol-password-12345!"

	made := consoleData[UserResponse](t, createUser(s, tenantAdmin, `{"username":"carol","password":"`+password+`"}`), http.StatusOK)
	assert.Equal(t, "carol", made.Username)
	assert.Equal(t, "acme", made.TenantID, "a tenant's administrator makes users of its tenant")
	assert.Equal(t, auth.UserStatusActive, made.Status)
	assert.Equal(t, []string{"read"}, made.Roles)
	assert.NotEqual(t, "carol", made.ID, "a user's ID is not its name")
	stored := storedUser(t, s, made.ID)
	assert.Equal(t, "carol", stored.Username)

	sso := consoleData[UserResponse](t, createUser(s, admin,
		`{"username":"dan@corp.test","authProvider":"oauth:corp","tenantId":"beta","roles":["user"],"status":"inactive"}`), http.StatusOK)
	assert.Equal(t, "dan@corp.test", sso.Email)
	assert.Equal(t, "dan@corp.test", storedUser(t, s, sso.ID).ExternalID)
	assert.Equal(t, "beta", sso.TenantID)
	assert.Equal(t, auth.UserStatusInactive, sso.Status)
	assert.Equal(t, []string{auth.RoleAdmin}, consoleData[UserResponse](t, createUser(s, admin,
		`{"username":"eve","password":"`+password+`","roles":["admin"]}`), http.StatusOK).Roles)

	for _, tc := range []struct {
		caller *auth.User
		body   string
		status int
	}{
		{admin, `not json`, http.StatusBadRequest},
		{admin, `{"password":"` + password + `"}`, http.StatusBadRequest},
		{admin, `{"username":"nopass"}`, http.StatusBadRequest},
		{admin, `{"username":"weak","password":"short"}`, http.StatusBadRequest},
		{admin, `{"username":"odd","password":"` + password + `","status":"bogus"}`, http.StatusBadRequest},
		{member, `{"username":"by-member","password":"` + password + `"}`, http.StatusForbidden},
		{tenantAdmin, `{"username":"elsewhere","password":"` + password + `","tenantId":"beta"}`, http.StatusForbidden},
		{tenantAdmin, `{"username":"boss","password":"` + password + `","roles":["admin"]}`, http.StatusForbidden},
		{admin, `{"username":"carol","password":"` + password + `"}`, http.StatusConflict},
		{admin, `{"username":"other","password":"` + password + `","email":"dan@corp.test"}`, http.StatusConflict},
	} {
		assert.Equal(t, tc.status, createUser(s, tc.caller, tc.body).Code, tc.body)
	}
	rec := httptest.NewRecorder()
	s.handleCreateUser(rec, httptest.NewRequest(http.MethodPost, "/api/v1/users", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code, "an empty body")
	for _, name := range []string{"by-member", "elsewhere", "boss", "weak", "other", "odd"} {
		_, err := s.authManager.GetUserByUsername(t.Context(), name)
		assert.Error(t, err, "%s is not made", name)
	}
	require.NotNil(t, made)
}
