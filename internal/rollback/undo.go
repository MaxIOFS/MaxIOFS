package rollback

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// Report summarises one undo pass.
type Report struct {
	ObjectsRestored int // interrupted overwrite undone
	PartsRestored   int // interrupted part replacement undone
	Committed       int // the write did reach the index: nothing to undo
	Discarded       int // retained copies that describe nothing the index holds
	Retained        int // no authoritative entry: keep the copy without restoring it
	Failures        []string
}

// Changed reports whether the pass put any data back.
func (r *Report) Changed() bool {
	return r.ObjectsRestored > 0 || r.PartsRestored > 0
}

// Undo puts back every in-place write that was interrupted before its index
// entry was committed, reading the copies retained next to the object tree.
//
// The decision per retained copy is: does the index still hold what it held
// when the copy was taken? Then the write never landed and the copy goes back.
// Does it hold something else? Then the write did land and the copy is stale.
// Anything undecidable is left untouched and reported.
//
// Restore the oldest copy matching the current entry. Obsolete copies cannot
// settle a destination, and a failed restore must not fall back to a later copy.
//
// Run it with the object tree quiescent — at start-up, before anything serves
// traffic — because it writes over live object paths.
func Undo(ctx context.Context, root string, backend storage.Backend, store metadata.Store, logger *logrus.Logger) (*Report, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	report := &Report{}

	objectManifests, err := Manifests(root, ObjectPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list retained object copies: %w", err)
	}
	settled := make(map[string]bool, len(objectManifests))
	blocked := make(map[string]bool)
	for _, manifestPath := range objectManifests {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		manifest, err := ReadObjectManifest(manifestPath)
		if err != nil {
			report.fail("retained copy %s: %v (left in place)", manifestPath, err)
			continue
		}
		dest := strings.Join([]string{manifest.Ref.Bucket, manifest.Ref.Key, manifest.Ref.VersionID}, "\x00")
		if blocked[dest] {
			report.Retained++
			continue
		}
		if settled[dest] {
			// An older copy already decided this object: this one was taken
			// from bytes no client was ever told about.
			report.Discarded++
			discard(manifestPath, DataPath(manifestPath), report, logger)
			continue
		}
		restored := report.ObjectsRestored
		failures := len(report.Failures)
		if undoObject(ctx, manifestPath, manifest, backend, store, report, logger) && report.ObjectsRestored > restored {
			settled[dest] = true
		}
		if len(report.Failures) > failures {
			blocked[dest] = true
		}
	}

	partManifests, err := Manifests(root, PartPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list retained part copies: %w", err)
	}
	settledParts := make(map[string]bool, len(partManifests))
	blockedParts := make(map[string]bool)
	for _, manifestPath := range partManifests {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		manifest, err := ReadPartManifest(manifestPath)
		if err != nil {
			report.fail("retained copy %s: %v (left in place)", manifestPath, err)
			continue
		}
		dest := fmt.Sprintf("%s\x00%d", manifest.UploadID, manifest.PartNumber)
		if blockedParts[dest] {
			report.Retained++
			continue
		}
		if settledParts[dest] {
			report.Discarded++
			discard(manifestPath, DataPath(manifestPath), report, logger)
			continue
		}
		restored := report.PartsRestored
		failures := len(report.Failures)
		if undoPart(ctx, manifestPath, manifest, backend, store, report, logger) && report.PartsRestored > restored {
			settledParts[dest] = true
		}
		if len(report.Failures) > failures {
			blockedParts[dest] = true
		}
	}

	// A copy whose manifest never made it to disk names nothing and restores
	// nothing: the write it belonged to had not touched the object yet.
	for _, prefix := range []string{ObjectPrefix, PartPrefix} {
		sweepUnidentifiedCopies(root, prefix, report, logger)
	}

	return report, nil
}

