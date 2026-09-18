// Package rollback retains what an in-place overwrite is about to destroy and
// undoes interrupted writes at the next start.
//
// An object overwrite and a multipart part replacement both publish new bytes
// over a live path before the matching index entry is committed. The retained
// copy plus its manifest are what turns that window into something reversible:
// the writer restores from it in-process, and a crash leaves both files on disk
// for the boot pass to finish the job.
package rollback

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/maxiofs/maxiofs/internal/storage"
)

const (
	// ObjectPrefix names the retained copy of an object being overwritten.
	// The name predates this package — orphans written by older versions are
	// still recognised.
	ObjectPrefix = "maxiofs-mpu-backup-"
	// PartPrefix names the retained copy of a multipart part being replaced.
	PartPrefix = "maxiofs-part-backup-"
	// ManifestSuffix is appended to the backup file name.
	ManifestSuffix = ".json"
)

// Committed is the identity the index held when the copy was retained. It is
// what makes an interrupted write decidable afterwards: if the index still says
// this, the write never got there.
type Committed struct {
	Size int64  `json:"size"`
	ETag string `json:"etag"`
}

// ObjectManifest describes where a retained object copy belongs.
type ObjectManifest struct {
	Ref       storage.ObjectRef `json:"ref"`
	Metadata  map[string]string `json:"metadata"`
	Committed *Committed        `json:"committed,omitempty"`
}

// PartManifest describes where a retained part copy belongs.
type PartManifest struct {
	UploadID   string            `json:"upload_id"`
	PartNumber int               `json:"part_number"`
	Metadata   map[string]string `json:"metadata"`
	Committed  *Committed        `json:"committed,omitempty"`
}

// WriteObjectManifest records the destination of a retained object copy and the
// index entry it was taken from (nil when the object had no entry).
func WriteObjectManifest(path string, ref storage.ObjectRef, metadata map[string]string, committed *Committed) error {
	return writeManifest(path, ObjectManifest{Ref: ref, Metadata: metadata, Committed: committed})
}

// WritePartManifest records the destination of a retained part copy and the
// part row it was taken from.
func WritePartManifest(path, uploadID string, partNumber int, metadata map[string]string, committed *Committed) error {
	return writeManifest(path, PartManifest{UploadID: uploadID, PartNumber: partNumber, Metadata: metadata, Committed: committed})
}

// ReadObjectManifest loads an object manifest written by WriteObjectManifest.
func ReadObjectManifest(path string) (*ObjectManifest, error) {
	var manifest ObjectManifest
	if err := readManifest(path, &manifest); err != nil {
		return nil, err
	}
	if manifest.Ref.Bucket == "" || manifest.Ref.Key == "" {
		return nil, fmt.Errorf("object manifest %q names no object", path)
	}
	return &manifest, nil
}

// ReadPartManifest loads a part manifest written by WritePartManifest.
func ReadPartManifest(path string) (*PartManifest, error) {
	var manifest PartManifest
	if err := readManifest(path, &manifest); err != nil {
		return nil, err
	}
	if manifest.UploadID == "" || manifest.PartNumber < 1 {
		return nil, fmt.Errorf("part manifest %q names no part", path)
	}
	return &manifest, nil
}

// DataPath returns the backup file a manifest belongs to.
func DataPath(manifestPath string) string {
	return strings.TrimSuffix(manifestPath, ManifestSuffix)
}

// Manifests lists the manifest files of one backup kind under root, oldest
// first, so repeated interruptions are undone in the order they happened.
func Manifests(root, prefix string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(root, prefix+"*"+ManifestSuffix))
	if err != nil {
		return nil, err
	}
	sortByModTime(matches)
	return matches, nil
}

func writeManifest(path string, payload any) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(payload); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return storage.SyncDirectory(filepath.Dir(path))
}

func readManifest(path string, into any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(content, into); err != nil {
		return fmt.Errorf("unreadable manifest %q: %w", path, err)
	}
	return nil
}

func sortByModTime(paths []string) {
	modTime := make(map[string]int64, len(paths))
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil {
			modTime[path] = info.ModTime().UnixNano()
		}
	}
	// Ties (same nanosecond, or an unreadable stat) fall back to the name,
	// which carries the random suffix — any stable order will do there.
	sort.Slice(paths, func(i, j int) bool {
		a, b := paths[i], paths[j]
		if modTime[a] != modTime[b] {
			return modTime[a] < modTime[b]
		}
		return a < b
	})
}
