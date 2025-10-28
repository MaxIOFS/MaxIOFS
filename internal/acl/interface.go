package acl

import (
	"context"

	"github.com/dgraph-io/badger/v4"
)

// Manager defines the interface for ACL management
type Manager interface {
	// Bucket ACL operations
	GetBucketACL(ctx context.Context, tenantID, bucketName string) (*ACL, error)
	SetBucketACL(ctx context.Context, tenantID, bucketName string, acl *ACL) error

	// Object ACL operations
	GetObjectACL(ctx context.Context, tenantID, bucketName, objectKey string) (*ACL, error)
	SetObjectACL(ctx context.Context, tenantID, bucketName, objectKey string, acl *ACL) error

	// Canned ACL helpers
	GetCannedACL(cannedACL string, ownerID, ownerDisplayName string) (*ACL, error)

	// Permission checking
	CheckPermission(ctx context.Context, acl *ACL, userID string, permission Permission) bool
	CheckPublicAccess(acl *ACL, permission Permission) bool
	CheckAuthenticatedAccess(acl *ACL, permission Permission) bool
}

// NewManager creates a new ACL manager
func NewManager(db *badger.DB) Manager {
	return &aclManager{
		db: db,
	}
}
