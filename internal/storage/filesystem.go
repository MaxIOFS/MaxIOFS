package storage

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// metadataStagingSuffix is appended to a sidecar path to form the STAGED
const metadataStagingSuffix = "-staging" // full staged name: <object>.metadata-staging

const folderMarkerName = ".maxiofs-folder"

// pathLockShards is the number of striped mutexes serialising per-path
// commit/repair sections. Collisions only serialise unrelated paths briefly.
const pathLockShards = 256

// FilesystemBackend implements the Backend interface for local filesystem storage
type FilesystemBackend struct {
	rootPath  string
	config    Config
	pathLocks [pathLockShards]sync.Mutex
}

// NewFilesystemBackend creates a new filesystem storage backend
func NewFilesystemBackend(config Config) (*FilesystemBackend, error) {
	if err := os.MkdirAll(config.Root, 0750); err != nil {
		return nil, NewErrorWithCause("CreateRootDir", "Failed to create root directory", err)
	}

	version, fresh, err := ReadLayoutVersion(config.Root)
	if err != nil {
		return nil, NewErrorWithCause("ReadLayout", "Failed to read the storage layout marker", err)
	}
	switch {
	case fresh:
		if err := WriteLayoutVersion(config.Root, LayoutVersion); err != nil {
			return nil, NewErrorWithCause("WriteLayout", "Failed to write the storage layout marker", err)
		}
	case version < LayoutVersion:
		return nil, NewError("LayoutTooOld", fmt.Sprintf(
			"storage layout v%d found at %s; this build reads v%d — run the migration",
			version, config.Root, LayoutVersion))
	case version > LayoutVersion:
		return nil, NewError("LayoutTooNew", fmt.Sprintf(
			"storage layout v%d found at %s; this build reads v%d — upgrade MaxIOFS",
			version, config.Root, LayoutVersion))
	}

	backend := &FilesystemBackend{
		rootPath: config.Root,
		config:   config,
	}

	return backend, nil
}

// GetRootPath returns the root path of the filesystem backend
func (fs *FilesystemBackend) GetRootPath() string {
	return fs.rootPath
}

// Put stores an object in the filesystem
func (fs *FilesystemBackend) putAt(ctx context.Context, path string, data io.Reader, metadata map[string]string) error {
	if err := fs.validatePath(path); err != nil {
		return err
	}

	fullPath := fs.getFullPath(path)

	// Create directory if it doesn't exist
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return NewErrorWithCause("CreateDirectory", "Failed to create directory", err)
	}

	// Create temporary file
	tempFile, err := os.CreateTemp(dir, ".tmp_")
	if err != nil {
		return NewErrorWithCause("CreateTempFile", "Failed to create temporary file", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Copy data and calculate hash
	hasher := md5.New()
	multiWriter := io.MultiWriter(tempFile, hasher)

	size, err := io.Copy(multiWriter, data)
	if err != nil {
		return NewErrorWithCause("WriteData", "Failed to write data", err)
	}

	if err := tempFile.Sync(); err != nil {
		return NewErrorWithCause("SyncData", "Failed to flush data to disk", err)
	}

	// Close is where a delayed write error surfaces; discarding it reported a
	// successful write for data that never landed.
	if err := tempFile.Close(); err != nil {
		return NewErrorWithCause("CloseData", "Failed to close the data file", err)
	}

	// Add calculated metadata
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata["size"] = fmt.Sprintf("%d", size)
	metadata["etag"] = hex.EncodeToString(hasher.Sum(nil))
	metadata["last_modified"] = fmt.Sprintf("%d", time.Now().Unix())

	unlock := fs.lockPath(path)
	defer unlock()
	fs.repairStagedCommit(path)

	metadataTempPath, err := fs.prepareMetadataTemp(path, metadata)
	if err != nil {
		return err
	}
	defer os.Remove(metadataTempPath)

	stagingPath := fs.getStagingMetadataPath(path)
	if err := os.Rename(metadataTempPath, stagingPath); err != nil {
		return NewErrorWithCause("StageMetadata", "Failed to stage metadata file", err)
	}

	if err := os.Rename(tempFile.Name(), fullPath); err != nil {
		os.Remove(stagingPath) // old pair stays fully intact
		return NewErrorWithCause("AtomicMove", "Failed to move file to final location", err)
	}

	if err := os.Rename(stagingPath, fs.getMetadataPath(path)); err != nil {
		// Data is committed; leave the stage in place so the read-path repair
		// rolls the metadata commit forward as soon as the rename can succeed.
		return NewErrorWithCause("AtomicMetadataMove", "Failed to move metadata file to final location", err)
	}

	if err := syncDir(dir); err != nil {
		return NewErrorWithCause("SyncDirectory", "Failed to flush the directory entry to disk", err)
	}

	return nil
}

// Get retrieves an object from the filesystem
func (fs *FilesystemBackend) getAt(ctx context.Context, path string) (io.ReadCloser, map[string]string, error) {
	if err := fs.validatePath(path); err != nil {
		return nil, nil, err
	}

	// Resolve any staged sidecar left by a crashed Put before serving.
	fs.maybeRepair(path)

	fullPath := fs.getFullPath(path)

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return nil, nil, ErrObjectNotFound
	} else if err != nil {
		return nil, nil, NewErrorWithCause("StatFile", "Failed to stat file", err)
	}

	// Open file
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, nil, NewErrorWithCause("OpenFile", "Failed to open file", err)
	}

	// Get metadata
	metadata, err := fs.metadataAt(ctx, path)
	if err != nil {
		file.Close()
		return nil, nil, err
	}

	return file, metadata, nil
}

