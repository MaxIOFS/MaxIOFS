package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/inventory"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/notifications"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// anonymousGet reads an object without credentials, as a share link does.
func anonymousGet(t *testing.T, s *Server, bucketName, key string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/"+bucketName+"/"+key, nil)
	w := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(w, req)
	body, _ := io.ReadAll(w.Result().Body)
	return w.Code, string(body)
}

// giveBucketState gives a bucket a share, a replication rule, an inventory
// configuration, a permission, a notification configuration and integrity
// scans.
func giveBucketState(t *testing.T, s *Server, bucketName string) {
	t.Helper()
	ctx := context.Background()
	_, err := s.shareManager.CreateShare(ctx, bucketName, "doc", "", "AKID", "secret", "admin", nil)
	require.NoError(t, err)
	require.NoError(t, s.replicationManager.CreateRule(ctx, testRule("rule-"+bucketName, bucketName)))
	require.NoError(t, s.inventoryManager.CreateConfig(ctx, &inventory.InventoryConfig{BucketName: bucketName, Enabled: true,
		Frequency: "daily", Format: "csv", DestinationBucket: "elsewhere", IncludedFields: []string{"object_key"}, ScheduleTime: "02:00"}))
	_, err = s.db.Exec(`INSERT INTO bucket_permissions (id, bucket_name, bucket_tenant_id, user_id, permission_level, granted_by, granted_at)
		VALUES (?, ?, '', NULL, 'read', 'admin', 1)`, "perm-"+bucketName, bucketName)
	require.NoError(t, err)
	require.NoError(t, s.notificationManager.PutConfiguration(ctx, &notifications.NotificationConfiguration{BucketName: bucketName,
		Rules: []notifications.NotificationRule{{ID: "r", Enabled: true, WebhookURL: "https://hooks.example.com/x", Events: []notifications.EventType{"s3:ObjectCreated:*"}}}}))
	s.saveIntegrityResult(ctx, bucketName, &object.BucketIntegrityReport{Bucket: bucketName}, "manual")
	require.NotEmpty(t, bucketState(t, s, bucketName))
}

// bucketState names what a bucket holds of the state giveBucketState gives it.
func bucketState(t *testing.T, s *Server, bucketName string) []string {
	t.Helper()
	ctx := context.Background()
	var held []string
	for _, c := range []struct{ what, query string }{
		{"share", `SELECT COUNT(*) FROM shares WHERE bucket_name = ?`},
		{"replication rule", `SELECT COUNT(*) FROM replication_rules WHERE source_bucket = ?`},
		{"inventory", `SELECT COUNT(*) FROM bucket_inventory_configs WHERE bucket_name = ?`},
		{"permission", `SELECT COUNT(*) FROM bucket_permissions WHERE bucket_name = ?`},
		{"owner policy", `SELECT COUNT(*) FROM iam_inline_policies WHERE name = 'owner-' || ?`},
	} {
		if hasRow(t, s, c.query, bucketName) {
			held = append(held, c.what)
		}
	}
	if config, err := s.notificationManager.GetConfiguration(ctx, "", bucketName); err == nil && config != nil && len(config.Rules) > 0 {
		held = append(held, "notification configuration")
	}
	if kv, ok := s.metadataStore.(metadata.RawKVStore); ok {
		if _, err := kv.GetRaw(ctx, integrityScanKey(bucketName)); err == nil {
			held = append(held, "integrity scans")
		}
	}
	return held
}

// A bucket a client deletes, empty or not, takes what it owned with it: a
// share link of the old bucket opens nothing in a new bucket of the name.
func TestADeletedBucketTakesWhatItOwned(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	for _, force := range []bool{false, true} {
		name := "owned-plain"
		if force {
			name = "owned-forced"
		}
		require.NoError(t, s.bucketManager.CreateBucket(ctx, "", name, "admin"))
		_, err := s.objectManager.PutObject(ctx, name, "doc", strings.NewReader("first"), http.Header{})
		require.NoError(t, err)
		giveBucketState(t, s, name)
		code, body := anonymousGet(t, s, name, "doc")
		require.Equal(t, http.StatusOK, code, body)
		require.Equal(t, "first", body)

		if force {
			require.NoError(t, s.bucketManager.ForceDeleteBucket(ctx, "", name))
		} else {
			_, err := s.objectManager.DeleteObject(ctx, name, "doc", false)
			require.NoError(t, err)
			require.NoError(t, s.bucketManager.DeleteBucket(ctx, "", name))
		}
		assert.Empty(t, bucketState(t, s, name), name)

		require.NoError(t, s.bucketManager.CreateBucket(ctx, "", name, "admin"))
		_, err = s.objectManager.PutObject(ctx, name, "doc", strings.NewReader("second"), http.Header{})
		require.NoError(t, err)
		code, body = anonymousGet(t, s, name, "doc")
		assert.Equal(t, http.StatusForbidden, code, "%s: %s", name, body)
		assert.NotContains(t, body, "second")
	}
}

