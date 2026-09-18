package metadata

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAcknowledgedMetadataGroupSurvivesKill(t *testing.T) {
	const writers = 32
	if root := os.Getenv("MAXIOFS_TEST_GROUP_COMMIT_ROOT"); root != "" {
		s, err := NewPebbleStore(PebbleOptions{DataDir: root, WALSyncInterval: -1})
		require.NoError(t, err)
		defer s.Close()
		require.NoError(t, s.CreateBucket(t.Context(), &BucketMetadata{Name: "bucket"}))
		require.NoError(t, s.CreateMultipartUpload(t.Context(), &MultipartUploadMetadata{UploadID: "upload", Bucket: "bucket", Key: "key"}))
		results := make(chan error, writers)
		for n := range writers {
			go func() {
				err := s.PutObject(t.Context(), &ObjectMetadata{Bucket: "bucket", Key: fmt.Sprintf("object-%d", n), Size: 32, ETag: "object"})
				if err == nil {
					err = s.PutObjectVersion(t.Context(), &ObjectMetadata{Bucket: "bucket", Key: fmt.Sprintf("version-%d", n), Size: 64, ETag: "version"}, &ObjectVersion{VersionID: "v1", IsLatest: true})
				}
				if err == nil {
					err = s.PutPart(t.Context(), &PartMetadata{UploadID: "upload", PartNumber: n + 1, Size: 96, ETag: "part"})
				}
				results <- err
			}()
		}
		for range writers {
			require.NoError(t, <-results)
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, "ready"), []byte("committed"), 0600))
		for {
			time.Sleep(time.Second)
		}
	}

	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAcknowledgedMetadataGroupSurvivesKill$", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "MAXIOFS_TEST_GROUP_COMMIT_ROOT="+root)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("child did not acknowledge writes: %s", output.String())
		}
		time.Sleep(time.Millisecond)
	}
	require.NoError(t, cmd.Process.Kill())
	require.Error(t, cmd.Wait())
	s, err := NewPebbleStore(PebbleOptions{DataDir: root, WALSyncInterval: -1})
	require.NoError(t, err)
	defer s.Close()
	require.False(t, s.WasCleanShutdown())
	for n := range writers {
		obj, err := s.GetObject(t.Context(), "bucket", fmt.Sprintf("object-%d", n))
		require.NoError(t, err)
		require.Equal(t, int64(32), obj.Size)
		require.Equal(t, "object", obj.ETag)
		version, err := s.GetObject(t.Context(), "bucket", fmt.Sprintf("version-%d", n), "v1")
		require.NoError(t, err)
		require.Equal(t, int64(64), version.Size)
		require.Equal(t, "version", version.ETag)
		latest, err := s.GetObject(t.Context(), "bucket", fmt.Sprintf("version-%d", n))
		require.NoError(t, err)
		require.Equal(t, "v1", latest.VersionID)
		part, err := s.GetPart(t.Context(), "upload", n+1)
		require.NoError(t, err)
		require.Equal(t, int64(96), part.Size)
		require.Equal(t, "part", part.ETag)
	}
}
