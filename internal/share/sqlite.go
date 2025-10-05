package share

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
	schema := `
	CREATE TABLE IF NOT EXISTS shares (
		id TEXT PRIMARY KEY,
		bucket_name TEXT NOT NULL,
		object_key TEXT NOT NULL,
		access_key_id TEXT NOT NULL,
		secret_key TEXT NOT NULL,
		share_token TEXT NOT NULL UNIQUE,
		expires_at INTEGER,
		created_at INTEGER NOT NULL,
		created_by TEXT NOT NULL,
		UNIQUE(bucket_name, object_key)
	);

	CREATE INDEX IF NOT EXISTS idx_shares_token ON shares(share_token);
	CREATE INDEX IF NOT EXISTS idx_shares_bucket_object ON shares(bucket_name, object_key);
	CREATE INDEX IF NOT EXISTS idx_shares_created_by ON shares(created_by);
	CREATE INDEX IF NOT EXISTS idx_shares_expires_at ON shares(expires_at);
	`

	_, err := s.db.Exec(schema)
	return err
}

// CreateShare creates a new share
func (s *SQLiteStore) CreateShare(ctx context.Context, share *Share) error {
	var expiresAt interface{}
	if share.ExpiresAt != nil {
		expiresAt = share.ExpiresAt.Unix()
	}

	query := `
		INSERT INTO shares (id, bucket_name, object_key, access_key_id, secret_key, share_token, expires_at, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(bucket_name, object_key) DO UPDATE SET
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
		SELECT id, bucket_name, object_key, access_key_id, secret_key, share_token, expires_at, created_at, created_by
		FROM shares
		WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, shareID)
	return s.scanShare(row)
}

// GetShareByToken retrieves a share by token
func (s *SQLiteStore) GetShareByToken(ctx context.Context, shareToken string) (*Share, error) {
	query := `
		SELECT id, bucket_name, object_key, access_key_id, secret_key, share_token, expires_at, created_at, created_by
		FROM shares
		WHERE share_token = ?
		AND (expires_at IS NULL OR expires_at > ?)
	`

	row := s.db.QueryRowContext(ctx, query, shareToken, time.Now().UTC().Unix())
	return s.scanShare(row)
}

// GetShareByObject retrieves a share by bucket and object
func (s *SQLiteStore) GetShareByObject(ctx context.Context, bucketName, objectKey string) (*Share, error) {
	query := `
		SELECT id, bucket_name, object_key, access_key_id, secret_key, share_token, expires_at, created_at, created_by
		FROM shares
		WHERE bucket_name = ? AND object_key = ?
		AND (expires_at IS NULL OR expires_at > ?)
	`

	row := s.db.QueryRowContext(ctx, query, bucketName, objectKey, time.Now().UTC().Unix())
	return s.scanShare(row)
}

// ListShares lists all shares for a user
func (s *SQLiteStore) ListShares(ctx context.Context, userID string) ([]*Share, error) {
	query := `
		SELECT id, bucket_name, object_key, access_key_id, secret_key, share_token, expires_at, created_at, created_by
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

// ListBucketShares lists all shares for a bucket
func (s *SQLiteStore) ListBucketShares(ctx context.Context, bucketName string) ([]*Share, error) {
	query := `
		SELECT id, bucket_name, object_key, access_key_id, secret_key, share_token, expires_at, created_at, created_by
		FROM shares
		WHERE bucket_name = ?
		AND (expires_at IS NULL OR expires_at > ?)
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, bucketName, time.Now().UTC().Unix())
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
