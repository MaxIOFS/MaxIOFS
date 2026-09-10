package layout

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// v1Tree builds a storage root in the previous layout: the key is the path, and
// the bucket marker is an empty file.
type v1Tree struct {
	t     *testing.T
	root  string
	store metadata.Store
}

func newV1Tree(t *testing.T) (*v1Tree, func()) {
	t.Helper()
	dataDir, err := os.MkdirTemp("", "layout-migrate-*")
	require.NoError(t, err)

	root := filepath.Join(dataDir, "objects")
	require.NoError(t, os.MkdirAll(root, 0o750))

	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: dataDir, WALSyncInterval: -1})
	require.NoError(t, err)

	return &v1Tree{t: t, root: root, store: store}, func() {
		store.Close()         //nolint:errcheck
		os.RemoveAll(dataDir) //nolint:errcheck
	}
}

func (tr *v1Tree) bucket(bucketPath, tenantID, name string, versioned bool) {
	tr.t.Helper()
	dir := filepath.Join(tr.root, filepath.FromSlash(bucketPath))
	require.NoError(tr.t, os.MkdirAll(dir, 0o750))
	require.NoError(tr.t, os.WriteFile(filepath.Join(dir, ".maxiofs-bucket"), nil, 0o640))

	bm := &metadata.BucketMetadata{Name: name, TenantID: tenantID, OwnerID: "u"}
	if versioned {
		bm.Versioning = &metadata.VersioningMetadata{Enabled: true, Status: "Enabled"}
	}
	require.NoError(tr.t, tr.store.CreateBucket(context.Background(), bm))
}

// object writes the data file and sidecar where the previous layout put them,
// and registers the object in the index.
func (tr *v1Tree) object(bucketPath, key, versionID, content string) {
	tr.t.Helper()
	ctx := context.Background()

	rel := v1Path(storage.ObjectRef{Bucket: bucketPath, Key: key, VersionID: versionID})
	full := filepath.Join(tr.root, filepath.FromSlash(rel))
	require.NoError(tr.t, os.MkdirAll(filepath.Dir(full), 0o750))
	require.NoError(tr.t, os.WriteFile(full, []byte(content), 0o640))

	sidecar := map[string]string{
		"size":          strconv.Itoa(len(content)),
		"etag":          "etag-" + key,
		"last_modified": strconv.FormatInt(time.Now().Unix(), 10),
		"content-type":  "text/plain",
	}
	data, err := json.Marshal(sidecar)
	require.NoError(tr.t, err)
	require.NoError(tr.t, os.WriteFile(full+".metadata", data, 0o640))

	obj := &metadata.ObjectMetadata{
		Bucket: bucketPath, Key: key, VersionID: versionID,
		Size: int64(len(content)), ETag: "etag-" + key, LastModified: time.Now(),
	}
	if versionID == "" {
		require.NoError(tr.t, tr.store.PutObject(ctx, obj))
		return
	}
	require.NoError(tr.t, tr.store.PutObjectVersion(ctx, obj, &metadata.ObjectVersion{
		VersionID: versionID, IsLatest: true, Key: key,
		Size: int64(len(content)), ETag: "etag-" + key, LastModified: time.Now(),
	}))
}

// folderMarker registers a folder marker the way the previous layout did: an
// index entry with no data file behind it.
func (tr *v1Tree) folderMarker(bucketPath, key string) {
	tr.t.Helper()
	require.NoError(tr.t, tr.store.PutObject(context.Background(), &metadata.ObjectMetadata{
		Bucket: bucketPath, Key: key, Size: 0,
		ETag: "d41d8cd98f00b204e9800998ecf8427e", LastModified: time.Now(),
	}))
}

// bucketRaw writes a bucket entry straight into the index, bypassing the
// uniqueness the store enforces, to reproduce a tree that predates it.
func (tr *v1Tree) bucketRaw(bucketPath, tenantID, name string) {
	tr.t.Helper()
	dir := filepath.Join(tr.root, filepath.FromSlash(bucketPath))
	require.NoError(tr.t, os.MkdirAll(dir, 0o750))
	require.NoError(tr.t, os.WriteFile(filepath.Join(dir, ".maxiofs-bucket"), nil, 0o640))

	raw, ok := tr.store.(metadata.RawKVStore)
	require.True(tr.t, ok)
	value, err := json.Marshal(&metadata.BucketMetadata{Name: name, TenantID: tenantID, OwnerID: "u"})
	require.NoError(tr.t, err)
	require.NoError(tr.t, raw.PutRaw(context.Background(), "bucket:"+tenantID+":"+name, value))
}

