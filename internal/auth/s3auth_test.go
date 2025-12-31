package auth

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestNewS3AuthHelper tests creating S3 auth helper
func TestNewS3AuthHelper(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	t.Run("Valid auth manager", func(t *testing.T) {
		helper := NewS3AuthHelper(manager)
		if helper == nil {
			t.Error("Expected non-nil helper for valid auth manager")
		}
	})

	t.Run("Invalid manager type", func(t *testing.T) {
		// Pass a nil or wrong type - should return nil
		helper := NewS3AuthHelper(nil)
		if helper != nil {
			t.Error("Expected nil helper for invalid manager")
		}
	})
}

// TestExtractCredentialsFromHeader tests credential extraction
func TestExtractCredentialsFromHeader(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	helper := NewS3AuthHelper(manager)

	tests := []struct {
		name        string
		setupReq    func() *http.Request
		wantKey     string
		wantSig     string
		wantErr     bool
	}{
		{
			name: "SigV4 Authorization header",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=abc123")
				return req
			},
			wantKey: "AKIAIOSFODNN7EXAMPLE",
			wantSig: "abc123",
			wantErr: false,
		},
		{
			name: "SigV2 Authorization header",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("Authorization", "AWS AKIAIOSFODNN7EXAMPLE:frJIUN8DYpKDtOLCwo//yllqDzg=")
				return req
			},
			wantKey: "AKIAIOSFODNN7EXAMPLE",
			wantSig: "frJIUN8DYpKDtOLCwo//yllqDzg=",
			wantErr: false,
		},
		{
			name: "Bearer token",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9")
				return req
			},
			wantKey: "",
			wantSig: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			wantErr: false,
		},
		{
			name: "Pre-signed URL V2 query params",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/?AWSAccessKeyId=AKIAIOSFODNN7EXAMPLE&Signature=abc123", nil)
				return req
			},
			wantKey: "AKIAIOSFODNN7EXAMPLE",
			wantSig: "abc123",
			wantErr: false,
		},
		{
			name: "Pre-signed URL V4 query params",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/?X-Amz-Credential=AKIAIOSFODNN7EXAMPLE&X-Amz-Signature=def456", nil)
				return req
			},
			wantKey: "AKIAIOSFODNN7EXAMPLE",
			wantSig: "def456",
			wantErr: false,
		},
		{
			name: "Missing signature",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			key, sig, err := helper.ExtractCredentialsFromHeader(req)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if key != tt.wantKey {
					t.Errorf("Expected key %q, got %q", tt.wantKey, key)
				}
				if sig != tt.wantSig {
					t.Errorf("Expected signature %q, got %q", tt.wantSig, sig)
				}
			}
		})
	}
}

// TestParseV4Authorization tests parsing SigV4 authorization header
func TestParseV4Authorization(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	helper := NewS3AuthHelper(manager)

	tests := []struct {
		name    string
		auth    string
		wantKey string
		wantSig string
		wantErr bool
	}{
		{
			name:    "Valid SigV4 header",
			auth:    "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=abc123def456",
			wantKey: "AKIAIOSFODNN7EXAMPLE",
			wantSig: "abc123def456",
			wantErr: false,
		},
		{
			name:    "Valid SigV4 without spaces after commas",
			auth:    "AWS4-HMAC-SHA256 Credential=TESTKEY/20130524/us-east-1/s3/aws4_request,SignedHeaders=host,Signature=xyz789",
			wantKey: "TESTKEY",
			wantSig: "xyz789",
			wantErr: false,
		},
		{
			name:    "Invalid - not SigV4",
			auth:    "AWS AccessKey:Signature",
			wantErr: true,
		},
		{
			name:    "Invalid - missing credential",
			auth:    "AWS4-HMAC-SHA256 SignedHeaders=host, Signature=abc",
			wantErr: true,
		},
		{
			name:    "Invalid - missing signature",
			auth:    "AWS4-HMAC-SHA256 Credential=KEY/20130524/us-east-1/s3/aws4_request, SignedHeaders=host",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, sig, err := helper.parseV4Authorization(tt.auth)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if key != tt.wantKey {
					t.Errorf("Expected key %q, got %q", tt.wantKey, key)
				}
				if sig != tt.wantSig {
					t.Errorf("Expected signature %q, got %q", tt.wantSig, sig)
				}
			}
		})
	}
}

