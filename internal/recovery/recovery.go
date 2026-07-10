// Package recovery rebuilds the Pebble metadata store from the filesystem
// object tree — the disaster-recovery path for "metadata DB lost or corrupt,
// object files intact".
//
// Every object on disk carries a .metadata sidecar with everything needed to
// reconstruct its Pebble entry (size, etag, content-type, encryption fields
// including the wrapped DEK). Buckets are identified by their .maxiofs-bucket
// marker (whose sidecar records the owning tenant). Given a KEK recovery
// bundle, the tool also verifies that every envelope object's DEK unwraps and
// restores the keys into the auth SQLite so the recovered server can decrypt.
//
// The rebuild always writes into a FRESH Pebble directory — it never touches
// an existing (possibly corrupt) store.
//
// Known limitation (deliberate, data-recovery bias): delete markers exist
// only in the metadata store, not on the filesystem. A versioned object whose
// latest "version" was a deletion therefore comes back VISIBLE after a
// rebuild — its stored versions are all real files, and the marker that hid
// them is gone. The operator re-deletes if the deletion should stand.
package recovery

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/pkg/encryption"
	"github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// Options configures a recovery run.
type Options struct {
	// DataDir is the MaxIOFS data directory (contains objects/ and db/).
	DataDir string
	// BundlePath is the KEK recovery bundle (optional — required to verify
	// and serve encrypted objects).
	BundlePath string
	// Passphrase opens the bundle.
	Passphrase string
	// OutDB is the directory for the FRESH Pebble store. Empty defaults to
	// <DataDir>/metadata-recovered. Must not already contain a store.
	OutDB string
	// DryRun walks and verifies without writing anything.
	DryRun bool
	// Verbose logs every recovered object.
	Verbose bool
}

// Report summarises a recovery run.
type Report struct {
	Buckets             int
	Objects             int
	Versions            int
	EncryptedVerified   int // envelope objects whose DEK unwrapped with the bundle keys
	EncryptedUnverified int // encrypted objects that could not be verified (no bundle / missing version)
	LegacyEncrypted     int // direct-encrypted objects (no DEK; need KEK v1 at read time)
	Plaintext           int
	Skipped             int // markers, temp files
	Failures            []string
	OutDB               string
	KEKsRestored        int
}

// bucketEntry is one discovered bucket root.
type bucketEntry struct {
	dirPath    string // absolute path of the bucket directory
	bucketPath string // Pebble bucket path: "bucket" or "tenant/bucket"
	name       string
	tenantID   string
	createdAt  time.Time
	versioned  bool
	objects    []*metadata.ObjectMetadata
	versions   map[string][]*metadata.ObjectMetadata // key → versions
	totalSize  int64
}

// Run executes the recovery.
func Run(opts Options) (*Report, error) {
	report := &Report{}

	objectsRoot := filepath.Join(opts.DataDir, "objects")
	if info, err := os.Stat(objectsRoot); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("objects directory not found at %s — is --data-dir correct?", objectsRoot)
	}

	if opts.OutDB == "" {
		opts.OutDB = filepath.Join(opts.DataDir, "metadata-recovered")
	}
	report.OutDB = opts.OutDB
	if !opts.DryRun {
		if entries, err := os.ReadDir(opts.OutDB); err == nil && len(entries) > 0 {
			return nil, fmt.Errorf("output directory %s is not empty — refusing to overwrite (recovery always writes into a fresh store)", opts.OutDB)
		}
	}

	// Open the KEK bundle when provided.
	var keys map[int][]byte
	if opts.BundlePath != "" {
		data, err := os.ReadFile(opts.BundlePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read recovery bundle: %w", err)
		}
		records, err := kek.DecryptBundle(data, opts.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("failed to open recovery bundle: %w", err)
		}
		keys = make(map[int][]byte, len(records))
		for _, r := range records {
			keyBytes, _ := hex.DecodeString(r.KeyHex)
			keys[r.Version] = keyBytes
		}
		logrus.WithField("versions", len(keys)).Info("Recovery bundle opened")
	}

	// Discover bucket roots and walk their objects.
	buckets, err := discoverBuckets(objectsRoot)
	if err != nil {
		return nil, err
	}
	report.Buckets = len(buckets)

	encryptor := encryption.NewAESGCMEncryptor(encryption.DefaultEncryptionConfig())
	for _, bkt := range buckets {
		if err := walkBucket(bkt, encryptor, keys, report, opts.Verbose); err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("bucket %s: %v", bkt.bucketPath, err))
		}
	}

	if opts.DryRun {
		logrus.Info("Dry run — nothing written")
		return report, nil
	}

	// Rebuild the fresh Pebble store.
	if err := rebuildPebble(opts.OutDB, buckets, report); err != nil {
		return nil, err
	}

	// Restore the encryption keys into the auth SQLite so the recovered
	// server can decrypt (bootstrap will load them instead of generating).
	if len(keys) > 0 {
		restored, err := restoreKEKs(filepath.Join(opts.DataDir, "db", "maxiofs.db"), opts.BundlePath, opts.Passphrase)
		if err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("KEK restore: %v", err))
		} else {
			report.KEKsRestored = restored
		}
	}

	return report, nil
}

