package s3compat

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aclTestEnv contains the testing environment for ACL security tests
type aclTestEnv struct {
	handler       *Handler
	authManager   auth.Manager
	bucketManager bucket.Manager
	objectManager object.Manager
	tenantID      string
	userID        string
	tempDir       string
	cleanup       func()
}

// setupACLTestEnvironment creates a test environment for ACL security testing
func setupACLTestEnvironment(t *testing.T) *aclTestEnv {
	// Create temporary directory for test data
	tempDir, err := os.MkdirTemp("", "maxiofs-acl-test-*")
	require.NoError(t, err)

	// Initialize auth manager
	authDir := filepath.Join(tempDir, "auth")
	err = os.MkdirAll(authDir, 0755)
	require.NoError(t, err)

	authConfig := config.AuthConfig{
		EnableAuth: true,
		JWTSecret:  "test-secret-key-for-testing-only-minimum-32-chars-long-string",
	}
	authManager := auth.NewManager(authConfig, authDir)
	require.NotNil(t, authManager)

	ctx := context.Background()

	// Create test tenant
	tenant := &auth.Tenant{
		ID:              "test-tenant",
		Name:            "test-tenant",
		DisplayName:     "Test Tenant",
		Status:          "active",
		MaxAccessKeys:   100,
		MaxStorageBytes: 10 * 1024 * 1024 * 1024, // 10GB
		MaxBuckets:      1000,
		CreatedAt:       time.Now().Unix(),
		UpdatedAt:       time.Now().Unix(),
	}
	err = authManager.CreateTenant(ctx, tenant)
	require.NoError(t, err)

	// Create test user
	testUser := &auth.User{
		ID:          "test-user-id",
		Username:    "testuser",
		DisplayName: "Test User",
		Email:       "test@example.com",
		Status:      "active",
		TenantID:    tenant.ID,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err = authManager.CreateUser(ctx, testUser)
	require.NoError(t, err)

	// Initialize storage backend
	storageDir := filepath.Join(tempDir, "storage")
	err = os.MkdirAll(storageDir, 0755)
	require.NoError(t, err)

	storageBackend, err := storage.NewBackend(config.StorageConfig{
		Backend: "filesystem",
		Root:    storageDir,
	})
	require.NoError(t, err)

	// Initialize metadata store
	metadataDir := filepath.Join(tempDir, "metadata")
	metadataStore, err := metadata.NewPebbleStore(metadata.PebbleOptions{		DataDir: metadataDir,
		Logger:  logrus.StandardLogger(),})
	require.NoError(t, err)

	// Create managers
	bucketManager := bucket.NewManager(storageBackend, metadataStore)
	objectManager := object.NewManager(storageBackend, metadataStore, config.StorageConfig{
		Backend: "filesystem",
		Root:    storageDir,
	})

	// Create handler
	handler := NewHandler(bucketManager, objectManager)
	handler.SetAuthManager(authManager)

	cleanup := func() {
		if metadataStore != nil {
			metadataStore.Close()
		}
		time.Sleep(100 * time.Millisecond) // Wait for cleanup
		os.RemoveAll(tempDir)
	}

	return &aclTestEnv{
		handler:       handler,
		authManager:   authManager,
		bucketManager: bucketManager,
		objectManager: objectManager,
		tenantID:      tenant.ID,
		userID:        testUser.ID,
		tempDir:       tempDir,
		cleanup:       cleanup,
	}
}

// TestCheckBucketACLPermission_DefaultACL tests bucket with default ACL (owner="maxiofs")
// SECURITY TEST: Ensures default ACL is restrictive (private to owner)
func TestCheckBucketACLPermission_DefaultACL(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create a bucket (gets default ACL with owner="maxiofs")
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, "test-bucket", env.userID)
	require.NoError(t, err, "Should create bucket")

	// Check permission for owner "maxiofs" - should have FULL_CONTROL
	hasPermission := env.handler.checkBucketACLPermission(ctx, env.tenantID, "test-bucket", "maxiofs", acl.PermissionFullControl)
	assert.True(t, hasPermission, "Owner 'maxiofs' should have FULL_CONTROL on bucket with default ACL")

	// Check permission for different user - should be denied (private ACL)
	hasPermissionOther := env.handler.checkBucketACLPermission(ctx, env.tenantID, "test-bucket", env.userID, acl.PermissionRead)
	assert.False(t, hasPermissionOther, "Non-owner should NOT have access to bucket with default private ACL")
}

