package object

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

// setupVersionedBucket returns a manager over a bucket with versioning enabled.
func setupVersionedBucket(t *testing.T, name string) (*objectManager, metadata.Store, func()) {
	t.Helper()
	om, store, cleanup := setupTestManagerWithStore(t)
	require.NoError(t, store.CreateBucket(context.Background(), &metadata.BucketMetadata{
		Name:       name,
		OwnerID:    "u",
		Versioning: &metadata.VersioningMetadata{Enabled: true, Status: "Enabled"},
	}))
	return om, store, cleanup
}

func put(t *testing.T, om *objectManager, bucket, key, body string) string {
	t.Helper()
	obj, err := om.PutObject(context.Background(), bucket, key, bytes.NewReader([]byte(body)), http.Header{})
	require.NoError(t, err)
	return obj.VersionID
}

func getVersion(t *testing.T, om *objectManager, bucket, key, versionID string) string {
	t.Helper()
	_, rc, err := om.GetObject(context.Background(), bucket, key, versionID)
	require.NoError(t, err)
	defer rc.Close() //nolint:errcheck
	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	return string(body)
}

func listedKeys(t *testing.T, om *objectManager, bucket, prefix, delimiter string) ([]string, []string) {
	t.Helper()
	res, err := om.ListObjects(context.Background(), bucket, prefix, delimiter, "", 1000)
	require.NoError(t, err)

	keys := make([]string, 0, len(res.Objects))
	for _, o := range res.Objects {
		keys = append(keys, o.Key)
	}
	prefixes := make([]string, 0, len(res.CommonPrefixes))
	for _, p := range res.CommonPrefixes {
		prefixes = append(prefixes, p.Prefix)
	}
	return keys, prefixes
}

// A key and a prefix of the same name are two keys, and versioning keeps every
// version of both apart.
func TestVersionedBucket_KeyThatIsAlsoAPrefix(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "vb-prefix")
	defer cleanup()
	const bucket = "vb-prefix"

	firstV1 := put(t, om, bucket, "2026", "the object, v1")
	firstV2 := put(t, om, bucket, "2026", "the object, v2")
	markerV1 := put(t, om, bucket, "2026/", "")
	nestedV1 := put(t, om, bucket, "2026/enero/informe.pdf", "under the prefix")

	require.NotEqual(t, firstV1, firstV2)

	require.Equal(t, "the object, v1", getVersion(t, om, bucket, "2026", firstV1))
	require.Equal(t, "the object, v2", getVersion(t, om, bucket, "2026", firstV2))
	require.Equal(t, "", getVersion(t, om, bucket, "2026/", markerV1))
	require.Equal(t, "under the prefix", getVersion(t, om, bucket, "2026/enero/informe.pdf", nestedV1))

	versions, err := om.GetObjectVersions(context.Background(), bucket, "2026")
	require.NoError(t, err)
	require.Len(t, versions, 2, "the versions of the object must not include the prefix's")
}

// Keys differing only in case are distinct in a versioned bucket too.
func TestVersionedBucket_KeysDifferingOnlyInCase(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "vb-case")
	defer cleanup()
	const bucket = "vb-case"

	upper := put(t, om, bucket, "docs/Report.pdf", "UPPERCASE")
	lower := put(t, om, bucket, "docs/report.pdf", "lowercase")

	require.Equal(t, "UPPERCASE", getVersion(t, om, bucket, "docs/Report.pdf", upper))
	require.Equal(t, "lowercase", getVersion(t, om, bucket, "docs/report.pdf", lower))

	keys, _ := listedKeys(t, om, bucket, "docs/", "")
	require.ElementsMatch(t, []string{"docs/Report.pdf", "docs/report.pdf"}, keys)
}

// Listing derives the hierarchy from the keys; no folder object is invented.
func TestVersionedBucket_DelimiterDerivesPrefixes(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "vb-delim")
	defer cleanup()
	const bucket = "vb-delim"

	put(t, om, bucket, "2026/enero/a.txt", "a")
	put(t, om, bucket, "2026/enero/b.txt", "b")
	put(t, om, bucket, "2026/febrero/c.txt", "c")
	put(t, om, bucket, "root.txt", "r")

	keys, prefixes := listedKeys(t, om, bucket, "", "/")
	require.Equal(t, []string{"root.txt"}, keys)
	require.Equal(t, []string{"2026/"}, prefixes)

	keys, prefixes = listedKeys(t, om, bucket, "2026/", "/")
	require.Empty(t, keys)
	require.ElementsMatch(t, []string{"2026/enero/", "2026/febrero/"}, prefixes)

	keys, prefixes = listedKeys(t, om, bucket, "2026/enero/", "/")
	require.ElementsMatch(t, []string{"2026/enero/a.txt", "2026/enero/b.txt"}, keys)
	require.Empty(t, prefixes)
}

