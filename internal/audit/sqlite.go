package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// SQLiteStore implements the Store interface using SQLite
type SQLiteStore struct {
	db     *sql.DB
	logger *logrus.Logger
}

// NewSQLiteStore creates a new SQLite-based audit log store
func NewSQLiteStore(dbPath string, logger *logrus.Logger) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	store := &SQLiteStore{
		db:     db,
		logger: logger,
	}

	// Initialize database schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize audit schema: %w", err)
	}

	logger.Info("Audit log SQLite store initialized successfully")
	return store, nil
}

// initSchema creates the audit_logs table and indexes if they don't exist
func (s *SQLiteStore) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		tenant_id TEXT,
		user_id TEXT NOT NULL,
		username TEXT NOT NULL,
		event_type TEXT NOT NULL,
		resource_type TEXT,
		resource_id TEXT,
		resource_name TEXT,
		action TEXT NOT NULL,
		status TEXT NOT NULL,
		ip_address TEXT,
		user_agent TEXT,
		details TEXT,
		created_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_audit_logs_timestamp ON audit_logs(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_id ON audit_logs(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_event_type ON audit_logs(event_type);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_status ON audit_logs(status);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_resource_type ON audit_logs(resource_type);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create audit schema: %w", err)
	}

	return nil
}

// LogEvent records an audit event
func (s *SQLiteStore) LogEvent(ctx context.Context, event *AuditEvent) error {
	now := time.Now().Unix()

	// Convert details map to JSON
	var detailsJSON string
	if event.Details != nil && len(event.Details) > 0 {
		detailsBytes, err := json.Marshal(event.Details)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to marshal audit event details to JSON")
			detailsJSON = "{}"
		} else {
			detailsJSON = string(detailsBytes)
		}
	} else {
		detailsJSON = "{}"
	}

	query := `
		INSERT INTO audit_logs (
			timestamp, tenant_id, user_id, username, event_type,
			resource_type, resource_id, resource_name, action, status,
			ip_address, user_agent, details, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		now,
		event.TenantID,
		event.UserID,
		event.Username,
		event.EventType,
		event.ResourceType,
		event.ResourceID,
		event.ResourceName,
		event.Action,
		event.Status,
		event.IPAddress,
		event.UserAgent,
		detailsJSON,
		now,
	)

	if err != nil {
		return fmt.Errorf("failed to insert audit log: %w", err)
	}

	return nil
}

// GetLogs retrieves audit logs with filters (for global admin)
func (s *SQLiteStore) GetLogs(ctx context.Context, filters *AuditLogFilters) ([]*AuditLog, int, error) {
	// Build WHERE clause
	whereClause, args := s.buildWhereClause(filters)

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_logs %s", whereClause)
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

	// Get paginated results
	offset := (filters.Page - 1) * filters.PageSize
	query := fmt.Sprintf(`
		SELECT id, timestamp, tenant_id, user_id, username, event_type,
		       resource_type, resource_id, resource_name, action, status,
		       ip_address, user_agent, details, created_at
		FROM audit_logs %s
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, filters.PageSize, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	logs, err := s.scanLogs(rows)
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetLogsByTenant retrieves logs for a specific tenant (for tenant admin)
func (s *SQLiteStore) GetLogsByTenant(ctx context.Context, tenantID string, filters *AuditLogFilters) ([]*AuditLog, int, error) {
	// Override tenant_id in filters to ensure isolation
	filters.TenantID = tenantID

	return s.GetLogs(ctx, filters)
}