// TestCheckBucketACLPermission_WithACL_UserHasPermission tests ACL grants user access
// SECURITY TEST: Ensures ACL allows authorized users
func TestCheckBucketACLPermission_WithACL_UserHasPermission(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create a bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, "test-bucket", env.userID)
	require.NoError(t, err)

	// Create ACL that grants READ permission to the user
	bucketACL := &acl.ACL{
		Owner: acl.Owner{
			ID:          env.userID,
			DisplayName: "Test User",
		},
		Grants: []acl.Grant{
			{
				Grantee: acl.Grantee{
					Type: acl.GranteeTypeCanonicalUser,
					ID:   env.userID,
				},
				Permission: acl.PermissionRead,
			},
		},
	}

	// Set bucket ACL
	err = env.bucketManager.SetBucketACL(ctx, env.tenantID, "test-bucket", bucketACL)
	require.NoError(t, err, "Should set bucket ACL")

	// Check permission - should grant access
	hasPermission := env.handler.checkBucketACLPermission(ctx, env.tenantID, "test-bucket", env.userID, acl.PermissionRead)

	assert.True(t, hasPermission, "Should grant access when user has explicit ACL permission")
}

// TestCheckBucketACLPermission_WithACL_UserNoPermission tests ACL denies unauthorized users
// SECURITY TEST: Ensures ACL blocks unauthorized users
func TestCheckBucketACLPermission_WithACL_UserNoPermission(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create second user without permissions
	otherUser := &auth.User{
		ID:          "other-user-id",
		Username:    "otheruser",
		DisplayName: "Other User",
		Email:       "other@example.com",
		Status:      "active",
		TenantID:    env.tenantID,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := env.authManager.CreateUser(ctx, otherUser)
	require.NoError(t, err)

	// Create a bucket
	err = env.bucketManager.CreateBucket(ctx, env.tenantID, "test-bucket", env.userID)
	require.NoError(t, err)

	// Create ACL that grants READ permission ONLY to env.userID (NOT otherUser)
	bucketACL := &acl.ACL{
		Owner: acl.Owner{
			ID:          env.userID,
			DisplayName: "Test User",
		},
		Grants: []acl.Grant{
			{
				Grantee: acl.Grantee{
					Type: acl.GranteeTypeCanonicalUser,
					ID:   env.userID, // Only this user
				},
				Permission: acl.PermissionRead,
			},
		},
	}

	err = env.bucketManager.SetBucketACL(ctx, env.tenantID, "test-bucket", bucketACL)
	require.NoError(t, err)

	// Check permission for the OTHER user - should deny access
	hasPermission := env.handler.checkBucketACLPermission(ctx, env.tenantID, "test-bucket", otherUser.ID, acl.PermissionRead)

	assert.False(t, hasPermission, "Should deny access when user has no explicit ACL permission")
}

// TestCheckBucketACLPermission_PublicAccess tests public access via AllUsers group
// SECURITY TEST: Ensures public ACLs work correctly
func TestCheckBucketACLPermission_PublicAccess(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create second user (unauthenticated scenario)
	publicUser := &auth.User{
		ID:          "public-user-id",
		Username:    "publicuser",
		DisplayName: "Public User",
		Email:       "public@example.com",
		Status:      "active",
		TenantID:    env.tenantID,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := env.authManager.CreateUser(ctx, publicUser)
	require.NoError(t, err)

	// Create a bucket
	err = env.bucketManager.CreateBucket(ctx, env.tenantID, "test-bucket", env.userID)
	require.NoError(t, err)

	// Create ACL with public READ access (AllUsers group)
	bucketACL := &acl.ACL{
		Owner: acl.Owner{
			ID:          env.userID,
			DisplayName: "Test User",
		},
		Grants: []acl.Grant{
			{
				Grantee: acl.Grantee{
					Type: acl.GranteeTypeGroup,
					URI:  acl.GroupAllUsers, // Public access
				},
				Permission: acl.PermissionRead,
			},
		},
	}

	err = env.bucketManager.SetBucketACL(ctx, env.tenantID, "test-bucket", bucketACL)
	require.NoError(t, err)

	// Check permission for user without explicit grant - should allow via AllUsers
	hasPermission := env.handler.checkBucketACLPermission(ctx, env.tenantID, "test-bucket", publicUser.ID, acl.PermissionRead)

	assert.True(t, hasPermission, "Should grant access via AllUsers public ACL")
}

// TestCheckBucketACLPermission_ACLManagerNotAvailable tests behavior when ACL manager is unavailable
// SECURITY TEST: Ensures proper handling when ACL subsystem fails
func TestCheckBucketACLPermission_ACLManagerNotAvailable(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create a bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, "test-bucket", env.userID)
	require.NoError(t, err)

	// NOTE: In this test environment, ACL manager IS available and initialized
	// Testing the "ACL manager not available" path would require mocking
	// For now, verify that with a properly initialized ACL manager,
	// the owner has access
	hasPermission := env.handler.checkBucketACLPermission(ctx, env.tenantID, "test-bucket", "maxiofs", acl.PermissionRead)

	assert.True(t, hasPermission, "Owner should have access when ACL manager is available")
}

// TestCheckBucketACLPermission_DifferentPermissions tests various permission types
// SECURITY TEST: Ensures different permissions are correctly enforced
func TestCheckBucketACLPermission_DifferentPermissions(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create second user (non-owner) to test permission restrictions
	otherUser := &auth.User{
		ID:          "other-user-id",
		Username:    "otheruser",
		DisplayName: "Other User",
		Email:       "other@example.com",
		Status:      "active",
		TenantID:    env.tenantID,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := env.authManager.CreateUser(ctx, otherUser)
	require.NoError(t, err)

	// Create a bucket with env.userID as owner
	err = env.bucketManager.CreateBucket(ctx, env.tenantID, "test-bucket", env.userID)
	require.NoError(t, err)

	// Create ACL that grants ONLY READ permission to otherUser (not owner)
	bucketACL := &acl.ACL{
		Owner: acl.Owner{
			ID:          env.userID,
			DisplayName: "Test User",
		},
		Grants: []acl.Grant{
			{
				Grantee: acl.Grantee{
					Type: acl.GranteeTypeCanonicalUser,
					ID:   otherUser.ID,
				},
				Permission: acl.PermissionRead,
			},
		},
	}

	err = env.bucketManager.SetBucketACL(ctx, env.tenantID, "test-bucket", bucketACL)
	require.NoError(t, err)

	// Test READ permission for non-owner - should be granted
	hasRead := env.handler.checkBucketACLPermission(ctx, env.tenantID, "test-bucket", otherUser.ID, acl.PermissionRead)
	assert.True(t, hasRead, "Should have READ permission")

	// Test WRITE permission for non-owner - should be denied (not granted in ACL)
	hasWrite := env.handler.checkBucketACLPermission(ctx, env.tenantID, "test-bucket", otherUser.ID, acl.PermissionWrite)
	assert.False(t, hasWrite, "Should NOT have WRITE permission (not granted)")

	// Test FULL_CONTROL for non-owner - should be denied
	hasFullControl := env.handler.checkBucketACLPermission(ctx, env.tenantID, "test-bucket", otherUser.ID, acl.PermissionFullControl)
	assert.False(t, hasFullControl, "Should NOT have FULL_CONTROL permission (not granted)")

	// AWS S3 BEHAVIOR: Owner ALWAYS has FULL_CONTROL regardless of ACL grants
	hasOwnerFullControl := env.handler.checkBucketACLPermission(ctx, env.tenantID, "test-bucket", env.userID, acl.PermissionFullControl)
	assert.True(t, hasOwnerFullControl, "Owner should ALWAYS have FULL_CONTROL (AWS S3 behavior)")
}

// TestCheckObjectACLPermission_WithObjectACL tests object-level ACL permissions
// SECURITY TEST: Ensures object ACL correctly grants/denies access
func TestCheckObjectACLPermission_WithObjectACL(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, "test-bucket", env.userID)
	require.NoError(t, err)

	// Create second user
	otherUser := &auth.User{
		ID:          "other-user-id",
		Username:    "otheruser",
		DisplayName: "Other User",
		Email:       "other@example.com",
		Status:      "active",
		TenantID:    env.tenantID,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err = env.authManager.CreateUser(ctx, otherUser)
	require.NoError(t, err)

	// Upload an object to the bucket
	bucketPath := env.tenantID + "/test-bucket"
	objectKey := "test-object.txt"
	objectData := strings.NewReader("test data")

	headers := make(http.Header)
	headers.Set("Content-Type", "text/plain")

	_, err = env.objectManager.PutObject(ctx, bucketPath, objectKey, objectData, headers)
	require.NoError(t, err)

	// Create object ACL that grants READ to otherUser (using object.ACL type)
	objectACL := &object.ACL{
		Owner: object.Owner{
			ID:          env.userID,
			DisplayName: "Test User",
		},
		Grants: []object.Grant{
			{
				Grantee: object.Grantee{
					Type: "CanonicalUser",
					ID:   otherUser.ID,
				},
				Permission: "READ",
			},
		},
	}

	err = env.objectManager.SetObjectACL(ctx, bucketPath, objectKey, objectACL)
	require.NoError(t, err)

	// Test: otherUser should have READ permission on object
	hasRead := env.handler.checkObjectACLPermission(ctx, bucketPath, objectKey, otherUser.ID, acl.PermissionRead)
	assert.True(t, hasRead, "User with READ grant should have READ permission on object")

	// Test: otherUser should NOT have WRITE permission
	hasWrite := env.handler.checkObjectACLPermission(ctx, bucketPath, objectKey, otherUser.ID, acl.PermissionWrite)
	assert.False(t, hasWrite, "User without WRITE grant should NOT have WRITE permission on object")

	// Test: Owner should always have FULL_CONTROL
	hasOwnerFullControl := env.handler.checkObjectACLPermission(ctx, bucketPath, objectKey, env.userID, acl.PermissionFullControl)
	assert.True(t, hasOwnerFullControl, "Object owner should always have FULL_CONTROL")
}

// TestCheckObjectACLPermission_FallbackToBucket tests fallback to bucket ACL when object has no ACL
// SECURITY TEST: Ensures proper fallback behavior
func TestCheckObjectACLPermission_FallbackToBucket(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create bucket with owner
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, "test-bucket", env.userID)
	require.NoError(t, err)

	// Create second user
	otherUser := &auth.User{
		ID:          "other-user-id",
		Username:    "otheruser",
		DisplayName: "Other User",
		Email:       "other@example.com",
		Status:      "active",
		TenantID:    env.tenantID,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err = env.authManager.CreateUser(ctx, otherUser)
	require.NoError(t, err)

	// Set bucket ACL to grant READ to otherUser
	bucketACL := &acl.ACL{
		Owner: acl.Owner{
			ID:          env.userID,
			DisplayName: "Test User",
		},
		Grants: []acl.Grant{
			{
				Grantee: acl.Grantee{
					Type: acl.GranteeTypeCanonicalUser,
					ID:   otherUser.ID,
				},
				Permission: acl.PermissionRead,
			},
		},
	}
	err = env.bucketManager.SetBucketACL(ctx, env.tenantID, "test-bucket", bucketACL)
	require.NoError(t, err)

	// Upload object WITHOUT setting object-level ACL
	bucketPath := env.tenantID + "/test-bucket"
	objectKey := "test-object-no-acl.txt"
	objectData := strings.NewReader("test data")

	headers := make(http.Header)
	headers.Set("Content-Type", "text/plain")

	_, err = env.objectManager.PutObject(ctx, bucketPath, objectKey, objectData, headers)
	require.NoError(t, err)

	// Test: otherUser should inherit READ permission from bucket ACL
	hasRead := env.handler.checkObjectACLPermission(ctx, bucketPath, objectKey, otherUser.ID, acl.PermissionRead)
	assert.True(t, hasRead, "User should inherit READ permission from bucket ACL when object has no ACL")

	// Test: otherUser should NOT have WRITE (not granted in bucket ACL)
	hasWrite := env.handler.checkObjectACLPermission(ctx, bucketPath, objectKey, otherUser.ID, acl.PermissionWrite)
	assert.False(t, hasWrite, "User should NOT have WRITE permission (not in bucket ACL)")
}

// TestCheckPublicBucketAccess_PublicReadACL tests public bucket access
// SECURITY TEST: Ensures public access is correctly granted/denied
func TestCheckPublicBucketAccess_PublicReadACL(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, "public-bucket", env.userID)
	require.NoError(t, err)

	// Set bucket ACL to public-read (AllUsers READ)
	bucketACL := &acl.ACL{
		Owner: acl.Owner{
			ID:          env.userID,
			DisplayName: "Test User",
		},
		Grants: []acl.Grant{
			{
				Grantee: acl.Grantee{
					Type: acl.GranteeTypeGroup,
					URI:  acl.GroupAllUsers,
				},
				Permission: acl.PermissionRead,
			},
		},
	}
	err = env.bucketManager.SetBucketACL(ctx, env.tenantID, "public-bucket", bucketACL)
	require.NoError(t, err)

	// Test: Public should have READ access
	hasPublicRead := env.handler.checkPublicBucketAccess(ctx, env.tenantID, "public-bucket", acl.PermissionRead)
	assert.True(t, hasPublicRead, "Bucket with AllUsers READ grant should allow public READ")

	// Test: Public should NOT have WRITE access
	hasPublicWrite := env.handler.checkPublicBucketAccess(ctx, env.tenantID, "public-bucket", acl.PermissionWrite)
	assert.False(t, hasPublicWrite, "Bucket without AllUsers WRITE grant should deny public WRITE")
}

// TestCheckPublicBucketAccess_PrivateACL tests private bucket denies public access
// SECURITY TEST: Ensures private buckets are actually private
func TestCheckPublicBucketAccess_PrivateACL(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create bucket with private ACL (default)
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, "private-bucket", env.userID)
	require.NoError(t, err)

	// The bucket now has default private ACL with owner=env.userID
	// Test: Public should NOT have READ access
	hasPublicRead := env.handler.checkPublicBucketAccess(ctx, env.tenantID, "private-bucket", acl.PermissionRead)
	assert.False(t, hasPublicRead, "Private bucket should deny public READ access")

	// Test: Public should NOT have WRITE access
	hasPublicWrite := env.handler.checkPublicBucketAccess(ctx, env.tenantID, "private-bucket", acl.PermissionWrite)
	assert.False(t, hasPublicWrite, "Private bucket should deny public WRITE access")
}
