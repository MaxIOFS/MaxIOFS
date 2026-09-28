package object

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

func readObject(t *testing.T, m *objectManager, bucket, key string, versionID ...string) string {
	t.Helper()
	_, reader, err := m.GetObject(context.Background(), bucket, key, versionID...)
	require.NoError(t, err)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(data)
}

// A bucket keeps the versions written before its versioning was suspended. A
// copy of one of them is stored as that version, not over the object written
// since, and a copy of a delete marker stays a delete marker.
func TestReplicaCopyIntoASuspendedBucketKeepsItsVersion(t *testing.T) {
	m, s := setupAccountingManager(t)
	ctx := context.Background()
	const b = "suspended-copy"
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: b, OwnerID: "u", Versioning: &metadata.VersioningMetadata{Status: "Suspended"},
	}))
	_, err := m.PutObject(ctx, b, "k", strings.NewReader("current"), http.Header{})
	require.NoError(t, err)

	earlier := time.Now().Add(-time.Hour).Truncate(time.Second)
	copyCtx := WithReplicaCopy(replicaCtx("1700000001000000000.aaaaaaaa", earlier))
	_, err = m.PutObject(copyCtx, b, "k", strings.NewReader("older version"), http.Header{})
	require.NoError(t, err)

	require.Equal(t, "current", readObject(t, m, b, "k"), "the object written since stays the current one")
	require.Equal(t, "older version", readObject(t, m, b, "k", "1700000001000000000.aaaaaaaa"))

	_, err = m.DeleteObject(replicaCtx("1700000002000000000.bbbbbbbb", earlier.Add(time.Second)), b, "k", false)
	require.NoError(t, err)
	require.Equal(t, "current", readObject(t, m, b, "k"), "a copied delete marker deletes nothing")
	versions, err := m.GetObjectVersions(ctx, b, "k")
	require.NoError(t, err)
	var marker bool
	for _, v := range versions {
		marker = marker || (v.VersionID == "1700000002000000000.bbbbbbbb" && v.IsDeleteMarker)
	}
	require.True(t, marker, "the delete marker is kept as one")
	assertAccountingAgrees(t, m, s, b)
}

// A copy carries what its headers cannot: a multipart ETag, tags, ACL and
// restore state. Bytes that do not hash to the ETag they claim are refused.
func TestReplicaCopyCarriesItsAttributes(t *testing.T) {
	m, s := setupAccountingManager(t)
	ctx := context.Background()
	const b = "attributes-copy"
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: b, OwnerID: "u", Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
	}))
	expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	attrs := ReplicatedAttributes{
		ETag:             "0123456789abcdef0123456789abcdef-3",
		Tags:             &TagSet{Tags: []Tag{{Key: "team", Value: "storage"}}},
		ACL:              &ACL{Owner: Owner{ID: "u"}, Grants: []Grant{{Grantee: Grantee{Type: "Group", URI: "http://acs.amazonaws.com/groups/global/AllUsers"}, Permission: "READ"}}},
		RestoreStatus:    "restored",
		RestoreExpiresAt: &expires,
	}
	copyCtx := WithReplicatedAttributes(WithReplicaCopy(replicaCtx("1700000001000000000.aaaaaaaa", time.Now().Add(-time.Minute))), attrs)
	_, err := m.PutObject(copyCtx, b, "k", strings.NewReader("assembled parts"), http.Header{})
	require.NoError(t, err)

	stored, err := m.GetObjectMetadata(ctx, b, "k")
	require.NoError(t, err)
	require.Equal(t, attrs.ETag, stored.ETag)
	require.Equal(t, attrs.Tags.Tags, stored.Tags.Tags)
	require.Equal(t, attrs.ACL, stored.ACL)
	require.Equal(t, "restored", stored.RestoreStatus)
	require.NotNil(t, stored.RestoreExpiresAt)
	require.True(t, stored.RestoreExpiresAt.Equal(expires))

	wrong := WithReplicatedAttributes(WithReplicaCopy(replicaCtx("1700000002000000000.bbbbbbbb", time.Now())),
		ReplicatedAttributes{ETag: "0123456789abcdef0123456789abcdef"})
	_, err = m.PutObject(wrong, b, "k", strings.NewReader("not those bytes"), http.Header{})
	require.ErrorIs(t, err, ErrReplicaDigestMismatch)
	_, err = s.GetObject(ctx, b, "k", "1700000002000000000.bbbbbbbb")
	require.Error(t, err, "a refused copy stores nothing")
	assertAccountingAgrees(t, m, s, b)
}

