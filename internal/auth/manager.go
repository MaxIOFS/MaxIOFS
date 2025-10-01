package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
)

// Manager defines the interface for authentication and authorization
type Manager interface {
	// Authentication
	ValidateCredentials(ctx context.Context, accessKey, secretKey string) (*User, error)
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
	DeleteUser(ctx context.Context, userID string) error
	GetUser(ctx context.Context, accessKey string) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)

	// Access key management
	GenerateAccessKey(ctx context.Context, userID string) (*AccessKey, error)
	RevokeAccessKey(ctx context.Context, accessKey string) error
	ListAccessKeys(ctx context.Context, userID string) ([]AccessKey, error)

	// HTTP Middleware
	Middleware() func(http.Handler) http.Handler

	// Health check
	IsReady() bool
}

// User represents a user in the system
type User struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"display_name"`
	Email       string            `json:"email,omitempty"`
	Status      string            `json:"status"` // active, inactive, suspended
	Roles       []string          `json:"roles"`
	Policies    []string          `json:"policies"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   int64             `json:"created_at"`
	UpdatedAt   int64             `json:"updated_at"`
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
	config config.AuthConfig
	users  map[string]*User      // accessKey -> user mapping
	keys   map[string]*AccessKey // accessKey -> AccessKey mapping
}

// NewManager creates a new authentication manager
func NewManager(config config.AuthConfig) Manager {
	manager := &authManager{
		config: config,
		users:  make(map[string]*User),
		keys:   make(map[string]*AccessKey),
	}

	// Add default user if configured
	if config.AccessKey != "" && config.SecretKey != "" {
		defaultUser := &User{
			ID:          "default",
			DisplayName: "Default User",
			Status:      "active",
			Roles:       []string{"admin"},
			CreatedAt:   0,
			UpdatedAt:   0,
		}

		defaultKey := &AccessKey{
			AccessKeyID:     config.AccessKey,
			SecretAccessKey: config.SecretKey,
			UserID:          "default",
			Status:          "active",
			CreatedAt:       0,
		}

		manager.users[config.AccessKey] = defaultUser
		manager.keys[config.AccessKey] = defaultKey
	}

	return manager
}

