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
	GetShareByObject(ctx context.Context, bucketName, objectKey string) (*Share, error)
	ListShares(ctx context.Context, userID string) ([]*Share, error)
	ListBucketShares(ctx context.Context, bucketName string) ([]*Share, error)
	DeleteShare(ctx context.Context, shareID string) error
	DeleteExpiredShares(ctx context.Context) error
}

// ShareManager implements Manager interface
type ShareManager struct {
	store Store
}

// NewManager creates a new share manager
func NewManager(store Store) Manager {
	return &ShareManager{
		store: store,
	}
}

// NewManagerWithDB creates a new share manager with SQLite database
func NewManagerWithDB(dataDir string) (Manager, error) {
	dbPath := filepath.Join(dataDir, "maxiofs.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store, err := NewSQLiteStore(db)
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

	share := &Share{
		ID:          generateID(),
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
func (m *ShareManager) GetShareByObject(ctx context.Context, bucketName, objectKey string) (*Share, error) {
	return m.store.GetShareByObject(ctx, bucketName, objectKey)
}

// ListShares lists all shares for a user
func (m *ShareManager) ListShares(ctx context.Context, userID string) ([]*Share, error) {
	return m.store.ListShares(ctx, userID)
}

// ListBucketShares lists all shares for a bucket
func (m *ShareManager) ListBucketShares(ctx context.Context, bucketName string) ([]*Share, error) {
	return m.store.ListBucketShares(ctx, bucketName)
}

// DeleteShare deletes a share
func (m *ShareManager) DeleteShare(ctx context.Context, shareID string) error {
	return m.store.DeleteShare(ctx, shareID)
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

func generateID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
