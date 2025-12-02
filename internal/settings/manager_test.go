package settings

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) (*sql.DB, string) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	return db, tmpDir
}

func TestNewManager(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	logger := logrus.New()
	logger.SetOutput(os.Stderr)

	manager, err := NewManager(db, logger)
	require.NoError(t, err)
	assert.NotNil(t, manager)
}

func TestGet(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	// Get existing setting
	value, err := manager.Get("security.session_timeout")
	require.NoError(t, err)
	assert.Equal(t, "86400", value)

	// Get non-existent setting
	_, err = manager.Get("non.existent.key")
	assert.Error(t, err)
}

func TestGetInt(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	// Get int setting
	value, err := manager.GetInt("security.session_timeout")
	require.NoError(t, err)
	assert.Equal(t, 86400, value)

	// Get non-existent setting
	_, err = manager.GetInt("non.existent.key")
	assert.Error(t, err)
}

func TestGetBool(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	// Get bool setting
	value, err := manager.GetBool("audit.enabled")
	require.NoError(t, err)
	assert.True(t, value)

	// Get false bool setting
	value, err = manager.GetBool("security.require_2fa_admin")
	require.NoError(t, err)
	assert.False(t, value)
}

func TestGetSetting(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	// Get existing setting
	setting, err := manager.GetSetting("security.session_timeout")
	require.NoError(t, err)
	assert.NotNil(t, setting)
	assert.Equal(t, "security.session_timeout", setting.Key)
	assert.Equal(t, "86400", setting.Value)
	assert.Equal(t, "int", setting.Type)
	assert.Equal(t, "security", setting.Category)

	// Get non-existent setting
	_, err = manager.GetSetting("non.existent.key")
	assert.Error(t, err)
}

func TestSet(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	// Update existing setting
	err = manager.Set("security.session_timeout", "3600")
	require.NoError(t, err)

	// Verify update
	value, err := manager.Get("security.session_timeout")
	require.NoError(t, err)
	assert.Equal(t, "3600", value)

	// Update bool setting
	err = manager.Set("audit.enabled", "false")
	require.NoError(t, err)

	boolValue, err := manager.GetBool("audit.enabled")
	require.NoError(t, err)
	assert.False(t, boolValue)
}

func TestSet_InvalidValue(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	// Try to set invalid int value
	err = manager.Set("security.session_timeout", "invalid")
	assert.Error(t, err)

	// Try to set invalid bool value
	err = manager.Set("audit.enabled", "invalid")
	assert.Error(t, err)
}

func TestListAll(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	settings, err := manager.ListAll()
	require.NoError(t, err)
	assert.NotEmpty(t, settings)

	// Verify we have default settings
	assert.Greater(t, len(settings), 10)
}

func TestListByCategory(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	// List security settings
	settings, err := manager.ListByCategory("security")
	require.NoError(t, err)
	assert.NotEmpty(t, settings)

	// Verify all returned settings are in security category
	for _, setting := range settings {
		assert.Equal(t, "security", setting.Category)
	}

	// List non-existent category
	settings, err = manager.ListByCategory("non_existent")
	require.NoError(t, err)
	assert.Empty(t, settings)
}

func TestBulkUpdate(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	// Bulk update multiple settings
	updates := map[string]string{
		"security.session_timeout":     "7200",
		"security.max_failed_attempts": "3",
		"audit.enabled":                "false",
	}

	err = manager.BulkUpdate(updates)
	require.NoError(t, err)

	// Verify updates
	value, err := manager.Get("security.session_timeout")
	require.NoError(t, err)
	assert.Equal(t, "7200", value)

	value, err = manager.Get("security.max_failed_attempts")
	require.NoError(t, err)
	assert.Equal(t, "3", value)

	boolValue, err := manager.GetBool("audit.enabled")
	require.NoError(t, err)
	assert.False(t, boolValue)
}

func TestBulkUpdate_PartialFailure(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	// Bulk update with one invalid value
	updates := map[string]string{
		"security.session_timeout":     "7200",
		"security.max_failed_attempts": "invalid", // This will fail
	}

	err = manager.BulkUpdate(updates)
	assert.Error(t, err)
}

func TestGetCategories(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	categories, err := manager.GetCategories()
	require.NoError(t, err)
	assert.NotEmpty(t, categories)

	// Verify expected categories exist
	expectedCategories := []string{"security", "audit", "storage", "metrics", "logging"}
	for _, expected := range expectedCategories {
		assert.Contains(t, categories, expected)
	}
}

func TestDefaultSettings(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	// Verify critical default settings
	tests := []struct {
		key      string
		expected string
	}{
		{"security.session_timeout", "86400"},
		{"security.max_failed_attempts", "5"},
		{"security.lockout_duration", "900"},
		{"audit.enabled", "true"},
		{"audit.retention_days", "90"},
		{"storage.default_object_lock_days", "7"},
		{"metrics.enabled", "true"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			value, err := manager.Get(tt.key)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, value)
		})
	}
}

func TestConcurrentAccess(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	manager, err := NewManager(db, logrus.New())
	require.NoError(t, err)

	// Concurrent reads
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, err := manager.Get("security.session_timeout")
			assert.NoError(t, err)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
