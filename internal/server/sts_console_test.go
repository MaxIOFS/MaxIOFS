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

// consoleUser is a user of no tenant, who may manage its own keys or not.
func consoleUser(t *testing.T, s *Server, id string, manageKeys bool) *auth.User {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Unix()
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: id, Username: id, Status: auth.UserStatusActive,
		Roles: []string{}, CreatedAt: now, UpdatedAt: now}))
	if manageKeys {
		permissions, ok := s.authManager.(interface {
			SetUserPermissions(userID string, permissions *auth.UserPermissions) error
		})
		require.True(t, ok)
		require.NoError(t, permissions.SetUserPermissions(id, &auth.UserPermissions{Global: []string{auth.ActionManageOwnKeys}}))
	}
	user, err := s.authManager.GetUser(ctx, id)
	require.NoError(t, err)
	return user
}

// stsConsoleUsers are a user who may manage its own keys and one who may not.
func stsConsoleUsers(t *testing.T, s *Server) (allowed, refused *auth.User) {
	t.Helper()
	return consoleUser(t, s, "sts-allowed", true), consoleUser(t, s, "sts-refused", false)
}

func issueSession(s *Server, user *auth.User, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleIssueSTSSession(rec, requestAs(user, map[string]string{"method": http.MethodPost}, nil, body))
	return rec
}

// Temporary credentials are the caller's own, issued to whoever may manage
// its own keys.
func TestTemporaryCredentialsAreTheCallersOwn(t *testing.T) {
	s := newClusterTestNode(t)
	allowed, refused := stsConsoleUsers(t, s)
	ctx := context.Background()

	assert.Equal(t, http.StatusForbidden, issueSession(s, nil, "").Code, "nobody signed in")
	assert.Equal(t, http.StatusForbidden, issueSession(s, refused, "").Code, "may not manage its keys")

	rec := issueSession(s, allowed, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response struct {
		Data stsSessionResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	issued := response.Data
	assert.NotEmpty(t, issued.SecretAccessKey)
	assert.NotEmpty(t, issued.SessionToken)
	sessions, err := s.authManager.ListSTSSessions(ctx, allowed.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, issued.AccessKeyID, sessions[0].TempAccessKeyID)
}

// A session policy is taken as a document or as a string holding one, and a
// body that is not JSON is refused: credentials are not issued without the
// policy the caller meant to bound them with.
func TestASessionIsIssuedOnlyAsAsked(t *testing.T) {
	s := newClusterTestNode(t)
	allowed, _ := stsConsoleUsers(t, s)
	ctx := context.Background()
	const policy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`

	for _, body := range []string{
		`{"durationSeconds":900,"sessionPolicy":` + policy + `}`,
		`{"durationSeconds":900,"sessionPolicy":` + quoted(t, policy) + `}`,
	} {
		rec := issueSession(s, allowed, body)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	sessions, err := s.authManager.ListSTSSessions(ctx, allowed.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	for _, sess := range sessions {
		assert.JSONEq(t, policy, sess.SessionPolicy)
	}

	for body, status := range map[string]int{
		`{"durationSeconds":900,"sessionPolicy":` + policy: http.StatusBadRequest,
		`not json`:               http.StatusBadRequest,
		`{"sessionPolicy":1}`:    http.StatusBadRequest,
		`{"durationSeconds":60}`: http.StatusBadRequest,
		`{"sessionPolicy":{"Version":"2012-10-17","Statement":[{"Effect":"Maybe"}]}}`: http.StatusBadRequest,
	} {
		assert.Equal(t, status, issueSession(s, allowed, body).Code, body)
	}
	sessions, err = s.authManager.ListSTSSessions(ctx, allowed.ID)
	require.NoError(t, err)
	assert.Len(t, sessions, 2, "a refused request issues nothing")
}

func quoted(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return string(b)
}

// A user lists and revokes its own sessions; a global administrator lists and
// revokes anyone's.
func TestSessionsAreListedAndRevokedByTheirOwner(t *testing.T) {
	s := newClusterTestNode(t)
	allowed, other := stsConsoleUsers(t, s)
	ctx := context.Background()
	admin, err := s.authManager.GetUser(ctx, "admin")
	require.NoError(t, err)
	own, err := s.authManager.IssueSTSSession(ctx, allowed.ID, 0, "")
	require.NoError(t, err)
	others, err := s.authManager.IssueSTSSession(ctx, other.ID, 0, "")
	require.NoError(t, err)

	listed := func(user *auth.User, target string) []string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req = req.WithContext(context.WithValue(req.Context(), "user", user))
		rec := httptest.NewRecorder()
		s.handleListSTSSessions(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var response struct {
			Data []stsSessionListItem `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		var keys []string
		for _, item := range response.Data {
			keys = append(keys, item.AccessKeyID)
			owner, err := s.authManager.GetUser(ctx, item.UserID)
			require.NoError(t, err)
			assert.Equal(t, owner.Username, item.Username, "each session names its user")
		}
		return keys
	}
	revoke := func(user *auth.User, keyID string) int {
		rec := httptest.NewRecorder()
		s.handleRevokeSTSSession(rec, requestAs(user, map[string]string{"method": http.MethodDelete}, map[string]string{"keyId": keyID}, ""))
		return rec.Code
	}

	assert.Equal(t, []string{own.TempAccessKeyID}, listed(allowed, "/"))
	assert.Equal(t, []string{own.TempAccessKeyID}, listed(allowed, "/?all=true"), "only an administrator lists every user's")
	assert.ElementsMatch(t, []string{own.TempAccessKeyID, others.TempAccessKeyID}, listed(admin, "/?all=true"))

	assert.Equal(t, http.StatusForbidden, revoke(nil, own.TempAccessKeyID))
	assert.Equal(t, http.StatusForbidden, revoke(allowed, others.TempAccessKeyID))
	assert.Equal(t, http.StatusNotFound, revoke(allowed, "ASIAUNKNOWN"))
	assert.Equal(t, http.StatusOK, revoke(allowed, own.TempAccessKeyID))
	assert.Empty(t, listed(allowed, "/"))
	assert.Equal(t, http.StatusOK, revoke(admin, others.TempAccessKeyID))
	assert.Empty(t, listed(admin, "/?all=true"))
}

// A user holds at most STSMaxSessionsPerUser sessions, and one deactivated
// since it signed in is issued none.
func TestNoMoreSessionsAreIssuedThanAUserMayHold(t *testing.T) {
	s := newClusterTestNode(t)
	allowed, _ := stsConsoleUsers(t, s)
	ctx := context.Background()
	for range auth.STSMaxSessionsPerUser {
		_, err := s.authManager.IssueSTSSession(ctx, allowed.ID, 0, "")
		require.NoError(t, err)
	}
	assert.Equal(t, http.StatusTooManyRequests, issueSession(s, allowed, "").Code)

	fresh := consoleUser(t, s, "sts-second", true)
	stored, err := s.authManager.GetUser(ctx, fresh.ID)
	require.NoError(t, err)
	stored.Status = auth.UserStatusSuspended
	require.NoError(t, s.authManager.UpdateUser(ctx, stored))
	assert.Equal(t, http.StatusForbidden, issueSession(s, fresh, "").Code, "signed in before it was suspended")
}
