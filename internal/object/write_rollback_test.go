package object

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

// Undoing a write deletes the version it created under the retention or legal
// hold that write set. A client delete still cannot, nor can the undo touch
// another version.
func TestWriteRollbackDeletesTheProtectedVersionItCreated(t *testing.T) {
	until := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	for name, lock := range map[string]map[string]string{
		"compliance": {"x-amz-object-lock-mode": RetentionModeCompliance, "x-amz-object-lock-retain-until-date": until},
		"legal hold": {"x-amz-object-lock-legal-hold": LegalHoldStatusOn},
	} {
		t.Run(name, func(t *testing.T) {
			m, _, s := setupManagerWithConfigKey(t)
			ctx := t.Context()
			require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{
				Name:       "worm",
				Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
				ObjectLock: &metadata.ObjectLockMetadata{Enabled: true},
			}))
			headers := http.Header{}
			for k, v := range lock {
				headers.Set(k, v)
			}
			older, err := m.PutObject(ctx, "worm", "k", strings.NewReader("older"), headers.Clone())
			require.NoError(t, err)
			written, err := m.PutObject(ctx, "worm", "k", strings.NewReader("written"), headers.Clone())
			require.NoError(t, err)

			_, err = m.DeleteObject(ctx, "worm", "k", true, written.VersionID)
			require.Error(t, err, "a client delete, even bypassing governance, is refused")

			_, err = m.DeleteObject(WithWriteRollback(ctx), "worm", "k", true, written.VersionID)
			require.NoError(t, err)
			_, _, err = m.GetObject(ctx, "worm", "k", written.VersionID)
			require.ErrorIs(t, err, ErrObjectNotFound)

			obj, reader, err := m.GetObject(ctx, "worm", "k", older.VersionID)
			require.NoError(t, err)
			body, err := io.ReadAll(reader)
			require.NoError(t, reader.Close())
			require.NoError(t, err)
			require.Equal(t, "older", string(body))
			require.Equal(t, older.VersionID, obj.VersionID)
		})
	}
}
