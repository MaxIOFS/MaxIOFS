package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A bucket's marker keeps its path and its configuration, readable as JSON; a
// marker of an earlier release, the path alone or nothing, still reads.
func TestABucketMarkerKeepsTheBucketsConfiguration(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, WriteBucketMarker(root, "tenant-a/photos"))

	marker, err := ReadBucketMarker(filepath.Join(root, "photos"))
	require.NoError(t, err)
	assert.Equal(t, BucketMarker{Bucket: "tenant-a/photos"}, marker, "a marker of an earlier release")

	written, err := WriteBucketConfig(root, "tenant-a/photos", []byte(`{"name":"photos","object_lock":{"enabled":true}}`))
	require.NoError(t, err)
	assert.True(t, written)
	marker, err = ReadBucketMarker(filepath.Join(root, "photos"))
	require.NoError(t, err)
	assert.Equal(t, "tenant-a/photos", marker.Bucket)
	assert.JSONEq(t, `{"name":"photos","object_lock":{"enabled":true}}`, string(marker.Config))

	raw, err := os.ReadFile(filepath.Join(root, "photos", bucketMarkerName))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "\n  \"config\": {", "indented, to be read by a person")

	written, err = WriteBucketConfig(root, "tenant-a/photos", []byte(`{"name":"photos","object_lock":{"enabled":true}}`))
	require.NoError(t, err)
	assert.False(t, written, "the same configuration is not written again")

	found, version, err := inferLayoutFromBuckets(root)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, LayoutVersion, version, "a marker with its configuration is of the current layout")

	entries, err := os.ReadDir(filepath.Join(root, "photos"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary file is left")

	require.NoError(t, os.WriteFile(filepath.Join(root, "photos", bucketMarkerName), nil, 0640))
	marker, err = ReadBucketMarker(filepath.Join(root, "photos"))
	require.NoError(t, err)
	assert.Empty(t, marker.Bucket, "the previous layout's empty marker")
}

// A bucket without its directory gets none: a marker written as its bucket
// is deleted would bring the bucket back at a recovery.
func TestABucketWithoutItsDirectoryGetsNoMarker(t *testing.T) {
	root := t.TempDir()
	written, err := WriteBucketConfig(root, "gone", []byte(`{"name":"gone"}`))
	require.NoError(t, err)
	assert.False(t, written)
	_, err = os.Stat(filepath.Join(root, "gone"))
	assert.True(t, os.IsNotExist(err))
}