// Delete removes an object from the filesystem
func (fs *FilesystemBackend) deleteAt(ctx context.Context, path string) error {
	if err := fs.validatePath(path); err != nil {
		return err
	}

	unlock := fs.lockPath(path)
	defer unlock()

	fullPath := fs.getFullPath(path)

	// Check if file exists
	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return ErrObjectNotFound
	} else if err != nil {
		return NewErrorWithCause("StatFile", "Failed to stat file", err)
	}

	if info.IsDir() {
		return ErrObjectNotFound
	}

	// On Windows a just-written file may be briefly held by an external
	var rmErr error
	for attempt := 0; attempt < 5; attempt++ {
		if rmErr = os.Remove(fullPath); rmErr == nil {
			break
		}
		time.Sleep(time.Duration(10*(attempt+1)) * time.Millisecond)
	}
	if rmErr != nil {
		return NewErrorWithCause("DeleteFile", "Failed to delete file", rmErr)
	}

	// Delete metadata (and any staged sidecar from a crashed Put)
	metadataPath := fs.getMetadataPath(path)
	if _, err := os.Stat(metadataPath); err == nil {
		os.Remove(metadataPath) // Ignore errors for metadata cleanup
	}
	os.Remove(fs.getStagingMetadataPath(path)) //nolint:errcheck

	return nil
}

// Exists checks if an object exists in the filesystem
func (fs *FilesystemBackend) existsAt(ctx context.Context, path string) (bool, error) {
	if err := fs.validatePath(path); err != nil {
		return false, err
	}

	fullPath := fs.getFullPath(path)
	_, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, NewErrorWithCause("StatFile", "Failed to stat file", err)
	}

	return true, nil
}

// isTransientArtifact reports whether a file is work in progress or the remains
// of a crashed one, rather than an object.
func isTransientArtifact(name string) bool {
	switch {
	case name == ".maxiofs-bucket" || name == ".maxiofs-folder",
		strings.HasSuffix(name, metadataStagingSuffix),
		strings.HasPrefix(name, ".tmp_"),
		strings.HasPrefix(name, ".metadata-tmp-"),
		strings.HasPrefix(name, "maxiofs-upload-"),
		strings.HasPrefix(name, "maxiofs-encmigrate"),
		strings.HasPrefix(name, "maxiofs-multipart-"):
		return true
	}
	return false
}

