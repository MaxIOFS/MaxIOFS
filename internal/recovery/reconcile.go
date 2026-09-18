package recovery

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// ReconcileReport summarises one reconciliation pass.
type ReconcileReport struct {
	Buckets          int
	FilesScanned     int
	EntriesRestored  int // data on disk, Pebble entry rebuilt
	VersionsRestored int
	EntriesRepaired  int // entry present but describing different bytes
	Failures         []string
}

// Changed reports whether the pass modified anything.
func (r *ReconcileReport) Changed() bool {
	return r.EntriesRestored > 0 || r.VersionsRestored > 0 || r.EntriesRepaired > 0
}

// reconcileThrottle: yield briefly every N files so a post-crash boot does
// not monopolise disk IO on large deployments (same pacing as the integrity
// scrubber).
const (
	reconcileBatchSize = 500
	reconcileBatchRest = 10 * time.Millisecond
)

// Reconcile repairs a live metadata store against the on-disk object tree.
// Safe to run while the node serves traffic; ctx cancellation stops between
// files and returns the partial report.
func Reconcile(ctx context.Context, dataDir string, store metadata.Store, logger *logrus.Logger) (*ReconcileReport, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	report := &ReconcileReport{}

	objectsRoot := filepath.Join(dataDir, "objects")
	if info, err := os.Stat(objectsRoot); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("objects directory not found at %s", objectsRoot)
	}

	layoutVersion, _, err := storage.ReadLayoutVersion(objectsRoot)
	if err != nil {
		return nil, err
	}
	buckets, err := discoverBuckets(objectsRoot)
	if err != nil {
		return nil, err
	}
	report.Buckets = len(buckets)

	for _, bkt := range buckets {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if _, err := store.GetBucket(ctx, bkt.tenantID, bkt.name); err != nil {
			report.Failures = append(report.Failures,
				fmt.Sprintf("bucket %s: not in metadata store — skipped (use `maxiofs recover` if this bucket should exist)", bkt.bucketPath))
			continue
		}

		changed, err := reconcileBucket(ctx, objectsRoot, layoutVersion > 1, bkt, store, report, logger)
		if err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("bucket %s: %v", bkt.bucketPath, err))
			continue
		}
		if changed {
			if err := store.RecalculateBucketStats(ctx, bkt.tenantID, bkt.name); err != nil {
				report.Failures = append(report.Failures, fmt.Sprintf("recalculate stats %s: %v", bkt.bucketPath, err))
			}
		}
	}

	return report, nil
}

// repairEntryAgainstSidecar corrects an index entry that no longer describes
// the object on disk — an overwrite interrupted between publishing the bytes
// and committing the entry, which otherwise leaves a GET announcing the old
// size and ETag over the new content.
//
// It rewrites size, ETag and last-modified only, on the live path only, and
// only when the sidecar states an identity of its own. It never deletes an
// entry and never touches a file.
func repairEntryAgainstSidecar(ctx context.Context, store metadata.Store, entry *metadata.ObjectMetadata,
	bucketPath, key, versionID string, sidecar map[string]string, report *ReconcileReport, logger *logrus.Logger) bool {
	if entry == nil || sidecar == nil || versionID != "" || entry.VersionID != "" {
		// A version is written to its own path and never overwritten in place;
		// without a sidecar there is nothing trustworthy to compare against.
		return false
	}
	if entry.Size == 0 && entry.ETag == "" {
		return false // delete marker: no bytes of its own to describe
	}
	sizeKey := "size"
	if sidecar["encrypted"] == "true" {
		sizeKey = "original-size"
	}
	if _, ok := sidecar[sizeKey]; !ok {
		return false // sidecar predates stored sizes — nothing to compare
	}
	if size, err := strconv.ParseInt(sidecar[sizeKey], 10, 64); err != nil || size < 0 {
		report.Failures = append(report.Failures, fmt.Sprintf("invalid stored size for %s/%s", bucketPath, key))
		return false
	}

	size, etag := storage.SidecarIdentity(sidecar)
	storedAt := time.Time{}
	if lm, err := strconv.ParseInt(sidecar["last_modified"], 10, 64); err == nil && lm > 0 {
		storedAt = time.Unix(lm, 0)
	}

	switch {
	case entry.Size != size:
		// The entry cannot describe these bytes at all: a GET would announce
		// one length and deliver another.
	case etagsDisagree(entry.ETag, etag) && !storedAt.Before(entry.LastModified.Truncate(time.Second)):
		// The key lock excludes a writer; second-resolution timestamps cannot
		// distinguish consecutive overwrites of the same size.
	default:
		return false
	}

	repaired := *entry
	repaired.Size = size
	if etag != "" {
		repaired.ETag = etag
	}
	if !storedAt.IsZero() {
		repaired.LastModified = storedAt
	}
	if err := store.PutObject(ctx, &repaired); err != nil {
		report.Failures = append(report.Failures, fmt.Sprintf("repair %s/%s: %v", bucketPath, key, err))
		return false
	}

	report.EntriesRepaired++
	logger.WithFields(logrus.Fields{
		"bucket": bucketPath, "key": key,
		"entry_size": entry.Size, "stored_size": size,
		"entry_etag": entry.ETag, "stored_etag": etag,
	}).Warn("Reconcile: entry did not describe the stored object — corrected from the sidecar")
	return true
}

