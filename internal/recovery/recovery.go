// Package recovery rebuilds the Pebble metadata store from the filesystem
package recovery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/maxiofs/maxiofs/pkg/encryption"
	"github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// Options configures a recovery run.
type Options struct {
	// DataDir is the MaxIOFS data directory (contains db/).
	DataDir string
	// ObjectsRoot is the objects directory, storage.root of the
	// configuration. Empty is <DataDir>/objects.
	ObjectsRoot string
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
	CustomerKey         int // encrypted with a customer key (SSE-C): read with the client's key
	Plaintext           int
	Skipped             int // markers, temp files
	BucketSettings      int // buckets whose configuration came back from their marker
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
	// config is the configuration the bucket's marker keeps, nil in a marker
	// of an earlier release.
	config *metadata.BucketMetadata
}

// Run executes the recovery. A recovery that stopped is continued by running
// it again with the same options.
func Run(opts Options) (*Report, error) {
	objectsRoot := opts.ObjectsRoot
	if objectsRoot == "" {
		objectsRoot = filepath.Join(opts.DataDir, "objects")
	}
	if info, err := os.Stat(objectsRoot); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("objects directory not found at %s — is --data-dir or --storage-root correct?", objectsRoot)
	}

	if opts.OutDB == "" {
		opts.OutDB = filepath.Join(opts.DataDir, "metadata-recovered")
	}

	keys, bundle, err := openBundle(opts.BundlePath, opts.Passphrase)
	if err != nil {
		return nil, err
	}

	buckets, err := discoverBuckets(objectsRoot)
	if err != nil {
		return nil, err
	}

	r := &rebuild{
		objectsRoot: objectsRoot,
		outDB:       opts.OutDB,
		encryptor:   encryption.NewAESGCMEncryptor(encryption.DefaultEncryptionConfig()),
		keys:        keys,
		verbose:     opts.Verbose,
		dryRun:      opts.DryRun,
		started:     time.Now(),
	}
	r.told = r.started
	r.total.Store(-1)
	counting, stopCounting := context.WithCancel(context.Background())
	counted := make(chan struct{})
	go func() {
		defer close(counted)
		countObjects(counting, buckets, &r.total)
	}()
	defer func() {
		stopCounting()
		<-counted
	}()

	if opts.DryRun {
		r.p = &progress{}
		if err := r.buckets(context.Background(), buckets); err != nil {
			return nil, err
		}
		logrus.WithField("took", time.Since(r.started).Round(time.Second).String()).Info("Dry run — nothing written")
		return r.report(buckets), nil
	}

	if r.p, err = startProgress(opts, bundle); err != nil {
		return nil, err
	}
	p := r.p
	r.workBefore = p.Done + p.Marked

	if p.Stage == stageBuckets {
		inner := filepath.Join(opts.OutDB, "metadata")
		if _, err := os.Stat(inner); p.BucketPath != "" && err != nil {
			return nil, fmt.Errorf("the store being rebuilt is gone from %s; remove %s to start over", inner, p.path)
		}
		// NewPebbleStore creates the store under <DataDir>/metadata — it is
		// built there and hoisted afterwards so outDB matches the server's layout.
		store, err := metadata.NewPebbleStore(metadata.PebbleOptions{
			DataDir:   opts.OutDB,
			Logger:    logrus.StandardLogger(),
			DeferSync: true, // made durable at each checkpoint
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create fresh metadata store at %s: %w", opts.OutDB, err)
		}
		r.store = store
		err = r.buckets(context.Background(), buckets)
		if closeErr := store.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("failed to close rebuilt store: %w", closeErr)
		}
		if err != nil {
			return nil, err
		}
		p.Stage = stageKeys
		if err := p.save(); err != nil {
			return nil, err
		}
	}

	// Restore the encryption keys into the auth SQLite so the recovered
	// server can decrypt (bootstrap will load them instead of generating).
	if p.Stage == stageKeys {
		if len(keys) > 0 {
			restored, err := restoreKEKs(filepath.Join(opts.DataDir, "db", "maxiofs.db"), opts.BundlePath, opts.Passphrase)
			if err != nil {
				p.Report.Failures = append(p.Report.Failures, fmt.Sprintf("KEK restore: %v", err))
			} else {
				p.Report.KEKsRestored = restored
			}
		}
		if err := stopping("keys"); err != nil {
			return nil, err
		}
		p.Stage = stageHoist
		if err := p.save(); err != nil {
			return nil, err
		}
	}

	if err := hoist(opts.OutDB); err != nil {
		return nil, err
	}
	logrus.WithFields(logrus.Fields{"out_db": opts.OutDB, "took": time.Since(r.started).Round(time.Second).String()}).
		Info("Fresh metadata store rebuilt")
	if err := os.Remove(p.path); err != nil {
		return nil, fmt.Errorf("failed to remove the recovery progress: %w", err)
	}
	return r.report(buckets), nil
}