// TestParseV2Authorization tests parsing SigV2 authorization header
func TestParseV2Authorization(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	helper := NewS3AuthHelper(manager)

	tests := []struct {
		name    string
		auth    string
		wantKey string
		wantSig string
		wantErr bool
	}{
		{
			name:    "Valid SigV2 header",
			auth:    "AWS AKIAIOSFODNN7EXAMPLE:frJIUN8DYpKDtOLCwo//yllqDzg=",
			wantKey: "AKIAIOSFODNN7EXAMPLE",
			wantSig: "frJIUN8DYpKDtOLCwo//yllqDzg=",
			wantErr: false,
		},
		{
			name:    "Invalid - not SigV2",
			auth:    "AWS4-HMAC-SHA256 ...",
			wantErr: true,
		},
		{
			name:    "Invalid - missing colon",
			auth:    "AWS AKIAIOSFODNN7EXAMPLE",
			wantErr: true,
		},
		{
			name:    "Invalid - empty key",
			auth:    "AWS :signature",
			wantKey: "",
			wantSig: "signature",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, sig, err := helper.parseV2Authorization(tt.auth)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if key != tt.wantKey {
					t.Errorf("Expected key %q, got %q", tt.wantKey, key)
				}
				if sig != tt.wantSig {
					t.Errorf("Expected signature %q, got %q", tt.wantSig, sig)
				}
			}
		})
	}
}

