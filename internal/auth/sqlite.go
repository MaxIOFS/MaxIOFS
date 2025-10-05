package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

// SQLiteStore implements authentication storage using SQLite
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite-based auth store
func NewSQLiteStore(dataDir string) (*SQLiteStore, error) {
	// Ensure auth directory exists
	authDir := filepath.Join(dataDir, "auth")
	if err := ensureDir(authDir); err != nil {
		return nil, fmt.Errorf("failed to create auth directory: %w", err)
	}

	dbPath := filepath.Join(authDir, "auth.db")

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &SQLiteStore{db: db}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	logrus.WithField("db_path", dbPath).Info("SQLite auth store initialized")
	return store, nil
}

// initSchema creates the database schema
func (s *SQLiteStore) initSchema() error {
	schema := `
	-- Tenants table
	CREATE TABLE IF NOT EXISTS tenants (
		id TEXT PRIMARY KEY,
		name TEXT UNIQUE NOT NULL,
		display_name TEXT,
		description TEXT,
		status TEXT DEFAULT 'active',
		max_access_keys INTEGER DEFAULT 10,
		max_storage_bytes INTEGER DEFAULT 107374182400,
		current_storage_bytes INTEGER DEFAULT 0,
		max_buckets INTEGER DEFAULT 100,
		current_buckets INTEGER DEFAULT 0,
		metadata TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);

	-- Users table (console users)
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		display_name TEXT,
		email TEXT,
		status TEXT DEFAULT 'active',
		tenant_id TEXT,
		roles TEXT,
		policies TEXT,
		metadata TEXT,
		failed_login_attempts INTEGER DEFAULT 0,
		locked_until INTEGER DEFAULT 0,
		last_failed_login INTEGER DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE SET NULL
	);

	-- Access keys table (S3 API keys)
	CREATE TABLE IF NOT EXISTS access_keys (
		access_key_id TEXT PRIMARY KEY,
		secret_access_key TEXT NOT NULL,
		user_id TEXT NOT NULL,
		status TEXT DEFAULT 'active',
		created_at INTEGER NOT NULL,
		last_used INTEGER,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	-- Bucket permissions table
	CREATE TABLE IF NOT EXISTS bucket_permissions (
		id TEXT PRIMARY KEY,
		bucket_name TEXT NOT NULL,
		user_id TEXT,
		tenant_id TEXT,
		permission_level TEXT NOT NULL,
		granted_by TEXT NOT NULL,
		granted_at INTEGER NOT NULL,
		expires_at INTEGER,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
		FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
		UNIQUE(bucket_name, user_id),
		UNIQUE(bucket_name, tenant_id)
	);

	-- Indexes for performance
	CREATE INDEX IF NOT EXISTS idx_tenants_name ON tenants(name);
	CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
	CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);
	CREATE INDEX IF NOT EXISTS idx_users_tenant_id ON users(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_access_keys_user_id ON access_keys(user_id);
	CREATE INDEX IF NOT EXISTS idx_access_keys_status ON access_keys(status);
	CREATE INDEX IF NOT EXISTS idx_bucket_permissions_bucket ON bucket_permissions(bucket_name);
	CREATE INDEX IF NOT EXISTS idx_bucket_permissions_user ON bucket_permissions(user_id);
	CREATE INDEX IF NOT EXISTS idx_bucket_permissions_tenant ON bucket_permissions(tenant_id);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// Run migrations for existing databases
	return s.runMigrations()
}

// runMigrations applies database migrations for existing tables
func (s *SQLiteStore) runMigrations() error {
	// Migration 1: Add account lockout columns to users table
	migrations := []string{
		`ALTER TABLE users ADD COLUMN failed_login_attempts INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN locked_until INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN last_failed_login INTEGER DEFAULT 0`,
	}

	for _, migration := range migrations {
		// Try to run migration - ignore errors if column already exists
		_, err := s.db.Exec(migration)
		if err != nil && !isDuplicateColumnError(err) {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}

// isDuplicateColumnError checks if error is due to duplicate column
func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return contains(errMsg, "duplicate column") || contains(errMsg, "already exists")
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// VerifyPassword verifies a password against a bcrypt hash
func VerifyPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// CreateUser creates a new user
func (s *SQLiteStore) CreateUser(user *User) error {
	// Hash password with bcrypt
	hashedPassword, err := HashPassword(user.Password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Serialize JSON fields
	rolesJSON, _ := json.Marshal(user.Roles)
	policiesJSON, _ := json.Marshal(user.Policies)
	metadataJSON, _ := json.Marshal(user.Metadata)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO users (id, username, password_hash, display_name, email, status, tenant_id, roles, policies, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, user.ID, user.Username, hashedPassword, user.DisplayName, user.Email, user.Status,
		nullString(user.TenantID), string(rolesJSON), string(policiesJSON), string(metadataJSON), user.CreatedAt, user.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return tx.Commit()
}

// nullString returns sql.NullString for optional string fields
func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// GetUserByUsername retrieves a user by username
func (s *SQLiteStore) GetUserByUsername(username string) (*User, error) {
	var user User
	var rolesJSON, policiesJSON, metadataJSON string
	var tenantID sql.NullString

	err := s.db.QueryRow(`
		SELECT id, username, password_hash, display_name, email, status, tenant_id, roles, policies, metadata, created_at, updated_at
		FROM users
		WHERE username = ? AND status != 'deleted'
	`, username).Scan(
		&user.ID, &user.Username, &user.Password, &user.DisplayName, &user.Email, &user.Status,
		&tenantID, &rolesJSON, &policiesJSON, &metadataJSON, &user.CreatedAt, &user.UpdatedAt,
	)

	if tenantID.Valid {
		user.TenantID = tenantID.String
	}

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	// Deserialize JSON fields
	json.Unmarshal([]byte(rolesJSON), &user.Roles)
	json.Unmarshal([]byte(policiesJSON), &user.Policies)
	json.Unmarshal([]byte(metadataJSON), &user.Metadata)

	return &user, nil
}

// GetUserByID retrieves a user by ID
func (s *SQLiteStore) GetUserByID(userID string) (*User, error) {
	var user User
	var rolesJSON, policiesJSON, metadataJSON string
	var tenantID sql.NullString

	err := s.db.QueryRow(`
		SELECT id, username, password_hash, display_name, email, status, tenant_id, roles, policies, metadata, created_at, updated_at
		FROM users
		WHERE id = ? AND status != 'deleted'
	`, userID).Scan(
		&user.ID, &user.Username, &user.Password, &user.DisplayName, &user.Email, &user.Status,
		&tenantID, &rolesJSON, &policiesJSON, &metadataJSON, &user.CreatedAt, &user.UpdatedAt,
	)

	if tenantID.Valid {
		user.TenantID = tenantID.String
	}

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	// Deserialize JSON fields
	json.Unmarshal([]byte(rolesJSON), &user.Roles)
	json.Unmarshal([]byte(policiesJSON), &user.Policies)
	json.Unmarshal([]byte(metadataJSON), &user.Metadata)

	return &user, nil
}

// UpdateUser updates an existing user
func (s *SQLiteStore) UpdateUser(user *User) error {
	// Serialize JSON fields
	rolesJSON, _ := json.Marshal(user.Roles)
	policiesJSON, _ := json.Marshal(user.Policies)
	metadataJSON, _ := json.Marshal(user.Metadata)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE users
		SET display_name = ?, email = ?, status = ?, tenant_id = ?, roles = ?, policies = ?, metadata = ?, password_hash = ?, updated_at = ?
		WHERE id = ?
	`, user.DisplayName, user.Email, user.Status, nullString(user.TenantID), string(rolesJSON), string(policiesJSON), string(metadataJSON), user.Password, user.UpdatedAt, user.ID)

	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return tx.Commit()
}