// openBundle opens the recovery bundle at path, when there is one: its keys by
// version and the SHA-256 of the file.
func openBundle(path, passphrase string) (map[int][]byte, string, error) {
	if path == "" {
		return nil, "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read recovery bundle: %w", err)
	}
	records, err := kek.DecryptBundle(data, passphrase)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open recovery bundle: %w", err)
	}
	keys := make(map[int][]byte, len(records))
	for _, r := range records {
		keyBytes, _ := hex.DecodeString(r.KeyHex)
		keys[r.Version] = keyBytes
	}
	logrus.WithField("versions", len(keys)).Info("Recovery bundle opened")
	sum := sha256.Sum256(data)
	return keys, hex.EncodeToString(sum[:]), nil
}

// checkpointEvery is how many files a recovery walks between two records of
// where it stands.
var checkpointEvery = 1000

// stopAt, when set, is asked at each step of a recovery whether it stops there.
var stopAt func(step string) error

func stopping(step string) error {
	if stopAt == nil {
		return nil
	}
	return stopAt(step)
}

const (
	stageBuckets = "buckets"
	stageKeys    = "keys"
	stageHoist   = "hoist"
)

// progress is where an unfinished recovery stands. It is kept beside OutDB
// until the recovery ends, so running the same recovery again continues it.
type progress struct {
	DataDir string `json:"data_dir"`
	// Bundle is the SHA-256 of the bundle file, "" without one.
	Bundle string `json:"bundle"`
	Stage  string `json:"stage"`
	// Bucket is the index of the bucket being rebuilt and BucketPath its path.
	// Last is its last file written, relative to the objects directory, and
	// Walked says all of them are.
	Bucket     int    `json:"bucket"`
	BucketPath string `json:"bucket_path,omitempty"`
	Last       string `json:"last,omitempty"`
	Walked     bool   `json:"walked,omitempty"`
	Versioned  bool   `json:"versioned,omitempty"`
	// Locked says a version of the bucket has a retention or a legal hold.
	Locked bool `json:"locked,omitempty"`
	// Done is how many objects and versions the recovery has walked, Marked
	// how many keys of the buckets it finished got their newest version current.
	Done   int64  `json:"done,omitempty"`
	Marked int64  `json:"marked,omitempty"`
	Report Report `json:"report"`

	path string // "" on a dry run, which keeps nothing
}

// startProgress continues the unfinished recovery recorded beside OutDB, which
// has to be this one, or starts one into an empty OutDB.
func startProgress(opts Options, bundle string) (*progress, error) {
	dataDir, err := filepath.Abs(opts.DataDir)
	if err != nil {
		return nil, err
	}
	path := opts.OutDB + ".progress"
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		p := &progress{path: path}
		if err := json.Unmarshal(data, p); err != nil {
			return nil, fmt.Errorf("unreadable recovery progress %s: %w", path, err)
		}
		if p.DataDir != dataDir || p.Bundle != bundle {
			return nil, fmt.Errorf("%s records an unfinished recovery of %s; continue it with the same --data-dir and --recovery-bundle, or remove it and %s to start over",
				path, p.DataDir, opts.OutDB)
		}
		logrus.WithFields(logrus.Fields{"stage": p.Stage, "bucket": p.BucketPath, "last": p.Last}).
			Info("Continuing the unfinished recovery")
		return p, nil
	case !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("failed to read the recovery progress: %w", err)
	}

	if entries, err := os.ReadDir(opts.OutDB); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("output directory %s is not empty — refusing to overwrite (recovery always writes into a fresh store)", opts.OutDB)
	}
	if _, err := os.Stat(opts.OutDB + ".hoist"); err == nil {
		return nil, fmt.Errorf("%s.hoist holds a store a stopped recovery left; move it to %s or remove it", opts.OutDB, opts.OutDB)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	p := &progress{DataDir: dataDir, Bundle: bundle, Stage: stageBuckets, path: path}
	return p, p.save()
}

