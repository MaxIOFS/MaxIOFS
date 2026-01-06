package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/sirupsen/logrus"
)

// Manager defines the interface for authentication and authorization
type Manager interface {
	// Authentication
	ValidateCredentials(ctx context.Context, accessKey, secretKey string) (*User, error)
	ValidateConsoleCredentials(ctx context.Context, username, password string) (*User, error)
	ValidateJWT(ctx context.Context, token string) (*User, error)
	GenerateJWT(ctx context.Context, user *User) (string, error)

	// S3 Signature validation
	ValidateS3Signature(ctx context.Context, r *http.Request) (*User, error)
	ValidateS3SignatureV4(ctx context.Context, r *http.Request) (*User, error)
	ValidateS3SignatureV2(ctx context.Context, r *http.Request) (*User, error)

	// Authorization
	CheckPermission(ctx context.Context, user *User, action, resource string) error
	CheckBucketPermission(ctx context.Context, user *User, bucket, action string) error
	CheckObjectPermission(ctx context.Context, user *User, bucket, object, action string) error

	// User management
	CreateUser(ctx context.Context, user *User) error
	UpdateUser(ctx context.Context, user *User) error
	UpdateUserPreferences(ctx context.Context, userID, themePreference, languagePreference string) error
	DeleteUser(ctx context.Context, userID string) error
	GetUser(ctx context.Context, accessKey string) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)

	// Access key management
	GenerateAccessKey(ctx context.Context, userID string) (*AccessKey, error)
	GetAccessKey(ctx context.Context, accessKeyID string) (*AccessKey, error)
	RevokeAccessKey(ctx context.Context, accessKey string) error
	ListAccessKeys(ctx context.Context, userID string) ([]AccessKey, error)

	// Tenant management
	CreateTenant(ctx context.Context, tenant *Tenant) error
	GetTenant(ctx context.Context, tenantID string) (*Tenant, error)
	GetTenantByName(ctx context.Context, name string) (*Tenant, error)
	ListTenants(ctx context.Context) ([]*Tenant, error)
	UpdateTenant(ctx context.Context, tenant *Tenant) error
	DeleteTenant(ctx context.Context, tenantID string) error
	ListTenantUsers(ctx context.Context, tenantID string) ([]*User, error)
	IncrementTenantBucketCount(ctx context.Context, tenantID string) error
	DecrementTenantBucketCount(ctx context.Context, tenantID string) error
	IncrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error
	DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error
	CheckTenantStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) error

	// Bucket permission management
	GrantBucketAccess(ctx context.Context, bucketName, userID, tenantID, permissionLevel, grantedBy string, expiresAt int64) error
	RevokeBucketAccess(ctx context.Context, bucketName, userID, tenantID string) error
	CheckBucketAccess(ctx context.Context, bucketName, userID string) (bool, string, error)
	ListBucketPermissions(ctx context.Context, bucketName string) ([]*BucketPermission, error)
	ListUserBucketPermissions(ctx context.Context, userID string) ([]*BucketPermission, error)

	// HTTP Middleware
	Middleware() func(http.Handler) http.Handler

	// Account lockout management
	CheckRateLimit(ip string) bool
	IsAccountLocked(ctx context.Context, userID string) (bool, int64, error)
	LockAccount(ctx context.Context, userID string) error
	UnlockAccount(ctx context.Context, adminUserID, targetUserID string) error
	RecordFailedLogin(ctx context.Context, userID, ip string) error
	RecordSuccessfulLogin(ctx context.Context, userID string) error
	SetUserLockedCallback(callback func(*User))

	// Two-Factor Authentication
	Setup2FA(ctx context.Context, userID string) (*TOTPSetup, error)
	Enable2FA(ctx context.Context, userID, code string, secret string) ([]string, error)
	Disable2FA(ctx context.Context, userID, requestingUserID string, isGlobalAdmin bool) error
	Verify2FACode(ctx context.Context, userID, code string) (bool, error)
	RegenerateBackupCodes(ctx context.Context, userID string) ([]string, error)
	Get2FAStatus(ctx context.Context, userID string) (bool, int64, error)

	// Health check
	IsReady() bool

	// Database access (for sharing with other managers)
	GetDB() interface{}
}

