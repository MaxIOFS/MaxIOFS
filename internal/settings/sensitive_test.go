package settings

import (
	"testing"

	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A password or a token set by an administrator is kept, and never logged.
func TestASecretSettingIsNotLogged(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()
	logger, hook := logtest.NewNullLogger()
	logger.SetLevel(logrus.InfoLevel)
	manager, err := NewManager(db, logger)
	require.NoError(t, err)

	for key, value := range map[string]string{"metrics.token": "prometheus-scrape-token", "email.smtp_password": "smtp-secret"} {
		hook.Reset()
		require.NoError(t, manager.Set(key, value))
		kept, err := manager.Get(key)
		require.NoError(t, err)
		assert.Equal(t, value, kept)
		for _, entry := range hook.AllEntries() {
			line, _ := entry.String()
			assert.NotContains(t, line, value, key)
		}
	}

	hook.Reset()
	require.NoError(t, manager.Set("metrics.collection_interval", "15"))
	require.NotNil(t, hook.LastEntry())
	assert.Equal(t, "15", hook.LastEntry().Data["value"], "the value of any other setting is logged")
}
