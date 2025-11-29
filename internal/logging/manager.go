package logging

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Manager manages structured logging with multiple outputs
type Manager struct {
	settingsManager SettingsManager
	outputs         map[string]Output
	mu              sync.RWMutex
	logger          *logrus.Logger
}

// SettingsManager interface for accessing dynamic settings
type SettingsManager interface {
	Get(key string) (string, error)
	GetInt(key string) (int, error)
	GetBool(key string) (bool, error)
}

// NewManager creates a new logging manager
func NewManager(logger *logrus.Logger) *Manager {
	return &Manager{
		outputs: make(map[string]Output),
		logger:  logger,
	}
}

// SetSettingsManager sets the settings manager and reconfigures logging
func (m *Manager) SetSettingsManager(sm SettingsManager) {
	m.mu.Lock()
	m.settingsManager = sm
	m.mu.Unlock()

	// Initial configuration
	m.Reconfigure()
}

// Reconfigure applies current settings from database
func (m *Manager) Reconfigure() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.settingsManager == nil {
		m.logger.Warn("Settings manager not set, using defaults")
		return
	}

	// Apply log format
	format, err := m.settingsManager.Get("logging.format")
	if err != nil {
		format = "json" // default
	}

	if format == "json" {
		m.logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339Nano,
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "timestamp",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "message",
			},
		})
	} else {
		m.logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339,
		})
	}

	// Apply log level
	levelStr, err := m.settingsManager.Get("logging.level")
	if err != nil {
		levelStr = "info" // default
	}
	level, err := logrus.ParseLevel(levelStr)
	if err != nil {
		m.logger.WithError(err).Warn("Invalid log level, using info")
		level = logrus.InfoLevel
	}
	m.logger.SetLevel(level)

	// Include caller information if enabled
	includeCaller, err := m.settingsManager.GetBool("logging.include_caller")
	if err != nil {
		includeCaller = false // default
	}
	m.logger.SetReportCaller(includeCaller)

	// Reconfigure outputs
	m.reconfigureOutputs()

	m.logger.WithFields(logrus.Fields{
		"format":         format,
		"level":          levelStr,
		"include_caller": includeCaller,
	}).Info("Logging configuration updated")
}

// reconfigureOutputs reconfigures all log outputs based on current settings
func (m *Manager) reconfigureOutputs() {
	// Syslog output
	syslogEnabled, err := m.settingsManager.GetBool("logging.syslog_enabled")
	if err == nil && syslogEnabled {
		m.configureSyslog()
	} else {
		m.closeOutput("syslog")
	}

	// HTTP output
	httpEnabled, err := m.settingsManager.GetBool("logging.http_enabled")
	if err == nil && httpEnabled {
		m.configureHTTP()
	} else {
		m.closeOutput("http")
	}
}

// configureSyslog sets up syslog output
func (m *Manager) configureSyslog() {
	host, err := m.settingsManager.Get("logging.syslog_host")
	if err != nil || host == "" {
		m.logger.Warn("Syslog enabled but no host configured")
		m.closeOutput("syslog")
		return
	}

	port, err := m.settingsManager.GetInt("logging.syslog_port")
	if err != nil {
		port = 514 // default
	}

	protocol, err := m.settingsManager.Get("logging.syslog_protocol")
	if err != nil {
		protocol = "tcp" // default
	}

	tag, err := m.settingsManager.Get("logging.syslog_tag")
	if err != nil {
		tag = "maxiofs" // default
	}

	// Close existing output if any
	m.closeOutput("syslog")

	// Create new syslog output
	output, err := NewSyslogOutput(protocol, host, port, tag)
	if err != nil {
		m.logger.WithError(err).Error("Failed to create syslog output")
		return
	}

	m.outputs["syslog"] = output
	m.logger.WithFields(logrus.Fields{
		"protocol": protocol,
		"host":     host,
		"port":     port,
		"tag":      tag,
	}).Info("Syslog output configured")

	// Add hook to logrus
	m.logger.AddHook(NewOutputHook(output))
}

// configureHTTP sets up HTTP output
func (m *Manager) configureHTTP() {
	url, err := m.settingsManager.Get("logging.http_url")
	if err != nil || url == "" {
		m.logger.Warn("HTTP logging enabled but no URL configured")
		m.closeOutput("http")
		return
	}

	token, err := m.settingsManager.Get("logging.http_auth_token")
	if err != nil {
		token = "" // default (no auth)
	}

	batchSize, err := m.settingsManager.GetInt("logging.http_batch_size")
	if err != nil {
		batchSize = 100 // default
	}

	flushInterval, err := m.settingsManager.GetInt("logging.http_flush_interval")
	if err != nil {
		flushInterval = 10 // default
	}

	// Close existing output if any
	m.closeOutput("http")

	// Create new HTTP output
	output := NewHTTPOutput(url, token, batchSize, time.Duration(flushInterval)*time.Second)

	m.outputs["http"] = output
	m.logger.WithFields(logrus.Fields{
		"url":            url,
		"batch_size":     batchSize,
		"flush_interval": flushInterval,
	}).Info("HTTP output configured")

	// Add hook to logrus
	m.logger.AddHook(NewOutputHook(output))
}

// closeOutput closes and removes an output
func (m *Manager) closeOutput(name string) {
	if output, exists := m.outputs[name]; exists {
		output.Close()
		delete(m.outputs, name)
		m.logger.WithField("output", name).Info("Output closed")
	}
}

// Close closes all outputs
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, output := range m.outputs {
		output.Close()
		m.logger.WithField("output", name).Info("Output closed on shutdown")
	}

	m.outputs = make(map[string]Output)
}

// TestOutput tests a specific output configuration
func (m *Manager) TestOutput(outputType string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.settingsManager == nil {
		return ErrSettingsManagerNotSet
	}

	switch outputType {
	case "syslog":
		return m.testSyslog()
	case "http":
		return m.testHTTP()
	default:
		return ErrInvalidOutputType
	}
}

// testSyslog sends a test message to syslog
func (m *Manager) testSyslog() error {
	host, err := m.settingsManager.Get("logging.syslog_host")
	if err != nil || host == "" {
		return ErrSyslogHostNotConfigured
	}

	port, err := m.settingsManager.GetInt("logging.syslog_port")
	if err != nil {
		port = 514
	}

	protocol, err := m.settingsManager.Get("logging.syslog_protocol")
	if err != nil {
		protocol = "tcp"
	}

	tag, err := m.settingsManager.Get("logging.syslog_tag")
	if err != nil {
		tag = "maxiofs"
	}

	output, err := NewSyslogOutput(protocol, host, port, tag)
	if err != nil {
		return err
	}
	defer output.Close()

	// Send test entry
	testEntry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "Syslog test message from MaxIOFS",
		Fields: map[string]interface{}{
			"test": true,
			"type": "syslog_connectivity_test",
		},
	}

	return output.Write(testEntry)
}

// testHTTP sends a test message to HTTP endpoint
func (m *Manager) testHTTP() error {
	url, err := m.settingsManager.Get("logging.http_url")
	if err != nil || url == "" {
		return ErrHTTPURLNotConfigured
	}

	token, err := m.settingsManager.Get("logging.http_auth_token")
	if err != nil {
		token = ""
	}
	output := NewHTTPOutput(url, token, 1, time.Second)
	defer output.Close()

	// Send test entry
	testEntry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "HTTP test message from MaxIOFS",
		Fields: map[string]interface{}{
			"test": true,
			"type": "http_connectivity_test",
		},
	}

	return output.Write(testEntry)
}