// discoverBuckets finds every directory carrying a .maxiofs-bucket marker
// (global buckets at depth 1, tenant buckets at depth 2).
func discoverBuckets(objectsRoot string) ([]*bucketEntry, error) {
	var buckets []*bucketEntry

	appendIfBucket := func(dirPath, tenantHint string) (bool, error) {
		markerPath := filepath.Join(dirPath, ".maxiofs-bucket")
		if _, err := os.Stat(markerPath); err != nil {
			return false, nil
		}

		name := filepath.Base(dirPath)
		tenantID := tenantHint
		createdAt := time.Now()

		// The marker sidecar records the owning tenant and creation time.
		if sidecar, err := readSidecar(markerPath); err == nil {
			if tid, ok := sidecar["tenant-id"]; ok {
				tenantID = tid
			}
			if created, ok := sidecar["bucket-created"]; ok {
				if ts, err := time.Parse(time.RFC3339, created); err == nil {
					createdAt = ts
				}
			}
		}

		bucketPath := name
		if tenantID != "" {
			bucketPath = tenantID + "/" + name
		}
		buckets = append(buckets, &bucketEntry{
			dirPath:    dirPath,
			bucketPath: bucketPath,
			name:       name,
			tenantID:   tenantID,
			createdAt:  createdAt,
			versions:   make(map[string][]*metadata.ObjectMetadata),
		})
		return true, nil
	}

	topEntries, err := os.ReadDir(objectsRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to read objects directory: %w", err)
	}
	for _, top := range topEntries {
		if !top.IsDir() || strings.HasPrefix(top.Name(), ".") {
			continue // .maxiofs (multipart temp) and stray files
		}
		topPath := filepath.Join(objectsRoot, top.Name())
		isBucket, err := appendIfBucket(topPath, "")
		if err != nil {
			return nil, err
		}
		if isBucket {
			continue
		}
		// Not a bucket → tenant directory: its subdirectories are buckets.
		subEntries, err := os.ReadDir(topPath)
		if err != nil {
			continue
		}
		for _, sub := range subEntries {
			if !sub.IsDir() || strings.HasPrefix(sub.Name(), ".") {
				continue
			}
			if _, err := appendIfBucket(filepath.Join(topPath, sub.Name()), top.Name()); err != nil {
				return nil, err
			}
		}
	}
	return buckets, nil
}