func (tr *v1Tree) migrate(dryRun bool) (*Report, error) {
	return Migrate(context.Background(), Options{
		Root: tr.root, Store: tr.store, Logger: logrus.StandardLogger(), DryRun: dryRun,
	})
}

// backend opens the storage backend over the migrated root, which only succeeds
// on the current layout.
func (tr *v1Tree) backend() *storage.FilesystemBackend {
	tr.t.Helper()
	fs, err := storage.NewFilesystemBackend(storage.Config{Root: tr.root})
	require.NoError(tr.t, err)
	return fs
}

func readObject(t *testing.T, fs *storage.FilesystemBackend, ref storage.ObjectRef) string {
	t.Helper()
	rc, _, err := fs.Get(context.Background(), ref)
	require.NoError(t, err)
	defer rc.Close() //nolint:errcheck
	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	return string(body)
}

func TestMigrateMovesEveryObjectAndVersion(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("global", "", "global", false)
	tr.object("global", "flat.txt", "", "flat content")
	tr.object("global", "docs/nested/deep.bin", "", "nested content")
	tr.folderMarker("global", "2026/")
	tr.object("global", "2026/enero/informe.pdf", "", "under the prefix")

	tr.bucket("tenant-x/facturas", "tenant-x", "facturas", true)
	tr.object("tenant-x/facturas", "doc.txt", "1700000000.aaaa1111", "version one")
	tr.object("tenant-x/facturas", "doc.txt", "1700000001.bbbb2222", "version two")

	report, err := tr.migrate(false)
	require.NoError(t, err)
	require.Empty(t, report.Failures)
	require.Empty(t, report.MissingData)
	require.Equal(t, 2, report.Buckets)
	require.Equal(t, 3, report.ObjectsMoved)
	require.Equal(t, 1, report.MarkersCreated)
	require.Equal(t, 2, report.VersionsMoved)

	fs := tr.backend()
	require.Equal(t, "flat content", readObject(t, fs, storage.ObjectRef{Bucket: "global", Key: "flat.txt"}))
	require.Equal(t, "nested content", readObject(t, fs, storage.ObjectRef{Bucket: "global", Key: "docs/nested/deep.bin"}))
	require.Equal(t, "", readObject(t, fs, storage.ObjectRef{Bucket: "global", Key: "2026/"}))
	require.Equal(t, "under the prefix", readObject(t, fs, storage.ObjectRef{Bucket: "global", Key: "2026/enero/informe.pdf"}))
	require.Equal(t, "version one", readObject(t, fs, storage.ObjectRef{Bucket: "tenant-x/facturas", Key: "doc.txt", VersionID: "1700000000.aaaa1111"}))
	require.Equal(t, "version two", readObject(t, fs, storage.ObjectRef{Bucket: "tenant-x/facturas", Key: "doc.txt", VersionID: "1700000001.bbbb2222"}))

	// The tenant directory is gone and the bucket sits at the root.
	_, err = os.Stat(filepath.Join(tr.root, "tenant-x"))
	require.True(t, os.IsNotExist(err), "the tenant directory should be gone")

	marker, err := os.ReadFile(filepath.Join(tr.root, "facturas", ".maxiofs-bucket"))
	require.NoError(t, err)
	require.Equal(t, "tenant-x/facturas", string(marker))

	// No file is left where the old layout kept it.
	_, err = os.Stat(filepath.Join(tr.root, "global", "flat.txt"))
	require.True(t, os.IsNotExist(err))

	version, err := os.ReadFile(filepath.Join(tr.root, ".maxiofs-layout"))
	require.NoError(t, err)
	require.Equal(t, "2", string(version))
}

func TestMigrateIsIdempotent(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("global", "", "global", false)
	tr.object("global", "a.txt", "", "content")

	first, err := tr.migrate(false)
	require.NoError(t, err)
	require.Equal(t, 1, first.ObjectsMoved)

	second, err := tr.migrate(false)
	require.NoError(t, err)
	require.True(t, second.AlreadyCurrent)
	require.Zero(t, second.ObjectsMoved)

	fs := tr.backend()
	require.Equal(t, "content", readObject(t, fs, storage.ObjectRef{Bucket: "global", Key: "a.txt"}))
}

