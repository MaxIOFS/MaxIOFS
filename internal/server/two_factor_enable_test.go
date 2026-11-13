package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A second factor in use is not replaced by setting one up again: an open
// session alone would take the account's second factor.
func TestASecondFactorInUseIsNotReplaced(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()
	ctx := context.Background()
	user, err := server.authManager.ValidateJWT(ctx, getAdminToken(t, server))
	require.NoError(t, err)

	enable := func(secret string) (int, string) {
		code, err := totp.GenerateCode(secret, time.Now())
		require.NoError(t, err)
		raw, _ := json.Marshal(map[string]string{"code": code, "secret": secret})
		req := httptest.NewRequest("POST", "/api/v1/auth/2fa/enable", bytes.NewReader(raw))
		req = req.WithContext(context.WithValue(req.Context(), "user", user))
		rr := httptest.NewRecorder()
		server.handleEnable2FA(rr, req)
		return rr.Code, rr.Body.String()
	}

	status, body := enable("JBSWY3DPEHPK3PXP")
	require.Equal(t, http.StatusOK, status, body)

	status, body = enable("KRSXG5CTMVRXEZLU")
	assert.Equal(t, http.StatusConflict, status, body)
	assert.Contains(t, body, "already enabled")
	assert.NotContains(t, body, "backup_codes", "the refusal hands out backup codes")

	require.NoError(t, server.authManager.Disable2FA(ctx, user.ID, user.ID, true))
	status, body = enable("KRSXG5CTMVRXEZLU")
	assert.Equal(t, http.StatusOK, status, "2FA cannot be set up again once disabled: %s", body)
}
