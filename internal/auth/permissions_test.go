package auth

import (
	"strings"
	"testing"
	"time"
)

// setupTestStore creates a test SQLite store
func setupTestStore(t *testing.T) (*SQLiteStore, string) {
	manager, tmpDir := setupTestAuthManager(t)

	// Type assert to get the internal store
	authMgr, ok := manager.(*authManager)
	if !ok {
		t.Fatal("Failed to type assert Manager to *authManager")
	}

	return authMgr.store, tmpDir
}

// TestGrantBucketAccess tests granting bucket access
func TestGrantBucketAccess(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Create a test user first
	testUser := &User{
		ID:          "test-user-1",
		Username:    "testuser",
		Password:    "TestPassword123!",
		DisplayName: "Test User",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := store.CreateUser(testUser)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	tests := []struct {
		name            string
		bucketName      string
		userID          string
		tenantID        string
		permissionLevel string
		grantedBy       string
		expiresAt       int64
		wantErr         bool
		errContains     string
	}{
		{
			name:            "Grant read access to user",
			bucketName:      "test-bucket-1",
			userID:          "test-user-1",
			tenantID:        "",
			permissionLevel: PermissionLevelRead,
			grantedBy:       "admin",
			expiresAt:       0,
			wantErr:         false,
		},
		{
			name:            "Grant write access to user",
			bucketName:      "test-bucket-2",
			userID:          "test-user-1",
			tenantID:        "",
			permissionLevel: PermissionLevelWrite,
			grantedBy:       "admin",
			expiresAt:       0,
			wantErr:         false,
		},
		{
			name:            "Grant admin access to user",
			bucketName:      "test-bucket-3",
			userID:          "test-user-1",
			tenantID:        "",
			permissionLevel: PermissionLevelAdmin,
			grantedBy:       "admin",
			expiresAt:       0,
			wantErr:         false,
		},
		{
			name:            "Grant access with expiration",
			bucketName:      "test-bucket-4",
			userID:          "test-user-1",
			tenantID:        "",
			permissionLevel: PermissionLevelRead,
			grantedBy:       "admin",
			expiresAt:       time.Now().Add(24 * time.Hour).Unix(),
			wantErr:         false,
		},
		// Skipping tenant test - would need to create tenant first
		// which requires more complex setup
		{
			name:            "Invalid permission level",
			bucketName:      "test-bucket-6",
			userID:          "test-user-1",
			tenantID:        "",
			permissionLevel: "invalid",
			grantedBy:       "admin",
			expiresAt:       0,
			wantErr:         true,
			errContains:     "invalid permission level",
		},
		{
			name:            "No userID or tenantID",
			bucketName:      "test-bucket-7",
			userID:          "",
			tenantID:        "",
			permissionLevel: PermissionLevelRead,
			grantedBy:       "admin",
			expiresAt:       0,
			wantErr:         true,
			errContains:     "must specify either userID or tenantID",
		},
		{
			name:            "Both userID and tenantID",
			bucketName:      "test-bucket-8",
			userID:          "test-user-1",
			tenantID:        "tenant-1",
			permissionLevel: PermissionLevelRead,
			grantedBy:       "admin",
			expiresAt:       0,
			wantErr:         true,
			errContains:     "must specify either userID or tenantID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.GrantBucketAccess(
				tt.bucketName,
				tt.userID,
				tt.tenantID,
				tt.permissionLevel,
				tt.grantedBy,
				tt.expiresAt,
			)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// TestGrantBucketAccess_UpdateExisting tests updating existing permissions
func TestGrantBucketAccess_UpdateExisting(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Create test user first
	testUser := &User{
		ID:          "update-user",
		Username:    "updateuser",
		Password:    "TestPassword123!",
		DisplayName: "Update User",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := store.CreateUser(testUser)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	bucketName := "update-test-bucket"
	userID := testUser.ID

	// Grant initial read access
	err = store.GrantBucketAccess(bucketName, userID, "", PermissionLevelRead, "admin", 0)
	if err != nil {
		t.Fatalf("Failed to grant initial access: %v", err)
	}

	// Update to write access
	err = store.GrantBucketAccess(bucketName, userID, "", PermissionLevelWrite, "admin", 0)
	if err != nil {
		t.Fatalf("Failed to update access: %v", err)
	}

	// Verify the permission was updated
	hasAccess, level, err := store.CheckBucketAccess(bucketName, userID)
	if err != nil {
		t.Fatalf("Failed to check access: %v", err)
	}

	if !hasAccess {
		t.Error("User should have access")
	}

	if level != PermissionLevelWrite {
		t.Errorf("Expected permission level %s, got %s", PermissionLevelWrite, level)
	}
}

// TestRevokeBucketAccess tests revoking bucket access
func TestRevokeBucketAccess(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Create test user first
	testUser := &User{
		ID:          "revoke-user",
		Username:    "revokeuser",
		Password:    "TestPassword123!",
		DisplayName: "Revoke User",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := store.CreateUser(testUser)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	bucketName := "revoke-test-bucket"
	userID := testUser.ID

	// Grant access first
	err = store.GrantBucketAccess(bucketName, userID, "", PermissionLevelRead, "admin", 0)
	if err != nil {
		t.Fatalf("Failed to grant access: %v", err)
	}

	// Verify access was granted
	hasAccess, _, err := store.CheckBucketAccess(bucketName, userID)
	if err != nil {
		t.Fatalf("Failed to check access: %v", err)
	}
	if !hasAccess {
		t.Error("User should have access before revoke")
	}

	// Revoke access
	err = store.RevokeBucketAccess(bucketName, userID, "")
	if err != nil {
		t.Fatalf("Failed to revoke access: %v", err)
	}

	// Verify access was revoked
	hasAccess, _, err = store.CheckBucketAccess(bucketName, userID)
	if err != nil {
		t.Fatalf("Failed to check access after revoke: %v", err)
	}
	if hasAccess {
		t.Error("User should not have access after revoke")
	}
}

// TestRevokeBucketAccess_Tenant is skipped because it would require
// creating a tenant first, which needs tenant management setup
// The tenant revoke functionality is tested via integration tests

// TestRevokeBucketAccess_NonExistent tests revoking non-existent permission
func TestRevokeBucketAccess_NonExistent(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Revoke non-existent permission should not error
	err := store.RevokeBucketAccess("nonexistent-bucket", "nonexistent-user", "")
	if err != nil {
		t.Errorf("Revoking non-existent permission should not error: %v", err)
	}
}

// TestCheckBucketAccess tests checking bucket access
func TestCheckBucketAccess(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Create test user without tenant (tenant tests would require more setup)
	testUser := &User{
		ID:          "check-user",
		Username:    "checkuser",
		Password:    "TestPassword123!",
		DisplayName: "Check User",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := store.CreateUser(testUser)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	t.Run("User permission", func(t *testing.T) {
		bucketName := "user-perm-bucket"

		// Grant user permission
		err := store.GrantBucketAccess(bucketName, testUser.ID, "", PermissionLevelWrite, "admin", 0)
		if err != nil {
			t.Fatalf("Failed to grant access: %v", err)
		}

		// Check access
		hasAccess, level, err := store.CheckBucketAccess(bucketName, testUser.ID)
		if err != nil {
			t.Fatalf("Failed to check access: %v", err)
		}

		if !hasAccess {
			t.Error("User should have access")
		}

		if level != PermissionLevelWrite {
			t.Errorf("Expected level %s, got %s", PermissionLevelWrite, level)
		}
	})

	// Tenant permission test skipped - would require creating tenant first

	t.Run("No permission", func(t *testing.T) {
		hasAccess, _, err := store.CheckBucketAccess("no-access-bucket", testUser.ID)
		if err != nil {
			t.Fatalf("Failed to check access: %v", err)
		}

		if hasAccess {
			t.Error("User should not have access")
		}
	})

	t.Run("Expired permission", func(t *testing.T) {
		bucketName := "expired-bucket"

		// Grant access that expired 1 hour ago
		expiredTime := time.Now().Add(-1 * time.Hour).Unix()
		err := store.GrantBucketAccess(bucketName, testUser.ID, "", PermissionLevelRead, "admin", expiredTime)
		if err != nil {
			t.Fatalf("Failed to grant expired access: %v", err)
		}

		// Check access - should be denied due to expiration
		hasAccess, _, err := store.CheckBucketAccess(bucketName, testUser.ID)
		if err != nil {
			t.Fatalf("Failed to check expired access: %v", err)
		}

		if hasAccess {
			t.Error("User should not have access to expired permission")
		}
	})

	t.Run("Future expiration", func(t *testing.T) {
		bucketName := "future-exp-bucket"

		// Grant access that expires in 1 hour
		futureTime := time.Now().Add(1 * time.Hour).Unix()
		err := store.GrantBucketAccess(bucketName, testUser.ID, "", PermissionLevelRead, "admin", futureTime)
		if err != nil {
			t.Fatalf("Failed to grant future expiring access: %v", err)
		}

		// Check access - should be allowed
		hasAccess, _, err := store.CheckBucketAccess(bucketName, testUser.ID)
		if err != nil {
			t.Fatalf("Failed to check future expiring access: %v", err)
		}

		if !hasAccess {
			t.Error("User should have access before expiration")
		}
	})
}

// TestListBucketPermissions tests listing permissions for a bucket
func TestListBucketPermissions(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Create test users first
	user1 := &User{
		ID:          "user-1",
		Username:    "listuser1",
		Password:    "TestPassword123!",
		DisplayName: "List User 1",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := store.CreateUser(user1)
	if err != nil {
		t.Fatalf("Failed to create user 1: %v", err)
	}

	user2 := &User{
		ID:          "user-2",
		Username:    "listuser2",
		Password:    "TestPassword123!",
		DisplayName: "List User 2",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err = store.CreateUser(user2)
	if err != nil {
		t.Fatalf("Failed to create user 2: %v", err)
	}

	bucketName := "list-test-bucket"

	// Grant multiple permissions
	err = store.GrantBucketAccess(bucketName, "user-1", "", PermissionLevelRead, "admin", 0)
	if err != nil {
		t.Fatalf("Failed to grant access 1: %v", err)
	}

	err = store.GrantBucketAccess(bucketName, "user-2", "", PermissionLevelWrite, "admin", 0)
	if err != nil {
		t.Fatalf("Failed to grant access 2: %v", err)
	}

	// List permissions
	permissions, err := store.ListBucketPermissions(bucketName)
	if err != nil {
		t.Fatalf("Failed to list permissions: %v", err)
	}

	if len(permissions) != 2 {
		t.Errorf("Expected 2 permissions, got %d", len(permissions))
	}

	// Verify permissions are returned
	foundUser1 := false
	foundUser2 := false

	for _, perm := range permissions {
		if perm.UserID == "user-1" && perm.PermissionLevel == PermissionLevelRead {
			foundUser1 = true
		}
		if perm.UserID == "user-2" && perm.PermissionLevel == PermissionLevelWrite {
			foundUser2 = true
		}
	}

	if !foundUser1 {
		t.Error("User 1 permission not found")
	}
	if !foundUser2 {
		t.Error("User 2 permission not found")
	}
}

// TestListBucketPermissions_Empty tests listing permissions for bucket with no permissions
func TestListBucketPermissions_Empty(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	permissions, err := store.ListBucketPermissions("empty-bucket")
	if err != nil {
		t.Fatalf("Failed to list permissions: %v", err)
	}

	if len(permissions) != 0 {
		t.Errorf("Expected 0 permissions, got %d", len(permissions))
	}
}

// TestListUserBucketPermissions tests listing all bucket permissions for a user
func TestListUserBucketPermissions(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Create test user without tenant
	testUser := &User{
		ID:          "list-user",
		Username:    "listuser",
		Password:    "TestPassword123!",
		DisplayName: "List User",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := store.CreateUser(testUser)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Grant direct user permissions
	err = store.GrantBucketAccess("bucket-1", testUser.ID, "", PermissionLevelRead, "admin", 0)
	if err != nil {
		t.Fatalf("Failed to grant bucket-1 access: %v", err)
	}

	err = store.GrantBucketAccess("bucket-2", testUser.ID, "", PermissionLevelWrite, "admin", 0)
	if err != nil {
		t.Fatalf("Failed to grant bucket-2 access: %v", err)
	}

	// List user permissions
	permissions, err := store.ListUserBucketPermissions(testUser.ID)
	if err != nil {
		t.Fatalf("Failed to list user permissions: %v", err)
	}

	// Should include both user permissions
	if len(permissions) != 2 {
		t.Errorf("Expected 2 permissions, got %d", len(permissions))
	}
}

// TestListUserBucketPermissions_NoTenant tests listing permissions for user without tenant
func TestListUserBucketPermissions_NoTenant(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Create test user without tenant
	testUser := &User{
		ID:          "no-tenant-user",
		Username:    "notenantuser",
		Password:    "TestPassword123!",
		DisplayName: "No Tenant User",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := store.CreateUser(testUser)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Grant permission
	err = store.GrantBucketAccess("solo-bucket", testUser.ID, "", PermissionLevelRead, "admin", 0)
	if err != nil {
		t.Fatalf("Failed to grant access: %v", err)
	}

	// List permissions
	permissions, err := store.ListUserBucketPermissions(testUser.ID)
	if err != nil {
		t.Fatalf("Failed to list permissions: %v", err)
	}

	if len(permissions) != 1 {
		t.Errorf("Expected 1 permission, got %d", len(permissions))
	}
}

// TestListUserBucketPermissions_NonExistent tests listing permissions for non-existent user
func TestListUserBucketPermissions_NonExistent(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	permissions, err := store.ListUserBucketPermissions("nonexistent-user")
	// Should not error, just return empty list or handle gracefully
	if err != nil {
		t.Logf("Got error for non-existent user (acceptable): %v", err)
	}

	if permissions == nil {
		permissions = []*BucketPermission{}
	}

	if len(permissions) != 0 {
		t.Errorf("Expected 0 permissions for non-existent user, got %d", len(permissions))
	}
}

// TestGeneratePermissionID tests permission ID generation
func TestGeneratePermissionID(t *testing.T) {
	// Generate multiple IDs
	ids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id := GeneratePermissionID()

		// Check format
		if !strings.HasPrefix(id, "perm-") {
			t.Errorf("Permission ID should start with 'perm-', got %s", id)
		}

		// Check uniqueness
		if ids[id] {
			t.Errorf("Duplicate permission ID generated: %s", id)
		}
		ids[id] = true

		// Check length (should be "perm-" + 32 hex chars)
		expectedLength := len("perm-") + 32
		if len(id) != expectedLength {
			t.Errorf("Expected ID length %d, got %d", expectedLength, len(id))
		}
	}
}

// TestNullInt64 tests the nullInt64 helper function
func TestNullInt64(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		wantNil  bool
	}{
		{
			name:    "Zero value",
			input:   0,
			wantNil: true,
		},
		{
			name:    "Positive value",
			input:   123,
			wantNil: false,
		},
		{
			name:    "Negative value",
			input:   -456,
			wantNil: false,
		},
		{
			name:    "Large value",
			input:   time.Now().Unix(),
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nullInt64(tt.input)

			if tt.wantNil {
				if result != nil {
					t.Errorf("Expected nil for input %d, got %v", tt.input, result)
				}
			} else {
				if result == nil {
					t.Errorf("Expected non-nil for input %d", tt.input)
				} else if result.(int64) != tt.input {
					t.Errorf("Expected %d, got %v", tt.input, result)
				}
			}
		})
	}
}

// TestPermissionLevelConstants tests that permission level constants are defined
func TestPermissionLevelConstants(t *testing.T) {
	if PermissionLevelRead != "read" {
		t.Errorf("Expected PermissionLevelRead to be 'read', got %s", PermissionLevelRead)
	}

	if PermissionLevelWrite != "write" {
		t.Errorf("Expected PermissionLevelWrite to be 'write', got %s", PermissionLevelWrite)
	}

	if PermissionLevelAdmin != "admin" {
		t.Errorf("Expected PermissionLevelAdmin to be 'admin', got %s", PermissionLevelAdmin)
	}
}
