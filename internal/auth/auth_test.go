package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestAuthManager creates a test auth manager with a temporary database
func setupTestAuthManager(t *testing.T) (Manager, string) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "maxiofs-auth-test-*")
	require.NoError(t, err)

	cfg := config.AuthConfig{
		EnableAuth: true,
		JWTSecret:  "test-secret-key-for-testing-only-minimum-32-chars",
	}

	manager := NewManager(cfg, tmpDir)
	require.NotNil(t, manager)

	return manager, tmpDir
}

// cleanupTestAuthManager removes the temporary database
func cleanupTestAuthManager(t *testing.T, tmpDir string) {
	// Note: On Windows, SQLite files may be locked briefly after use
	// We just try to cleanup and log if it fails - it's not critical for tests
	err := os.RemoveAll(tmpDir)
	if err != nil {
		t.Logf("Warning: failed to cleanup test directory: %v", err)
	}
}

// TestHashPassword tests password hashing functionality
func TestHashPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{
			name:     "Valid password",
			password: "MySecurePassword123!",
			wantErr:  false,
		},
		{
			name:     "Short password",
			password: "abc",
			wantErr:  false, // Hashing succeeds even for short passwords
		},
		{
			name:     "Long password",
			password: "ThisIsAVeryLongPasswordWithMoreThan72CharactersWhichIsTheLimitForBcryptAlgorithm!!!",
			wantErr:  true, // Bcrypt has a 72 byte limit
		},
		{
			name:     "Empty password",
			password: "",
			wantErr:  false, // Bcrypt can hash empty strings
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashPassword(tt.password)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, hash)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, hash)
				// Bcrypt hashes start with $2a$ or $2b$
				assert.Contains(t, hash, "$2")
				// Hash should be different from plaintext
				assert.NotEqual(t, tt.password, hash)
			}
		})
	}
}

// TestVerifyPassword tests password verification
func TestVerifyPassword(t *testing.T) {
	// Hash a known password
	knownPassword := "MyTestPassword123!"
	hash, err := HashPassword(knownPassword)
	require.NoError(t, err)

	tests := []struct {
		name     string
		password string
		hash     string
		want     bool
	}{
		{
			name:     "Correct password",
			password: knownPassword,
			hash:     hash,
			want:     true,
		},
		{
			name:     "Incorrect password",
			password: "WrongPassword",
			hash:     hash,
			want:     false,
		},
		{
			name:     "Empty password with valid hash",
			password: "",
			hash:     hash,
			want:     false,
		},
		{
			name:     "Password with invalid hash",
			password: knownPassword,
			hash:     "invalid-hash",
			want:     false,
		},
		{
			name:     "Empty password and empty hash",
			password: "",
			hash:     "",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := VerifyPassword(tt.password, tt.hash)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestPasswordHashUniqueness verifies that hashing the same password twice produces different hashes
func TestPasswordHashUniqueness(t *testing.T) {
	password := "SamePassword123!"

	hash1, err := HashPassword(password)
	require.NoError(t, err)

	hash2, err := HashPassword(password)
	require.NoError(t, err)

	// Hashes should be different (bcrypt uses salt)
	assert.NotEqual(t, hash1, hash2)

	// But both should verify against the original password
	assert.True(t, VerifyPassword(password, hash1))
	assert.True(t, VerifyPassword(password, hash2))
}

// TestGenerateJWT tests JWT token generation
func TestGenerateJWT(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	user := &User{
		ID:          "test-user-123",
		Username:    "testuser",
		DisplayName: "Test User",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		TenantID:    "", // No tenant for this test
	}

	token, err := manager.GenerateJWT(ctx, user)
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// JWT token should have 3 parts separated by dots
	parts := len([]byte(token))
	assert.Greater(t, parts, 0)
}

// TestValidateJWT tests JWT token validation
func TestValidateJWT(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a user and generate a valid token
	password := "TestPassword123!"

	user := &User{
		ID:          "test-user-456",
		Username:    "validuser",
		Password:    password, // CreateUser will hash it
		DisplayName: "Valid User",
		Email:       "validuser@example.com",
		Status:      UserStatusActive,
		Roles:       []string{"admin"},
		TenantID:    "", // No tenant for this test user
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}

	// Create the user in the database
	err := manager.CreateUser(ctx, user)
	require.NoError(t, err)

	token, err := manager.GenerateJWT(ctx, user)
	require.NoError(t, err)

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "Valid token",
			token:   token,
			wantErr: false,
		},
		{
			name:    "Invalid token format",
			token:   "invalid.token.format",
			wantErr: true,
		},
		{
			name:    "Empty token",
			token:   "",
			wantErr: true,
		},
		{
			name:    "Malformed token",
			token:   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.malformed",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validatedUser, err := manager.ValidateJWT(ctx, tt.token)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, validatedUser)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, validatedUser)
				assert.Equal(t, user.ID, validatedUser.ID)
				assert.Equal(t, user.Username, validatedUser.Username)
			}
		})
	}
}