// User represents a user in the system
type User struct {
	ID          string            `json:"id"`
	Username    string            `json:"username,omitempty"` // For console login
	Password    string            `json:"password,omitempty"` // Hashed password for console login
	DisplayName string            `json:"display_name"`
	Email       string            `json:"email,omitempty"`
	Status      string            `json:"status"`              // active, inactive, suspended
	TenantID    string            `json:"tenant_id,omitempty"` // NEW: Tenant assignment
	Roles       []string          `json:"roles"`
	Policies    []string          `json:"policies"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   int64             `json:"created_at"`
	UpdatedAt   int64             `json:"updated_at"`

	// Two-Factor Authentication fields
	TwoFactorEnabled bool     `json:"two_factor_enabled"`
	TwoFactorSecret  string   `json:"-"` // NEVER return in JSON - encrypted in DB
	TwoFactorSetupAt int64    `json:"two_factor_setup_at,omitempty"`
	BackupCodes      []string `json:"-"` // NEVER return in JSON - hashed in DB
	BackupCodesUsed  []string `json:"-"` // Track used backup codes

	// Account lockout fields
	FailedLoginAttempts int   `json:"failed_login_attempts,omitempty"`
	LastFailedLogin     int64 `json:"last_failed_login,omitempty"`
	LockedUntil         int64 `json:"locked_until,omitempty"`

	// User preferences
	ThemePreference    string `json:"themePreference,omitempty"`    // 'light', 'dark', 'system'
	LanguagePreference string `json:"languagePreference,omitempty"` // 'en', 'es', etc.
}

// Tenant represents an organizational unit for multi-tenancy
type Tenant struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	DisplayName         string            `json:"display_name"`
	Description         string            `json:"description"`
	Status              string            `json:"status"` // active, inactive
	MaxAccessKeys       int64             `json:"max_access_keys"`
	CurrentAccessKeys   int64             `json:"current_access_keys"` // Calculated in real-time
	MaxStorageBytes     int64             `json:"max_storage_bytes"`
	CurrentStorageBytes int64             `json:"current_storage_bytes"` // Calculated in real-time
	MaxBuckets          int64             `json:"max_buckets"`
	CurrentBuckets      int64             `json:"current_buckets"` // Incremented/decremented on create/delete
	Metadata            map[string]string `json:"metadata,omitempty"`
	CreatedAt           int64             `json:"created_at"`
	UpdatedAt           int64             `json:"updated_at"`
}

// BucketPermission represents access permissions for a bucket
type BucketPermission struct {
	ID              string `json:"id"`
	BucketName      string `json:"bucketName"`
	UserID          string `json:"userId,omitempty"`
	TenantID        string `json:"tenantId,omitempty"`
	PermissionLevel string `json:"permissionLevel"` // read, write, admin
	GrantedBy       string `json:"grantedBy"`
	GrantedAt       int64  `json:"grantedAt"`
	ExpiresAt       int64  `json:"expiresAt,omitempty"`
}

// AccessKey represents an access key pair
type AccessKey struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	UserID          string `json:"user_id"`
	Status          string `json:"status"` // active, inactive
	CreatedAt       int64  `json:"created_at"`
	LastUsed        int64  `json:"last_used,omitempty"`
}

// authManager implements the Manager interface
type authManager struct {
	config             config.AuthConfig
	store              *SQLiteStore
	rateLimiter        *LoginRateLimiter
	auditManager       *audit.Manager
	userLockedCallback func(*User)
	settingsManager    SettingsManager
}

// SettingsManager interface for retrieving system settings
type SettingsManager interface {
	GetInt(key string) (int, error)
}

// NewManager creates a new authentication manager with SQLite backend
func NewManager(cfg config.AuthConfig, dataDir string) Manager {
	// Initialize SQLite store
	store, err := NewSQLiteStore(dataDir)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to initialize SQLite auth store")
	}

	// Create rate limiter: max 5 attempts per 60 seconds (1 minute)
	rateLimiter := NewLoginRateLimiter(5, 60)

	manager := &authManager{
		config:      cfg,
		store:       store,
		rateLimiter: rateLimiter,
	}

	// Create default admin user if not exists (without access keys)
	_, err = store.GetUserByUsername("admin")
	if err != nil {
		// Admin doesn't exist, create it
		now := time.Now().Unix()
		adminUser := &User{
			ID:          "admin",
			Username:    "admin",
			Password:    "admin", // Will be hashed by SQLiteStore.CreateUser
			DisplayName: "Administrator",
			Email:       "admin@maxiofs.local",
			Status:      UserStatusActive,
			Roles:       []string{"admin"},
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		if err := store.CreateUser(adminUser); err != nil {
			logrus.WithError(err).Error("Failed to create default admin user")
		} else {
			logrus.Info("✅ Created default admin user (username: admin, password: admin)")
			logrus.Warn("⚠️  Please create S3 access keys through the web console - no default keys are created for security")
		}
	}

	return manager
}

// ValidateCredentials validates access/secret key credentials (S3 API)
func (am *authManager) ValidateCredentials(ctx context.Context, accessKey, secretKey string) (*User, error) {
	if !am.config.EnableAuth {
		// Return default user when auth is disabled
		return &User{
			ID:          "anonymous",
			DisplayName: "Anonymous User",
			Status:      "active",
			Roles:       []string{"admin"},
		}, nil
	}

	// Get access key from database
	key, err := am.store.GetAccessKey(accessKey)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	// Validate secret key
	if key.SecretAccessKey != secretKey {
		return nil, ErrInvalidCredentials
	}

	// Check key status
	if key.Status != AccessKeyStatusActive {
		return nil, ErrInvalidCredentials
	}

	// Get user associated with this key
	user, err := am.store.GetUserByID(key.UserID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Check user status
	if user.Status != UserStatusActive {
		return nil, ErrInvalidCredentials
	}

	// Update last used timestamp
	am.store.UpdateAccessKeyLastUsed(accessKey, time.Now().Unix())

	return user, nil
}

// ValidateConsoleCredentials validates username/password for console login
func (am *authManager) ValidateConsoleCredentials(ctx context.Context, username, password string) (*User, error) {
	if !am.config.EnableAuth {
		// Return default user when auth is disabled
		return &User{
			ID:          "anonymous",
			Username:    "anonymous",
			DisplayName: "Anonymous User",
			Status:      "active",
			Roles:       []string{"admin"},
		}, nil
	}

	// Get user by username from database
	user, err := am.store.GetUserByUsername(username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	// Try bcrypt verification first
	if !VerifyPassword(password, user.Password) {
		// If bcrypt fails, try legacy SHA256 hash (for migration)
		sha256Hash := hashPasswordSHA256(password)
		if user.Password != sha256Hash {
			return nil, ErrInvalidCredentials
		}

		// Password is still SHA256 - migrate to bcrypt
		newHash, hashErr := HashPassword(password)
		if hashErr != nil {
			logrus.WithError(hashErr).Error("Failed to migrate password to bcrypt")
		} else {
			// Update user's password to bcrypt hash
			updateErr := am.store.UpdateUserPassword(user.ID, newHash)
			if updateErr != nil {
				logrus.WithError(updateErr).Error("Failed to update password hash in database")
			} else {
				logrus.WithField("user_id", user.ID).Info("Successfully migrated user password to bcrypt")
			}
		}
	}

	// Check user status
	if user.Status != UserStatusActive {
		return nil, ErrUserInactive
	}

	return user, nil
}

// hashPasswordSHA256 creates a SHA256 hash (legacy - for migration only)
func hashPasswordSHA256(password string) string {
	h := sha256.New()
	h.Write([]byte(password))
	return hex.EncodeToString(h.Sum(nil))
}

// ValidateJWT validates a JWT token
func (am *authManager) ValidateJWT(ctx context.Context, token string) (*User, error) {
	if token == "" {
		return nil, ErrInvalidToken
	}

	// For MVP, implement basic token validation
	// In production, use proper JWT library like golang-jwt/jwt
	claims, err := am.parseBasicToken(token)
	if err != nil {
		return nil, err
	}

	// Check expiration
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, ErrTokenExpired
	}

	// Get user by username (AccessKey in claims is username for console users)
	user, err := am.store.GetUserByUsername(claims.AccessKey)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GenerateJWT generates a JWT token for a user
func (am *authManager) GenerateJWT(ctx context.Context, user *User) (string, error) {
	// For console users, always use username as AccessKey
	accessKey := user.Username
	if accessKey == "" {
		// If no username, this shouldn't happen for console users
		return "", fmt.Errorf("user has no username")
	}

	// Create claims
	now := time.Now()
	claims := JWTClaims{
		UserID:    user.ID,
		TenantID:  user.TenantID,
		AccessKey: accessKey,
		Roles:     user.Roles,
		ExpiresAt: now.Add(24 * time.Hour).Unix(), // 24 hour max session - idle timeout handled by frontend
		IssuedAt:  now.Unix(),
		NotBefore: now.Unix(),
		Issuer:    "maxiofs",
		Subject:   user.ID,
		Audience:  "maxiofs-api",
	}

	// For MVP, create basic token (not secure, use proper JWT in production)
	return am.createBasicToken(claims)
}

// ValidateS3Signature validates S3 request signature (auto-detect version)
func (am *authManager) ValidateS3Signature(ctx context.Context, r *http.Request) (*User, error) {
	// Auto-detect signature version and delegate to appropriate method
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, ErrMissingSignature
	}

	logrus.WithField("auth_header", authHeader).Info("ValidateS3Signature called")

	// Check if it's SigV4 (from Authorization header or query param)
	if strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256") || r.Header.Get("X-Amz-Algorithm") == "AWS4-HMAC-SHA256" {
		logrus.Info("Delegating to ValidateS3SignatureV4")
		return am.ValidateS3SignatureV4(ctx, r)
	}

	// Otherwise assume SigV2
	logrus.Info("Delegating to ValidateS3SignatureV2")
	return am.ValidateS3SignatureV2(ctx, r)
}

// ValidateS3SignatureV4 validates AWS Signature Version 4
func (am *authManager) ValidateS3SignatureV4(ctx context.Context, r *http.Request) (*User, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return nil, ErrMissingSignature
	}

	// Parse Authorization header
	sig, err := am.parseS3SignatureV4(auth, r)
	if err != nil {
		logrus.WithError(err).Error("Failed to parse SigV4 header")
		return nil, err
	}

	// Get access key from database
	accessKey, err := am.store.GetAccessKey(sig.AccessKey)
	if err != nil {
		logrus.WithField("access_key", sig.AccessKey).Warn("Access key not found")
		return nil, ErrInvalidCredentials
	}

	// Get user associated with this key
	user, err := am.store.GetUserByID(accessKey.UserID)
	if err != nil {
		logrus.WithField("user_id", accessKey.UserID).Warn("User not found for access key")
		return nil, ErrUserNotFound
	}

	// Verify signature
	if !am.verifyS3SignatureV4(r, sig, accessKey.SecretAccessKey) {
		return nil, ErrInvalidSignature
	}

	// Update last used
	am.store.UpdateAccessKeyLastUsed(accessKey.AccessKeyID, time.Now().Unix())

	return user, nil
}

// ValidateS3SignatureV2 validates AWS Signature Version 2
func (am *authManager) ValidateS3SignatureV2(ctx context.Context, r *http.Request) (*User, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return nil, ErrMissingSignature
	}

	// Parse Authorization header for V2
	sig, err := am.parseS3SignatureV2(auth, r)
	if err != nil {
		return nil, err
	}

	// Get access key from database
	accessKey, err := am.store.GetAccessKey(sig.AccessKey)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	// Get user associated with this key
	user, err := am.store.GetUserByID(accessKey.UserID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Verify signature
	if !am.verifyS3SignatureV2(r, sig, accessKey.SecretAccessKey) {
		return nil, ErrInvalidSignature
	}

	// Update last used
	am.store.UpdateAccessKeyLastUsed(accessKey.AccessKeyID, time.Now().Unix())

	return user, nil
}

// CheckPermission checks if user has permission for action on resource
func (am *authManager) CheckPermission(ctx context.Context, user *User, action, resource string) error {
	// TODO: Implement in Fase 1.4 - Authentication Manager
	// For now, allow all operations for admin users
	for _, role := range user.Roles {
		if role == "admin" {
			return nil
		}
	}
	return ErrAccessDenied
}

// CheckBucketPermission checks bucket-level permissions
func (am *authManager) CheckBucketPermission(ctx context.Context, user *User, bucket, action string) error {
	// TODO: Implement in Fase 1.4 - Authentication Manager
	return am.CheckPermission(ctx, user, action, "bucket:"+bucket)
}

// CheckObjectPermission checks object-level permissions
func (am *authManager) CheckObjectPermission(ctx context.Context, user *User, bucket, object, action string) error {
	// TODO: Implement in Fase 1.4 - Authentication Manager
	return am.CheckPermission(ctx, user, action, "object:"+bucket+"/"+object)
}

// User management methods
func (am *authManager) CreateUser(ctx context.Context, user *User) error {
	if user.Username == "" {
		return fmt.Errorf("username is required")
	}

	// Set timestamps
	now := time.Now().Unix()
	user.CreatedAt = now
	user.UpdatedAt = now

	// Set default status
	if user.Status == "" {
		user.Status = UserStatusActive
	}

	// Initialize metadata if nil
	if user.Metadata == nil {
		user.Metadata = make(map[string]string)
	}

	// Create user in database (password will be hashed by SQLiteStore)
	err := am.store.CreateUser(user)
	if err != nil {
		return err
	}

	// Get the user performing the action from context
	actingUser, actingUserExists := GetUserFromContext(ctx)
	actingUserID := ""
	actingUsername := "system"
	if actingUserExists {
		actingUserID = actingUser.ID
		actingUsername = actingUser.Username
	}

	// Log audit event for user created
	am.logAuditEvent(ctx, &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       actingUserID,   // Who performed the action
		Username:     actingUsername, // Who performed the action
		EventType:    audit.EventTypeUserCreated,
		ResourceType: audit.ResourceTypeUser,
		ResourceID:   user.ID,       // The created user
		ResourceName: user.Username, // The created user
		Action:       audit.ActionCreate,
		Status:       audit.StatusSuccess,
		Details: map[string]interface{}{
			"created_user": user.Username,
			"roles":        user.Roles,
			"status":       user.Status,
		},
	})

	return nil
}

func (am *authManager) UpdateUser(ctx context.Context, user *User) error {
	// Update timestamp
	user.UpdatedAt = time.Now().Unix()

	// Update user in database
	err := am.store.UpdateUser(user)
	if err != nil {
		return err
	}

	// Get the user performing the action from context
	actingUser, actingUserExists := GetUserFromContext(ctx)
	actingUserID := ""
	actingUsername := "system"
	if actingUserExists {
		actingUserID = actingUser.ID
		actingUsername = actingUser.Username
	}

	// Log audit event for user updated
	am.logAuditEvent(ctx, &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       actingUserID,
		Username:     actingUsername,
		EventType:    audit.EventTypeUserUpdated,
		ResourceType: audit.ResourceTypeUser,
		ResourceID:   user.ID,
		ResourceName: user.Username,
		Action:       audit.ActionUpdate,
		Status:       audit.StatusSuccess,
		Details: map[string]interface{}{
			"updated_user": user.Username,
			"roles":        user.Roles,
			"status":       user.Status,
		},
	})

	return nil
}

func (am *authManager) UpdateUserPreferences(ctx context.Context, userID, themePreference, languagePreference string) error {
	// Validate theme preference
	validThemes := map[string]bool{"light": true, "dark": true, "system": true}
	if !validThemes[themePreference] {
		return fmt.Errorf("invalid theme preference: must be 'light', 'dark', or 'system'")
	}

	// Validate language preference (basic check for non-empty)
	if languagePreference == "" {
		return fmt.Errorf("language preference cannot be empty")
	}

	// Update preferences in database
	err := am.store.UpdateUserPreferences(userID, themePreference, languagePreference)
	if err != nil {
		return err
	}

	// Get the user performing the action from context
	actingUser, actingUserExists := GetUserFromContext(ctx)
	actingUserID := ""
	actingUsername := "system"
	if actingUserExists {
		actingUserID = actingUser.ID
		actingUsername = actingUser.Username
	}

	// Log audit event for preferences updated
	am.logAuditEvent(ctx, &audit.AuditEvent{
		TenantID:     "",
		UserID:       actingUserID,
		Username:     actingUsername,
		EventType:    audit.EventTypeUserUpdated,
		ResourceType: audit.ResourceTypeUser,
		ResourceID:   userID,
		ResourceName: "",
		Action:       audit.ActionUpdate,
		Status:       audit.StatusSuccess,
		Details: map[string]interface{}{
			"preferences_updated": true,
			"theme":               themePreference,
			"language":            languagePreference,
		},
	})

	return nil
}

func (am *authManager) DeleteUser(ctx context.Context, userID string) error {
	// Don't allow deleting admin user (last resort account)
	if userID == "admin" {
		return fmt.Errorf("cannot delete admin user")
	}

	// Get user info before deleting
	user, err := am.store.GetUserByID(userID)
	if err != nil {
		return err
	}

	// Soft delete user in database (also soft deletes associated access keys)
	err = am.store.DeleteUser(userID)
	if err != nil {
		return err
	}

	// Get the user performing the action from context
	actingUser, actingUserExists := GetUserFromContext(ctx)
	actingUserID := ""
	actingUsername := "system"
	if actingUserExists {
		actingUserID = actingUser.ID
		actingUsername = actingUser.Username
	}

	// Log audit event for user deleted
	am.logAuditEvent(ctx, &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       actingUserID,
		Username:     actingUsername,
		EventType:    audit.EventTypeUserDeleted,
		ResourceType: audit.ResourceTypeUser,
		ResourceID:   user.ID,
		ResourceName: user.Username,
		Action:       audit.ActionDelete,
		Status:       audit.StatusSuccess,
		Details: map[string]interface{}{
			"deleted_user": user.Username,
		},
	})

	return nil
}

func (am *authManager) GetUser(ctx context.Context, userID string) (*User, error) {
	return am.store.GetUserByID(userID)
}

func (am *authManager) ListUsers(ctx context.Context) ([]User, error) {
	usersPtrs, err := am.store.ListUsers()
	if err != nil {
		return nil, err
	}

	// NOTE: Filtering is handled by the HTTP handler (console_api.go)
	// This method returns all users from the database
	// The handler will filter based on:
	// - Global admin (admin role + no tenantID): sees all users
	// - Tenant admin (admin role + tenantID): sees users from their tenant
	// - Regular user: sees only themselves

	// Convert []*User to []User
	users := make([]User, len(usersPtrs))
	for i, u := range usersPtrs {
		users[i] = *u
	}
	return users, nil
}

// Access key management methods
func (am *authManager) GenerateAccessKey(ctx context.Context, userID string) (*AccessKey, error) {
	// Verify user exists
	_, err := am.store.GetUserByID(userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Generate new access key pair (AWS-compatible format)
	accessKeyID, err := am.generateAccessKeyID()
	if err != nil {
		return nil, err
	}

	secretAccessKey, err := am.generateSecretAccessKey()
	if err != nil {
		return nil, err
	}

	// Create access key
	accessKey := &AccessKey{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		UserID:          userID,
		Status:          AccessKeyStatusActive,
		CreatedAt:       time.Now().Unix(),
	}

	// Store in database
	if err := am.store.CreateAccessKey(accessKey); err != nil {
		return nil, err
	}

	return accessKey, nil
}

func (am *authManager) GetAccessKey(ctx context.Context, accessKeyID string) (*AccessKey, error) {
	return am.store.GetAccessKey(accessKeyID)
}

func (am *authManager) RevokeAccessKey(ctx context.Context, accessKey string) error {
	// Soft delete (set status to 'deleted')
	return am.store.DeleteAccessKey(accessKey)
}

func (am *authManager) ListAccessKeys(ctx context.Context, userID string) ([]AccessKey, error) {
	keysPtrs, err := am.store.ListAccessKeysByUser(userID)
	if err != nil {
		return nil, err
	}

	// Convert []*AccessKey to []AccessKey
	keys := make([]AccessKey, len(keysPtrs))
	for i, k := range keysPtrs {
		keys[i] = *k
	}
	return keys, nil
}

// Middleware returns an HTTP middleware for authentication
func (am *authManager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO: Implement in Fase 1.4 - Authentication Manager
			// For now, allow all requests if auth is disabled
			if !am.config.EnableAuth {
				next.ServeHTTP(w, r)
				return
			}

			// Public routes that don't require authentication
			publicRoutes := []string{
				"/auth/login",
				"/auth/register",
				"/health",
			}

			// Check if this is a public route
			for _, route := range publicRoutes {
				if strings.Contains(r.URL.Path, route) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Check if this is a presigned URL request (bypass auth middleware)
			query := r.URL.Query()
			isPresigned := query.Get("X-Amz-Algorithm") != "" || query.Get("AWSAccessKeyId") != ""
			if isPresigned {
				// Presigned URLs are validated in the S3 handler itself
				logrus.WithFields(logrus.Fields{
					"method": r.Method,
					"path":   r.URL.Path,
				}).Debug("Presigned URL request - bypassing auth middleware")
				next.ServeHTTP(w, r)
				return
			}

			// Check if request has authentication headers
			hasAuth := r.Header.Get("Authorization") != ""

			// Try to validate request
			user, err := am.ValidateS3Signature(r.Context(), r)
			if err != nil {
				// If there WAS an auth header but it's invalid, return error
				if hasAuth {
					logrus.WithFields(logrus.Fields{
						"method": r.Method,
						"path":   r.URL.Path,
						"error":  err.Error(),
						"auth":   r.Header.Get("Authorization"),
					}).Warn("Authentication failed")

					// Return S3-compatible XML error for 4xx errors
					writeS3Error(w, r, "InvalidAccessKeyId", "The AWS Access Key Id you provided does not exist in our records.", http.StatusUnauthorized)
					return
				}

				// If there was NO auth header, let the request pass (handler will check for shares)
				logrus.WithFields(logrus.Fields{
					"method": r.Method,
					"path":   r.URL.Path,
				}).Debug("No authentication provided - passing to handler")
				next.ServeHTTP(w, r)
				return
			}

			// Add user to request context
			ctx := context.WithValue(r.Context(), "user", user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// IsReady checks if the auth manager is ready
func (am *authManager) IsReady() bool {
	// TODO: Implement readiness check
	return true
}

// SetAuditManager sets the audit manager for logging events
// This is called after initialization to inject the audit dependency
func (am *authManager) SetAuditManager(auditMgr *audit.Manager) {
	am.auditManager = auditMgr
}

// SetUserLockedCallback sets the callback to be called when a user is locked
func (am *authManager) SetUserLockedCallback(callback func(*User)) {
	am.userLockedCallback = callback
}

// SetSettingsManager sets the settings manager for dynamic configuration
func (am *authManager) SetSettingsManager(settingsMgr SettingsManager) {
	am.settingsManager = settingsMgr

	// Recreate rate limiter with settings from database
	if settingsMgr != nil {
		maxAttempts, err := settingsMgr.GetInt("security.ratelimit_login_per_minute")
		if err != nil {
			logrus.WithError(err).Warn("Failed to get ratelimit_login_per_minute, using default 5")
			maxAttempts = 5
		}

		// Window is always 60 seconds (1 minute) to match the setting name
		am.rateLimiter = NewLoginRateLimiter(maxAttempts, 60)

		logrus.WithFields(logrus.Fields{
			"max_attempts_per_minute": maxAttempts,
		}).Info("Rate limiter configured from settings")
	}
}

// GetDB returns the underlying SQLite database connection
// This allows other managers (like settings) to share the same database
func (am *authManager) GetDB() interface{} {
	return am.store.db
}

// Tenant management methods
func (am *authManager) CreateTenant(ctx context.Context, tenant *Tenant) error {
	return am.store.CreateTenant(tenant)
}

func (am *authManager) GetTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	return am.store.GetTenant(tenantID)
}

func (am *authManager) GetTenantByName(ctx context.Context, name string) (*Tenant, error) {
	return am.store.GetTenantByName(name)
}

func (am *authManager) ListTenants(ctx context.Context) ([]*Tenant, error) {
	return am.store.ListTenants()
}

func (am *authManager) UpdateTenant(ctx context.Context, tenant *Tenant) error {
	return am.store.UpdateTenant(tenant)
}

func (am *authManager) DeleteTenant(ctx context.Context, tenantID string) error {
	return am.store.DeleteTenant(tenantID)
}

func (am *authManager) ListTenantUsers(ctx context.Context, tenantID string) ([]*User, error) {
	return am.store.ListTenantUsers(tenantID)
}

func (am *authManager) IncrementTenantBucketCount(ctx context.Context, tenantID string) error {
	return am.store.IncrementTenantBucketCount(tenantID)
}

func (am *authManager) DecrementTenantBucketCount(ctx context.Context, tenantID string) error {
	return am.store.DecrementTenantBucketCount(tenantID)
}

func (am *authManager) IncrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	return am.store.IncrementTenantStorage(tenantID, bytes)
}

func (am *authManager) DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	return am.store.DecrementTenantStorage(tenantID, bytes)
}

func (am *authManager) CheckTenantStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) error {
	return am.store.CheckTenantStorageQuota(tenantID, additionalBytes)
}

// Bucket permission management methods
func (am *authManager) GrantBucketAccess(ctx context.Context, bucketName, userID, tenantID, permissionLevel, grantedBy string, expiresAt int64) error {
	return am.store.GrantBucketAccess(bucketName, userID, tenantID, permissionLevel, grantedBy, expiresAt)
}

func (am *authManager) RevokeBucketAccess(ctx context.Context, bucketName, userID, tenantID string) error {
	return am.store.RevokeBucketAccess(bucketName, userID, tenantID)
}

func (am *authManager) CheckBucketAccess(ctx context.Context, bucketName, userID string) (bool, string, error) {
	return am.store.CheckBucketAccess(bucketName, userID)
}

func (am *authManager) ListBucketPermissions(ctx context.Context, bucketName string) ([]*BucketPermission, error) {
	return am.store.ListBucketPermissions(bucketName)
}

func (am *authManager) ListUserBucketPermissions(ctx context.Context, userID string) ([]*BucketPermission, error) {
	return am.store.ListUserBucketPermissions(userID)
}

// Helper methods

// generateRandomString generates a random string of specified length
func (am *authManager) generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length], nil
}

// generateAccessKeyID generates an AWS-compatible access key ID
// Format: AKIA + 16 random uppercase alphanumeric characters (total 20 chars)
func (am *authManager) generateAccessKeyID() (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const randomLength = 16

	bytes := make([]byte, randomLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Convert random bytes to uppercase alphanumeric characters
	result := make([]byte, randomLength)
	for i := 0; i < randomLength; i++ {
		result[i] = charset[int(bytes[i])%len(charset)]
	}

	// AWS access keys start with AKIA (AWS Key ID Access)
	return "AKIA" + string(result), nil
}

// generateSecretAccessKey generates an AWS-compatible secret access key
// Format: 40 characters using base64 encoding (alphanumeric + / and +)
func (am *authManager) generateSecretAccessKey() (string, error) {
	// Generate 30 random bytes which will produce 40 base64 characters
	bytes := make([]byte, 30)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Use standard base64 encoding (compatible with AWS format)
	return base64.StdEncoding.EncodeToString(bytes), nil
}

// parseBasicToken parses a basic JWT-like token (MVP implementation)
func (am *authManager) parseBasicToken(token string) (*JWTClaims, error) {
	// Split token into parts (header.payload.signature)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	// Decode payload (base64)
	payload, err := base64.URLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Parse JSON
	var claims JWTClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrInvalidToken
	}

	return &claims, nil
}

// createBasicToken creates a basic JWT-like token (MVP implementation)
func (am *authManager) createBasicToken(claims JWTClaims) (string, error) {
	// Create header
	header := map[string]string{
		"typ": "JWT",
		"alg": "HS256",
	}

	headerJSON, _ := json.Marshal(header)
	headerB64 := base64.URLEncoding.EncodeToString(headerJSON)

	// Create payload
	payloadJSON, _ := json.Marshal(claims)
	payloadB64 := base64.URLEncoding.EncodeToString(payloadJSON)

	// Create signature (simplified)
	message := headerB64 + "." + payloadB64
	hash := hmac.New(sha256.New, []byte(am.config.SecretKey))
	hash.Write([]byte(message))
	signature := base64.URLEncoding.EncodeToString(hash.Sum(nil))

	return message + "." + signature, nil
}

// parseS3SignatureV4 parses AWS Signature Version 4
func (am *authManager) parseS3SignatureV4(authHeader string, r *http.Request) (*S3SignatureV4, error) {
	// Parse Authorization header: AWS4-HMAC-SHA256 Credential=..., SignedHeaders=..., Signature=...
	logrus.WithField("auth_header", authHeader).Debug("Starting parseS3SignatureV4")

	if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256") {
		logrus.Error("Authorization header does not start with AWS4-HMAC-SHA256")
		return nil, ErrInvalidSignature
	}

	// Remove the "AWS4-HMAC-SHA256 " prefix
	authHeader = strings.TrimPrefix(authHeader, "AWS4-HMAC-SHA256 ")
	authHeader = strings.TrimSpace(authHeader)

	logrus.WithField("params_string", authHeader).Debug("Extracted parameters string")

	sig := &S3SignatureV4{
		Algorithm: "AWS4-HMAC-SHA256",
		Date:      r.Header.Get("X-Amz-Date"),
		Region:    "us-east-1", // Default region
		Service:   "s3",
	}

	// Parse credential, signed headers, and signature
	// Split by "," to get each parameter (some clients don't add space after comma)
	params := strings.Split(authHeader, ",")
	logrus.WithField("params_count", len(params)).Debug("Split parameters")

	for _, param := range params {
		param = strings.TrimSpace(param) // Remove leading/trailing spaces
		kv := strings.SplitN(param, "=", 2)
		if len(kv) != 2 {
			logrus.WithField("param", param).Warn("Skipping invalid parameter")
			continue
		}

		switch kv[0] {
		case "Credential":
			sig.Credential = kv[1]
			// Extract access key and date from credential
			// Format: AccessKey/Date/Region/Service/aws4_request
			credParts := strings.Split(kv[1], "/")
			if len(credParts) >= 2 {
				sig.AccessKey = credParts[0]
				// Extract date from credential (YYYYMMDD format)
				if len(credParts[1]) >= 8 {
					sig.Date = credParts[1] // This should be YYYYMMDD
				}
			}
			if len(credParts) >= 3 {
				sig.Region = credParts[2]
			}
			if len(credParts) >= 4 {
				sig.Service = credParts[3]
			}
		case "SignedHeaders":
			sig.SignedHeaders = kv[1]
		case "Signature":
			sig.Signature = kv[1]
		}
	}

	// If Date wasn't set from credential, try X-Amz-Date header
	if sig.Date == "" || len(sig.Date) < 8 {
		amzDate := r.Header.Get("X-Amz-Date")
		if len(amzDate) >= 8 {
			sig.Date = amzDate[:8] // Extract YYYYMMDD
		}
	}

	logrus.WithFields(logrus.Fields{
		"access_key":     sig.AccessKey,
		"date":           sig.Date,
		"region":         sig.Region,
		"service":        sig.Service,
		"signed_headers": sig.SignedHeaders,
		"credential":     sig.Credential,
	}).Info("Parsed SigV4 signature")

	return sig, nil
}

// parseS3SignatureV2 parses AWS Signature Version 2
func (am *authManager) parseS3SignatureV2(authHeader string, r *http.Request) (*S3SignatureV2, error) {
	// Parse Authorization header: AWS AccessKey:Signature
	if !strings.HasPrefix(authHeader, "AWS ") {
		return nil, ErrInvalidSignature
	}

	parts := strings.SplitN(authHeader[4:], ":", 2)
	if len(parts) != 2 {
		return nil, ErrInvalidSignature
	}

	return &S3SignatureV2{
		AccessKey: parts[0],
		Signature: parts[1],
	}, nil
}

// verifyS3SignatureV4 verifies AWS Signature Version 4
func (am *authManager) verifyS3SignatureV4(r *http.Request, sig *S3SignatureV4, secretKey string) bool {
	// AWS SigV4 signature verification
	// Reference: https://docs.aws.amazon.com/general/latest/gr/signature-version-4.html

	// Create canonical request
	canonicalRequest := am.createCanonicalRequest(r, sig.SignedHeaders)
	canonicalRequestHash := fmt.Sprintf("%x", sha256.Sum256([]byte(canonicalRequest)))

	// Create string to sign
	// Format: Algorithm + "\n" + RequestDateTime + "\n" + CredentialScope + "\n" + HashedCanonicalRequest
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s/%s/%s/aws4_request\n%s",
		r.Header.Get("X-Amz-Date"),
		sig.Date,
		sig.Region,
		sig.Service,
		canonicalRequestHash)

	// Calculate signature
	calculatedSig := am.calculateSignatureV4(stringToSign, secretKey, sig.Date, sig.Region, sig.Service)

	// Debug logging
	logrus.WithFields(logrus.Fields{
		"received_signature":     sig.Signature,
		"calculated_signature":   calculatedSig,
		"access_key":             sig.AccessKey,
		"signed_headers":         sig.SignedHeaders,
		"date":                   sig.Date,
		"region":                 sig.Region,
		"service":                sig.Service,
		"x_amz_date":             r.Header.Get("X-Amz-Date"),
		"canonical_request_hash": canonicalRequestHash,
		"match":                  calculatedSig == sig.Signature,
	}).Info("SigV4 verification details")

	return calculatedSig == sig.Signature
}

// verifyS3SignatureV2 verifies AWS Signature Version 2
func (am *authManager) verifyS3SignatureV2(r *http.Request, sig *S3SignatureV2, secretKey string) bool {
	// Simplified signature verification for MVP
	stringToSign := am.createStringToSignV2(r)

	// Calculate signature
	hash := hmac.New(sha256.New, []byte(secretKey))
	hash.Write([]byte(stringToSign))
	calculatedSig := base64.StdEncoding.EncodeToString(hash.Sum(nil))

	return calculatedSig == sig.Signature
}

// uriEncode encodes a URI path according to AWS SigV4 requirements (RFC 3986)
// It encodes all characters except: A-Z, a-z, 0-9, hyphen (-), underscore (_), period (.), tilde (~), and forward slash (/)
func uriEncode(path string) string {
	if path == "" {
		return "/"
	}

	// AWS SigV4 requires double encoding for some scenarios, but for the canonical URI
	// we use single encoding with specific rules:
	// - Encode all characters except: A-Z a-z 0-9 - _ . ~ /
	// - Space should be encoded as %20 (not +)
	// - Forward slashes / are NOT encoded

	var encoded strings.Builder
	for i := 0; i < len(path); i++ {
		c := path[i]
		// Check if character should not be encoded
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' || c == '/' {
			encoded.WriteByte(c)
		} else {
			// Encode the character as %XX
			encoded.WriteString(fmt.Sprintf("%%%02X", c))
		}
	}
	return encoded.String()
}

// createCanonicalRequest creates canonical request for SigV4
func (am *authManager) createCanonicalRequest(r *http.Request, signedHeaders string) string {
	// AWS SigV4 Canonical Request format:
	// HTTPMethod + "\n" +
	// CanonicalURI + "\n" +
	// CanonicalQueryString + "\n" +
	// CanonicalHeaders + "\n" +
	// SignedHeaders + "\n" +
	// HashedPayload

	method := r.Method

	// Use the already-encoded path from the request
	// AWS SigV4 expects the URI to be encoded according to RFC 3986
	uri := r.URL.EscapedPath()
	if uri == "" {
		uri = "/"
	}
	// Ensure proper encoding for SigV4
	uri = uriEncode(r.URL.Path)

	// Canonical Query String - sorted by key
	queryString := ""
	if r.URL.RawQuery != "" {
		values, _ := url.ParseQuery(r.URL.RawQuery)
		var keys []string
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var pairs []string
		for _, k := range keys {
			vals := values[k]
			sort.Strings(vals)
			for _, v := range vals {
				pairs = append(pairs, url.QueryEscape(k)+"="+url.QueryEscape(v))
			}
		}
		queryString = strings.Join(pairs, "&")
	}

	// Canonical Headers - must be lowercase and sorted
	canonicalHeaders := ""
	if signedHeaders != "" {
		headerNames := strings.Split(signedHeaders, ";")
		for _, name := range headerNames {
			headerName := strings.ToLower(strings.TrimSpace(name))

			var value string
			// Special handling for 'host' header
			if headerName == "host" {
				value = r.Host
			} else {
				value = r.Header.Get(headerName)
			}

			if value != "" {
				// Trim and normalize whitespace
				value = strings.TrimSpace(value)
				canonicalHeaders += headerName + ":" + value + "\n"
			}
		}
	}

	// Payload hash
	payloadHash := r.Header.Get("X-Amz-Content-Sha256")
	if payloadHash == "" {
		payloadHash = "UNSIGNED-PAYLOAD"
	}

	canonicalRequest := method + "\n" +
		uri + "\n" +
		queryString + "\n" +
		canonicalHeaders + "\n" +
		signedHeaders + "\n" +
		payloadHash

	logrus.WithFields(logrus.Fields{
		"method":            method,
		"uri":               uri,
		"query":             queryString,
		"canonical_headers": strings.ReplaceAll(canonicalHeaders, "\n", "\\n"),
		"signed_headers":    signedHeaders,
		"payload_hash":      payloadHash,
	}).Info("Canonical request components")

	return canonicalRequest
}

// createStringToSignV2 creates string to sign for SigV2
func (am *authManager) createStringToSignV2(r *http.Request) string {
	// Simplified string to sign for SigV2
	method := r.Method
	contentMD5 := r.Header.Get("Content-MD5")
	contentType := r.Header.Get("Content-Type")
	date := r.Header.Get("Date")

	resource := "/"
	if r.URL != nil {
		resource = r.URL.Path
		if resource == "" {
			resource = "/"
		}
	}

	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s",
		method, contentMD5, contentType, date, resource)
}

// calculateSignatureV4 calculates AWS SigV4 signature
func (am *authManager) calculateSignatureV4(stringToSign, secretKey, date, region, service string) string {
	// AWS SigV4 signing key derivation
	// Reference: https://docs.aws.amazon.com/general/latest/gr/signature-v4-examples.html

	// Extract date in YYYYMMDD format
	dateStamp := date
	if len(date) > 8 {
		dateStamp = date[:8]
	}

	// Step 1: DateKey = HMAC-SHA256("AWS4" + Secret, Date)
	dateKey := hmacSHA256([]byte("AWS4"+secretKey), []byte(dateStamp))

	// Step 2: DateRegionKey = HMAC-SHA256(DateKey, Region)
	dateRegionKey := hmacSHA256(dateKey, []byte(region))

	// Step 3: DateRegionServiceKey = HMAC-SHA256(DateRegionKey, Service)
	dateRegionServiceKey := hmacSHA256(dateRegionKey, []byte(service))

	// Step 4: SigningKey = HMAC-SHA256(DateRegionServiceKey, "aws4_request")
	signingKey := hmacSHA256(dateRegionServiceKey, []byte("aws4_request"))

	// Step 5: Signature = HMAC-SHA256(SigningKey, StringToSign)
	signature := hmacSHA256(signingKey, []byte(stringToSign))

	return hex.EncodeToString(signature)
}

// hmacSHA256 helper function
func hmacSHA256(key, data []byte) []byte {
	hash := hmac.New(sha256.New, key)
	hash.Write(data)
	return hash.Sum(nil)
}

// GetUserFromContext extracts the authenticated user from the request context
func GetUserFromContext(ctx context.Context) (*User, bool) {
	user, ok := ctx.Value("user").(*User)
	return user, ok
}

// GetUserIDFromContext extracts the user ID from the request context
func GetUserIDFromContext(ctx context.Context) string {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return ""
	}
	return user.ID
}

// GetTenantIDFromContext extracts the tenant ID from the request context
func GetTenantIDFromContext(ctx context.Context) string {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return ""
	}
	return user.TenantID
}

// IsAdminUser checks if the user in context has admin role
func IsAdminUser(ctx context.Context) bool {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return false
	}
	for _, role := range user.Roles {
		if role == "admin" {
			return true
		}
	}
	return false
}

// writeS3Error writes an S3-compatible XML error response
func writeS3Error(w http.ResponseWriter, r *http.Request, code, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(statusCode)

	type S3Error struct {
		XMLName   xml.Name `xml:"Error"`
		Code      string   `xml:"Code"`
		Message   string   `xml:"Message"`
		Resource  string   `xml:"Resource,omitempty"`
		RequestId string   `xml:"RequestId,omitempty"`
	}

	errorResponse := S3Error{
		Code:      code,
		Message:   message,
		Resource:  r.URL.Path,
		RequestId: r.Header.Get("X-Request-ID"),
	}

	xml.NewEncoder(w).Encode(errorResponse)
}

// CheckRateLimit checks if login is allowed from given IP address
func (am *authManager) CheckRateLimit(ip string) bool {
	return am.rateLimiter.AllowLogin(ip)
}

// IsAccountLocked checks if an account is currently locked
// Returns: (isLocked, lockedUntilTimestamp, error)
func (am *authManager) IsAccountLocked(ctx context.Context, userID string) (bool, int64, error) {
	failedAttempts, lockedUntil, err := am.store.GetAccountLockStatus(userID)
	if err != nil {
		return false, 0, err
	}

	now := time.Now().Unix()

	// Check if account is locked
	if lockedUntil > 0 && now < lockedUntil {
		return true, lockedUntil, nil
	}

	// Auto-unlock if lock period has expired
	if lockedUntil > 0 && now >= lockedUntil {
		am.store.UnlockAccount(userID)
		logrus.WithFields(logrus.Fields{
			"user_id": userID,
		}).Info("Account auto-unlocked after lock period expired")
		return false, 0, nil
	}

	// Note: Account locking due to failed attempts is now handled in RecordFailedLogin
	// to ensure accurate attempt counting
	_ = failedAttempts // Silence unused variable warning

	return false, 0, nil
}

// LockAccount manually locks a user account
func (am *authManager) LockAccount(ctx context.Context, userID string) error {
	// Get lockout duration from settings (default: 900 seconds = 15 minutes)
	lockDuration := int64(15 * 60) // 15 minutes in seconds (default)
	if am.settingsManager != nil {
		if duration, err := am.settingsManager.GetInt("security.lockout_duration"); err == nil {
			lockDuration = int64(duration)
		}
	}

	err := am.store.LockAccount(userID, lockDuration)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"user_id":          userID,
		"duration_minutes": lockDuration / 60,
	}).Info("Account manually locked")

	// Log audit event for user blocked
	user, _ := am.store.GetUserByID(userID)
	if user != nil {
		am.logAuditEvent(ctx, &audit.AuditEvent{
			TenantID:     user.TenantID,
			UserID:       user.ID,
			Username:     user.Username,
			EventType:    audit.EventTypeUserBlocked,
			ResourceType: audit.ResourceTypeUser,
			ResourceID:   user.ID,
			ResourceName: user.Username,
			Action:       audit.ActionBlock,
			Status:       audit.StatusSuccess,
			Details: map[string]interface{}{
				"duration_minutes": lockDuration / 60,
			},
		})
	}

	return nil
}

// UnlockAccount unlocks a user account
// Can only be done by Global Admin or Tenant Admin (for users in same tenant)
func (am *authManager) UnlockAccount(ctx context.Context, adminUserID, targetUserID string) error {
	// Get admin user
	admin, err := am.store.GetUserByID(adminUserID)
	if err != nil {
		return fmt.Errorf("admin user not found: %w", err)
	}

	// Get target user
	target, err := am.store.GetUserByID(targetUserID)
	if err != nil {
		return fmt.Errorf("target user not found: %w", err)
	}

	// Check permissions
	isGlobalAdmin := admin.TenantID == "" && containsRole(admin.Roles, "admin")
	isTenantAdmin := admin.TenantID != "" && admin.TenantID == target.TenantID && containsRole(admin.Roles, "admin")

	if !isGlobalAdmin && !isTenantAdmin {
		return fmt.Errorf("insufficient permissions to unlock account")
	}

	// Unlock account
	err = am.store.UnlockAccount(targetUserID)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"admin_user_id":   adminUserID,
		"target_user_id":  targetUserID,
		"is_global_admin": isGlobalAdmin,
	}).Info("Account unlocked by admin")

	// Log audit event for user unblocked
	am.logAuditEvent(ctx, &audit.AuditEvent{
		TenantID:     target.TenantID,
		UserID:       admin.ID,
		Username:     admin.Username,
		EventType:    audit.EventTypeUserUnblocked,
		ResourceType: audit.ResourceTypeUser,
		ResourceID:   target.ID,
		ResourceName: target.Username,
		Action:       audit.ActionUnblock,
		Status:       audit.StatusSuccess,
		Details: map[string]interface{}{
			"unlocked_by_admin": admin.Username,
		},
	})

	return nil
}

// RecordFailedLogin records a failed login attempt
func (am *authManager) RecordFailedLogin(ctx context.Context, userID, ip string) error {
	logrus.WithFields(logrus.Fields{
		"user_id": userID,
		"ip":      ip,
	}).Debug("RecordFailedLogin called")

	// Record in rate limiter
	if ip != "" {
		am.rateLimiter.RecordFailedAttempt(ip)
	}

	// Increment failed attempts in database
	err := am.store.IncrementFailedLoginAttempts(userID)
	if err != nil {
		return err
	}

	// Get current attempt count
	failedAttempts, lockedUntil, err := am.store.GetAccountLockStatus(userID)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"user_id":         userID,
		"ip":              ip,
		"failed_attempts": failedAttempts,
		"locked_until":    lockedUntil,
	}).Warn("Failed login attempt recorded")

	// Get max failed attempts from settings (default: 5)
	maxFailedAttempts := 5
	if am.settingsManager != nil {
		if max, err := am.settingsManager.GetInt("security.max_failed_attempts"); err == nil {
			maxFailedAttempts = max
		}
	}

	// Check if account should be locked due to failed attempts
	if failedAttempts >= maxFailedAttempts {
		// Get lockout duration from settings (default: 900 seconds = 15 minutes)
		lockDuration := int64(15 * 60) // 15 minutes in seconds
		if am.settingsManager != nil {
			if duration, err := am.settingsManager.GetInt("security.lockout_duration"); err == nil {
				lockDuration = int64(duration)
			}
		}

		lockErr := am.store.LockAccount(userID, lockDuration)
		if lockErr != nil {
			logrus.WithError(lockErr).Error("Failed to lock account")
		} else {
			newLockedUntil := time.Now().Unix() + lockDuration
			logrus.WithFields(logrus.Fields{
				"user_id":               userID,
				"attempts":              failedAttempts,
				"max_failed_attempts":   maxFailedAttempts,
				"lockout_duration_mins": lockDuration / 60,
				"locked_until":          time.Unix(newLockedUntil, 0).Format(time.RFC3339),
			}).Warn("Account locked due to failed login attempts")

			// Notify via callback if set
			logrus.WithField("callback_set", am.userLockedCallback != nil).Info("Checking user locked callback")
			if am.userLockedCallback != nil {
				user, err := am.store.GetUserByID(userID)
				if err != nil {
					logrus.WithError(err).Error("Failed to get user for callback")
				} else if user != nil {
					logrus.WithField("user_id", userID).Info("Calling user locked callback")
					am.userLockedCallback(user)
				} else {
					logrus.Warn("User is nil, cannot call callback")
				}
			} else {
				logrus.Warn("User locked callback is NOT set")
			}
		}
	}

	// Log audit event for failed login
	user, _ := am.store.GetUserByID(userID)
	if user != nil {
		am.logAuditEvent(ctx, &audit.AuditEvent{
			TenantID:  user.TenantID,
			UserID:    user.ID,
			Username:  user.Username,
			EventType: audit.EventTypeLoginFailed,
			Action:    audit.ActionLogin,
			Status:    audit.StatusFailed,
			IPAddress: ip,
			Details: map[string]interface{}{
				"failed_attempts": failedAttempts,
			},
		})
	}

	return nil
}

// RecordSuccessfulLogin records a successful login and resets failed attempts
func (am *authManager) RecordSuccessfulLogin(ctx context.Context, userID string) error {
	// Reset failed attempts
	err := am.store.ResetFailedLoginAttempts(userID)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"user_id": userID,
	}).Info("Successful login - failed attempts reset")

	// Note: Audit logging is now handled by the HTTP handler (console_api.go)
	// which has access to IP address and User Agent from the HTTP request

	return nil
}

// containsRole checks if a role exists in a slice of roles
func containsRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// =============================================================================
// Two-Factor Authentication (2FA) Methods
// =============================================================================

// Setup2FA initiates 2FA setup for a user
// Returns the TOTP secret, QR code, and URL for the user to scan
func (m *authManager) Setup2FA(ctx context.Context, userID string) (*TOTPSetup, error) {
	// Get user to retrieve username
	user, err := m.store.GetUserByID(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Generate TOTP secret
	issuer := "MaxIOFS"
	setup, err := Generate2FASecret(user.Username, issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to generate 2FA secret: %w", err)
	}

	return setup, nil
}

// Enable2FA enables 2FA for a user after verifying the TOTP code
// Returns backup codes that the user MUST save
func (m *authManager) Enable2FA(ctx context.Context, userID, code string, secret string) ([]string, error) {
	// Verify the TOTP code
	if !VerifyTOTPCode(secret, code) {
		return nil, fmt.Errorf("invalid verification code")
	}

	// Generate backup codes
	backupCodes, err := GenerateBackupCodes()
	if err != nil {
		return nil, fmt.Errorf("failed to generate backup codes: %w", err)
	}

	// Hash backup codes for storage
	hashedCodes := make([]string, len(backupCodes))
	for i, code := range backupCodes {
		hashed, err := HashBackupCode(code)
		if err != nil {
			return nil, fmt.Errorf("failed to hash backup code: %w", err)
		}
		hashedCodes[i] = hashed
	}

	// Update user in database
	err = m.store.Enable2FA(ctx, userID, secret, hashedCodes)
	if err != nil {
		return nil, fmt.Errorf("failed to enable 2FA: %w", err)
	}

	// Log audit event for 2FA enabled
	user, _ := m.store.GetUserByID(userID)
	if user != nil {
		m.logAuditEvent(ctx, &audit.AuditEvent{
			TenantID:     user.TenantID,
			UserID:       user.ID,
			Username:     user.Username,
			EventType:    audit.EventType2FAEnabled,
			ResourceType: audit.ResourceTypeUser,
			ResourceID:   user.ID,
			ResourceName: user.Username,
			Action:       audit.ActionEnable,
			Status:       audit.StatusSuccess,
		})
	}

	return backupCodes, nil
}

// Disable2FA disables 2FA for a user
// Only global admins can disable 2FA for other users
func (m *authManager) Disable2FA(ctx context.Context, userID, requestingUserID string, isGlobalAdmin bool) error {
	// If disabling for another user, must be global admin
	if userID != requestingUserID && !isGlobalAdmin {
		return fmt.Errorf("only global administrators can disable 2FA for other users")
	}

	// Get user info before disabling
	user, err := m.store.GetUserByID(userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	err = m.store.Disable2FA(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to disable 2FA: %w", err)
	}

	// Log audit event for 2FA disabled
	details := make(map[string]interface{})
	if userID != requestingUserID {
		requestingUser, _ := m.store.GetUserByID(requestingUserID)
		if requestingUser != nil {
			details["disabled_by_admin"] = requestingUser.Username
		}
	}

	m.logAuditEvent(ctx, &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventType2FADisabled,
		ResourceType: audit.ResourceTypeUser,
		ResourceID:   user.ID,
		ResourceName: user.Username,
		Action:       audit.ActionDisable,
		Status:       audit.StatusSuccess,
		Details:      details,
	})

	return nil
}

// Verify2FACode verifies a TOTP code or backup code for a user
// Returns true if valid, false otherwise
func (m *authManager) Verify2FACode(ctx context.Context, userID, code string) (bool, error) {
	// Get user with 2FA data
	user, err := m.store.GetUserWith2FA(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("failed to get user: %w", err)
	}

	if !user.TwoFactorEnabled {
		return false, fmt.Errorf("2FA is not enabled for this user")
	}

	// Check if it's a backup code format
	if IsBackupCode(code) {
		// Verify against backup codes
		for i, hashedCode := range user.BackupCodes {
			if VerifyBackupCode(code, hashedCode) {
				// Check if already used
				for _, usedCode := range user.BackupCodesUsed {
					if usedCode == hashedCode {
						return false, fmt.Errorf("backup code already used")
					}
				}

				// Mark as used
				err = m.store.MarkBackupCodeUsed(ctx, userID, i)
				if err != nil {
					return false, fmt.Errorf("failed to mark backup code as used: %w", err)
				}

				return true, nil
			}
		}
		return false, nil
	}

	// Verify TOTP code
	return VerifyTOTPCode(user.TwoFactorSecret, code), nil
}

// RegenerateBackupCodes generates new backup codes for a user
func (m *authManager) RegenerateBackupCodes(ctx context.Context, userID string) ([]string, error) {
	// Generate new backup codes
	backupCodes, err := GenerateBackupCodes()
	if err != nil {
		return nil, fmt.Errorf("failed to generate backup codes: %w", err)
	}

	// Hash backup codes
	hashedCodes := make([]string, len(backupCodes))
	for i, code := range backupCodes {
		hashed, err := HashBackupCode(code)
		if err != nil {
			return nil, fmt.Errorf("failed to hash backup code: %w", err)
		}
		hashedCodes[i] = hashed
	}

	// Update user in database
	err = m.store.UpdateBackupCodes(ctx, userID, hashedCodes)
	if err != nil {
		return nil, fmt.Errorf("failed to update backup codes: %w", err)
	}

	return backupCodes, nil
}

// Get2FAStatus returns the 2FA status for a user
func (m *authManager) Get2FAStatus(ctx context.Context, userID string) (bool, int64, error) {
	user, err := m.store.GetUserByID(userID)
	if err != nil {
		return false, 0, fmt.Errorf("failed to get user: %w", err)
	}

	return user.TwoFactorEnabled, user.TwoFactorSetupAt, nil
}
