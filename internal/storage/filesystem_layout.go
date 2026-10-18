package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	bucketMarkerName     = ".maxiofs-bucket"
	layoutMarkerName     = ".maxiofs-layout"
	multipartPartsPrefix = ".maxiofs/multipart/parts"

	// LayoutVersion is the on-disk layout this build reads and writes.
	LayoutVersion = 2
)

// BucketDirName is the directory a bucket owns under the storage root. Bucket
// names are globally unique, so the tenant is not part of the path; the
// tenant-qualified path lives in the bucket marker and in every sidecar.
func BucketDirName(bucket string) string {
	if i := strings.LastIndex(bucket, "/"); i >= 0 {
		return bucket[i+1:]
	}
	return bucket
}

// RefPath maps an object reference onto its location under the storage root.
// The key never reaches the filesystem: the name is a digest, so no key can
// collide with another, with a prefix of itself, or with a name that differs
// only in case. A shard is beside its write's file, named after it.
func RefPath(ref ObjectRef) string {
	sum := sha256.Sum256(digestInput(ref))
	name := hex.EncodeToString(sum[:])
	p := path.Join(BucketDirName(ref.Bucket), name[0:2], name[2:4], name)
	if i, ok := ref.ShardIndex(); ok {
		p += shardSuffix + strconv.Itoa(i)
	}
	return p
}

const shardSuffix = ".shard-"

// IsShardFileName reports whether a file under a bucket is a shard's data.
func IsShardFileName(name string) bool {
	base, index, ok := strings.Cut(name, shardSuffix)
	if !ok || len(base) != sha256.Size*2 || index == "" {
		return false
	}
	for _, c := range index {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Shards lists the indexes of the shards stored for the write ref names.
func (fs *FilesystemBackend) Shards(ctx context.Context, ref ObjectRef) (_ []int, err error) {
	defer fs.observe("Shards", time.Now(), &err)
	ref = ref.Write()
	if err := validateRef(ref); err != nil {
		return nil, err
	}
	write := RefPath(ref)
	entries, err := os.ReadDir(fs.getFullPath(path.Dir(write)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, NewErrorWithCause("ReadDirectory", "Failed to list the shards of a write", err)
	}
	prefix := path.Base(write) + shardSuffix
	var shards []int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix) || !IsShardFileName(name) {
			continue
		}
		if i, err := strconv.Atoi(name[len(prefix):]); err == nil {
			shards = append(shards, i)
		}
	}
	slices.Sort(shards)
	return shards, nil
}

// ObjectPath returns where an object lives under the storage root.
func (fs *FilesystemBackend) ObjectPath(ref ObjectRef) string {
	return RefPath(ref)
}

// WriteBucketMarker records a bucket's tenant-qualified path in its directory.
func WriteBucketMarker(root, bucket string) error {
	dir := filepath.Join(root, BucketDirName(bucket))
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, bucketMarkerName), []byte(bucket), 0640); err != nil {
		return err
	}
	return syncDir(dir)
}

func digestInput(ref ObjectRef) []byte {
	if ref.VersionID == "" {
		return []byte(ref.Key)
	}
	return []byte(ref.Key + "\x00" + ref.VersionID)
}

func partPath(uploadID string, partNumber int) string {
	return fmt.Sprintf("%s/%s/%05d", multipartPartsPrefix, uploadID, partNumber)
}

func (fs *FilesystemBackend) Put(ctx context.Context, ref ObjectRef, data io.Reader, metadata map[string]string) (err error) {
	defer fs.observe("Put", time.Now(), &err)
	if err := validateRef(ref); err != nil {
		return err
	}
	return fs.putAt(ctx, RefPath(ref), data, withIdentity(metadata, ref))
}

func (fs *FilesystemBackend) Get(ctx context.Context, ref ObjectRef) (_ io.ReadCloser, _ map[string]string, err error) {
	defer fs.observe("Get", time.Now(), &err)
	if err := validateRef(ref); err != nil {
		return nil, nil, err
	}
	return fs.getAt(ctx, RefPath(ref))
}