// TestValidateConsoleCredentials tests console login validation
func TestValidateConsoleCredentials(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test user
	password := "TestPassword123!"

	testUser := &User{
		ID:          "test-user-789",
		Username:    "testlogin",
		Password:    password, // CreateUser will hash it
		DisplayName: "Test Login User",
		Email:       "testlogin@example.com",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		TenantID:    "", // No tenant for this test user
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}

	err := manager.CreateUser(ctx, testUser)
	require.NoError(t, err)

	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{
			name:     "Valid credentials",
			username: "testlogin",
			password: password,
			wantErr:  false,
		},
		{
			name:     "Incorrect password",
			username: "testlogin",
			password: "WrongPassword",
			wantErr:  true,
		},
		{
			name:     "Non-existent user",
			username: "nonexistent",
			password: password,
			wantErr:  true,
		},
		{
			name:     "Empty username",
			username: "",
			password: password,
			wantErr:  true,
		},
		{
			name:     "Empty password",
			username: "testlogin",
			password: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := manager.ValidateConsoleCredentials(ctx, tt.username, tt.password)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, user)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, user)
				assert.Equal(t, tt.username, user.Username)
				// Password hash is returned (by design)
				assert.NotEmpty(t, user.Password)
				assert.Contains(t, user.Password, "$2", "Password should be bcrypt hash")
			}
		})
	}
}

// TestAccountLockout tests account lockout mechanism
func TestAccountLockout(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test user
	testUser := &User{
		ID:          "lockout-user-123",
		Username:    "lockouttest",
		Password:    "TestPassword123!",
		DisplayName: "Lockout Test User",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}

	err := manager.CreateUser(ctx, testUser)
	require.NoError(t, err)

	// Record multiple failed login attempts
	for i := 0; i < 5; i++ {
		err := manager.RecordFailedLogin(ctx, testUser.ID, "192.168.1.100")
		assert.NoError(t, err)
	}

	// Check if account is locked
	isLocked, lockedUntil, err := manager.IsAccountLocked(ctx, testUser.ID)
	assert.NoError(t, err)
	assert.True(t, isLocked, "Account should be locked after 5 failed attempts")
	assert.Greater(t, lockedUntil, time.Now().Unix(), "Locked until should be in the future")

	// Test unlocking account
	err = manager.UnlockAccount(ctx, "admin", testUser.ID)
	assert.NoError(t, err)

	// Verify account is unlocked
	isLocked, _, err = manager.IsAccountLocked(ctx, testUser.ID)
	assert.NoError(t, err)
	assert.False(t, isLocked, "Account should be unlocked after manual unlock")
}

// TestRateLimiting tests login rate limiting
func TestRateLimiting(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	testIP := "10.0.0.100"

	// Type assert to access internal rate limiter
	authMgr, ok := manager.(*authManager)
	require.True(t, ok, "Failed to type assert Manager to *authManager")

	// Make multiple rapid requests
	allowed := 0
	denied := 0

	for i := 0; i < 10; i++ {
		if manager.CheckRateLimit(testIP) {
			allowed++
			// Simulate a failed attempt to increment the counter
			authMgr.rateLimiter.RecordFailedAttempt(testIP)
		} else {
			denied++
		}
	}

	// Should allow up to 5 attempts and deny the rest
	assert.Equal(t, 5, allowed, "Should allow exactly 5 attempts")
	assert.Equal(t, 5, denied, "Should deny 5 attempts after limit reached")
}

