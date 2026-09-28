package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newClusterTestNode is a whole server on its own data directory, its routes
// set up.
func newClusterTestNode(t *testing.T) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "maxiofs-migration-node-*")
	require.NoError(t, err)
	cfg := &config.Config{
		Listen:           "127.0.0.1:0",
		ConsoleListen:    "127.0.0.1:0",
		DataDir:          dir,
		LogLevel:         "error",
		PublicAPIURL:     "http://localhost:8080",
		PublicConsoleURL: "http://localhost:8081",
		Storage:          config.StorageConfig{Backend: "filesystem", Root: filepath.Join(dir, "storage")},
		Auth: config.AuthConfig{
			EnableAuth:       true,
			JWTSecret:        "test-jwt-secret-shared",
			EncryptionSecret: "test-secret-key",
		},
		Audit: config.AuditConfig{RetentionDays: 7, DBPath: filepath.Join(dir, "audit.db")},
	}
	srv, err := New(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	srv.serverCtx = ctx
	require.NoError(t, srv.setupRoutes())
	t.Cleanup(func() {
		cancel()
		srv.bucketMigrator.Wait()
		_ = srv.shutdown()
		_ = os.RemoveAll(dir)
	})
	return srv
}

// migrationCluster is a source node and a target node that trust each other.
// intercept, when set, answers a request to the target in its place.
type migrationCluster struct {
	source, target *Server
	sourceID       string
	targetID       string
	token          string
	intercept      atomic.Pointer[func(http.ResponseWriter, *http.Request) bool]
	stop           context.CancelFunc
}

func newMigrationCluster(t *testing.T) *migrationCluster {
	t.Helper()
	c := &migrationCluster{source: newClusterTestNode(t), target: newClusterTestNode(t)}
	ctx := context.Background()
	targetRoutes := c.target.clusterServer.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f := c.intercept.Load(); f != nil && (*f)(w, r) {
			return
		}
		targetRoutes.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	_, err := c.source.clusterManager.InitializeCluster(ctx, "source", "us-east-1", "http://source.invalid")
	require.NoError(t, err)
	c.sourceID, err = c.source.clusterManager.GetLocalNodeID(ctx)
	require.NoError(t, err)
	c.token, err = c.source.clusterManager.GetLocalNodeToken(ctx)
	require.NoError(t, err)
	target := &cluster.Node{Name: "target", Endpoint: ts.URL, NodeToken: c.token, Region: "us-east-1", Priority: 100, Metadata: "{}"}
	require.NoError(t, c.source.clusterManager.AddNode(ctx, target))
	_, err = c.source.db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusHealthy, target.ID)
	require.NoError(t, err)
	c.targetID = target.ID
	require.NoError(t, c.target.clusterManager.AddNode(ctx, &cluster.Node{
		ID: c.sourceID, Name: "source", Endpoint: "http://source.invalid", NodeToken: c.token,
		Region: "us-east-1", Priority: 100, Metadata: "{}",
	}))
	c.startMigrations(t)
	return c
}

// startMigrations starts the source's migrations as a node does at boot.
func (c *migrationCluster) startMigrations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c.stop = cancel
	migrator := c.source.bucketMigrator
	migrator.SetRetryInterval(20 * time.Millisecond)
	migrator.Start(ctx)
	t.Cleanup(func() {
		cancel()
		migrator.Wait()
	})
}

// restartMigrations stops the source's migrations and starts them again with
// what a restarted node keeps: its database and stores, not what it held in
// memory.
func (c *migrationCluster) restartMigrations(t *testing.T) {
	c.stop()
	c.source.bucketMigrator.Wait()
	aclMgr, _ := c.source.bucketManager.GetACLManager().(acl.Manager)
	c.source.bucketGate = cluster.NewBucketWriteGate()
	c.source.bucketMigrator = cluster.NewBucketMigrator(c.source.clusterManager, c.source.objectManager,
		c.source.metadataStore, c.source.bucketManager, aclMgr, c.source.authManager, c.source.bucketGate)
	c.startMigrations(t)
}

func (c *migrationCluster) failTarget(match func(*http.Request) bool) {
	f := func(w http.ResponseWriter, r *http.Request) bool {
		if !match(r) {
			return false
		}
		_, _ = io.Copy(io.Discard, r.Body)
		http.Error(w, "refused by the test", http.StatusInternalServerError)
		return true
	}
	c.intercept.Store(&f)
}

func (c *migrationCluster) migrate(t *testing.T, bucketName string) *cluster.MigrationJob {
	t.Helper()
	job, err := c.source.bucketMigrator.Migrate(context.Background(), bucketName, c.targetID)
	require.NoError(t, err)
	c.source.bucketMigrator.Wait()
	done, err := c.source.clusterManager.GetMigrationJob(context.Background(), job.ID)
	require.NoError(t, err)
	return done
}

