package logging

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// TargetType represents the type of logging target
type TargetType string

const (
	TargetTypeSyslog TargetType = "syslog"
	TargetTypeHTTP   TargetType = "http"
)

// TargetConfig represents a logging target stored in the database
type TargetConfig struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"` // "syslog" | "http"
	Enabled       bool      `json:"enabled"`
	Protocol      string    `json:"protocol"` // "tcp" | "udp" | "tcp+tls"
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	Tag           string    `json:"tag"`
	Format        string    `json:"format"` // "rfc3164" | "rfc5424"
	TLSEnabled    bool      `json:"tls_enabled"`
	TLSCert       string    `json:"tls_cert,omitempty"`
	TLSKey        string    `json:"tls_key,omitempty"`
	TLSCA         string    `json:"tls_ca,omitempty"`
	TLSSkipVerify bool      `json:"tls_skip_verify"`
	FilterLevel   string    `json:"filter_level"`             // "debug" | "info" | "warn" | "error"
	AuthToken     string    `json:"auth_token,omitempty"`     // For HTTP targets
	URL           string    `json:"url,omitempty"`            // For HTTP targets
	BatchSize     int       `json:"batch_size,omitempty"`     // For HTTP targets
	FlushInterval int       `json:"flush_interval,omitempty"` // For HTTP targets (seconds)
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TargetStore manages logging targets in SQLite
type TargetStore struct {
	db     *sql.DB
	logger *logrus.Logger
}

// NewTargetStore creates a new target store
func NewTargetStore(db *sql.DB, logger *logrus.Logger) (*TargetStore, error) {
	s := &TargetStore{
		db:     db,
		logger: logger,
	}
	return s, nil
}

// List returns all logging targets
func (s *TargetStore) List() ([]TargetConfig, error) {
	query := `
	SELECT id, name, type, enabled, protocol, host, port, tag, format,
	       tls_enabled, tls_cert, tls_key, tls_ca, tls_skip_verify,
	       filter_level, auth_token, url, batch_size, flush_interval,
	       created_at, updated_at
	FROM logging_targets
	ORDER BY name
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query logging targets: %w", err)
	}
	defer rows.Close()

	return s.scanTargets(rows)
}

// ListEnabled returns all enabled logging targets
func (s *TargetStore) ListEnabled() ([]TargetConfig, error) {
	query := `
	SELECT id, name, type, enabled, protocol, host, port, tag, format,
	       tls_enabled, tls_cert, tls_key, tls_ca, tls_skip_verify,
	       filter_level, auth_token, url, batch_size, flush_interval,
	       created_at, updated_at
	FROM logging_targets
	WHERE enabled = 1
	ORDER BY name
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query enabled logging targets: %w", err)
	}
	defer rows.Close()

	return s.scanTargets(rows)
}

// Get returns a single logging target by ID
func (s *TargetStore) Get(id string) (*TargetConfig, error) {
	query := `
	SELECT id, name, type, enabled, protocol, host, port, tag, format,
	       tls_enabled, tls_cert, tls_key, tls_ca, tls_skip_verify,
	       filter_level, auth_token, url, batch_size, flush_interval,
	       created_at, updated_at
	FROM logging_targets
	WHERE id = ?
	`

	var cfg TargetConfig
	var enabled, tlsEnabled, tlsSkipVerify int
	var createdAt, updatedAt int64
	var tlsCert, tlsKey, tlsCA, authToken, url sql.NullString
	var batchSize, flushInterval sql.NullInt64

	err := s.db.QueryRow(query, id).Scan(
		&cfg.ID, &cfg.Name, &cfg.Type, &enabled,
		&cfg.Protocol, &cfg.Host, &cfg.Port, &cfg.Tag, &cfg.Format,
		&tlsEnabled, &tlsCert, &tlsKey, &tlsCA, &tlsSkipVerify,
		&cfg.FilterLevel, &authToken, &url, &batchSize, &flushInterval,
		&createdAt, &updatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("logging target not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get logging target: %w", err)
	}

	cfg.Enabled = enabled == 1
	cfg.TLSEnabled = tlsEnabled == 1
	cfg.TLSSkipVerify = tlsSkipVerify == 1
	cfg.TLSCert = nullStringValue(tlsCert)
	cfg.TLSKey = nullStringValue(tlsKey)
	cfg.TLSCA = nullStringValue(tlsCA)
	cfg.AuthToken = nullStringValue(authToken)
	cfg.URL = nullStringValue(url)
	cfg.BatchSize = int(nullInt64Value(batchSize))
	cfg.FlushInterval = int(nullInt64Value(flushInterval))
	cfg.CreatedAt = time.Unix(createdAt, 0)
	cfg.UpdatedAt = time.Unix(updatedAt, 0)

	return &cfg, nil
}

