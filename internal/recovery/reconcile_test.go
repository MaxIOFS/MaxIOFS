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

// TestReconcileNeverDeletesVersionedEntries reproduces the production incident:
// a versioned (Object Lock) bucket stores object data under .versions/, so the
// plain path bucket/key never exists as a file. Reconcile must NOT treat those
// latest-version entries as ghosts. Regression for the reconcile that deleted
// ~50k Veeam metadata entries.
func TestReconcileNeverDeletesVersionedEntries(t *testing.T) {
	dataDir, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	ctx := context.Background()

	const versionID = "1775486761442908795.439e1cad"
	// Versioned object: data + sidecar live ONLY under .versions/, and the
	// store holds both the version entry and the "latest" pointer — exactly
	// the on-disk shape a Veeam/immutable bucket produces. No plain-path file.
	writeObjectPair(t, dataDir, ".versions/Veeam/Backup/blk-0001/"+versionID, "veeam-block-bytes", 1775486761)
	obj := &metadata.ObjectMetadata{Bucket: "bkt", Key: "Veeam/Backup/blk-0001", VersionID: versionID, Size: 17, ETag: "test-etag", IsLatest: true}
	version := &metadata.ObjectVersion{VersionID: versionID, IsLatest: true, Key: "Veeam/Backup/blk-0001", Size: 17, ETag: "test-etag"}
	if err := store.PutObjectVersion(ctx, obj, version); err != nil {
		t.Fatal(err)
	}

	report, err := Reconcile(ctx, dataDir, store, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("Reconcile failed: %v (failures: %v)", err, report.Failures)
	}

	// The latest-pointer entry must still resolve after reconcile.
	if _, err := store.GetObject(ctx, "bkt", "Veeam/Backup/blk-0001"); err != nil {
		t.Errorf("versioned object's latest entry was destroyed by reconcile: %v", err)
	}
	if _, err := store.GetObject(ctx, "bkt", "Veeam/Backup/blk-0001", versionID); err != nil {
		t.Errorf("versioned object's version entry was destroyed by reconcile: %v", err)
	}
}

// TestReconcileDoesNotPruneMissingDataEntry: a metadata entry whose plain data
// file is not visible must be reported by external tooling, not deleted by
// online reconcile. Missing paths can be caused by mount/layout/transient
// filesystem problems; pruning here caused production data loss.
func TestReconcileDoesNotPruneMissingDataEntry(t *testing.T) {
	dataDir, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	ctx := context.Background()

	// Genuine ghost: non-versioned entry, no data file on disk.
	if err := store.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: "bkt", Key: "ghost.txt", Size: 4, ETag: "ghost-etag",
	}); err != nil {
		t.Fatal(err)
	}
	// Delete marker: no disk artifact by design — must be kept.
	if err := store.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: "bkt", Key: "deleted.txt", Size: 0, ETag: "",
	}); err != nil {
		t.Fatal(err)
	}
	// Versioned entry with data on disk under .versions/ — must be kept.
	const versionID = "1775486761442908795.439e1cad"
	writeObjectPair(t, dataDir, ".versions/live-versioned.bin/"+versionID, "bytes", 1775486761)
	vobj := &metadata.ObjectMetadata{Bucket: "bkt", Key: "live-versioned.bin", VersionID: versionID, Size: 5, ETag: "e", IsLatest: true}
	if err := store.PutObjectVersion(ctx, vobj, &metadata.ObjectVersion{VersionID: versionID, IsLatest: true, Key: "live-versioned.bin", Size: 5, ETag: "e"}); err != nil {
		t.Fatal(err)
	}

	_, err := Reconcile(ctx, dataDir, store, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if _, err := store.GetObject(ctx, "bkt", "ghost.txt"); err != nil {
		t.Errorf("metadata entry without visible data file must survive: %v", err)
	}
	if _, err := store.GetObject(ctx, "bkt", "deleted.txt"); err != nil {
		t.Errorf("delete marker must survive: %v", err)
	}
	if _, err := store.GetObject(ctx, "bkt", "live-versioned.bin"); err != nil {
		t.Errorf("versioned object with data on disk must survive: %v", err)
	}
}

func TestReconcileDoesNotPruneOrphanSidecar(t *testing.T) {
	dataDir, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	bucketDir := filepath.Join(dataDir, "objects", "bkt")

	// Orphan sidecar: no sibling data file, no metadata entry. Online reconcile
	// must leave it in place because filesystem absence is not deletion proof.
	orphan := filepath.Join(bucketDir, "dead.txt.metadata")
	if err := os.WriteFile(orphan, []byte(`{"size":"3"}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Staged sidecar: owned by the storage backend's two-phase repair — untouched.
	staging := filepath.Join(bucketDir, "staged.txt.metadata-staging")
	if err := os.WriteFile(staging, []byte(`{"size":"3"}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Healthy versioned sidecar: its sibling data file exists → must be kept.
	const vid = "1775486761442908795.aaaa"
	writeObjectPair(t, dataDir, ".versions/keep.bin/"+vid, "bytes", 1775486761)

	_, err := Reconcile(context.Background(), dataDir, store, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Error("orphan sidecar must NOT be touched")
	}
	if _, err := os.Stat(staging); err != nil {
		t.Error("staged sidecar must NOT be touched")
	}
	if _, err := os.Stat(filepath.Join(bucketDir, ".versions", "keep.bin", vid+".metadata")); err != nil {
		t.Error("healthy versioned sidecar (sibling data present) must NOT be removed")
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
