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

// The whole point of the feature: an administrator hands out a password, the
// user replaces it, and the obligation is gone. This walks that path against
// the real handlers because the first version shipped a form the server
// refused — the button looked inert and nothing said why.
func TestForcedPasswordChange_UserReplacesItAndTheObligationLifts(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()

	target := &auth.User{
		ID:        "forced-change-user",
		Username:  "forced-change-user",
		Status:    "active",
		Roles:     []string{"user"},
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, server.authManager.CreateUser(ctx, target))
	defer server.authManager.DeleteUser(ctx, target.ID)

	admin := &auth.User{ID: "admin-1", Username: "admin", Roles: []string{"admin"}}

	changePassword := func(actor *auth.User, targetID string, body map[string]any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("PUT", "/api/v1/users/"+targetID+"/password", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), "user", actor))
		req = mux.SetURLVars(req, map[string]string{"user": targetID})
		rr := httptest.NewRecorder()
		server.handleChangePassword(rr, req)
		return rr
	}

	// The administrator hands out a password and marks the account.
	rr := changePassword(admin, target.ID, map[string]any{
		"newPassword":        "HandedOver123!",
		"mustChangePassword": true,
	})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	marked, err := server.authManager.GetUser(ctx, target.ID)
	require.NoError(t, err)
	require.True(t, marked.MustChangePassword, "the account should owe a password change")

	// While it owes one, the console is closed to it apart from the way out.
	listReq := httptest.NewRequest("GET", "/api/v1/buckets", nil)
	listReq = listReq.WithContext(context.WithValue(listReq.Context(), "user", marked))
	blocked := httptest.NewRecorder()
	server.pendingPasswordChangeMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("a request reached the console while a password change was pending")
	})).ServeHTTP(blocked, listReq)
	require.Equal(t, http.StatusForbidden, blocked.Code)
	assert.Contains(t, blocked.Body.String(), PasswordChangeRequiredCode)

	// The user sets their own, without repeating the password the administrator
	// handed over: logging in with it a moment ago was the proof.
	rr = changePassword(marked, target.ID, map[string]any{
		"newPassword": "TheirOwn456!",
	})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	cleared, err := server.authManager.GetUser(ctx, target.ID)
	require.NoError(t, err)
	assert.False(t, cleared.MustChangePassword, "replacing the password lifts the obligation")

	// And the account works again.
	openReq := httptest.NewRequest("GET", "/api/v1/buckets", nil)
	openReq = openReq.WithContext(context.WithValue(openReq.Context(), "user", cleared))
	reached := false
	server.pendingPasswordChangeMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	})).ServeHTTP(httptest.NewRecorder(), openReq)
	assert.True(t, reached, "the console should be open once the password has been replaced")

	// The waiver belongs to the forced change alone. An ordinary self-service
	// change still has to prove knowledge of the current password.
	rr = changePassword(cleared, target.ID, map[string]any{
		"newPassword": "YetAnother789!",
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code,
		"a normal password change must still require the current one")

	rr = changePassword(cleared, target.ID, map[string]any{
		"currentPassword": "WrongOne000!",
		"newPassword":     "YetAnother789!",
	})
	assert.Equal(t, http.StatusUnauthorized, rr.Code,
		"a wrong current password must still be rejected")
}
