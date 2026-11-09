package settings

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A default no one changed is dated at the epoch, one an earlier release dated
// at its install too, so in a cluster a change made on any node is newer; a
// changed setting keeps its time.
func TestAnUnchangedDefaultIsDatedAtTheEpoch(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()
	_, err := NewManager(db, logrus.New())
	require.NoError(t, err)
	updated := func(key string) int64 {
		t.Helper()
		var at int64
		require.NoError(t, db.QueryRow(`SELECT updated_at FROM system_settings WHERE key = ?`, key).Scan(&at))
		return at
	}
	assert.Zero(t, updated("security.session_timeout"))

	_, err = db.Exec(`UPDATE system_settings SET created_at = 500, updated_at = 500 WHERE key = 'security.session_timeout'`)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE system_settings SET value = '15', created_at = 500, updated_at = 600 WHERE key = 'metrics.collection_interval'`)
	require.NoError(t, err)
	_, err = NewManager(db, logrus.New())
	require.NoError(t, err)
	assert.Zero(t, updated("security.session_timeout"), "an earlier release's untouched default")
	assert.EqualValues(t, 600, updated("metrics.collection_interval"))
}