// walkBucket collects every object and version under one bucket root.
func walkBucket(bkt *bucketEntry, encryptor encryption.Encryptor, keys map[int][]byte, report *Report, verbose bool) error {
	return filepath.WalkDir(bkt.dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		// Markers, sidecars and staging leftovers.
		if name == ".maxiofs-bucket" || name == ".maxiofs-folder" ||
			strings.HasSuffix(name, ".metadata") ||
			strings.HasSuffix(name, ".metadata-staging") ||
			strings.HasPrefix(name, ".tmp_") ||
			strings.HasPrefix(name, ".metadata-tmp-") ||
			strings.HasPrefix(name, "maxiofs-upload-") ||
			strings.HasPrefix(name, "maxiofs-encmigrate") ||
			strings.HasPrefix(name, "maxiofs-multipart-") {
			report.Skipped++
			return nil
		}

		rel, err := filepath.Rel(bkt.dirPath, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		var key, versionID string
		if strings.HasPrefix(rel, ".versions/") {
			// bucket/.versions/<key...>/<versionID>
			trimmed := strings.TrimPrefix(rel, ".versions/")
			slash := strings.LastIndex(trimmed, "/")
			if slash <= 0 {
				report.Skipped++
				return nil
			}
			key = trimmed[:slash]
			versionID = trimmed[slash+1:]
			bkt.versioned = true
		} else {
			key = rel
		}

		obj, class, oErr := objectFromSidecar(path, bkt.bucketPath, key, versionID, encryptor, keys)
		if oErr != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s/%s: %v", bkt.bucketPath, key, oErr))
			if obj == nil {
				return nil
			}
			// Partial failure (e.g. the wrapped DEK does not unwrap with the
			// bundle keys): the bytes exist on disk, so the object is still
			// indexed — hiding it from listings would make it unrecoverable
			// forever. The failure stays in the report for the operator.
		}

		switch class {
		case classEncryptedVerified:
			report.EncryptedVerified++
		case classEncryptedUnverified:
			report.EncryptedUnverified++
		case classLegacyEncrypted:
			report.LegacyEncrypted++
		default:
			report.Plaintext++
		}

		if versionID != "" {
			bkt.versions[key] = append(bkt.versions[key], obj)
			report.Versions++
		} else {
			bkt.objects = append(bkt.objects, obj)
			report.Objects++
		}
		bkt.totalSize += obj.Size

		if verbose {
			logrus.WithFields(logrus.Fields{
				"bucket": bkt.bucketPath, "key": key, "version": versionID, "size": obj.Size,
			}).Info("Recovered object entry")
		}
		return nil
	})
}

type objectClass int

const (
	classPlaintext objectClass = iota
	classEncryptedVerified
	classEncryptedUnverified
	classLegacyEncrypted
)

// objectFromSidecar builds the Pebble entry for one stored file from its
// sidecar (or from file stats when the sidecar is missing).
func objectFromSidecar(path, bucketPath, key, versionID string, encryptor encryption.Encryptor, keys map[int][]byte) (*metadata.ObjectMetadata, objectClass, error) {
	sidecar, err := readSidecar(path)
	if err != nil {
		// No sidecar: best-effort entry from file stats (plaintext assumed —
		// an encrypted object without its sidecar has lost its DEK anyway).
		info, sErr := os.Stat(path)
		if sErr != nil {
			return nil, classPlaintext, sErr
		}
		return &metadata.ObjectMetadata{
			Bucket: bucketPath, Key: key, VersionID: versionID,
			Size: info.Size(), LastModified: info.ModTime(),
			ContentType: "application/octet-stream",
		}, classPlaintext, nil
	}

	encrypted := sidecar["encrypted"] == "true"

	var size int64
	var etag string
	if encrypted {
		size, _ = strconv.ParseInt(sidecar["original-size"], 10, 64)
		etag = sidecar["original-etag"]
	} else {
		size, _ = strconv.ParseInt(sidecar["size"], 10, 64)
		etag = sidecar["etag"]
	}

	lastModified := time.Now()
	if lm, err := strconv.ParseInt(sidecar["last_modified"], 10, 64); err == nil && lm > 0 {
		lastModified = time.Unix(lm, 0)
	}

	contentType := sidecar["content-type"]
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	obj := &metadata.ObjectMetadata{
		Bucket: bucketPath, Key: key, VersionID: versionID,
		Size: size, ETag: etag, LastModified: lastModified,
		ContentType:        contentType,
		ContentDisposition: sidecar["content-disposition"],
		ContentEncoding:    sidecar["content-encoding"],
		CacheControl:       sidecar["cache-control"],
		ContentLanguage:    sidecar["content-language"],
		StorageClass:       sidecar["storage-class"],
	}

	if !encrypted {
		return obj, classPlaintext, nil
	}
	if sidecar["wrapped-dek"] == "" {
		return obj, classLegacyEncrypted, nil
	}

	// Envelope: verify the DEK unwraps with the bundle keys when we have them.
	version, vErr := strconv.Atoi(sidecar["kek-version"])
	if vErr != nil || keys == nil {
		return obj, classEncryptedUnverified, nil
	}
	kekKey, ok := keys[version]
	if !ok {
		return obj, classEncryptedUnverified, nil
	}
	wrapped, _ := hex.DecodeString(sidecar["wrapped-dek"])
	iv, _ := hex.DecodeString(sidecar["wrapped-dek-iv"])
	if _, err := encryptor.Decrypt(&encryption.EncryptedData{Data: wrapped, IV: iv}, kekKey); err != nil {
		return obj, classEncryptedUnverified, fmt.Errorf("wrapped DEK does not unwrap with bundle key v%d", version)
	}
	return obj, classEncryptedVerified, nil
}

