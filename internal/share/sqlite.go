package share

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// SQLiteStore implements Store interface using SQLite
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite store
func NewSQLiteStore(db *sql.DB) (Store, error) {
	store := &SQLiteStore{db: db}
	if err := store.initialize(); err != nil {
		return nil, err
	}
	return store, nil
}

// initialize creates the shares table
func (s *SQLiteStore) initialize() error {
	// Check if shares table exists and get its structure
	var tableSql string
	err := s.db.QueryRow(`
		SELECT sql FROM sqlite_master 
		WHERE type='table' AND name='shares'
	`).Scan(&tableSql)

	// If table exists but doesn't have the correct UNIQUE constraint, we need to migrate
	needsMigration := err == nil && tableSql != "" &&
		(tableSql == "" || // Empty means no table
			!contains(tableSql, "UNIQUE(bucket_name, object_key, tenant_id)") &&
				!contains(tableSql, "UNIQUE (bucket_name, object_key, tenant_id)"))

	if needsMigration {
		fmt.Println("⚠️  Shares table has incorrect structure, migrating...")

		// Get all existing shares first
		var existingShares []map[string]interface{}
		rows, err := s.db.Query(`SELECT * FROM shares`)
		if err == nil {
			defer rows.Close()
			cols, _ := rows.Columns()
			for rows.Next() {
				columns := make([]interface{}, len(cols))
				columnPointers := make([]interface{}, len(cols))
				for i := range columns {
					columnPointers[i] = &columns[i]
				}
				rows.Scan(columnPointers...)

				share := make(map[string]interface{})
				for i, colName := range cols {
					share[colName] = columns[i]
				}
				existingShares = append(existingShares, share)
			}
		}

		// Drop the old table
		if _, err := s.db.Exec(`DROP TABLE IF EXISTS shares`); err != nil {
			return fmt.Errorf("failed to drop old shares table: %v", err)
		}

		// Create new table with correct structure
		createTableSQL := `
		CREATE TABLE shares (
			id TEXT PRIMARY KEY,
			bucket_name TEXT NOT NULL,
			object_key TEXT NOT NULL,
			tenant_id TEXT DEFAULT '',
			access_key_id TEXT NOT NULL,
			secret_key TEXT NOT NULL,
			share_token TEXT NOT NULL UNIQUE,
			expires_at INTEGER,
			created_at INTEGER NOT NULL,
			created_by TEXT NOT NULL,
			UNIQUE(bucket_name, object_key, tenant_id)
		)`

		if _, err := s.db.Exec(createTableSQL); err != nil {
			return fmt.Errorf("failed to create new shares table: %v", err)
		}

		// Restore existing shares with tenant_id
		for _, share := range existingShares {
			tenantID := ""
			if tid, ok := share["tenant_id"]; ok && tid != nil {
				if tidStr, ok := tid.(string); ok {
					tenantID = tidStr
				}
			}

			_, err := s.db.Exec(`
				INSERT OR IGNORE INTO shares 
				(id, bucket_name, object_key, tenant_id, access_key_id, secret_key, share_token, expires_at, created_at, created_by)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				share["id"], share["bucket_name"], share["object_key"], tenantID,
				share["access_key_id"], share["secret_key"], share["share_token"],
				share["expires_at"], share["created_at"], share["created_by"],
			)
			if err != nil {
				logrus.WithError(err).Warnf("Failed to restore share %v", share["id"])
			}
		}

		logrus.Infof("Migrated %d shares to new structure", len(existingShares))
	}

	// Create table if not exists (this will only run on first install)
	schema := `
	CREATE TABLE IF NOT EXISTS shares (
		id TEXT PRIMARY KEY,
		bucket_name TEXT NOT NULL,
		object_key TEXT NOT NULL,
		tenant_id TEXT DEFAULT '',
		access_key_id TEXT NOT NULL,
		secret_key TEXT NOT NULL,
		share_token TEXT NOT NULL UNIQUE,
		expires_at INTEGER,
		created_at INTEGER NOT NULL,
		created_by TEXT NOT NULL,
		UNIQUE(bucket_name, object_key, tenant_id)
	);

	CREATE INDEX IF NOT EXISTS idx_shares_token ON shares(share_token);
	CREATE INDEX IF NOT EXISTS idx_shares_bucket_object ON shares(bucket_name, object_key);
	CREATE INDEX IF NOT EXISTS idx_shares_created_by ON shares(created_by);
	CREATE INDEX IF NOT EXISTS idx_shares_expires_at ON shares(expires_at);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	return nil
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				indexOf(s, substr) >= 0)))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// CreateShare creates a new share
func (s *SQLiteStore) CreateShare(ctx context.Context, share *Share) error {
	var expiresAt interface{}
	if share.ExpiresAt != nil {
		expiresAt = share.ExpiresAt.Unix()
	}

	query := `
		INSERT INTO shares (id, bucket_name, object_key, tenant_id, access_key_id, secret_key, share_token, expires_at, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(bucket_name, object_key, tenant_id) DO UPDATE SET
			access_key_id = excluded.access_key_id,
			secret_key = excluded.secret_key,
			share_token = excluded.share_token,
			expires_at = excluded.expires_at,
			created_at = excluded.created_at,
			created_by = excluded.created_by
	`

	_, err := s.db.ExecContext(ctx, query,
		share.ID,
		share.BucketName,
		share.ObjectKey,
		share.TenantID,
		share.AccessKeyID,
		share.SecretKey,
		share.ShareToken,
		expiresAt,
		share.CreatedAt.Unix(),
		share.CreatedBy,
	)

	return err
}

// GetShare retrieves a share by ID
func (s *SQLiteStore) GetShare(ctx context.Context, shareID string) (*Share, error) {
	query := `
		SELECT id, bucket_name, object_key, tenant_id, access_key_id, secret_key, share_token, expires_at, created_at, created_by
		FROM shares
		WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, shareID)
	return s.scanShare(row)
}

// GetShareByToken retrieves a share by token
func (s *SQLiteStore) GetShareByToken(ctx context.Context, shareToken string) (*Share, error) {
	query := `
		SELECT id, bucket_name, object_key, tenant_id, access_key_id, secret_key, share_token, expires_at, created_at, created_by
		FROM shares
		WHERE share_token = ?
		AND (expires_at IS NULL OR expires_at > ?)
	`

	row := s.db.QueryRowContext(ctx, query, shareToken, time.Now().UTC().Unix())
	return s.scanShare(row)
}

// GetShareByObject retrieves a share by bucket and object
func (s *SQLiteStore) GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (*Share, error) {
	query := `
		SELECT id, bucket_name, object_key, tenant_id, access_key_id, secret_key, share_token, expires_at, created_at, created_by
		FROM shares
		WHERE bucket_name = ? AND object_key = ? AND tenant_id = ?
		AND (expires_at IS NULL OR expires_at > ?)
	`

	row := s.db.QueryRowContext(ctx, query, bucketName, objectKey, tenantID, time.Now().UTC().Unix())
	return s.scanShare(row)
}

// ListShares lists all shares for a user
func (s *SQLiteStore) ListShares(ctx context.Context, userID string) ([]*Share, error) {
	query := `
		SELECT id, bucket_name, object_key, tenant_id, access_key_id, secret_key, share_token, expires_at, created_at, created_by
		FROM shares
		WHERE created_by = ?
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shares []*Share
	for rows.Next() {
		share, err := s.scanShare(rows)
		if err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}

	return shares, rows.Err()
}

// ListBucketShares lists all shares for a bucket and tenant
func (s *SQLiteStore) ListBucketShares(ctx context.Context, bucketName, tenantID string) ([]*Share, error) {
	query := `
		SELECT id, bucket_name, object_key, tenant_id, access_key_id, secret_key, share_token, expires_at, created_at, created_by
		FROM shares
		WHERE bucket_name = ? AND tenant_id = ?
		AND (expires_at IS NULL OR expires_at > ?)
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, bucketName, tenantID, time.Now().UTC().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shares []*Share
	for rows.Next() {
		share, err := s.scanShare(rows)
		if err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}

	return shares, rows.Err()
}

// DeleteShare deletes a share
func (s *SQLiteStore) DeleteShare(ctx context.Context, shareID string) error {
	query := `DELETE FROM shares WHERE id = ?`
	result, err := s.db.ExecContext(ctx, query, shareID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrShareNotFound
	}

	return nil
}

// DeleteExpiredShares deletes all expired shares
func (s *SQLiteStore) DeleteExpiredShares(ctx context.Context) error {
	query := `DELETE FROM shares WHERE expires_at IS NOT NULL AND expires_at < ?`
	_, err := s.db.ExecContext(ctx, query, time.Now().UTC().Unix())
	return err
}

// scanShare scans a share from a database row
func (s *SQLiteStore) scanShare(scanner interface {
	Scan(dest ...interface{}) error
}) (*Share, error) {
	var share Share
	var expiresAt sql.NullInt64
	var createdAt int64

	err := scanner.Scan(
		&share.ID,
		&share.BucketName,
		&share.ObjectKey,
		&share.TenantID,
		&share.AccessKeyID,
		&share.SecretKey,
		&share.ShareToken,
		&expiresAt,
		&createdAt,
		&share.CreatedBy,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrShareNotFound
		}
		return nil, fmt.Errorf("failed to scan share: %w", err)
	}

	share.CreatedAt = time.Unix(createdAt, 0).UTC()

	if expiresAt.Valid {
		expiry := time.Unix(expiresAt.Int64, 0).UTC()
		share.ExpiresAt = &expiry
	}

	return &share, nil
}
