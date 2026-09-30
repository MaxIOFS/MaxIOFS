package share

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"
)

// Manager handles share operations
type Manager interface {
	CreateShare(ctx context.Context, bucketName, objectKey, tenantID, accessKeyID, secretKey, userID string, expiresIn *int64) (*Share, error)
	GetShare(ctx context.Context, shareID string) (*Share, error)
	GetShareByToken(ctx context.Context, shareToken string) (*Share, error)
	GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (*Share, error)
	ListShares(ctx context.Context, userID string) ([]*Share, error)
	ListBucketShares(ctx context.Context, bucketName, tenantID string) ([]*Share, error)
	DeleteShare(ctx context.Context, shareID string) error
	DeleteExpiredShares(ctx context.Context) error
	SetChangeObserver(o ChangeObserver)
}

// SharesTable is the table shares are kept in.
const SharesTable = "shares"

// ChangeObserver is told, once stored, of every share created or deleted:
// the table, the share's ID, and whether it was deleted.
type ChangeObserver func(ctx context.Context, table, id string, deleted bool)

// ShareManager implements Manager interface
type ShareManager struct {
	store    Store
	observer ChangeObserver
}

// SetChangeObserver sets who is told of every stored change. Shares deleted
// because they expired are not told: every node expires them on its own.
func (m *ShareManager) SetChangeObserver(o ChangeObserver) {
	m.observer = o
}

func (m *ShareManager) changed(ctx context.Context, id string, deleted bool) {
	if m.observer != nil {
		m.observer(ctx, SharesTable, id, deleted)
	}
}

// NewManager creates a new share manager
func NewManager(store Store) Manager {
	return &ShareManager{
		store: store,
	}
}

// NewManagerWithDB creates a new share manager with SQLite database.
// key returns the key secret_key is encrypted with at rest (AES-256-GCM); nil
// disables it.
func NewManagerWithDB(dataDir string, key func() string) (Manager, error) {
	dbPath := filepath.Join(dataDir, "db", "maxiofs.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store, err := NewSQLiteStore(db, key)
	if err != nil {
		return nil, fmt.Errorf("failed to create share store: %w", err)
	}

	return NewManager(store), nil
}

// CreateShare creates a new share for an object
func (m *ShareManager) CreateShare(ctx context.Context, bucketName, objectKey, tenantID, accessKeyID, secretKey, userID string, expiresIn *int64) (*Share, error) {
	// Generate unique share token
	token, err := generateShareToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate share token: %w", err)
	}

	// Calculate expiration
	var expiresAt *time.Time
	if expiresIn != nil && *expiresIn > 0 {
		expiry := time.Now().UTC().Add(time.Duration(*expiresIn) * time.Second)
		expiresAt = &expiry
	}

	// Generate unique share ID
	shareID, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate share ID: %w", err)
	}

	share := &Share{
		ID:          shareID,
		BucketName:  bucketName,
		ObjectKey:   objectKey,
		TenantID:    tenantID,
		AccessKeyID: accessKeyID,
		SecretKey:   secretKey,
		ShareToken:  token,
		ExpiresAt:   expiresAt,
		CreatedAt:   time.Now().UTC(),
		CreatedBy:   userID,
	}

	if err := m.store.CreateShare(ctx, share); err != nil {
		return nil, err
	}
	m.changed(ctx, share.ID, false)

	return share, nil
}

// GetShare retrieves a share by ID
func (m *ShareManager) GetShare(ctx context.Context, shareID string) (*Share, error) {
	share, err := m.store.GetShare(ctx, shareID)
	if err != nil {
		return nil, err
	}

	if share.IsExpired() {
		return nil, ErrShareExpired
	}

	return share, nil
}

// GetShareByToken retrieves a share by token
func (m *ShareManager) GetShareByToken(ctx context.Context, shareToken string) (*Share, error) {
	share, err := m.store.GetShareByToken(ctx, shareToken)
	if err != nil {
		return nil, err
	}

	if share.IsExpired() {
		return nil, ErrShareExpired
	}

	return share, nil
}

// GetShareByObject retrieves active share for an object
func (m *ShareManager) GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (*Share, error) {
	return m.store.GetShareByObject(ctx, bucketName, objectKey, tenantID)
}

// ListShares lists all shares for a user
func (m *ShareManager) ListShares(ctx context.Context, userID string) ([]*Share, error) {
	return m.store.ListShares(ctx, userID)
}

// ListBucketShares lists all shares for a bucket
func (m *ShareManager) ListBucketShares(ctx context.Context, bucketName, tenantID string) ([]*Share, error) {
	return m.store.ListBucketShares(ctx, bucketName, tenantID)
}

// DeleteShare deletes a share
func (m *ShareManager) DeleteShare(ctx context.Context, shareID string) error {
	if err := m.store.DeleteShare(ctx, shareID); err != nil {
		return err
	}
	m.changed(ctx, shareID, true)
	return nil
}

// DeleteExpiredShares deletes all expired shares
func (m *ShareManager) DeleteExpiredShares(ctx context.Context) error {
	return m.store.DeleteExpiredShares(ctx)
}

// Helper functions

func generateShareToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// NEW-03: return error so a rand.Read failure is not silently swallowed.
func generateID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
