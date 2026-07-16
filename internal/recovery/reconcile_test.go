package recovery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// setupReconcileTest builds a data dir with one registered bucket ("bkt") and
// a live Pebble store over the same layout the server uses.
func setupReconcileTest(t *testing.T) (dataDir string, store metadata.Store, cleanup func()) {
	t.Helper()
	dataDir, err := os.MkdirTemp("", "reconcile-test-*")
	if err != nil {
		t.Fatal(err)
	}

	bucketDir := filepath.Join(dataDir, "objects", "bkt")
	if err := os.MkdirAll(bucketDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketDir, ".maxiofs-bucket"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	ps, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: dataDir, WALSyncInterval: -1})
	if err != nil {
		t.Fatal(err)
	}
	if err := ps.CreateBucket(context.Background(), &metadata.BucketMetadata{Name: "bkt"}); err != nil {
		ps.Close() //nolint:errcheck
		t.Fatal(err)
	}

	return dataDir, ps, func() {
		ps.Close()            //nolint:errcheck
		os.RemoveAll(dataDir) //nolint:errcheck
	}
}

// writeObjectPair writes a data file plus the plaintext sidecar the
// filesystem backend would have produced.
func writeObjectPair(t *testing.T, dataDir, relPath, content string, lastModified int64) {
	t.Helper()
	full := filepath.Join(dataDir, "objects", "bkt", filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	sidecar := map[string]string{
		"size":          strconv.Itoa(len(content)),
		"etag":          "test-etag",
		"last_modified": strconv.FormatInt(lastModified, 10),
		"content-type":  "text/plain",
	}
	data, err := json.Marshal(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full+".metadata", data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileRestoresLostEntry(t *testing.T) {
	dataDir, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	ctx := context.Background()

	// Crash victim: file + sidecar on disk, no Pebble entry.
	const lastModified = int64(1700000000)
	writeObjectPair(t, dataDir, "lost.txt", "hello", lastModified)

	// Healthy neighbour: file + sidecar + entry — must survive untouched.
	writeObjectPair(t, dataDir, "healthy.txt", "okdata", lastModified)
	if err := store.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: "bkt", Key: "healthy.txt", Size: 6, ETag: "healthy-etag",
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Reconcile(ctx, dataDir, store, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("Reconcile failed: %v (failures: %v)", err, report.Failures)
	}
	if report.EntriesRestored != 1 {
		t.Fatalf("EntriesRestored = %d, want 1 (failures: %v)", report.EntriesRestored, report.Failures)
	}

	obj, err := store.GetObject(ctx, "bkt", "lost.txt")
	if err != nil {
		t.Fatalf("restored entry not found: %v", err)
	}
	if obj.Size != 5 || obj.ETag != "test-etag" {
		t.Errorf("restored entry: size=%d etag=%q, want 5/test-etag", obj.Size, obj.ETag)
	}
	if obj.LastModified.Unix() != lastModified {
		t.Errorf("restored LastModified = %d, want %d (must come from the sidecar, not now)", obj.LastModified.Unix(), lastModified)
	}

	// Healthy entry keeps its own metadata (live store is authoritative).
	healthy, err := store.GetObject(ctx, "bkt", "healthy.txt")
	if err != nil || healthy.ETag != "healthy-etag" {
		t.Errorf("healthy entry disturbed: %v / %+v", err, healthy)
	}

	// Bucket stats were recalculated to cover the restored object.
	count, size, err := store.GetBucketStats(ctx, "", "bkt")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || size != 11 {
		t.Errorf("bucket stats = (%d, %d), want (2, 11)", count, size)
	}
}

func TestReconcileRemovesGhostEntry(t *testing.T) {
	dataDir, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	ctx := context.Background()

	// Ghost: Pebble entry with no data file (pre-fix crash during delete).
	if err := store.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: "bkt", Key: "ghost.txt", Size: 4, ETag: "ghost-etag",
	}); err != nil {
		t.Fatal(err)
	}
	// Delete marker: entry with no disk artifact BY DESIGN — must be kept.
	if err := store.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: "bkt", Key: "deleted-versioned.txt", Size: 0, ETag: "",
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Reconcile(ctx, dataDir, store, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if report.GhostsRemoved != 1 {
		t.Fatalf("GhostsRemoved = %d, want 1 (failures: %v)", report.GhostsRemoved, report.Failures)
	}

	if _, err := store.GetObject(ctx, "bkt", "ghost.txt"); err != metadata.ErrObjectNotFound {
		t.Errorf("ghost entry should be removed, got err=%v", err)
	}
	if _, err := store.GetObject(ctx, "bkt", "deleted-versioned.txt"); err != nil {
		t.Errorf("delete marker must survive reconciliation: %v", err)
	}
}

func TestReconcileCleansOrphanSidecarKeepsStaging(t *testing.T) {
	dataDir, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	bucketDir := filepath.Join(dataDir, "objects", "bkt")

	// Orphan sidecar: data file and entry both gone (half-completed delete).
	orphan := filepath.Join(bucketDir, "dead.txt.metadata")
	if err := os.WriteFile(orphan, []byte(`{"size":"3"}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Staged sidecar: owned by the storage backend's two-phase repair.
	staging := filepath.Join(bucketDir, "staged.txt.metadata-staging")
	if err := os.WriteFile(staging, []byte(`{"size":"3"}`), 0644); err != nil {
		t.Fatal(err)
	}

	report, err := Reconcile(context.Background(), dataDir, store, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if report.SidecarsCleaned != 1 {
		t.Fatalf("SidecarsCleaned = %d, want 1 (failures: %v)", report.SidecarsCleaned, report.Failures)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("orphan sidecar should be removed")
	}
	if _, err := os.Stat(staging); err != nil {
		t.Error("staged sidecar must NOT be touched by the reconciler")
	}
}

func TestReconcileRestoresVersion(t *testing.T) {
	dataDir, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	ctx := context.Background()

	// A versioned PUT stores the latest bytes at the plain path AND the
	// version copy under .versions/ — a crash loses both metadata entries
	// while both files survive.
	const versionID = "1700000000000000001"
	writeObjectPair(t, dataDir, "doc.txt", "v1-bytes", 1700000000)
	writeObjectPair(t, dataDir, ".versions/doc.txt/"+versionID, "v1-bytes", 1700000000)

	report, err := Reconcile(ctx, dataDir, store, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if report.VersionsRestored != 1 {
		t.Fatalf("VersionsRestored = %d, want 1 (failures: %v)", report.VersionsRestored, report.Failures)
	}
	if report.GhostsRemoved != 0 {
		t.Fatalf("GhostsRemoved = %d, want 0 (failures: %v)", report.GhostsRemoved, report.Failures)
	}

	obj, err := store.GetObject(ctx, "bkt", "doc.txt", versionID)
	if err != nil {
		t.Fatalf("restored version not found: %v", err)
	}
	if obj.Size != 8 {
		t.Errorf("restored version size = %d, want 8", obj.Size)
	}
	// Restored as the only version → it is latest, so the main entry exists too.
	if _, err := store.GetObject(ctx, "bkt", "doc.txt"); err != nil {
		t.Errorf("latest restored version should also surface as the main entry: %v", err)
	}
}

func TestReconcileSkipsBucketMissingFromStore(t *testing.T) {
	dataDir, store, cleanup := setupReconcileTest(t)
	defer cleanup()

	// A whole bucket on disk that the store does not know about is beyond
	// the crash window — recorded as a failure, never half-repaired.
	strayDir := filepath.Join(dataDir, "objects", "stray")
	if err := os.MkdirAll(strayDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(strayDir, ".maxiofs-bucket"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	writeObjectPair(t, dataDir, "../stray/file.txt", "data", 1700000000)

	report, err := Reconcile(context.Background(), dataDir, store, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if report.EntriesRestored != 0 {
		t.Errorf("nothing should be restored into an unknown bucket, got %d", report.EntriesRestored)
	}
	if len(report.Failures) != 1 {
		t.Errorf("expected exactly one recorded failure for the stray bucket, got %v", report.Failures)
	}
}