// save records p in a new file that replaces the last, so a recovery stopped
// at any moment finds this record or the one before.
func (p *progress) save() error {
	if p.path == "" {
		return nil
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp := p.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to record the recovery progress: %w", err)
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, p.path)
	}
	if err != nil {
		return fmt.Errorf("failed to record the recovery progress: %w", err)
	}
	return nil
}

// rebuild is one recovery run; without a store it is a dry run.
type rebuild struct {
	objectsRoot string
	outDB       string
	encryptor   encryption.Encryptor
	keys        map[int][]byte
	verbose     bool
	store       *metadata.PebbleStore
	p           *progress
	unsaved     int // files walked since the last record

	dryRun     bool
	nBuckets   int
	total      atomic.Int64 // objects and versions of every bucket, -1 until counted
	started    time.Time    // when this run started
	workBefore int64        // work done before this run
	marking    int64        // keys of the bucket being marked made current so far
	told       time.Time    // the last progress logged
}

// checkpoint records where the recovery stands, once what it wrote is on
// disk: the store does not wait for the disk on each write.
func (r *rebuild) checkpoint() error {
	if r.store != nil {
		if err := r.store.Sync(); err != nil {
			return fmt.Errorf("failed to sync the rebuilt store: %w", err)
		}
	}
	return r.p.save()
}

// progressEvery is how often a recovery logs how far it is.
var progressEvery = 30 * time.Second

// tell logs how far the recovery is, at most once per progressEvery, with
// what it is doing in fields.
func (r *rebuild) tell(fields logrus.Fields) {
	now := time.Now()
	if now.Sub(r.told) < progressEvery {
		return
	}
	r.told = now
	done, total := r.work()
	for k, v := range progressFields(done, total, done-r.workBefore, now.Sub(r.started), now) {
		fields[k] = v
	}
	logrus.WithFields(fields).Info("Recovery progress")
}

// work is the work a recovery has done and the work there is, -1 while the
// files are being counted. Each object and version is read, then each key of
// a versioned bucket gets its newest version current: the keys are taken to
// be as many as the versions, in the share of them read so far.
func (r *rebuild) work() (done, total int64) {
	done = r.p.Done + r.p.Marked + r.marking
	objects := r.total.Load()
	if objects < 0 {
		return done, -1
	}
	if r.dryRun {
		return done, objects
	}
	share := 1.0
	if read := r.p.Report.Objects + r.p.Report.Versions; read > 0 {
		share = float64(r.p.Report.Versions) / float64(read)
	}
	return done, objects + int64(share*float64(objects))
}

// progressFields says how far a recovery is from the work done and the work
// there is, total -1 while it is not known, and the work done this run in
// elapsed: its percentage, its rate, the time left and when it should end.
func progressFields(done, total, doneThisRun int64, elapsed time.Duration, now time.Time) logrus.Fields {
	fields := logrus.Fields{}
	if total >= 0 {
		fields["percent"] = fmt.Sprintf("%.1f%%", min(100, 100*float64(done)/float64(max(total, 1))))
	}
	if doneThisRun <= 0 || elapsed <= 0 {
		return fields
	}
	rate := float64(doneThisRun) / elapsed.Seconds()
	fields["rate"] = fmt.Sprintf("%.0f/s", rate)
	if total >= 0 {
		left := time.Duration(float64(max(total-done, 0)) / rate * float64(time.Second)).Round(time.Second)
		fields["left"] = left.String()
		fields["eta"] = now.Add(left).Format("2006-01-02 15:04")
	}
	return fields
}

// ofTotal says n of the total, "(counting)" while the total is not known.
func ofTotal(n, total int64) string {
	if total < 0 {
		return fmt.Sprintf("%d of (counting)", n)
	}
	return fmt.Sprintf("%d of %d", n, total)
}