// A bucket created where a former bucket of the name left rows starts without
// them; the policies it had are gone and only the new owner's is granted.
func TestANewBucketStartsWithoutWhatAFormerOneLeft(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	im := s.authManager.(auth.IAMManager)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "left-behind", "admin"))
	giveBucketState(t, s, "left-behind")
	require.NoError(t, im.PutIAMInlinePolicy(ctx, auth.IAMTargetUser, "former-reader", "bucket-left-behind",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`))
	// As an earlier release left them: the bucket entry went, nothing else.
	require.NoError(t, s.metadataStore.DeleteBucket(ctx, "", "left-behind"))
	time.Sleep(1100 * time.Millisecond)

	s.dropOrphanedBucketState(ctx)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "left-behind", "new-owner"))
	assert.Equal(t, []string{"owner policy"}, bucketState(t, s, "left-behind"))
	assert.True(t, hasRow(t, s, `SELECT COUNT(*) FROM iam_inline_policies WHERE name = 'owner-left-behind' AND target_id = 'new-owner'`))
	assert.False(t, hasRow(t, s, `SELECT COUNT(*) FROM iam_inline_policies WHERE name = 'bucket-left-behind'`))
}

// The notification configuration and integrity scans a node keeps for a
// bucket it no longer holds are dropped at start; those of its buckets stay.
func TestTheStateOfDeletedBucketsIsDroppedAtStart(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	for _, name := range []string{"kept-state", "orphan-state"} {
		require.NoError(t, s.bucketManager.CreateBucket(ctx, "", name, "admin"))
		require.NoError(t, s.notificationManager.PutConfiguration(ctx, &notifications.NotificationConfiguration{BucketName: name,
			Rules: []notifications.NotificationRule{{ID: "r", Enabled: true, WebhookURL: "https://hooks.example.com/x", Events: []notifications.EventType{"s3:ObjectCreated:*"}}}}))
		s.saveIntegrityResult(ctx, name, &object.BucketIntegrityReport{Bucket: name}, "manual")
	}
	require.NoError(t, s.metadataStore.DeleteBucket(ctx, "", "orphan-state"))

	s.dropOrphanedBucketState(ctx)
	assert.Subset(t, bucketState(t, s, "kept-state"), []string{"notification configuration", "integrity scans"})
	held := bucketState(t, s, "orphan-state")
	assert.NotContains(t, held, "notification configuration")
	assert.NotContains(t, held, "integrity scans")
}

// With every node holding every bucket, a bucket deleted on one node takes
// its rows on every node, and each node drops what it alone kept.
func TestHADeletedBucketTakesWhatItOwnedOnEveryNode(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "everywhere", "admin"))
	_, err := p.a.shareManager.CreateShare(ctx, "everywhere", "doc", "", "AKID", "secret", "admin", nil)
	require.NoError(t, err)
	require.NoError(t, p.b.notificationManager.PutConfiguration(ctx, &notifications.NotificationConfiguration{BucketName: "everywhere",
		Rules: []notifications.NotificationRule{{ID: "r", Enabled: true, WebhookURL: "https://hooks.example.com/x", Events: []notifications.EventType{"s3:ObjectCreated:*"}}}}))
	require.True(t, hasRow(t, p.b, `SELECT COUNT(*) FROM shares WHERE bucket_name = 'everywhere'`))

	require.NoError(t, p.a.bucketManager.DeleteBucket(ctx, "", "everywhere"))
	require.False(t, hasBucket(p.b, "", "everywhere"))
	assert.False(t, hasRow(t, p.b, `SELECT COUNT(*) FROM shares WHERE bucket_name = 'everywhere'`))
	assert.Empty(t, bucketState(t, p.b, "everywhere"))
}