// rebuildPebble writes buckets, objects and versions into a fresh store.
// On success, outDB itself IS the store directory (Pebble files + v2
// sentinel), so activating it is exactly `mv <outDB> <data-dir>/metadata`.
func rebuildPebble(outDB string, buckets []*bucketEntry, report *Report) error {
	ctx := context.Background()

	// NewPebbleStore creates the actual store under <DataDir>/metadata — build
	// there and hoist afterwards so outDB matches the server's layout.
	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: outDB,
		Logger:  logrus.StandardLogger(),
	})
	if err != nil {
		return fmt.Errorf("failed to create fresh metadata store at %s: %w", outDB, err)
	}
	closed := false
	defer func() {
		if !closed {
			store.Close()
		}
	}()

	for _, bkt := range buckets {
		bucketMeta := &metadata.BucketMetadata{
			Name:      bkt.name,
			TenantID:  bkt.tenantID,
			OwnerID:   "admin", // ownership lives in SQLite, re-applied after recovery
			OwnerType: "user",
			Region:    "us-east-1",
			CreatedAt: bkt.createdAt,
			UpdatedAt: time.Now(),
			ObjectCount: int64(len(bkt.objects)) + func() int64 {
				var n int64
				for _, vs := range bkt.versions {
					n += int64(len(vs))
				}
				return n
			}(),
			TotalSize: bkt.totalSize,
		}
		if bkt.versioned {
			bucketMeta.Versioning = &metadata.VersioningMetadata{Status: "Enabled"}
		}
		if err := store.CreateBucket(ctx, bucketMeta); err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("create bucket %s: %v", bkt.bucketPath, err))
			continue
		}

		for _, obj := range bkt.objects {
			if err := store.PutObject(ctx, obj); err != nil {
				report.Failures = append(report.Failures, fmt.Sprintf("put %s/%s: %v", bkt.bucketPath, obj.Key, err))
			}
		}

		for key, versionList := range bkt.versions {
			// Chronological order; version IDs start with a nanosecond timestamp.
			sort.Slice(versionList, func(i, j int) bool {
				return versionList[i].VersionID < versionList[j].VersionID
			})
			for i, obj := range versionList {
				isLatest := i == len(versionList)-1
				obj.IsLatest = isLatest
				version := &metadata.ObjectVersion{
					VersionID:    obj.VersionID,
					IsLatest:     isLatest,
					Key:          key,
					Size:         obj.Size,
					ETag:         obj.ETag,
					LastModified: obj.LastModified,
					StorageClass: obj.StorageClass,
				}
				if err := store.PutObjectVersion(ctx, obj, version); err != nil {
					report.Failures = append(report.Failures, fmt.Sprintf("put version %s/%s@%s: %v", bkt.bucketPath, key, obj.VersionID, err))
				}
			}
		}
	}

	// Hoist <outDB>/metadata up so outDB itself is the store directory —
	// matching the server layout (<data-dir>/metadata, sentinel inside).
	if err := store.Close(); err != nil {
		return fmt.Errorf("failed to close rebuilt store: %w", err)
	}
	closed = true
	inner := filepath.Join(outDB, "metadata")
	tmp := outDB + ".hoist"
	if err := os.Rename(inner, tmp); err != nil {
		return fmt.Errorf("failed to stage rebuilt store: %w", err)
	}
	if err := os.RemoveAll(outDB); err != nil {
		return fmt.Errorf("failed to clear rebuild scaffolding: %w", err)
	}
	if err := os.Rename(tmp, outDB); err != nil {
		return fmt.Errorf("failed to finalise rebuilt store: %w", err)
	}

	logrus.WithField("out_db", outDB).Info("Fresh metadata store rebuilt")
	return nil
}