// countObjects counts the objects and versions of buckets into total, which
// stays -1 until it is done. Listing the files takes a fraction of the time
// their recovery takes.
func countObjects(ctx context.Context, buckets []*bucketEntry, total *atomic.Int64) {
	var n int64
	for _, bkt := range buckets {
		err := filepath.WalkDir(bkt.dirPath, func(_ string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == nil && !d.IsDir() && !isInternalObjectName(d.Name()) {
				n++
			}
			return nil
		})
		if err != nil {
			return
		}
	}
	total.Store(n)
	logrus.WithField("objects", n).Info("Objects and versions to recover counted")
}

func (r *rebuild) report(buckets []*bucketEntry) *Report {
	report := r.p.Report
	report.Buckets, report.OutDB = len(buckets), r.outDB
	return &report
}

// changed is the error of a recovery continued over files that are not those
// it stopped over.
func (r *rebuild) changed(what string) error {
	return fmt.Errorf("%s has changed since the recovery stopped; remove %s and %s to start over", what, r.outDB, r.p.path)
}

// buckets rebuilds every bucket from the one a stopped recovery left.
func (r *rebuild) buckets(ctx context.Context, buckets []*bucketEntry) error {
	p := r.p
	if p.BucketPath != "" && (p.Bucket >= len(buckets) || buckets[p.Bucket].bucketPath != p.BucketPath) {
		return r.changed(fmt.Sprintf("the bucket list of %s", r.objectsRoot))
	}
	r.nBuckets = len(buckets)
	for i := p.Bucket; i < len(buckets); i++ {
		bkt := buckets[i]
		if i != p.Bucket || p.BucketPath == "" {
			p.Bucket, p.BucketPath, p.Last, p.Walked, p.Versioned, p.Locked = i, bkt.bucketPath, "", false, false, false
		}
		logrus.WithField("bucket", fmt.Sprintf("%s (%d of %d)", bkt.bucketPath, i+1, len(buckets))).Info("Recovering bucket")
		if err := r.bucket(ctx, bkt); err != nil {
			return err
		}
		if err := stopping("bucket"); err != nil {
			return err
		}
	}
	return nil
}

// bucket rebuilds one bucket: its files are written as they are walked, then
// the newest version of each key becomes its current entry and the bucket
// gets its versioning and its counts.
func (r *rebuild) bucket(ctx context.Context, bkt *bucketEntry) error {
	p := r.p
	if bkt.config != nil && p.Last == "" && !p.Walked {
		p.Report.BucketSettings++
	}
	if r.store != nil {
		meta := &metadata.BucketMetadata{
			Name:      bkt.name,
			TenantID:  bkt.tenantID,
			OwnerID:   "admin", // a marker of an earlier release does not name the owner
			OwnerType: "user",
			Region:    "us-east-1",
			CreatedAt: bkt.createdAt,
			UpdatedAt: time.Now(),
		}
		if bkt.config != nil {
			kept := *bkt.config
			kept.Name, kept.TenantID = bkt.name, bkt.tenantID
			meta = &kept
		}
		err := r.store.CreateBucket(ctx, meta)
		if err != nil && !errors.Is(err, metadata.ErrBucketAlreadyExists) {
			p.Report.Failures = append(p.Report.Failures, fmt.Sprintf("create bucket %s: %v", bkt.bucketPath, err))
			return nil
		}
	}
	if !p.Walked {
		if err := r.walk(bkt); err != nil {
			return err
		}
		p.Walked = true
		if err := r.checkpoint(); err != nil {
			return err
		}
	}
	if r.store == nil {
		return nil
	}
	if p.Versioned {
		if err := r.markLatest(ctx, bkt); err != nil {
			return err
		}
	}
	r.count(ctx, bkt)
	return nil
}

