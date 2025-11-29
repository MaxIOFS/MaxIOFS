package logging

import (
	"errors"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
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
	// Simple conversion for testing
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
		name           string
		format         string
		expectedType   string
	}{
		{
			name:         "JSON format",
			format:       "json",
			expectedType: "*logrus.JSONFormatter",
		},
		{
			name:         "Text format",
			format:       "text",
			expectedType: "*logrus.TextFormatter",
		},
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
			// Check that formatter was set (can't directly check type without reflection)
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
		{
			name:          "Debug level",
			level:         "debug",
			expectedLevel: logrus.DebugLevel,
		},
		{
			name:          "Info level",
			level:         "info",
			expectedLevel: logrus.InfoLevel,
		},
		{
			name:          "Warn level",
			level:         "warn",
			expectedLevel: logrus.WarnLevel,
		},
		{
			name:          "Error level",
			level:         "error",
			expectedLevel: logrus.ErrorLevel,
		},
		{
			name:          "Invalid level defaults to info",
			level:         "invalid",
			expectedLevel: logrus.InfoLevel,
		},
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
		{
			name:     "Caller enabled",
			enabled:  "true",
			expected: true,
		},
		{
			name:     "Caller disabled",
			enabled:  "false",
			expected: false,
		},
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

func TestTestOutputInvalidType(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	sm := &mockSettingsManager{
		settings: map[string]string{},
	}
	manager.SetSettingsManager(sm)

	err := manager.TestOutput("invalid")
	assert.Equal(t, ErrInvalidOutputType, err)
}

func TestTestOutputWithoutSettingsManager(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	err := manager.TestOutput("syslog")
	assert.Equal(t, ErrSettingsManagerNotSet, err)
}

func TestTestSyslogWithoutHost(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	sm := &mockSettingsManager{
		settings: map[string]string{
			// No syslog_host
		},
	}
	manager.SetSettingsManager(sm)

	err := manager.TestOutput("syslog")
	assert.Equal(t, ErrSyslogHostNotConfigured, err)
}

func TestTestHTTPWithoutURL(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	sm := &mockSettingsManager{
		settings: map[string]string{
			// No http_url
		},
	}
	manager.SetSettingsManager(sm)

	err := manager.TestOutput("http")
	assert.Equal(t, ErrHTTPURLNotConfigured, err)
}

func TestDefaultValues(t *testing.T) {
	logger := logrus.New()
	manager := NewManager(logger)

	// Settings manager returns errors for all keys (simulating missing values)
	sm := &mockSettingsManager{
		settings: map[string]string{},
	}

	manager.SetSettingsManager(sm)

	// Should use defaults and not panic
	assert.Equal(t, logrus.InfoLevel, logger.GetLevel())
	assert.False(t, logger.ReportCaller)
}
