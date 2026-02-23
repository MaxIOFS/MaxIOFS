package acl

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// aclManager implements the Manager interface
type aclManager struct {
	kvStore metadata.RawKVStore
}

// Storage key prefixes
const (
	bucketACLPrefix = "acl:bucket:"
	objectACLPrefix = "acl:object:"
)

// GetBucketACL retrieves the ACL for a bucket
func (m *aclManager) GetBucketACL(ctx context.Context, tenantID, bucketName string) (*ACL, error) {
	key := m.bucketACLKey(tenantID, bucketName)

	data, err := m.kvStore.GetRaw(ctx, key)
	if err != nil {
		if err == metadata.ErrNotFound {
			logrus.WithFields(logrus.Fields{
				"tenant": tenantID,
				"bucket": bucketName,
			}).Debug("ACL not found, returning default private ACL")
			return CreateDefaultACL("maxiofs", "MaxIOFS"), nil
		}
		return nil, fmt.Errorf("failed to get bucket ACL: %w", err)
	}

	var acl ACL
	if err := json.Unmarshal(data, &acl); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bucket ACL: %w", err)
	}
	return &acl, nil
}

// SetBucketACL sets the ACL for a bucket
func (m *aclManager) SetBucketACL(ctx context.Context, tenantID, bucketName string, acl *ACL) error {
	if acl == nil {
		return ErrInvalidACL
	}

	key := m.bucketACLKey(tenantID, bucketName)

	data, err := json.Marshal(acl)
	if err != nil {
		return fmt.Errorf("failed to marshal ACL: %w", err)
	}

	if err := m.kvStore.PutRaw(ctx, key, data); err != nil {
		return fmt.Errorf("failed to set bucket ACL: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"tenant":     tenantID,
		"bucket":     bucketName,
		"canned_acl": acl.CannedACL,
		"grants":     len(acl.Grants),
	}).Debug("Bucket ACL set successfully")

	return nil
}

// GetObjectACL retrieves the ACL for an object
func (m *aclManager) GetObjectACL(ctx context.Context, tenantID, bucketName, objectKey string) (*ACL, error) {
	key := m.objectACLKey(tenantID, bucketName, objectKey)

	data, err := m.kvStore.GetRaw(ctx, key)
	if err != nil {
		if err == metadata.ErrNotFound {
			logrus.WithFields(logrus.Fields{
				"tenant": tenantID,
				"bucket": bucketName,
				"object": objectKey,
			}).Debug("ACL not found, returning default private ACL")
			return CreateDefaultACL("maxiofs", "MaxIOFS"), nil
		}
		return nil, fmt.Errorf("failed to get object ACL: %w", err)
	}

	var acl ACL
	if err := json.Unmarshal(data, &acl); err != nil {
		return nil, fmt.Errorf("failed to unmarshal object ACL: %w", err)
	}
	return &acl, nil
}

// SetObjectACL sets the ACL for an object
func (m *aclManager) SetObjectACL(ctx context.Context, tenantID, bucketName, objectKey string, acl *ACL) error {
	if acl == nil {
		return ErrInvalidACL
	}

	key := m.objectACLKey(tenantID, bucketName, objectKey)

	data, err := json.Marshal(acl)
	if err != nil {
		return fmt.Errorf("failed to marshal ACL: %w", err)
	}

	if err := m.kvStore.PutRaw(ctx, key, data); err != nil {
		return fmt.Errorf("failed to set object ACL: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"tenant":     tenantID,
		"bucket":     bucketName,
		"object":     objectKey,
		"canned_acl": acl.CannedACL,
		"grants":     len(acl.Grants),
	}).Debug("Object ACL set successfully")

	return nil
}

// GetCannedACL creates an ACL from a canned ACL string
func (m *aclManager) GetCannedACL(cannedACL string, ownerID, ownerDisplayName string) (*ACL, error) {
	if !IsValidCannedACL(cannedACL) {
		return nil, ErrInvalidCannedACL
	}

	grants := GetCannedACLGrants(cannedACL, ownerID, ownerDisplayName)
	if grants == nil {
		return nil, ErrInvalidCannedACL
	}

	return &ACL{
		Owner: Owner{
			ID:          ownerID,
			DisplayName: ownerDisplayName,
		},
		Grants:    grants,
		CannedACL: cannedACL,
	}, nil
}

// CheckPermission checks if a user has a specific permission in an ACL
func (m *aclManager) CheckPermission(ctx context.Context, acl *ACL, userID string, permission Permission) bool {
	if acl == nil {
		return false
	}

	// Owner always has full control
	if acl.Owner.ID == userID {
		return true
	}

	// Check grants
	for _, grant := range acl.Grants {
		if m.grantMatchesUser(grant, userID) {
			if m.permissionSatisfies(grant.Permission, permission) {
				return true
			}
		}
	}

	return false
}

// CheckPublicAccess checks if an ACL allows public access for a permission
func (m *aclManager) CheckPublicAccess(acl *ACL, permission Permission) bool {
	if acl == nil {
		return false
	}

	for _, grant := range acl.Grants {
		if grant.Grantee.Type == GranteeTypeGroup && grant.Grantee.URI == GroupAllUsers {
			if m.permissionSatisfies(grant.Permission, permission) {
				return true
			}
		}
	}

	return false
}

// CheckAuthenticatedAccess checks if an ACL allows authenticated user access
func (m *aclManager) CheckAuthenticatedAccess(acl *ACL, permission Permission) bool {
	if acl == nil {
		return false
	}

	for _, grant := range acl.Grants {
		if grant.Grantee.Type == GranteeTypeGroup && grant.Grantee.URI == GroupAuthenticatedUsers {
			if m.permissionSatisfies(grant.Permission, permission) {
				return true
			}
		}
	}

	return false
}

// grantMatchesUser checks if a grant applies to a specific user
func (m *aclManager) grantMatchesUser(grant Grant, userID string) bool {
	switch grant.Grantee.Type {
	case GranteeTypeCanonicalUser:
		return grant.Grantee.ID == userID
	case GranteeTypeGroup:
		return false
	case GranteeTypeAmazonCustomer:
		return false
	default:
		return false
	}
}

// permissionSatisfies checks if a granted permission satisfies a required permission
func (m *aclManager) permissionSatisfies(granted Permission, required Permission) bool {
	if granted == PermissionFullControl {
		return true
	}
	return granted == required
}

// bucketACLKey generates the storage key for a bucket ACL
func (m *aclManager) bucketACLKey(tenantID, bucketName string) string {
	if tenantID == "" {
		return fmt.Sprintf("%s%s", bucketACLPrefix, bucketName)
	}
	return fmt.Sprintf("%s%s:%s", bucketACLPrefix, tenantID, bucketName)
}

// objectACLKey generates the storage key for an object ACL
func (m *aclManager) objectACLKey(tenantID, bucketName, objectKey string) string {
	if tenantID == "" {
		return fmt.Sprintf("%s%s:%s", objectACLPrefix, bucketName, objectKey)
	}
	return fmt.Sprintf("%s%s:%s:%s", objectACLPrefix, tenantID, bucketName, objectKey)
}