// ValidateCredentials validates access/secret key credentials
func (am *authManager) ValidateCredentials(ctx context.Context, accessKey, secretKey string) (*User, error) {
	// TODO: Implement in Fase 1.4 - Authentication Manager
	if !am.config.EnableAuth {
		// Return default user when auth is disabled
		return &User{
			ID:          "anonymous",
			DisplayName: "Anonymous User",
			Status:      "active",
			Roles:       []string{"admin"},
		}, nil
	}

	// Check default credentials
	if accessKey == am.config.AccessKey && secretKey == am.config.SecretKey {
		return am.users[accessKey], nil
	}

	return nil, ErrInvalidCredentials
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

	// Get user by access key
	user, err := am.GetUser(ctx, claims.AccessKey)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GenerateJWT generates a JWT token for a user
func (am *authManager) GenerateJWT(ctx context.Context, user *User) (string, error) {
	// Find user's access key
	var accessKey string
	for key, u := range am.users {
		if u.ID == user.ID {
			accessKey = key
			break
		}
	}

	if accessKey == "" {
		return "", ErrUserNotFound
	}

	// Create claims
	now := time.Now()
	claims := JWTClaims{
		UserID:    user.ID,
		AccessKey: accessKey,
		Roles:     user.Roles,
		ExpiresAt: now.Add(24 * time.Hour).Unix(), // 24 hour expiry
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
	// TODO: Implement in Fase 1.4 - Authentication Manager
	// Auto-detect signature version and delegate to appropriate method
	if r.Header.Get("Authorization") != "" {
		if r.Header.Get("X-Amz-Algorithm") != "" {
			return am.ValidateS3SignatureV4(ctx, r)
		}
		return am.ValidateS3SignatureV2(ctx, r)
	}
	return nil, ErrMissingSignature
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
		return nil, err
	}

	// Get user by access key
	accessKey, exists := am.keys[sig.AccessKey]
	if !exists {
		return nil, ErrInvalidCredentials
	}

	user := am.users[sig.AccessKey]
	if user == nil {
		return nil, ErrUserNotFound
	}

	// Verify signature
	if !am.verifyS3SignatureV4(r, sig, accessKey.SecretAccessKey) {
		return nil, ErrInvalidSignature
	}

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

	// Get user by access key
	accessKey, exists := am.keys[sig.AccessKey]
	if !exists {
		return nil, ErrInvalidCredentials
	}

	user := am.users[sig.AccessKey]
	if user == nil {
		return nil, ErrUserNotFound
	}

	// Verify signature
	if !am.verifyS3SignatureV2(r, sig, accessKey.SecretAccessKey) {
		return nil, ErrInvalidSignature
	}

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
	if user.ID == "" {
		return fmt.Errorf("user ID is required")
	}

	// Check if user already exists
	for _, existingUser := range am.users {
		if existingUser.ID == user.ID {
			return fmt.Errorf("user already exists")
		}
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

	// For MVP, store in memory with userID as key
	// In production, users would be linked to access keys
	am.users[user.ID] = user
	return nil
}

func (am *authManager) UpdateUser(ctx context.Context, user *User) error {
	// Find and update user
	for accessKey, existingUser := range am.users {
		if existingUser.ID == user.ID {
			// Update fields
			existingUser.DisplayName = user.DisplayName
			existingUser.Email = user.Email
			existingUser.Status = user.Status
			existingUser.Roles = user.Roles
			existingUser.Policies = user.Policies
			existingUser.UpdatedAt = time.Now().Unix()

			// Update metadata
			if user.Metadata != nil {
				if existingUser.Metadata == nil {
					existingUser.Metadata = make(map[string]string)
				}
				for k, v := range user.Metadata {
					existingUser.Metadata[k] = v
				}
			}

			am.users[accessKey] = existingUser
			return nil
		}
	}

	return ErrUserNotFound
}

func (am *authManager) DeleteUser(ctx context.Context, userID string) error {
	// Find and delete user and associated keys
	for accessKey, user := range am.users {
		if user.ID == userID {
			// Don't allow deleting default user
			if userID == "default" {
				return fmt.Errorf("cannot delete default user")
			}

			// Delete user and access key
			delete(am.users, accessKey)
			delete(am.keys, accessKey)
			return nil
		}
	}

	return ErrUserNotFound
}

func (am *authManager) GetUser(ctx context.Context, accessKey string) (*User, error) {
	if user, exists := am.users[accessKey]; exists {
		return user, nil
	}
	return nil, ErrUserNotFound
}

func (am *authManager) ListUsers(ctx context.Context) ([]User, error) {
	var users []User
	for _, user := range am.users {
		users = append(users, *user)
	}
	return users, nil
}

// Access key management methods
func (am *authManager) GenerateAccessKey(ctx context.Context, userID string) (*AccessKey, error) {
	// Find user
	var user *User
	for _, u := range am.users {
		if u.ID == userID {
			user = u
			break
		}
	}

	if user == nil {
		return nil, ErrUserNotFound
	}

	// Generate new access key pair
	accessKeyID, err := am.generateRandomString(20)
	if err != nil {
		return nil, err
	}

	secretAccessKey, err := am.generateRandomString(40)
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

	// Store in memory
	am.keys[accessKeyID] = accessKey
	am.users[accessKeyID] = user

	return accessKey, nil
}

func (am *authManager) RevokeAccessKey(ctx context.Context, accessKey string) error {
	key, exists := am.keys[accessKey]
	if !exists {
		return fmt.Errorf("access key not found")
	}

	// Don't allow revoking default key
	if accessKey == am.config.AccessKey {
		return fmt.Errorf("cannot revoke default access key")
	}

	// Set status to inactive instead of deleting
	key.Status = AccessKeyStatusInactive
	am.keys[accessKey] = key

	return nil
}

func (am *authManager) ListAccessKeys(ctx context.Context, userID string) ([]AccessKey, error) {
	var keys []AccessKey
	for _, key := range am.keys {
		if key.UserID == userID {
			keys = append(keys, *key)
		}
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

			// Try to validate request
			user, err := am.ValidateS3Signature(r.Context(), r)
			if err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
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

// Helper methods

// generateRandomString generates a random string of specified length
func (am *authManager) generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length], nil
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
	if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256") {
		return nil, ErrInvalidSignature
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 {
		return nil, ErrInvalidSignature
	}

	sig := &S3SignatureV4{
		Algorithm: "AWS4-HMAC-SHA256",
		Date:      r.Header.Get("X-Amz-Date"),
		Region:    "us-east-1", // Default region
		Service:   "s3",
	}

	// Parse credential, signed headers, and signature
	params := strings.Split(parts[1], ", ")
	for _, param := range params {
		kv := strings.SplitN(param, "=", 2)
		if len(kv) != 2 {
			continue
		}

		switch kv[0] {
		case "Credential":
			sig.Credential = kv[1]
			// Extract access key from credential
			credParts := strings.Split(kv[1], "/")
			if len(credParts) > 0 {
				sig.AccessKey = credParts[0]
			}
		case "SignedHeaders":
			sig.SignedHeaders = kv[1]
		case "Signature":
			sig.Signature = kv[1]
		}
	}

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
	// Simplified signature verification for MVP
	// In production, implement full AWS SigV4 algorithm

	// Create canonical request
	canonicalRequest := am.createCanonicalRequest(r, sig.SignedHeaders)

	// Create string to sign
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		sig.Date,
		sig.Credential,
		fmt.Sprintf("%x", sha256.Sum256([]byte(canonicalRequest))))

	// Calculate signature
	calculatedSig := am.calculateSignatureV4(stringToSign, secretKey, sig.Date)

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

// createCanonicalRequest creates canonical request for SigV4
func (am *authManager) createCanonicalRequest(r *http.Request, signedHeaders string) string {
	// Simplified canonical request creation
	method := r.Method
	uri := r.URL.Path
	if uri == "" {
		uri = "/"
	}

	// Query string
	queryString := r.URL.RawQuery
	if queryString != "" {
		// Sort query parameters
		values, _ := url.ParseQuery(queryString)
		var keys []string
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var pairs []string
		for _, k := range keys {
			for _, v := range values[k] {
				pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
			}
		}
		queryString = strings.Join(pairs, "&")
	}

	// Canonical headers (simplified)
	headers := r.Header
	canonicalHeaders := ""
	if signedHeaders != "" {
		headerNames := strings.Split(signedHeaders, ";")
		for _, name := range headerNames {
			if value := headers.Get(name); value != "" {
				canonicalHeaders += fmt.Sprintf("%s:%s\n", strings.ToLower(name), value)
			}
		}
	}

	// Payload hash (simplified)
	payloadHash := "UNSIGNED-PAYLOAD"
	if hash := r.Header.Get("X-Amz-Content-Sha256"); hash != "" {
		payloadHash = hash
	}

	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		method, uri, queryString, canonicalHeaders, signedHeaders, payloadHash)
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
func (am *authManager) calculateSignatureV4(stringToSign, secretKey, date string) string {
	// Simplified signature calculation (not full AWS algorithm)
	hash := hmac.New(sha256.New, []byte("AWS4"+secretKey))
	hash.Write([]byte(stringToSign))
	return hex.EncodeToString(hash.Sum(nil))
}