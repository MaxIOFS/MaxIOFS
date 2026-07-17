package metadata

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// setupPaginationStore seeds a store with a deterministic key set:
// 5 folders of 7 objects each + 11 root objects = 46 keys.
func setupPaginationStore(t *testing.T) (*PebbleStore, []string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "pebble-pagination-*")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewPebbleStore(PebbleOptions{DataDir: dir, WALSyncInterval: -1})
	if err != nil {
		os.RemoveAll(dir) //nolint:errcheck
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.CreateBucket(ctx, &BucketMetadata{Name: "pgbkt"}); err != nil {
		t.Fatal(err)
	}

	var keys []string
	for f := 0; f < 5; f++ {
		for o := 0; o < 7; o++ {
			keys = append(keys, fmt.Sprintf("folder-%02d/obj-%02d.bin", f, o))
		}
	}
	for r := 0; r < 11; r++ {
		keys = append(keys, fmt.Sprintf("root-%02d.bin", r))
	}
	for _, k := range keys {
		if err := store.PutObject(ctx, &ObjectMetadata{Bucket: "pgbkt", Key: k, Size: 1, ETag: "e"}); err != nil {
			t.Fatal(err)
		}
	}

	return store, keys, func() {
		store.Close()      //nolint:errcheck
		os.RemoveAll(dir)  //nolint:errcheck
	}
}

// TestListObjectsPaginationLossless drives a full marker loop at several page
// sizes and asserts the union is exactly the seeded key set — no key lost at
// any page boundary, no duplicates. Regression for the NextMarker off-by-one
// that dropped one object per page.
func TestListObjectsPaginationLossless(t *testing.T) {
	store, keys, cleanup := setupPaginationStore(t)
	defer cleanup()
	ctx := context.Background()

	for _, pageSize := range []int{1, 2, 3, 5, 10, 46, 100} {
		seen := make(map[string]int)
		marker := ""
		for page := 0; page < 200; page++ {
			objs, next, err := store.ListObjects(ctx, "pgbkt", "", marker, pageSize)
			if err != nil {
				t.Fatalf("pageSize=%d: %v", pageSize, err)
			}
			for _, o := range objs {
				seen[o.Key]++
			}
			if next == "" {
				break
			}
			marker = next
		}
		if len(seen) != len(keys) {
			t.Errorf("pageSize=%d: got %d unique keys, want %d", pageSize, len(seen), len(keys))
		}
		for _, k := range keys {
			if seen[k] == 0 {
				t.Errorf("pageSize=%d: key LOST at page boundary: %s", pageSize, k)
			} else if seen[k] > 1 {
				t.Errorf("pageSize=%d: key DUPLICATED across pages: %s (%d times)", pageSize, k, seen[k])
			}
		}
	}
}

// TestListObjectsDelimitedPaginationLossless does the same for the delimited
// listing: the union of objects + common prefixes across a marker loop must
// be exactly {5 folders} + {11 root objects}, at page sizes that force
// boundaries to land both on prefixes and on objects.
func TestListObjectsDelimitedPaginationLossless(t *testing.T) {
	store, _, cleanup := setupPaginationStore(t)
	defer cleanup()
	ctx := context.Background()

	wantPrefixes := 5
	wantObjects := 11

	for _, pageSize := range []int{1, 2, 3, 4, 7, 16, 100} {
		prefixes := make(map[string]int)
		objects := make(map[string]int)
		marker := ""
		for page := 0; page < 200; page++ {
			res, err := store.ListObjectsDelimited(ctx, "pgbkt", "", "/", marker, pageSize)
			if err != nil {
				t.Fatalf("pageSize=%d: %v", pageSize, err)
			}
			for _, p := range res.CommonPrefixes {
				prefixes[p]++
			}
			for _, o := range res.Objects {
				objects[o.Key]++
			}
			if !res.IsTruncated || res.NextMarker == "" {
				break
			}
			marker = res.NextMarker
		}
		if len(prefixes) != wantPrefixes {
			t.Errorf("pageSize=%d: got %d unique prefixes, want %d (%v)", pageSize, len(prefixes), wantPrefixes, prefixes)
		}
		if len(objects) != wantObjects {
			t.Errorf("pageSize=%d: got %d unique objects, want %d (%v)", pageSize, len(objects), wantObjects, objects)
		}
		for p, n := range prefixes {
			if n > 1 {
				t.Errorf("pageSize=%d: prefix DUPLICATED: %s (%d times)", pageSize, p, n)
			}
		}
		for k, n := range objects {
			if n > 1 {
				t.Errorf("pageSize=%d: object DUPLICATED: %s (%d times)", pageSize, k, n)
			}
		}
	}
}

// TestSearchObjectsPaginationLossless covers the search scan path.
func TestSearchObjectsPaginationLossless(t *testing.T) {
	store, keys, cleanup := setupPaginationStore(t)
	defer cleanup()
	ctx := context.Background()

	for _, pageSize := range []int{1, 3, 10, 100} {
		seen := make(map[string]int)
		marker := ""
		for page := 0; page < 200; page++ {
			objs, next, err := store.SearchObjects(ctx, "pgbkt", "", marker, pageSize, &ObjectFilter{})
			if err != nil {
				t.Fatalf("pageSize=%d: %v", pageSize, err)
			}
			for _, o := range objs {
				seen[o.Key]++
			}
			if next == "" {
				break
			}
			marker = next
		}
		if len(seen) != len(keys) {
			t.Errorf("pageSize=%d: got %d unique keys, want %d", pageSize, len(seen), len(keys))
		}
	}
}
