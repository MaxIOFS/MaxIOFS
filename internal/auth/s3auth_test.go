package auth

import (
	"net/http"
	"testing"
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
func TestWriteS3ErrorIncludesAmzHeaders(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		message    string
		statusCode int
	}{
		{"Unauthorized", "InvalidAccessKeyId", "The key does not exist", 401},
		{"Forbidden", "AccessDenied", "Access Denied", 403},
		{"Internal", "InternalError", "Server error", 500},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "/bucket/key", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			w := newTestResponseWriter()
			writeS3Error(w, req, tc.code, tc.message, tc.statusCode)

			if w.code != tc.statusCode {
				t.Errorf("Status code = %d, want %d", w.code, tc.statusCode)
			}

			requestID := w.headers.Get("X-Amz-Request-Id")
			if requestID == "" {
				t.Error("X-Amz-Request-Id header must be set on error responses")
			}
			if len(requestID) != 16 {
				t.Errorf("X-Amz-Request-Id length = %d, want 16 (uppercase hex)", len(requestID))
			}

			amzId2 := w.headers.Get("X-Amz-Id-2")
			if amzId2 == "" {
				t.Error("X-Amz-Id-2 header must be set on error responses")
			}
			if len(amzId2) != 64 {
				t.Errorf("X-Amz-Id-2 length = %d, want 64 (hex)", len(amzId2))
			}
		})
	}
}

// testResponseWriter is a minimal http.ResponseWriter for unit testing.
type testResponseWriter struct {
	headers http.Header
	code    int
	body    []byte
}

func newTestResponseWriter() *testResponseWriter {
	return &testResponseWriter{headers: make(http.Header)}
}

func (rw *testResponseWriter) Header() http.Header        { return rw.headers }
func (rw *testResponseWriter) WriteHeader(statusCode int) { rw.code = statusCode }
func (rw *testResponseWriter) Write(b []byte) (int, error) {
	rw.body = append(rw.body, b...)
	return len(b), nil
}
