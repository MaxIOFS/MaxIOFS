package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BucketMarker is what a bucket's marker holds: the bucket's tenant-qualified
// path and, as JSON, its configuration as the metadata store keeps it, for
// maxiofs recover to give it back. A marker of an earlier release holds the
// path alone, as text.
type BucketMarker struct {
	Bucket string          `json:"bucket"`
	Config json.RawMessage `json:"config,omitempty"`
}

// ReadBucketMarker reads the marker of the bucket directory dir. The marker
// of the previous layout is empty and gives no path.
func ReadBucketMarker(dir string) (BucketMarker, error) {
	data, err := os.ReadFile(filepath.Join(dir, bucketMarkerName))
	if err != nil {
		return BucketMarker{}, err
	}
	text := strings.TrimSpace(string(data))
	if !strings.HasPrefix(text, "{") {
		return BucketMarker{Bucket: text}, nil
	}
	var marker BucketMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return BucketMarker{}, fmt.Errorf("unreadable bucket marker in %s: %w", dir, err)
	}
	return marker, nil
}

// WriteBucketConfig keeps config, the configuration of bucket, in the
// bucket's marker under root, and reports whether the marker changed. A bucket
// without its directory is left alone. The marker is replaced whole, so a
// crash leaves the former one or the new one.
func WriteBucketConfig(root, bucket string, config []byte) (bool, error) {
	if err := validateBucket(bucket); err != nil {
		return false, err
	}
	dir := filepath.Join(root, BucketDirName(bucket))
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	data, err := json.MarshalIndent(BucketMarker{Bucket: bucket, Config: config}, "", "  ")
	if err != nil {
		return false, err
	}
	data = append(data, '\n')
	path := filepath.Join(dir, bucketMarkerName)
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return false, nil
	}

	tmp := filepath.Join(dir, ".tmp_"+bucketMarkerName)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0640)
	if err != nil {
		return false, err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, syncDir(dir)
}
