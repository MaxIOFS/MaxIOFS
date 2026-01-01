package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"
)

// generateTestID generates a unique ID for testing
func generateTestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ========================================
// Tests for Tenant Management Functions
// ========================================

func TestCreateTenant(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	tests := []struct {
		name    string
		tenant  *Tenant
		wantErr bool
	}{
		{
			name: "Create valid tenant",
			tenant: &Tenant{
				ID:          generateTestID(),
				Name:        "test-tenant-create",
				DisplayName: "Test Tenant",
				Status:      "active",
				CreatedAt:   time.Now().Unix(),
				UpdatedAt:   time.Now().Unix(),
			},
			wantErr: false,
		},
		{
			name: "Create tenant with quotas",
			tenant: &Tenant{
				ID:               generateTestID(),
				Name:             "quota-tenant-create",
				DisplayName:      "Quota Tenant",
				Status:           "active",
				MaxStorageBytes:  1099511627776, // 1TB
				MaxBuckets:       100,
				MaxAccessKeys:    10,
				CreatedAt:        time.Now().Unix(),
				UpdatedAt:        time.Now().Unix(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.CreateTenant(ctx, tt.tenant)

			if tt.wantErr {
				if err == nil {
					t.Error("CreateTenant() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("CreateTenant() unexpected error: %v", err)
				return
			}

			if tt.tenant.ID == "" {
				t.Error("CreateTenant() tenant ID should be set")
			}
		})
	}
}

func TestGetTenant(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test tenant
	tenant := &Tenant{
		ID:          generateTestID(),
		Name:        "test-tenant-get",
		DisplayName: "Test Tenant",
		Status:      "active",
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := manager.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	tests := []struct {
		name     string
		tenantID string
		wantErr  bool
	}{
		{
			name:     "Get existing tenant",
			tenantID: tenant.ID,
			wantErr:  false,
		},
		{
			name:     "Get non-existent tenant",
			tenantID: "nonexistent-id",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := manager.GetTenant(ctx, tt.tenantID)

			if tt.wantErr {
				if err == nil {
					t.Error("GetTenant() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("GetTenant() unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Error("GetTenant() returned nil tenant")
				return
			}

			if result.ID != tt.tenantID {
				t.Errorf("GetTenant() returned tenant ID = %s, want %s", result.ID, tt.tenantID)
			}
		})
	}
}

func TestGetTenantByName(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test tenant
	tenant := &Tenant{
		ID:          generateTestID(),
		Name:        "unique-tenant-byname",
		DisplayName: "Unique Tenant",
		Status:      "active",
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := manager.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	tests := []struct {
		name       string
		tenantName string
		wantErr    bool
	}{
		{
			name:       "Get existing tenant by name",
			tenantName: "unique-tenant-byname",
			wantErr:    false,
		},
		{
			name:       "Get non-existent tenant by name",
			tenantName: "nonexistent-tenant",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := manager.GetTenantByName(ctx, tt.tenantName)

			if tt.wantErr {
				if err == nil {
					t.Error("GetTenantByName() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("GetTenantByName() unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Error("GetTenantByName() returned nil tenant")
				return
			}

			if result.Name != tt.tenantName {
				t.Errorf("GetTenantByName() returned tenant name = %s, want %s", result.Name, tt.tenantName)
			}
		})
	}
}

func TestListTenants(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create multiple test tenants
	tenants := []*Tenant{
		{ID: generateTestID(), Name: "tenant1-list", DisplayName: "Tenant 1", Status: "active", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()},
		{ID: generateTestID(), Name: "tenant2-list", DisplayName: "Tenant 2", Status: "active", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()},
		{ID: generateTestID(), Name: "tenant3-list", DisplayName: "Tenant 3", Status: "inactive", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()},
	}

	for _, tenant := range tenants {
		err := manager.CreateTenant(ctx, tenant)
		if err != nil {
			t.Fatalf("Failed to create tenant: %v", err)
		}
	}

	result, err := manager.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants() unexpected error: %v", err)
	}

	if len(result) < 3 {
		t.Errorf("ListTenants() returned %d tenants, want at least 3", len(result))
	}
}

func TestUpdateTenant(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test tenant
	tenant := &Tenant{
		ID:          generateTestID(),
		Name:        "update-tenant-test",
		DisplayName: "Original Display Name",
		Status:      "active",
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := manager.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// Update the tenant
	tenant.DisplayName = "Updated Display Name"
	tenant.MaxBuckets = 50

	err = manager.UpdateTenant(ctx, tenant)
	if err != nil {
		t.Errorf("UpdateTenant() unexpected error: %v", err)
	}

	// Verify the update
	updated, err := manager.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Failed to get updated tenant: %v", err)
	}

	if updated.DisplayName != "Updated Display Name" {
		t.Errorf("UpdateTenant() DisplayName = %s, want %s", updated.DisplayName, "Updated Display Name")
	}

	if updated.MaxBuckets != 50 {
		t.Errorf("UpdateTenant() MaxBuckets = %d, want %d", updated.MaxBuckets, 50)
	}
}

func TestDeleteTenant(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test tenant
	tenant := &Tenant{
		ID:          generateTestID(),
		Name:        "delete-tenant-test",
		DisplayName: "Delete Tenant",
		Status:      "active",
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := manager.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// Delete the tenant
	err = manager.DeleteTenant(ctx, tenant.ID)
	if err != nil {
		t.Errorf("DeleteTenant() unexpected error: %v", err)
	}

	// Verify the tenant is deleted
	_, err = manager.GetTenant(ctx, tenant.ID)
	if err == nil {
		t.Error("DeleteTenant() tenant should not exist after deletion")
	}
}

func TestListTenantUsers(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test tenant
	tenant := &Tenant{
		ID:          generateTestID(),
		Name:        "user-tenant-list",
		DisplayName: "User Tenant",
		Status:      "active",
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := manager.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// Create users for this tenant
	users := []*User{
		{ID: generateTestID(), Username: "tenant-user1-list", Email: "user1@tenant-list.com", Password: "pass123", TenantID: tenant.ID},
		{ID: generateTestID(), Username: "tenant-user2-list", Email: "user2@tenant-list.com", Password: "pass123", TenantID: tenant.ID},
	}

	for _, user := range users {
		err := manager.CreateUser(ctx, user)
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}
	}

	result, err := manager.ListTenantUsers(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListTenantUsers() unexpected error: %v", err)
	}

	if len(result) < 2 {
		t.Errorf("ListTenantUsers() returned %d users, want at least 2", len(result))
	}
}

// ========================================
// Tests for Tenant Quota Functions
// ========================================

func TestIncrementTenantBucketCount(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test tenant
	tenant := &Tenant{
		ID:          generateTestID(),
		Name:        "bucket-count-tenant-test",
		DisplayName: "Bucket Count Tenant",
		Status:      "active",
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := manager.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// Increment bucket count
	err = manager.IncrementTenantBucketCount(ctx, tenant.ID)
	if err != nil {
		t.Errorf("IncrementTenantBucketCount() unexpected error: %v", err)
	}

	// Verify the increment
	updated, err := manager.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Failed to get tenant: %v", err)
	}

	if updated.CurrentBuckets != 1 {
		t.Errorf("IncrementTenantBucketCount() CurrentBuckets = %d, want %d", updated.CurrentBuckets, 1)
	}
}

func TestDecrementTenantBucketCount(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test tenant
	tenant := &Tenant{
		ID:          generateTestID(),
		Name:        "bucket-decrement-tenant-test",
		DisplayName: "Bucket Decrement Tenant",
		Status:      "active",
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := manager.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// Increment and then decrement
	err = manager.IncrementTenantBucketCount(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Failed to increment: %v", err)
	}

	err = manager.DecrementTenantBucketCount(ctx, tenant.ID)
	if err != nil {
		t.Errorf("DecrementTenantBucketCount() unexpected error: %v", err)
	}

	// Verify the decrement
	updated, err := manager.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Failed to get tenant: %v", err)
	}

	if updated.CurrentBuckets != 0 {
		t.Errorf("DecrementTenantBucketCount() CurrentBuckets = %d, want %d", updated.CurrentBuckets, 0)
	}
}

func TestIncrementTenantStorage(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test tenant
	tenant := &Tenant{
		ID:          generateTestID(),
		Name:        "storage-tenant-inc-test",
		DisplayName: "Storage Tenant",
		Status:      "active",
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := manager.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// Increment storage
	storageBytes := int64(1024 * 1024) // 1MB
	err = manager.IncrementTenantStorage(ctx, tenant.ID, storageBytes)
	if err != nil {
		t.Errorf("IncrementTenantStorage() unexpected error: %v", err)
	}

	// Verify the increment
	updated, err := manager.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Failed to get tenant: %v", err)
	}

	if updated.CurrentStorageBytes != storageBytes {
		t.Errorf("IncrementTenantStorage() CurrentStorageBytes = %d, want %d", updated.CurrentStorageBytes, storageBytes)
	}
}

func TestDecrementTenantStorage(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test tenant
	tenant := &Tenant{
		ID:          generateTestID(),
		Name:        "storage-decrement-tenant-test",
		DisplayName: "Storage Decrement Tenant",
		Status:      "active",
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err := manager.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	storageBytes := int64(1024 * 1024) // 1MB

	// Increment and then decrement
	err = manager.IncrementTenantStorage(ctx, tenant.ID, storageBytes)
	if err != nil {
		t.Fatalf("Failed to increment: %v", err)
	}

	err = manager.DecrementTenantStorage(ctx, tenant.ID, storageBytes)
	if err != nil {
		t.Errorf("DecrementTenantStorage() unexpected error: %v", err)
	}

	// Verify the decrement
	updated, err := manager.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Failed to get tenant: %v", err)
	}

	if updated.CurrentStorageBytes != 0 {
		t.Errorf("DecrementTenantStorage() CurrentStorageBytes = %d, want %d", updated.CurrentStorageBytes, 0)
	}
}

func TestCheckTenantStorageQuota(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	tests := []struct {
		name            string
		maxStorage      int64
		currentStorage  int64
		additionalBytes int64
		wantErr         bool
	}{
		{
			name:            "Within quota",
			maxStorage:      1024 * 1024 * 10, // 10MB
			currentStorage:  1024 * 1024,      // 1MB
			additionalBytes: 1024 * 1024,      // 1MB
			wantErr:         false,
		},
		{
			name:            "Exceeds quota",
			maxStorage:      1024 * 1024 * 10, // 10MB
			currentStorage:  1024 * 1024 * 9,  // 9MB
			additionalBytes: 1024 * 1024 * 2,  // 2MB - would exceed
			wantErr:         true,
		},
		{
			name:            "No quota limit",
			maxStorage:      0,                 // No limit
			currentStorage:  1024 * 1024 * 100, // 100MB
			additionalBytes: 1024 * 1024 * 100, // 100MB more
			wantErr:         false,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test tenant with specific quotas and unique name
			tenant := &Tenant{
				ID:                  generateTestID(),
				Name:                generateTestID(), // Use UUID to ensure uniqueness
				DisplayName:         tt.name,
				Status:              "active",
				MaxStorageBytes:     tt.maxStorage,
				CurrentStorageBytes: tt.currentStorage,
				CreatedAt:           time.Now().Unix() + int64(i), // Unique timestamp
				UpdatedAt:           time.Now().Unix() + int64(i),
			}
			err := manager.CreateTenant(ctx, tenant)
			if err != nil {
				t.Fatalf("Failed to create tenant: %v", err)
			}

			err = manager.CheckTenantStorageQuota(ctx, tenant.ID, tt.additionalBytes)

			if tt.wantErr {
				if err == nil {
					t.Error("CheckTenantStorageQuota() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("CheckTenantStorageQuota() unexpected error: %v", err)
			}
		})
	}
}

// ========================================
// Tests for Setter Functions
// ========================================
// Note: Bucket permission tests (GrantBucketAccess, RevokeBucketAccess, etc.)
// are in permissions_test.go

func TestSetAuditManager(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access SetAuditManager
	am := manager.(*authManager)

	// SetAuditManager accepts nil (it's just setting a field)
	am.SetAuditManager(nil)

	// Verify the field is set (should be nil)
	if am.auditManager != nil {
		t.Error("SetAuditManager() auditManager should be nil")
	}
}

func TestSetUserLockedCallback(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access SetUserLockedCallback
	am := manager.(*authManager)

	called := false
	callback := func(u *User) {
		called = true
	}

	am.SetUserLockedCallback(callback)

	// Verify the callback is set by calling it
	if am.userLockedCallback != nil {
		am.userLockedCallback(&User{})
		if !called {
			t.Error("SetUserLockedCallback() callback was not called")
		}
	}
}

func TestSetSettingsManager(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access SetSettingsManager
	am := manager.(*authManager)

	// SetSettingsManager accepts nil
	am.SetSettingsManager(nil)

	// Verify the field is set (should be nil)
	if am.settingsManager != nil {
		t.Error("SetSettingsManager() settingsManager should be nil")
	}
}

// ========================================
// Tests for User Management Functions
// ========================================

func TestLockAccount(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test user
	user := &User{
		ID:       generateTestID(),
		Username: "lockuser-test",
		Email:    "lockuser-test@example.com",
		Password: "password123",
		Roles:    []string{"user"},
	}
	err := manager.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Lock the account
	err = manager.LockAccount(ctx, user.ID)
	if err != nil {
		t.Errorf("LockAccount() unexpected error: %v", err)
	}

	// Note: GetUserByID doesn't currently retrieve the locked_until field from the database,
	// so we can't verify it was set by reading the user back. This test just verifies
	// that LockAccount doesn't error.
}

func TestRecordSuccessfulLogin(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test user
	user := &User{
		ID:       generateTestID(),
		Username: "successuser-test",
		Email:    "success-test@example.com",
		Password: "password123",
		Roles:    []string{"user"},
	}
	err := manager.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Record successful login
	err = manager.RecordSuccessfulLogin(ctx, user.ID)
	if err != nil {
		t.Errorf("RecordSuccessfulLogin() unexpected error: %v", err)
	}

	// Verify failed attempts are reset (we can't directly check this without accessing the store,
	// but at least we verify the function doesn't error)
}
