package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

// Common authentication errors
var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUserNotFound         = errors.New("user not found")
	ErrGroupNotFound        = errors.New("group not found")
	ErrUserInactive         = errors.New("user is not active")
	ErrAccessDenied         = errors.New("access denied")
	ErrInvalidToken         = errors.New("invalid token")
	ErrTokenExpired         = errors.New("token expired")
	ErrMissingSignature     = errors.New("missing signature")
	ErrInvalidSignature     = errors.New("invalid signature")
	ErrTimestampSkew        = errors.New("timestamp skew too large")
	ErrStorageQuotaExceeded = errors.New("storage quota exceeded")
	ErrBucketQuotaExceeded  = errors.New("bucket quota exceeded")
)

// Role represents a user role
type Role struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Policies    []string          `json:"policies"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   int64             `json:"created_at"`
	UpdatedAt   int64             `json:"updated_at"`
}

// Policy represents an access policy
type Policy struct {
	Version   string      `json:"Version"`
	Statement []Statement `json:"Statement"`
}

// Statement represents a policy statement
type Statement struct {
	Sid       string                 `json:"Sid,omitempty"`
	Effect    string                 `json:"Effect"` // Allow, Deny
	Principal map[string]interface{} `json:"Principal,omitempty"`
	Action    interface{}            `json:"Action"`   // string or []string
	Resource  interface{}            `json:"Resource"` // string or []string
	Condition map[string]interface{} `json:"Condition,omitempty"`
}

// Permission represents a specific permission
type Permission struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"` // allow, deny
}

// S3SignatureV4 represents AWS Signature Version 4 components
type S3SignatureV4 struct {
	Algorithm     string
	Credential    string
	SignedHeaders string
	Signature     string
	Date          string
	Region        string
	Service       string
	AccessKey     string
	SecretKey     string
	SessionToken  string
	PayloadHash   string
}

// S3SignatureV2 represents AWS Signature Version 2 components
type S3SignatureV2 struct {
	AccessKey    string
	Signature    string
	StringToSign string
	SecretKey    string
}

// JWTClaims represents JWT token claims
type JWTClaims struct {
	jwt.RegisteredClaims
	UserID      string   `json:"user_id"`
	TenantID    string   `json:"tenant_id,omitempty"`
	AccessKey   string   `json:"access_key"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
	// TokenType distinguishes "access" tokens (used in Authorization headers)
	// from "refresh" tokens (used only in POST /auth/refresh) and "download"
	// tokens (used only to fetch one named object).
	// Empty or missing means "access" (backward-compatible with pre-v1.1 tokens).
	TokenType string `json:"token_type,omitempty"`
	// Resource names the single thing a token may be used for. Only download
	// tokens carry it: the token travels in a URL, so it must not be worth more
	// than the one object it was minted for.
	Resource string `json:"resource,omitempty"`
}

// TokenTypeDownload marks a token that may only fetch one named object.
//
// A browser cannot set an Authorization header when it navigates, and
// navigating is the only way it will stream a large file to disk instead of
// holding it in memory. So the credential has to travel in the URL — which is
// logged, kept in history and handed to the next page in Referer. That makes
// the scope the whole design: one object, two minutes, no other endpoint.
const TokenTypeDownload = "download"

// DownloadTokenTTL is deliberately short. The token is redeemed immediately by
// the navigation that follows minting it; it does not need to outlive that, and
// the transfer itself is unaffected once the response has begun.
const DownloadTokenTTL = 120

// TokenPair holds a short-lived access token and a longer-lived refresh token.
// ExpiresIn contains the access-token TTL in seconds so the client can schedule
// proactive refresh calls without parsing the JWT.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // access-token TTL, seconds
	TokenType    string `json:"token_type"` // always "Bearer"
}

// AuthContext represents authentication context in request
type AuthContext struct {
	User        *User
	TenantID    string
	AccessKey   string
	Method      string // jwt, signature_v4, signature_v2
	Permissions []Permission
	RequestID   string
}

// SessionInfo represents user session information
type SessionInfo struct {
	SessionID  string            `json:"session_id"`
	UserID     string            `json:"user_id"`
	AccessKey  string            `json:"access_key"`
	CreatedAt  int64             `json:"created_at"`
	ExpiresAt  int64             `json:"expires_at"`
	LastAccess int64             `json:"last_access"`
	IPAddress  string            `json:"ip_address"`
	UserAgent  string            `json:"user_agent"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// UserGroup represents a group of users
type UserGroup struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Members     []string          `json:"members"` // user IDs
	Roles       []string          `json:"roles"`
	Policies    []string          `json:"policies"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   int64             `json:"created_at"`
	UpdatedAt   int64             `json:"updated_at"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID        string            `json:"id"`
	Timestamp int64             `json:"timestamp"`
	UserID    string            `json:"user_id"`
	AccessKey string            `json:"access_key"`
	Action    string            `json:"action"`
	Resource  string            `json:"resource"`
	Result    string            `json:"result"` // success, failure
	IPAddress string            `json:"ip_address"`
	UserAgent string            `json:"user_agent"`
	Details   map[string]string `json:"details,omitempty"`
}

