package settings

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Manager manages system settings stored in SQLite
type Manager struct {
	db     *sql.DB
	logger *logrus.Logger
}

// NewManager creates a new settings manager
func NewManager(db *sql.DB, logger *logrus.Logger) (*Manager, error) {
	m := &Manager{
		db:     db,
		logger: logger,
	}

	// Initialize database schema
	if err := m.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Remove deprecated settings from older versions
	if err := m.removeDeprecated(); err != nil {
		return nil, fmt.Errorf("failed to remove deprecated settings: %w", err)
	}

	// Insert default settings
	if err := m.insertDefaults(); err != nil {
		return nil, fmt.Errorf("failed to insert defaults: %w", err)
	}

	return m, nil
}

// initSchema creates the system_settings table
func (m *Manager) initSchema() error {
	query := `
	CREATE TABLE IF NOT EXISTS system_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		type TEXT NOT NULL,
		category TEXT NOT NULL,
		description TEXT,
		editable INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_settings_category ON system_settings(category);
	`

	_, err := m.db.Exec(query)
	return err
}

// insertDefaults inserts default settings if they don't exist
func (m *Manager) insertDefaults() error {
	defaults := []Setting{
		// Security Settings
		{
			Key:         "security.session_timeout",
			Value:       "86400",
			Type:        string(TypeInt),
			Category:    string(CategorySecurity),
			Description: "Session timeout in seconds (24 hours)",
			Editable:    true,
		},
		{
			Key:         "security.max_failed_attempts",
			Value:       "5",
			Type:        string(TypeInt),
			Category:    string(CategorySecurity),
			Description: "Maximum failed login attempts before account lockout",
			Editable:    true,
		},
		{
			Key:         "security.lockout_duration",
			Value:       "900",
			Type:        string(TypeInt),
			Category:    string(CategorySecurity),
			Description: "Account lockout duration in seconds (15 minutes)",
			Editable:    true,
		},
		{
			Key:         "security.require_2fa_admin",
			Value:       "false",
			Type:        string(TypeBool),
			Category:    string(CategorySecurity),
			Description: "Require 2FA for all admin users",
			Editable:    true,
		},
		{
			Key:         "security.password_min_length",
			Value:       "8",
			Type:        string(TypeInt),
			Category:    string(CategorySecurity),
			Description: "Minimum password length",
			Editable:    true,
		},
		{
			Key:         "security.password_require_uppercase",
			Value:       "true",
			Type:        string(TypeBool),
			Category:    string(CategorySecurity),
			Description: "Require uppercase letters in passwords",
			Editable:    true,
		},
		{
			Key:         "security.password_require_numbers",
			Value:       "true",
			Type:        string(TypeBool),
			Category:    string(CategorySecurity),
			Description: "Require numbers in passwords",
			Editable:    true,
		},
		{
			Key:         "security.password_require_special",
			Value:       "false",
			Type:        string(TypeBool),
			Category:    string(CategorySecurity),
			Description: "Require special characters in passwords",
			Editable:    true,
		},

		// Audit Settings
		{
			Key:         "audit.enabled",
			Value:       "true",
			Type:        string(TypeBool),
			Category:    string(CategoryAudit),
			Description: "Enable audit logging",
			Editable:    true,
		},
		{
			Key:         "audit.retention_days",
			Value:       "90",
			Type:        string(TypeInt),
			Category:    string(CategoryAudit),
			Description: "Audit log retention period in days",
			Editable:    true,
		},
		{
			Key:         "audit.log_s3_operations",
			Value:       "true",
			Type:        string(TypeBool),
			Category:    string(CategoryAudit),
			Description: "Log S3 API operations",
			Editable:    true,
		},
		{
			Key:         "audit.log_console_operations",
			Value:       "true",
			Type:        string(TypeBool),
			Category:    string(CategoryAudit),
			Description: "Log Console API operations",
			Editable:    true,
		},

		// Storage Settings
		{
			Key:         "storage.default_bucket_versioning",
			Value:       "false",
			Type:        string(TypeBool),
			Category:    string(CategoryStorage),
			Description: "Enable versioning by default for new buckets",
			Editable:    true,
		},
		{
			Key:         "storage.default_object_lock_days",
			Value:       "7",
			Type:        string(TypeInt),
			Category:    string(CategoryStorage),
			Description: "Default object lock retention period in days",
			Editable:    true,
		},
		// Metrics Settings
		{
			Key:         "metrics.enabled",
			Value:       "true",
			Type:        string(TypeBool),
			Category:    string(CategoryMetrics),
			Description: "Enable Prometheus metrics endpoint",
			Editable:    true,
		},
		{
			Key:         "metrics.collection_interval",
			Value:       "10",
			Type:        string(TypeInt),
			Category:    string(CategoryMetrics),
			Description: "Metrics collection interval in seconds",
			Editable:    true,
		},

		// Rate Limiting Settings
		{
			Key:         "security.ratelimit_enabled",
			Value:       "true",
			Type:        string(TypeBool),
			Category:    string(CategorySecurity),
			Description: "Enable rate limiting",
			Editable:    true,
		},
		{
			Key:         "security.ratelimit_login_per_minute",
			Value:       "5",
			Type:        string(TypeInt),
			Category:    string(CategorySecurity),
			Description: "Maximum login attempts per minute per IP",
			Editable:    true,
		},
		{
			Key:         "security.ratelimit_api_per_second",
			Value:       "100",
			Type:        string(TypeInt),
			Category:    string(CategorySecurity),
			Description: "Maximum API requests per second per user",
			Editable:    true,
		},

		// Logging Settings
		{
			Key:         "logging.format",
			Value:       "json",
			Type:        string(TypeString),
			Category:    string(CategoryLogging),
			Description: "Log format (json or text)",
			Editable:    true,
		},
		{
			Key:         "logging.level",
			Value:       "info",
			Type:        string(TypeString),
			Category:    string(CategoryLogging),
			Description: "Log level (debug, info, warn, error)",
			Editable:    true,
		},
		{
			Key:         "logging.include_caller",
			Value:       "false",
			Type:        string(TypeBool),
			Category:    string(CategoryLogging),
			Description: "Include file and line number in logs",
			Editable:    true,
		},
		{
			Key:         "logging.syslog_enabled",
			Value:       "false",
			Type:        string(TypeBool),
			Category:    string(CategoryLogging),
			Description: "Enable syslog output",
			Editable:    true,
		},
		{
			Key:         "logging.syslog_protocol",
			Value:       "tcp",
			Type:        string(TypeString),
			Category:    string(CategoryLogging),
			Description: "Syslog protocol (tcp or udp)",
			Editable:    true,
		},
		{
			Key:         "logging.syslog_host",
			Value:       "",
			Type:        string(TypeString),
			Category:    string(CategoryLogging),
			Description: "Syslog server hostname or IP",
			Editable:    true,
		},
		{
			Key:         "logging.syslog_port",
			Value:       "514",
			Type:        string(TypeInt),
			Category:    string(CategoryLogging),
			Description: "Syslog server port",
			Editable:    true,
		},
		{
			Key:         "logging.syslog_tag",
			Value:       "maxiofs",
			Type:        string(TypeString),
			Category:    string(CategoryLogging),
			Description: "Syslog tag/identifier",
			Editable:    true,
		},
		{
			Key:         "logging.http_enabled",
			Value:       "false",
			Type:        string(TypeBool),
			Category:    string(CategoryLogging),
			Description: "Enable HTTP log endpoint (Elastic, Splunk, etc.)",
			Editable:    true,
		},
		{
			Key:         "logging.http_url",
			Value:       "",
			Type:        string(TypeString),
			Category:    string(CategoryLogging),
			Description: "HTTP endpoint URL for logs",
			Editable:    true,
		},
		{
			Key:         "logging.http_auth_token",
			Value:       "",
			Type:        string(TypeString),
			Category:    string(CategoryLogging),
			Description: "HTTP endpoint authentication token",
			Editable:    true,
		},
		{
			Key:         "logging.http_batch_size",
			Value:       "100",
			Type:        string(TypeInt),
			Category:    string(CategoryLogging),
			Description: "Number of logs to batch before sending",
			Editable:    true,
		},
		{
			Key:         "logging.http_flush_interval",
			Value:       "10",
			Type:        string(TypeInt),
			Category:    string(CategoryLogging),
			Description: "Flush interval in seconds",
			Editable:    true,
		},
		{
			Key:         "logging.frontend_enabled",
			Value:       "true",
			Type:        string(TypeBool),
			Category:    string(CategoryLogging),
			Description: "Accept logs from frontend",
			Editable:    true,
		},
		{
			Key:         "logging.frontend_level",
			Value:       "error",
			Type:        string(TypeString),
			Category:    string(CategoryLogging),
			Description: "Minimum log level for frontend logs (debug, info, warn, error)",
			Editable:    true,
		},

		// System Settings
		{
			Key:         "system.maintenance_mode",
			Value:       "false",
			Type:        string(TypeBool),
			Category:    string(CategorySystem),
			Description: "Enable maintenance mode (read-only access)",
			Editable:    true,
		},
		{
			Key:         "system.max_upload_size_mb",
			Value:       "5120",
			Type:        string(TypeInt),
			Category:    string(CategorySystem),
			Description: "Maximum upload size in MB (5GB default)",
			Editable:    true,
		},
		{
			Key:         "system.disk_warning_threshold",
			Value:       "80",
			Type:        string(TypeInt),
			Category:    string(CategorySystem),
			Description: "Disk usage warning threshold percentage (send alert when disk is above this %)",
			Editable:    true,
		},
		{
			Key:         "system.disk_critical_threshold",
			Value:       "90",
			Type:        string(TypeInt),
			Category:    string(CategorySystem),
			Description: "Disk usage critical threshold percentage (send urgent alert when disk is above this %)",
			Editable:    true,
		},

		// Email / SMTP Settings
		{
			Key:         "email.enabled",
			Value:       "false",
			Type:        string(TypeBool),
			Category:    string(CategoryEmail),
			Description: "Enable email notifications (requires SMTP configuration below)",
			Editable:    true,
		},
		{
			Key:         "email.smtp_host",
			Value:       "",
			Type:        string(TypeString),
			Category:    string(CategoryEmail),
			Description: "SMTP server hostname or IP address (e.g. smtp.gmail.com)",
			Editable:    true,
		},
		{
			Key:         "email.smtp_port",
			Value:       "587",
			Type:        string(TypeInt),
			Category:    string(CategoryEmail),
			Description: "SMTP server port (587 for STARTTLS, 465 for implicit TLS, 25 for plain)",
			Editable:    true,
		},
		{
			Key:         "email.smtp_user",
			Value:       "",
			Type:        string(TypeString),
			Category:    string(CategoryEmail),
			Description: "SMTP authentication username (leave empty if server does not require auth)",
			Editable:    true,
		},
		{
			Key:         "email.smtp_password",
			Value:       "",
			Type:        string(TypeString),
			Category:    string(CategoryEmail),
			Description: "SMTP authentication password (stored in plain text in SQLite)",
			Editable:    true,
		},
		{
			Key:         "email.from_address",
			Value:       "",
			Type:        string(TypeString),
			Category:    string(CategoryEmail),
			Description: "Sender address for outgoing emails (e.g. alerts@yourdomain.com)",
			Editable:    true,
		},
		{
			Key:         "email.tls_mode",
			Value:       "none",
			Type:        string(TypeString),
			Category:    string(CategoryEmail),
			Description: "SMTP TLS mode: none = plain SMTP (port 25), starttls = upgrade with STARTTLS (port 587), ssl = implicit TLS (port 465)",
			Editable:    true,
		},
		{
			Key:         "email.skip_tls_verify",
			Value:       "false",
			Type:        string(TypeBool),
			Category:    string(CategoryEmail),
			Description: "Skip TLS certificate verification — enable for self-signed certificates (insecure, use only in trusted networks)",
			Editable:    true,
		},
	}

	now := time.Now().Unix()

	for _, setting := range defaults {
		query := `
		INSERT OR IGNORE INTO system_settings (key, value, type, category, description, editable, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`
		_, err := m.db.Exec(query, setting.Key, setting.Value, setting.Type, setting.Category, setting.Description, setting.Editable, now, now)
		if err != nil {
			return fmt.Errorf("failed to insert default setting %s: %w", setting.Key, err)
		}
	}

	m.logger.Info("System settings initialized with defaults")
	return nil
}