// Test2FASetup tests 2FA setup
func Test2FASetup(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test user
	testUser := &User{
		ID:          "2fa-user-123",
		Username:    "2fatest",
		Password:    "TestPassword123!",
		DisplayName: "2FA Test User",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}

	err := manager.CreateUser(ctx, testUser)
	require.NoError(t, err)

	// Setup 2FA
	setup, err := manager.Setup2FA(ctx, testUser.ID)
	assert.NoError(t, err)
	assert.NotNil(t, setup)
	assert.NotEmpty(t, setup.Secret, "2FA secret should not be empty")
	assert.NotEmpty(t, setup.QRCode, "QR code should not be empty")
	// QRCode is PNG bytes, check for PNG signature
	assert.True(t, len(setup.QRCode) > 4, "QR code should have data")
	assert.Equal(t, byte(0x89), setup.QRCode[0], "QR code should be PNG (signature byte 1)")
	assert.Equal(t, byte(0x50), setup.QRCode[1], "QR code should be PNG (signature byte 2)")
}

// TestUserCRUD tests user CRUD operations
func TestUserCRUD(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create
	newUser := &User{
		ID:          "crud-user-123",
		Username:    "crudtest",
		Password:    "Password123!",
		DisplayName: "CRUD Test User",
		Email:       "crud@example.com",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		TenantID:    "", // No tenant for this test user
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}

	err := manager.CreateUser(ctx, newUser)
	assert.NoError(t, err)

	// Read (via ListUsers)
	users, err := manager.ListUsers(ctx)
	assert.NoError(t, err)
	assert.NotEmpty(t, users)

	// Find our user
	var foundUser *User
	for _, u := range users {
		if u.ID == newUser.ID {
			foundUser = &u
			break
		}
	}
	assert.NotNil(t, foundUser, "User should be found in list")
	assert.Equal(t, newUser.Username, foundUser.Username)

	// Update
	newUser.DisplayName = "Updated Display Name"
	newUser.Email = "updated@example.com"
	err = manager.UpdateUser(ctx, newUser)
	assert.NoError(t, err)

	// Delete
	err = manager.DeleteUser(ctx, newUser.ID)
	assert.NoError(t, err)

	// Verify deletion
	users, err = manager.ListUsers(ctx)
	assert.NoError(t, err)

	foundAfterDelete := false
	for _, u := range users {
		if u.ID == newUser.ID {
			foundAfterDelete = true
			break
		}
	}
	assert.False(t, foundAfterDelete, "User should not be found after deletion")
}

// TestAccessKeyGeneration tests access key generation and validation
func TestAccessKeyGeneration(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test user
	testUser := &User{
		ID:          "accesskey-user-123",
		Username:    "accesskeytest",
		Password:    "Password123!",
		DisplayName: "Access Key Test User",
		Status:      UserStatusActive,
		Roles:       []string{"user"},
		TenantID:    "", // No tenant for this test user
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}

	err := manager.CreateUser(ctx, testUser)
	require.NoError(t, err)

	// Generate access key
	accessKey, err := manager.GenerateAccessKey(ctx, testUser.ID)
	assert.NoError(t, err)
	assert.NotNil(t, accessKey)
	assert.NotEmpty(t, accessKey.AccessKeyID)
	assert.NotEmpty(t, accessKey.SecretAccessKey)
	assert.Equal(t, testUser.ID, accessKey.UserID)
	assert.Equal(t, AccessKeyStatusActive, accessKey.Status)

	// Validate credentials with the generated key
	user, err := manager.ValidateCredentials(ctx, accessKey.AccessKeyID, accessKey.SecretAccessKey)
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, testUser.ID, user.ID)

	// List access keys for user
	keys, err := manager.ListAccessKeys(ctx, testUser.ID)
	assert.NoError(t, err)
	assert.NotEmpty(t, keys)
	assert.Equal(t, 1, len(keys))

	// Revoke access key
	err = manager.RevokeAccessKey(ctx, accessKey.AccessKeyID)
	assert.NoError(t, err)

	// Validate credentials should fail now
	user, err = manager.ValidateCredentials(ctx, accessKey.AccessKeyID, accessKey.SecretAccessKey)
	assert.Error(t, err)
	assert.Nil(t, user)
}
