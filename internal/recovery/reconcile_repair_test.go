package recovery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// writeObjectWithSidecar writes a data file plus an arbitrary sidecar, so a
// test can state exactly what the storage layer left behind.
func writeObjectWithSidecar(t *testing.T, dataDir, relPath, content string, sidecar map[string]string) {
	t.Helper()
	full := filepath.Join(dataDir, "objects", "bkt", filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full+".metadata", data, 0644); err != nil {
		t.Fatal(err)
	}
}

// An entry left describing the bytes an overwrite replaced makes a GET announce
// one length and deliver another. The bytes on disk are what is served, so the
// entry is the side that has to move.
func TestReconcileRepairsEntryThatDescribesOtherBytes(t *testing.T) {
	dataDir, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	ctx := context.Background()

	const stored = "forty bytes of freshly written replacement"
	writeObjectWithSidecar(t, dataDir, "key.txt", stored, map[string]string{
		"size":          "42",
		"etag":          "new-etag",
		"last_modified": "1700000600",
		"content-type":  "text/plain",
	})
	if err := store.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: "bkt", Key: "key.txt", Size: 12, ETag: "old-etag",
		LastModified: time.Unix(1700000000, 0), ContentType: "text/plain",
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Reconcile(ctx, dataDir, store, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if report.EntriesRepaired != 1 {
		t.Fatalf("EntriesRepaired = %d, want 1 (failures: %v)", report.EntriesRepaired, report.Failures)
	}

	entry, err := store.GetObject(ctx, "bkt", "key.txt")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Size != int64(len(stored)) {
		t.Errorf("size = %d, want %d", entry.Size, len(stored))
	}
	if entry.ETag != "new-etag" {
		t.Errorf("etag = %q, want new-etag", entry.ETag)
	}
	if entry.LastModified.Unix() != 1700000600 {
		t.Errorf("last modified = %d, want 1700000600", entry.LastModified.Unix())
	}
	if entry.ContentType != "text/plain" {
		t.Errorf("content type = %q, want text/plain", entry.ContentType)
	}
}

// The repair takes the ETag the client was given, not one recomputed from the
// assembled bytes.
func TestReconcileRepairKeepsMultipartETagFromSidecar(t *testing.T) {
	dataDir, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	ctx := context.Background()

	const stored = "assembled multipart content"
	writeObjectWithSidecar(t, dataDir, "mp.txt", stored, map[string]string{
		"size":           "27",
		"etag":           "whole-object-md5",
		"multipart-etag": "d41d8cd98f00b204e9800998ecf8427e-3",
		"last_modified":  "1700000600",
	})
	if err := store.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: "bkt", Key: "mp.txt", Size: 5, ETag: "stale-etag-2",
		LastModified: time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Reconcile(ctx, dataDir, store, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if report.EntriesRepaired != 1 {
		t.Fatalf("EntriesRepaired = %d, want 1 (failures: %v)", report.EntriesRepaired, report.Failures)
	}
	entry, err := store.GetObject(ctx, "bkt", "mp.txt")
	if err != nil {
		t.Fatal(err)
	}
	if entry.ETag != "d41d8cd98f00b204e9800998ecf8427e-3" {
		t.Errorf("etag = %q, want the multipart ETag from the sidecar", entry.ETag)
	}
}

// Everything the repair must keep its hands off.
func TestReconcileLeavesEntriesItCannotProveStale(t *testing.T) {
	cases := []struct {
		name    string
		content string
		sidecar map[string]string
		entry   *metadata.ObjectMetadata
	}{
		{
			// A multipart object written before the multipart ETag reached the
			// sidecar: same bytes, two ways of naming them.
			name:    "multipart entry against a legacy sidecar",
			content: "sixteen bytes!!!",
			sidecar: map[string]string{"size": "16", "etag": "whole-object-md5", "last_modified": "1700000600"},
			entry: &metadata.ObjectMetadata{
				Bucket: "bkt", Key: "legacy-mp.txt", Size: 16, ETag: "aaaabbbbccccdddd-4",
				LastModified: time.Unix(1700000000, 0),
			},
		},
		{
			// Same length, different ETag, but the file is the older of the
			// two: nothing says the entry is the stale side.
			name:    "entry newer than the file",
			content: "sixteen bytes!!!",
			sidecar: map[string]string{"size": "16", "etag": "file-etag", "last_modified": "1700000000"},
			entry: &metadata.ObjectMetadata{
				Bucket: "bkt", Key: "newer-entry.txt", Size: 16, ETag: "entry-etag",
				LastModified: time.Unix(1700000600, 0),
			},
		},
		{
			// A sidecar that states no size cannot contradict anything.
			name:    "sidecar without a size",
			content: "sixteen bytes!!!",
			sidecar: map[string]string{"etag": "file-etag", "last_modified": "1700000600"},
			entry: &metadata.ObjectMetadata{
				Bucket: "bkt", Key: "no-size.txt", Size: 99, ETag: "entry-etag",
				LastModified: time.Unix(1700000000, 0),
			},
		},
		{
			// A delete marker describes no bytes of its own.
			name:    "delete marker",
			content: "sixteen bytes!!!",
			sidecar: map[string]string{"size": "16", "etag": "file-etag", "last_modified": "1700000600"},
			entry: &metadata.ObjectMetadata{
				Bucket: "bkt", Key: "marker.txt", Size: 0, ETag: "",
				LastModified: time.Unix(1700000000, 0),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir, store, cleanup := setupReconcileTest(t)
			defer cleanup()
			ctx := context.Background()

			writeObjectWithSidecar(t, dataDir, tc.entry.Key, tc.content, tc.sidecar)
			if err := store.PutObject(ctx, tc.entry); err != nil {
				t.Fatal(err)
			}
			before, err := store.GetObject(ctx, "bkt", tc.entry.Key)
			if err != nil {
				t.Fatal(err)
			}

			report, err := Reconcile(ctx, dataDir, store, logrus.StandardLogger())
			if err != nil {
				t.Fatalf("Reconcile failed: %v", err)
			}
			if report.EntriesRepaired != 0 {
				t.Errorf("EntriesRepaired = %d, want 0", report.EntriesRepaired)
			}

			after, err := store.GetObject(ctx, "bkt", tc.entry.Key)
			if err != nil {
				t.Fatal(err)
			}
			if after.Size != before.Size || after.ETag != before.ETag || !after.LastModified.Equal(before.LastModified) {
				t.Errorf("entry changed: %d/%q/%v → %d/%q/%v",
					before.Size, before.ETag, before.LastModified,
					after.Size, after.ETag, after.LastModified)
			}
		})
	}
}

// The repair rewrites the identity fields and nothing else: retention, tags and
// ACLs are the index's own knowledge, absent from any sidecar.
func TestReconcileRepairPreservesEntryOnlyMetadata(t *testing.T) {
	dataDir, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	ctx := context.Background()

	writeObjectWithSidecar(t, dataDir, "held.txt", "content after the overwrite", map[string]string{
		"size":          "26",
		"etag":          "new-etag",
		"last_modified": "1700000600",
	})
	retainUntil := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	if err := store.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: "bkt", Key: "held.txt", Size: 4, ETag: "old-etag",
		LastModified: time.Unix(1700000000, 0),
		Tags:         map[string]string{"team": "backups"},
		LegalHold:    true,
		Retention:    &metadata.RetentionMetadata{Mode: "COMPLIANCE", RetainUntilDate: retainUntil},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := Reconcile(ctx, dataDir, store, logrus.StandardLogger()); err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	entry, err := store.GetObject(ctx, "bkt", "held.txt")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Size != 26 || entry.ETag != "new-etag" {
		t.Errorf("identity not repaired: size=%d etag=%q", entry.Size, entry.ETag)
	}
	if !entry.LegalHold {
		t.Error("legal hold lost")
	}
	if entry.Retention == nil || entry.Retention.Mode != "COMPLIANCE" {
		t.Errorf("retention lost: %+v", entry.Retention)
	}
	if entry.Tags["team"] != "backups" {
		t.Errorf("tags lost: %v", entry.Tags)
	}
}