// An explicit folder marker is an object of its own: it is listed, and deleting
// it leaves everything under the prefix alone.
func TestVersionedBucket_FolderMarkerIsJustAnObject(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "vb-marker")
	defer cleanup()
	ctx := context.Background()
	const bucket = "vb-marker"

	put(t, om, bucket, "photos/", "")
	nested := put(t, om, bucket, "photos/cat.jpg", "the bytes")

	keys, _ := listedKeys(t, om, bucket, "photos/", "")
	require.ElementsMatch(t, []string{"photos/", "photos/cat.jpg"}, keys)

	_, err := om.DeleteObject(ctx, bucket, "photos/", false)
	require.NoError(t, err)

	require.Equal(t, "the bytes", getVersion(t, om, bucket, "photos/cat.jpg", nested))
}

// Deleting without a version ID leaves a delete marker; the versions stay
// readable and the neighbours under the same prefix are untouched.
func TestVersionedBucket_DeleteMarkerUnderAPrefix(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "vb-delmark")
	defer cleanup()
	ctx := context.Background()
	const bucket = "vb-delmark"

	v1 := put(t, om, bucket, "logs/2026/app.log", "first")
	v2 := put(t, om, bucket, "logs/2026/app.log", "second")
	neighbour := put(t, om, bucket, "logs/2026/other.log", "neighbour")

	markerID, err := om.DeleteObject(ctx, bucket, "logs/2026/app.log", false)
	require.NoError(t, err)
	require.NotEmpty(t, markerID, "a versioned delete leaves a marker")

	_, _, err = om.GetObject(ctx, bucket, "logs/2026/app.log")
	require.Error(t, err, "the current version is a delete marker")

	require.Equal(t, "first", getVersion(t, om, bucket, "logs/2026/app.log", v1))
	require.Equal(t, "second", getVersion(t, om, bucket, "logs/2026/app.log", v2))
	require.Equal(t, "neighbour", getVersion(t, om, bucket, "logs/2026/other.log", neighbour))
}

// Every version of a key under a prefix is reachable, and the versions of one
// key never leak into another that shares its prefix.
func TestVersionedBucket_VersionsDoNotLeakAcrossKeys(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "vb-leak")
	defer cleanup()
	ctx := context.Background()
	const bucket = "vb-leak"

	for i := 0; i < 3; i++ {
		put(t, om, bucket, "shared/prefix/a.bin", "a")
		put(t, om, bucket, "shared/prefix/b.bin", "b")
	}

	for _, key := range []string{"shared/prefix/a.bin", "shared/prefix/b.bin"} {
		versions, err := om.GetObjectVersions(ctx, bucket, key)
		require.NoError(t, err)
		require.Len(t, versions, 3, "key %s", key)
		for _, v := range versions {
			require.Equal(t, key, v.Key)
		}
	}
}

// A key whose name ends in the sidecar suffix is an ordinary key now that the
// file name is a digest.
func TestVersionedBucket_KeyNamedLikeASidecar(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "vb-sidecar")
	defer cleanup()
	const bucket = "vb-sidecar"

	real := put(t, om, bucket, "docs/notes.txt", "real")
	decoy := put(t, om, bucket, "docs/notes.txt.metadata", "decoy")

	require.Equal(t, "real", getVersion(t, om, bucket, "docs/notes.txt", real))
	require.Equal(t, "decoy", getVersion(t, om, bucket, "docs/notes.txt.metadata", decoy))
}

// Uploading a nested key creates that key and nothing else: AWS invents no
// object for the prefixes above it.
func TestVersionedBucket_NoObjectIsInventedForParentPrefixes(t *testing.T) {
	om, store, cleanup := setupVersionedBucket(t, "vb-implicit")
	defer cleanup()
	ctx := context.Background()
	const bucket = "vb-implicit"

	put(t, om, bucket, "logs/2026/app.log", "entry")

	for _, prefix := range []string{"logs/", "logs/2026/"} {
		_, err := store.GetObject(ctx, bucket, prefix)
		require.Error(t, err, "%s should not exist as an object", prefix)
	}

	versions, err := store.ListAllObjectVersions(ctx, bucket, "", 0)
	require.NoError(t, err)
	keys := make([]string, 0, len(versions))
	for _, v := range versions {
		keys = append(keys, v.Key)
	}
	require.Equal(t, []string{"logs/2026/app.log"}, keys,
		"the version listing must show only what the client uploaded")
}
