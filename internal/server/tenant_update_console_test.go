package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A tenant is changed by a global administrator: what the request names, and
// nothing else. A status tenants are not put in, and a negative quota, are
// refused.
func TestATenantIsChangedByAGlobalAdministrator(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	admin := storedUser(t, s, "admin")
	update := func(caller *auth.User, id, body string) *httptest.ResponseRecorder {
		return consoleAs(caller, s.handleUpdateTenant, http.MethodPut, map[string]string{"tenant": id}, body)
	}
	before, err := s.authManager.GetTenant(ctx, "acme")
	require.NoError(t, err)

	changed := consoleData[auth.Tenant](t, update(admin, "acme", `{"displayName":"Acme Inc","status":"inactive",`+
		`"maxBuckets":5,"maxAccessKeys":7,"maxStorageBytes":1024,"maxBandwidthBytesPerSec":2048,"metadata":{"plan":"gold"}}`), http.StatusOK)
	assert.Equal(t, "Acme Inc", changed.DisplayName)
	stored, err := s.authManager.GetTenant(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, "Acme Inc", stored.DisplayName)
	assert.Equal(t, auth.TenantStatusInactive, stored.Status)
	assert.Equal(t, int64(5), stored.MaxBuckets)
	assert.Equal(t, int64(7), stored.MaxAccessKeys)
	assert.Equal(t, int64(1024), stored.MaxStorageBytes)
	assert.Equal(t, int64(2048), stored.MaxBandwidthBytesPerSec)
	assert.Equal(t, map[string]string{"plan": "gold"}, stored.Metadata)
	assert.Equal(t, before.Description, stored.Description, "what is not named stays")

	rec := httptest.NewRecorder()
	s.handleUpdateTenant(rec, httptest.NewRequest(http.MethodPut, "/", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	for _, tc := range []struct {
		caller *auth.User
		id     string
		body   string
		status int
	}{
		{tenantAdmin, "acme", `{"displayName":"x"}`, http.StatusForbidden},
		{admin, "nope", `{"displayName":"x"}`, http.StatusNotFound},
		{admin, "acme", `not json`, http.StatusBadRequest},
		{admin, "acme", `{"maxBandwidthBytesPerSec":-1}`, http.StatusBadRequest},
		{admin, "acme", `{"maxBuckets":-1}`, http.StatusBadRequest},
		{admin, "acme", `{"maxAccessKeys":-1}`, http.StatusBadRequest},
		{admin, "acme", `{"maxStorageBytes":-1}`, http.StatusBadRequest},
		{admin, "acme", `{"status":"bogus"}`, http.StatusBadRequest},
	} {
		assert.Equal(t, tc.status, update(tc.caller, tc.id, tc.body).Code, tc.body)
	}
	stored, err = s.authManager.GetTenant(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, "Acme Inc", stored.DisplayName, "refused changes change nothing")
	assert.Equal(t, int64(5), stored.MaxBuckets)
	assert.Equal(t, auth.TenantStatusInactive, stored.Status)
}