// List returns every object under one bucket, versions included. The identity
// comes from each sidecar; a data file whose sidecar is missing cannot be named
// and is left out.
func (fs *FilesystemBackend) List(ctx context.Context, bucket string) ([]ObjectInfo, error) {
	if err := validateBucket(bucket); err != nil {
		return nil, err
	}

	root := fs.getFullPath(BucketDirName(bucket))
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}

	var objects []ObjectInfo
	var walkErr error

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if !os.IsNotExist(err) {
				walkErr = err
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}

		name := filepath.Base(path)
		if strings.HasSuffix(name, ".metadata") || strings.HasSuffix(name, ".metadata"+metadataStagingSuffix) {
			return nil
		}
		if isTransientArtifact(name) {
			return nil
		}

		sidecar, mErr := fs.readSidecarAt(path)
		if mErr != nil || sidecar[MetadataKeyField] == "" {
			return nil
		}

		ref := ObjectRef{
			Bucket:    sidecar[MetadataBucketField],
			Key:       sidecar[MetadataKeyField],
			VersionID: sidecar[MetadataVersionField],
		}
		if ref.Bucket == "" {
			ref.Bucket = bucket
		}

		objects = append(objects, ObjectInfo{
			Ref:          ref,
			Size:         info.Size(),
			LastModified: info.ModTime().Unix(),
			ETag:         sidecar["etag"],
			Metadata:     sidecar,
		})
		return nil
	})

	if err != nil {
		return nil, NewErrorWithCause("WalkDirectory", "Failed to walk directory", err)
	}
	if walkErr != nil {
		return nil, NewErrorWithCause("WalkDirectory", "Failed to read part of the tree", walkErr)
	}

	return objects, nil
}

// readSidecarAt reads the sidecar next to an absolute data-file path.
func (fs *FilesystemBackend) readSidecarAt(dataPath string) (map[string]string, error) {
	data, err := os.ReadFile(dataPath + ".metadata")
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (fs *FilesystemBackend) metadataAt(ctx context.Context, path string) (map[string]string, error) {
	if err := fs.validatePath(path); err != nil {
		return nil, err
	}

	// Resolve any staged sidecar left by a crashed Put before reading.
	fs.maybeRepair(path)

	metadataPath := fs.getMetadataPath(path)

	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return fs.generateBasicMetadata(path)
	}

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, NewErrorWithCause("ReadMetadata", "Failed to read metadata file", err)
	}

	var metadata map[string]string
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, NewErrorWithCause("ParseMetadata", "Failed to parse metadata", err)
	}

	return metadata, nil
}

func (fs *FilesystemBackend) setMetadataAt(ctx context.Context, path string, metadata map[string]string) error {
	if err := fs.validatePath(path); err != nil {
		return err
	}

	// Resolve any staged sidecar first — a pending roll-forward applied AFTER
	// this write would silently clobber the metadata being set here.
	unlock := fs.lockPath(path)
	defer unlock()
	fs.repairStagedCommit(path)

	return fs.saveMetadata(path, metadata)
}

func (fs *FilesystemBackend) Close() error {
	return nil
}

func (fs *FilesystemBackend) validatePath(path string) error {
	if path == "" {
		return ErrInvalidPath
	}

	// Backslashes are native separators on Windows. Accepting them in object
	// keys would make "a/b" and "a\b" resolve to the same physical file.
	if strings.Contains(path, "\\") {
		return ErrInvalidPath
	}

	// Ensure path doesn't start with a Unix absolute prefix (/) or
	// a Windows volume-relative prefix (\), preventing absolute path injection.
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		return ErrInvalidPath
	}

	if isDriveQualifiedPath(path) {
		return ErrInvalidPath
	}

	// Prevent directory traversal while still allowing valid S3 keys such as
	// "file..txt" or "folder/.../file.txt".
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return ErrInvalidPath
		}
	}

	if filepath.VolumeName(filepath.FromSlash(path)) != "" {
		return ErrInvalidPath
	}

	fullPath := filepath.Clean(filepath.Join(fs.rootPath, filepath.FromSlash(path)))
	cleanRoot := filepath.Clean(fs.rootPath) + string(filepath.Separator)
	if !strings.HasPrefix(fullPath, cleanRoot) {
		return ErrInvalidPath
	}

	return nil
}