// Get retrieves a setting value as a string
func (m *Manager) Get(key string) (string, error) {
	var value string
	query := `SELECT value FROM system_settings WHERE key = ?`
	err := m.db.QueryRow(query, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("setting not found: %s", key)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get setting: %w", err)
	}
	return value, nil
}

// GetInt retrieves a setting value as an integer
func (m *Manager) GetInt(key string) (int, error) {
	value, err := m.Get(key)
	if err != nil {
		return 0, err
	}

	intValue, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("setting %s is not a valid integer: %w", key, err)
	}
	return intValue, nil
}

// GetBool retrieves a setting value as a boolean
func (m *Manager) GetBool(key string) (bool, error) {
	value, err := m.Get(key)
	if err != nil {
		return false, err
	}

	// Accept: true, false, 1, 0, yes, no (case-insensitive)
	lowerValue := strings.ToLower(value)
	switch lowerValue {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("setting %s is not a valid boolean: %s", key, value)
	}
}

// GetSetting retrieves a full setting object
func (m *Manager) GetSetting(key string) (*Setting, error) {
	var setting Setting
	query := `
	SELECT key, value, type, category, description, editable, created_at, updated_at
	FROM system_settings
	WHERE key = ?
	`

	var createdAt, updatedAt int64
	err := m.db.QueryRow(query, key).Scan(
		&setting.Key,
		&setting.Value,
		&setting.Type,
		&setting.Category,
		&setting.Description,
		&setting.Editable,
		&createdAt,
		&updatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("setting not found: %s", key)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get setting: %w", err)
	}

	setting.CreatedAt = time.Unix(createdAt, 0)
	setting.UpdatedAt = time.Unix(updatedAt, 0)

	return &setting, nil
}

