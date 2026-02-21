package logging

import (
	"errors"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ErrSettingNotFound = errors.New("setting not found")

type mockSettingsManager struct {
	settings map[string]string
}

func (m *mockSettingsManager) Get(key string) (string, error) {
	val, ok := m.settings[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return val, nil
}

func (m *mockSettingsManager) GetInt(key string) (int, error) {
	val, err := m.Get(key)
	if err != nil {
		return 0, err
	}
	switch val {
	case "514":
		return 514, nil
	case "100":
		return 100, nil
	case "10":
		return 10, nil
	default:
		return 0, nil
	}
}

func (m *mockSettingsManager) GetBool(key string) (bool, error) {
	val, err := m.Get(key)
	if err != nil {
		return false, err
	}
	return val == "true" || val == "1", nil
}

func TestNewManager(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	assert.NotNil(t, manager)
	assert.NotNil(t, manager.outputs)
	assert.NotNil(t, manager.dispatchHook)
	assert.Equal(t, logger, manager.logger)
}

func TestSetSettingsManager(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	sm := &mockSettingsManager{
		settings: map[string]string{
			"logging.format":         "json",
			"logging.level":          "info",
			"logging.include_caller": "false",
		},
	}

	manager.SetSettingsManager(sm)
	assert.Equal(t, sm, manager.settingsManager)
}

func TestReconfigureLogFormat(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	tests := []struct {
		name   string
		format string
	}{
		{"JSON format", "json"},
		{"Text format", "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &mockSettingsManager{
				settings: map[string]string{
					"logging.format":         tt.format,
					"logging.level":          "info",
					"logging.include_caller": "false",
				},
			}

			manager.SetSettingsManager(sm)
			assert.NotNil(t, logger.Formatter)
		})
	}
}

func TestReconfigureLogLevel(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	tests := []struct {
		name          string
		level         string
		expectedLevel logrus.Level
	}{
		{"Debug level", "debug", logrus.DebugLevel},
		{"Info level", "info", logrus.InfoLevel},
		{"Warn level", "warn", logrus.WarnLevel},
		{"Error level", "error", logrus.ErrorLevel},
		{"Invalid level defaults to info", "invalid", logrus.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &mockSettingsManager{
				settings: map[string]string{
					"logging.format":         "json",
					"logging.level":          tt.level,
					"logging.include_caller": "false",
				},
			}

			manager.SetSettingsManager(sm)
			assert.Equal(t, tt.expectedLevel, logger.GetLevel())
		})
	}
}

func TestReconfigureIncludeCaller(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	tests := []struct {
		name     string
		enabled  string
		expected bool
	}{
		{"Caller enabled", "true", true},
		{"Caller disabled", "false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &mockSettingsManager{
				settings: map[string]string{
					"logging.format":         "json",
					"logging.level":          "info",
					"logging.include_caller": tt.enabled,
				},
			}

			manager.SetSettingsManager(sm)
			assert.Equal(t, tt.expected, logger.ReportCaller)
		})
	}
}

func TestReconfigureWithoutSettingsManager(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	// Should not panic when settings manager is nil
	manager.Reconfigure()
	assert.NotNil(t, manager)
}

func TestCloseOutputs(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	// Close should not panic even with no outputs
	manager.Close()
	assert.Empty(t, manager.outputs)
}

func TestDefaultValues(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	sm := &mockSettingsManager{
		settings: map[string]string{},
	}

	manager.SetSettingsManager(sm)

	assert.Equal(t, logrus.InfoLevel, logger.GetLevel())
	assert.False(t, logger.ReportCaller)
}

func TestGetActiveOutputs(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	assert.Equal(t, 0, manager.GetActiveOutputs())
}

func TestDispatchHookFilterLevel(t *testing.T) {
	tests := []struct {
		name       string
		entryLevel string
		filterLvl  string
		expected   bool
	}{
		{"debug entry, debug filter", "debug", "debug", true},
		{"info entry, debug filter", "info", "debug", true},
		{"debug entry, info filter", "debug", "info", false},
		{"error entry, warn filter", "error", "warn", true},
		{"info entry, error filter", "info", "error", false},
		{"warn entry, warn filter", "warn", "warn", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shouldDispatch(tt.entryLevel, tt.filterLvl))
		})
	}
}

func TestTargetConfigChanged(t *testing.T) {
	base := &TargetConfig{
		Type: "syslog", Protocol: "tcp", Host: "a.com",
		Port: 514, Tag: "maxiofs", Format: "rfc3164",
		FilterLevel: "info",
	}

	same := &TargetConfig{
		Type: "syslog", Protocol: "tcp", Host: "a.com",
		Port: 514, Tag: "maxiofs", Format: "rfc3164",
		FilterLevel: "info",
	}
	assert.False(t, targetConfigChanged(base, same))

	different := &TargetConfig{
		Type: "syslog", Protocol: "tcp", Host: "b.com",
		Port: 514, Tag: "maxiofs", Format: "rfc3164",
		FilterLevel: "info",
	}
	assert.True(t, targetConfigChanged(base, different))
}

func TestReconfigureFromStoreWithTargets(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(&discardWriter{}) // suppress output
	manager := NewManager(logger)

	sm := &mockSettingsManager{
		settings: map[string]string{
			"logging.format":         "json",
			"logging.level":          "info",
			"logging.include_caller": "false",
		},
	}
	manager.SetSettingsManager(sm)

	// Create a store with an in-memory SQLite
	db := setupTestDB(t)
	store, err := NewTargetStore(db, logger)
	require.NoError(t, err)

	// Add a syslog target (will fail to connect since no server, but tests the flow)
	cfg := &TargetConfig{
		Name:        "Test Target",
		Type:        "syslog",
		Enabled:     true,
		Protocol:    "tcp",
		Host:        "127.0.0.1",
		Port:        19999, // no server listening, so it won't connect
		Tag:         "test",
		Format:      "rfc3164",
		FilterLevel: "info",
	}
	err = store.Create(cfg)
	require.NoError(t, err)

	// Setting the store triggers Reconfigure — the target won't connect,
	// but the manager should not panic
	manager.SetTargetStore(store)
	// The output won't be added because connection fails, which is expected
}

// discardWriter is a writer that discards all output
type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (n int, err error) { return len(p), nil }
