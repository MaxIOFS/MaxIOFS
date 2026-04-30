package metadata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestMigrateFromBadgerIfNeededRecoversPromotedPebbleTemp(t *testing.T) {
	dataDir := t.TempDir()
	tmpDir := filepath.Join(dataDir, "metadata_pebble")
	require.NoError(t, os.MkdirAll(tmpDir, 0755))

	db, err := pebble.Open(tmpDir, &pebble.Options{})
	require.NoError(t, err)
	require.NoError(t, db.Set([]byte("bucket::global"), []byte(`{"name":"global"}`), pebble.Sync))
	require.NoError(t, db.Close())

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	require.NoError(t, MigrateFromBadgerIfNeeded(dataDir, logger))

	_, err = os.Stat(filepath.Join(dataDir, "metadata_pebble"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dataDir, "metadata", PebbleV2SentinelFile))
	require.NoError(t, err)

	recoveredDB, err := pebble.Open(filepath.Join(dataDir, "metadata"), &pebble.Options{})
	require.NoError(t, err)
	defer recoveredDB.Close() //nolint:errcheck

	val, closer, err := recoveredDB.Get([]byte("bucket::global"))
	require.NoError(t, err)
	require.Equal(t, []byte(`{"name":"global"}`), append([]byte(nil), val...))
	require.NoError(t, closer.Close())
}
