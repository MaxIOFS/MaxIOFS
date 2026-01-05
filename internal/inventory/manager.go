package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Manager handles inventory configuration and report operations
type Manager struct {
	db  *sql.DB
	log *logrus.Entry
}

// NewManager creates a new inventory manager
func NewManager(db *sql.DB) *Manager {
	return &Manager{
		db:  db,
		log: logrus.WithField("component", "inventory_manager"),
	}
}

// CreateConfig creates a new inventory configuration
func (m *Manager) CreateConfig(ctx context.Context, config *InventoryConfig) error {
	if config.ID == "" {
		config.ID = uuid.New().String()
	}

	now := time.Now().Unix()
	config.CreatedAt = now
	config.UpdatedAt = now

	// Calculate next run time
	nextRun, err := CalculateNextRunTime(config.Frequency, config.ScheduleTime, nil)
	if err != nil {
		return fmt.Errorf("failed to calculate next run time: %w", err)
	}
	config.NextRunAt = &nextRun

	// Serialize included fields to JSON
	fieldsJSON, err := json.Marshal(config.IncludedFields)
	if err != nil {
		return fmt.Errorf("failed to marshal included fields: %w", err)
	}

	query := `
		INSERT INTO bucket_inventory_configs
		(id, bucket_name, tenant_id, enabled, frequency, format, destination_bucket,
		 destination_prefix, included_fields, schedule_time, last_run_at, next_run_at,
		 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = m.db.ExecContext(ctx, query,
		config.ID, config.BucketName, nullString(config.TenantID), config.Enabled,
		config.Frequency, config.Format, config.DestinationBucket, config.DestinationPrefix,
		string(fieldsJSON), config.ScheduleTime, config.LastRunAt, config.NextRunAt,
		config.CreatedAt, config.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create inventory config: %w", err)
	}

	m.log.WithFields(logrus.Fields{
		"config_id": config.ID,
		"bucket":    config.BucketName,
	}).Info("Inventory configuration created")

	return nil
}

// GetConfig retrieves an inventory configuration by bucket name
func (m *Manager) GetConfig(ctx context.Context, bucketName, tenantID string) (*InventoryConfig, error) {
	query := `
		SELECT id, bucket_name, tenant_id, enabled, frequency, format, destination_bucket,
		       destination_prefix, included_fields, schedule_time, last_run_at, next_run_at,
		       created_at, updated_at
		FROM bucket_inventory_configs
		WHERE bucket_name = ? AND (tenant_id = ? OR (tenant_id IS NULL AND ? = ''))
	`

	var config InventoryConfig
	var fieldsJSON string
	var tenantIDVal sql.NullString

	err := m.db.QueryRowContext(ctx, query, bucketName, tenantID, tenantID).Scan(
		&config.ID, &config.BucketName, &tenantIDVal, &config.Enabled,
		&config.Frequency, &config.Format, &config.DestinationBucket, &config.DestinationPrefix,
		&fieldsJSON, &config.ScheduleTime, &config.LastRunAt, &config.NextRunAt,
		&config.CreatedAt, &config.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("inventory configuration not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get inventory config: %w", err)
	}

	if tenantIDVal.Valid {
		config.TenantID = tenantIDVal.String
	}

	// Deserialize included fields
	if err := json.Unmarshal([]byte(fieldsJSON), &config.IncludedFields); err != nil {
		return nil, fmt.Errorf("failed to unmarshal included fields: %w", err)
	}

	return &config, nil
}

// UpdateConfig updates an existing inventory configuration
func (m *Manager) UpdateConfig(ctx context.Context, config *InventoryConfig) error {
	config.UpdatedAt = time.Now().Unix()

	// Recalculate next run time if schedule changed
	if config.NextRunAt == nil || config.LastRunAt != nil {
		nextRun, err := CalculateNextRunTime(config.Frequency, config.ScheduleTime, config.LastRunAt)
		if err != nil {
			return fmt.Errorf("failed to calculate next run time: %w", err)
		}
		config.NextRunAt = &nextRun
	}

	// Serialize included fields to JSON
	fieldsJSON, err := json.Marshal(config.IncludedFields)
	if err != nil {
		return fmt.Errorf("failed to marshal included fields: %w", err)
	}

	query := `
		UPDATE bucket_inventory_configs
		SET enabled = ?, frequency = ?, format = ?, destination_bucket = ?,
		    destination_prefix = ?, included_fields = ?, schedule_time = ?,
		    last_run_at = ?, next_run_at = ?, updated_at = ?
		WHERE id = ?
	`

	result, err := m.db.ExecContext(ctx, query,
		config.Enabled, config.Frequency, config.Format, config.DestinationBucket,
		config.DestinationPrefix, string(fieldsJSON), config.ScheduleTime,
		config.LastRunAt, config.NextRunAt, config.UpdatedAt, config.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update inventory config: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("inventory configuration not found")
	}

	m.log.WithFields(logrus.Fields{
		"config_id": config.ID,
		"bucket":    config.BucketName,
	}).Info("Inventory configuration updated")

	return nil
}

// DeleteConfig deletes an inventory configuration
func (m *Manager) DeleteConfig(ctx context.Context, bucketName, tenantID string) error {
	query := `
		DELETE FROM bucket_inventory_configs
		WHERE bucket_name = ? AND (tenant_id = ? OR (tenant_id IS NULL AND ? = ''))
	`

	result, err := m.db.ExecContext(ctx, query, bucketName, tenantID, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete inventory config: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("inventory configuration not found")
	}

	m.log.WithField("bucket", bucketName).Info("Inventory configuration deleted")
	return nil
}

// ListReadyConfigs returns all enabled configurations that are ready to run
func (m *Manager) ListReadyConfigs(ctx context.Context) ([]*InventoryConfig, error) {
	now := time.Now().Unix()

	query := `
		SELECT id, bucket_name, tenant_id, enabled, frequency, format, destination_bucket,
		       destination_prefix, included_fields, schedule_time, last_run_at, next_run_at,
		       created_at, updated_at
		FROM bucket_inventory_configs
		WHERE enabled = 1 AND next_run_at <= ?
	`

	rows, err := m.db.QueryContext(ctx, query, now)
	if err != nil {
		return nil, fmt.Errorf("failed to list ready configs: %w", err)
	}
	defer rows.Close()

	var configs []*InventoryConfig
	for rows.Next() {
		var config InventoryConfig
		var fieldsJSON string
		var tenantIDVal sql.NullString

		err := rows.Scan(
			&config.ID, &config.BucketName, &tenantIDVal, &config.Enabled,
			&config.Frequency, &config.Format, &config.DestinationBucket, &config.DestinationPrefix,
			&fieldsJSON, &config.ScheduleTime, &config.LastRunAt, &config.NextRunAt,
			&config.CreatedAt, &config.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan config: %w", err)
		}

		if tenantIDVal.Valid {
			config.TenantID = tenantIDVal.String
		}

		// Deserialize included fields
		if err := json.Unmarshal([]byte(fieldsJSON), &config.IncludedFields); err != nil {
			m.log.WithError(err).Warn("Failed to unmarshal included fields, using defaults")
			config.IncludedFields = DefaultIncludedFields()
		}

		configs = append(configs, &config)
	}

	return configs, nil
}

// CreateReport creates a new inventory report record
func (m *Manager) CreateReport(ctx context.Context, report *InventoryReport) error {
	if report.ID == "" {
		report.ID = uuid.New().String()
	}

	report.CreatedAt = time.Now().Unix()
	if report.Status == "" {
		report.Status = "pending"
	}

	query := `
		INSERT INTO bucket_inventory_reports
		(id, config_id, bucket_name, report_path, object_count, total_size,
		 status, started_at, completed_at, error_message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := m.db.ExecContext(ctx, query,
		report.ID, report.ConfigID, report.BucketName, report.ReportPath,
		report.ObjectCount, report.TotalSize, report.Status,
		report.StartedAt, report.CompletedAt, report.ErrorMessage, report.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create inventory report: %w", err)
	}

	return nil
}

// UpdateReport updates an inventory report
func (m *Manager) UpdateReport(ctx context.Context, report *InventoryReport) error {
	query := `
		UPDATE bucket_inventory_reports
		SET object_count = ?, total_size = ?, status = ?,
		    started_at = ?, completed_at = ?, error_message = ?
		WHERE id = ?
	`

	result, err := m.db.ExecContext(ctx, query,
		report.ObjectCount, report.TotalSize, report.Status,
		report.StartedAt, report.CompletedAt, report.ErrorMessage, report.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update inventory report: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("inventory report not found")
	}

	return nil
}

// ListReports lists inventory reports for a bucket with pagination
func (m *Manager) ListReports(ctx context.Context, bucketName, tenantID string, limit, offset int) ([]*InventoryReport, error) {
	query := `
		SELECT r.id, r.config_id, r.bucket_name, r.report_path, r.object_count, r.total_size,
		       r.status, r.started_at, r.completed_at, r.error_message, r.created_at
		FROM bucket_inventory_reports r
		INNER JOIN bucket_inventory_configs c ON r.config_id = c.id
		WHERE r.bucket_name = ? AND (c.tenant_id = ? OR (c.tenant_id IS NULL AND ? = ''))
		ORDER BY r.created_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := m.db.QueryContext(ctx, query, bucketName, tenantID, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list reports: %w", err)
	}
	defer rows.Close()

	var reports []*InventoryReport
	for rows.Next() {
		var report InventoryReport
		var errorMsg sql.NullString

		err := rows.Scan(
			&report.ID, &report.ConfigID, &report.BucketName, &report.ReportPath,
			&report.ObjectCount, &report.TotalSize, &report.Status,
			&report.StartedAt, &report.CompletedAt, &errorMsg, &report.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}

		if errorMsg.Valid {
			report.ErrorMessage = &errorMsg.String
		}

		reports = append(reports, &report)
	}

	return reports, nil
}

// nullString returns nil for empty strings, otherwise returns the string
func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
