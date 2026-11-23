package server

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deliveredLogs is everything written under prefix of the bucket at path.
func deliveredLogs(t *testing.T, s *Server, path, prefix string) string {
	t.Helper()
	ctx := context.Background()
	listed, err := s.objectManager.ListObjects(ctx, path, prefix, "", "", 1000)
	require.NoError(t, err)
	all := ""
	for _, o := range listed.Objects {
		_, reader, err := s.objectManager.GetObject(ctx, path, o.Key)
		require.NoError(t, err)
		body, err := io.ReadAll(reader)
		reader.Close() //nolint:errcheck
		require.NoError(t, err)
		all += string(body)
	}
	return all
}

// A tenant bucket's access logs reach its target, a bucket of its tenant,
// whoever made the requests: a user of the tenant, an anonymous client, a
// global administrator. A target elsewhere, configured before that was
// checked, gets nothing.
func TestATenantBucketsAccessLogsReachItsTarget(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", Status: "active"}))
	for _, name := range []string{"acme-src", "acme-logs", "acme-astray"} {
		require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", name, ""))
	}
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "global-bucket", ""))
	require.NoError(t, s.bucketManager.SetLogging(ctx, "acme", "acme-src", &bucket.LoggingConfig{TargetBucket: "acme-logs", TargetPrefix: "logs/"}))
	require.NoError(t, s.bucketManager.SetLogging(ctx, "acme", "acme-astray", &bucket.LoggingConfig{TargetBucket: "global-bucket", TargetPrefix: "planted/"}))

	l := NewBucketAccessLogger(s.bucketManager, s.objectManager, s.bucketTenant)
	for _, requester := range []string{"acme-user", "-", "admin"} {
		l.Log(AccessLogEntry{Timestamp: time.Now(), BucketName: "acme-src", ObjectKey: "o", Operation: "REST.GET.OBJECT", RemoteIP: "127.0.0.1", Requester: requester, HTTPStatus: 200})
	}
	l.Log(AccessLogEntry{Timestamp: time.Now(), BucketName: "acme-astray", ObjectKey: "o", Operation: "REST.GET.OBJECT", RemoteIP: "127.0.0.1", Requester: "acme-user", HTTPStatus: 200})
	l.Stop()

	logs := deliveredLogs(t, s, "acme/acme-logs", "logs/")
	for _, requester := range []string{"acme-user", "-", "admin"} {
		assert.Contains(t, logs, "] 127.0.0.1 "+requester+" ", "a request of "+requester)
	}
	assert.Empty(t, deliveredLogs(t, s, "global-bucket", ""), "nothing in a bucket of another tenant")
}

// In the console too, a bucket's inventory reports go only to a bucket the
// caller may write to.
func TestInventoryReportsGoOnlyWhereTheCallerMayWrite(t *testing.T) {
	s := administrators(t)
	ctx := context.Background()
	for _, name := range []string{"inv-src", "inv-open", "inv-closed"} {
		require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", name, ""))
	}
	store := s.authManager.(interface {
		SetUserPermissions(string, *auth.UserPermissions) error
	})
	require.NoError(t, store.SetUserPermissions("acme-member", &auth.UserPermissions{Global: []string{auth.ActionConsoleAccess}, Buckets: []auth.BucketGrant{
		{Bucket: "inv-src", Actions: []string{auth.ActionPutBucketInventory, auth.ActionGetBucketInventory}},
		{Bucket: "inv-open", Actions: []string{auth.ActionPutObject}},
	}}))
	into := func(target string) string {
		return `{"enabled":true,"frequency":"daily","format":"csv","destination_bucket":"` + target + `","destination_prefix":"reports/"}`
	}
	src := map[string]string{"bucket": "inv-src"}

	assert.Equal(t, http.StatusForbidden, callAs(t, s, "acme-member", s.handlePutBucketInventory, src, into("inv-closed")), "a bucket it may not write to")
	assert.Equal(t, http.StatusOK, callAs(t, s, "acme-member", s.handlePutBucketInventory, src, into("inv-open")), "a bucket it may write to")
}
