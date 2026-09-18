package object

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/stretchr/testify/require"
)

func TestReviewAcknowledgedPartSurvivesProcessKill(t *testing.T) {
	if root := os.Getenv("MAXIOFS_REVIEW_PART_ROOT"); root != "" {
		m, s := openFaultManager(t, root)
		defer s.Close()
		_, err := m.UploadPart(t.Context(), os.Getenv("MAXIOFS_REVIEW_UPLOAD"), 1, strings.NewReader(faultNew))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, "ready"), []byte("uploaded"), 0600))
		for {
			time.Sleep(time.Second)
		}
	}
	root := t.TempDir()
	m, s := openFaultManager(t, root)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "fault", OwnerID: "owner"}))
	u, err := m.CreateMultipartUpload(t.Context(), "fault", "key", http.Header{})
	require.NoError(t, err)
	_, err = m.UploadPart(t.Context(), u.UploadID, 1, strings.NewReader(faultOld))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	cmd := exec.Command(os.Args[0], "-test.run=^TestReviewAcknowledgedPartSurvivesProcessKill$", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "MAXIOFS_REVIEW_PART_ROOT="+root, "MAXIOFS_REVIEW_UPLOAD="+u.UploadID)
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
			t.Fatalf("child did not complete UploadPart: %s", output.String())
		}
		time.Sleep(time.Millisecond)
	}
	require.NoError(t, cmd.Process.Kill())
	require.Error(t, cmd.Wait())
	m, s = openFaultManager(t, root)
	defer s.Close()
	require.False(t, s.WasCleanShutdown())
	manifests, err := rollback.Manifests(m.config.Root, rollback.PartPrefix)
	require.NoError(t, err)
	t.Logf("retained part backups after acknowledged UploadPart: %d", len(manifests))
	report, err := rollback.Undo(t.Context(), m.config.Root, m.storage, s, nil)
	require.NoError(t, err)
	require.Empty(t, report.Failures)
	row, err := s.GetPart(t.Context(), u.UploadID, 1)
	require.NoError(t, err)
	r, sidecar, err := m.storage.GetPart(t.Context(), u.UploadID, 1)
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, int64(len(data)), row.Size, "successful UploadPart lost its row and has no backup to restore")
	require.Equal(t, sidecar["etag"], row.ETag)
}
