package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type keyAnswer struct {
	ID        string `json:"id"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	UserID    string `json:"userId"`
	Status    string `json:"status"`
}

func keyCall(s *Server, caller *auth.User, handler http.HandlerFunc, method string, vars map[string]string) *httptest.ResponseRecorder {
	return consoleAs(caller, handler, method, vars, "")
}

// A user's access keys are listed and made by the user, with the permission to
// manage its own, or by who administers it, within the tenant's quota; their
// secret is shown once, when made. The audit log names who made one.
func TestAccessKeysAreMadeByTheirUserOrWhoAdministersIt(t *testing.T) {
	s := newClusterTestNodeWith(t, func(cfg *config.Config) { cfg.Audit.Enable = true })
	ctx := context.Background()
	tenantAdmin, member, betaUser, _ := userUpdateFixture(t, s)
	admin := storedUser(t, s, "admin")
	keyHolder := consoleUser(t, s, "key-holder", true)
	keyless := consoleUser(t, s, "keyless", false)
	make := func(caller *auth.User, owner string) *httptest.ResponseRecorder {
		return keyCall(s, caller, s.handleCreateAccessKey, http.MethodPost, map[string]string{"user": owner})
	}
	list := func(caller *auth.User, owner string) *httptest.ResponseRecorder {
		return keyCall(s, caller, s.handleListAccessKeys, http.MethodGet, map[string]string{"user": owner})
	}

	own := consoleData[keyAnswer](t, make(keyHolder, keyHolder.ID), http.StatusOK)
	assert.NotEmpty(t, own.SecretKey)
	assert.Equal(t, keyHolder.ID, own.UserID)
	listed := consoleData[[]map[string]any](t, list(keyHolder, keyHolder.ID), http.StatusOK)
	require.Len(t, listed, 1)
	assert.Equal(t, own.AccessKey, listed[0]["id"])
	assert.NotContains(t, listed[0], "secretKey", "a secret is shown once")

	byTenantAdmin := consoleData[keyAnswer](t, make(tenantAdmin, member.ID), http.StatusOK)
	assert.Equal(t, member.ID, byTenantAdmin.UserID)
	s.auditManager.Flush()
	logs, _, err := s.auditManager.GetLogs(ctx, &audit.AuditLogFilters{EventType: audit.EventTypeAccessKeyCreated})
	require.NoError(t, err)
	actors := map[string]string{}
	for _, log := range logs {
		actors[log.ResourceID] = log.UserID
	}
	assert.Equal(t, tenantAdmin.ID, actors[byTenantAdmin.AccessKey], "the audit log names who made it")
	assert.Equal(t, keyHolder.ID, actors[own.AccessKey])

	for _, tc := range []struct {
		rec    *httptest.ResponseRecorder
		status int
	}{
		{make(nil, member.ID), http.StatusForbidden},
		{make(member, betaUser.ID), http.StatusForbidden},
		{make(keyless, keyless.ID), http.StatusForbidden},
		{make(tenantAdmin, betaUser.ID), http.StatusForbidden},
		{make(admin, "ghost"), http.StatusNotFound},
		{list(nil, member.ID), http.StatusForbidden},
		{list(member, betaUser.ID), http.StatusForbidden},
		{list(tenantAdmin, betaUser.ID), http.StatusForbidden},
		{list(admin, "ghost"), http.StatusNotFound},
	} {
		assert.Equal(t, tc.status, tc.rec.Code, tc.rec.Body.String())
	}

	tenant, err := s.authManager.GetTenant(ctx, "acme")
	require.NoError(t, err)
	tenant.MaxAccessKeys = 1
	require.NoError(t, s.authManager.UpdateTenant(ctx, tenant))
	rec := make(admin, member.ID)
	assert.Equal(t, http.StatusForbidden, rec.Code, "acme holds its one key")
	assert.Contains(t, rec.Body.String(), "quota exceeded")
}

// ownerUnreadable is an auth manager that cannot read one user.
type ownerUnreadable struct {
	auth.Manager
	id string
}

func (o ownerUnreadable) GetUser(ctx context.Context, id string) (*auth.User, error) {
	if id == o.id {
		return nil, errors.New("the user could not be read")
	}
	return o.Manager.GetUser(ctx, id)
}

// A key is deleted by its user or by who administers it; one that does not
// exist is not found; the administrator of a tenant deletes none whose user it
// cannot tell is of its tenant.
func TestAnAccessKeyIsDeletedByItsUserOrWhoAdministersIt(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, member, betaUser, _ := userUpdateFixture(t, s)
	admin := storedUser(t, s, "admin")
	key := func(owner string) string {
		created, err := s.authManager.GenerateAccessKey(ctx, owner)
		require.NoError(t, err)
		return created.AccessKeyID
	}
	remove := func(caller *auth.User, id string) *httptest.ResponseRecorder {
		return keyCall(s, caller, s.handleDeleteAccessKey, http.MethodDelete, map[string]string{"accessKey": id})
	}
	gone := func(id string) bool {
		_, err := s.authManager.GetAccessKey(ctx, id)
		return err != nil
	}

	members, betas := key(member.ID), key(betaUser.ID)
	for _, tc := range []struct {
		caller *auth.User
		id     string
		status int
	}{
		{nil, members, http.StatusForbidden},
		{betaUser, members, http.StatusForbidden},
		{tenantAdmin, betas, http.StatusForbidden},
		{admin, "AKIANOSUCHKEY", http.StatusNotFound},
	} {
		assert.Equal(t, tc.status, remove(tc.caller, tc.id).Code, tc.id)
	}
	assert.False(t, gone(members))
	assert.False(t, gone(betas))

	assert.Equal(t, http.StatusOK, remove(member, members).Code, "its own")
	assert.True(t, gone(members))
	assert.Equal(t, http.StatusOK, remove(admin, betas).Code)
	assert.True(t, gone(betas))

	unreadable := key(betaUser.ID)
	working := s.authManager
	s.authManager = ownerUnreadable{working, betaUser.ID}
	assert.NotEqual(t, http.StatusOK, remove(tenantAdmin, unreadable).Code, "a key whose user cannot be read")
	s.authManager = working
	assert.False(t, gone(unreadable))
}
