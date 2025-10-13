package share

import (
	"context"
)

// Store defines the interface for share persistence
type Store interface {
	CreateShare(ctx context.Context, share *Share) error
	GetShare(ctx context.Context, shareID string) (*Share, error)
	GetShareByToken(ctx context.Context, shareToken string) (*Share, error)
	GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (*Share, error)
	ListShares(ctx context.Context, userID string) ([]*Share, error)
	ListBucketShares(ctx context.Context, bucketName string) ([]*Share, error)
	DeleteShare(ctx context.Context, shareID string) error
	DeleteExpiredShares(ctx context.Context) error
}