func isDriveQualifiedPath(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	first := path[0]
	return (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
}

func (fs *FilesystemBackend) getFullPath(path string) string {
	return filepath.Join(fs.rootPath, filepath.FromSlash(path))
}

func (fs *FilesystemBackend) getMetadataPath(path string) string {
	return fs.getFullPath(path) + ".metadata"
}

// getStagingMetadataPath returns the deterministic staged-sidecar path used by
// Put's two-phase commit.
func (fs *FilesystemBackend) getStagingMetadataPath(path string) string {
	return fs.getMetadataPath(path) + metadataStagingSuffix
}

// lockPath acquires the striped per-path mutex and returns its unlock func.
// It serialises Put's commit section against repairStagedCommit so a reader
// can never resolve (and discard) the staged sidecar of an in-flight write.
func (fs *FilesystemBackend) lockPath(path string) func() {
	h := fnv.New32a()
	h.Write([]byte(path)) //nolint:errcheck // fnv Write never fails
	mu := &fs.pathLocks[h.Sum32()%pathLockShards]
	mu.Lock()
	return mu.Unlock
}

// maybeRepair runs the staged-commit repair only when a staged sidecar exists.
// The common case costs a single os.Stat and takes no lock.
func (fs *FilesystemBackend) maybeRepair(path string) {
	if _, err := os.Stat(fs.getStagingMetadataPath(path)); err != nil {
		return
	}
	unlock := fs.lockPath(path)
	defer unlock()
	fs.repairStagedCommit(path)
}

// repairStagedCommit resolves a staged sidecar left behind by a Put that
func (fs *FilesystemBackend) repairStagedCommit(path string) {
	stagingPath := fs.getStagingMetadataPath(path)
	data, err := os.ReadFile(stagingPath)
	if err != nil {
		return // no stage (or unreadable this instant) — nothing to do
	}

	var staged map[string]string
	if jErr := json.Unmarshal(data, &staged); jErr != nil || staged["etag"] == "" {
		// A staged sidecar is always written atomically (temp+rename), so an
		// unparseable one is foreign/corrupt — discard it.
		os.Remove(stagingPath) //nolint:errcheck
		return
	}

	fullPath := fs.getFullPath(path)
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		// No data file to commit against (crash before the data commit of a
		// brand-new object): the stage is dead.
		os.Remove(stagingPath) //nolint:errcheck
		return
	}

	// Cheap pre-check: a size mismatch already proves the data commit never
	// happened — no need to hash a potentially large file.
	if stagedSize, sErr := strconv.ParseInt(staged["size"], 10, 64); sErr == nil && stagedSize != info.Size() {
		os.Remove(stagingPath) //nolint:errcheck
		logrus.WithField("path", path).Warn("Staged sidecar rolled back (size mismatch — data commit never happened)")
		return
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return // transient; retry on a later access
	}
	hasher := md5.New()
	_, cErr := io.Copy(hasher, f)
	f.Close()
	if cErr != nil {
		return // transient; retry on a later access
	}

	if hex.EncodeToString(hasher.Sum(nil)) == staged["etag"] {
		if err := os.Rename(stagingPath, fs.getMetadataPath(path)); err != nil {
			logrus.WithError(err).WithField("path", path).
				Error("Staged sidecar roll-forward failed — object stays unreadable until repair succeeds")
			return
		}
		logrus.WithField("path", path).Warn("Staged sidecar rolled forward (completed a crashed Put's metadata commit)")
	} else {
		os.Remove(stagingPath) //nolint:errcheck
		logrus.WithField("path", path).Warn("Staged sidecar rolled back (data does not match — old object pair intact)")
	}
}

