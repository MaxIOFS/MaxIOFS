package metadata

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listedVersion is what a walk over the pages saw of one version.
type listedVersion struct {
	key, version string
	latest       bool
}

// walkVersions reads every page of the versions under prefix, as a client does
// with the markers each page gives, and the common prefixes it found.
func walkVersions(t *testing.T, store *PebbleStore, bucket, prefix, delimiter string, pageSize int) ([]listedVersion, []string) {
	t.Helper()
	var versions []listedVersion
	var common []string
	keyMarker, versionMarker := "", ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 10000, "the pages never end")
		page, err := store.ListObjectVersionsPage(context.Background(), bucket, prefix, delimiter, keyMarker, versionMarker, pageSize)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(page.Versions)+len(page.CommonPrefixes), pageSize)
		for _, v := range page.Versions {
			versions = append(versions, listedVersion{v.Key, versionLabel(v), v.IsLatest})
		}
		common = append(common, page.CommonPrefixes...)
		if !page.IsTruncated {
			return versions, common
		}
		keyMarker, versionMarker = page.NextKeyMarker, page.NextVersionIDMarker
	}
}

// Whatever the size of a page, a walk over the versions of a bucket gives each
// exactly once, keys in S3's order and the versions of a key newest first. Keys
// that sort differently from their version entries are in it: "a.b" sorts after
// "a" and before it in the version entries, whose key goes on with a colon.
func TestAWalkOverTheVersionsGivesEachOnce(t *testing.T) {
	store, cleanup := newVersionTestStore(t)
	defer cleanup()
	const bucket = "pages"
	at := time.Unix(1_700_000_000, 0)
	keys := []string{"a", "a.b", "a:b", "a/b", "b/1", "b/2", "c"}
	var want []listedVersion
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	for _, key := range sorted {
		for n := 3; n >= 1; n-- {
			want = append(want, listedVersion{key, fmt.Sprintf("v%d", n), n == 3})
		}
	}
	for _, key := range keys {
		for n := 1; n <= 3; n++ {
			putVersion(t, store, bucket, key, fmt.Sprintf("v%d", n), int64(n), at.Add(time.Duration(n)*time.Second), n == 3)
		}
	}

	for size := 1; size <= len(want)+1; size++ {
		got, common := walkVersions(t, store, bucket, "", "", size)
		assert.Equal(t, want, got, "pages of %d", size)
		assert.Empty(t, common)
	}
}

// Versions written within one second, as a backup does, keep one order from page
// to page: a time of whole seconds alone would leave it to chance.
func TestVersionsOfOneSecondKeepTheirOrderAcrossPages(t *testing.T) {
	store, cleanup := newVersionTestStore(t)
	defer cleanup()
	at := time.Unix(1_700_000_000, 0)
	var want []listedVersion
	for n := 9; n >= 0; n-- {
		want = append(want, listedVersion{"k", fmt.Sprintf("v%d", n), n == 9})
	}
	for n := 0; n < 10; n++ {
		putVersion(t, store, "second", "k", fmt.Sprintf("v%d", n), 1, at, n == 9)
	}

	for size := 1; size <= 4; size++ {
		got, _ := walkVersions(t, store, "second", "", "", size)
		assert.Equal(t, want, got, "pages of %d", size)
	}
}

// A prefix and a delimiter list what S3 lists, and a common prefix counts once
// however many keys it holds.
func TestAPageFollowsThePrefixAndTheDelimiter(t *testing.T) {
	store, cleanup := newVersionTestStore(t)
	defer cleanup()
	at := time.Unix(1_700_000_000, 0)
	for _, key := range []string{"blocks/0/a", "blocks/0/b", "blocks/1/a", "blocks/2/a", "index", "other/x"} {
		putVersion(t, store, "tree", key, "v1", 1, at, true)
	}

	versions, common := walkVersions(t, store, "tree", "", "/", 1)
	assert.Equal(t, []string{"blocks/", "other/"}, common)
	assert.Equal(t, []listedVersion{{"index", "v1", true}}, versions)

	versions, common = walkVersions(t, store, "tree", "blocks/", "/", 2)
	assert.Equal(t, []string{"blocks/0/", "blocks/1/", "blocks/2/"}, common)
	assert.Empty(t, versions)

	versions, common = walkVersions(t, store, "tree", "blocks/0/", "", 1)
	assert.Equal(t, []listedVersion{{"blocks/0/a", "v1", true}, {"blocks/0/b", "v1", true}}, versions)
	assert.Empty(t, common)
}