// Create inserts a new logging target
func (s *TargetStore) Create(cfg *TargetConfig) error {
	if cfg.ID == "" {
		cfg.ID = uuid.New().String()
	}

	now := time.Now()
	cfg.CreatedAt = now
	cfg.UpdatedAt = now

	if err := validateTargetConfig(cfg); err != nil {
		return err
	}

	query := `
	INSERT INTO logging_targets (
		id, name, type, enabled, protocol, host, port, tag, format,
		tls_enabled, tls_cert, tls_key, tls_ca, tls_skip_verify,
		filter_level, auth_token, url, batch_size, flush_interval,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.Exec(query,
		cfg.ID, cfg.Name, cfg.Type, boolToInt(cfg.Enabled),
		cfg.Protocol, cfg.Host, cfg.Port, cfg.Tag, cfg.Format,
		boolToInt(cfg.TLSEnabled), nullString(cfg.TLSCert), nullString(cfg.TLSKey),
		nullString(cfg.TLSCA), boolToInt(cfg.TLSSkipVerify),
		cfg.FilterLevel, nullString(cfg.AuthToken), nullString(cfg.URL),
		nullInt(cfg.BatchSize), nullInt(cfg.FlushInterval),
		now.Unix(), now.Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to create logging target: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"id":   cfg.ID,
		"name": cfg.Name,
		"type": cfg.Type,
		"host": cfg.Host,
	}).Info("Logging target created")

	return nil
}

// Update modifies an existing logging target
func (s *TargetStore) Update(cfg *TargetConfig) error {
	if err := validateTargetConfig(cfg); err != nil {
		return err
	}

	now := time.Now()
	cfg.UpdatedAt = now

	query := `
	UPDATE logging_targets SET
		name = ?, type = ?, enabled = ?, protocol = ?, host = ?, port = ?,
		tag = ?, format = ?, tls_enabled = ?, tls_cert = ?, tls_key = ?,
		tls_ca = ?, tls_skip_verify = ?, filter_level = ?,
		auth_token = ?, url = ?, batch_size = ?, flush_interval = ?,
		updated_at = ?
	WHERE id = ?
	`

	result, err := s.db.Exec(query,
		cfg.Name, cfg.Type, boolToInt(cfg.Enabled), cfg.Protocol,
		cfg.Host, cfg.Port, cfg.Tag, cfg.Format,
		boolToInt(cfg.TLSEnabled), nullString(cfg.TLSCert), nullString(cfg.TLSKey),
		nullString(cfg.TLSCA), boolToInt(cfg.TLSSkipVerify),
		cfg.FilterLevel, nullString(cfg.AuthToken), nullString(cfg.URL),
		nullInt(cfg.BatchSize), nullInt(cfg.FlushInterval),
		now.Unix(), cfg.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update logging target: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("logging target not found: %s", cfg.ID)
	}

	s.logger.WithFields(logrus.Fields{
		"id":   cfg.ID,
		"name": cfg.Name,
	}).Info("Logging target updated")

	return nil
}

// Delete removes a logging target
func (s *TargetStore) Delete(id string) error {
	result, err := s.db.Exec("DELETE FROM logging_targets WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete logging target: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("logging target not found: %s", id)
	}

	s.logger.WithField("id", id).Info("Logging target deleted")
	return nil
}

// MigrateFromSettings migrates old syslog/http settings to new logging_targets table.
// This is called once during the migration to preserve existing configs.
func (s *TargetStore) MigrateFromSettings(sm SettingsManager) error {
	// Migrate syslog settings
	syslogEnabled, err := sm.GetBool("logging.syslog_enabled")
	if err == nil && syslogEnabled {
		host, _ := sm.Get("logging.syslog_host")
		if host != "" {
			port, _ := sm.GetInt("logging.syslog_port")
			if port == 0 {
				port = 514
			}
			protocol, _ := sm.Get("logging.syslog_protocol")
			if protocol == "" {
				protocol = "tcp"
			}
			tag, _ := sm.Get("logging.syslog_tag")
			if tag == "" {
				tag = "maxiofs"
			}

			cfg := &TargetConfig{
				Name:        "Syslog (migrated)",
				Type:        string(TargetTypeSyslog),
				Enabled:     true,
				Protocol:    protocol,
				Host:        host,
				Port:        port,
				Tag:         tag,
				Format:      "rfc3164",
				FilterLevel: "info",
			}
			if err := s.Create(cfg); err != nil {
				s.logger.WithError(err).Warn("Failed to migrate syslog settings")
			} else {
				s.logger.Info("Migrated legacy syslog settings to logging_targets")
			}
		}
	}

	// Migrate HTTP settings
	httpEnabled, err := sm.GetBool("logging.http_enabled")
	if err == nil && httpEnabled {
		url, _ := sm.Get("logging.http_url")
		if url != "" {
			token, _ := sm.Get("logging.http_auth_token")
			batchSize, _ := sm.GetInt("logging.http_batch_size")
			if batchSize == 0 {
				batchSize = 100
			}
			flushInterval, _ := sm.GetInt("logging.http_flush_interval")
			if flushInterval == 0 {
				flushInterval = 10
			}

			cfg := &TargetConfig{
				Name:          "HTTP (migrated)",
				Type:          string(TargetTypeHTTP),
				Enabled:       true,
				Protocol:      "https",
				Host:          url,
				Port:          443,
				Format:        "rfc3164",
				FilterLevel:   "info",
				AuthToken:     token,
				URL:           url,
				BatchSize:     batchSize,
				FlushInterval: flushInterval,
			}
			if err := s.Create(cfg); err != nil {
				s.logger.WithError(err).Warn("Failed to migrate HTTP logging settings")
			} else {
				s.logger.Info("Migrated legacy HTTP logging settings to logging_targets")
			}
		}
	}

	return nil
}

// scanTargets scans rows into TargetConfig slice
func (s *TargetStore) scanTargets(rows *sql.Rows) ([]TargetConfig, error) {
	var targets []TargetConfig
	for rows.Next() {
		var cfg TargetConfig
		var enabled, tlsEnabled, tlsSkipVerify int
		var createdAt, updatedAt int64
		var tlsCert, tlsKey, tlsCA, authToken, url sql.NullString
		var batchSize, flushInterval sql.NullInt64

		err := rows.Scan(
			&cfg.ID, &cfg.Name, &cfg.Type, &enabled,
			&cfg.Protocol, &cfg.Host, &cfg.Port, &cfg.Tag, &cfg.Format,
			&tlsEnabled, &tlsCert, &tlsKey, &tlsCA, &tlsSkipVerify,
			&cfg.FilterLevel, &authToken, &url, &batchSize, &flushInterval,
			&createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan logging target: %w", err)
		}

		cfg.Enabled = enabled == 1
		cfg.TLSEnabled = tlsEnabled == 1
		cfg.TLSSkipVerify = tlsSkipVerify == 1
		cfg.TLSCert = nullStringValue(tlsCert)
		cfg.TLSKey = nullStringValue(tlsKey)
		cfg.TLSCA = nullStringValue(tlsCA)
		cfg.AuthToken = nullStringValue(authToken)
		cfg.URL = nullStringValue(url)
		cfg.BatchSize = int(nullInt64Value(batchSize))
		cfg.FlushInterval = int(nullInt64Value(flushInterval))
		cfg.CreatedAt = time.Unix(createdAt, 0)
		cfg.UpdatedAt = time.Unix(updatedAt, 0)

		targets = append(targets, cfg)
	}

	return targets, nil
}

// validateTargetConfig validates a target configuration
func validateTargetConfig(cfg *TargetConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("target name is required")
	}
	if cfg.Type != string(TargetTypeSyslog) && cfg.Type != string(TargetTypeHTTP) {
		return fmt.Errorf("invalid target type: %s (must be 'syslog' or 'http')", cfg.Type)
	}

	switch cfg.Type {
	case string(TargetTypeSyslog):
		if cfg.Host == "" {
			return fmt.Errorf("host is required for syslog targets")
		}
		if cfg.Port <= 0 || cfg.Port > 65535 {
			return fmt.Errorf("port must be between 1 and 65535")
		}
		if cfg.Protocol != "tcp" && cfg.Protocol != "udp" && cfg.Protocol != "tcp+tls" {
			return fmt.Errorf("invalid protocol: %s (must be 'tcp', 'udp', or 'tcp+tls')", cfg.Protocol)
		}
		if cfg.Format != "rfc3164" && cfg.Format != "rfc5424" {
			return fmt.Errorf("invalid format: %s (must be 'rfc3164' or 'rfc5424')", cfg.Format)
		}
	case string(TargetTypeHTTP):
		if cfg.URL == "" {
			return fmt.Errorf("URL is required for HTTP targets")
		}
	}

	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[cfg.FilterLevel] {
		return fmt.Errorf("invalid filter level: %s (must be 'debug', 'info', 'warn', or 'error')", cfg.FilterLevel)
	}

	return nil
}

// Helper functions for SQL null handling
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt(i int) sql.NullInt64 {
	if i == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(i), Valid: true}
}

func nullStringValue(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func nullInt64Value(ni sql.NullInt64) int64 {
	if ni.Valid {
		return ni.Int64
	}
	return 0
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
