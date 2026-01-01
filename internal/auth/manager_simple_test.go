package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ========================================
// Tests for Simple Getter Functions
// ========================================

func TestGetUser(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test user
	user := &User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
		Roles:    []string{"user"},
	}
	err := manager.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	tests := []struct {
		name    string
		userID  string
		wantErr bool
	}{
		{
			name:    "Get existing user",
			userID:  user.ID,
			wantErr: false,
		},
		{
			name:    "Get non-existent user",
			userID:  "nonexistent-id",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := manager.GetUser(ctx, tt.userID)

			if tt.wantErr {
				if err == nil {
					t.Error("GetUser() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("GetUser() unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Error("GetUser() returned nil user")
				return
			}

			if result.ID != tt.userID {
				t.Errorf("GetUser() returned user ID = %s, want %s", result.ID, tt.userID)
			}
		})
	}
}

func TestGetAccessKey(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test user
	user := &User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
		Roles:    []string{"user"},
	}
	err := manager.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Generate an access key
	accessKeyObj, err := manager.GenerateAccessKey(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to generate access key: %v", err)
	}

	tests := []struct {
		name        string
		accessKeyID string
		wantErr     bool
	}{
		{
			name:        "Get existing access key",
			accessKeyID: accessKeyObj.AccessKeyID,
			wantErr:     false,
		},
		{
			name:        "Get non-existent access key",
			accessKeyID: "NONEXISTENTKEY123456",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := manager.GetAccessKey(ctx, tt.accessKeyID)

			if tt.wantErr {
				if err == nil {
					t.Error("GetAccessKey() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("GetAccessKey() unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Error("GetAccessKey() returned nil")
				return
			}

			if result.AccessKeyID != tt.accessKeyID {
				t.Errorf("GetAccessKey() returned key ID = %s, want %s", result.AccessKeyID, tt.accessKeyID)
			}
		})
	}
}

func TestGetDB(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access GetDB
	am := manager.(*authManager)

	db := am.GetDB()
	if db == nil {
		t.Error("GetDB() returned nil")
	}

	// Verify it's the database interface
	if db == nil {
		t.Error("GetDB() should return non-nil database")
	}
}

func TestIsReady(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access IsReady
	am := manager.(*authManager)

	ready := am.IsReady()
	if !ready {
		t.Error("IsReady() = false, want true")
	}
}

// ========================================
// Tests for Context Helper Functions
// ========================================

func TestGetUserIDFromContext(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func() context.Context
		want     string
	}{
		{
			name: "Context with user",
			setupCtx: func() context.Context {
				user := &User{
					ID:       "user-123",
					Username: "testuser",
				}
				return context.WithValue(context.Background(), "user", user)
			},
			want: "user-123",
		},
		{
			name: "Context without user",
			setupCtx: func() context.Context {
				return context.Background()
			},
			want: "",
		},
		{
			name: "Context with nil user",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), "user", (*User)(nil))
			},
			want: "",
		},
		{
			name: "Context with wrong type",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), "user", "not-a-user")
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			result := GetUserIDFromContext(ctx)
			if result != tt.want {
				t.Errorf("GetUserIDFromContext() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestGetTenantIDFromContext(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func() context.Context
		want     string
	}{
		{
			name: "Context with user having tenant",
			setupCtx: func() context.Context {
				user := &User{
					ID:       "user-123",
					Username: "testuser",
					TenantID: "tenant-456",
				}
				return context.WithValue(context.Background(), "user", user)
			},
			want: "tenant-456",
		},
		{
			name: "Context with user without tenant",
			setupCtx: func() context.Context {
				user := &User{
					ID:       "user-123",
					Username: "testuser",
					TenantID: "",
				}
				return context.WithValue(context.Background(), "user", user)
			},
			want: "",
		},
		{
			name: "Context without user",
			setupCtx: func() context.Context {
				return context.Background()
			},
			want: "",
		},
		{
			name: "Context with nil user",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), "user", (*User)(nil))
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			result := GetTenantIDFromContext(ctx)
			if result != tt.want {
				t.Errorf("GetTenantIDFromContext() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestIsAdminUser(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func() context.Context
		want     bool
	}{
		{
			name: "Admin user",
			setupCtx: func() context.Context {
				user := &User{
					ID:       "user-123",
					Username: "admin",
					Roles:    []string{"admin"},
				}
				return context.WithValue(context.Background(), "user", user)
			},
			want: true,
		},
		{
			name: "Admin user with multiple roles",
			setupCtx: func() context.Context {
				user := &User{
					ID:       "user-123",
					Username: "poweruser",
					Roles:    []string{"user", "admin", "moderator"},
				}
				return context.WithValue(context.Background(), "user", user)
			},
			want: true,
		},
		{
			name: "Non-admin user",
			setupCtx: func() context.Context {
				user := &User{
					ID:       "user-123",
					Username: "regularuser",
					Roles:    []string{"user"},
				}
				return context.WithValue(context.Background(), "user", user)
			},
			want: false,
		},
		{
			name: "User with no roles",
			setupCtx: func() context.Context {
				user := &User{
					ID:       "user-123",
					Username: "noroles",
					Roles:    []string{},
				}
				return context.WithValue(context.Background(), "user", user)
			},
			want: false,
		},
		{
			name: "Context without user",
			setupCtx: func() context.Context {
				return context.Background()
			},
			want: false,
		},
		{
			name: "Context with nil user",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), "user", (*User)(nil))
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			result := IsAdminUser(ctx)
			if result != tt.want {
				t.Errorf("IsAdminUser() = %v, want %v", result, tt.want)
			}
		})
	}
}

