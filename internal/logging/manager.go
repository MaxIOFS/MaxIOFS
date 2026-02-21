package logging

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Manager manages structured logging with multiple outputs
type Manager struct {
	settingsManager SettingsManager
	targetStore     *TargetStore
	outputs         map[string]Output        // target ID → Output
	targetConfigs   map[string]*TargetConfig // target ID → last known config (for change detection)
	dispatchHook    *DispatchHook            // single hook registered with logrus
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
	m := &Manager{
		outputs:       make(map[string]Output),
		targetConfigs: make(map[string]*TargetConfig),
		logger:        logger,
	}

	// Create and register a single dispatch hook that routes to all active outputs.
	// The hook uses an atomic snapshot, so Fire() never acquires the manager mutex.
	// This prevents deadlocks when Reconfigure() holds the write lock and logs via logrus.
	m.dispatchHook = NewDispatchHook()
	logger.AddHook(m.dispatchHook)

	return m
}

// SetSettingsManager sets the settings manager and reconfigures logging
func (m *Manager) SetSettingsManager(sm SettingsManager) {
	m.mu.Lock()
	m.settingsManager = sm
	m.mu.Unlock()

	// Initial configuration
	m.Reconfigure()
}

// SetTargetStore sets the target store for database-backed target management
func (m *Manager) SetTargetStore(store *TargetStore) {
	m.mu.Lock()
	m.targetStore = store
	m.mu.Unlock()

	// Reconfigure with database targets
	m.Reconfigure()
}

// InitTargetStore creates a target store from a database connection and migrates legacy settings
func (m *Manager) InitTargetStore(db *sql.DB) error {
	store, err := NewTargetStore(db, m.logger)
	if err != nil {
		return err
	}

	m.mu.RLock()
	sm := m.settingsManager
	m.mu.RUnlock()

	// Migrate legacy settings if the targets table is empty
	if sm != nil {
		targets, err := store.List()
		if err == nil && len(targets) == 0 {
			if err := store.MigrateFromSettings(sm); err != nil {
				m.logger.WithError(err).Warn("Failed to migrate legacy logging settings")
			}
		}
	}

	m.SetTargetStore(store)
	return nil
}

// GetTargetStore returns the target store (for use by API handlers)
func (m *Manager) GetTargetStore() *TargetStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.targetStore
}

// Reconfigure applies current settings from database.
// Logging statements are deferred until after the write lock is released
// to prevent deadlocks with the DispatchHook.
func (m *Manager) Reconfigure() {
	var format, levelStr string
	var includeCaller bool
	var activeTargets int

	m.mu.Lock()

	if m.settingsManager == nil {
		m.mu.Unlock()
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
	levelStr, err = m.settingsManager.Get("logging.level")
	if err != nil {
		levelStr = "info" // default
	}
	level, err := logrus.ParseLevel(levelStr)
	if err != nil {
		level = logrus.InfoLevel
	}
	m.logger.SetLevel(level)

	// Include caller information if enabled
	includeCaller, err = m.settingsManager.GetBool("logging.include_caller")
	if err != nil {
		includeCaller = false // default
	}
	m.logger.SetReportCaller(includeCaller)

	// Reconfigure outputs from target store
	if m.targetStore != nil {
		m.reconfigureFromStore()
	}

	// Publish the updated snapshot atomically for the DispatchHook
	m.publishSnapshot()

	activeTargets = len(m.outputs)
	m.mu.Unlock()

	// Log AFTER releasing the lock to avoid deadlock with DispatchHook
	m.logger.WithFields(logrus.Fields{
		"format":         format,
		"level":          levelStr,
		"include_caller": includeCaller,
		"active_targets": activeTargets,
	}).Info("Logging configuration updated")
}

// reconfigureFromStore reconciles running outputs with the database targets
func (m *Manager) reconfigureFromStore() {
	targets, err := m.targetStore.ListEnabled()
	if err != nil {
		m.logger.WithError(err).Error("Failed to list enabled logging targets")
		return
	}

	// Build set of desired target IDs
	desiredIDs := make(map[string]*TargetConfig, len(targets))
	for i := range targets {
		desiredIDs[targets[i].ID] = &targets[i]
	}

	// Close outputs that are no longer in the desired set
	for id, output := range m.outputs {
		if _, wanted := desiredIDs[id]; !wanted {
			output.Close()
			delete(m.outputs, id)
			delete(m.targetConfigs, id)
			m.logger.WithField("target_id", id).Info("Logging target removed")
		}
	}

	// Create or update outputs for desired targets
	for id, cfg := range desiredIDs {
		existing, exists := m.targetConfigs[id]
		if exists && !targetConfigChanged(existing, cfg) {
			continue // no change, skip
		}

		// Close existing output if it's being reconfigured
		if existingOutput, ok := m.outputs[id]; ok {
			existingOutput.Close()
			delete(m.outputs, id)
		}

		// Create new output
		output, err := m.createOutputFromConfig(cfg)
		if err != nil {
			m.logger.WithError(err).WithFields(logrus.Fields{
				"target_id":   id,
				"target_name": cfg.Name,
				"target_type": cfg.Type,
			}).Error("Failed to create logging output")
			continue
		}

		m.outputs[id] = output
		m.targetConfigs[id] = cfg

		m.logger.WithFields(logrus.Fields{
			"target_id":   id,
			"target_name": cfg.Name,
			"target_type": cfg.Type,
			"host":        cfg.Host,
			"port":        cfg.Port,
		}).Info("Logging target configured")
	}
}

// createOutputFromConfig creates an Output from a TargetConfig
func (m *Manager) createOutputFromConfig(cfg *TargetConfig) (Output, error) {
	switch cfg.Type {
	case string(TargetTypeSyslog):
		return NewSyslogOutputWithConfig(SyslogConfig{
			Protocol:      cfg.Protocol,
			Host:          cfg.Host,
			Port:          cfg.Port,
			Tag:           cfg.Tag,
			Format:        cfg.Format,
			TLSEnabled:    cfg.TLSEnabled,
			TLSCert:       cfg.TLSCert,
			TLSKey:        cfg.TLSKey,
			TLSCA:         cfg.TLSCA,
			TLSSkipVerify: cfg.TLSSkipVerify,
		})

	case string(TargetTypeHTTP):
		batchSize := cfg.BatchSize
		if batchSize <= 0 {
			batchSize = 100
		}
		flushInterval := cfg.FlushInterval
		if flushInterval <= 0 {
			flushInterval = 10
		}
		return NewHTTPOutput(
			cfg.URL,
			cfg.AuthToken,
			batchSize,
			time.Duration(flushInterval)*time.Second,
		), nil

	default:
		return nil, fmt.Errorf("unknown target type: %s", cfg.Type)
	}
}

// closeOutput closes and removes a single output by key
func (m *Manager) closeOutput(name string) {
	if output, exists := m.outputs[name]; exists {
		output.Close()
		delete(m.outputs, name)
		delete(m.targetConfigs, name)
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
	m.targetConfigs = make(map[string]*TargetConfig)

	// Publish empty snapshot so DispatchHook stops dispatching
	m.publishSnapshot()
}

// GetActiveOutputs returns the count of active outputs (for monitoring)
func (m *Manager) GetActiveOutputs() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.outputs)
}