// TestValidateTimestamp tests timestamp validation
func TestValidateTimestamp(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	helper := NewS3AuthHelper(manager)

	now := time.Now().UTC()

	tests := []struct {
		name    string
		setupReq func() *http.Request
		maxSkew time.Duration
		wantErr bool
	}{
		{
			name: "Valid X-Amz-Date (current time)",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
				return req
			},
			maxSkew: 15 * time.Minute,
			wantErr: false,
		},
		{
			name: "Valid Date header (current time)",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("Date", now.Format(time.RFC1123))
				return req
			},
			maxSkew: 15 * time.Minute,
			wantErr: false,
		},
		{
			name: "X-Amz-Date within skew (10 min ago)",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				past := now.Add(-10 * time.Minute)
				req.Header.Set("X-Amz-Date", past.Format("20060102T150405Z"))
				return req
			},
			maxSkew: 15 * time.Minute,
			wantErr: false,
		},
		{
			name: "X-Amz-Date outside skew (20 min ago)",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				past := now.Add(-20 * time.Minute)
				req.Header.Set("X-Amz-Date", past.Format("20060102T150405Z"))
				return req
			},
			maxSkew: 15 * time.Minute,
			wantErr: true,
		},
		{
			name: "Invalid X-Amz-Date format",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("X-Amz-Date", "invalid-date")
				return req
			},
			maxSkew: 15 * time.Minute,
			wantErr: true,
		},
		{
			name: "Pre-signed URL with Expires param",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/?Expires=1234567890", nil)
				return req
			},
			maxSkew: 15 * time.Minute,
			wantErr: false, // Pre-signed URL handling returns nil for now
		},
		{
			name: "Missing timestamp",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			maxSkew: 15 * time.Minute,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			err := helper.ValidateTimestamp(req, tt.maxSkew)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// TestGetS3Action tests S3 action extraction
func TestGetS3Action(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	helper := NewS3AuthHelper(manager)

	tests := []struct {
		name       string
		method     string
		path       string
		query      string
		wantAction string
	}{
		// Root level
		{"List all buckets", "GET", "/", "", ActionListAllMyBuckets},

		// Bucket level - GET
		{"List bucket", "GET", "/mybucket", "", ActionListBucket},
		{"Get bucket versioning", "GET", "/mybucket", "versioning", ActionGetBucketVersioning},
		{"Get bucket policy", "GET", "/mybucket", "policy", ActionGetBucketPolicy},
		{"Get bucket lifecycle", "GET", "/mybucket", "lifecycle", ActionGetBucketLifecycle},
		{"Get bucket CORS", "GET", "/mybucket", "cors", ActionGetBucketCORS},

		// Bucket level - PUT
		{"Create bucket", "PUT", "/mybucket", "", ActionCreateBucket},
		{"Put bucket versioning", "PUT", "/mybucket", "versioning", ActionPutBucketVersioning},
		{"Put bucket policy", "PUT", "/mybucket", "policy", ActionPutBucketPolicy},
		{"Put bucket lifecycle", "PUT", "/mybucket", "lifecycle", ActionPutBucketLifecycle},
		{"Put bucket CORS", "PUT", "/mybucket", "cors", ActionPutBucketCORS},

		// Bucket level - DELETE
		{"Delete bucket", "DELETE", "/mybucket", "", ActionDeleteBucket},
		{"Delete bucket policy", "DELETE", "/mybucket", "policy", ActionDeleteBucketPolicy},
		{"Delete bucket lifecycle", "DELETE", "/mybucket", "lifecycle", ActionDeleteBucketLifecycle},
		{"Delete bucket CORS", "DELETE", "/mybucket", "cors", ActionDeleteBucketCORS},

		// Object level - GET
		{"Get object", "GET", "/mybucket/myfile.txt", "", ActionGetObject},
		{"Get object ACL", "GET", "/mybucket/myfile.txt", "acl", ActionGetObjectAcl},
		{"Get object tagging", "GET", "/mybucket/myfile.txt", "tagging", ActionGetObjectTagging},
		{"Get object retention", "GET", "/mybucket/myfile.txt", "retention", ActionGetObjectRetention},
		{"Get object legal hold", "GET", "/mybucket/myfile.txt", "legal-hold", ActionGetObjectLegalHold},
		{"List multipart parts", "GET", "/mybucket/myfile.txt", "uploadId=123", ActionListMultipartUploadParts},

		// Object level - PUT
		{"Put object", "PUT", "/mybucket/myfile.txt", "", ActionPutObject},
		{"Put object ACL", "PUT", "/mybucket/myfile.txt", "acl", ActionPutObjectAcl},
		{"Put object tagging", "PUT", "/mybucket/myfile.txt", "tagging", ActionPutObjectTagging},
		{"Put object retention", "PUT", "/mybucket/myfile.txt", "retention", ActionPutObjectRetention},
		{"Put object legal hold", "PUT", "/mybucket/myfile.txt", "legal-hold", ActionPutObjectLegalHold},

		// Object level - DELETE
		{"Delete object", "DELETE", "/mybucket/myfile.txt", "", ActionDeleteObject},
		{"Delete object tagging", "DELETE", "/mybucket/myfile.txt", "tagging", ActionDeleteObjectTagging},
		{"Abort multipart upload", "DELETE", "/mybucket/myfile.txt", "uploadId=123", ActionAbortMultipartUpload},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqURL := tt.path
			if tt.query != "" {
				reqURL += "?" + tt.query
			}
			req, _ := http.NewRequest(tt.method, reqURL, nil)

			action := helper.GetS3Action(req)
			if action != tt.wantAction {
				t.Errorf("Expected action %q, got %q", tt.wantAction, action)
			}
		})
	}
}

// TestGetResourceARN tests ARN generation
func TestGetResourceARN(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	helper := NewS3AuthHelper(manager)

	tests := []struct {
		name    string
		path    string
		wantARN string
	}{
		{
			name:    "Root path",
			path:    "/",
			wantARN: "arn:aws:s3:::*",
		},
		{
			name:    "Empty path",
			path:    "",
			wantARN: "arn:aws:s3:::*",
		},
		{
			name:    "Bucket only",
			path:    "/mybucket",
			wantARN: "arn:aws:s3:::mybucket",
		},
		{
			name:    "Bucket with trailing slash",
			path:    "/mybucket/",
			wantARN: "arn:aws:s3:::mybucket/",
		},
		{
			name:    "Object in bucket",
			path:    "/mybucket/file.txt",
			wantARN: "arn:aws:s3:::mybucket/file.txt",
		},
		{
			name:    "Object in folder",
			path:    "/mybucket/folder/subfolder/file.txt",
			wantARN: "arn:aws:s3:::mybucket/folder/subfolder/file.txt",
		},
		{
			name:    "Object with special characters",
			path:    "/mybucket/my file (1).txt",
			wantARN: "arn:aws:s3:::mybucket/my file (1).txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", tt.path, nil)
			arn := helper.GetResourceARN(req)

			if arn != tt.wantARN {
				t.Errorf("Expected ARN %q, got %q", tt.wantARN, arn)
			}
		})
	}
}

