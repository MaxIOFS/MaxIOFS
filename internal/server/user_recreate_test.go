package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signIn is the console token of a sign-in with username and password.
func signIn(t *testing.T, s *Server, username, password string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	s.handleLogin(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var answer LoginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	require.NotEmpty(t, answer.Token)
	return answer.Token
}

// A user made again with the username of one deleted is another user: what
// was the deleted one's, its console sessions first, is not the new one's.
func TestAUserMadeAgainIsAnotherUser(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	admin := storedUser(t, s, "admin")
	const password = "Bob-password-12345!"
	create := func() string {
		rec := consoleAs(admin, s.handleCreateUser, http.MethodPost, nil,
			`{"username":"bob","password":"`+password+`","roles":["user"]}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return consoleData[UserResponse](t, rec, http.StatusOK).ID
	}

	firstID := create()
	token := signIn(t, s, "bob", password)
	_, err := s.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent,
		consoleAs(admin, s.handleDeleteUser, http.MethodDelete, map[string]string{"user": firstID}, "").Code)
	_, err = s.authManager.ValidateJWT(ctx, token)
	assert.Error(t, err, "a deleted user's session ends")

	secondID := create()
	_, err = s.authManager.ValidateJWT(ctx, token)
	assert.Error(t, err, "nor is it the new bob's")
	t.Logf("ids: %s then %s", firstID, secondID)
}