// undoObject settles one retained object copy. It reports whether the
// destination was decided — false leaves both files on disk for an operator.
func undoObject(ctx context.Context, manifestPath string, manifest *ObjectManifest,
	backend storage.Backend, store metadata.Store, report *Report, logger *logrus.Logger) bool {
	dataPath := DataPath(manifestPath)
	if _, err := os.Stat(dataPath); err != nil {
		if !os.IsNotExist(err) {
			report.fail("retained copy %s: %v", dataPath, err)
			return false
		}
		// Manifest without its copy: the restore already ran, or the copy was
		// never completed. Either way there is nothing left to put back.
		discard(manifestPath, "", report, logger)
		return false
	}

	entry, err := store.GetObject(ctx, manifest.Ref.Bucket, manifest.Ref.Key, manifest.Ref.VersionID)
	switch err {
	case nil:
		if !manifest.matchesCommitted(entry) {
			// The index moved on: the interrupted write did commit.
			report.Committed++
			discard(manifestPath, dataPath, report, logger)
			return true
		}
	case metadata.ErrObjectNotFound, metadata.ErrVersionNotFound:
		// Absence also follows an acknowledged DELETE. Never infer a lost
		// commit from it or discard the only recoverable copy.
		report.Retained++
		logger.WithField("manifest", manifestPath).Warn("Rollback: no index entry; retained copy requires operator review")
		return false
	default:
		report.fail("%s/%s: %v (retained copy left in place)", manifest.Ref.Bucket, manifest.Ref.Key, err)
		return false
	}

	backup, err := os.Open(dataPath)
	if err != nil {
		report.fail("retained copy %s: %v", dataPath, err)
		return false
	}
	// Closed before the copy is discarded: an open handle blocks removal on
	// Windows.
	err = backend.Put(ctx, manifest.Ref, backup, manifest.Metadata)
	backup.Close()
	if err != nil {
		report.fail("restore %s/%s: %v (retained copy left at %s)", manifest.Ref.Bucket, manifest.Ref.Key, err, dataPath)
		return false
	}

	report.ObjectsRestored++
	logger.WithFields(logrus.Fields{
		"bucket": manifest.Ref.Bucket, "key": manifest.Ref.Key, "version": manifest.Ref.VersionID,
	}).Warn("Rollback: undid an overwrite that was interrupted before it was committed")
	discard(manifestPath, dataPath, report, logger)
	return true
}

func undoPart(ctx context.Context, manifestPath string, manifest *PartManifest,
	backend storage.Backend, store metadata.Store, report *Report, logger *logrus.Logger) bool {
	dataPath := DataPath(manifestPath)
	if _, err := os.Stat(dataPath); err != nil {
		if !os.IsNotExist(err) {
			report.fail("retained copy %s: %v", dataPath, err)
			return false
		}
		discard(manifestPath, "", report, logger)
		return false
	}

	if _, err := store.GetMultipartUpload(ctx, manifest.UploadID); err != nil {
		if err == metadata.ErrUploadNotFound {
			// The upload was completed or aborted: a restored part would be a
			// file nothing points at.
			report.Committed++
			discard(manifestPath, dataPath, report, logger)
			return true
		}
		report.fail("upload %s: %v (retained copy left in place)", manifest.UploadID, err)
		return false
	}

	row, err := store.GetPart(ctx, manifest.UploadID, manifest.PartNumber)
	switch err {
	case nil:
		if !manifest.matchesCommitted(row) {
			report.Committed++
			discard(manifestPath, dataPath, report, logger)
			return true
		}
	case metadata.ErrPartNotFound:
		// No row promises these bytes to anyone.
		report.Committed++
		discard(manifestPath, dataPath, report, logger)
		return true
	default:
		report.fail("upload %s part %d: %v (retained copy left in place)", manifest.UploadID, manifest.PartNumber, err)
		return false
	}

	backup, err := os.Open(dataPath)
	if err != nil {
		report.fail("retained copy %s: %v", dataPath, err)
		return false
	}
	err = backend.PutPart(ctx, manifest.UploadID, manifest.PartNumber, backup, manifest.Metadata)
	backup.Close()
	if err != nil {
		report.fail("restore upload %s part %d: %v (retained copy left at %s)", manifest.UploadID, manifest.PartNumber, err, dataPath)
		return false
	}

	report.PartsRestored++
	logger.WithFields(logrus.Fields{
		"uploadID": manifest.UploadID, "part": manifest.PartNumber,
	}).Warn("Rollback: undid a part replacement that was interrupted before it was committed")
	discard(manifestPath, dataPath, report, logger)
	return true
}

