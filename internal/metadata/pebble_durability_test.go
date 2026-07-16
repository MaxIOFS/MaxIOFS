package metadata

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCleanShutdownSentinel verifies the sentinel lifecycle: fresh stores and
// cleanly-closed stores report a clean shutdown; a store whose sentinel is
// missing (previous process died hard) reports unclean.
func TestCleanShutdownSentinel(t *testing.T) {
	dir, err := os.MkdirTemp("", "pebble-sentinel-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir) //nolint:errcheck

	sentinel := filepath.Join(dir, "metadata", cleanShutdownSentinelFile)

	// Fresh store: nothing to reconcile.
	s1, err := NewPebbleStore(PebbleOptions{DataDir: dir, WALSyncInterval: -1})
	if err != nil {
		t.Fatal(err)
	}
	if !s1.WasCleanShutdown() {
		t.Error("fresh store should report a clean shutdown")
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("Close should write the clean-shutdown sentinel: %v", err)
	}

	// Reopen after clean close: clean, and the sentinel is consumed.
	s2, err := NewPebbleStore(PebbleOptions{DataDir: dir, WALSyncInterval: -1})
	if err != nil {
		t.Fatal(err)
	}
	if !s2.WasCleanShutdown() {
		t.Error("store closed cleanly should report a clean shutdown")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Error("open should consume the clean-shutdown sentinel")
	}
	if err := s2.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate a hard kill: the sentinel the previous Close wrote is gone.
	if err := os.Remove(sentinel); err != nil {
		t.Fatal(err)
	}
	s3, err := NewPebbleStore(PebbleOptions{DataDir: dir, WALSyncInterval: -1})
	if err != nil {
		t.Fatal(err)
	}
	if s3.WasCleanShutdown() {
		t.Error("store without sentinel should report an UNCLEAN shutdown")
	}
	if err := s3.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestWALSyncLoop exercises the periodic WAL fsync loop end to end: writes
// mark the WAL dirty, the loop drains the flag, and Close does not deadlock
// or race with an in-flight tick.
func TestWALSyncLoop(t *testing.T) {
	dir, err := os.MkdirTemp("", "pebble-walsync-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir) //nolint:errcheck

	store, err := NewPebbleStore(PebbleOptions{DataDir: dir, WALSyncInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := store.CreateBucket(ctx, &BucketMetadata{Name: "walsync-bucket"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		obj := &ObjectMetadata{Bucket: "walsync-bucket", Key: "obj-" + string(rune('a'+i)), Size: 10, ETag: "etag"}
		if err := store.PutObject(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}

	// Let several ticks fire so the loop demonstrably drains the dirty flag.
	deadline := time.Now().Add(2 * time.Second)
	for store.walDirty.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if store.walDirty.Load() {
		t.Error("WAL sync loop never drained the dirty flag")
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close with active WAL sync loop failed: %v", err)
	}
}