// etagsDisagree reports a real disagreement between two ETags. A multipart ETag
// ("<md5>-<parts>") and a whole-object MD5 describe the same bytes in different
// ways — objects stored before the multipart ETag was written to the sidecar
// carry exactly that mix — so a shape difference alone proves nothing.
func etagsDisagree(entryETag, sidecarETag string) bool {
	if entryETag == "" || sidecarETag == "" {
		return false
	}
	entryETag = strings.Trim(entryETag, "\"")
	sidecarETag = strings.Trim(sidecarETag, "\"")
	if isMultipartETag(entryETag) != isMultipartETag(sidecarETag) {
		return false
	}
	return !strings.EqualFold(entryETag, sidecarETag)
}

// isMultipartETag reports the "<md5>-<parts>" shape S3 gives an object
// assembled from parts. Only a trailing "-<digits>" counts: an ETag may carry
// hyphens for other reasons.
func isMultipartETag(etag string) bool {
	dash := strings.LastIndex(etag, "-")
	if dash <= 0 || dash == len(etag)-1 {
		return false
	}
	for _, r := range etag[dash+1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// reconcileBucket walks one bucket root in the disk→store direction only.
// It never prunes store metadata or sidecars based on filesystem absence.
func reconcileBucket(ctx context.Context, objectsRoot string, hashedKeys bool, bkt *bucketEntry, store metadata.Store, report *ReconcileReport, logger *logrus.Logger) (bool, error) {
	changed := false

	walkErr := filepath.WalkDir(bkt.dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if cErr := ctx.Err(); cErr != nil {
			return cErr
		}
		if d.IsDir() {
			return nil
		}

		report.FilesScanned++
		if report.FilesScanned%reconcileBatchSize == 0 {
			time.Sleep(reconcileBatchRest)
		}

		name := d.Name()
		switch {
		case name == ".maxiofs-bucket" || name == ".maxiofs-folder",
			strings.HasSuffix(name, ".metadata-staging"),
			strings.HasPrefix(name, ".tmp_"),
			strings.HasPrefix(name, ".metadata-tmp-"),
			strings.HasPrefix(name, "maxiofs-upload-"),
			strings.HasPrefix(name, "maxiofs-encmigrate"),
			strings.HasPrefix(name, "maxiofs-multipart-"):
			return nil
		case strings.HasSuffix(name, ".metadata"):
			return nil
		}

		sidecar, _ := readSidecar(path)

		bucketPath, key, versionID := identityFromSidecar(sidecar, bkt.bucketPath)
		if key == "" {
			if hashedKeys {
				if _, err := os.Stat(path + ".metadata-staging"); err != nil {
					report.Failures = append(report.Failures, fmt.Sprintf("%s: missing sidecar identity; hashed object cannot be indexed", path))
				}
				return nil
			}
			var ok bool
			if key, versionID, ok = keyFromRelPath(bkt.dirPath, path); !ok {
				return nil
			}
		}

		defer storage.LockObject(objectsRoot, bucketPath, key)()
		if _, err := os.Stat(path); err != nil {
			if !os.IsNotExist(err) {
				report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", path, err))
			}
			return nil
		}
		entry, gErr := store.GetObject(ctx, bucketPath, key, versionID)
		sidecar, _ = readSidecar(path)
		if gErr == nil {
			// Entry present: authoritative for everything the disk does not
			// know, but it cannot outvote the bytes a GET actually serves.
			if repairEntryAgainstSidecar(ctx, store, entry, bucketPath, key, versionID, sidecar, report, logger) {
				changed = true
			}
			return nil
		}
		if gErr != metadata.ErrObjectNotFound && gErr != metadata.ErrVersionNotFound {
			report.Failures = append(report.Failures, fmt.Sprintf("%s/%s: %v", bucketPath, key, gErr))
			return nil
		}

		// Entry missing. Re-stat before restoring: a concurrent DELETE may
		// have removed the file between the walk seeing it and now.
		if _, sErr := os.Stat(path); sErr != nil {
			return nil
		}

		obj, _, oErr := objectFromSidecar(path, bucketPath, key, versionID, sidecar, nil, nil)
		if obj == nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s/%s: %v", bucketPath, key, oErr))
			return nil
		}

		if versionID != "" {
			existing, vErr := store.GetObjectVersions(ctx, bucketPath, key)
			if vErr != nil && vErr != metadata.ErrObjectNotFound {
				report.Failures = append(report.Failures, fmt.Sprintf("%s/%s@%s: %v", bucketPath, key, versionID, vErr))
				return nil
			}
			// Version IDs are nanosecond-timestamp-prefixed: lexicographic
			// order is chronological. Newest-first listing → index 0.
			isLatest := len(existing) == 0 || existing[0].VersionID < versionID
			version := &metadata.ObjectVersion{
				VersionID:    versionID,
				IsLatest:     isLatest,
				Key:          key,
				Size:         obj.Size,
				ETag:         obj.ETag,
				LastModified: obj.LastModified,
				StorageClass: obj.StorageClass,
			}
			if pErr := store.PutObjectVersion(ctx, obj, version); pErr != nil {
				report.Failures = append(report.Failures, fmt.Sprintf("restore version %s/%s@%s: %v", bucketPath, key, versionID, pErr))
				return nil
			}
			report.VersionsRestored++
		} else {
			if pErr := store.PutObject(ctx, obj); pErr != nil {
				report.Failures = append(report.Failures, fmt.Sprintf("restore %s/%s: %v", bucketPath, key, pErr))
				return nil
			}
			report.EntriesRestored++
		}
		changed = true
		logger.WithFields(logrus.Fields{
			"bucket": bucketPath, "key": key, "version": versionID,
		}).Info("Reconcile: restored metadata entry lost in unclean shutdown")
		return nil
	})
	if walkErr != nil {
		return changed, walkErr
	}

	return changed, nil
}

// keyFromRelPath converts an absolute file path under a bucket root into an
// object key (and version ID for files under .versions/).
func keyFromRelPath(bucketDir, path string) (key, versionID string, ok bool) {
	rel, err := filepath.Rel(bucketDir, path)
	if err != nil {
		return "", "", false
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, ".versions/") {
		trimmed := strings.TrimPrefix(rel, ".versions/")
		slash := strings.LastIndex(trimmed, "/")
		if slash <= 0 {
			return "", "", false
		}
		return trimmed[:slash], trimmed[slash+1:], true
	}
	return rel, "", true
}
