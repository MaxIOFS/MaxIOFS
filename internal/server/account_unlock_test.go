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
	// The user last changed a minute ago; the user sync keeps the copy changed last.
	_, err := p.a.db.ExecContext(ctx, `UPDATE users SET updated_at = updated_at - 60 WHERE id = 'locked-on-b'`)
	require.NoError(t, err)
	p.a.userSyncMgr.TriggerSync(ctx)
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
