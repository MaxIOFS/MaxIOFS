package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settingCall is handler's answer to user, allowed every S3 action, changing
// a setting of bucketName with body.
func settingCall(user *auth.User, handler http.HandlerFunc, bucketName, query, body string) *httptest.ResponseRecorder {
	req := mux.SetURLVars(consoleRequestWith(user, body, "s3:*"), map[string]string{"bucket": bucketName})
	req.URL.RawQuery = query
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// lockedBucket is a bucket of tenantID with versioning and Object Lock on.
func lockedBucket(t *testing.T, s *Server, tenantID, name string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.bucketManager.CreateBucket(ctx, tenantID, name, ""))
	require.NoError(t, s.bucketManager.SetVersioning(ctx, tenantID, name, &bucket.VersioningConfig{Status: "Enabled"}))
	require.NoError(t, s.bucketManager.SetObjectLockConfig(ctx, tenantID, name, &bucket.ObjectLockConfig{ObjectLockEnabled: true}))
}

// Each change of a bucket's settings from the console is in the audit log,
// with what was done to it.
func TestABucketsSettingsChangesAreAudited(t *testing.T) {
	s := newClusterTestNodeWith(t, func(cfg *config.Config) { cfg.Audit.Enable = true })
	ctx := context.Background()
	admin := storedUser(t, s, "admin")
	lockedBucket(t, s, "", "settings")
	for _, tc := range []struct {
		handler http.HandlerFunc
		body    string
		event   string
		action  string
	}{
		{s.handlePutBucketOwnership, `{"objectOwnership":"ObjectWriter"}`, "bucket_ownership_controls_configured", audit.ActionUpdate},
		{s.handlePutBucketEncryption, `{"type":"AES256"}`, "bucket_encryption_configured", audit.ActionUpdate},
		{s.handlePutPublicAccessBlock, `{"blockPublicAcls":true,"ignorePublicAcls":true,"blockPublicPolicy":true,"restrictPublicBuckets":true}`,
			"bucket_public_access_block_configured", audit.ActionUpdate},
		{s.handlePutBucketNotification, `{"rules":[{"id":"r1","enabled":true,"webhookUrl":"https://hooks.example.test/x","events":["s3:ObjectCreated:*"]}]}`,
			"bucket_notification_configured", audit.ActionUpdate},
		{s.handleDeleteBucketNotification, ``, "bucket_notification_deleted", audit.ActionDelete},
		{s.handlePutObjectLockConfiguration, `{"mode":"GOVERNANCE","days":2}`, "bucket_object_lock_configured", audit.ActionUpdate},
	} {
		rec := settingCall(admin, tc.handler, "settings", "", tc.body)
		require.Equal(t, http.StatusOK, rec.Code, "%s: %s", tc.event, rec.Body.String())
		s.auditManager.Flush()
		logs, total, err := s.auditManager.GetLogs(ctx, &audit.AuditLogFilters{EventType: tc.event})
		require.NoError(t, err)
		require.Equal(t, 1, total, tc.event)
		assert.Equal(t, tc.action, logs[0].Action, tc.event)
		assert.Equal(t, audit.StatusSuccess, logs[0].Status, tc.event)
		assert.Equal(t, "settings", logs[0].ResourceID, tc.event)
		assert.Equal(t, admin.ID, logs[0].UserID, tc.event)
	}
}

// The administrator of a tenant changes the default retention of its
// tenant's bucket, a global administrator does not; a retention AWS would
// refuse is refused, and so are a shorter one and one of another mode.
func TestARetentionIsChangedByTheTenantsAdministrator(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	admin := storedUser(t, s, "admin")
	lockedBucket(t, s, "acme", "worm")

	assert.Equal(t, http.StatusForbidden,
		settingCall(admin, s.handlePutObjectLockConfiguration, "worm", "tenantId=acme", `{"mode":"COMPLIANCE","days":5}`).Code)
	rec := settingCall(tenantAdmin, s.handlePutObjectLockConfiguration, "worm", "", `{"mode":"COMPLIANCE","days":5}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	acme, err := s.bucketManager.GetObjectLockConfig(ctx, "acme", "worm")
	require.NoError(t, err)
	require.NotNil(t, acme.Rule)
	assert.Equal(t, "COMPLIANCE", acme.Rule.DefaultRetention.Mode)
	require.NotNil(t, acme.Rule.DefaultRetention.Days)
	assert.Equal(t, 5, *acme.Rule.DefaultRetention.Days)

	for _, body := range []string{
		`{"mode":"COMPLIANCE","days":-1,"years":1}`,
		`{"mode":"COMPLIANCE","days":0}`,
		`{"mode":"FOO","days":9}`,
		`{"mode":"COMPLIANCE","days":4}`,
		`{"mode":"GOVERNANCE","days":9}`,
	} {
		assert.Equal(t, http.StatusBadRequest, settingCall(tenantAdmin, s.handlePutObjectLockConfiguration, "worm", "", body).Code, body)
	}
	assert.Equal(t, http.StatusNotFound, settingCall(admin, s.handlePutObjectLockConfiguration, "nope", "", `{"mode":"COMPLIANCE","days":5}`).Code)
	acme, err = s.bucketManager.GetObjectLockConfig(ctx, "acme", "worm")
	require.NoError(t, err)
	assert.Equal(t, 5, *acme.Rule.DefaultRetention.Days, "refused changes change nothing")
}

// aclUnreadable is a bucket manager that cannot read a bucket's ACL.
type aclUnreadable struct {
	bucket.Manager
}

func (aclUnreadable) GetBucketACL(context.Context, string, string) (interface{}, error) {
	return nil, errors.New("the ACL could not be read")
}

// ACLs are not turned off for a bucket whose ACL cannot be read: it may grant
// others access.
func TestACLsAreNotTurnedOffBlind(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	admin := storedUser(t, s, "admin")
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "acl-blind", ""))
	require.NoError(t, s.bucketManager.SetOwnershipControls(ctx, "", "acl-blind",
		&bucket.OwnershipControlsConfig{ObjectOwnership: bucket.ObjectOwnershipObjectWriter}))
	working := s.bucketManager
	s.bucketManager = aclUnreadable{working}

	rec := settingCall(admin, s.handlePutBucketOwnership, "acl-blind", "", `{"objectOwnership":"BucketOwnerEnforced"}`)
	assert.NotEqual(t, http.StatusOK, rec.Code, rec.Body.String())
	s.bucketManager = working
	ownership, err := working.GetOwnershipControls(ctx, "", "acl-blind")
	require.NoError(t, err)
	assert.Equal(t, bucket.ObjectOwnershipObjectWriter, ownership.ObjectOwnership)
}
