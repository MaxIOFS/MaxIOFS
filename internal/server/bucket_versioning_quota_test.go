package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bucketUnreadable is a bucket manager that cannot read a bucket.
type bucketUnreadable struct {
	bucket.Manager
}

func (bucketUnreadable) GetBucketInfo(context.Context, string, string) (*bucket.Bucket, error) {
	return nil, errors.New("the bucket could not be read")
}

// Versioning is enabled and suspended by who may configure the bucket, but
// never suspended on a bucket with Object Lock, nor on one that cannot be
// read to tell.
func TestVersioningIsNeverSuspendedUnderObjectLock(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "plain", tenantAdmin.ID))
	lockedBucket(t, s, "acme", "worm")
	set := func(name, body string) *httptest.ResponseRecorder {
		req := mux.SetURLVars(consoleRequestWith(tenantAdmin, body, "s3:*"), map[string]string{"bucket": name})
		rec := httptest.NewRecorder()
		s.handlePutBucketVersioning(rec, req)
		return rec
	}
	status := func(name string) string {
		versioning, err := s.bucketManager.GetVersioning(ctx, "acme", name)
		require.NoError(t, err)
		if versioning == nil {
			return ""
		}
		return versioning.Status
	}

	require.Equal(t, http.StatusOK, set("plain", `{"status":"Enabled"}`).Code)
	assert.Equal(t, "Enabled", status("plain"))
	require.Equal(t, http.StatusOK, set("plain", `{"status":"Suspended"}`).Code)
	assert.Equal(t, "Suspended", status("plain"))
	for name, tc := range map[string]struct {
		body   string
		status int
	}{
		"worm":    {`{"status":"Suspended"}`, http.StatusConflict},
		"plain":   {`{"status":"Off"}`, http.StatusBadRequest},
		"nowhere": {`{"status":"Enabled"}`, http.StatusNotFound},
	} {
		assert.Equal(t, tc.status, set(name, tc.body).Code, name)
	}
	assert.Equal(t, "Enabled", status("worm"))

	working := s.bucketManager
	s.bucketManager = bucketUnreadable{working}
	assert.NotEqual(t, http.StatusOK, set("worm", `{"status":"Suspended"}`).Code, "a bucket that cannot be read")
	s.bucketManager = working
	assert.Equal(t, "Enabled", status("worm"))
}

// No bucket of a tenant is given a size quota larger than the tenant's
// storage, whoever owns it.
func TestABucketQuotaStaysWithinItsTenants(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, member, _, _ := userUpdateFixture(t, s)
	tenant, err := s.authManager.GetTenant(ctx, "acme")
	require.NoError(t, err)
	tenant.MaxStorageBytes = 1000
	require.NoError(t, s.authManager.UpdateTenant(ctx, tenant))
	for _, creator := range []*auth.User{tenantAdmin, member} {
		rec := httptest.NewRecorder()
		s.handleCreateBucket(rec, consoleRequestWith(creator, `{"name":"by-`+creator.ID+`"}`, "s3:*"))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	quota := func(name, body string) int {
		req := mux.SetURLVars(consoleRequestWith(tenantAdmin, body, "s3:*", auth.ActionPutBucketQuota), map[string]string{"bucket": name})
		rec := httptest.NewRecorder()
		s.handlePutBucketQuota(rec, req)
		return rec.Code
	}

	for _, name := range []string{"by-" + tenantAdmin.ID, "by-" + member.ID} {
		assert.Equal(t, http.StatusOK, quota(name, `{"maxSizeBytes":900}`), name)
		assert.Equal(t, http.StatusBadRequest, quota(name, `{"maxSizeBytes":5000}`), name)
		info, err := s.bucketManager.GetBucketInfo(ctx, "acme", name)
		require.NoError(t, err)
		require.NotNil(t, info.Quota, name)
		assert.Equal(t, int64(900), info.Quota.MaxSizeBytes, name)
	}
}