// TestTarget tests connectivity to a specific target by ID
func (m *Manager) TestTarget(id string) error {
	m.mu.RLock()
	store := m.targetStore
	m.mu.RUnlock()

	if store == nil {
		return ErrSettingsManagerNotSet
	}

	cfg, err := store.Get(id)
	if err != nil {
		return fmt.Errorf("target not found: %w", err)
	}

	return m.TestTargetConfig(cfg)
}

// TestTargetConfig tests a target configuration without saving it
func (m *Manager) TestTargetConfig(cfg *TargetConfig) error {
	output, err := m.createOutputFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create test output: %w", err)
	}
	defer output.Close()

	testEntry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "MaxIOFS logging target connectivity test",
		Fields: map[string]interface{}{
			"test":        true,
			"target_name": cfg.Name,
			"target_type": cfg.Type,
		},
	}

	return output.Write(testEntry)
}

// publishSnapshot builds and atomically publishes the outputs snapshot
// to the DispatchHook. MUST be called under the write lock.
func (m *Manager) publishSnapshot() {
	snapshot := make([]outputWithFilter, 0, len(m.outputs))
	for id, output := range m.outputs {
		filterLevel := "debug" // default: send everything
		if cfg, ok := m.targetConfigs[id]; ok {
			filterLevel = cfg.FilterLevel
		}
		snapshot = append(snapshot, outputWithFilter{
			output:      output,
			filterLevel: filterLevel,
		})
	}
	m.dispatchHook.UpdateSnapshot(snapshot)
}

// outputWithFilter pairs an output with its minimum log level filter
type outputWithFilter struct {
	output      Output
	filterLevel string
}

// targetConfigChanged checks if a target config has changed in a way that requires reconnection
func targetConfigChanged(old, new *TargetConfig) bool {
	if old.Type != new.Type {
		return true
	}
	if old.Protocol != new.Protocol {
		return true
	}
	if old.Host != new.Host {
		return true
	}
	if old.Port != new.Port {
		return true
	}
	if old.Tag != new.Tag {
		return true
	}
	if old.Format != new.Format {
		return true
	}
	if old.TLSEnabled != new.TLSEnabled {
		return true
	}
	if old.TLSCert != new.TLSCert {
		return true
	}
	if old.TLSKey != new.TLSKey {
		return true
	}
	if old.TLSCA != new.TLSCA {
		return true
	}
	if old.TLSSkipVerify != new.TLSSkipVerify {
		return true
	}
	if old.FilterLevel != new.FilterLevel {
		return true
	}
	if old.URL != new.URL {
		return true
	}
	if old.AuthToken != new.AuthToken {
		return true
	}
	if old.BatchSize != new.BatchSize {
		return true
	}
	if old.FlushInterval != new.FlushInterval {
		return true
	}
	return false
}