// ========================================
// Tests for Permission Functions
// ========================================

func TestCheckBucketPermission(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	tests := []struct {
		name    string
		user    *User
		bucket  string
		action  string
		wantErr bool
	}{
		{
			name: "Admin user can perform any action",
			user: &User{
				ID:       "admin-user",
				Username: "admin",
				Roles:    []string{"admin"},
			},
			bucket:  "test-bucket",
			action:  "s3:PutObject",
			wantErr: false,
		},
		{
			name: "Non-admin user denied",
			user: &User{
				ID:       "regular-user",
				Username: "user",
				Roles:    []string{"user"},
			},
			bucket:  "test-bucket",
			action:  "s3:DeleteBucket",
			wantErr: true,
		},
		{
			name: "User with readonly role denied write",
			user: &User{
				ID:       "readonly-user",
				Username: "readonly",
				Roles:    []string{"readonly"},
			},
			bucket:  "test-bucket",
			action:  "s3:PutObject",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.CheckBucketPermission(ctx, tt.user, tt.bucket, tt.action)

			if tt.wantErr {
				if err == nil {
					t.Error("CheckBucketPermission() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("CheckBucketPermission() unexpected error: %v", err)
			}
		})
	}
}

func TestCheckObjectPermission(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	tests := []struct {
		name    string
		user    *User
		bucket  string
		object  string
		action  string
		wantErr bool
	}{
		{
			name: "Admin user can perform any action",
			user: &User{
				ID:       "admin-user",
				Username: "admin",
				Roles:    []string{"admin"},
			},
			bucket:  "test-bucket",
			object:  "test-object.txt",
			action:  "s3:GetObject",
			wantErr: false,
		},
		{
			name: "Non-admin user denied",
			user: &User{
				ID:       "regular-user",
				Username: "user",
				Roles:    []string{"user"},
			},
			bucket:  "test-bucket",
			object:  "secret.txt",
			action:  "s3:DeleteObject",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.CheckObjectPermission(ctx, tt.user, tt.bucket, tt.object, tt.action)

			if tt.wantErr {
				if err == nil {
					t.Error("CheckObjectPermission() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("CheckObjectPermission() unexpected error: %v", err)
			}
		})
	}
}

// ========================================
// Tests for User Management Functions
// ========================================

func TestUpdateUserPreferences(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	// Create a test user
	user := &User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
		Roles:    []string{"user"},
	}
	err := manager.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	tests := []struct {
		name               string
		userID             string
		themePreference    string
		languagePreference string
		wantErr            bool
	}{
		{
			name:               "Valid light theme",
			userID:             user.ID,
			themePreference:    "light",
			languagePreference: "en",
			wantErr:            false,
		},
		{
			name:               "Valid dark theme",
			userID:             user.ID,
			themePreference:    "dark",
			languagePreference: "es",
			wantErr:            false,
		},
		{
			name:               "Valid system theme",
			userID:             user.ID,
			themePreference:    "system",
			languagePreference: "fr",
			wantErr:            false,
		},
		{
			name:               "Invalid theme",
			userID:             user.ID,
			themePreference:    "invalid-theme",
			languagePreference: "en",
			wantErr:            true,
		},
		{
			name:               "Empty language",
			userID:             user.ID,
			themePreference:    "light",
			languagePreference: "",
			wantErr:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.UpdateUserPreferences(ctx, tt.userID, tt.themePreference, tt.languagePreference)

			if tt.wantErr {
				if err == nil {
					t.Error("UpdateUserPreferences() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("UpdateUserPreferences() unexpected error: %v", err)
			}
		})
	}
}

// ========================================
// Tests for Utility Functions
// ========================================

func TestWriteS3Error(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		message    string
		statusCode int
	}{
		{
			name:       "AccessDenied error",
			code:       "AccessDenied",
			message:    "Access Denied",
			statusCode: http.StatusForbidden,
		},
		{
			name:       "NoSuchBucket error",
			code:       "NoSuchBucket",
			message:    "The specified bucket does not exist",
			statusCode: http.StatusNotFound,
		},
		{
			name:       "InternalError",
			code:       "InternalError",
			message:    "We encountered an internal error",
			statusCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test HTTP response recorder
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/bucket/object", nil)

			// Call writeS3Error
			writeS3Error(w, r, tt.code, tt.message, tt.statusCode)

			// Check status code
			if w.Code != tt.statusCode {
				t.Errorf("writeS3Error() status code = %d, want %d", w.Code, tt.statusCode)
			}

			// Check Content-Type header
			contentType := w.Header().Get("Content-Type")
			if contentType != "application/xml" {
				t.Errorf("writeS3Error() Content-Type = %q, want %q", contentType, "application/xml")
			}

			// Check response body contains error code and message
			body := w.Body.String()
			if !strings.Contains(body, tt.code) {
				t.Errorf("writeS3Error() body doesn't contain code %q", tt.code)
			}
			if !strings.Contains(body, tt.message) {
				t.Errorf("writeS3Error() body doesn't contain message %q", tt.message)
			}
		})
	}
}