// matchesCommitted reports whether the index still holds what it held when the
// copy was retained — that is, whether the interrupted write never reached it.
//
// The recorded identity comes from the index itself, so the comparison is exact.
// Only copies retained before that was recorded fall back to comparing the
// stored identity, where size decides and ETags only weigh in when both sides
// express the identity the same way (a multipart ETag and a whole-object MD5 can
// describe the very same bytes).
func (m *ObjectManifest) matchesCommitted(entry *metadata.ObjectMetadata) bool {
	if entry == nil {
		return true // the entry is gone too: nothing moved on from this copy
	}
	if m.Committed != nil {
		return entry.Size == m.Committed.Size && sameETag(entry.ETag, m.Committed.ETag)
	}
	size, etag := storage.SidecarIdentity(m.Metadata)
	return entry.Size == size && !etagsDisagree(entry.ETag, etag)
}

func (m *PartManifest) matchesCommitted(row *metadata.PartMetadata) bool {
	if row == nil {
		return true
	}
	if m.Committed != nil {
		return row.Size == m.Committed.Size && sameETag(row.ETag, m.Committed.ETag)
	}
	size, _ := strconv.ParseInt(m.Metadata["size"], 10, 64)
	return row.Size == size && !etagsDisagree(row.ETag, m.Metadata["etag"])
}

func sameETag(a, b string) bool {
	return strings.EqualFold(strings.Trim(a, "\""), strings.Trim(b, "\""))
}

// etagsDisagree reports a real disagreement. A shape difference alone proves
// nothing: a multipart ETag and a whole-object MD5 can describe the same bytes.
func etagsDisagree(committed, retained string) bool {
	if committed == "" || retained == "" {
		return false
	}
	committed = strings.Trim(committed, "\"")
	retained = strings.Trim(retained, "\"")
	if isMultipartETag(committed) != isMultipartETag(retained) {
		return false
	}
	return !strings.EqualFold(committed, retained)
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

// sweepUnidentifiedCopies removes retained copies that have no manifest. The
// manifest is written after the copy, so one without the other belongs to a
// write that had not reached the object yet.
func sweepUnidentifiedCopies(root, prefix string, report *Report, logger *logrus.Logger) {
	matches, err := filepath.Glob(filepath.Join(root, prefix+"*"))
	if err != nil {
		report.fail("failed to list retained copies: %v", err)
		return
	}
	for _, path := range matches {
		if strings.HasSuffix(path, ManifestSuffix) {
			continue
		}
		if _, err := os.Stat(path + ManifestSuffix); err == nil {
			continue // handled with its manifest
		} else if !os.IsNotExist(err) {
			report.fail("failed to inspect manifest for %s: %v", path, err)
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			report.fail("failed to remove unidentified copy %s: %v", path, err)
			continue
		}
		report.Discarded++
		logger.WithField("path", path).Info("Rollback: removed a retained copy with no manifest")
	}
}

func discard(manifestPath, dataPath string, report *Report, logger *logrus.Logger) {
	if dataPath != "" {
		if err := os.Remove(dataPath); err != nil && !os.IsNotExist(err) {
			report.fail("failed to remove retained copy %s: %v", dataPath, err)
			return
		}
	}
	if err := os.Remove(manifestPath); err != nil && !os.IsNotExist(err) {
		report.fail("failed to remove manifest %s: %v", manifestPath, err)
		return
	}
	logger.WithField("manifest", manifestPath).Debug("Rollback: retained copy no longer needed")
}

func (r *Report) fail(format string, args ...any) {
	r.Failures = append(r.Failures, fmt.Sprintf(format, args...))
}