// UpdateUserPassword updates only the password hash for a user
func (s *SQLiteStore) UpdateUserPassword(userID, passwordHash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE users
		SET password_hash = ?, updated_at = ?
		WHERE id = ?
	`, passwordHash, time.Now().Unix(), userID)

	if err != nil {
		return fmt.Errorf("failed to update user password: %w", err)
	}

	return tx.Commit()
}

// DeleteUser soft deletes a user (sets status to 'deleted')
func (s *SQLiteStore) DeleteUser(userID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Soft delete user
	_, err = tx.Exec(`UPDATE users SET status = 'deleted', updated_at = ? WHERE id = ?`, time.Now().Unix(), userID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	// Soft delete all associated access keys
	_, err = tx.Exec(`UPDATE access_keys SET status = 'deleted' WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user access keys: %w", err)
	}

	return tx.Commit()
}

// ListUsers returns all active users
func (s *SQLiteStore) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`
		SELECT id, username, password_hash, display_name, email, status, tenant_id, roles, policies, metadata, created_at, updated_at
		FROM users
		WHERE status != 'deleted'
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var user User
		var rolesJSON, policiesJSON, metadataJSON string
		var tenantID sql.NullString

		err := rows.Scan(
			&user.ID, &user.Username, &user.Password, &user.DisplayName, &user.Email, &user.Status,
			&tenantID, &rolesJSON, &policiesJSON, &metadataJSON, &user.CreatedAt, &user.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if tenantID.Valid {
			user.TenantID = tenantID.String
		}

		// Deserialize JSON fields
		json.Unmarshal([]byte(rolesJSON), &user.Roles)
		json.Unmarshal([]byte(policiesJSON), &user.Policies)
		json.Unmarshal([]byte(metadataJSON), &user.Metadata)

		users = append(users, &user)
	}

	return users, nil
}

// CreateAccessKey creates a new access key
func (s *SQLiteStore) CreateAccessKey(key *AccessKey) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO access_keys (access_key_id, secret_access_key, user_id, status, created_at, last_used)
		VALUES (?, ?, ?, ?, ?, ?)
	`, key.AccessKeyID, key.SecretAccessKey, key.UserID, key.Status, key.CreatedAt, key.LastUsed)

	if err != nil {
		return fmt.Errorf("failed to create access key: %w", err)
	}

	return tx.Commit()
}

