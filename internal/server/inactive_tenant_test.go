package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Nothing of an inactive tenant is served or issued: its static website is
// not, its users get no temporary credentials. Reactivating it gives them back.
func TestAnInactiveTenantServesNoWebsiteAndIssuesNoCredentials(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "dormant", Name: "dormant", Status: auth.TenantStatusActive}))
	now := time.Now().Unix()
	user := &auth.User{ID: "dormant-user", Username: "dormant-user", TenantID: "dormant", Status: auth.UserStatusActive,
		Roles: []string{auth.RoleUser}, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, s.authManager.CreateUser(ctx, user))

	const site = "dormant-site"
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "dormant", site, user.ID))
	require.NoError(t, s.bucketManager.SetWebsite(ctx, "dormant", site, &bucket.WebsiteConfig{IndexDocument: "index.html"}))
	_, err := s.objectManager.PutObject(ctx, "dormant/"+site, "index.html", strings.NewReader("<h1>hello</h1>"), http.Header{})
	require.NoError(t, err)

	served := func() int {
		rr := httptest.NewRecorder()
		s.handleWebsiteRequest(rr, httptest.NewRequest(http.MethodGet, "/", nil), site)
		return rr.Code
	}
	setStatus := func(status string) {
		tenant, err := s.authManager.GetTenant(ctx, "dormant")
		require.NoError(t, err)
		tenant.Status = status
		require.NoError(t, s.authManager.UpdateTenant(ctx, tenant))
	}

	require.Equal(t, http.StatusOK, served(), "the website of an active tenant")
	require.NoError(t, s.authorizeSTSSubject(ctx, user), "credentials for a user of an active tenant")

	setStatus(auth.TenantStatusInactive)
	assert.Equal(t, http.StatusForbidden, served(), "the website of an inactive tenant")
	assert.Error(t, s.authorizeSTSSubject(ctx, user), "credentials for a user of an inactive tenant")

	setStatus(auth.TenantStatusActive)
	assert.Equal(t, http.StatusOK, served(), "its website once it is active again")
	assert.NoError(t, s.authorizeSTSSubject(ctx, user), "credentials once it is active again")
}