// Set updates a setting value
func (m *Manager) Set(key, value string) error {
	// Get the setting to validate it exists and is editable
	setting, err := m.GetSetting(key)
	if err != nil {
		return err
	}

	if !setting.Editable {
		return fmt.Errorf("setting %s is not editable", key)
	}

	// Validate value based on type
	if err := m.validateValue(value, setting.Type); err != nil {
		return fmt.Errorf("invalid value for setting %s: %w", key, err)
	}

	// Update the setting
	query := `UPDATE system_settings SET value = ?, updated_at = ? WHERE key = ?`
	now := time.Now().Unix()

	_, err = m.db.Exec(query, value, now, key)
	if err != nil {
		return fmt.Errorf("failed to update setting: %w", err)
	}

	m.logger.WithFields(logrus.Fields{
		"key":   key,
		"value": value,
	}).Info("Setting updated")

	return nil
}

// validateValue validates a value based on its type
func (m *Manager) validateValue(value, settingType string) error {
	switch settingType {
	case string(TypeInt):
		_, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("value must be a valid integer")
		}
	case string(TypeBool):
		lowerValue := strings.ToLower(value)
		if lowerValue != "true" && lowerValue != "false" && lowerValue != "1" && lowerValue != "0" {
			return fmt.Errorf("value must be true, false, 1, or 0")
		}
	case string(TypeString):
		// Any string is valid
	case string(TypeJSON):
		// TODO: Validate JSON format
	default:
		return fmt.Errorf("unknown type: %s", settingType)
	}
	return nil
}