func (fs *FilesystemBackend) Delete(ctx context.Context, ref ObjectRef) (err error) {
	defer fs.observe("Delete", time.Now(), &err)
	if err := validateRef(ref); err != nil {
		return err
	}
	return fs.deleteAt(ctx, RefPath(ref))
}

func (fs *FilesystemBackend) Exists(ctx context.Context, ref ObjectRef) (_ bool, err error) {
	defer fs.observe("Exists", time.Now(), &err)
	if err := validateRef(ref); err != nil {
		return false, err
	}
	return fs.existsAt(ctx, RefPath(ref))
}

func (fs *FilesystemBackend) GetMetadata(ctx context.Context, ref ObjectRef) (_ map[string]string, err error) {
	defer fs.observe("GetMetadata", time.Now(), &err)
	if err := validateRef(ref); err != nil {
		return nil, err
	}
	return fs.metadataAt(ctx, RefPath(ref))
}

func (fs *FilesystemBackend) SetMetadata(ctx context.Context, ref ObjectRef, metadata map[string]string) (err error) {
	defer fs.observe("SetMetadata", time.Now(), &err)
	if err := validateRef(ref); err != nil {
		return err
	}
	return fs.setMetadataAt(ctx, RefPath(ref), withIdentity(metadata, ref))
}

func validateRef(ref ObjectRef) error {
	if ref.Key == "" {
		return ErrInvalidPath
	}
	return validateBucket(ref.Bucket)
}

func validateBucket(bucket string) error {
	name := BucketDirName(bucket)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `\/`) {
		return ErrInvalidPath
	}
	return nil
}

func withIdentity(metadata map[string]string, ref ObjectRef) map[string]string {
	if metadata == nil {
		metadata = make(map[string]string, 3)
	}
	metadata[MetadataBucketField] = ref.Bucket
	metadata[MetadataKeyField] = ref.Key
	if ref.VersionID == "" {
		delete(metadata, MetadataVersionField)
	} else {
		metadata[MetadataVersionField] = ref.VersionID
	}
	if i, ok := ref.ShardIndex(); ok {
		metadata[MetadataShardField] = strconv.Itoa(i)
	} else {
		delete(metadata, MetadataShardField)
	}
	return metadata
}

func (fs *FilesystemBackend) PutPart(ctx context.Context, uploadID string, partNumber int, data io.Reader, metadata map[string]string) (err error) {
	defer fs.observe("PutPart", time.Now(), &err)
	return fs.putAt(ctx, partPath(uploadID, partNumber), data, metadata)
}

func (fs *FilesystemBackend) GetPart(ctx context.Context, uploadID string, partNumber int) (_ io.ReadCloser, _ map[string]string, err error) {
	defer fs.observe("GetPart", time.Now(), &err)
	return fs.getAt(ctx, partPath(uploadID, partNumber))
}

func (fs *FilesystemBackend) PartMetadata(ctx context.Context, uploadID string, partNumber int) (_ map[string]string, err error) {
	defer fs.observe("PartMetadata", time.Now(), &err)
	return fs.metadataAt(ctx, partPath(uploadID, partNumber))
}

func (fs *FilesystemBackend) PartExists(ctx context.Context, uploadID string, partNumber int) (_ bool, err error) {
	defer fs.observe("PartExists", time.Now(), &err)
	return fs.existsAt(ctx, partPath(uploadID, partNumber))
}

func (fs *FilesystemBackend) DeletePart(ctx context.Context, uploadID string, partNumber int) (err error) {
	defer fs.observe("DeletePart", time.Now(), &err)
	return fs.deleteAt(ctx, partPath(uploadID, partNumber))
}

// CreateBucket records the tenant-qualified bucket path in the marker: the
// directory name alone cannot carry it.
func (fs *FilesystemBackend) uploadDir(uploadID string) string {
	return filepath.Join(fs.rootPath, filepath.FromSlash(multipartPartsPrefix), uploadID)
}

