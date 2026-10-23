package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each node counts the failed sign-ins it serves. An administrator's unlock,
// served by the coordinator, unlocks the account on the node where it locked.
func TestAnUnlockReachesTheNodeWhereTheAccountLocked(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.authManager.CreateUser(ctx, &auth.User{ID: "unlock-admin", Username: "unlock-admin", Roles: []string{"admin"}, Status: auth.UserStatusActive}))
	require.NoError(t, p.a.authManager.CreateUser(ctx, &auth.User{ID: "locked-on-b", Username: "locked-on-b", Roles: []string{"user"}, Status: auth.UserStatusActive}))
	require.Eventually(t, func() bool {
		_, err := p.b.authManager.GetUser(ctx, "locked-on-b")
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "the user reaches b")

	maxFailed, err := p.b.settingsManager.GetInt("security.max_failed_attempts")
	require.NoError(t, err)
	for i := 0; i < maxFailed; i++ {
		require.NoError(t, p.b.authManager.RecordFailedLogin(ctx, "locked-on-b", "203.0.113.7"))
	}
	locked, _, err := p.b.authManager.IsAccountLocked(ctx, "locked-on-b")
	require.NoError(t, err)
	require.True(t, locked, "locked on b")

	req := createAuthenticatedRequest(http.MethodPost, "/api/v1/users/locked-on-b/unlock", nil, "", "unlock-admin", true)
	req = mux.SetURLVars(req, map[string]string{"user": "locked-on-b"})
	w := httptest.NewRecorder()
	p.a.handleUnlockAccount(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Eventually(t, func() bool {
		locked, _, err := p.b.authManager.IsAccountLocked(ctx, "locked-on-b")
		return err == nil && !locked
	}, 10*time.Second, 20*time.Millisecond, "unlocked on b")
}

// A change of a user reaches the other nodes at once, without waiting for the
// periodic sync: a user made on a signs in on b, and after a new password the
// old one is refused on b.
func TestAChangeOfAUserReachesTheOtherNodesAtOnce(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	user := &auth.User{ID: "pushed", Username: "pushed", Password: "Old-Secret-1!", Roles: []string{"user"}, Status: auth.UserStatusActive}
	require.NoError(t, p.a.authManager.CreateUser(ctx, user))
	require.Eventually(t, func() bool {
		_, err := p.b.authManager.ValidateConsoleCredentials(ctx, "pushed", "Old-Secret-1!")
		return err == nil
	}, 5*time.Second, 20*time.Millisecond, "the user signs in on b")

	changed, err := p.a.authManager.GetUser(ctx, "pushed")
	require.NoError(t, err)
	changed.Password, err = auth.HashPassword("New-Secret-2!")
	require.NoError(t, err)
	require.NoError(t, p.a.authManager.UpdateUser(ctx, changed))
	assert.Eventually(t, func() bool {
		_, err := p.b.authManager.ValidateConsoleCredentials(ctx, "pushed", "New-Secret-2!")
		return err == nil
	}, 5*time.Second, 20*time.Millisecond, "the new password signs in on b")
	_, err = p.b.authManager.ValidateConsoleCredentials(ctx, "pushed", "Old-Secret-1!")
	assert.Error(t, err, "the old one is refused on b")
}