// saveMetadata saves metadata to a file
func (fs *FilesystemBackend) saveMetadata(path string, metadata map[string]string) error {
	metadataPath := fs.getMetadataPath(path)

	tempPath, err := fs.prepareMetadataTemp(path, metadata)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)

	if err := os.Rename(tempPath, metadataPath); err != nil {
		return NewErrorWithCause("AtomicMetadataMove", "Failed to move metadata file to final location", err)
	}

	return nil
}

func (fs *FilesystemBackend) prepareMetadataTemp(path string, metadata map[string]string) (string, error) {
	metadataPath := fs.getMetadataPath(path)

	// Create directory for metadata file
	dir := filepath.Dir(metadataPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", NewErrorWithCause("CreateMetadataDirectory", "Failed to create metadata directory", err)
	}

	// Marshal metadata
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", NewErrorWithCause("MarshalMetadata", "Failed to marshal metadata", err)
	}

	tempFile, err := os.CreateTemp(dir, ".metadata-tmp-*")
	if err != nil {
		return "", NewErrorWithCause("CreateMetadataTempFile", "Failed to create temporary metadata file", err)
	}
	tempPath := tempFile.Name()

	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return "", NewErrorWithCause("WriteMetadata", "Failed to write metadata file", err)
	}
	// The sidecar carries the wrapped DEK: bytes that survive a crash without
	// it are unreadable forever, so it is flushed like the data it describes.
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return "", NewErrorWithCause("SyncMetadata", "Failed to flush metadata to disk", err)
	}
	if err := tempFile.Close(); err != nil {
		return "", NewErrorWithCause("CloseMetadataTempFile", "Failed to close temporary metadata file", err)
	}

	return tempPath, nil
}

// generateBasicMetadata generates basic metadata from file stats
func (fs *FilesystemBackend) generateBasicMetadata(path string) (map[string]string, error) {
	fullPath := fs.getFullPath(path)

	stat, err := os.Stat(fullPath)
	if err != nil {
		// Check if it's a file not found error
		if os.IsNotExist(err) {
			return nil, ErrObjectNotFound
		}
		return nil, NewErrorWithCause("StatFile", "Failed to stat file", err)
	}

	metadata := make(map[string]string)
	metadata["size"] = fmt.Sprintf("%d", stat.Size())
	metadata["last_modified"] = fmt.Sprintf("%d", stat.ModTime().Unix())

	metadata[MetadataGeneratedKey] = "true"

	// Try to calculate ETag by reading file
	file, err := os.Open(fullPath)
	if err == nil {
		defer file.Close()
		hasher := md5.New()
		if _, err := io.Copy(hasher, file); err == nil {
			metadata["etag"] = hex.EncodeToString(hasher.Sum(nil))
		}
	}

	return metadata, nil
}

// RemoveDirectory removes a directory and all its contents
// This is a special method for the FilesystemBackend to support bucket deletion
func (fs *FilesystemBackend) RemoveDirectory(path string) error {
	if err := fs.validatePath(path); err != nil {
		return err
	}

	fullPath := fs.getFullPath(path)

	// Check if directory exists
	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return nil // Already deleted, nothing to do
	}
	if err != nil {
		return NewErrorWithCause("StatDirectory", "Failed to stat directory", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}

	// On Windows a file another process still holds open refuses to go.
	var rmErr error
	for attempt := 0; attempt < 5; attempt++ {
		if rmErr = os.RemoveAll(fullPath); rmErr == nil {
			return nil
		}
		time.Sleep(time.Duration(10*(attempt+1)) * time.Millisecond)
	}
	return NewErrorWithCause("RemoveDirectory", "Failed to remove directory", rmErr)
}