// walk writes the bucket's files in the order they are walked, after the last
// one a stopped recovery wrote.
func (r *rebuild) walk(bkt *bucketEntry) error {
	p := r.p
	after := p.Last
	err := filepath.WalkDir(bkt.dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if after == "" {
				p.Report.Failures = append(p.Report.Failures, fmt.Sprintf("%s: %v", path, err))
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(r.objectsRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if after != "" {
			if rel == after {
				after = ""
			}
			return nil
		}
		r.file(bkt, path)
		p.Last = rel
		if !isInternalObjectName(d.Name()) {
			p.Done++
			r.tell(logrus.Fields{
				"bucket":  fmt.Sprintf("%s (%d of %d)", p.BucketPath, p.Bucket+1, r.nBuckets),
				"objects": ofTotal(p.Done, r.total.Load()),
			})
		}
		if r.unsaved++; r.unsaved >= checkpointEvery {
			if err := r.checkpoint(); err != nil {
				return err
			}
			r.unsaved = 0
		}
		return stopping("file")
	})
	if err != nil {
		return err
	}
	if after != "" {
		return r.changed(fmt.Sprintf("%s (%s is gone)", bkt.dirPath, after))
	}
	return nil
}

// discoverBuckets finds every directory carrying a .maxiofs-bucket marker
// (global buckets at depth 1, tenant buckets at depth 2).
func discoverBuckets(objectsRoot string) ([]*bucketEntry, error) {
	var buckets []*bucketEntry

	appendIfBucket := func(dirPath, tenantHint string) (bool, error) {
		markerPath := filepath.Join(dirPath, ".maxiofs-bucket")
		info, statErr := os.Stat(markerPath)
		if statErr != nil {
			return false, nil
		}

		name := filepath.Base(dirPath)
		tenantID := tenantHint
		createdAt := info.ModTime()
		bucketPath := ""

		// The current layout writes the tenant-qualified path into the marker,
		// with the bucket's configuration since it is kept there; the previous
		// layout left it empty and recorded the tenant in a sidecar.
		marker, err := storage.ReadBucketMarker(dirPath)
		if err != nil {
			logrus.WithError(err).WithField("dir", dirPath).Warn("Bucket marker unreadable: the bucket is named after its directory")
		}
		var config *metadata.BucketMetadata
		if len(marker.Config) > 0 {
			config = &metadata.BucketMetadata{}
			if err := json.Unmarshal(marker.Config, config); err != nil {
				logrus.WithError(err).WithField("dir", dirPath).Warn("Bucket configuration in the marker unreadable: it is not restored")
				config = nil
			}
		}
		if recorded := marker.Bucket; recorded != "" {
			bucketPath = recorded
			name = recorded
			tenantID = ""
			if i := strings.LastIndex(recorded, "/"); i >= 0 {
				tenantID, name = recorded[:i], recorded[i+1:]
			}
		}

		if bucketPath == "" {
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
			bucketPath = name
			if tenantID != "" {
				bucketPath = tenantID + "/" + name
			}
		}
		buckets = append(buckets, &bucketEntry{
			dirPath:    dirPath,
			bucketPath: bucketPath,
			name:       name,
			tenantID:   tenantID,
			createdAt:  createdAt,
			config:     config,
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

// file reads one file of a bucket into the report and, unless this is a dry
// run, into the store: a version as one that is not the latest yet.
func (r *rebuild) file(bkt *bucketEntry, path string) {
	ctx := context.Background()
	report := &r.p.Report
	// Markers, sidecars and staging leftovers.
	if isInternalObjectName(filepath.Base(path)) {
		report.Skipped++
		return
	}

	sidecar, _ := readSidecar(path)

	bucketPath, key, versionID := identityFromSidecar(sidecar, bkt.bucketPath)
	if key == "" {
		var ok bool
		if key, versionID, ok = keyFromRelPath(bkt.dirPath, path); !ok {
			report.Skipped++
			return
		}
	}
	if versionID != "" {
		r.p.Versioned = true
	}

	obj, class, oErr := objectFromSidecar(path, bucketPath, key, versionID, sidecar, r.encryptor, r.keys)
	if oErr != nil {
		report.Failures = append(report.Failures, fmt.Sprintf("%s/%s: %v", bucketPath, key, oErr))
		if obj == nil {
			return
		}
	}

	if obj.Retention != nil || obj.LegalHold {
		r.p.Locked = true
	}

	switch class {
	case classEncryptedVerified:
		report.EncryptedVerified++
	case classEncryptedUnverified:
		report.EncryptedUnverified++
	case classLegacyEncrypted:
		report.LegacyEncrypted++
	case classCustomerKey:
		report.CustomerKey++
	default:
		report.Plaintext++
	}
	if versionID != "" {
		report.Versions++
	} else {
		report.Objects++
	}

	if r.verbose {
		logrus.WithFields(logrus.Fields{
			"bucket": bkt.bucketPath, "key": key, "version": versionID, "size": obj.Size,
		}).Info("Recovered object entry")
	}
	if r.store == nil {
		return
	}
	if versionID == "" {
		if err := r.store.PutObject(ctx, obj); err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("put %s/%s: %v", bkt.bucketPath, obj.Key, err))
		}
		return
	}
	if err := r.store.PutObjectVersion(ctx, obj, versionOf(obj, false)); err != nil {
		report.Failures = append(report.Failures, fmt.Sprintf("put version %s/%s@%s: %v", bkt.bucketPath, key, versionID, err))
	}
}

func versionOf(obj *metadata.ObjectMetadata, latest bool) *metadata.ObjectVersion {
	return &metadata.ObjectVersion{
		VersionID:    obj.VersionID,
		IsLatest:     latest,
		Key:          obj.Key,
		Size:         obj.Size,
		ETag:         obj.ETag,
		LastModified: obj.LastModified,
		StorageClass: obj.StorageClass,
	}
}

// markLatest makes the newest version of each key of the bucket its current
// entry, as its write did; done again, it changes nothing. Version IDs start
// with a nanosecond timestamp.
func (r *rebuild) markLatest(ctx context.Context, bkt *bucketEntry) error {
	newest := make(map[string]string)
	err := r.store.RawScan(ctx, "version:"+bkt.bucketPath+":", "", func(_ string, val []byte) bool {
		var v metadata.ObjectMetadata
		if json.Unmarshal(val, &v) != nil || v.Key == "" || v.VersionID == "" || v.VersionID == metadata.NullVersionID {
			return true
		}
		if v.VersionID > newest[v.Key] {
			newest[v.Key] = v.VersionID
		}
		return true
	})
	if err != nil {
		return fmt.Errorf("failed to read the versions of %s: %w", bkt.bucketPath, err)
	}
	logrus.WithFields(logrus.Fields{"bucket": bkt.bucketPath, "keys": len(newest)}).Info("Making the newest version of each key current")
	r.marking = 0
	// In key order, as the store keeps them: in a map's order each read is
	// somewhere else in the store.
	for _, key := range slices.Sorted(maps.Keys(newest)) {
		versionID := newest[key]
		obj, err := r.store.GetObject(ctx, bkt.bucketPath, key, versionID)
		if err == nil {
			err = r.store.PutObjectVersion(ctx, obj, versionOf(obj, true))
		}
		if err != nil {
			r.p.Report.Failures = append(r.p.Report.Failures, fmt.Sprintf("latest version %s/%s@%s: %v", bkt.bucketPath, key, versionID, err))
		}
		if err := stopping("latest"); err != nil {
			return err
		}
		r.marking++
		r.tell(logrus.Fields{
			"bucket": fmt.Sprintf("%s (%d of %d)", bkt.bucketPath, r.p.Bucket+1, r.nBuckets),
			"keys":   fmt.Sprintf("%d of %d", r.marking, len(newest)),
		})
	}
	r.p.Marked += r.marking
	r.marking = 0
	return nil
}

// count gives the bucket its versioning, and the counts the server keeps.
func (r *rebuild) count(ctx context.Context, bkt *bucketEntry) {
	fail := func(err error) {
		r.p.Report.Failures = append(r.p.Report.Failures, fmt.Sprintf("bucket %s: %v", bkt.bucketPath, err))
	}
	// What the marker does not say is inferred from the files: versions exist
	// only in a bucket with versioning, and a retention or a legal hold only in
	// one with Object Lock.
	if r.p.Versioned || r.p.Locked {
		stored, err := r.store.GetBucket(ctx, bkt.tenantID, bkt.name)
		if err != nil {
			fail(err)
			return
		}
		inferred := false
		if stored.Versioning == nil || stored.Versioning.Status == "" {
			stored.Versioning = &metadata.VersioningMetadata{Status: "Enabled"}
			inferred = true
		}
		if r.p.Locked && (stored.ObjectLock == nil || !stored.ObjectLock.Enabled) {
			stored.ObjectLock = &metadata.ObjectLockMetadata{Enabled: true}
			inferred = true
		}
		if inferred {
			if err := r.store.UpdateBucket(ctx, stored); err != nil {
				fail(err)
				return
			}
		}
	}
	if err := r.store.RecalculateBucketStats(ctx, bkt.tenantID, bkt.name); err != nil {
		fail(err)
	}
}

// isInternalObjectName reports whether a filename under a bucket root is a
// MaxIOFS-internal artifact (markers, sidecars, staging/temp files) rather
// than object data. Shared by the offline rebuild and the online reconciler.
func isInternalObjectName(name string) bool {
	return name == ".maxiofs-bucket" || name == ".maxiofs-folder" || storage.IsShardFileName(name) ||
		strings.HasSuffix(name, ".metadata") ||
		strings.HasSuffix(name, ".metadata-staging") ||
		strings.HasPrefix(name, ".tmp_") ||
		strings.HasPrefix(name, ".metadata-tmp-") ||
		strings.HasPrefix(name, "maxiofs-upload-") ||
		strings.HasPrefix(name, "maxiofs-encmigrate") ||
		strings.HasPrefix(name, "maxiofs-multipart-")
}

type objectClass int

const (
	classPlaintext objectClass = iota
	classEncryptedVerified
	classEncryptedUnverified
	classLegacyEncrypted
	classCustomerKey
)

// objectFromSidecar builds the Pebble entry for one stored file from its
// sidecar (or from file stats when the sidecar is missing).
// identityFromSidecar returns the identity the storage layer stamped on write.
// A sidecar written before that carries none and key comes back empty, leaving
// the caller to derive it from the path.
func identityFromSidecar(sidecar map[string]string, fallbackBucket string) (bucket, key, versionID string) {
	key = sidecar[storage.MetadataKeyField]
	if key == "" {
		return fallbackBucket, "", ""
	}
	bucket = sidecar[storage.MetadataBucketField]
	if bucket == "" {
		bucket = fallbackBucket
	}
	return bucket, key, sidecar[storage.MetadataVersionField]
}

func objectFromSidecar(path, bucketPath, key, versionID string, sidecar map[string]string, encryptor encryption.Encryptor, keys map[int][]byte) (*metadata.ObjectMetadata, objectClass, error) {
	if sidecar == nil {
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

	size, etag := storage.SidecarIdentity(sidecar)

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
		ContentType:  contentType,
		StorageClass: sidecar["storage-class"],
		LegalHold:    sidecar[storage.MetadataLegalHoldField] == "ON",
	}
	obj.TakeStoredHeaders(sidecar)
	if mode := sidecar[storage.MetadataLockModeField]; mode != "" {
		if until, err := time.Parse(time.RFC3339Nano, sidecar[storage.MetadataLockUntilField]); err == nil {
			obj.Retention = &metadata.RetentionMetadata{Mode: mode, RetainUntilDate: until}
		}
	}

	if !encrypted {
		return obj, classPlaintext, nil
	}
	// The client's key wraps its DEK: it is kept as it is, to be read with it.
	if dek := sidecar[storage.MetadataCustomerDEKField]; dek != "" {
		obj.CustomerDEK, obj.CustomerDEKIV = dek, sidecar[storage.MetadataCustomerDEKIVField]
		return obj, classCustomerKey, nil
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

// hoist moves the store built in <outDB>/metadata up so outDB itself is the
// store directory (Pebble files + v2 sentinel), matching the server layout:
// activating it is exactly `mv <outDB> <data-dir>/metadata`. After a stop it
// finishes the move.
func hoist(outDB string) error {
	inner := filepath.Join(outDB, "metadata")
	staged := outDB + ".hoist"
	if _, err := os.Stat(staged); errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Stat(inner); errors.Is(err, fs.ErrNotExist) {
			if _, err := os.Stat(outDB); err != nil {
				return fmt.Errorf("the rebuilt store is gone from %s", outDB)
			}
			return nil
		}
		if err := os.Rename(inner, staged); err != nil {
			return fmt.Errorf("failed to stage rebuilt store: %w", err)
		}
		if err := stopping("staged"); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(outDB); err != nil {
		return fmt.Errorf("failed to clear rebuild scaffolding: %w", err)
	}
	if err := stopping("cleared"); err != nil {
		return err
	}
	if err := os.Rename(staged, outDB); err != nil {
		return fmt.Errorf("failed to finalise rebuilt store: %w", err)
	}
	return nil
}

// restoreKEKs writes the bundle's key versions into the auth SQLite so the
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
