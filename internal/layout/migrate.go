// Package layout moves an existing storage tree from the key-as-path layout to
// the digest layout.
package layout

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

const (
	progressKeyPrefix = "migration:layout2:"
	sidecarSuffix     = ".metadata"
	stagingSuffix     = ".metadata-staging"
)

// Options configures one migration run.
type Options struct {
	Root   string
	Store  metadata.Store
	Logger *logrus.Logger

	// DryRun reports what would move without touching a single file.
	DryRun bool
}

// Report is the outcome of a run. Anything the migration could not account for
// lands in one of the slices rather than stopping the run.
type Report struct {
	DryRun         bool
	AlreadyCurrent bool
	Buckets        int
	BucketsSkipped int
	// Files moved, split by where they were in the old layout. A versioned
	// bucket kept its objects under .versions/, so its objects are counted in
	// VersionsMoved: neither field is a count of objects in a bucket.
	ObjectsMoved   int
	VersionsMoved  int
	MarkersCreated int
	FoldersPurged  int
	BytesMoved     int64
	MissingData    []string
	Stranded       []string
	Damaged        []string
	Failures       []string
}

type bucketRef struct {
	path     string // "tenant/name" or "name"
	name     string
	tenantID string
}

// Migrate brings the storage root to the current layout. It never removes a
// file before its replacement is in place and verified, and a run that stops
// half way can be repeated.
func Migrate(ctx context.Context, opts Options) (*Report, error) {
	log := opts.Logger
	if log == nil {
		log = logrus.StandardLogger()
	}
	report := &Report{DryRun: opts.DryRun}

	if _, err := os.Stat(opts.Root); os.IsNotExist(err) {
		report.AlreadyCurrent = true // nothing stored yet; the backend stamps the layout
		return report, nil
	}

	version, fresh, err := storage.ReadLayoutVersion(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("could not read the storage layout: %w", err)
	}
	if version > storage.LayoutVersion {
		return nil, fmt.Errorf("storage layout v%d is newer than this build reads (v%d)", version, storage.LayoutVersion)
	}

	buckets, err := listBuckets(ctx, opts.Store)
	if err != nil {
		return nil, err
	}

	// An up-to-date root still gets audited: a bucket directory the index does
	// not know is stranded whether or not anything moved today.
	if fresh || version == storage.LayoutVersion {
		report.AlreadyCurrent = true
		purgeImplicitFolders(ctx, opts, buckets, report)
		findStranded(opts.Root, buckets, report)
		return report, nil
	}

	if err := preflight(buckets); err != nil {
		return nil, err
	}

	for _, bkt := range buckets {
		done, err := isDone(ctx, opts.Store, bkt.path)
		if err != nil {
			return nil, err
		}
		if done {
			report.BucketsSkipped++
			continue
		}

		if err := migrateBucket(ctx, opts, bkt, report, log); err != nil {
			return report, err
		}
		report.Buckets++

		if !opts.DryRun {
			if err := markDone(ctx, opts.Store, bkt.path); err != nil {
				return report, err
			}
		}
	}

	purgeImplicitFolders(ctx, opts, buckets, report)
	findStranded(opts.Root, buckets, report)

	if !opts.DryRun {
		dropFolderMarkers(filepath.Join(opts.Root, ".maxiofs"))
		if err := storage.WriteLayoutVersion(opts.Root, storage.LayoutVersion); err != nil {
			return report, fmt.Errorf("could not record the new layout: %w", err)
		}
	}
	return report, nil
}

// findStranded reports directories under the storage root that still hold
// object data while the index knows no bucket there. A deleted bucket whose
// files could not be removed looks exactly like this: the bucket marker is
// gone, the objects are not. They stay in the old layout and the server will
// never serve them, so the operator decides whether to recover or delete them.
func findStranded(root string, buckets []bucketRef, report *Report) {
	known := make(map[string]bool, len(buckets))
	for _, b := range buckets {
		known[storage.BucketDirName(b.path)] = true
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || known[e.Name()] {
			continue
		}
		for dir, count := range dataBearingDirs(filepath.Join(root, e.Name()), root) {
			report.Stranded = append(report.Stranded, fmt.Sprintf("%s (%d files)", dir, count))
		}
	}
	sort.Strings(report.Stranded)
}