// GetLogByID retrieves a single log entry
func (s *SQLiteStore) GetLogByID(ctx context.Context, id int64) (*AuditLog, error) {
	query := `
		SELECT id, timestamp, tenant_id, user_id, username, event_type,
		       resource_type, resource_id, resource_name, action, status,
		       ip_address, user_agent, details, created_at
		FROM audit_logs
		WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, id)

	log := &AuditLog{}
	var tenantID, resourceType, resourceID, resourceName, ipAddress, userAgent, detailsJSON sql.NullString

	err := row.Scan(
		&log.ID,
		&log.Timestamp,
		&tenantID,
		&log.UserID,
		&log.Username,
		&log.EventType,
		&resourceType,
		&resourceID,
		&resourceName,
		&log.Action,
		&log.Status,
		&ipAddress,
		&userAgent,
		&detailsJSON,
		&log.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("audit log not found: %d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get audit log: %w", err)
	}

	// Handle nullable fields
	log.TenantID = tenantID.String
	log.ResourceType = resourceType.String
	log.ResourceID = resourceID.String
	log.ResourceName = resourceName.String
	log.IPAddress = ipAddress.String
	log.UserAgent = userAgent.String

	// Parse details JSON
	if detailsJSON.Valid && detailsJSON.String != "" && detailsJSON.String != "{}" {
		var details map[string]interface{}
		if err := json.Unmarshal([]byte(detailsJSON.String), &details); err != nil {
			s.logger.WithError(err).Warn("Failed to unmarshal audit log details")
			log.Details = make(map[string]interface{})
		} else {
			log.Details = details
		}
	} else {
		log.Details = make(map[string]interface{})
	}

	return log, nil
}

// PurgeLogs deletes logs older than specified days (maintenance)
func (s *SQLiteStore) PurgeLogs(ctx context.Context, olderThanDays int) (int, error) {
	cutoffTime := time.Now().AddDate(0, 0, -olderThanDays).Unix()

	query := "DELETE FROM audit_logs WHERE timestamp < ?"
	result, err := s.db.ExecContext(ctx, query, cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to purge old audit logs: %w", err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get deleted rows count: %w", err)
	}

	return int(deleted), nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// buildWhereClause builds the WHERE clause and arguments for filtering
func (s *SQLiteStore) buildWhereClause(filters *AuditLogFilters) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if filters.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, filters.TenantID)
	}

	if filters.UserID != "" {
		conditions = append(conditions, "user_id = ?")
		args = append(args, filters.UserID)
	}

	if filters.EventType != "" {
		conditions = append(conditions, "event_type = ?")
		args = append(args, filters.EventType)
	}

	if filters.ResourceType != "" {
		conditions = append(conditions, "resource_type = ?")
		args = append(args, filters.ResourceType)
	}

	if filters.Action != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, filters.Action)
	}

	if filters.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filters.Status)
	}

	if filters.StartDate > 0 {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, filters.StartDate)
	}

	if filters.EndDate > 0 {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, filters.EndDate)
	}

	if len(conditions) == 0 {
		return "", args
	}

	whereClause := "WHERE " + conditions[0]
	for i := 1; i < len(conditions); i++ {
		whereClause += " AND " + conditions[i]
	}

	return whereClause, args
}

// scanLogs scans multiple rows into AuditLog structs
func (s *SQLiteStore) scanLogs(rows *sql.Rows) ([]*AuditLog, error) {
	var logs []*AuditLog

	for rows.Next() {
		log := &AuditLog{}
		var tenantID, resourceType, resourceID, resourceName, ipAddress, userAgent, detailsJSON sql.NullString

		err := rows.Scan(
			&log.ID,
			&log.Timestamp,
			&tenantID,
			&log.UserID,
			&log.Username,
			&log.EventType,
			&resourceType,
			&resourceID,
			&resourceName,
			&log.Action,
			&log.Status,
			&ipAddress,
			&userAgent,
			&detailsJSON,
			&log.CreatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}

		// Handle nullable fields
		log.TenantID = tenantID.String
		log.ResourceType = resourceType.String
		log.ResourceID = resourceID.String
		log.ResourceName = resourceName.String
		log.IPAddress = ipAddress.String
		log.UserAgent = userAgent.String

		// Parse details JSON
		if detailsJSON.Valid && detailsJSON.String != "" && detailsJSON.String != "{}" {
			var details map[string]interface{}
			if err := json.Unmarshal([]byte(detailsJSON.String), &details); err != nil {
				s.logger.WithError(err).Warn("Failed to unmarshal audit log details")
				log.Details = make(map[string]interface{})
			} else {
				log.Details = details
			}
		} else {
			log.Details = make(map[string]interface{})
		}

		logs = append(logs, log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating audit logs: %w", err)
	}

	return logs, nil
}