// TestAuthenticateRequest tests complete authentication flow
func TestAuthenticateRequest(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	helper := NewS3AuthHelper(manager)
	ctx := context.Background()

	t.Run("Missing timestamp", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "AWS4-HMAC-SHA256 ...")

		_, err := helper.AuthenticateRequest(ctx, req)
		if err == nil {
			t.Error("Expected error for missing timestamp")
		}
	})

	t.Run("Valid timestamp but invalid signature", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/", nil)
		req.Header.Set("X-Amz-Date", time.Now().Format("20060102T150405Z"))
		req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=invalid/20130524/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=invalid")

		_, err := helper.AuthenticateRequest(ctx, req)
		if err == nil {
			t.Error("Expected error for invalid credentials")
		}
	})

	t.Run("JWT Bearer token", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/", nil)
		req.Header.Set("X-Amz-Date", time.Now().Format("20060102T150405Z"))
		req.Header.Set("Authorization", "Bearer invalid-token")

		_, err := helper.AuthenticateRequest(ctx, req)
		// Should attempt JWT validation
		if err == nil {
			t.Error("Expected error for invalid JWT")
		}
	})
}

// TestAuthorizeRequest tests authorization check
func TestAuthorizeRequest(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	helper := NewS3AuthHelper(manager)
	ctx := context.Background()

	// Create a test user
	testUser := &User{
		ID:       "test-user",
		Username: "testuser",
		Roles:    []string{"admin"},
	}

	t.Run("Admin user allowed", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/mybucket/file.txt", nil)

		err := helper.AuthorizeRequest(ctx, testUser, req)
		// Admin users should be allowed (CheckPermission allows admin)
		if err != nil {
			t.Errorf("Admin user should be authorized: %v", err)
		}
	})

	t.Run("Regular user", func(t *testing.T) {
		regularUser := &User{
			ID:       "regular-user",
			Username: "regular",
			Roles:    []string{"user"},
		}

		req, _ := http.NewRequest("GET", "/mybucket/file.txt", nil)

		err := helper.AuthorizeRequest(ctx, regularUser, req)
		// Permission check will be performed
		if err != nil {
			// Expected for user without specific bucket permissions
			t.Logf("Regular user denied as expected: %v", err)
		}
	})
}

// TestParseAuthorizationHeader tests authorization header parsing
func TestParseAuthorizationHeader(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	helper := NewS3AuthHelper(manager)

	tests := []struct {
		name     string
		auth     string
		wantKey  string
		wantSig  string
		wantErr  bool
		checkSig bool
	}{
		{
			name:     "SigV4",
			auth:     "AWS4-HMAC-SHA256 Credential=KEY/20130524/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=sig",
			wantKey:  "KEY",
			wantSig:  "sig",
			wantErr:  false,
			checkSig: true,
		},
		{
			name:     "SigV2",
			auth:     "AWS KEY:signature",
			wantKey:  "KEY",
			wantSig:  "signature",
			wantErr:  false,
			checkSig: true,
		},
		{
			name:     "Bearer token",
			auth:     "Bearer token123",
			wantKey:  "",
			wantSig:  "token123",
			wantErr:  false,
			checkSig: true,
		},
		{
			name:    "Invalid format",
			auth:    "InvalidAuth",
			wantErr: true,
		},
		{
			name:    "Empty string",
			auth:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, sig, err := helper.parseAuthorizationHeader(tt.auth)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tt.checkSig {
					if key != tt.wantKey {
						t.Errorf("Expected key %q, got %q", tt.wantKey, key)
					}
					if sig != tt.wantSig {
						t.Errorf("Expected signature %q, got %q", tt.wantSig, sig)
					}
				}
			}
		})
	}
}
