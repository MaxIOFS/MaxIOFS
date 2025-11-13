package audit

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

// Manager handles audit logging operations
type Manager struct {
	store  Store
	logger *logrus.Logger
}

// NewManager creates a new audit manager
func NewManager(store Store, logger *logrus.Logger) *Manager {
	return &Manager{
		store:  store,
		logger: logger,
	}
}

// LogEvent records an audit event
// This is the main entry point for logging audit events from across the application
func (m *Manager) LogEvent(ctx context.Context, event *AuditEvent) error {
	if event == nil {
		m.logger.Warn("Attempted to log nil audit event")
		return nil
	}

	// Validate required fields
	if event.UserID == "" {
		m.logger.Warn("Audit event missing required UserID field")
		return nil
	}

	if event.EventType == "" {
		m.logger.Warn("Audit event missing required EventType field")
		return nil
	}

	if event.Action == "" {
		m.logger.Warn("Audit event missing required Action field")
		return nil
	}

	if event.Status == "" {
		m.logger.Warn("Audit event missing required Status field")
		return nil
	}

	// Log the event
	err := m.store.LogEvent(ctx, event)
	if err != nil {
		m.logger.WithError(err).WithFields(logrus.Fields{
			"event_type": event.EventType,
			"user_id":    event.UserID,
			"action":     event.Action,
			"status":     event.Status,
		}).Error("Failed to log audit event")
		return err
	}

	// Debug log for successful audit logging
	m.logger.WithFields(logrus.Fields{
		"event_type":    event.EventType,
		"user_id":       event.UserID,
		"username":      event.Username,
		"action":        event.Action,
		"status":        event.Status,
		"resource_type": event.ResourceType,
		"resource_id":   event.ResourceID,
	}).Debug("Audit event logged successfully")

	return nil
}

// GetLogs retrieves audit logs with filters (for global admin)
func (m *Manager) GetLogs(ctx context.Context, filters *AuditLogFilters) ([]*AuditLog, int, error) {
	if filters == nil {
		filters = &AuditLogFilters{}
	}

	// Set default pagination if not provided
	if filters.Page <= 0 {
		filters.Page = 1
	}
	if filters.PageSize <= 0 {
		filters.PageSize = 50 // Default page size
	}
	if filters.PageSize > 100 {
		filters.PageSize = 100 // Max page size
	}

	logs, total, err := m.store.GetLogs(ctx, filters)
	if err != nil {
		m.logger.WithError(err).Error("Failed to retrieve audit logs")
		return nil, 0, err
	}

	m.logger.WithFields(logrus.Fields{
		"total_logs": total,
		"page":       filters.Page,
		"page_size":  filters.PageSize,
	}).Debug("Retrieved audit logs")

	return logs, total, nil
}

// GetLogsByTenant retrieves logs for a specific tenant (for tenant admin)
func (m *Manager) GetLogsByTenant(ctx context.Context, tenantID string, filters *AuditLogFilters) ([]*AuditLog, int, error) {
	if tenantID == "" {
		m.logger.Warn("Attempted to get tenant logs with empty tenant ID")
		return nil, 0, nil
	}

	if filters == nil {
		filters = &AuditLogFilters{}
	}

	// Set default pagination if not provided
	if filters.Page <= 0 {
		filters.Page = 1
	}
	if filters.PageSize <= 0 {
		filters.PageSize = 50 // Default page size
	}
	if filters.PageSize > 100 {
		filters.PageSize = 100 // Max page size
	}

	logs, total, err := m.store.GetLogsByTenant(ctx, tenantID, filters)
	if err != nil {
		m.logger.WithError(err).WithField("tenant_id", tenantID).Error("Failed to retrieve tenant audit logs")
		return nil, 0, err
	}

	m.logger.WithFields(logrus.Fields{
		"tenant_id":  tenantID,
		"total_logs": total,
		"page":       filters.Page,
		"page_size":  filters.PageSize,
	}).Debug("Retrieved tenant audit logs")

	return logs, total, nil
}

// GetLogByID retrieves a single log entry
func (m *Manager) GetLogByID(ctx context.Context, id int64) (*AuditLog, error) {
	log, err := m.store.GetLogByID(ctx, id)
	if err != nil {
		m.logger.WithError(err).WithField("log_id", id).Error("Failed to retrieve audit log by ID")
		return nil, err
	}

	return log, nil
}

// PurgeLogs deletes logs older than specified days (maintenance)
func (m *Manager) PurgeLogs(ctx context.Context, olderThanDays int) (int, error) {
	if olderThanDays <= 0 {
		m.logger.Warn("Invalid retention days for purge operation")
		return 0, nil
	}

	count, err := m.store.PurgeLogs(ctx, olderThanDays)
	if err != nil {
		m.logger.WithError(err).WithField("retention_days", olderThanDays).Error("Failed to purge old audit logs")
		return 0, err
	}

	m.logger.WithFields(logrus.Fields{
		"deleted_count":  count,
		"retention_days": olderThanDays,
	}).Info("Successfully purged old audit logs")

	return count, nil
}

// StartRetentionJob starts a background job to automatically purge old logs
// This should be called once on server startup
func (m *Manager) StartRetentionJob(ctx context.Context, retentionDays int) {
	if retentionDays <= 0 {
		m.logger.Info("Audit log retention disabled (retention_days <= 0)")
		return
	}

	m.logger.WithField("retention_days", retentionDays).Info("Starting audit log retention job")

	go func() {
		ticker := time.NewTicker(24 * time.Hour) // Run once per day
		defer ticker.Stop()

		// Run immediately on startup
		m.runRetentionCleanup(ctx, retentionDays)

		for {
			select {
			case <-ctx.Done():
				m.logger.Info("Stopping audit log retention job")
				return
			case <-ticker.C:
				m.runRetentionCleanup(ctx, retentionDays)
			}
		}
	}()
}

// runRetentionCleanup performs the actual cleanup operation
func (m *Manager) runRetentionCleanup(ctx context.Context, retentionDays int) {
	m.logger.WithField("retention_days", retentionDays).Debug("Running audit log retention cleanup")

	count, err := m.PurgeLogs(ctx, retentionDays)
	if err != nil {
		m.logger.WithError(err).Error("Audit log retention cleanup failed")
		return
	}

	if count > 0 {
		m.logger.WithFields(logrus.Fields{
			"deleted_count":  count,
			"retention_days": retentionDays,
		}).Info("Audit log retention cleanup completed")
	}
}

// Close closes the audit manager and underlying store
func (m *Manager) Close() error {
	if m.store != nil {
		return m.store.Close()
	}
	return nil
}