// proxiedS3 sends a request to the source's S3 API as another node forwards
// one, for a global administrator.
func (c *migrationCluster) proxiedS3(t *testing.T, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	req.Header.Set("X-MaxIOFS-Proxied", "true")
	cluster.AddClusterProxyHeaders(req, c.targetID, c.token, "admin", "", "admin")
	w := httptest.NewRecorder()
	c.source.httpServer.Handler.ServeHTTP(w, req)
	return w
}

func manifestsOf(t *testing.T, s *Server, path string) map[string][]cluster.VersionManifest {
	t.Helper()
	ctx := context.Background()
	entries, _, err := s.metadataStore.ListObjects(ctx, path, "", "", 10000)
	require.NoError(t, err)
	out := map[string][]cluster.VersionManifest{}
	for _, e := range entries {
		m, err := cluster.KeyManifest(ctx, s.objectManager, path, e.Key)
		require.NoError(t, err)
		out[e.Key] = m
	}
	return out
}

func readBody(t *testing.T, s *Server, path, key string, versionID ...string) string {
	t.Helper()
	_, reader, err := s.objectManager.GetObject(context.Background(), path, key, versionID...)
	require.NoError(t, err)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(data)
}

func countRows(t *testing.T, s *Server, table, column, bucket string) int {
	t.Helper()
	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE `+column+` = ?`, bucket).Scan(&n))
	return n
}

// A migration moves a bucket whole: every version and delete marker with its
// data, headers, metadata, tags, ACL and lock state, the object written while
// versioning was suspended, a multipart ETag, the bucket's configuration and
// ACL and its rows in the node's database. The source keeps nothing of it and
// routes it to the target.
func TestBucketMigrationMovesEveryVersionAndItsState(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	src, dst := c.source, c.target
	const tenantID, name = "t-mig", "moved"
	path := tenantID + "/" + name
	for _, s := range []*Server{src, dst} {
		require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: tenantID, Name: tenantID, Status: "active"}))
	}
	require.NoError(t, src.bucketManager.CreateBucket(ctx, tenantID, name, "owner-1"))
	b, err := src.metadataStore.GetBucketByName(ctx, name)
	require.NoError(t, err)
	b.Versioning = &metadata.VersioningMetadata{Status: "Enabled"}
	b.ObjectLock = &metadata.ObjectLockMetadata{Enabled: true}
	b.Tags = map[string]string{"team": "storage"}
	b.Quota = &metadata.BucketQuota{MaxSizeBytes: 1 << 30}
	if b.Metadata == nil {
		b.Metadata = map[string]string{}
	}
	b.Metadata["purpose"] = "migration test"
	require.NoError(t, src.metadataStore.UpdateBucket(ctx, b))
	aclMgr := src.bucketManager.GetACLManager().(acl.Manager)
	bucketACL := acl.CreateDefaultACL("owner-1", "Owner")
	bucketACL.Grants = append(bucketACL.Grants, acl.Grant{
		Grantee:    acl.Grantee{Type: acl.GranteeTypeGroup, URI: acl.GroupAllUsers},
		Permission: acl.PermissionRead,
	})
	require.NoError(t, aclMgr.SetBucketACL(ctx, tenantID, name, bucketACL))

	put := func(key, body string, headers http.Header) *object.Object {
		t.Helper()
		if headers == nil {
			headers = http.Header{}
		}
		obj, err := src.objectManager.PutObject(ctx, path, key, strings.NewReader(body), headers)
		require.NoError(t, err)
		return obj
	}
	v1 := put("a", "one", http.Header{"Content-Type": {"text/plain"}, "X-Amz-Meta-Color": {"blue"}})
	require.NoError(t, src.objectManager.SetObjectTagging(ctx, path, "a", &object.TagSet{Tags: []object.Tag{{Key: "v", Value: "1"}}}, v1.VersionID))
	require.NoError(t, src.objectManager.SetObjectACL(ctx, path, "a", &object.ACL{
		Owner:  object.Owner{ID: "owner-1"},
		Grants: []object.Grant{{Grantee: object.Grantee{Type: "Group", URI: acl.GroupAllUsers}, Permission: "READ"}},
	}, v1.VersionID))
	put("a", "two", nil)
	_, err = src.objectManager.DeleteObject(ctx, path, "a", false)
	require.NoError(t, err)
	put("a", "three", nil)
	put("gone", "hidden", nil)
	_, err = src.objectManager.DeleteObject(ctx, path, "gone", false)
	require.NoError(t, err)
	put("locked", "held", http.Header{
		"X-Amz-Object-Lock-Mode":              {"GOVERNANCE"},
		"X-Amz-Object-Lock-Retain-Until-Date": {time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)},
		"X-Amz-Object-Lock-Legal-Hold":        {"ON"},
	})
	upload, err := src.objectManager.CreateMultipartUpload(ctx, path, "mp", http.Header{})
	require.NoError(t, err)
	var parts []object.Part
	for i, body := range []string{"first part ", "second part"} {
		part, err := src.objectManager.UploadPart(ctx, upload.UploadID, i+1, strings.NewReader(body))
		require.NoError(t, err)
		parts = append(parts, *part)
	}
	mp, err := src.objectManager.CompleteMultipartUpload(ctx, upload.UploadID, parts)
	require.NoError(t, err)
	require.Contains(t, mp.ETag, "-")
	put("folder/", "", nil)
	put("empty", "", nil)

	b, err = src.metadataStore.GetBucketByName(ctx, name)
	require.NoError(t, err)
	b.Versioning.Status = "Suspended"
	require.NoError(t, src.metadataStore.UpdateBucket(ctx, b))
	put("a", "four", nil)
	require.NoError(t, src.objectManager.SetObjectTagging(ctx, path, "a", &object.TagSet{Tags: []object.Tag{{Key: "current", Value: "yes"}}}))
	put("plain", "only one", nil)

	now := time.Now().Unix()
	for _, stmt := range []string{
		`INSERT INTO shares (id, bucket_name, object_key, tenant_id, access_key_id, secret_key, share_token, created_at, created_by)
			VALUES ('share-1', 'moved', 'a', 't-mig', 'AK', 'SK', 'token-1', ?, 'owner-1')`,
		`INSERT INTO bucket_inventory_configs (id, bucket_name, tenant_id, enabled, frequency, format, destination_bucket, included_fields, schedule_time, created_at, updated_at)
			VALUES ('inv-1', 'moved', 't-mig', 1, 'daily', 'csv', 'reports', '["Key"]', '02:00', ?, ?)`,
		`INSERT INTO bucket_inventory_reports (id, config_id, bucket_name, report_path, status, created_at)
			VALUES ('rep-1', 'inv-1', 'moved', 'reports/r1.csv', 'completed', ?)`,
		`INSERT INTO replication_rules (id, tenant_id, source_bucket, destination_endpoint, destination_bucket, destination_access_key, destination_secret_key)
			VALUES ('rule-1', 't-mig', 'moved', 'https://elsewhere.invalid', 'copy', 'AK', 'secret-key')`,
		`INSERT INTO replication_queue (rule_id, tenant_id, bucket, object_key, action) VALUES ('rule-1', 't-mig', 'moved', 'a', 'PUT')`,
		`INSERT INTO replication_status (rule_id, tenant_id, source_bucket, source_key, destination_bucket, destination_key, status)
			VALUES ('rule-1', 't-mig', 'moved', 'plain', 'copy', 'plain', 'completed')`,
	} {
		args := make([]any, strings.Count(stmt, "?"))
		for i := range args {
			args[i] = now
		}
		_, err := src.db.ExecContext(ctx, stmt, args...)
		require.NoError(t, err, stmt)
	}

	for _, stmt := range []string{
		`INSERT INTO replication_rules (id, tenant_id, source_bucket, destination_endpoint, destination_bucket, destination_access_key, destination_secret_key)
			VALUES ('rule-other', 't-mig', 'other', 'https://elsewhere.invalid', 'copy', 'AK', 'secret-key')`,
		`INSERT INTO replication_queue (rule_id, tenant_id, bucket, object_key, action) VALUES ('rule-other', 't-mig', 'other', 'x', 'PUT')`,
		`INSERT INTO replication_status (rule_id, tenant_id, source_bucket, source_key, destination_bucket, destination_key, status)
			VALUES ('rule-other', 't-mig', 'other', 'x', 'copy', 'x', 'completed')`,
	} {
		_, err := dst.db.ExecContext(ctx, stmt)
		require.NoError(t, err, stmt)
	}

	before := manifestsOf(t, src, path)
	require.Len(t, before, 7)
	sourceBucket, err := src.metadataStore.GetBucketByName(ctx, name)
	require.NoError(t, err)
	tenantBefore, err := src.authManager.GetTenant(ctx, tenantID)
	require.NoError(t, err)

	job := c.migrate(t, name)
	require.Equal(t, cluster.MigrationStatusCompleted, job.Status, job.ErrorMessage)
	assert.Equal(t, int64(6), job.ObjectsMigrated, "the keys that are visible")

	assert.Equal(t, before, manifestsOf(t, dst, path), "the target holds every version as the source did")
	assert.Equal(t, "four", readBody(t, dst, path, "a"))
	assert.Equal(t, "one", readBody(t, dst, path, "a", v1.VersionID))
	assert.Equal(t, "first part second part", readBody(t, dst, path, "mp"))

	moved, err := dst.metadataStore.GetBucketByName(ctx, name)
	require.NoError(t, err)
	assert.False(t, moved.Moving())
	assert.Equal(t, sourceBucket.Versioning, moved.Versioning)
	assert.Equal(t, sourceBucket.ObjectLock, moved.ObjectLock)
	assert.Equal(t, sourceBucket.Tags, moved.Tags)
	assert.Equal(t, sourceBucket.Quota, moved.Quota)
	assert.Equal(t, sourceBucket.Metadata, moved.Metadata)
	assert.Equal(t, sourceBucket.OwnerID, moved.OwnerID)
	assert.True(t, sourceBucket.CreatedAt.Equal(moved.CreatedAt))
	incremental := moved.TotalSize
	require.NoError(t, dst.metadataStore.RecalculateBucketStats(ctx, tenantID, name))
	recounted, err := dst.metadataStore.GetBucketByName(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, recounted.TotalSize, incremental, "the target counts what it holds")
	movedACL, err := dst.bucketManager.GetACLManager().(acl.Manager).GetBucketACL(ctx, tenantID, name)
	require.NoError(t, err)
	assert.Equal(t, bucketACL, movedACL)

	for _, table := range [][2]string{
		{"shares", "bucket_name"}, {"bucket_inventory_configs", "bucket_name"}, {"bucket_inventory_reports", "bucket_name"},
		{"replication_rules", "source_bucket"}, {"replication_queue", "bucket"}, {"replication_status", "source_bucket"},
	} {
		assert.Equal(t, 1, countRows(t, dst, table[0], table[1], name), "%s moved", table[0])
		assert.Equal(t, 0, countRows(t, src, table[0], table[1], name), "%s left the source", table[0])
	}
	rules, err := dst.replicationManager.GetRulesForBucket(ctx, name)
	require.NoError(t, err)
	require.Len(t, rules, 1, "the target reads the moved rule")
	assert.Equal(t, 1, countRows(t, dst, "replication_queue", "bucket", "other"),
		"a moved row takes a new ID; it does not replace the target's row with the same one")
	assert.Equal(t, 1, countRows(t, dst, "replication_status", "source_bucket", "other"))
	leftACL, err := aclMgr.GetBucketACL(ctx, tenantID, name)
	require.NoError(t, err)
	assert.Equal(t, acl.CreateDefaultACL("maxiofs", "MaxIOFS"), leftACL, "the source keeps no ACL for the bucket")

	_, err = src.metadataStore.GetBucketByName(ctx, name)
	assert.ErrorIs(t, err, metadata.ErrBucketNotFound)
	assert.False(t, src.bucketGate.Frozen(name))
	tenantAfter, err := src.authManager.GetTenant(ctx, tenantID)
	require.NoError(t, err)
	assert.Equal(t, max(tenantBefore.CurrentStorageBytes-sourceBucket.TotalSize, 0), tenantAfter.CurrentStorageBytes,
		"the source no longer counts the bucket's bytes for the tenant")

	node, local, err := src.clusterRouter.RouteRequest(ctx, name)
	require.NoError(t, err)
	require.False(t, local)
	assert.Equal(t, c.targetID, node.ID, "the source sends the bucket's requests to the target")
}

// While a bucket is migrated its writes are refused with 503, through the S3
// API and the console alike, and its reads go on. Other buckets are not held.
func TestMigratingBucketTakesNoWrites(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	src := c.source
	for _, name := range []string{"held", "free"} {
		require.NoError(t, src.bucketManager.CreateBucket(ctx, "", name, "admin"))
	}
	_, err := src.objectManager.PutObject(ctx, "held", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	require.NoError(t, src.bucketGate.Freeze(ctx, "held"))

	w := c.proxiedS3(t, http.MethodPut, "/held/new", strings.NewReader("x"))
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "<Code>ServiceUnavailable</Code>")
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
	w = c.proxiedS3(t, http.MethodDelete, "/held/k", nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	w = c.proxiedS3(t, http.MethodGet, "/held/k", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "data", w.Body.String())
	w = c.proxiedS3(t, http.MethodPut, "/free/k", strings.NewReader("x"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = c.proxiedS3(t, http.MethodPost, "/held/k?select&select-type=2", strings.NewReader("<SelectObjectContentRequest/>"))
	assert.NotEqual(t, http.StatusServiceUnavailable, w.Code, "S3 Select reads")

	token := getAdminToken(t, src)
	console := func(method, target string, body io.Reader) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, body)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		src.consoleRouter.ServeHTTP(w, req)
		return w
	}
	w = console(http.MethodPut, "/api/v1/buckets/held/objects/new", strings.NewReader("x"))
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "BUCKET_MIGRATING")
	w = console(http.MethodPost, "/api/v1/buckets/held/objects/k/download-token", nil)
	assert.NotEqual(t, http.StatusServiceUnavailable, w.Code, "handing out a download changes nothing")
	w = console(http.MethodPut, "/api/v1/buckets/held/objects/k/download-token", strings.NewReader("x"))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, "an object whose key ends like a read route is written all the same")

	src.bucketGate.Thaw("held")
	w = c.proxiedS3(t, http.MethodPut, "/held/new", strings.NewReader("x"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A migration starts copying only once the writes under way have ended, so a
// write the client was told succeeded is part of the copy.
func TestMigrationWaitsForTheWritesUnderWay(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	require.NoError(t, c.source.bucketManager.CreateBucket(ctx, "", "busy", "admin"))

	body, feed := io.Pipe()
	written := make(chan int, 1)
	go func() {
		written <- c.proxiedS3(t, http.MethodPut, "/busy/slow", body).Code
	}()
	require.Eventually(t, func() bool {
		// The write is inside the gate once the bucket cannot be frozen at once.
		probe, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		if err := c.source.bucketGate.Freeze(probe, "busy"); err == nil {
			c.source.bucketGate.Thaw("busy")
			return false
		}
		return true
	}, 5*time.Second, 20*time.Millisecond)

	job, err := c.source.bucketMigrator.Migrate(ctx, "busy", c.targetID)
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	_, err = c.target.metadataStore.GetBucketByName(ctx, "busy")
	require.ErrorIs(t, err, metadata.ErrBucketNotFound, "nothing is copied while a write runs")

	_, err = feed.Write([]byte("written during the migration"))
	require.NoError(t, err)
	require.NoError(t, feed.Close())
	require.Equal(t, http.StatusOK, <-written)
	c.source.bucketMigrator.Wait()

	done, err := c.source.clusterManager.GetMigrationJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, cluster.MigrationStatusCompleted, done.Status, done.ErrorMessage)
	assert.Equal(t, "written during the migration", readBody(t, c.target, "busy", "slow"))
}

// A migration that cannot copy the bucket leaves it where it was, taking
// writes, and removes what it had copied.
func TestFailedMigrationLeavesTheBucketWhereItWas(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	require.NoError(t, c.source.bucketManager.CreateBucket(ctx, "", "stays", "admin"))
	for _, key := range []string{"a", "boom", "c"} {
		_, err := c.source.objectManager.PutObject(ctx, "stays", key, strings.NewReader(key), http.Header{})
		require.NoError(t, err)
	}
	c.failTarget(func(r *http.Request) bool {
		return r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/ha/objects/boom")
	})

	job := c.migrate(t, "stays")
	require.Equal(t, cluster.MigrationStatusFailed, job.Status)
	assert.Contains(t, job.ErrorMessage, "boom")
	_, err := c.target.metadataStore.GetBucketByName(ctx, "stays")
	assert.ErrorIs(t, err, metadata.ErrBucketNotFound, "the copy is removed")
	b, err := c.source.metadataStore.GetBucketByName(ctx, "stays")
	require.NoError(t, err)
	assert.False(t, b.Moving())
	assert.False(t, c.source.bucketGate.Frozen("stays"))
	assert.Equal(t, "boom", readBody(t, c.source, "stays", "boom"))
}

// A bucket whose copy the target describes differently is not handed over:
// here the target claims to hold nothing of an object without a version ID.
func TestMigrationVerifiesTheCopy(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	require.NoError(t, c.source.bucketManager.CreateBucket(ctx, "", "checked", "admin"))
	_, err := c.source.objectManager.PutObject(ctx, "checked", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	lie := func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/migration/manifest") {
			return false
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"k":[]}`))
		return true
	}
	c.intercept.Store(&lie)

	job := c.migrate(t, "checked")
	require.Equal(t, cluster.MigrationStatusFailed, job.Status)
	assert.Contains(t, job.ErrorMessage, "differs")
	_, err = c.source.metadataStore.GetBucketByName(ctx, "checked")
	require.NoError(t, err, "the bucket stays")
}