// ListAll retrieves all settings
func (m *Manager) ListAll() ([]Setting, error) {
	query := `
	SELECT key, value, type, category, description, editable, created_at, updated_at
	FROM system_settings
	ORDER BY category, key
	`

	rows, err := m.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query settings: %w", err)
	}
	defer rows.Close()

	var settings []Setting
	for rows.Next() {
		var setting Setting
		var createdAt, updatedAt int64

		err := rows.Scan(
			&setting.Key,
			&setting.Value,
			&setting.Type,
			&setting.Category,
			&setting.Description,
			&setting.Editable,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan setting: %w", err)
		}

		setting.CreatedAt = time.Unix(createdAt, 0)
		setting.UpdatedAt = time.Unix(updatedAt, 0)

		settings = append(settings, setting)
	}

	return settings, nil
}

// ListByCategory retrieves all settings in a specific category
func (m *Manager) ListByCategory(category string) ([]Setting, error) {
	query := `
	SELECT key, value, type, category, description, editable, created_at, updated_at
	FROM system_settings
	WHERE category = ?
	ORDER BY key
	`

	rows, err := m.db.Query(query, category)
	if err != nil {
		return nil, fmt.Errorf("failed to query settings: %w", err)
	}
	defer rows.Close()

	var settings []Setting
	for rows.Next() {
		var setting Setting
		var createdAt, updatedAt int64

		err := rows.Scan(
			&setting.Key,
			&setting.Value,
			&setting.Type,
			&setting.Category,
			&setting.Description,
			&setting.Editable,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan setting: %w", err)
		}

		setting.CreatedAt = time.Unix(createdAt, 0)
		setting.UpdatedAt = time.Unix(updatedAt, 0)

		settings = append(settings, setting)
	}

	return settings, nil
}

