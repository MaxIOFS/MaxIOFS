package object

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

func publicRead() *ACL {
	return &ACL{Owner: Owner{ID: "u"}, Grants: []Grant{{Grantee: Grantee{Type: "Group", URI: acl.GroupAllUsers}, Permission: "READ"}}}
}

func isPublic(t *testing.T, m *objectManager, bucket, key string, versionID ...string) bool {
	t.Helper()
	got, err := m.GetObjectACL(context.Background(), bucket, key, versionID...)
	require.NoError(t, err)
	for _, g := range got.Grants {
		if g.Grantee.URI == acl.GroupAllUsers {
			return true
		}
	}
	return false
}

// An object's ACL belongs to that object. The next object written at the key,
// by overwrite, as a new version or after a delete, starts private.
func TestAnObjectDoesNotInheritTheACLOfTheOneBefore(t *testing.T) {
	m, s := setupAccountingManager(t)
	ctx := context.Background()
	put := func(bucket, key string) *Object {
		t.Helper()
		obj, err := m.PutObject(ctx, bucket, key, strings.NewReader("data"), http.Header{})
		require.NoError(t, err)
		return obj
	}

	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "plain", OwnerID: "u"}))
	put("plain", "k")
	require.NoError(t, m.SetObjectACL(ctx, "plain", "k", publicRead()))
	require.True(t, isPublic(t, m, "plain", "k"))
	put("plain", "k")
	require.False(t, isPublic(t, m, "plain", "k"), "an overwrite starts private")
	require.NoError(t, m.SetObjectACL(ctx, "plain", "k", publicRead()))
	_, err := m.DeleteObject(ctx, "plain", "k", false)
	require.NoError(t, err)
	put("plain", "k")
	require.False(t, isPublic(t, m, "plain", "k"), "an object created after a delete starts private")

	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "versioned", OwnerID: "u",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"}}))
	v1 := put("versioned", "k")
	require.NoError(t, m.SetObjectACL(ctx, "versioned", "k", publicRead()))
	put("versioned", "k")
	require.False(t, isPublic(t, m, "versioned", "k"), "a new version starts private")
	require.True(t, isPublic(t, m, "versioned", "k", v1.VersionID), "the version the ACL was set on keeps it")
}

// The ACLs once kept by key are moved into the object each key holds when it
// has none of its own, so every object answers with the ACL it answered with
// before, and are removed. Moving them twice changes nothing.
func TestKeyACLsAreAdoptedByTheirObjects(t *testing.T) {
	m, s := setupAccountingManager(t)
	ctx := context.Background()
	raw := s.(metadata.RawKVStore)
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "g", OwnerID: "u"}))
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "tb", TenantID: "t1", OwnerID: "u"}))
	for _, o := range []struct{ bucket, key string }{{"g", "a:b"}, {"t1/tb", "x"}, {"g", "own"}} {
		_, err := m.PutObject(ctx, o.bucket, o.key, strings.NewReader("data"), http.Header{})
		require.NoError(t, err)
	}
	require.NoError(t, m.SetObjectACL(ctx, "g", "own", &ACL{Owner: Owner{ID: "u"}}))

	public, err := json.Marshal(acl.ACL{Owner: acl.Owner{ID: "u"}, Grants: []acl.Grant{{
		Grantee: acl.Grantee{Type: acl.GranteeTypeGroup, URI: acl.GroupAllUsers}, Permission: acl.PermissionRead}}})
	require.NoError(t, err)
	for _, k := range []string{"g:a:b", "t1:tb:x", "g:own", "g:gone"} {
		require.NoError(t, raw.PutRaw(ctx, acl.ObjectACLPrefix+k, public))
	}

	adopted, err := AdoptKeyACLs(ctx, s)
	require.NoError(t, err)
	require.Equal(t, 2, adopted)
	require.True(t, isPublic(t, m, "g", "a:b"), "a key holding ':' is found")
	require.True(t, isPublic(t, m, "t1/tb", "x"))
	require.False(t, isPublic(t, m, "g", "own"), "an object's own ACL wins")
	var left int
	require.NoError(t, raw.RawScan(ctx, acl.ObjectACLPrefix, "", func(string, []byte) bool { left++; return true }))
	require.Zero(t, left)

	adopted, err = AdoptKeyACLs(ctx, s)
	require.NoError(t, err)
	require.Zero(t, adopted)
	require.True(t, isPublic(t, m, "g", "a:b"))
}