// restoreKEKs writes the bundle's key versions into the auth SQLite so the
// recovered server's bootstrap loads them instead of generating a new KEK.
// The table is created with the pre-cluster schema so pending migrations
// still apply cleanly on the next server start.
func restoreKEKs(dbPath, bundlePath, passphrase string) (int, error) {
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		return 0, err
	}
	records, err := kek.DecryptBundle(data, passphrase)
	if err != nil {
		return 0, err
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0750); err != nil {
		return 0, err
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(10000)")
	if err != nil {
		return 0, err
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS encryption_keys (
			version INTEGER PRIMARY KEY,
			key_hex TEXT NOT NULL,
			is_current INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)
	`); err != nil {
		return 0, fmt.Errorf("failed to create encryption_keys table: %w", err)
	}

	// Does the DB already have a current key? A partially-surviving database
	// is more recent than any external bundle, so its current marker wins;
	// the bundle's current is only applied when the DB had none. Inserting the
	// bundle's is_current flag blindly could leave TWO current rows and make
	// the server's bootstrap pick one non-deterministically.
	var existingCurrent int
	if err := db.QueryRow(`SELECT COUNT(*) FROM encryption_keys WHERE is_current = 1`).Scan(&existingCurrent); err != nil {
		return 0, err
	}

	restored := 0
	bundleCurrent := 0
	maxVersion := 0
	for _, r := range records {
		if r.IsCurrent {
			bundleCurrent = r.Version
		}
		if r.Version > maxVersion {
			maxVersion = r.Version
		}
		var existing string
		err := db.QueryRow(`SELECT key_hex FROM encryption_keys WHERE version = ?`, r.Version).Scan(&existing)
		switch {
		case err == sql.ErrNoRows:
			// Always inserted non-current; the marker is applied once, below.
			if _, err := db.Exec(
				`INSERT INTO encryption_keys (version, key_hex, is_current, created_at) VALUES (?, ?, 0, ?)`,
				r.Version, r.KeyHex, time.Now().Unix(),
			); err != nil {
				return restored, fmt.Errorf("failed to insert key v%d: %w", r.Version, err)
			}
			restored++
		case err != nil:
			return restored, err
		case existing != r.KeyHex:
			return restored, fmt.Errorf("key version %d already exists in %s with DIFFERENT material — refusing to overwrite", r.Version, dbPath)
		}
	}

	if existingCurrent == 0 {
		markCurrent := bundleCurrent
		if markCurrent == 0 {
			markCurrent = maxVersion // defensive: a bundle always carries a current
		}
		tx, err := db.Begin()
		if err != nil {
			return restored, err
		}
		if _, err := tx.Exec(`UPDATE encryption_keys SET is_current = 0`); err != nil {
			tx.Rollback() //nolint:errcheck
			return restored, err
		}
		if _, err := tx.Exec(`UPDATE encryption_keys SET is_current = 1 WHERE version = ?`, markCurrent); err != nil {
			tx.Rollback() //nolint:errcheck
			return restored, err
		}
		if err := tx.Commit(); err != nil {
			return restored, err
		}
	} else {
		logrus.Info("Database already has a current encryption key — keeping it (bundle keys added for decryption only)")
	}

	logrus.WithFields(logrus.Fields{"db": dbPath, "restored": restored, "total": len(records)}).
		Info("Encryption keys restored from recovery bundle")
	return restored, nil
}

// readSidecar loads a .metadata sidecar for the given data path (or the
// sidecar itself when path already ends in .metadata).
func readSidecar(path string) (map[string]string, error) {
	sidecarPath := path
	if !strings.HasSuffix(path, ".metadata") {
		sidecarPath = path + ".metadata"
	}
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}