// LoginAttempt represents a login attempt
type LoginAttempt struct {
	ID         string `json:"id"`
	Timestamp  int64  `json:"timestamp"`
	AccessKey  string `json:"access_key"`
	IPAddress  string `json:"ip_address"`
	UserAgent  string `json:"user_agent"`
	Success    bool   `json:"success"`
	FailReason string `json:"fail_reason,omitempty"`
}

// Constants for user status
const (
	UserStatusActive    = "active"
	UserStatusInactive  = "inactive"
	UserStatusSuspended = "suspended"
	UserStatusLocked    = "locked"
)

// Constants for access key status
const (
	AccessKeyStatusActive   = "active"
	AccessKeyStatusInactive = "inactive"
)

// Constants for policy effects
const (
	EffectAllow = "Allow"
	EffectDeny  = "Deny"
)

// Constants for authentication methods
const (
	AuthMethodJWT         = "jwt"
	AuthMethodSignatureV4 = "signature_v4"
	AuthMethodSignatureV2 = "signature_v2"
	AuthMethodAPIKey      = "api_key"
)

// Constants for default roles
const (
	RoleAdmin       = "admin"
	RoleTenantAdmin = "tenant-admin"
	RoleUser        = "user"
	RoleReadOnly    = "readonly"
	RoleGuest       = "guest"
)

