package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The security overview is of the tenant of whoever asks, and of every tenant
// for a global administrator, never for a user of no tenant who is not one:
// what Object Lock protects, the users and their failed sign-ins, in one
// answer.
func TestTheSecurityOverviewIsOfTheTenantOfWhoAsks(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, member, _, _ := userUpdateFixture(t, s)
	loner := consoleUserOf(t, s, "loner", "", auth.RoleUser)
	admin := storedUser(t, s, "admin")
	for tenantID, names := range map[string][]string{"": {"g-locked", "g-plain"}, "acme": {"t-locked"}} {
		for _, name := range names {
			require.NoError(t, s.bucketManager.CreateBucket(ctx, tenantID, name, ""))
		}
	}
	for tenantID, name := range map[string]string{"": "g-locked", "acme": "t-locked"} {
		require.NoError(t, s.bucketManager.SetObjectLockConfig(ctx, tenantID, name, &bucket.ObjectLockConfig{ObjectLockEnabled: true}))
	}
	signInAnswer(s, `{"username":"acme-user","password":"wrong"}`, "192.0.2.9")
	overview := func(user *auth.User) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/security/status", nil)
		if user != nil {
			req = req.WithContext(context.WithValue(req.Context(), "user", user))
		}
		rec := httptest.NewRecorder()
		s.handleGetSecurityStatus(rec, req)
		return rec
	}

	assert.Equal(t, http.StatusUnauthorized, overview(nil).Code)
	assert.Equal(t, http.StatusForbidden, overview(loner).Code, "a user of no tenant")
	type numbers struct {
		Data struct {
			ObjectLock struct {
				BucketsWithLock int `json:"bucketsWithLock"`
			} `json:"objectLock"`
			Authentication struct {
				ActiveUsers     int `json:"activeUsers"`
				FailedLogins24h int `json:"failedLogins24h"`
			} `json:"authentication"`
		} `json:"data"`
	}
	for _, user := range []*auth.User{member, tenantAdmin} {
		rec := overview(user)
		require.Equal(t, http.StatusOK, rec.Code, user.ID)
		var acme numbers
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &acme))
		assert.Equal(t, 1, acme.Data.ObjectLock.BucketsWithLock, "acme's locked bucket")
		assert.Equal(t, 3, acme.Data.Authentication.ActiveUsers, "acme's users")
		assert.Equal(t, 1, acme.Data.Authentication.FailedLogins24h)
	}
	rec := overview(admin)
	require.Equal(t, http.StatusOK, rec.Code)
	var answer struct {
		Data struct {
			ObjectLock struct {
				Enabled         bool `json:"enabled"`
				BucketsWithLock int  `json:"bucketsWithLock"`
			} `json:"objectLock"`
			Authentication struct {
				ActiveUsers     int `json:"activeUsers"`
				FailedLogins24h int `json:"failedLogins24h"`
			} `json:"authentication"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	assert.True(t, answer.Data.ObjectLock.Enabled, "one envelope: %s", rec.Body.String())
	assert.Equal(t, 2, answer.Data.ObjectLock.BucketsWithLock, "the locked buckets of every tenant")
	users, err := s.authManager.ListUsers(ctx)
	require.NoError(t, err)
	assert.Equal(t, len(users), answer.Data.Authentication.ActiveUsers)
	assert.Equal(t, 1, answer.Data.Authentication.FailedLogins24h)
}
