package layout

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// listBuckets reads every bucket in the index, across all tenants.
func listBuckets(ctx context.Context, store metadata.Store) ([]bucketRef, error) {
	raw, ok := store.(metadata.RawKVStore)
	if !ok {
		return nil, fmt.Errorf("the metadata store does not expose raw access, which the migration needs to enumerate every tenant")
	}

	var buckets []bucketRef
	err := raw.RawScan(ctx, "bucket:", "", func(_ string, val []byte) bool {
		var bm metadata.BucketMetadata
		if json.Unmarshal(val, &bm) != nil || bm.Name == "" {
			return true
		}
		ref := bucketRef{name: bm.Name, tenantID: bm.TenantID, path: bm.Name}
		if bm.TenantID != "" {
			ref.path = bm.TenantID + "/" + bm.Name
		}
		buckets = append(buckets, ref)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("could not enumerate buckets: %w", err)
	}

	sort.Slice(buckets, func(i, j int) bool { return buckets[i].path < buckets[j].path })
	return buckets, nil
}

func isDone(ctx context.Context, store metadata.Store, bucketPath string) (bool, error) {
	raw, ok := store.(metadata.RawKVStore)
	if !ok {
		return false, nil
	}
	_, err := raw.GetRaw(ctx, progressKeyPrefix+bucketPath)
	if err == nil {
		return true, nil
	}
	if err == metadata.ErrNotFound {
		return false, nil
	}
	return false, err
}

func markDone(ctx context.Context, store metadata.Store, bucketPath string) error {
	raw, ok := store.(metadata.RawKVStore)
	if !ok {
		return nil
	}
	return raw.PutRaw(ctx, progressKeyPrefix+bucketPath, []byte("done"))
}

// collectDataFiles snapshots every object file under a bucket directory before
// anything moves, so files landing in their new place are not walked again.
func collectDataFiles(dir string) ([]string, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}

	var files []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if isInternalName(info.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func isInternalName(name string) bool {
	switch {
	case name == ".maxiofs-bucket", name == ".maxiofs-folder", name == ".maxiofs-layout",
		strings.HasSuffix(name, sidecarSuffix),
		strings.HasSuffix(name, stagingSuffix),
		strings.HasPrefix(name, ".tmp_"),
		strings.HasPrefix(name, ".metadata-tmp-"),
		strings.HasPrefix(name, "maxiofs-upload-"),
		strings.HasPrefix(name, "maxiofs-encmigrate"),
		strings.HasPrefix(name, "maxiofs-multipart-"):
		return true
	}
	return false
}

// splitV1Path reads an object key and version out of a path relative to the
// bucket directory, the way the previous layout encoded them.
func splitV1Path(rel string) (key, versionID string, ok bool) {
	if strings.HasPrefix(rel, ".versions/") {
		trimmed := strings.TrimPrefix(rel, ".versions/")
		slash := strings.LastIndex(trimmed, "/")
		if slash <= 0 {
			return "", "", false
		}
		return trimmed[:slash], trimmed[slash+1:], true
	}
	if rel == "" {
		return "", "", false
	}
	return rel, "", true
}

// v1Path is where a reference lived in the previous layout, relative to root.
func v1Path(ref storage.ObjectRef) string {
	if ref.VersionID == "" {
		return path.Join(ref.Bucket, ref.Key)
	}
	return path.Join(ref.Bucket, ".versions", ref.Key, ref.VersionID)
}

// movePair puts one object and its sidecar in their new place and only then
// removes the old pair. Returns false when the object was already migrated.
func movePair(oldFull, newFull string, ref storage.ObjectRef, dryRun bool) (bool, int64, error) {
	oldInfo, err := os.Stat(oldFull)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, err
	}
	if oldInfo.IsDir() {
		return false, 0, fmt.Errorf("a directory occupies the path of the object")
	}

	if _, err := os.Stat(newFull); err == nil {
		if !dryRun {
			return false, 0, removeOldPair(oldFull)
		}
		return false, 0, nil
	}

	sidecar, err := sidecarFor(oldFull, ref)
	if err != nil {
		return false, 0, err
	}

	if dryRun {
		return true, oldInfo.Size(), nil
	}

	if err := os.MkdirAll(filepath.Dir(newFull), 0750); err != nil {
		return false, 0, err
	}

	linked := true
	if err := os.Link(oldFull, newFull); err != nil {
		linked = false
		if copyErr := copyFile(oldFull, newFull); copyErr != nil {
			return false, 0, copyErr
		}
	}

	if err := verifyMoved(oldFull, newFull, oldInfo.Size(), linked); err != nil {
		os.Remove(newFull) //nolint:errcheck
		return false, 0, err
	}

	if err := writeSidecar(newFull+sidecarSuffix, sidecar); err != nil {
		os.Remove(newFull) //nolint:errcheck
		return false, 0, err
	}

	if err := removeOldPair(oldFull); err != nil {
		return false, 0, err
	}
	return true, oldInfo.Size(), nil
}

// verifyMoved confirms the replacement before the original is removed. A hard
// link shares the bytes, so its size is the whole check; a copy is hashed.
func verifyMoved(oldFull, newFull string, size int64, linked bool) error {
	info, err := os.Stat(newFull)
	if err != nil {
		return err
	}
	if info.Size() != size {
		return fmt.Errorf("moved file is %d bytes, expected %d", info.Size(), size)
	}
	if linked {
		return nil
	}

	oldSum, err := fileMD5(oldFull)
	if err != nil {
		return err
	}
	newSum, err := fileMD5(newFull)
	if err != nil {
		return err
	}
	if oldSum != newSum {
		return fmt.Errorf("copied file does not match the original")
	}
	return nil
}

func removeOldPair(oldFull string) error {
	os.Remove(oldFull + sidecarSuffix) //nolint:errcheck
	os.Remove(oldFull + stagingSuffix) //nolint:errcheck
	if err := os.Remove(oldFull); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// sidecarFor returns the sidecar to write next to the moved object: the
// existing one with the identity stamped in, or one derived from the bytes when
// the object never had a sidecar.
func sidecarFor(oldFull string, ref storage.ObjectRef) (map[string]string, error) {
	sidecar := map[string]string{}

	raw, err := os.ReadFile(oldFull + sidecarSuffix)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &sidecar); err != nil {
			return nil, fmt.Errorf("unreadable sidecar: %w", err)
		}
	case os.IsNotExist(err):
		info, sErr := os.Stat(oldFull)
		if sErr != nil {
			return nil, sErr
		}
		sum, hErr := fileMD5(oldFull)
		if hErr != nil {
			return nil, hErr
		}
		sidecar["size"] = fmt.Sprintf("%d", info.Size())
		sidecar["last_modified"] = fmt.Sprintf("%d", info.ModTime().Unix())
		sidecar["etag"] = sum
		sidecar[storage.MetadataGeneratedKey] = "true"
	default:
		return nil, err
	}

	sidecar[storage.MetadataBucketField] = ref.Bucket
	sidecar[storage.MetadataKeyField] = ref.Key
	if ref.VersionID == "" {
		delete(sidecar, storage.MetadataVersionField)
	} else {
		sidecar[storage.MetadataVersionField] = ref.VersionID
	}
	return sidecar, nil
}