// GetAccessKey retrieves an access key by ID
func (s *SQLiteStore) GetAccessKey(accessKeyID string) (*AccessKey, error) {
	var key AccessKey
	var lastUsed sql.NullInt64

	err := s.db.QueryRow(`
		SELECT access_key_id, secret_access_key, user_id, status, created_at, last_used
		FROM access_keys
		WHERE access_key_id = ? AND status != 'deleted'
	`, accessKeyID).Scan(
		&key.AccessKeyID, &key.SecretAccessKey, &key.UserID, &key.Status, &key.CreatedAt, &lastUsed,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("access key not found")
	}
	if err != nil {
		return nil, err
	}

	if lastUsed.Valid {
		key.LastUsed = lastUsed.Int64
	}

	return &key, nil
}

// UpdateAccessKeyLastUsed updates the last used timestamp
func (s *SQLiteStore) UpdateAccessKeyLastUsed(accessKeyID string, timestamp int64) error {
	_, err := s.db.Exec(`
		UPDATE access_keys
		SET last_used = ?
		WHERE access_key_id = ?
	`, timestamp, accessKeyID)

	return err
}

// DeleteAccessKey soft deletes an access key (sets status to 'deleted')
func (s *SQLiteStore) DeleteAccessKey(accessKeyID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`UPDATE access_keys SET status = 'deleted' WHERE access_key_id = ?`, accessKeyID)
	if err != nil {
		return fmt.Errorf("failed to delete access key: %w", err)
	}

	return tx.Commit()
}

// ListAccessKeysByUser returns all active access keys for a user
func (s *SQLiteStore) ListAccessKeysByUser(userID string) ([]*AccessKey, error) {
	rows, err := s.db.Query(`
		SELECT access_key_id, secret_access_key, user_id, status, created_at, last_used
		FROM access_keys
		WHERE user_id = ? AND status != 'deleted'
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*AccessKey
	for rows.Next() {
		var key AccessKey
		var lastUsed sql.NullInt64

		err := rows.Scan(
			&key.AccessKeyID, &key.SecretAccessKey, &key.UserID, &key.Status, &key.CreatedAt, &lastUsed,
		)
		if err != nil {
			return nil, err
		}

		if lastUsed.Valid {
			key.LastUsed = lastUsed.Int64
		}

		keys = append(keys, &key)
	}

	return keys, nil
}

// ListAllAccessKeys returns all active access keys
func (s *SQLiteStore) ListAllAccessKeys() ([]*AccessKey, error) {
	rows, err := s.db.Query(`
		SELECT access_key_id, secret_access_key, user_id, status, created_at, last_used
		FROM access_keys
		WHERE status != 'deleted'
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*AccessKey
	for rows.Next() {
		var key AccessKey
		var lastUsed sql.NullInt64

		err := rows.Scan(
			&key.AccessKeyID, &key.SecretAccessKey, &key.UserID, &key.Status, &key.CreatedAt, &lastUsed,
		)
		if err != nil {
			return nil, err
		}

		if lastUsed.Valid {
			key.LastUsed = lastUsed.Int64
		}

		keys = append(keys, &key)
	}

	return keys, nil
}

// IncrementFailedLoginAttempts increments failed login attempts for a user
func (s *SQLiteStore) IncrementFailedLoginAttempts(userID string) error {
	_, err := s.db.Exec(`
		UPDATE users
		SET failed_login_attempts = failed_login_attempts + 1,
		    last_failed_login = ?
		WHERE id = ?
	`, time.Now().Unix(), userID)
	return err
}

// LockAccount locks a user account for specified duration (in seconds)
func (s *SQLiteStore) LockAccount(userID string, durationSeconds int64) error {
	lockUntil := time.Now().Add(time.Duration(durationSeconds) * time.Second).Unix()
	_, err := s.db.Exec(`
		UPDATE users
		SET locked_until = ?,
		    last_failed_login = ?
		WHERE id = ?
	`, lockUntil, time.Now().Unix(), userID)
	return err
}

// UnlockAccount unlocks a user account and resets failed login attempts
func (s *SQLiteStore) UnlockAccount(userID string) error {
	_, err := s.db.Exec(`
		UPDATE users
		SET failed_login_attempts = 0,
		    locked_until = 0
		WHERE id = ?
	`, userID)
	return err
}

// ResetFailedLoginAttempts resets failed login attempts to 0
func (s *SQLiteStore) ResetFailedLoginAttempts(userID string) error {
	_, err := s.db.Exec(`
		UPDATE users
		SET failed_login_attempts = 0
		WHERE id = ?
	`, userID)
	return err
}

// GetAccountLockStatus retrieves account lock information
func (s *SQLiteStore) GetAccountLockStatus(userID string) (failedAttempts int, lockedUntil int64, err error) {
	err = s.db.QueryRow(`
		SELECT failed_login_attempts, locked_until
		FROM users
		WHERE id = ?
	`, userID).Scan(&failedAttempts, &lockedUntil)
	return
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// ensureDir creates a directory if it doesn't exist
func ensureDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		logrus.WithField("dir", dir).Debug("Created directory")
	}
	return nil
}
