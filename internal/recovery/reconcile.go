package recovery

// Online reconciliation after an unclean shutdown.
//
// Hot-path Pebble commits are NoSync (fsynced within ~1s by the store's WAL
// sync loop), so a hard kill can lose the last moments of metadata writes
// while the object files and their sidecars survived on disk. Reconcile walks
// the object tree against a LIVE store and repairs only in the non-destructive
// direction:
//
//   - data file + sidecar present, Pebble entry missing → the entry is
//     rebuilt from the sidecar (a PUT whose metadata commit was lost).
//
// It deliberately never removes Pebble metadata or sidecars merely because a
// data path is absent. A missing path can mean a storage mount, layout, or
// transient filesystem problem; treating that as deletion authority risks data
// loss, especially for versioned/Object Lock buckets.
//
// It runs in the background on a serving node: GETs already work through the
// sidecar fallback while entries are missing, and listings converge as the
// walk progresses. Same recovery bias as the offline rebuild: when a file
// exists on disk it is indexed — a delete that fsynced its tombstone and
// died before unlinking the file (sub-millisecond window) comes back visible
// rather than risking the loss of a just-written object.
//
// Staged sidecars (*.metadata-staging) are never touched here: the storage
// backend's two-phase-commit repair owns them.

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// ReconcileReport summarises one reconciliation pass.
type ReconcileReport struct {
	Buckets          int
	FilesScanned     int
	EntriesRestored  int // data on disk, Pebble entry rebuilt
	VersionsRestored int
	Failures         []string
}

// Changed reports whether the pass modified anything.
func (r *ReconcileReport) Changed() bool {
	return r.EntriesRestored > 0 || r.VersionsRestored > 0
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
			// A bucket dir without a Pebble bucket entry is beyond the crash
			// window (bucket creation commits are Sync'd) — full-recover
			// territory, not something to silently half-repair here.
			report.Failures = append(report.Failures,
				fmt.Sprintf("bucket %s: not in metadata store — skipped (use `maxiofs recover` if this bucket should exist)", bkt.bucketPath))
			continue
		}

		changed, err := reconcileBucket(ctx, bkt, store, report, logger)
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

// reconcileBucket walks one bucket root in the disk→store direction only.
// It never prunes store metadata or sidecars based on filesystem absence.
func reconcileBucket(ctx context.Context, bkt *bucketEntry, store metadata.Store, report *ReconcileReport, logger *logrus.Logger) (bool, error) {
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

		key, versionID, ok := keyFromRelPath(bkt.dirPath, path)
		if !ok {
			return nil
		}

		_, gErr := store.GetObject(ctx, bkt.bucketPath, key, versionID)
		if gErr == nil {
			return nil // entry present — live store is authoritative
		}
		if gErr != metadata.ErrObjectNotFound && gErr != metadata.ErrVersionNotFound {
			report.Failures = append(report.Failures, fmt.Sprintf("%s/%s: %v", bkt.bucketPath, key, gErr))
			return nil
		}

		// Entry missing. Re-stat before restoring: a concurrent DELETE may
		// have removed the file between the walk seeing it and now.
		if _, sErr := os.Stat(path); sErr != nil {
			return nil
		}

		obj, _, oErr := objectFromSidecar(path, bkt.bucketPath, key, versionID, nil, nil)
		if obj == nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s/%s: %v", bkt.bucketPath, key, oErr))
			return nil
		}

		if versionID != "" {
			existing, vErr := store.GetObjectVersions(ctx, bkt.bucketPath, key)
			if vErr != nil && vErr != metadata.ErrObjectNotFound {
				report.Failures = append(report.Failures, fmt.Sprintf("%s/%s@%s: %v", bkt.bucketPath, key, versionID, vErr))
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
				report.Failures = append(report.Failures, fmt.Sprintf("restore version %s/%s@%s: %v", bkt.bucketPath, key, versionID, pErr))
				return nil
			}
			report.VersionsRestored++
		} else {
			if pErr := store.PutObject(ctx, obj); pErr != nil {
				report.Failures = append(report.Failures, fmt.Sprintf("restore %s/%s: %v", bkt.bucketPath, key, pErr))
				return nil
			}
			report.EntriesRestored++
		}
		changed = true
		logger.WithFields(logrus.Fields{
			"bucket": bkt.bucketPath, "key": key, "version": versionID,
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