// dataBearingDirs maps each directory holding object data, relative to root, to
// how many data files it holds.
func dataBearingDirs(dir, root string) map[string]int {
	found := map[string]int{}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || isInternalName(info.Name()) {
			return nil //nolint:nilerr
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(p))
		if relErr != nil {
			return nil
		}
		found[filepath.ToSlash(rel)]++
		return nil
	})
	return found
}

// preflight refuses to run on a tree the new layout cannot represent, before
// anything moves.
func preflight(buckets []bucketRef) error {
	byName := map[string][]string{}
	tenants := map[string]bool{}
	for _, b := range buckets {
		byName[b.name] = append(byName[b.name], b.path)
		if b.tenantID != "" {
			tenants[b.tenantID] = true
		}
	}

	var conflicts []string
	for name, paths := range byName {
		if len(paths) > 1 {
			sort.Strings(paths)
			conflicts = append(conflicts, fmt.Sprintf("bucket name %q is used by %s", name, strings.Join(paths, " and ")))
		}
		if tenants[name] {
			conflicts = append(conflicts, fmt.Sprintf("bucket name %q is also a tenant ID, so its objects and that tenant's buckets share a directory", name))
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return fmt.Errorf("migration refused, rename these first:\n  %s", strings.Join(conflicts, "\n  "))
	}
	return nil
}

func migrateBucket(ctx context.Context, opts Options, bkt bucketRef, report *Report, log *logrus.Logger) error {
	oldDir := filepath.Join(opts.Root, filepath.FromSlash(bkt.path))
	newDir := filepath.Join(opts.Root, storage.BucketDirName(bkt.path))

	files, err := collectDataFiles(oldDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // bucket registered but never written to
		}
		return err
	}

	for _, rel := range files {
		key, versionID, ok := splitV1Path(rel)
		if !ok {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %s does not name an object", bkt.path, rel))
			continue
		}

		ref := storage.ObjectRef{Bucket: bkt.path, Key: key, VersionID: versionID}
		oldFull := filepath.Join(oldDir, filepath.FromSlash(rel))
		newFull := filepath.Join(opts.Root, filepath.FromSlash(storage.RefPath(ref)))

		moved, size, err := movePair(oldFull, newFull, ref, opts.DryRun)
		if err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s/%s: %v", bkt.path, key, err))
			continue
		}
		if moved {
			report.BytesMoved += size
			if versionID != "" {
				report.VersionsMoved++
			} else {
				report.ObjectsMoved++
			}
		}
	}

	if err := reconcileIndex(ctx, opts, bkt, report); err != nil {
		return err
	}

	if opts.DryRun {
		return nil
	}

	// The directories the old layout built out of key components are empty now.
	pruneMovedDirs(oldDir, files, log)

	if err := storage.WriteBucketMarker(opts.Root, bkt.path); err != nil {
		return fmt.Errorf("%s: could not write the bucket marker: %w", bkt.path, err)
	}
	for _, stale := range []string{".maxiofs-bucket.metadata", ".maxiofs-folder"} {
		os.Remove(filepath.Join(newDir, stale)) //nolint:errcheck
	}
	if oldDir != newDir {
		pruneEmpty(oldDir, opts.Root, log)
	}
	return nil
}

// reconcileIndex checks the index against the tree the move produced. It scans
// the raw entries rather than listing objects, so folder markers and delete
// markers are seen too. Folder markers had no data file in the old layout and
// get one here; anything else missing is reported and left alone.
func reconcileIndex(ctx context.Context, opts Options, bkt bucketRef, report *Report) error {
	raw, ok := opts.Store.(metadata.RawKVStore)
	if !ok {
		return nil
	}

	for _, prefix := range []string{"obj:" + bkt.path + ":", "version:" + bkt.path + ":"} {
		var scanErr error
		err := raw.RawScan(ctx, prefix, "", func(_ string, val []byte) bool {
			var obj metadata.ObjectMetadata
			if json.Unmarshal(val, &obj) != nil || obj.Key == "" {
				return true
			}
			if err := reconcileEntry(opts, bkt, obj, report); err != nil {
				scanErr = err
				return false
			}
			return true
		})
		if err != nil {
			return fmt.Errorf("%s: scanning the index failed: %w", bkt.path, err)
		}
		if scanErr != nil {
			return scanErr
		}
	}
	return nil
}

