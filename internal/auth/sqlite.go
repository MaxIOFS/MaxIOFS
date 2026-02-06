package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/maxiofs/maxiofs/internal/db/migrations"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// SQLiteStore implements authentication storage using SQLite
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite-based auth store
func NewSQLiteStore(dataDir string) (*SQLiteStore, error) {
	// Use unified DB path
	dbPath := filepath.Join(dataDir, "db", "maxiofs.db")
	if err := ensureDir(filepath.Dir(dbPath)); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &SQLiteStore{db: db}

	// Run database migrations
	migrationManager := migrations.NewMigrationManager(db, logrus.StandardLogger())
	if err := migrationManager.Migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	logrus.WithField("db_path", dbPath).Info("SQLite auth store initialized")
	return store, nil
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
	var twoFactorSecret sql.NullString
	var twoFactorSetupAt sql.NullInt64
	var backupCodesJSON sql.NullString
	var backupCodesUsedJSON sql.NullString
	var themePreference sql.NullString
	var languagePreference sql.NullString

	err := s.db.QueryRow(`
		SELECT id, username, password_hash, display_name, email, status, tenant_id, roles, policies, metadata, created_at, updated_at,
		       two_factor_enabled, two_factor_secret, two_factor_setup_at, backup_codes, backup_codes_used,
		       theme_preference, language_preference
		FROM users
		WHERE username = ? AND status != 'deleted'
	`, username).Scan(
		&user.ID, &user.Username, &user.Password, &user.DisplayName, &user.Email, &user.Status,
		&tenantID, &rolesJSON, &policiesJSON, &metadataJSON, &user.CreatedAt, &user.UpdatedAt,
		&user.TwoFactorEnabled, &twoFactorSecret, &twoFactorSetupAt, &backupCodesJSON, &backupCodesUsedJSON,
		&themePreference, &languagePreference,
	)

	if tenantID.Valid {
		user.TenantID = tenantID.String
	}

	if twoFactorSecret.Valid {
		user.TwoFactorSecret = twoFactorSecret.String
	}

	if twoFactorSetupAt.Valid {
		user.TwoFactorSetupAt = twoFactorSetupAt.Int64
	}

	if themePreference.Valid {
		user.ThemePreference = themePreference.String
	} else {
		user.ThemePreference = "system" // Default
	}

	if languagePreference.Valid {
		user.LanguagePreference = languagePreference.String
	} else {
		user.LanguagePreference = "en" // Default
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

	// Deserialize 2FA backup codes
	if backupCodesJSON.Valid && backupCodesJSON.String != "" {
		json.Unmarshal([]byte(backupCodesJSON.String), &user.BackupCodes)
	}
	if backupCodesUsedJSON.Valid && backupCodesUsedJSON.String != "" {
		json.Unmarshal([]byte(backupCodesUsedJSON.String), &user.BackupCodesUsed)
	}

	return &user, nil
}

// GetUserByID retrieves a user by ID
func (s *SQLiteStore) GetUserByID(userID string) (*User, error) {
	var user User
	var rolesJSON, policiesJSON, metadataJSON string
	var tenantID sql.NullString
	var twoFactorSecret sql.NullString
	var twoFactorSetupAt sql.NullInt64
	var backupCodesJSON sql.NullString
	var backupCodesUsedJSON sql.NullString
	var themePreference sql.NullString
	var languagePreference sql.NullString

	err := s.db.QueryRow(`
		SELECT id, username, password_hash, display_name, email, status, tenant_id, roles, policies, metadata, created_at, updated_at,
		       two_factor_enabled, two_factor_secret, two_factor_setup_at, backup_codes, backup_codes_used,
		       theme_preference, language_preference
		FROM users
		WHERE id = ? AND status != 'deleted'
	`, userID).Scan(
		&user.ID, &user.Username, &user.Password, &user.DisplayName, &user.Email, &user.Status,
		&tenantID, &rolesJSON, &policiesJSON, &metadataJSON, &user.CreatedAt, &user.UpdatedAt,
		&user.TwoFactorEnabled, &twoFactorSecret, &twoFactorSetupAt, &backupCodesJSON, &backupCodesUsedJSON,
		&themePreference, &languagePreference,
	)

	if tenantID.Valid {
		user.TenantID = tenantID.String
	}

	if twoFactorSecret.Valid {
		user.TwoFactorSecret = twoFactorSecret.String
	}

	if twoFactorSetupAt.Valid {
		user.TwoFactorSetupAt = twoFactorSetupAt.Int64
	}

	if themePreference.Valid {
		user.ThemePreference = themePreference.String
	} else {
		user.ThemePreference = "system" // Default
	}

	if languagePreference.Valid {
		user.LanguagePreference = languagePreference.String
	} else {
		user.LanguagePreference = "en" // Default
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

	// Deserialize 2FA backup codes
	if backupCodesJSON.Valid && backupCodesJSON.String != "" {
		json.Unmarshal([]byte(backupCodesJSON.String), &user.BackupCodes)
	}
	if backupCodesUsedJSON.Valid && backupCodesUsedJSON.String != "" {
		json.Unmarshal([]byte(backupCodesUsedJSON.String), &user.BackupCodesUsed)
	}

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

// UpdateUserPreferences updates only the theme and language preferences for a user
func (s *SQLiteStore) UpdateUserPreferences(userID, themePreference, languagePreference string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE users
		SET theme_preference = ?, language_preference = ?, updated_at = ?
		WHERE id = ?
	`, themePreference, languagePreference, time.Now().Unix(), userID)

	if err != nil {
		return fmt.Errorf("failed to update user preferences: %w", err)
	}

	return tx.Commit()
}

// DeleteUser permanently deletes a user
func (s *SQLiteStore) DeleteUser(userID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete all associated access keys first (foreign key constraint)
	_, err = tx.Exec(`DELETE FROM access_keys WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user access keys: %w", err)
	}

	// Delete user
	_, err = tx.Exec(`DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	return tx.Commit()
}

// ListUsers returns all active users
func (s *SQLiteStore) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`
		SELECT id, username, password_hash, display_name, email, status, tenant_id, roles, policies, metadata, created_at, updated_at,
		       two_factor_enabled, two_factor_secret, two_factor_setup_at, backup_codes, backup_codes_used, locked_until,
		       theme_preference, language_preference
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
		var twoFactorSecret sql.NullString
		var twoFactorSetupAt sql.NullInt64
		var backupCodesJSON sql.NullString
		var backupCodesUsedJSON sql.NullString
		var lockedUntil sql.NullInt64
		var themePreference sql.NullString
		var languagePreference sql.NullString

		err := rows.Scan(
			&user.ID, &user.Username, &user.Password, &user.DisplayName, &user.Email, &user.Status,
			&tenantID, &rolesJSON, &policiesJSON, &metadataJSON, &user.CreatedAt, &user.UpdatedAt,
			&user.TwoFactorEnabled, &twoFactorSecret, &twoFactorSetupAt, &backupCodesJSON, &backupCodesUsedJSON, &lockedUntil,
			&themePreference, &languagePreference,
		)
		if err != nil {
			return nil, err
		}

		if tenantID.Valid {
			user.TenantID = tenantID.String
		}

		if twoFactorSecret.Valid {
			user.TwoFactorSecret = twoFactorSecret.String
		}

		if twoFactorSetupAt.Valid {
			user.TwoFactorSetupAt = twoFactorSetupAt.Int64
		}

		if lockedUntil.Valid {
			user.LockedUntil = lockedUntil.Int64
		}

		if themePreference.Valid {
			user.ThemePreference = themePreference.String
		} else {
			user.ThemePreference = "system" // Default
		}

		if languagePreference.Valid {
			user.LanguagePreference = languagePreference.String
		} else {
			user.LanguagePreference = "en" // Default
		}

		// Deserialize JSON fields
		json.Unmarshal([]byte(rolesJSON), &user.Roles)
		json.Unmarshal([]byte(policiesJSON), &user.Policies)
		json.Unmarshal([]byte(metadataJSON), &user.Metadata)

		// Deserialize 2FA backup codes
		if backupCodesJSON.Valid && backupCodesJSON.String != "" {
			json.Unmarshal([]byte(backupCodesJSON.String), &user.BackupCodes)
		}
		if backupCodesUsedJSON.Valid && backupCodesUsedJSON.String != "" {
			json.Unmarshal([]byte(backupCodesUsedJSON.String), &user.BackupCodesUsed)
		}

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

// DeleteAccessKey permanently deletes an access key
func (s *SQLiteStore) DeleteAccessKey(accessKeyID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM access_keys WHERE access_key_id = ?`, accessKeyID)
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

func (s *SQLiteStore) CountActiveAccessKeysByTenant(tenantID string) (int, error) {
	var count int
	err := s.db.QueryRow(`
        SELECT COUNT(*)
        FROM access_keys ak
        JOIN users u ON ak.user_id = u.id
        WHERE u.tenant_id = ? AND ak.status = 'active'
    `, tenantID).Scan(&count)
	return count, err
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
		    last_failed_login = ?,
		    failed_login_attempts = 0
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

// =============================================================================
// Two-Factor Authentication (2FA) Store Methods
// =============================================================================

// Enable2FA enables 2FA for a user
func (s *SQLiteStore) Enable2FA(ctx context.Context, userID string, secret string, backupCodes []string) error {
	backupCodesJSON, err := json.Marshal(backupCodes)
	if err != nil {
		return fmt.Errorf("failed to marshal backup codes: %w", err)
	}

	query := `
		UPDATE users
		SET two_factor_enabled = 1,
			two_factor_secret = ?,
			two_factor_setup_at = ?,
			backup_codes = ?,
			backup_codes_used = '[]',
			updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, query, secret, now, string(backupCodesJSON), now, userID)
	if err != nil {
		return fmt.Errorf("failed to enable 2FA: %w", err)
	}

	return nil
}

// Disable2FA disables 2FA for a user
func (s *SQLiteStore) Disable2FA(ctx context.Context, userID string) error {
	query := `
		UPDATE users
		SET two_factor_enabled = 0,
			two_factor_secret = NULL,
			two_factor_setup_at = NULL,
			backup_codes = NULL,
			backup_codes_used = NULL,
			updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, query, now, userID)
	if err != nil {
		return fmt.Errorf("failed to disable 2FA: %w", err)
	}

	return nil
}

// GetUserWith2FA retrieves a user with 2FA data included
func (s *SQLiteStore) GetUserWith2FA(ctx context.Context, userID string) (*User, error) {
	query := `
		SELECT id, username, display_name, email, status, tenant_id,
			   roles, policies, metadata, created_at, updated_at,
			   two_factor_enabled, two_factor_secret, two_factor_setup_at,
			   backup_codes, backup_codes_used
		FROM users
		WHERE id = ?
	`

	var user User
	var rolesJSON, policiesJSON, metadataJSON, backupCodesJSON, backupCodesUsedJSON sql.NullString
	var twoFactorSecret sql.NullString
	var twoFactorSetupAt sql.NullInt64

	err := s.db.QueryRowContext(ctx, query, userID).Scan(
		&user.ID,
		&user.Username,
		&user.DisplayName,
		&user.Email,
		&user.Status,
		&user.TenantID,
		&rolesJSON,
		&policiesJSON,
		&metadataJSON,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.TwoFactorEnabled,
		&twoFactorSecret,
		&twoFactorSetupAt,
		&backupCodesJSON,
		&backupCodesUsedJSON,
	)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Parse JSON fields
	if rolesJSON.Valid {
		json.Unmarshal([]byte(rolesJSON.String), &user.Roles)
	}
	if policiesJSON.Valid {
		json.Unmarshal([]byte(policiesJSON.String), &user.Policies)
	}
	if metadataJSON.Valid {
		json.Unmarshal([]byte(metadataJSON.String), &user.Metadata)
	}

	// Parse 2FA fields
	if twoFactorSecret.Valid {
		user.TwoFactorSecret = twoFactorSecret.String
	}
	if twoFactorSetupAt.Valid {
		user.TwoFactorSetupAt = twoFactorSetupAt.Int64
	}
	if backupCodesJSON.Valid {
		json.Unmarshal([]byte(backupCodesJSON.String), &user.BackupCodes)
	}
	if backupCodesUsedJSON.Valid {
		json.Unmarshal([]byte(backupCodesUsedJSON.String), &user.BackupCodesUsed)
	}

	return &user, nil
}

// MarkBackupCodeUsed marks a backup code as used
func (s *SQLiteStore) MarkBackupCodeUsed(ctx context.Context, userID string, codeIndex int) error {
	// Get current backup codes
	user, err := s.GetUserWith2FA(ctx, userID)
	if err != nil {
		return err
	}

	if codeIndex < 0 || codeIndex >= len(user.BackupCodes) {
		return fmt.Errorf("invalid backup code index")
	}

	// Add to used codes
	usedCodes := append(user.BackupCodesUsed, user.BackupCodes[codeIndex])
	usedCodesJSON, err := json.Marshal(usedCodes)
	if err != nil {
		return fmt.Errorf("failed to marshal used codes: %w", err)
	}

	query := `
		UPDATE users
		SET backup_codes_used = ?,
			updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, query, string(usedCodesJSON), now, userID)
	if err != nil {
		return fmt.Errorf("failed to mark backup code as used: %w", err)
	}

	return nil
}

// UpdateBackupCodes updates backup codes for a user
func (s *SQLiteStore) UpdateBackupCodes(ctx context.Context, userID string, backupCodes []string) error {
	backupCodesJSON, err := json.Marshal(backupCodes)
	if err != nil {
		return fmt.Errorf("failed to marshal backup codes: %w", err)
	}

	query := `
		UPDATE users
		SET backup_codes = ?,
			backup_codes_used = '[]',
			updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, query, string(backupCodesJSON), now, userID)
	if err != nil {
		return fmt.Errorf("failed to update backup codes: %w", err)
	}

	return nil
}