// Parts of an upload in progress exist only on the node: the bucket is not
// moved until the upload is completed or aborted.
func TestMigrationRefusesABucketWithUploadsInProgress(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	require.NoError(t, c.source.bucketManager.CreateBucket(ctx, "", "uploading", "admin"))
	_, err := c.source.objectManager.CreateMultipartUpload(ctx, "uploading", "big", http.Header{})
	require.NoError(t, err)

	_, err = c.source.bucketMigrator.Migrate(ctx, "uploading", c.targetID)
	require.ErrorIs(t, err, cluster.ErrMigrationConflict)
	_, err = c.target.metadataStore.GetBucketByName(ctx, "uploading")
	assert.ErrorIs(t, err, metadata.ErrBucketNotFound)
	assert.False(t, c.source.bucketGate.Frozen("uploading"))
}

// A restart during the copy undoes the migration; a restart during the
// hand-over finishes it, holding the bucket's writes again until it is done.
func TestMigrationInterruptedByARestart(t *testing.T) {
	t.Run("while copying", func(t *testing.T) {
		c := newMigrationCluster(t)
		ctx := context.Background()
		require.NoError(t, c.source.bucketManager.CreateBucket(ctx, "", "copying", "admin"))
		for _, key := range []string{"a", "b"} {
			_, err := c.source.objectManager.PutObject(ctx, "copying", key, strings.NewReader(key), http.Header{})
			require.NoError(t, err)
		}
		reached, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		hang := func(w http.ResponseWriter, r *http.Request) bool {
			if !strings.HasSuffix(r.URL.Path, "/ha/objects/b") {
				return false
			}
			once.Do(func() { close(reached) })
			<-release
			http.Error(w, "gone", http.StatusServiceUnavailable)
			return true
		}
		c.intercept.Store(&hang)

		job, err := c.source.bucketMigrator.Migrate(ctx, "copying", c.targetID)
		require.NoError(t, err)
		<-reached
		c.intercept.Store(nil)
		c.restartMigrations(t)
		close(release)
		require.Eventually(t, func() bool {
			done, err := c.source.clusterManager.GetMigrationJob(ctx, job.ID)
			return err == nil && done.Status == cluster.MigrationStatusFailed
		}, 10*time.Second, 20*time.Millisecond)
		c.source.bucketMigrator.Wait()

		done, err := c.source.clusterManager.GetMigrationJob(ctx, job.ID)
		require.NoError(t, err)
		assert.Contains(t, done.ErrorMessage, "restart")
		_, err = c.target.metadataStore.GetBucketByName(ctx, "copying")
		assert.ErrorIs(t, err, metadata.ErrBucketNotFound, "the partial copy is removed")
		_, err = c.source.metadataStore.GetBucketByName(ctx, "copying")
		require.NoError(t, err)
	})

	t.Run("while handing over", func(t *testing.T) {
		c := newMigrationCluster(t)
		ctx := context.Background()
		require.NoError(t, c.source.bucketManager.CreateBucket(ctx, "", "handing", "admin"))
		_, err := c.source.objectManager.PutObject(ctx, "handing", "k", strings.NewReader("data"), http.Header{})
		require.NoError(t, err)
		var commits atomic.Int32
		refuse := func(w http.ResponseWriter, r *http.Request) bool {
			if !strings.HasSuffix(r.URL.Path, "/migration/commit") {
				return false
			}
			commits.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			http.Error(w, "down", http.StatusServiceUnavailable)
			return true
		}
		c.intercept.Store(&refuse)

		job, err := c.source.bucketMigrator.Migrate(ctx, "handing", c.targetID)
		require.NoError(t, err)
		require.Eventually(t, func() bool { return commits.Load() >= 2 }, 5*time.Second, 10*time.Millisecond,
			"a hand-over that fails is retried")
		c.stop()
		c.source.bucketMigrator.Wait()
		mid, err := c.source.clusterManager.GetMigrationJob(ctx, job.ID)
		require.NoError(t, err)
		require.Equal(t, cluster.MigrationStatusCommitting, mid.Status)

		holding := make(chan bool, 1)
		source := c.source
		observe := func(w http.ResponseWriter, r *http.Request) bool {
			if strings.HasSuffix(r.URL.Path, "/migration/commit") {
				select {
				case holding <- source.bucketGate.Frozen("handing"):
				default:
				}
			}
			return false
		}
		c.intercept.Store(&observe)
		c.restartMigrations(t)
		c.source.bucketMigrator.Wait()

		require.True(t, <-holding, "the restarted node holds the bucket's writes while it hands it over")
		done, err := c.source.clusterManager.GetMigrationJob(ctx, job.ID)
		require.NoError(t, err)
		require.Equal(t, cluster.MigrationStatusCompleted, done.Status, done.ErrorMessage)
		assert.Equal(t, "data", readBody(t, c.target, "handing", "k"))
		_, err = c.source.metadataStore.GetBucketByName(ctx, "handing")
		assert.ErrorIs(t, err, metadata.ErrBucketNotFound)
		assert.False(t, c.source.bucketGate.Frozen("handing"))
	})
}

