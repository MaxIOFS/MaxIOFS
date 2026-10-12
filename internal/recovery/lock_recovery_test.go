package recovery

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A store rebuilt from the files gets back each version's Object Lock state:
// the retention a write set, a retention extended after the write, a legal
// hold on, and a legal hold turned off.
func TestRecovery_ObjectLockSurvives(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	dbDir := filepath.Join(dataDir, "db")
	require.NoError(t, os.MkdirAll(dbDir, 0750))
	db, err := sql.Open("sqlite", filepath.Join(dbDir, "maxiofs.db"))
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE encryption_keys (
			version INTEGER PRIMARY KEY, key_hex TEXT NOT NULL,
			is_current INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL,
			cluster_shared INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE system_settings (
			key TEXT PRIMARY KEY, value TEXT NOT NULL, type TEXT NOT NULL,
			category TEXT NOT NULL, description TEXT, editable INTEGER DEFAULT 1,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
		);
	`)
	require.NoError(t, err)
	kekStore, err := kek.Bootstrap(db, "")
	require.NoError(t, err)
	objectsRoot := filepath.Join(dataDir, "objects")
	backend, err := storage.NewFilesystemBackend(storage.Config{Root: objectsRoot})
	require.NoError(t, err)
	metaStore, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: filepath.Join(dataDir, "live"), Logger: logrus.StandardLogger()})
	require.NoError(t, err)
	objMgr := object.NewManager(backend, metaStore, config.StorageConfig{Backend: "filesystem", Root: objectsRoot},
		object.WithKEKProvider(kekStore))

	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: "locked", OwnerID: "admin",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true},
	}))
	require.NoError(t, backend.CreateBucket(ctx, "locked"))
	until := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	headers := http.Header{}
	headers.Set("x-amz-object-lock-mode", object.RetentionModeGovernance)
	headers.Set("x-amz-object-lock-retain-until-date", until.Format(time.RFC3339))
	headers.Set("x-amz-object-lock-legal-hold", object.LegalHoldStatusOn)
	kept, err := objMgr.PutObject(ctx, "locked", "kept", bytes.NewReader([]byte("kept")), headers)
	require.NoError(t, err)
	extended := until.Add(48 * time.Hour)
	require.NoError(t, objMgr.SetObjectRetention(ctx, "locked", "kept",
		&object.RetentionConfig{Mode: object.RetentionModeCompliance, RetainUntilDate: extended}, kept.VersionID))
	plain := http.Header{}
	plain.Set("x-amz-object-lock-mode", object.RetentionModeGovernance)
	plain.Set("x-amz-object-lock-retain-until-date", until.Format(time.RFC3339))
	written, err := objMgr.PutObject(ctx, "locked", "written", bytes.NewReader([]byte("written")), plain)
	require.NoError(t, err)
	held := http.Header{}
	held.Set("x-amz-object-lock-legal-hold", object.LegalHoldStatusOn)
	released, err := objMgr.PutObject(ctx, "locked", "released", bytes.NewReader([]byte("released")), held)
	require.NoError(t, err)
	require.NoError(t, objMgr.SetObjectLegalHold(ctx, "locked", "released",
		&object.LegalHoldConfig{Status: object.LegalHoldStatusOff}, released.VersionID))
	require.NoError(t, metaStore.Close())
	require.NoError(t, db.Close())
	require.NoError(t, os.RemoveAll(filepath.Join(dataDir, "live")))

	report, err := Run(Options{DataDir: dataDir, OutDB: filepath.Join(dataDir, "metadata-recovered")})
	require.NoError(t, err)
	require.Empty(t, report.Failures)
	store := openRecoveredStore(t, report.OutDB)
	defer store.Close()

	got, err := store.GetObject(ctx, "locked", "kept", kept.VersionID)
	require.NoError(t, err)
	require.NotNil(t, got.Retention)
	assert.Equal(t, object.RetentionModeCompliance, got.Retention.Mode)
	assert.True(t, extended.Equal(got.Retention.RetainUntilDate), "%v != %v", extended, got.Retention.RetainUntilDate)
	assert.True(t, got.LegalHold)

	got, err = store.GetObject(ctx, "locked", "written", written.VersionID)
	require.NoError(t, err)
	require.NotNil(t, got.Retention, "the retention set by the write")
	assert.Equal(t, object.RetentionModeGovernance, got.Retention.Mode)
	assert.True(t, until.Equal(got.Retention.RetainUntilDate))
	assert.False(t, got.LegalHold)

	got, err = store.GetObject(ctx, "locked", "released", released.VersionID)
	require.NoError(t, err)
	assert.Nil(t, got.Retention)
	assert.False(t, got.LegalHold)
}