// Constants for S3 actions
const (
	// Service actions
	ActionListAllMyBuckets = "s3:ListAllMyBuckets"

	// Bucket actions
	ActionCreateBucket          = "s3:CreateBucket"
	ActionDeleteBucket          = "s3:DeleteBucket"
	ActionListBucket            = "s3:ListBucket"
	ActionListBucketVersions    = "s3:ListBucketVersions"
	ActionGetBucketLocation     = "s3:GetBucketLocation"
	ActionGetBucketVersioning   = "s3:GetBucketVersioning"
	ActionPutBucketVersioning   = "s3:PutBucketVersioning"
	ActionGetBucketPolicy       = "s3:GetBucketPolicy"
	ActionPutBucketPolicy       = "s3:PutBucketPolicy"
	ActionDeleteBucketPolicy    = "s3:DeleteBucketPolicy"
	ActionGetBucketLifecycle    = "s3:GetBucketLifecycle"
	ActionPutBucketLifecycle    = "s3:PutBucketLifecycle"
	ActionDeleteBucketLifecycle = "s3:DeleteBucketLifecycle"
	ActionGetBucketCORS         = "s3:GetBucketCORS"
	ActionPutBucketCORS         = "s3:PutBucketCORS"
	ActionDeleteBucketCORS      = "s3:DeleteBucketCORS"
	ActionGetBucketAcl          = "s3:GetBucketAcl"
	ActionPutBucketAcl          = "s3:PutBucketAcl"
	ActionGetBucketTagging      = "s3:GetBucketTagging"
	ActionPutBucketTagging      = "s3:PutBucketTagging"
	ActionDeleteBucketTagging   = "s3:DeleteBucketTagging"

	// The bucket's Object Lock configuration. Separate from the per-object
	// retention actions: this one sets the DEFAULT retention every new upload
	// inherits, so writing it locks data that does not exist yet.
	ActionGetBucketObjectLockConfiguration = "s3:GetBucketObjectLockConfiguration"
	ActionPutBucketObjectLockConfiguration = "s3:PutBucketObjectLockConfiguration"

	// Bucket subresources that had no action of their own and were therefore
	// guarded by an unrelated one — encryption by the CORS permission, logging
	// and website by lifecycle, notification by bucket-policy, and so on. A
	// single grant then silently conferred several unrelated configurations,
	// and the borrowed name gave no way to grant one without the other.
	//
	// Replication deserves its own for a second reason: a replication rule
	// names where a bucket's contents are copied TO, so writing one is the
	// power to send the data somewhere else.
	ActionGetBucketReplication  = "s3:GetBucketReplication"
	ActionPutBucketReplication  = "s3:PutBucketReplication"
	ActionGetBucketEncryption   = "s3:GetEncryptionConfiguration"
	ActionPutBucketEncryption   = "s3:PutEncryptionConfiguration"
	ActionGetBucketLogging      = "s3:GetBucketLogging"
	ActionPutBucketLogging      = "s3:PutBucketLogging"
	ActionGetBucketWebsite      = "s3:GetBucketWebsite"
	ActionPutBucketWebsite      = "s3:PutBucketWebsite"
	ActionDeleteBucketWebsite   = "s3:DeleteBucketWebsite"
	ActionGetBucketNotification = "s3:GetBucketNotification"
	ActionPutBucketNotification = "s3:PutBucketNotification"
	ActionGetBucketPublicAccess = "s3:GetBucketPublicAccessBlock"
	ActionPutBucketPublicAccess = "s3:PutBucketPublicAccessBlock"
	ActionGetBucketOwnership    = "s3:GetBucketOwnershipControls"
	ActionPutBucketOwnership    = "s3:PutBucketOwnershipControls"
	ActionGetBucketInventory    = "s3:GetInventoryConfiguration"
	ActionPutBucketInventory    = "s3:PutInventoryConfiguration"
	ActionGetAccelerate         = "s3:GetAccelerateConfiguration"
	ActionPutAccelerate         = "s3:PutAccelerateConfiguration"
	ActionGetRequestPayment     = "s3:GetBucketRequestPayment"
	ActionPutRequestPayment     = "s3:PutBucketRequestPayment"

	// ListBucketMultipartUploads covers GET /bucket?uploads (the in-flight
	// multipart uploads of a bucket, not the parts of one upload).
	ActionListBucketMultipartUploads = "s3:ListBucketMultipartUploads"

	// Object actions
	ActionGetObject           = "s3:GetObject"
	ActionPutObject           = "s3:PutObject"
	ActionDeleteObject        = "s3:DeleteObject"
	ActionGetObjectVersion    = "s3:GetObjectVersion"
	ActionDeleteObjectVersion = "s3:DeleteObjectVersion"
	ActionGetObjectAcl        = "s3:GetObjectAcl"
	ActionPutObjectAcl        = "s3:PutObjectAcl"
	ActionGetObjectTagging    = "s3:GetObjectTagging"
	ActionPutObjectTagging    = "s3:PutObjectTagging"
	ActionDeleteObjectTagging = "s3:DeleteObjectTagging"
	ActionRestoreObject       = "s3:RestoreObject"

	// Object Lock actions
	ActionGetObjectRetention        = "s3:GetObjectRetention"
	ActionPutObjectRetention        = "s3:PutObjectRetention"
	ActionBypassGovernanceRetention = "s3:BypassGovernanceRetention"
	ActionGetObjectLegalHold        = "s3:GetObjectLegalHold"
	ActionPutObjectLegalHold        = "s3:PutObjectLegalHold"

	// Multipart actions
	ActionListMultipartUploadParts = "s3:ListMultipartUploadParts"
	ActionAbortMultipartUpload     = "s3:AbortMultipartUpload"
)
