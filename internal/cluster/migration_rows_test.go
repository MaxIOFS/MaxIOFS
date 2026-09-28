package cluster

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bucketTablesDB is a database with the bucket tables, their columns of each
// kind a node stores.
func bucketTablesDB(t *testing.T) *sql.DB {
	t.Helper()
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	for _, ddl := range []string{
		`CREATE TABLE bucket_inventory_configs (id TEXT PRIMARY KEY, bucket_name TEXT NOT NULL, tenant_id TEXT, enabled BOOLEAN DEFAULT 1, created_at INTEGER)`,
		`CREATE TABLE bucket_inventory_reports (id TEXT PRIMARY KEY, config_id TEXT NOT NULL, bucket_name TEXT NOT NULL, error_message TEXT)`,
		`CREATE TABLE replication_rules (id TEXT PRIMARY KEY, source_bucket TEXT NOT NULL, created_at TIMESTAMP NOT NULL, secret BLOB)`,
		`CREATE TABLE replication_queue (id INTEGER PRIMARY KEY AUTOINCREMENT, rule_id TEXT NOT NULL, bucket TEXT NOT NULL, processed_at TIMESTAMP)`,
		`CREATE TABLE replication_status (id INTEGER PRIMARY KEY AUTOINCREMENT, rule_id TEXT NOT NULL, source_bucket TEXT NOT NULL, ratio REAL)`,
		`CREATE TABLE shares (id TEXT PRIMARY KEY, bucket_name TEXT NOT NULL, expires_at INTEGER)`,
	} {
		_, err := db.Exec(ddl)
		require.NoError(t, err)
	}
	return db
}

// A bucket's rows move to another node as they are, typed, and applying them
// twice stores them once. Rows whose IDs the database assigns take new ones,
// so they never replace the other node's own rows.
func TestBucketRowsMoveAsTheyAre(t *testing.T) {
	ctx := context.Background()
	src, dst := bucketTablesDB(t), bucketTablesDB(t)
	created := time.Date(2026, 9, 1, 10, 30, 0, 0, time.UTC)
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO bucket_inventory_configs VALUES ('inv', 'b', NULL, 1, 1700000000)`, nil},
		{`INSERT INTO bucket_inventory_reports VALUES ('rep', 'inv', 'b', 'it failed')`, nil},
		{`INSERT INTO replication_rules VALUES ('rule', 'b', ?, ?)`, []any{created, []byte{0, 1, 2}}},
		{`INSERT INTO replication_queue (rule_id, bucket, processed_at) VALUES ('rule', 'b', NULL)`, nil},
		{`INSERT INTO replication_status (rule_id, source_bucket, ratio) VALUES ('rule', 'b', 0.5)`, nil},
		{`INSERT INTO shares VALUES ('share', 'b', 1800000000)`, nil},
		{`INSERT INTO shares VALUES ('elsewhere', 'other', 1)`, nil},
	} {
		_, err := src.Exec(stmt.query, stmt.args...)
		require.NoError(t, err, stmt.query)
	}
	for _, stmt := range []string{
		`INSERT INTO replication_rules VALUES ('rule-other', 'other', '2026-01-01 00:00:00', NULL)`,
		`INSERT INTO replication_queue (rule_id, bucket) VALUES ('rule-other', 'other')`,
		`INSERT INTO replication_status (rule_id, source_bucket) VALUES ('rule-other', 'other')`,
	} {
		_, err := dst.Exec(stmt)
		require.NoError(t, err, stmt)
	}

	rows, err := readBucketRows(ctx, src, "b")
	require.NoError(t, err)
	for _, table := range bucketTables {
		require.Len(t, rows[table.name], 1, table.name)
	}
	require.NoError(t, applyBucketRows(ctx, dst, "b", rows))
	require.NoError(t, applyBucketRows(ctx, dst, "b", rows))

	moved, err := readBucketRows(ctx, dst, "b")
	require.NoError(t, err)
	assert.Equal(t, rows, moved)
	for _, check := range [][2]string{{"replication_queue", "bucket"}, {"replication_status", "source_bucket"}} {
		var n int
		require.NoError(t, dst.QueryRow(`SELECT COUNT(*) FROM `+check[0]+` WHERE `+check[1]+` = 'other'`).Scan(&n))
		assert.Equal(t, 1, n, "%s keeps the target's own row", check[0])
	}
	var when time.Time
	require.NoError(t, dst.QueryRow(`SELECT created_at FROM replication_rules WHERE id = 'rule'`).Scan(&when))
	assert.True(t, when.Equal(created))

	require.NoError(t, deleteBucketRows(ctx, src, "b"))
	left, err := readBucketRows(ctx, src, "b")
	require.NoError(t, err)
	assert.Empty(t, left)
	var others int
	require.NoError(t, src.QueryRow(`SELECT COUNT(*) FROM shares WHERE bucket_name = 'other'`).Scan(&others))
	assert.Equal(t, 1, others, "only the bucket's rows go")
}

// Rows that name another bucket, a table that is not a bucket table or a
// column this node does not have are refused, and nothing is stored.
func TestBucketRowsRefusedWhole(t *testing.T) {
	ctx := context.Background()
	db := bucketTablesDB(t)
	share := func(bucket string, extra map[string]RowValue) BucketRows {
		row := map[string]RowValue{"id": {Type: "text", Text: "s"}, "bucket_name": {Type: "text", Text: bucket}}
		for k, v := range extra {
			row[k] = v
		}
		return BucketRows{"shares": {row}}
	}
	assert.Error(t, applyBucketRows(ctx, db, "b", share("other", nil)))
	assert.ErrorContains(t, applyBucketRows(ctx, db, "b", share("b", map[string]RowValue{"missing": {Type: "int"}})),
		"does not exist on this node", "refused before any statement is built from the name")
	assert.Error(t, applyBucketRows(ctx, db, "b", BucketRows{"users": {}}))
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM shares`).Scan(&n))
	assert.Zero(t, n)
}