// A key marker alone starts after that key; with a version marker, after that
// version. A marker deleted since places the page by its ID.
func TestAPageStartsAfterItsMarkers(t *testing.T) {
	store, cleanup := newVersionTestStore(t)
	defer cleanup()
	ctx := context.Background()
	at := time.Unix(1_700_000_000, 0)
	for _, key := range []string{"a", "b", "c"} {
		for n := 1; n <= 3; n++ {
			putVersion(t, store, "marks", key, fmt.Sprintf("v%d", n), 1, at.Add(time.Duration(n)*time.Second), n == 3)
		}
	}
	first := func(keyMarker, versionMarker string) string {
		t.Helper()
		page, err := store.ListObjectVersionsPage(ctx, "marks", "", "", keyMarker, versionMarker, 1)
		require.NoError(t, err)
		require.NotEmpty(t, page.Versions)
		return page.Versions[0].Key + "/" + page.Versions[0].VersionID
	}

	assert.Equal(t, "c/v3", first("b", ""))
	assert.Equal(t, "b/v2", first("b", "v3"))
	assert.Equal(t, "b/v1", first("b", "v2"))
	assert.Equal(t, "c/v3", first("b", "v1"))

	require.NoError(t, store.DeleteObjectVersion(ctx, "marks", "b", "v2"))
	assert.Equal(t, "b/v1", first("b", "v2"), "the marker is gone: the older version follows")
}

// A page reads what it returns: entries past it are not touched, however many
// the bucket holds.
func TestAPageReadsOnlyWhatItReturns(t *testing.T) {
	store, cleanup := newVersionTestStore(t)
	defer cleanup()
	at := time.Unix(1_700_000_000, 0)
	for n := 0; n < 300; n++ {
		putVersion(t, store, "wide", fmt.Sprintf("k%04d", n), "v1", 1, at, true)
	}

	page, err := store.ListObjectVersionsPage(context.Background(), "wide", "", "", "k0100", "", 5)
	require.NoError(t, err)
	require.Len(t, page.Versions, 5)
	assert.Equal(t, "k0101", page.Versions[0].Key)
	assert.True(t, page.IsTruncated)
	assert.Equal(t, "k0105", page.NextKeyMarker)
	assert.Equal(t, "v1", page.NextVersionIDMarker)
}

// A null object that is current lists as the latest, over the versions of its
// key that are kept. A key with nothing after its page ends the listing.
func TestANullObjectListsAsTheLatest(t *testing.T) {
	store, cleanup := newVersionTestStore(t)
	defer cleanup()
	ctx := context.Background()
	at := time.Unix(1_700_000_000, 0)
	putVersion(t, store, "nulls", "k", "v1", 1, at, true)
	require.NoError(t, store.PutObject(ctx, &ObjectMetadata{Bucket: "nulls", Key: "k", Size: 2, ETag: "e", LastModified: at.Add(time.Second)}))

	got, _ := walkVersions(t, store, "nulls", "", "", 1)
	assert.Equal(t, []listedVersion{{"k", "null", true}, {"k", "v1", false}}, got)

	page, err := store.ListObjectVersionsPage(ctx, "nulls", "", "", "", "", 2)
	require.NoError(t, err)
	assert.Len(t, page.Versions, 2)
	assert.False(t, page.IsTruncated, "a full page with nothing after it is the last")
}

// A delete of a version does not hold the bucket through its fsync: deletes and
// writes of one bucket go on together, and each delete is durable once it
// returns.
func TestDeletesOfOneBucketShareTheirFsync(t *testing.T) {
	store, cleanup := newVersionTestStore(t)
	defer cleanup()
	ctx := context.Background()
	at := time.Unix(1_700_000_000, 0)
	const n = 200
	for i := 0; i < n; i++ {
		putVersion(t, store, "gone", fmt.Sprintf("k%d", i), "v1", 1, at, true)
	}

	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { errs <- store.DeleteObjectVersion(ctx, "gone", fmt.Sprintf("k%d", i), "v1") }()
	}
	for i := 0; i < n; i++ {
		require.NoError(t, <-errs)
	}
	got, _ := walkVersions(t, store, "gone", "", "", 50)
	assert.Empty(t, got)
}