func (fs *FilesystemBackend) ListUploads(ctx context.Context) (_ []string, err error) {
	defer fs.observe("ListUploads", time.Now(), &err)
	entries, err := os.ReadDir(filepath.Join(fs.rootPath, filepath.FromSlash(multipartPartsPrefix)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, NewErrorWithCause("ListUploads", "Failed to read the multipart directory", err)
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

func (fs *FilesystemBackend) DeleteUpload(ctx context.Context, uploadID string) (err error) {
	defer fs.observe("DeleteUpload", time.Now(), &err)
	if uploadID == "" || uploadID == "." || uploadID == ".." || strings.ContainsAny(uploadID, `\/`) {
		return ErrInvalidPath
	}
	if err := os.RemoveAll(fs.uploadDir(uploadID)); err != nil {
		return NewErrorWithCause("DeleteUpload", "Failed to remove the upload directory", err)
	}
	return nil
}

func (fs *FilesystemBackend) CreateBucket(ctx context.Context, bucket string) (err error) {
	defer fs.observe("CreateBucket", time.Now(), &err)
	if err := validateBucket(bucket); err != nil {
		return err
	}

	dir := fs.getFullPath(BucketDirName(bucket))
	if err := os.MkdirAll(dir, 0750); err != nil {
		return NewErrorWithCause("CreateBucketDir", "Failed to create bucket directory", err)
	}

	if err := os.WriteFile(filepath.Join(dir, bucketMarkerName), []byte(bucket), 0640); err != nil {
		return NewErrorWithCause("CreateBucketMarker", "Failed to write bucket marker", err)
	}

	return syncDir(dir)
}

func (fs *FilesystemBackend) DeleteBucket(ctx context.Context, bucket string) (err error) {
	defer fs.observe("DeleteBucket", time.Now(), &err)
	if err := validateBucket(bucket); err != nil {
		return err
	}
	return fs.RemoveDirectory(BucketDirName(bucket))
}

// ReadLayoutVersion reports the layout recorded at the storage root. A root
// with no marker is layout 1 when it already holds buckets, and a fresh root
// otherwise.
func ReadLayoutVersion(root string) (version int, fresh bool, err error) {
	data, readErr := os.ReadFile(filepath.Join(root, layoutMarkerName))
	if readErr == nil {
		v, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if convErr != nil {
			return 0, false, fmt.Errorf("unreadable layout marker at %s", root)
		}
		return v, false, nil
	}
	if !os.IsNotExist(readErr) {
		return 0, false, readErr
	}

	found, inferred, dirErr := inferLayoutFromBuckets(root)
	if dirErr != nil {
		return 0, false, dirErr
	}
	if !found {
		return LayoutVersion, true, nil
	}
	return inferred, false, nil
}

// inferLayoutFromBuckets reads the layout from the bucket markers themselves,
// for a root whose layout marker is missing. An empty marker is the previous
// layout; the current one writes the bucket path into it. The oldest layout
// found wins, so a half-migrated root still reports as needing migration.
func inferLayoutFromBuckets(root string) (found bool, version int, err error) {
	version = LayoutVersion

	inspect := func(dir string) bool {
		content, readErr := os.ReadFile(filepath.Join(dir, bucketMarkerName))
		if readErr != nil {
			return false
		}
		found = true
		if len(strings.TrimSpace(string(content))) == 0 {
			version = 1
		}
		return true
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return false, 0, err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if inspect(dir) {
			continue
		}
		// Not a bucket: in the previous layout this is a tenant directory.
		sub, subErr := os.ReadDir(dir)
		if subErr != nil {
			continue
		}
		for _, se := range sub {
			if se.IsDir() {
				inspect(filepath.Join(dir, se.Name()))
			}
		}
	}
	return found, version, nil
}

func WriteLayoutVersion(root string, version int) error {
	if err := os.WriteFile(filepath.Join(root, layoutMarkerName), []byte(strconv.Itoa(version)), 0640); err != nil {
		return err
	}
	return syncDir(root)
}
