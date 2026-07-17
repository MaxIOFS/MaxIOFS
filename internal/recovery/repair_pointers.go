package recovery

// RepairLatestPointers rebuilds the "latest object" pointers (obj:bucket:key)
// that were wrongly deleted by the pre-fix reconcile ghost-removal, using the
// surviving per-version entries (version:bucket:key:versionID).
//
// Why this works: a versioned object stores its bytes under .versions/ and is
// tracked by TWO metadata keys — the latest pointer obj:bucket:key and one
// version:bucket:key:versionID per version. The faulty reconcile deleted only
// the obj: pointer (DeleteObject with no versionID), never the version: keys,
// which is a SEPARATE keyspace and carries the full ObjectMetadata including
// Retention/LegalHold. So every deleted pointer can be reconstructed byte-for-
// byte from its latest surviving version, with Object Lock intact.
//
// This operation is strictly ADDITIVE: it only writes obj: keys that are
// currently MISSING, copying from an existing version entry. It never deletes,
// never modifies a version entry, and never touches the filesystem. Run it
// with the server stopped, against the live (damaged) metadata store.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// RepairReport summarises a pointer-repair run.
type RepairReport struct {
	VersionKeysScanned int
	DistinctObjects    int
	PointersPresent    int // obj: pointer already existed — left untouched
	PointersRebuilt    int // obj: pointer was missing — rebuilt from latest version
	DeleteMarkerLatest int // latest surviving version is a delete marker (pointer left absent)
	Failures           []string
}

// versionRecord is one surviving version entry for a key.
type versionRecord struct {
	versionID string
	raw       []byte
	meta      metadata.ObjectMetadata
}

// RepairLatestPointers scans version: entries and rebuilds any missing obj:
// pointer from the latest surviving version. dryRun reports without writing.
func RepairLatestPointers(dataDir string, dryRun bool, logger *logrus.Logger) (*RepairReport, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	report := &RepairReport{}
	ctx := context.Background()

	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: dataDir,
		Logger:  logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata store at %s: %w", dataDir, err)
	}
	defer store.Close() //nolint:errcheck

	// Group surviving version entries by their object key (bucket + key). We
	// scan the whole version: keyspace once; keys arrive in sorted order so all
	// versions of a key are contiguous — we flush each group as the key changes.
	var curBucket, curKey string
	var group []versionRecord

	flush := func() {
		if len(group) == 0 {
			return
		}
		report.DistinctObjects++
		if repErr := repairOne(ctx, store, curBucket, curKey, group, dryRun, report, logger); repErr != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s/%s: %v", curBucket, curKey, repErr))
		}
		group = group[:0]
	}

	scanErr := store.RawScan(ctx, "version:", "", func(rawKey string, val []byte) bool {
		report.VersionKeysScanned++
		bucket, key, versionID, ok := parseVersionKey(rawKey)
		if !ok {
			report.Failures = append(report.Failures, fmt.Sprintf("unparseable version key: %q", rawKey))
			return true
		}

		if bucket != curBucket || key != curKey {
			flush()
			curBucket, curKey = bucket, key
		}

		rec := versionRecord{versionID: versionID, raw: append([]byte(nil), val...)}
		if err := json.Unmarshal(val, &rec.meta); err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s/%s@%s: bad version JSON: %v", bucket, key, versionID, err))
			return true
		}
		group = append(group, rec)
		return true
	})
	flush()
	if scanErr != nil {
		return report, fmt.Errorf("version keyspace scan failed: %w", scanErr)
	}

	return report, nil
}

// repairOne rebuilds the obj: pointer for one key if it is missing.
func repairOne(ctx context.Context, store *metadata.PebbleStore, bucket, key string, group []versionRecord, dryRun bool, report *RepairReport, logger *logrus.Logger) error {
	objKey := fmt.Sprintf("obj:%s:%s", bucket, key)

	// Pointer already present → authoritative, leave it alone.
	if _, err := store.GetRaw(ctx, objKey); err == nil {
		report.PointersPresent++
		return nil
	} else if err != metadata.ErrNotFound {
		return fmt.Errorf("probe obj pointer: %w", err)
	}

	// Pick the latest surviving version: prefer the one flagged IsLatest; if
	// none (the flip was lost) or several, fall back to the largest versionID
	// (version IDs are nanosecond-timestamp-prefixed → lexicographically sortable).
	latest := pickLatest(group)
	if latest == nil {
		return fmt.Errorf("no usable version among %d entries", len(group))
	}

	// A delete marker as latest means the object was logically deleted; do NOT
	// resurrect it with a pointer. (Immutable Veeam objects have no delete
	// markers, so this branch simply never fires for them.)
	if latest.meta.ETag == "" && latest.meta.Size == 0 {
		report.DeleteMarkerLatest++
		return nil
	}

	// Rebuild the pointer: the obj: value is byte-identical to the latest
	// version entry with IsLatest=true (that is exactly how PutObjectVersion
	// writes both keys). Copy the version JSON, force IsLatest=true.
	rebuilt := latest.meta
	rebuilt.IsLatest = true
	rebuilt.Bucket = bucket
	rebuilt.Key = key
	rebuilt.VersionID = latest.versionID
	data, err := json.Marshal(&rebuilt)
	if err != nil {
		return fmt.Errorf("marshal rebuilt pointer: %w", err)
	}

	if dryRun {
		report.PointersRebuilt++
		return nil
	}
	if err := store.PutRaw(ctx, objKey, data); err != nil {
		return fmt.Errorf("write rebuilt pointer: %w", err)
	}
	report.PointersRebuilt++
	logger.WithFields(logrus.Fields{
		"bucket": bucket, "key": key, "version": latest.versionID,
	}).Info("Repair: rebuilt latest pointer from surviving version")
	return nil
}

// pickLatest returns the authoritative latest version of a key.
func pickLatest(group []versionRecord) *versionRecord {
	var flagged []int
	for i := range group {
		if group[i].meta.IsLatest {
			flagged = append(flagged, i)
		}
	}
	if len(flagged) == 1 {
		return &group[flagged[0]]
	}
	// Zero or multiple IsLatest flags (crash lost a flip): use max versionID.
	best := -1
	for i := range group {
		if best == -1 || group[i].versionID > group[best].versionID {
			best = i
		}
	}
	if best == -1 {
		return nil
	}
	return &group[best]
}

// parseVersionKey splits "version:{bucket}:{key}:{versionID}".
// bucket has no colon (S3 naming); versionID has no colon; the object key may
// contain colons, so bucket is taken up to the FIRST colon and versionID from
// the LAST colon, leaving the (possibly colon-containing) key in the middle.
func parseVersionKey(rawKey string) (bucket, key, versionID string, ok bool) {
	rest, found := strings.CutPrefix(rawKey, "version:")
	if !found {
		return "", "", "", false
	}
	firstColon := strings.IndexByte(rest, ':')
	lastColon := strings.LastIndexByte(rest, ':')
	if firstColon < 0 || lastColon <= firstColon {
		return "", "", "", false
	}
	bucket = rest[:firstColon]
	key = rest[firstColon+1 : lastColon]
	versionID = rest[lastColon+1:]
	if bucket == "" || key == "" || versionID == "" {
		return "", "", "", false
	}
	return bucket, key, versionID, true
}
