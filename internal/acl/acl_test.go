package acl

import (
	"context"
	"os"
	"testing"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) (*badger.DB, func()) {
	tmpDir, err := os.MkdirTemp("", "acl-test-*")
	require.NoError(t, err)

	opts := badger.DefaultOptions(tmpDir).WithLogger(nil)
	db, err := badger.Open(opts)
	require.NoError(t, err)

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

// TestIsValidCannedACL tests canned ACL validation
func TestIsValidCannedACL(t *testing.T) {
	tests := []struct {
		name      string
		cannedACL string
		want      bool
	}{
		{"private", CannedACLPrivate, true},
		{"public-read", CannedACLPublicRead, true},
		{"public-read-write", CannedACLPublicReadWrite, true},
		{"authenticated-read", CannedACLAuthenticatedRead, true},
		{"bucket-owner-read", CannedACLBucketOwnerRead, true},
		{"bucket-owner-full-control", CannedACLBucketOwnerFullControl, true},
		{"log-delivery-write", CannedACLLogDeliveryWrite, true},
		{"invalid", "invalid-acl", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidCannedACL(tt.cannedACL)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIsValidPermission tests permission validation
func TestIsValidPermission(t *testing.T) {
	tests := []struct {
		name       string
		permission Permission
		want       bool
	}{
		{"READ", PermissionRead, true},
		{"WRITE", PermissionWrite, true},
		{"READ_ACP", PermissionReadACP, true},
		{"WRITE_ACP", PermissionWriteACP, true},
		{"FULL_CONTROL", PermissionFullControl, true},
		{"invalid", Permission("INVALID"), false},
		{"empty", Permission(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidPermission(tt.permission)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestCreateDefaultACL tests default ACL creation
func TestCreateDefaultACL(t *testing.T) {
	acl := CreateDefaultACL("user-123", "Test User")

	assert.NotNil(t, acl)
	assert.Equal(t, "user-123", acl.Owner.ID)
	assert.Equal(t, "Test User", acl.Owner.DisplayName)
	assert.Equal(t, CannedACLPrivate, acl.CannedACL)
	assert.Len(t, acl.Grants, 1)
	assert.Equal(t, GranteeTypeCanonicalUser, acl.Grants[0].Grantee.Type)
	assert.Equal(t, "user-123", acl.Grants[0].Grantee.ID)
	assert.Equal(t, PermissionFullControl, acl.Grants[0].Permission)
}

// TestGetCannedACLGrants_Private tests private canned ACL
func TestGetCannedACLGrants_Private(t *testing.T) {
	grants := GetCannedACLGrants(CannedACLPrivate, "owner-123", "Owner Name")

	require.Len(t, grants, 1)
	assert.Equal(t, GranteeTypeCanonicalUser, grants[0].Grantee.Type)
	assert.Equal(t, "owner-123", grants[0].Grantee.ID)
	assert.Equal(t, "Owner Name", grants[0].Grantee.DisplayName)
	assert.Equal(t, PermissionFullControl, grants[0].Permission)
}

// TestGetCannedACLGrants_PublicRead tests public-read canned ACL
func TestGetCannedACLGrants_PublicRead(t *testing.T) {
	grants := GetCannedACLGrants(CannedACLPublicRead, "owner-123", "Owner Name")

	require.Len(t, grants, 2)

	// Owner has FULL_CONTROL
	assert.Equal(t, GranteeTypeCanonicalUser, grants[0].Grantee.Type)
	assert.Equal(t, "owner-123", grants[0].Grantee.ID)
	assert.Equal(t, PermissionFullControl, grants[0].Permission)

	// AllUsers group has READ
	assert.Equal(t, GranteeTypeGroup, grants[1].Grantee.Type)
	assert.Equal(t, GroupAllUsers, grants[1].Grantee.URI)
	assert.Equal(t, PermissionRead, grants[1].Permission)
}

// TestGetCannedACLGrants_PublicReadWrite tests public-read-write canned ACL
func TestGetCannedACLGrants_PublicReadWrite(t *testing.T) {
	grants := GetCannedACLGrants(CannedACLPublicReadWrite, "owner-123", "Owner Name")

	require.Len(t, grants, 3)

	// Owner has FULL_CONTROL
	assert.Equal(t, PermissionFullControl, grants[0].Permission)

	// AllUsers group has READ
	assert.Equal(t, GroupAllUsers, grants[1].Grantee.URI)
	assert.Equal(t, PermissionRead, grants[1].Permission)

	// AllUsers group has WRITE
	assert.Equal(t, GroupAllUsers, grants[2].Grantee.URI)
	assert.Equal(t, PermissionWrite, grants[2].Permission)
}

// TestGetCannedACLGrants_AuthenticatedRead tests authenticated-read canned ACL
func TestGetCannedACLGrants_AuthenticatedRead(t *testing.T) {
	grants := GetCannedACLGrants(CannedACLAuthenticatedRead, "owner-123", "Owner Name")

	require.Len(t, grants, 2)

	// Owner has FULL_CONTROL
	assert.Equal(t, PermissionFullControl, grants[0].Permission)

	// AuthenticatedUsers group has READ
	assert.Equal(t, GroupAuthenticatedUsers, grants[1].Grantee.URI)
	assert.Equal(t, PermissionRead, grants[1].Permission)
}

// TestGetCannedACLGrants_LogDeliveryWrite tests log-delivery-write canned ACL
func TestGetCannedACLGrants_LogDeliveryWrite(t *testing.T) {
	grants := GetCannedACLGrants(CannedACLLogDeliveryWrite, "owner-123", "Owner Name")

	require.Len(t, grants, 3)

	// Owner has FULL_CONTROL
	assert.Equal(t, PermissionFullControl, grants[0].Permission)

	// LogDelivery group has WRITE
	assert.Equal(t, GroupLogDelivery, grants[1].Grantee.URI)
	assert.Equal(t, PermissionWrite, grants[1].Permission)

	// LogDelivery group has READ_ACP
	assert.Equal(t, GroupLogDelivery, grants[2].Grantee.URI)
	assert.Equal(t, PermissionReadACP, grants[2].Permission)
}

// TestGetCannedACLGrants_Invalid tests invalid canned ACL
func TestGetCannedACLGrants_Invalid(t *testing.T) {
	grants := GetCannedACLGrants("invalid-acl", "owner-123", "Owner Name")
	assert.Nil(t, grants)
}

// TestSetAndGetBucketACL tests bucket ACL operations
func TestSetAndGetBucketACL(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db)
	ctx := context.Background()

	t.Run("Set and get bucket ACL", func(t *testing.T) {
		acl := &ACL{
			Owner: Owner{
				ID:          "owner-123",
				DisplayName: "Test Owner",
			},
			Grants: []Grant{
				{
					Grantee: Grantee{
						Type:        GranteeTypeCanonicalUser,
						ID:          "owner-123",
						DisplayName: "Test Owner",
					},
					Permission: PermissionFullControl,
				},
			},
		}

		err := manager.SetBucketACL(ctx, "tenant-1", "test-bucket", acl)
		assert.NoError(t, err)

		retrieved, err := manager.GetBucketACL(ctx, "tenant-1", "test-bucket")
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, "owner-123", retrieved.Owner.ID)
		assert.Equal(t, "Test Owner", retrieved.Owner.DisplayName)
		assert.Len(t, retrieved.Grants, 1)
		assert.Equal(t, PermissionFullControl, retrieved.Grants[0].Permission)
	})

	t.Run("Get non-existent bucket ACL returns default", func(t *testing.T) {
		retrieved, err := manager.GetBucketACL(ctx, "tenant-1", "nonexistent-bucket")
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		// Should return default ACL
		assert.Equal(t, CannedACLPrivate, retrieved.CannedACL)
	})

	t.Run("Update existing bucket ACL", func(t *testing.T) {
		acl := &ACL{
			Owner: Owner{
				ID:          "owner-123",
				DisplayName: "Test Owner",
			},
			Grants: []Grant{
				{
					Grantee: Grantee{
						Type:        GranteeTypeCanonicalUser,
						ID:          "owner-123",
						DisplayName: "Test Owner",
					},
					Permission: PermissionFullControl,
				},
				{
					Grantee: Grantee{
						Type: GranteeTypeGroup,
						URI:  GroupAllUsers,
					},
					Permission: PermissionRead,
				},
			},
		}

		err := manager.SetBucketACL(ctx, "tenant-1", "test-bucket", acl)
		assert.NoError(t, err)

		retrieved, err := manager.GetBucketACL(ctx, "tenant-1", "test-bucket")
		assert.NoError(t, err)
		assert.Len(t, retrieved.Grants, 2)
	})
}

// TestSetAndGetObjectACL tests object ACL operations
func TestSetAndGetObjectACL(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db)
	ctx := context.Background()

	t.Run("Set and get object ACL", func(t *testing.T) {
		acl := &ACL{
			Owner: Owner{
				ID:          "owner-456",
				DisplayName: "Object Owner",
			},
			Grants: []Grant{
				{
					Grantee: Grantee{
						Type:        GranteeTypeCanonicalUser,
						ID:          "owner-456",
						DisplayName: "Object Owner",
					},
					Permission: PermissionFullControl,
				},
			},
		}

		err := manager.SetObjectACL(ctx, "tenant-1", "test-bucket", "test-object.txt", acl)
		assert.NoError(t, err)

		retrieved, err := manager.GetObjectACL(ctx, "tenant-1", "test-bucket", "test-object.txt")
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, "owner-456", retrieved.Owner.ID)
		assert.Equal(t, "Object Owner", retrieved.Owner.DisplayName)
	})

	t.Run("Get non-existent object ACL returns default", func(t *testing.T) {
		retrieved, err := manager.GetObjectACL(ctx, "tenant-1", "test-bucket", "nonexistent-object.txt")
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		// Should return default ACL
		assert.Equal(t, CannedACLPrivate, retrieved.CannedACL)
	})
}

// TestGetCannedACL tests GetCannedACL method
func TestGetCannedACL(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db)

	t.Run("Get private canned ACL", func(t *testing.T) {
		acl, err := manager.GetCannedACL(CannedACLPrivate, "owner-123", "Test Owner")
		assert.NoError(t, err)
		assert.NotNil(t, acl)
		assert.Equal(t, "owner-123", acl.Owner.ID)
		assert.Equal(t, CannedACLPrivate, acl.CannedACL)
		assert.Len(t, acl.Grants, 1)
	})

	t.Run("Get public-read canned ACL", func(t *testing.T) {
		acl, err := manager.GetCannedACL(CannedACLPublicRead, "owner-123", "Test Owner")
		assert.NoError(t, err)
		assert.Len(t, acl.Grants, 2)
	})

	t.Run("Get invalid canned ACL", func(t *testing.T) {
		acl, err := manager.GetCannedACL("invalid-acl", "owner-123", "Test Owner")
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidCannedACL, err)
		assert.Nil(t, acl)
	})
}

// TestCheckPermission tests permission checking for specific users
func TestCheckPermission(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db)

	t.Run("Owner has full control", func(t *testing.T) {
		acl := CreateDefaultACL("owner-123", "Test Owner")

		has := manager.CheckPermission(context.Background(), acl, "owner-123", PermissionRead)
		assert.True(t, has)

		has = manager.CheckPermission(context.Background(), acl, "owner-123", PermissionWrite)
		assert.True(t, has)

		has = manager.CheckPermission(context.Background(), acl, "owner-123", PermissionFullControl)
		assert.True(t, has)
	})

	t.Run("Non-owner has no permission on private ACL", func(t *testing.T) {
		acl := CreateDefaultACL("owner-123", "Test Owner")

		has := manager.CheckPermission(context.Background(), acl, "other-user", PermissionRead)
		assert.False(t, has)
	})

	t.Run("User has specific permission", func(t *testing.T) {
		acl := &ACL{
			Owner: Owner{ID: "owner-123"},
			Grants: []Grant{
				{
					Grantee:    Grantee{Type: GranteeTypeCanonicalUser, ID: "user-456"},
					Permission: PermissionRead,
				},
			},
		}

		has := manager.CheckPermission(context.Background(), acl, "user-456", PermissionRead)
		assert.True(t, has)

		has = manager.CheckPermission(context.Background(), acl, "user-456", PermissionWrite)
		assert.False(t, has)
	})
}

// TestCheckPublicAccess tests public access checking
func TestCheckPublicAccess(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db)

	t.Run("Private ACL has no public access", func(t *testing.T) {
		acl := CreateDefaultACL("owner-123", "Test Owner")

		has := manager.CheckPublicAccess(acl, PermissionRead)
		assert.False(t, has)
	})

	t.Run("Public-read ACL has public read access", func(t *testing.T) {
		acl, _ := manager.GetCannedACL(CannedACLPublicRead, "owner-123", "Test Owner")

		has := manager.CheckPublicAccess(acl, PermissionRead)
		assert.True(t, has)

		has = manager.CheckPublicAccess(acl, PermissionWrite)
		assert.False(t, has)
	})

	t.Run("Public-read-write ACL has public read and write access", func(t *testing.T) {
		acl, _ := manager.GetCannedACL(CannedACLPublicReadWrite, "owner-123", "Test Owner")

		has := manager.CheckPublicAccess(acl, PermissionRead)
		assert.True(t, has)

		has = manager.CheckPublicAccess(acl, PermissionWrite)
		assert.True(t, has)
	})
}

