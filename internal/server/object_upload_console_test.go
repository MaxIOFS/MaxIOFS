package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uploadAs(s *Server, user *auth.User, bucketName, key, content string, headers map[string]string, actions ...string) *httptest.ResponseRecorder {
	req := mux.SetURLVars(consoleRequestWith(user, content, actions...), map[string]string{"bucket": bucketName, "object": key})
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	s.handleUploadObject(rec, req)
	return rec
}

// An object uploaded from the console keeps the retention it asks for, or
// takes its bucket's default; lock headers the bucket cannot take are the
// request's fault.
func TestAnUploadKeepsTheRetentionItAsksFor(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	lockedBucket(t, s, "acme", "locked")
	days := 30
	require.NoError(t, s.bucketManager.SetObjectLockConfig(ctx, "acme", "locked", &bucket.ObjectLockConfig{ObjectLockEnabled: true,
		Rule: &bucket.ObjectLockRule{DefaultRetention: &bucket.DefaultRetention{Mode: "COMPLIANCE", Days: &days}}}))
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "plain", tenantAdmin.ID))
	retentionOf := func(key string) (string, time.Time) {
		retention, err := s.objectManager.GetObjectRetention(ctx, "acme/locked", key)
		require.NoError(t, err)
		return retention.Mode, retention.RetainUntilDate
	}
	inAnHour := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	require.Equal(t, http.StatusOK, uploadAs(s, tenantAdmin, "locked", "asked", "x", map[string]string{
		"x-amz-object-lock-mode":              "GOVERNANCE",
		"x-amz-object-lock-retain-until-date": inAnHour.Format(time.RFC3339),
	}, "s3:*").Code)
	mode, until := retentionOf("asked")
	assert.Equal(t, "GOVERNANCE", mode, "the retention asked for, not the bucket's")
	assert.WithinDuration(t, inAnHour, until, time.Second)

	require.Equal(t, http.StatusOK, uploadAs(s, tenantAdmin, "locked", "plain", "x", nil, "s3:*").Code)
	mode, until = retentionOf("plain")
	assert.Equal(t, "COMPLIANCE", mode, "the bucket's default")
	assert.WithinDuration(t, time.Now().AddDate(0, 0, days), until, time.Minute)

	for name, headers := range map[string]map[string]string{
		"a mode of no kind":       {"x-amz-object-lock-mode": "FOREVER", "x-amz-object-lock-retain-until-date": inAnHour.Format(time.RFC3339)},
		"a date passed":           {"x-amz-object-lock-mode": "GOVERNANCE", "x-amz-object-lock-retain-until-date": "2020-01-01T00:00:00Z"},
		"a legal hold of no kind": {"x-amz-object-lock-legal-hold": "MAYBE"},
	} {
		assert.Equal(t, http.StatusBadRequest, uploadAs(s, tenantAdmin, "locked", "bad", "x", headers, "s3:*").Code, name)
	}
	assert.Equal(t, http.StatusBadRequest, uploadAs(s, tenantAdmin, "plain", "bad", "x",
		map[string]string{"x-amz-object-lock-legal-hold": "ON"}, "s3:*").Code, "a bucket without Object Lock")
	assert.Equal(t, http.StatusForbidden, uploadAs(s, tenantAdmin, "locked", "bad", "x",
		map[string]string{"x-amz-object-lock-legal-hold": "ON"}, "s3:PutObject").Code)
	for _, header := range []string{"x-amz-object-lock-mode", "x-amz-object-lock-retain-until-date"} {
		assert.Equal(t, http.StatusForbidden, uploadAs(s, tenantAdmin, "locked", "bad", "x",
			map[string]string{header: "GOVERNANCE"}, "s3:PutObject").Code, header)
	}
	assert.Equal(t, http.StatusOK, uploadAs(s, tenantAdmin, "locked", "plain-write", "x", nil, "s3:PutObject").Code,
		"no lock headers, no lock permission needed")
}

// A tenant's storage limit counts what an upload adds: overwriting an object
// with one as large adds nothing.
func TestAnUploadCountsWhatItAddsToItsTenant(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "small", Name: "small", Status: "active", MaxStorageBytes: 10}))
	admin := consoleUserOf(t, s, "small-admin", "small", auth.RoleTenantAdmin)
	require.Equal(t, http.StatusOK, func() int {
		rec := httptest.NewRecorder()
		s.handleCreateBucket(rec, consoleRequestWith(admin, `{"name":"tiny"}`, "s3:*"))
		return rec.Code
	}(), "the tenant's bucket")
	info, err := s.bucketManager.GetBucketInfo(ctx, "small", "tiny")
	require.NoError(t, err)
	require.Equal(t, "tenant", info.OwnerType)

	require.Equal(t, http.StatusOK, uploadAs(s, admin, "tiny", "a", "12345678", nil, "s3:*").Code)
	assert.Equal(t, http.StatusOK, uploadAs(s, admin, "tiny", "a", "87654321", nil, "s3:*").Code, "nothing added")
	assert.Equal(t, http.StatusForbidden, uploadAs(s, admin, "tiny", "b", "12345", nil, "s3:*").Code, "past the limit")
}

// A failed write is answered with the status of its cause.
func TestAFailedWriteHasTheStatusOfItsCause(t *testing.T) {
	for err, status := range map[error]int{
		object.ErrBucketQuotaExceeded:                           http.StatusForbidden,
		auth.ErrStorageQuotaExceeded:                            http.StatusForbidden,
		object.ErrNoRetentionConfiguration:                      http.StatusBadRequest,
		object.ErrInvalidRetentionMode:                          http.StatusBadRequest,
		object.ErrRetentionDateInPast:                           http.StatusBadRequest,
		fmt.Errorf("put: %w", object.ErrInvalidLegalHoldStatus): http.StatusBadRequest,
		object.ErrDiskFull:                                      http.StatusInsufficientStorage,
		object.ErrBucketNotFound:                                http.StatusNotFound,
		cluster.ErrClusterDegraded:                              http.StatusServiceUnavailable,
		errors.New("anything else"):                             http.StatusInternalServerError,
	} {
		assert.Equal(t, status, writeErrorStatus(err), err.Error())
	}
}
