package metadata

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// A paginated listing is a promise: follow the markers and you see every key
// exactly once. Each listing decides when to stop and what marker to hand back,
// and the two decisions have to agree — off by one and the page boundary either
// swallows a key or repeats it.
//
// This drives every paginated listing at every page size, so a listing that
// gets the marker wrong fails here whichever one it is.
type paginatedListing struct {
	name string
	page func(t *testing.T, s *PebbleStore, bucket, marker string, maxKeys int) (keys []string, next string)
}

func paginatedListings() []paginatedListing {
	return []paginatedListing{
		{
			name: "ListObjects",
			page: func(t *testing.T, s *PebbleStore, bucket, marker string, maxKeys int) ([]string, string) {
				objs, next, err := s.ListObjects(context.Background(), bucket, "", marker, maxKeys)
				require.NoError(t, err)
				return keysOf(objs), next
			},
		},
		{
			name: "searchObjectsByScan",
			page: func(t *testing.T, s *PebbleStore, bucket, marker string, maxKeys int) ([]string, string) {
				objs, next, err := s.searchObjectsByScan(context.Background(), bucket, "", marker, maxKeys, &ObjectFilter{})
				require.NoError(t, err)
				return keysOf(objs), next
			},
		},
		{
			name: "searchObjectsWithTags",
			page: func(t *testing.T, s *PebbleStore, bucket, marker string, maxKeys int) ([]string, string) {
				objs, next, err := s.searchObjectsWithTags(context.Background(), bucket, "", marker, maxKeys,
					&ObjectFilter{Tags: map[string]string{"team": "core"}})
				require.NoError(t, err)
				return keysOf(objs), next
			},
		},
	}
}

func keysOf(objs []*ObjectMetadata) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.Key)
	}
	return out
}

func setupPagerPropertyStore(t *testing.T, objectCount int) (*PebbleStore, string, []string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "maxiofs-pagination-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := NewPebbleStore(PebbleOptions{DataDir: dir, Logger: logrus.StandardLogger()})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	const bucket = "pagination"
	require.NoError(t, store.CreateBucket(ctx, &BucketMetadata{Name: bucket, OwnerID: "u"}))

	want := make([]string, 0, objectCount)
	for i := 0; i < objectCount; i++ {
		key := fmt.Sprintf("k%03d", i)
		require.NoError(t, store.PutObject(ctx, &ObjectMetadata{
			Bucket: bucket,
			Key:    key,
			Size:   int64(i + 1),
			ETag:   fmt.Sprintf("%032x", i+1),
			Tags:   map[string]string{"team": "core"},
		}))
		want = append(want, key)
	}
	return store, bucket, want
}

func TestPagination_EveryListingReturnsEveryKeyExactlyOnce(t *testing.T) {
	const objectCount = 25
	store, bucket, want := setupPagerPropertyStore(t, objectCount)

	for _, listing := range paginatedListings() {
		for _, pageSize := range []int{1, 2, 3, 7, 24, 25, 26, 100} {
			t.Run(fmt.Sprintf("%s/pageSize=%d", listing.name, pageSize), func(t *testing.T) {
				var got []string
				marker := ""
				for pages := 0; ; pages++ {
					require.Less(t, pages, objectCount+5, "paging did not terminate")

					keys, next := listing.page(t, store, bucket, marker, pageSize)
					got = append(got, keys...)
					if next == "" {
						break
					}
					require.NotEqual(t, marker, next, "the marker did not advance, so paging would loop")
					marker = next
				}

				require.Equal(t, want, got,
					"paging with page size %d did not return every key exactly once", pageSize)
			})
		}
	}
}

// The delimited listing pages over two kinds of entry — keys and the common
// prefixes that stand for a whole folder — and both have to survive the page
// boundary intact.
func TestPagination_DelimitedListingCoversKeysAndPrefixesExactlyOnce(t *testing.T) {
	dir, err := os.MkdirTemp("", "maxiofs-pagination-delim-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := NewPebbleStore(PebbleOptions{DataDir: dir, Logger: logrus.StandardLogger()})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	const bucket = "delimited"
	require.NoError(t, store.CreateBucket(ctx, &BucketMetadata{Name: bucket, OwnerID: "u"}))

	// Flat keys interleaved with folders holding several objects each, so a
	// page can end in the middle of a prefix group.
	var wantFlat []string
	var wantPrefixes []string
	for i := 0; i < 6; i++ {
		flat := fmt.Sprintf("a%02d.txt", i)
		require.NoError(t, store.PutObject(ctx, &ObjectMetadata{
			Bucket: bucket, Key: flat, Size: 1, ETag: "e",
		}))
		wantFlat = append(wantFlat, flat)

		folder := fmt.Sprintf("f%02d/", i)
		for j := 0; j < 3; j++ {
			require.NoError(t, store.PutObject(ctx, &ObjectMetadata{
				Bucket: bucket, Key: fmt.Sprintf("%so%d.txt", folder, j), Size: 1, ETag: "e",
			}))
		}
		wantPrefixes = append(wantPrefixes, folder)
	}

	for _, pageSize := range []int{1, 2, 3, 5, 11, 12, 13, 100} {
		t.Run(fmt.Sprintf("pageSize=%d", pageSize), func(t *testing.T) {
			var gotFlat, gotPrefixes []string
			marker := ""
			for pages := 0; ; pages++ {
				require.Less(t, pages, 50, "paging did not terminate")

				res, err := store.ListObjectsDelimited(ctx, bucket, "", "/", marker, pageSize)
				require.NoError(t, err)
				gotFlat = append(gotFlat, keysOf(res.Objects)...)
				gotPrefixes = append(gotPrefixes, res.CommonPrefixes...)

				if !res.IsTruncated || res.NextMarker == "" {
					break
				}
				require.NotEqual(t, marker, res.NextMarker, "the marker did not advance, so paging would loop")
				marker = res.NextMarker
			}

			require.ElementsMatch(t, wantFlat, gotFlat, "every key must appear exactly once")
			require.ElementsMatch(t, wantPrefixes, gotPrefixes, "every folder must appear exactly once")
		})
	}
}