// Changing an object's tags or ACL rewrites its metadata; the restore state of
// a restored object is part of it and stays.
func TestChangingAnObjectKeepsItsRestoreStatus(t *testing.T) {
	m, s := setupAccountingManager(t)
	ctx := context.Background()
	const b = "restore-kept"
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: b, OwnerID: "u"}))
	_, err := m.PutObject(ctx, b, "k", strings.NewReader("archived"), http.Header{})
	require.NoError(t, err)
	expires := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, m.SetRestoreStatus(ctx, b, "k", "restored", &expires))

	require.NoError(t, m.SetObjectTagging(ctx, b, "k", &TagSet{Tags: []Tag{{Key: "a", Value: "b"}}}))
	require.NoError(t, m.SetObjectACL(ctx, b, "k", &ACL{Owner: Owner{ID: "u"}}))

	obj, err := m.GetObjectMetadata(ctx, b, "k")
	require.NoError(t, err)
	require.Equal(t, "restored", obj.RestoreStatus)
	require.NotNil(t, obj.RestoreExpiresAt)
	require.True(t, obj.RestoreExpiresAt.Equal(expires))
}

// A write without a version ID over a version kept from before versioning was
// suspended adds its bytes: the version stays. Over a delete marker it makes
// the key visible again.
func TestSuspendedWriteKeepsTheVersionsBytes(t *testing.T) {
	m, s := setupAccountingManager(t)
	ctx := context.Background()
	const b = "suspended-accounting"
	bucket := &metadata.BucketMetadata{Name: b, OwnerID: "u", Versioning: &metadata.VersioningMetadata{Status: "Enabled"}}
	require.NoError(t, s.CreateBucket(ctx, bucket))
	_, err := m.PutObject(ctx, b, "kept", strings.NewReader("a version"), http.Header{})
	require.NoError(t, err)
	_, err = m.PutObject(ctx, b, "hidden", strings.NewReader("behind a marker"), http.Header{})
	require.NoError(t, err)
	_, err = m.DeleteObject(ctx, b, "hidden", false)
	require.NoError(t, err)
	bucket.Versioning.Status = "Suspended"
	require.NoError(t, s.UpdateBucket(ctx, bucket))

	for _, key := range []string{"kept", "hidden", "kept"} {
		_, err = m.PutObject(ctx, b, key, strings.NewReader("written while suspended"), http.Header{})
		require.NoError(t, err)
		assertAccountingAgrees(t, m, s, b)
	}
}

// A copy of a version written in the same second as the current object without
// a version ID does not replace it: the order within a second is unknown, and
// the object a client sees stays the one it saw.
func TestReplicaCopyOfTheSameSecondKeepsTheCurrentObject(t *testing.T) {
	m, s := setupAccountingManager(t)
	ctx := context.Background()
	const b = "same-second"
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: b, OwnerID: "u", Versioning: &metadata.VersioningMetadata{Status: "Suspended"},
	}))
	current, err := m.PutObject(ctx, b, "k", strings.NewReader("current"), http.Header{})
	require.NoError(t, err)

	copyCtx := WithReplicaCopy(replicaCtx("1700000001000000000.aaaaaaaa", current.LastModified))
	_, err = m.PutObject(copyCtx, b, "k", strings.NewReader("copied"), http.Header{})
	require.NoError(t, err)
	require.Equal(t, "current", readObject(t, m, b, "k"))
	assertAccountingAgrees(t, m, s, b)
}