// BulkUpdate updates multiple settings at once
func (m *Manager) BulkUpdate(updates map[string]string) error {
	// Start transaction
	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().Unix()

	for key, value := range updates {
		// Get setting to validate
		setting, err := m.GetSetting(key)
		if err != nil {
			return fmt.Errorf("invalid setting %s: %w", key, err)
		}

		if !setting.Editable {
			return fmt.Errorf("setting %s is not editable", key)
		}

		// Validate value
		if err := m.validateValue(value, setting.Type); err != nil {
			return fmt.Errorf("invalid value for %s: %w", key, err)
		}

		// Update
		query := `UPDATE system_settings SET value = ?, updated_at = ? WHERE key = ?`
		_, err = tx.Exec(query, value, now, key)
		if err != nil {
			return fmt.Errorf("failed to update %s: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	m.logger.WithField("count", len(updates)).Info("Bulk settings update completed")
	return nil
}

// GetCategories returns all unique categories
func (m *Manager) GetCategories() ([]string, error) {
	query := `SELECT DISTINCT category FROM system_settings ORDER BY category`

	rows, err := m.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query categories: %w", err)
	}
	defer rows.Close()

	var categories []string
	for rows.Next() {
		var category string
		if err := rows.Scan(&category); err != nil {
			return nil, fmt.Errorf("failed to scan category: %w", err)
		}
		categories = append(categories, category)
	}

	return categories, nil
}

// removeDeprecated deletes settings that have been removed in newer versions.
func (m *Manager) removeDeprecated() error {
	deprecated := []string{
		"storage.enable_compression",
		"storage.compression_level",
		"email.use_tls", // replaced by email.tls_mode
	}
	for _, key := range deprecated {
		if _, err := m.db.Exec(`DELETE FROM system_settings WHERE key = ?`, key); err != nil {
			return fmt.Errorf("failed to remove deprecated setting %s: %w", key, err)
		}
	}
	return nil
}
