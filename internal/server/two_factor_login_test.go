package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The second step of a sign-in with two-factor authentication needs the first:
// a code alone signs no one in, wrong codes lock the account, and the limit per
// address applies.
func TestTwoFactorSignIn(t *testing.T) {
	server, tmpDir, cleanup := setupTestServer(t)
	defer cleanup()
	server.systemMetrics = metrics.NewSystemMetrics(tmpDir)
	ctx := context.Background()

	router := mux.NewRouter()
	server.setupConsoleAPIRoutes(router)

	// Each request comes from its own address unless it names one, so the
	// limit per address stays out of the way.
	addresses := 0
	send := func(method, path, address, bearer string, body map[string]string) (int, map[string]interface{}) {
		t.Helper()
		if address == "" {
			addresses++
			address = fmt.Sprintf("203.0.113.%d", addresses)
		}
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = address + ":4000"
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		out := map[string]interface{}{}
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		return rr.Code, out
	}

	// newUser makes an administrator with two-factor authentication and
	// returns its secret.
	newUser := func(name string) string {
		t.Helper()
		user := &auth.User{ID: name, Username: name, Password: "Secret123!", Status: "active", Roles: []string{"admin"}}
		require.NoError(t, server.authManager.CreateUser(ctx, user))
		setup, err := auth.Generate2FASecret(name, "MaxIOFS")
		require.NoError(t, err)
		code, err := totp.GenerateCode(setup.Secret, time.Now())
		require.NoError(t, err)
		_, err = server.authManager.Enable2FA(ctx, user.ID, code, setup.Secret)
		require.NoError(t, err)
		return setup.Secret
	}
	codeOf := func(secret string) string {
		t.Helper()
		code, err := totp.GenerateCode(secret, time.Now())
		require.NoError(t, err)
		return code
	}
	// wrongCode is none of the codes accepted now.
	wrongCode := func(secret string) string {
		t.Helper()
		accepted := map[string]bool{}
		for _, offset := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
			code, err := totp.GenerateCode(secret, time.Now().Add(offset))
			require.NoError(t, err)
			accepted[code] = true
		}
		for n := 0; ; n++ {
			if code := fmt.Sprintf("%06d", n); !accepted[code] {
				return code
			}
		}
	}
	signIn := func(name string) string {
		t.Helper()
		status, out := send("POST", "/auth/login", "", "", map[string]string{"username": name, "password": "Secret123!"})
		require.Equal(t, http.StatusOK, status, out)
		require.Equal(t, true, out["requires_2fa"], out)
		assert.NotContains(t, out, "access_token")
		assert.NotContains(t, out, "user_id")
		token, _ := out["two_factor_token"].(string)
		require.NotEmpty(t, token, out)
		return token
	}

	secret := newUser("two-factor-user")

	t.Run("a code alone signs no one in", func(t *testing.T) {
		status, out := send("POST", "/auth/2fa/verify", "", "", map[string]string{"user_id": "two-factor-user", "code": codeOf(secret)})
		assert.Equal(t, http.StatusBadRequest, status, out)
		assert.NotContains(t, out, "access_token")
	})

	t.Run("the token of the first step is not a session", func(t *testing.T) {
		token := signIn("two-factor-user")
		status, _ := send("GET", "/auth/me", "", token, nil)
		assert.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("a session token does not stand for the first step", func(t *testing.T) {
		user, err := server.authManager.GetUser(ctx, "two-factor-user")
		require.NoError(t, err)
		pair, err := server.authManager.GenerateTokenPair(ctx, user)
		require.NoError(t, err)
		for _, token := range []string{pair.AccessToken, pair.RefreshToken} {
			status, out := send("POST", "/auth/2fa/verify", "", "", map[string]string{"two_factor_token": token, "code": codeOf(secret)})
			assert.Equal(t, http.StatusUnauthorized, status, out)
			assert.NotContains(t, out, "access_token")
		}
	})

	t.Run("the token and the code sign in", func(t *testing.T) {
		token := signIn("two-factor-user")
		status, out := send("POST", "/auth/2fa/verify", "", "", map[string]string{"two_factor_token": token, "code": codeOf(secret)})
		require.Equal(t, http.StatusOK, status, out)
		access, _ := out["access_token"].(string)
		require.NotEmpty(t, access)
		status, _ = send("GET", "/auth/me", "", access, nil)
		assert.Equal(t, http.StatusOK, status)

		assert.Eventually(t, func() bool {
			logs, _, err := server.auditManager.GetLogs(ctx, &audit.AuditLogFilters{
				UserID: "two-factor-user", EventType: audit.EventTypeLoginSuccess, Page: 1, PageSize: 10})
			return err == nil && len(logs) > 0
		}, 5*time.Second, 50*time.Millisecond, "the sign-in is in the audit log")
	})

	t.Run("a user disabled after the password step is not signed in", func(t *testing.T) {
		disabledSecret := newUser("two-factor-disabled")
		token := signIn("two-factor-disabled")
		user, err := server.authManager.GetUser(ctx, "two-factor-disabled")
		require.NoError(t, err)
		user.Status = auth.UserStatusInactive
		require.NoError(t, server.authManager.UpdateUser(ctx, user))

		status, out := send("POST", "/auth/2fa/verify", "", "", map[string]string{"two_factor_token": token, "code": codeOf(disabledSecret)})
		assert.Equal(t, http.StatusUnauthorized, status, out)
		assert.NotContains(t, out, "access_token")
	})

	t.Run("the account locks after the configured number of wrong codes", func(t *testing.T) {
		lockedSecret := newUser("two-factor-locked")
		maxFailed, err := server.settingsManager.GetInt("security.max_failed_attempts")
		require.NoError(t, err)
		token := signIn("two-factor-locked")
		for i := 0; i < maxFailed; i++ {
			status, out := send("POST", "/auth/2fa/verify", "", "", map[string]string{"two_factor_token": token, "code": wrongCode(lockedSecret)})
			require.Equal(t, http.StatusUnauthorized, status, out)
		}
		status, out := send("POST", "/auth/2fa/verify", "", "", map[string]string{"two_factor_token": token, "code": codeOf(lockedSecret)})
		assert.Equal(t, http.StatusForbidden, status, out)
		assert.Equal(t, true, out["locked"], out)
		assert.NotContains(t, out, "access_token")
	})

	t.Run("the limit per address applies", func(t *testing.T) {
		perMinute, err := server.settingsManager.GetInt("security.ratelimit_login_per_minute")
		require.NoError(t, err)
		const address = "198.51.100.9"
		for i := 0; i < perMinute; i++ {
			status, out := send("POST", "/auth/2fa/verify", address, "", map[string]string{"two_factor_token": "not-a-token", "code": "123456"})
			require.Equal(t, http.StatusUnauthorized, status, out)
		}
		status, out := send("POST", "/auth/2fa/verify", address, "", map[string]string{"two_factor_token": "not-a-token", "code": "123456"})
		assert.Equal(t, http.StatusTooManyRequests, status, out)
	})
}

// Two-factor authentication is a field of the user: enabled by the user on
// one node, it reaches the other nodes with the user, as a new password does.
func TestTwoFactorReachesEveryNodeOfACluster(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.authManager.CreateUser(ctx, &auth.User{ID: "tfa-user", Username: "tfa-user", Password: "Secret123!",
		Roles: []string{"admin"}, Status: auth.UserStatusActive}))
	require.Eventually(t, func() bool {
		_, err := p.b.authManager.GetUser(ctx, "tfa-user")
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "the user reaches b")

	setup, err := auth.Generate2FASecret("tfa-user", "MaxIOFS")
	require.NoError(t, err)
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	_, err = p.a.authManager.Enable2FA(ctx, "tfa-user", code, setup.Secret)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		code, err := totp.GenerateCode(setup.Secret, time.Now())
		require.NoError(t, err)
		ok, err := p.b.authManager.Verify2FACode(ctx, "tfa-user", code)
		return err == nil && ok
	}, 10*time.Second, 20*time.Millisecond, "b asks for the user's code and takes it")
}
