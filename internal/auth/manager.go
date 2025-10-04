package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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
	Username    string            `json:"username,omitempty"` // For console login
	Password    string            `json:"password,omitempty"` // Hashed password for console login
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
	store  *SQLiteStore
}

// NewManager creates a new authentication manager with SQLite backend
func NewManager(cfg config.AuthConfig, dataDir string) Manager {
	// Initialize SQLite store
	store, err := NewSQLiteStore(dataDir)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to initialize SQLite auth store")
	}

	manager := &authManager{
		config: cfg,
		store:  store,
	}

	// Create default admin user if not exists (ONLY admin, no default keys)
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
			logrus.Info("Created default admin user (username: admin, password: admin)")
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

	// Validate password with bcrypt
	if !VerifyPassword(password, user.Password) {
		return nil, ErrInvalidCredentials
	}

	// Check user status
	if user.Status != UserStatusActive {
		return nil, ErrUserInactive
	}

	return user, nil
}

// hashPassword creates a simple SHA256 hash of the password
// Note: In production, use bcrypt or argon2
func hashPassword(password string) string {
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
	return am.store.CreateUser(user)
}

func (am *authManager) UpdateUser(ctx context.Context, user *User) error {
	// Update timestamp
	user.UpdatedAt = time.Now().Unix()

	// Update user in database
	return am.store.UpdateUser(user)
}

func (am *authManager) DeleteUser(ctx context.Context, userID string) error {
	// Don't allow deleting admin user (last resort account)
	if userID == "admin" {
		return fmt.Errorf("cannot delete admin user")
	}

	// Soft delete user in database (also soft deletes associated access keys)
	return am.store.DeleteUser(userID)
}

func (am *authManager) GetUser(ctx context.Context, userID string) (*User, error) {
	return am.store.GetUserByID(userID)
}

func (am *authManager) ListUsers(ctx context.Context) ([]User, error) {
	usersPtrs, err := am.store.ListUsers()
	if err != nil {
		return nil, err
	}

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

	// Store in database
	if err := am.store.CreateAccessKey(accessKey); err != nil {
		return nil, err
	}

	return accessKey, nil
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

			// Try to validate request
			user, err := am.ValidateS3Signature(r.Context(), r)
			if err != nil {
				logrus.WithFields(logrus.Fields{
					"method": r.Method,
					"path":   r.URL.Path,
					"error":  err.Error(),
					"auth":   r.Header.Get("Authorization"),
				}).Warn("Authentication failed")
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
	// Split by ", " to get each parameter
	params := strings.Split(authHeader, ", ")
	logrus.WithField("params_count", len(params)).Debug("Split parameters")

	for _, param := range params {
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