func TestMigrateResumesAfterAnInterruptedRun(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("done-bucket", "", "done-bucket", false)
	tr.object("done-bucket", "a.txt", "", "first")
	tr.bucket("pending-bucket", "", "pending-bucket", false)
	tr.object("pending-bucket", "b.txt", "", "second")

	// Simulate a run that finished the first bucket and stopped.
	require.NoError(t, markDone(context.Background(), tr.store, "done-bucket"))

	report, err := tr.migrate(false)
	require.NoError(t, err)
	require.Equal(t, 1, report.BucketsSkipped)
	require.Equal(t, 1, report.ObjectsMoved)

	// The skipped bucket keeps its files exactly where they were.
	body, err := os.ReadFile(filepath.Join(tr.root, "done-bucket", "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "first", string(body))
}

func TestMigrateRefusesDuplicateBucketNames(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucketRaw("tenant-a/backups", "tenant-a", "backups")
	tr.object("tenant-a/backups", "a.txt", "", "from tenant a")
	tr.bucketRaw("tenant-b/backups", "tenant-b", "backups")
	tr.object("tenant-b/backups", "b.txt", "", "from tenant b")

	_, err := tr.migrate(false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tenant-a/backups")
	require.Contains(t, err.Error(), "tenant-b/backups")

	// Nothing moved.
	for _, p := range []string{
		filepath.Join(tr.root, "tenant-a", "backups", "a.txt"),
		filepath.Join(tr.root, "tenant-b", "backups", "b.txt"),
	} {
		_, statErr := os.Stat(p)
		require.NoError(t, statErr, "a refused migration must leave every file in place")
	}
}

func TestMigrateRefusesABucketNamedLikeATenant(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("acme", "", "acme", false)
	tr.object("acme", "data/report.txt", "", "global bucket object")
	tr.bucket("acme/data", "acme", "data", false)
	tr.object("acme/data", "report.txt", "", "tenant bucket object")

	_, err := tr.migrate(false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "also a tenant ID")
}

func TestMigrateGivesAFolderMarkerItsFile(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("global", "", "global", false)

	// The previous layout recorded folder markers in the index without ever
	// writing a data file for them.
	require.NoError(t, tr.store.PutObject(context.Background(), &metadata.ObjectMetadata{
		Bucket: "global", Key: "photos/", Size: 0,
		ETag: "d41d8cd98f00b204e9800998ecf8427e", LastModified: time.Now(),
	}))

	report, err := tr.migrate(false)
	require.NoError(t, err)
	require.Equal(t, 1, report.MarkersCreated)
	require.Empty(t, report.MissingData)

	fs := tr.backend()
	require.Equal(t, "", readObject(t, fs, storage.ObjectRef{Bucket: "global", Key: "photos/"}))
}

func TestMigrateReportsAnIndexedObjectWithNoData(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("global", "", "global", false)
	require.NoError(t, tr.store.PutObject(context.Background(), &metadata.ObjectMetadata{
		Bucket: "global", Key: "vanished.bin", Size: 10, ETag: "etag", LastModified: time.Now(),
	}))

	report, err := tr.migrate(false)
	require.NoError(t, err)
	require.Equal(t, []string{"global/vanished.bin"}, report.MissingData)
}

func TestMigrateReportsAnObjectWhosePathIsADirectory(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("global", "", "global", false)

	// What the folder-marker bug left behind: the index still names the object
	// while a directory occupies its path.
	require.NoError(t, os.MkdirAll(filepath.Join(tr.root, "global", "report"), 0o750))
	require.NoError(t, tr.store.PutObject(context.Background(), &metadata.ObjectMetadata{
		Bucket: "global", Key: "report", Size: 31, ETag: "etag", LastModified: time.Now(),
	}))

	report, err := tr.migrate(false)
	require.NoError(t, err)
	require.Len(t, report.Damaged, 1)
	require.Contains(t, report.Damaged[0], "global/report")

	_, err = os.Stat(filepath.Join(tr.root, "global", "report"))
	require.NoError(t, err, "a damaged object must be reported, not removed")
}

func TestMigrateDryRunTouchesNothing(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("global", "", "global", false)
	tr.object("global", "a.txt", "", "content")

	report, err := tr.migrate(true)
	require.NoError(t, err)
	require.True(t, report.DryRun)
	require.Equal(t, 1, report.ObjectsMoved)

	body, err := os.ReadFile(filepath.Join(tr.root, "global", "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "content", string(body))

	_, err = os.Stat(filepath.Join(tr.root, ".maxiofs-layout"))
	require.True(t, os.IsNotExist(err), "a dry run must not record the new layout")
}

func TestMigrateKeepsAnObjectThatHasNoSidecar(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("global", "", "global", false)
	tr.object("global", "legacy.bin", "", "legacy bytes")
	require.NoError(t, os.Remove(filepath.Join(tr.root, "global", "legacy.bin.metadata")))

	report, err := tr.migrate(false)
	require.NoError(t, err)
	require.Empty(t, report.Failures)

	fs := tr.backend()
	ref := storage.ObjectRef{Bucket: "global", Key: "legacy.bin"}
	require.Equal(t, "legacy bytes", readObject(t, fs, ref))

	meta, err := fs.GetMetadata(context.Background(), ref)
	require.NoError(t, err)
	require.Equal(t, "legacy.bin", meta[storage.MetadataKeyField])
	require.Equal(t, "global", meta[storage.MetadataBucketField])
}

func TestMigrateDryRunDoesNotReportObjectsStillInPlace(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("global", "", "global", false)
	tr.object("global", "a.txt", "", "content")
	tr.object("global", "docs/b.txt", "", "nested")
	tr.object("global", "v.txt", "1700000000.aaaa1111", "versioned")

	report, err := tr.migrate(true)
	require.NoError(t, err)
	require.Empty(t, report.MissingData,
		"a dry run moves nothing, so an object still in its old place is not missing")
	require.Empty(t, report.Damaged)
}

func TestMigrateReportsBucketDirectoriesTheIndexDoesNotKnow(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("tenant-x/live", "tenant-x", "live", false)
	tr.object("tenant-x/live", "a.txt", "", "content")

	// What a failed bucket deletion leaves: the marker removed, the files not.
	orphan := filepath.Join(tr.root, "tenant-x", "deleted-bucket")
	require.NoError(t, os.MkdirAll(orphan, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(orphan, ".maxiofs-folder"), nil, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(orphan, "left-behind.bin"), []byte("data"), 0o640))

	report, err := tr.migrate(false)
	require.NoError(t, err)
	require.Equal(t, []string{"tenant-x/deleted-bucket (1 files)"}, report.Stranded)

	// It is reported, never touched.
	body, err := os.ReadFile(filepath.Join(orphan, "left-behind.bin"))
	require.NoError(t, err)
	require.Equal(t, "data", string(body))
}

func TestMigratePrunesTheOldTreeThroughEmptyDirectories(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("tenant-x/facturas", "tenant-x", "facturas", true)
	tr.object("tenant-x/facturas", "docs/deep/a.txt", "1700000000.aaaa1111", "versioned")

	_, err := tr.migrate(false)
	require.NoError(t, err)

	// The bucket directory held .versions and a key path; both are gone with it.
	_, err = os.Stat(filepath.Join(tr.root, "tenant-x"))
	require.True(t, os.IsNotExist(err), "the tenant directory should be gone")
}

func TestMigrateClearsTheOldLayoutArtifacts(t *testing.T) {
	tr, cleanup := newV1Tree(t)
	defer cleanup()

	tr.bucket("global", "", "global", false)
	tr.object("global", "a.txt", "", "content")
	require.NoError(t, os.WriteFile(filepath.Join(tr.root, "global", ".maxiofs-bucket.metadata"), []byte("{}"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(tr.root, "global", ".maxiofs-folder"), nil, 0o640))

	_, err := tr.migrate(false)
	require.NoError(t, err)

	for _, stale := range []string{".maxiofs-bucket.metadata", ".maxiofs-folder"} {
		_, err := os.Stat(filepath.Join(tr.root, "global", stale))
		require.True(t, os.IsNotExist(err), "%s should not survive the migration", stale)
	}
	marker, err := os.ReadFile(filepath.Join(tr.root, "global", ".maxiofs-bucket"))
	require.NoError(t, err)
	require.Equal(t, "global", string(marker))
}