// TestCheckAuthenticatedAccess tests authenticated user access checking
func TestCheckAuthenticatedAccess(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db)

	t.Run("Private ACL has no authenticated access", func(t *testing.T) {
		acl := CreateDefaultACL("owner-123", "Test Owner")

		has := manager.CheckAuthenticatedAccess(acl, PermissionRead)
		assert.False(t, has)
	})

	t.Run("Authenticated-read ACL has authenticated read access", func(t *testing.T) {
		acl, _ := manager.GetCannedACL(CannedACLAuthenticatedRead, "owner-123", "Test Owner")

		has := manager.CheckAuthenticatedAccess(acl, PermissionRead)
		assert.True(t, has)

		has = manager.CheckAuthenticatedAccess(acl, PermissionWrite)
		assert.False(t, has)
	})
}

// TestMultiTenantACLs tests ACL isolation between tenants
func TestMultiTenantACLs(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db)
	ctx := context.Background()

	acl1 := CreateDefaultACL("owner-1", "Owner 1")
	acl2 := CreateDefaultACL("owner-2", "Owner 2")

	// Set ACLs for same bucket name in different tenants
	err := manager.SetBucketACL(ctx, "tenant-1", "shared-bucket", acl1)
	assert.NoError(t, err)

	err = manager.SetBucketACL(ctx, "tenant-2", "shared-bucket", acl2)
	assert.NoError(t, err)

	// Retrieve and verify they're different
	retrieved1, err := manager.GetBucketACL(ctx, "tenant-1", "shared-bucket")
	assert.NoError(t, err)
	assert.Equal(t, "owner-1", retrieved1.Owner.ID)

	retrieved2, err := manager.GetBucketACL(ctx, "tenant-2", "shared-bucket")
	assert.NoError(t, err)
	assert.Equal(t, "owner-2", retrieved2.Owner.ID)
}