func reconcileEntry(opts Options, bkt bucketRef, obj metadata.ObjectMetadata, report *Report) error {
	// A folder the client never asked for. Giving it a file would turn an entry
	// nobody sees into a real object; purgeImplicitFolders takes it out instead.
	if isImplicitFolder(&obj) {
		return nil
	}

	ref := storage.ObjectRef{Bucket: bkt.path, Key: obj.Key, VersionID: obj.VersionID}

	newFull := filepath.Join(opts.Root, filepath.FromSlash(storage.RefPath(ref)))
	if _, err := os.Stat(newFull); err == nil {
		return nil
	}

	// Still where it was: a dry run has moved nothing, and a real run that
	// could not move it has already recorded the failure.
	oldPath := filepath.Join(opts.Root, filepath.FromSlash(v1Path(ref)))
	if info, err := os.Stat(oldPath); err == nil && !info.IsDir() {
		return nil
	}

	// A folder-marker key owns a directory in the old layout by definition, so
	// it is checked before the damage check below.
	if strings.HasSuffix(ref.Key, "/") {
		if err := createFolderMarker(newFull, ref, opts.DryRun); err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s/%s: %v", bkt.path, ref.Key, err))
			return nil
		}
		report.MarkersCreated++
		return nil
	}

	if info, err := os.Stat(oldPath); err == nil && info.IsDir() {
		report.Damaged = append(report.Damaged, fmt.Sprintf("%s/%s: a directory occupies the path of the object", bkt.path, ref.Key))
		return nil
	}

	if obj.ETag == "" {
		return nil // delete marker: a tombstone, never had data
	}

	report.MissingData = append(report.MissingData, fmt.Sprintf("%s/%s", bkt.path, refName(ref)))
	return nil
}

func refName(ref storage.ObjectRef) string {
	if ref.VersionID == "" {
		return ref.Key
	}
	return ref.Key + "@" + ref.VersionID
}

// dropFolderMarkers removes the folder markers the previous layout sprinkled
// through the internal tree. Nothing creates or reads them any more.
func dropFolderMarkers(dir string) {
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == ".maxiofs-folder" {
			os.Remove(p) //nolint:errcheck
		}
		return nil
	})
}

// isImplicitFolder reports an index entry that earlier releases wrote for every
// parent prefix of an uploaded key.
func isImplicitFolder(obj *metadata.ObjectMetadata) bool {
	return strings.HasSuffix(obj.Key, "/") && obj.Size == 0 &&
		obj.ContentType == "application/x-directory" &&
		obj.Metadata != nil && obj.Metadata["x-maxiofs-implicit-folder"] == "true"
}

// purgeImplicitFolders drops those entries. They are invisible to clients only
// because every listing filters them; removing them is what lets the filters go.
func purgeImplicitFolders(ctx context.Context, opts Options, buckets []bucketRef, report *Report) {
	raw, ok := opts.Store.(metadata.RawKVStore)
	if !ok {
		return
	}

	for _, bkt := range buckets {
		var doomed []string
		err := raw.RawScan(ctx, "obj:"+bkt.path+":", "", func(key string, val []byte) bool {
			var obj metadata.ObjectMetadata
			if json.Unmarshal(val, &obj) == nil && isImplicitFolder(&obj) {
				doomed = append(doomed, key)
			}
			return true
		})
		if err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: scanning for implicit folders failed: %v", bkt.path, err))
			continue
		}
		report.FoldersPurged += len(doomed)
		if len(doomed) == 0 || opts.DryRun {
			continue
		}
		if err := raw.RawBatch(ctx, nil, doomed); err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: removing implicit folders failed: %v", bkt.path, err))
		}
	}
}
