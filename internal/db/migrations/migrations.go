package migrations

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
)

// Migration represents a single database migration
type Migration struct {
	Version     int
	Description string
	Up          func(*sql.Tx) error
	Down        func(*sql.Tx) error
}

// MigrationManager handles database migrations
type MigrationManager struct {
	db         *sql.DB
	migrations []Migration
	logger     *logrus.Logger
}

// NewMigrationManager creates a new migration manager
func NewMigrationManager(db *sql.DB, logger *logrus.Logger) *MigrationManager {
	if logger == nil {
		logger = logrus.New()
	}

	return &MigrationManager{
		db:         db,
		migrations: getAllMigrations(),
		logger:     logger,
	}
}

// Initialize creates the schema_version table if it doesn't exist
func (m *MigrationManager) Initialize() error {
	_, err := m.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	return nil
}

// GetCurrentVersion returns the current database schema version
func (m *MigrationManager) GetCurrentVersion() (int, error) {
	var version int
	err := m.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("failed to get current version: %w", err)
	}

	return version, nil
}

// GetTargetVersion returns the highest migration version available
func (m *MigrationManager) GetTargetVersion() int {
	if len(m.migrations) == 0 {
		return 0
	}

	maxVersion := 0
	for _, migration := range m.migrations {
		if migration.Version > maxVersion {
			maxVersion = migration.Version
		}
	}

	return maxVersion
}

// Migrate runs all pending migrations to bring the database to the target version
func (m *MigrationManager) Migrate() error {
	if err := m.Initialize(); err != nil {
		return err
	}

	currentVersion, err := m.GetCurrentVersion()
	if err != nil {
		return err
	}

	targetVersion := m.GetTargetVersion()

	if currentVersion == targetVersion {
		m.logger.Infof("Database schema is up to date (version %d)", currentVersion)
		return nil
	}

	if currentVersion > targetVersion {
		return fmt.Errorf("database schema version (%d) is higher than application version (%d). Please update MaxIOFS", currentVersion, targetVersion)
	}

	m.logger.Infof("Starting database migration from version %d to %d", currentVersion, targetVersion)

	// Sort migrations by version
	sort.Slice(m.migrations, func(i, j int) bool {
		return m.migrations[i].Version < m.migrations[j].Version
	})

	// Run pending migrations
	for _, migration := range m.migrations {
		if migration.Version <= currentVersion {
			continue
		}

		if migration.Version > targetVersion {
			break
		}

		if err := m.runMigration(migration); err != nil {
			return fmt.Errorf("migration %d (%s) failed: %w", migration.Version, migration.Description, err)
		}

		m.logger.Infof("✓ Applied migration %d: %s", migration.Version, migration.Description)
	}

	m.logger.Infof("Database migration completed successfully (version %d → %d)", currentVersion, targetVersion)
	return nil
}

// MigrateTo migrates the database to a specific version
func (m *MigrationManager) MigrateTo(targetVersion int) error {
	if err := m.Initialize(); err != nil {
		return err
	}

	currentVersion, err := m.GetCurrentVersion()
	if err != nil {
		return err
	}

	if currentVersion == targetVersion {
		m.logger.Infof("Database schema is already at version %d", currentVersion)
		return nil
	}

	if currentVersion < targetVersion {
		// Migrating up
		m.logger.Infof("Migrating database from version %d to %d", currentVersion, targetVersion)

		sort.Slice(m.migrations, func(i, j int) bool {
			return m.migrations[i].Version < m.migrations[j].Version
		})

		for _, migration := range m.migrations {
			if migration.Version <= currentVersion || migration.Version > targetVersion {
				continue
			}

			if err := m.runMigration(migration); err != nil {
				return fmt.Errorf("migration %d (%s) failed: %w", migration.Version, migration.Description, err)
			}

			m.logger.Infof("✓ Applied migration %d: %s", migration.Version, migration.Description)
		}
	} else {
		// Migrating down
		return fmt.Errorf("downward migrations are not yet supported")
	}

	return nil
}

// runMigration executes a single migration within a transaction
func (m *MigrationManager) runMigration(migration Migration) error {
	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Execute migration
	if err = migration.Up(tx); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	// Record migration
	_, err = tx.Exec(
		"INSERT INTO schema_version (version, description, applied_at) VALUES (?, ?, ?)",
		migration.Version,
		migration.Description,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetMigrationHistory returns the list of applied migrations
func (m *MigrationManager) GetMigrationHistory() ([]MigrationRecord, error) {
	rows, err := m.db.Query(`
		SELECT version, description, applied_at
		FROM schema_version
		ORDER BY version ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query migration history: %w", err)
	}
	defer rows.Close()

	var history []MigrationRecord
	for rows.Next() {
		var record MigrationRecord
		var appliedAt int64

		if err := rows.Scan(&record.Version, &record.Description, &appliedAt); err != nil {
			return nil, fmt.Errorf("failed to scan migration record: %w", err)
		}

		record.AppliedAt = time.Unix(appliedAt, 0)
		history = append(history, record)
	}

	return history, rows.Err()
}

// MigrationRecord represents a migration that has been applied
type MigrationRecord struct {
	Version     int
	Description string
	AppliedAt   time.Time
}