// The digest a migration compares covers what a client can read of a version,
// and nothing else.
func TestVersionDigestCoversWhatAClientReads(t *testing.T) {
	until := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	expires := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	base := func() *object.Object {
		return &object.Object{
			Key: "k", ETag: "e", Size: 3, LastModified: time.Unix(1700000000, 0),
			ContentType: "text/plain", ContentDisposition: "inline", ContentEncoding: "gzip",
			CacheControl: "no-cache", ContentLanguage: "en", StorageClass: "STANDARD",
			ChecksumAlgorithm: "SHA256", ChecksumValue: "c",
			Metadata:          map[string]string{"m": "1"},
			Tags:              &object.TagSet{Tags: []object.Tag{{Key: "t", Value: "1"}, {Key: "u", Value: "2"}}},
			ACL:               &object.ACL{Owner: object.Owner{ID: "o"}},
			Retention:         &object.RetentionConfig{Mode: object.RetentionModeGovernance, RetainUntilDate: until},
			LegalHold:         &object.LegalHoldConfig{Status: object.LegalHoldStatusOn},
			RestoreStatus:     "restored",
			RestoreExpiresAt:  &expires,
		}
	}
	reference := versionDigest(base(), false)

	same := base()
	same.Tags.Tags[0], same.Tags.Tags[1] = same.Tags.Tags[1], same.Tags.Tags[0]
	same.LastModified = same.LastModified.Add(300 * time.Millisecond)
	same.SSEAlgorithm = "AES256"
	assert.Equal(t, reference, versionDigest(same, false), "tag order, sub-second time and encryption at rest are not what a client reads")

	for name, change := range map[string]func(*object.Object){
		"etag":           func(o *object.Object) { o.ETag = "f" },
		"size":           func(o *object.Object) { o.Size++ },
		"modified":       func(o *object.Object) { o.LastModified = o.LastModified.Add(time.Second) },
		"content type":   func(o *object.Object) { o.ContentType = "text/html" },
		"disposition":    func(o *object.Object) { o.ContentDisposition = "attachment" },
		"encoding":       func(o *object.Object) { o.ContentEncoding = "br" },
		"cache control":  func(o *object.Object) { o.CacheControl = "max-age=1" },
		"language":       func(o *object.Object) { o.ContentLanguage = "es" },
		"storage class":  func(o *object.Object) { o.StorageClass = "GLACIER" },
		"checksum":       func(o *object.Object) { o.ChecksumValue = "d" },
		"metadata":       func(o *object.Object) { o.Metadata["m"] = "2" },
		"tags":           func(o *object.Object) { o.Tags.Tags[0].Value = "9" },
		"acl":            func(o *object.Object) { o.ACL.Owner.ID = "p" },
		"retention date": func(o *object.Object) { o.Retention.RetainUntilDate = until.Add(time.Nanosecond) },
		"retention mode": func(o *object.Object) { o.Retention.Mode = object.RetentionModeCompliance },
		"legal hold":     func(o *object.Object) { o.LegalHold.Status = object.LegalHoldStatusOff },
		"restore status": func(o *object.Object) { o.RestoreStatus = "ongoing" },
		"restore expiry": func(o *object.Object) { later := expires.Add(time.Hour); o.RestoreExpiresAt = &later },
	} {
		o := base()
		change(o)
		assert.NotEqual(t, reference, versionDigest(o, false), name)
	}
	assert.NotEqual(t, reference, versionDigest(base(), true), "delete marker")
}