// createFolderMarker gives a folder-marker object the empty file the previous
// layout never wrote for it.
func createFolderMarker(newFull string, ref storage.ObjectRef, dryRun bool) error {
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(newFull), 0750); err != nil {
		return err
	}
	if err := os.WriteFile(newFull, nil, 0640); err != nil {
		return err
	}
	return writeSidecar(newFull+sidecarSuffix, map[string]string{
		"size":                      "0",
		"etag":                      "d41d8cd98f00b204e9800998ecf8427e",
		"content-type":              "application/x-directory",
		storage.MetadataBucketField: ref.Bucket,
		storage.MetadataKeyField:    ref.Key,
	})
}

func writeSidecar(path string, sidecar map[string]string) error {
	data, err := json.Marshal(sidecar)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".metadata-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck

	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close() //nolint:errcheck
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close() //nolint:errcheck
		return err
	}
	return out.Close()
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck

	hasher := md5.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// pruneEmpty removes the emptied bucket directory, and the tenant directory
// above it once its last bucket is gone.
func pruneEmpty(dir, root string, log *logrus.Logger) {
	for dir != root && strings.HasPrefix(dir, root) {
		if !removeIfEmpty(dir) {
			log.WithField("path", dir).Debug("Left a directory behind that still holds something")
			return
		}
		dir = filepath.Dir(dir)
	}
}

// removeIfEmpty removes dir when nothing but the old layout's own artifacts is
// left under it, subdirectories included. It decides before it deletes, and only
// ever calls os.Remove, which refuses a directory that still holds anything.
func removeIfEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && !isInternalName(e.Name()) {
			return false
		}
	}
	for _, e := range entries {
		if e.IsDir() && !removeIfEmpty(filepath.Join(dir, e.Name())) {
			return false
		}
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return false
		}
	}
	return os.Remove(dir) == nil
}
