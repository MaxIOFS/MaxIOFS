package rollback_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

// Two copies retained within one tick of the filesystem clock carry the same
// modification time: they are still undone in the order they were retained,
// whatever their names.
func TestCopiesRetainedInTheSameTickAreUndoneInOrder(t *testing.T) {
	root := t.TempDir()
	ref := storage.ObjectRef{Bucket: "b", Key: "k"}
	first := filepath.Join(root, rollback.ObjectPrefix+"z-first"+rollback.ManifestSuffix)
	second := filepath.Join(root, rollback.ObjectPrefix+"a-second"+rollback.ManifestSuffix)
	require.NoError(t, rollback.WriteObjectManifest(first, ref, nil, nil))
	require.NoError(t, rollback.WriteObjectManifest(second, ref, nil, nil))
	tick := time.Now().Truncate(time.Second)
	for _, path := range []string{first, second} {
		require.NoError(t, os.Chtimes(path, tick, tick))
	}

	manifests, err := rollback.Manifests(root, rollback.ObjectPrefix)
	require.NoError(t, err)
	require.Equal(t, []string{first, second}, manifests)
}

// A manifest written before manifests said when their copy was retained is
// older than every one that does, whatever its modification time.
func TestAManifestOfAnEarlierReleaseIsUndoneFirst(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, rollback.ObjectPrefix+"a-current"+rollback.ManifestSuffix)
	earlier := filepath.Join(root, rollback.ObjectPrefix+"z-earlier"+rollback.ManifestSuffix)
	require.NoError(t, rollback.WriteObjectManifest(current, storage.ObjectRef{Bucket: "b", Key: "k"}, nil, nil))
	require.NoError(t, os.WriteFile(earlier, []byte(`{"ref":{"bucket":"b","key":"k"},"metadata":null}`), 0600))
	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(earlier, later, later))

	manifests, err := rollback.Manifests(root, rollback.ObjectPrefix)
	require.NoError(t, err)
	require.Equal(t, []string{earlier, current}, manifests)
}

// Manifests of an earlier release are ordered by modification time, and by
// name when that is the same.
func TestManifestsOfAnEarlierReleaseFollowTheirModificationTime(t *testing.T) {
	root := t.TempDir()
	legacy := []byte(`{"ref":{"bucket":"b","key":"k"},"metadata":null}`)
	older := filepath.Join(root, rollback.ObjectPrefix+"z-older"+rollback.ManifestSuffix)
	newer := filepath.Join(root, rollback.ObjectPrefix+"a-newer"+rollback.ManifestSuffix)
	asNew := filepath.Join(root, rollback.ObjectPrefix+"b-as-new"+rollback.ManifestSuffix)
	at := time.Now().Truncate(time.Second)
	for path, modified := range map[string]time.Time{older: at, newer: at.Add(time.Minute), asNew: at.Add(time.Minute)} {
		require.NoError(t, os.WriteFile(path, legacy, 0600))
		require.NoError(t, os.Chtimes(path, modified, modified))
	}

	manifests, err := rollback.Manifests(root, rollback.ObjectPrefix)
	require.NoError(t, err)
	require.Equal(t, []string{older, newer, asNew}, manifests)
}

// A manifest that names no object, or no part, is refused rather than undone
// onto something it does not name.
func TestAManifestNamingNothingIsRefused(t *testing.T) {
	root := t.TempDir()
	write := func(body string) string {
		t.Helper()
		path := filepath.Join(root, "manifest"+rollback.ManifestSuffix)
		require.NoError(t, os.WriteFile(path, []byte(body), 0600))
		return path
	}
	for _, body := range []string{`{"ref":{"bucket":"","key":"k"}}`, `{"ref":{"bucket":"b","key":""}}`} {
		_, err := rollback.ReadObjectManifest(write(body))
		require.Error(t, err, body)
	}
	_, err := rollback.ReadObjectManifest(write(`{"ref":{"bucket":"b","key":"k"}}`))
	require.NoError(t, err)
	for _, body := range []string{`{"upload_id":"","part_number":1}`, `{"upload_id":"u","part_number":0}`} {
		_, err := rollback.ReadPartManifest(write(body))
		require.Error(t, err, body)
	}
	_, err = rollback.ReadPartManifest(write(`{"upload_id":"u","part_number":1}`))
	require.NoError(t, err)
}