// A request another node forwards to the node a bucket left is answered with
// 503 and the header that tells that node its location is stale, not with
// NoSuchBucket.
func TestMovedBucketTellsTheSenderItsLocationIsStale(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	require.NoError(t, c.source.bucketManager.CreateBucket(ctx, "", "left", "admin"))
	_, err := c.source.objectManager.PutObject(ctx, "left", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	require.Equal(t, cluster.MigrationStatusCompleted, c.migrate(t, "left").Status)

	w := c.proxiedS3(t, http.MethodGet, "/left/k", nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Equal(t, "true", w.Header().Get(cluster.BucketNotHereHeader))

	token := getAdminToken(t, c.source)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/buckets/left/objects", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-MaxIOFS-Proxied", "true")
	rec := httptest.NewRecorder()
	c.source.consoleRouter.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	assert.Equal(t, "true", rec.Header().Get(cluster.BucketNotHereHeader))
}

// A step of a migration names its source; the node that sent it must be that
// source.
func TestMigrationStepMustComeFromItsSource(t *testing.T) {
	c := newMigrationCluster(t)
	req := httptest.NewRequest(http.MethodPost, "/api/internal/cluster/migration/abort",
		bytes.NewReader([]byte(`{"job_id":1,"source_node_id":"someone-else","bucket":"x"}`)))
	req = req.WithContext(context.WithValue(req.Context(), "cluster_node_id", c.sourceID))
	w := httptest.NewRecorder()
	c.target.handleMigrationAbort(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// A global administrator starts a migration through the console on the node
// the bucket lives on, whichever node coordinates. Keeping the source copy is
// refused: a bucket lives on one node.
func TestConsoleStartsAMigration(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	require.NoError(t, c.source.bucketManager.CreateBucket(ctx, "", "asked", "admin"))
	_, err := c.source.objectManager.PutObject(ctx, "asked", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	token := getAdminToken(t, c.source)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/buckets/asked/migrate", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c.source.consoleRouter.ServeHTTP(w, req)
		return w
	}

	w := post(`{"target_node_id":"` + c.targetID + `","delete_source":false}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	w = post(`{"target_node_id":"` + c.targetID + `"}`)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	c.source.bucketMigrator.Wait()
	assert.Equal(t, "data", readBody(t, c.target, "asked", "k"))
}

// staleNodeTransport answers every request as a node that no longer holds the
// bucket.
type staleNodeTransport struct{}

func (staleNodeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	header := http.Header{}
	header.Set(cluster.BucketNotHereHeader, "true")
	return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: header,
		Body: io.NopCloser(strings.NewReader(`{"success":false}`)), Request: r}, nil
}

// A console request forwarded to a node that no longer holds the bucket makes
// this node forget where it thought the bucket was; the client is told to
// retry, without the internal header.
func TestConsoleForgetsAStaleLocation(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	require.NoError(t, c.target.bucketManager.CreateBucket(ctx, "", "elsewhere", "admin"))
	token := getAdminToken(t, c.source)
	previous := consoleProxyClient.Transport
	consoleProxyClient.Transport = staleNodeTransport{}
	t.Cleanup(func() { consoleProxyClient.Transport = previous })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/buckets/elsewhere/objects", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	c.source.consoleRouter.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Empty(t, w.Header().Get(cluster.BucketNotHereHeader))
	assert.Equal(t, 0, c.source.clusterRouter.GetCacheStats()["total_entries"], "the stale location is forgotten")
}

// hide marks a bucket as a copy that is not where the bucket lives.
func hide(t *testing.T, s *Server, name, marker string) {
	t.Helper()
	b, err := s.metadataStore.GetBucketByName(context.Background(), name)
	require.NoError(t, err)
	if b.Metadata == nil {
		b.Metadata = map[string]string{}
	}
	b.Metadata[metadata.BucketMovingKey] = marker
	require.NoError(t, s.metadataStore.UpdateBucket(context.Background(), b))
}

// A copy a migration is filling or has left behind answers for nothing: it is
// not listed, the node does not route to it, a peer asking is told it is not
// here, and a request forwarded to it is told its location is stale.
func TestAHiddenCopyIsNotTheBucket(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	src := c.source
	require.NoError(t, src.bucketManager.CreateBucket(ctx, "", "shadow", "admin"))
	hide(t, src, "shadow", "incoming:someone:1")

	buckets, err := src.bucketManager.ListBuckets(ctx, "")
	require.NoError(t, err)
	for _, b := range buckets {
		assert.NotEqual(t, "shadow", b.Name, "not listed")
	}
	_, local, err := src.clusterRouter.RouteRequest(ctx, "shadow")
	assert.False(t, local, "not served here")
	assert.Error(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/internal/cluster/bucket-exists/shadow", nil)
	req = mux.SetURLVars(req, map[string]string{"name": "shadow"})
	w := httptest.NewRecorder()
	src.handleBucketExists(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code, "a peer is told it is not here")

	w = c.proxiedS3(t, http.MethodGet, "/shadow/k", nil)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "true", w.Header().Get(cluster.BucketNotHereHeader))
	token := getAdminToken(t, src)
	creq := httptest.NewRequest(http.MethodGet, "/api/v1/buckets/shadow/objects", nil)
	creq.Header.Set("Authorization", "Bearer "+token)
	creq.Header.Set("X-MaxIOFS-Proxied", "true")
	cw := httptest.NewRecorder()
	src.consoleRouter.ServeHTTP(cw, creq)
	assert.Equal(t, http.StatusServiceUnavailable, cw.Code, cw.Body.String())
	assert.Equal(t, "true", cw.Header().Get(cluster.BucketNotHereHeader))
}

// A migration never writes into, nor removes, a bucket that lives on the
// target: the stage is refused and the copy it did not make is not removed.
func TestMigrationLeavesALiveBucketOnTheTargetAlone(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	for _, s := range []*Server{c.source, c.target} {
		require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "clash", "admin"))
	}
	_, err := c.source.objectManager.PutObject(ctx, "clash", "from-source", strings.NewReader("s"), http.Header{})
	require.NoError(t, err)
	_, err = c.target.objectManager.PutObject(ctx, "clash", "on-target", strings.NewReader("t"), http.Header{})
	require.NoError(t, err)

	job := c.migrate(t, "clash")
	require.Equal(t, cluster.MigrationStatusFailed, job.Status)
	assert.Contains(t, job.ErrorMessage, "lives on this node")
	live, err := c.target.metadataStore.GetBucketByName(ctx, "clash")
	require.NoError(t, err, "the target's bucket stays")
	assert.False(t, live.Moving())
	assert.Equal(t, "t", readBody(t, c.target, "clash", "on-target"))
	_, err = c.target.objectManager.GetObjectMetadata(ctx, "clash", "from-source")
	assert.ErrorIs(t, err, object.ErrObjectNotFound, "nothing was copied into it")

	req := httptest.NewRequest(http.MethodPost, "/api/internal/cluster/migration/abort",
		strings.NewReader(`{"job_id":`+strconv.FormatInt(job.ID, 10)+`,"source_node_id":"`+c.sourceID+`","bucket":"clash"}`))
	req = req.WithContext(context.WithValue(req.Context(), "cluster_node_id", c.sourceID))
	w := httptest.NewRecorder()
	c.target.handleMigrationAbort(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
	_, err = c.target.metadataStore.GetBucketByName(ctx, "clash")
	require.NoError(t, err, "an abort never removes a bucket that lives there")
}

// A hand-over resumed after the source copy was already hidden finishes the
// move without releasing the bucket's bytes from the tenant a second time.
func TestResumedHandOverReleasesTheTenantBytesOnce(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	const tenantID, name = "t-once", "once"
	for _, s := range []*Server{c.source, c.target} {
		require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: tenantID, Name: tenantID, Status: "active"}))
	}
	require.NoError(t, c.source.bucketManager.CreateBucket(ctx, tenantID, name, "admin"))
	_, err := c.source.objectManager.PutObject(ctx, tenantID+"/"+name, "k", strings.NewReader("some bytes"), http.Header{})
	require.NoError(t, err)
	var commits atomic.Int32
	refuse := func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/migration/commit") {
			return false
		}
		commits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		http.Error(w, "down", http.StatusServiceUnavailable)
		return true
	}
	c.intercept.Store(&refuse)
	job, err := c.source.bucketMigrator.Migrate(ctx, name, c.targetID)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return commits.Load() >= 1 }, 5*time.Second, 10*time.Millisecond)
	c.stop()
	c.source.bucketMigrator.Wait()

	hide(t, c.source, name, "outgoing:"+c.targetID+":"+strconv.FormatInt(job.ID, 10))
	tenant, err := c.source.authManager.GetTenant(ctx, tenantID)
	require.NoError(t, err)
	c.intercept.Store(nil)
	c.restartMigrations(t)
	c.source.bucketMigrator.Wait()

	done, err := c.source.clusterManager.GetMigrationJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, cluster.MigrationStatusCompleted, done.Status, done.ErrorMessage)
	after, err := c.source.authManager.GetTenant(ctx, tenantID)
	require.NoError(t, err)
	assert.Equal(t, tenant.CurrentStorageBytes, after.CurrentStorageBytes)
	assert.Equal(t, "some bytes", readBody(t, c.target, tenantID+"/"+name, "k"))
	_, err = c.source.metadataStore.GetBucketByName(ctx, name)
	assert.ErrorIs(t, err, metadata.ErrBucketNotFound)
}
