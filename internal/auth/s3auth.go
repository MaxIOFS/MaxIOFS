package auth

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// S3AuthHelper provides S3-specific authentication helpers
type S3AuthHelper struct {
	manager *authManager
}

// NewS3AuthHelper creates a new S3 auth helper
func NewS3AuthHelper(manager Manager) *S3AuthHelper {
	if am, ok := manager.(*authManager); ok {
		return &S3AuthHelper{manager: am}
	}
	return nil
}

// ExtractCredentialsFromHeader extracts credentials from various auth headers
func (s *S3AuthHelper) ExtractCredentialsFromHeader(r *http.Request) (string, string, error) {
	// Check Authorization header first
	if auth := r.Header.Get("Authorization"); auth != "" {
		return s.parseAuthorizationHeader(auth)
	}

	// Check query parameters for pre-signed URLs
	if accessKey := r.URL.Query().Get("AWSAccessKeyId"); accessKey != "" {
		signature := r.URL.Query().Get("Signature")
		return accessKey, signature, nil
	}

	// Check for AWS SigV4 query parameters
	if accessKey := r.URL.Query().Get("X-Amz-Credential"); accessKey != "" {
		signature := r.URL.Query().Get("X-Amz-Signature")
		return accessKey, signature, nil
	}

	return "", "", ErrMissingSignature
}

// parseAuthorizationHeader parses various Authorization header formats
func (s *S3AuthHelper) parseAuthorizationHeader(auth string) (string, string, error) {
	auth = strings.TrimSpace(auth)

	// AWS Signature Version 4
	if strings.HasPrefix(auth, "AWS4-HMAC-SHA256") {
		return s.parseV4Authorization(auth)
	}

	// AWS Signature Version 2
	if strings.HasPrefix(auth, "AWS ") {
		return s.parseV2Authorization(auth)
	}

	// Bearer token (JWT)
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		return "", token, nil
	}

	return "", "", ErrInvalidSignature
}

// parseV4Authorization parses AWS SigV4 authorization header
func (s *S3AuthHelper) parseV4Authorization(auth string) (string, string, error) {
	parts := strings.Split(auth, " ")
	if len(parts) != 2 {
		return "", "", ErrInvalidSignature
	}

	params := strings.Split(parts[1], ", ")
	var accessKey, signature string

	for _, param := range params {
		kv := strings.SplitN(param, "=", 2)
		if len(kv) != 2 {
			continue
		}

		switch kv[0] {
		case "Credential":
			// Extract access key from credential
			credParts := strings.Split(kv[1], "/")
			if len(credParts) > 0 {
				accessKey = credParts[0]
			}
		case "Signature":
			signature = kv[1]
		}
	}

	if accessKey == "" || signature == "" {
		return "", "", ErrInvalidSignature
	}

	return accessKey, signature, nil
}

// parseV2Authorization parses AWS SigV2 authorization header
func (s *S3AuthHelper) parseV2Authorization(auth string) (string, string, error) {
	// Format: AWS AccessKey:Signature
	parts := strings.SplitN(auth[4:], ":", 2)
	if len(parts) != 2 {
		return "", "", ErrInvalidSignature
	}

	return parts[0], parts[1], nil
}

// ValidateTimestamp validates request timestamp to prevent replay attacks
func (s *S3AuthHelper) ValidateTimestamp(r *http.Request, maxSkew time.Duration) error {
	var timestamp string

	// Check X-Amz-Date header (SigV4)
	if timestamp = r.Header.Get("X-Amz-Date"); timestamp != "" {
		t, err := time.Parse("20060102T150405Z", timestamp)
		if err != nil {
			return ErrInvalidSignature
		}
		if time.Since(t).Abs() > maxSkew {
			return ErrTimestampSkew
		}
		return nil
	}

	// Check Date header (SigV2)
	if timestamp = r.Header.Get("Date"); timestamp != "" {
		t, err := time.Parse(time.RFC1123, timestamp)
		if err != nil {
			return ErrInvalidSignature
		}
		if time.Since(t).Abs() > maxSkew {
			return ErrTimestampSkew
		}
		return nil
	}

	// Check query parameter for pre-signed URLs
	if expires := r.URL.Query().Get("Expires"); expires != "" {
		// Handle pre-signed URL expiration
		// Implementation would parse expires and compare with current time
		return nil
	}

	return ErrMissingSignature
}

