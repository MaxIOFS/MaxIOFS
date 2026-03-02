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

const (
	// writeQueueSize is the number of events that can be buffered before LogEvent blocks.
	writeQueueSize = 4096
	// batchSize is the maximum number of events written per SQLite transaction.
	batchSize = 128
	// batchTimeout is how long the worker waits for more events before flushing.
	batchTimeout = 100 * time.Millisecond
)

// pendingWrite holds a pre-serialized audit event ready to be inserted.
type pendingWrite struct {
	timestamp    int64
	tenantID     string
	userID       string
	username     string
	eventType    string
	resourceType string
	resourceID   string
	resourceName string
	action       string
	status       string
	ipAddress    string
	userAgent    string
	detailsJSON  string
}

// SQLiteStore implements the Store interface using SQLite.
// All writes are serialized through a single worker goroutine to avoid SQLITE_BUSY.
type SQLiteStore struct {
	db        *sql.DB
	logger    *logrus.Logger
	writeChan chan *pendingWrite
	flushChan chan chan struct{} // flush barrier requests
	done      chan struct{}
}

// NewSQLiteStore creates a new SQLite-based audit log store
func NewSQLiteStore(dbPath string, logger *logrus.Logger) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit database: %w", err)
	}

	// A single connection is all we need — the worker goroutine is the only writer.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &SQLiteStore{
		db:        db,
		logger:    logger,
		writeChan: make(chan *pendingWrite, writeQueueSize),
		flushChan: make(chan chan struct{}, 8),
		done:      make(chan struct{}),
	}

	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize audit schema: %w", err)
	}

	// Start the single writer goroutine.
	go store.writeWorker()

	logger.Info("Audit log SQLite store initialized successfully")
	return store, nil
}

// initSchema creates the audit_logs table and indexes if they don't exist
func (s *SQLiteStore) initSchema() error {
	if _, err := s.db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		return fmt.Errorf("failed to enable WAL mode: %w", err)
	}

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

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create audit schema: %w", err)
	}

	return nil
}

// writeWorker is the only goroutine that writes to SQLite.
// It drains writeChan in batches for efficiency.
func (s *SQLiteStore) writeWorker() {
	defer close(s.done)

	batch := make([]*pendingWrite, 0, batchSize)
	timer := time.NewTimer(batchTimeout)
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.insertBatch(batch); err != nil {
			s.logger.WithError(err).Error("Failed to write audit log batch")
		}
		batch = batch[:0]
	}

	for {
		select {
		case w, ok := <-s.writeChan:
			if !ok {
				// Channel closed — flush remaining and exit.
				flush()
				return
			}
			batch = append(batch, w)
			if len(batch) >= batchSize {
				flush()
				// Reset the timer so we don't fire immediately again.
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(batchTimeout)
			}

		case reply := <-s.flushChan:
			// Drain any writes that are already sitting in writeChan
			// so the caller sees a consistent view of all events queued
			// before the Flush() call.
			for len(s.writeChan) > 0 {
				if w, ok := <-s.writeChan; ok {
					batch = append(batch, w)
				}
			}
			flush()
			close(reply)

		case <-timer.C:
			flush()
			timer.Reset(batchTimeout)
		}
	}
}

// Flush blocks until all events queued before this call have been committed to SQLite.
// Useful in tests and for graceful shutdown scenarios.
func (s *SQLiteStore) Flush() {
	reply := make(chan struct{})
	s.flushChan <- reply
	<-reply
}

// insertBatch inserts a slice of events in a single transaction.
func (s *SQLiteStore) insertBatch(batch []*pendingWrite) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.Prepare(`
		INSERT INTO audit_logs (
			timestamp, tenant_id, user_id, username, event_type,
			resource_type, resource_id, resource_name, action, status,
			ip_address, user_agent, details, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, w := range batch {
		if _, err := stmt.Exec(
			w.timestamp, w.tenantID, w.userID, w.username, w.eventType,
			w.resourceType, w.resourceID, w.resourceName, w.action, w.status,
			w.ipAddress, w.userAgent, w.detailsJSON, w.timestamp,
		); err != nil {
			return fmt.Errorf("failed to insert audit log row: %w", err)
		}
	}

	return tx.Commit()
}

// LogEvent records an audit event. The write is queued asynchronously so it
// never blocks the caller and never contends with other writers on SQLite.
func (s *SQLiteStore) LogEvent(ctx context.Context, event *AuditEvent) error {
	var detailsJSON string
	if len(event.Details) > 0 {
		b, err := json.Marshal(event.Details)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to marshal audit event details to JSON")
			detailsJSON = "{}"
		} else {
			detailsJSON = string(b)
		}
	} else {
		detailsJSON = "{}"
	}

	w := &pendingWrite{
		timestamp:    time.Now().Unix(),
		tenantID:     event.TenantID,
		userID:       event.UserID,
		username:     event.Username,
		eventType:    event.EventType,
		resourceType: event.ResourceType,
		resourceID:   event.ResourceID,
		resourceName: event.ResourceName,
		action:       event.Action,
		status:       event.Status,
		ipAddress:    event.IPAddress,
		userAgent:    event.UserAgent,
		detailsJSON:  detailsJSON,
	}

	select {
	case s.writeChan <- w:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetLogs retrieves audit logs with filters (for global admin)
func (s *SQLiteStore) GetLogs(ctx context.Context, filters *AuditLogFilters) ([]*AuditLog, int, error) {
	whereClause, args := s.buildWhereClause(filters)

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_logs %s", whereClause)
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

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
		&log.ID, &log.Timestamp, &tenantID, &log.UserID, &log.Username,
		&log.EventType, &resourceType, &resourceID, &resourceName,
		&log.Action, &log.Status, &ipAddress, &userAgent, &detailsJSON, &log.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("audit log not found: %d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get audit log: %w", err)
	}

	log.TenantID = tenantID.String
	log.ResourceType = resourceType.String
	log.ResourceID = resourceID.String
	log.ResourceName = resourceName.String
	log.IPAddress = ipAddress.String
	log.UserAgent = userAgent.String

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

	result, err := s.db.ExecContext(ctx, "DELETE FROM audit_logs WHERE timestamp < ?", cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to purge old audit logs: %w", err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get deleted rows count: %w", err)
	}

	return int(deleted), nil
}

// Close flushes pending writes and closes the database connection.
func (s *SQLiteStore) Close() error {
	// Closing the channel signals the worker to flush and exit.
	close(s.writeChan)
	// Wait for the worker to finish flushing.
	<-s.done
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
			&log.ID, &log.Timestamp, &tenantID, &log.UserID, &log.Username,
			&log.EventType, &resourceType, &resourceID, &resourceName,
			&log.Action, &log.Status, &ipAddress, &userAgent, &detailsJSON, &log.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}

		log.TenantID = tenantID.String
		log.ResourceType = resourceType.String
		log.ResourceID = resourceID.String
		log.ResourceName = resourceName.String
		log.IPAddress = ipAddress.String
		log.UserAgent = userAgent.String

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