// GetS3Action extracts S3 action from HTTP request
func (s *S3AuthHelper) GetS3Action(r *http.Request) string {
	method := r.Method
	path := r.URL.Path

	// Root level operations
	if path == "/" || path == "" {
		switch method {
		case "GET":
			return ActionListAllMyBuckets
		}
	}

	// Bucket level operations
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	if len(pathParts) == 1 && pathParts[0] != "" {
		// Bucket operations
		switch method {
		case "GET":
			if r.URL.Query().Has("versioning") {
				return ActionGetBucketVersioning
			}
			if r.URL.Query().Has("policy") {
				return ActionGetBucketPolicy
			}
			if r.URL.Query().Has("lifecycle") {
				return ActionGetBucketLifecycle
			}
			if r.URL.Query().Has("cors") {
				return ActionGetBucketCORS
			}
			return ActionListBucket
		case "PUT":
			if r.URL.Query().Has("versioning") {
				return ActionPutBucketVersioning
			}
			if r.URL.Query().Has("policy") {
				return ActionPutBucketPolicy
			}
			if r.URL.Query().Has("lifecycle") {
				return ActionPutBucketLifecycle
			}
			if r.URL.Query().Has("cors") {
				return ActionPutBucketCORS
			}
			return ActionCreateBucket
		case "DELETE":
			if r.URL.Query().Has("policy") {
				return ActionDeleteBucketPolicy
			}
			if r.URL.Query().Has("lifecycle") {
				return ActionDeleteBucketLifecycle
			}
			if r.URL.Query().Has("cors") {
				return ActionDeleteBucketCORS
			}
			return ActionDeleteBucket
		}
	}

	// Object level operations
	if len(pathParts) >= 2 {
		switch method {
		case "GET":
			if r.URL.Query().Has("acl") {
				return ActionGetObjectAcl
			}
			if r.URL.Query().Has("tagging") {
				return ActionGetObjectTagging
			}
			if r.URL.Query().Has("retention") {
				return ActionGetObjectRetention
			}
			if r.URL.Query().Has("legal-hold") {
				return ActionGetObjectLegalHold
			}
			if r.URL.Query().Has("uploadId") {
				return ActionListMultipartUploadParts
			}
			return ActionGetObject
		case "PUT":
			if r.URL.Query().Has("acl") {
				return ActionPutObjectAcl
			}
			if r.URL.Query().Has("tagging") {
				return ActionPutObjectTagging
			}
			if r.URL.Query().Has("retention") {
				return ActionPutObjectRetention
			}
			if r.URL.Query().Has("legal-hold") {
				return ActionPutObjectLegalHold
			}
			return ActionPutObject
		case "DELETE":
			if r.URL.Query().Has("tagging") {
				return ActionDeleteObjectTagging
			}
			if r.URL.Query().Has("uploadId") {
				return ActionAbortMultipartUpload
			}
			return ActionDeleteObject
		}
	}

	// Default to most restrictive action
	return ActionGetObject
}

// GetResourceARN generates an ARN for the requested resource
func (s *S3AuthHelper) GetResourceARN(r *http.Request) string {
	path := strings.Trim(r.URL.Path, "/")
	if path == "" {
		return "arn:aws:s3:::*"
	}

	pathParts := strings.Split(path, "/")
	if len(pathParts) == 1 {
		// Bucket resource
		return "arn:aws:s3:::" + pathParts[0]
	}

	// Object resource
	bucket := pathParts[0]
	object := strings.Join(pathParts[1:], "/")
	return "arn:aws:s3:::" + bucket + "/" + object
}

// AuthenticateRequest performs complete S3 request authentication
func (s *S3AuthHelper) AuthenticateRequest(ctx context.Context, r *http.Request) (*User, error) {
	// Validate timestamp to prevent replay attacks
	if err := s.ValidateTimestamp(r, 15*time.Minute); err != nil {
		return nil, err
	}

	// Try S3 signature authentication first
	user, err := s.manager.ValidateS3Signature(ctx, r)
	if err == nil {
		return user, nil
	}

	// Try JWT authentication
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		return s.manager.ValidateJWT(ctx, token)
	}

	return nil, err
}

// AuthorizeRequest checks if the authenticated user can perform the requested action
func (s *S3AuthHelper) AuthorizeRequest(ctx context.Context, user *User, r *http.Request) error {
	action := s.GetS3Action(r)
	resource := s.GetResourceARN(r)

	return s.manager.CheckPermission(ctx, user, action, resource)
}
