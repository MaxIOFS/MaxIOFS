package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// ========================================
// Test Helper Functions
// ========================================

func TestUriEncode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty string becomes slash",
			input:    "",
			expected: "/",
		},
		{
			name:     "Simple path unchanged",
			input:    "/bucket/object",
			expected: "/bucket/object",
		},
		{
			name:     "Path with spaces",
			input:    "/bucket/my file.txt",
			expected: "/bucket/my%20file.txt",
		},
		{
			name:     "Path with special characters",
			input:    "/bucket/file@#$.txt",
			expected: "/bucket/file%40%23%24.txt",
		},
		{
			name:     "Path with allowed characters (A-Z a-z 0-9 -_.~/)",
			input:    "/bucket/My_File-2024.txt",
			expected: "/bucket/My_File-2024.txt",
		},
		{
			name:     "Path with unicode characters",
			input:    "/bucket/文件.txt",
			expected: "/bucket/%E6%96%87%E4%BB%B6.txt",
		},
		{
			name:     "Path with percent encoding already present",
			input:    "/bucket/file%20name.txt",
			expected: "/bucket/file%2520name.txt", // Double encoded
		},
		{
			name:     "Path with plus sign",
			input:    "/bucket/file+name.txt",
			expected: "/bucket/file%2Bname.txt",
		},
		{
			name:     "Path with tilde (should not be encoded)",
			input:    "/bucket/~user/file.txt",
			expected: "/bucket/~user/file.txt",
		},
		{
			name:     "Path with multiple slashes",
			input:    "/bucket//object///file.txt",
			expected: "/bucket//object///file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := uriEncode(tt.input)
			if result != tt.expected {
				t.Errorf("uriEncode(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHmacSHA256(t *testing.T) {
	tests := []struct {
		name     string
		key      []byte
		data     []byte
		expected string // hex encoded
	}{
		{
			name:     "Simple HMAC",
			key:      []byte("secret"),
			data:     []byte("message"),
			expected: "8b5f48702995c1598c573db1e21866a9b825d4a794d169d7060a03605796360b",
		},
		{
			name:     "Empty key",
			key:      []byte(""),
			data:     []byte("message"),
			expected: "eb08c1f56d5ddee07f7bdf80468083da06b64cf4fac64fe3a90883df5feacae4",
		},
		{
			name:     "Empty data",
			key:      []byte("secret"),
			data:     []byte(""),
			expected: "f9e66e179b6747ae54108f82f8ade8b3c25d76fd30afde6c395822c530196169",
		},
		{
			name:     "Both empty",
			key:      []byte(""),
			data:     []byte(""),
			expected: "b613679a0814d9ec772f95d778c35fc5ff1697c493715653c6c712144292c5ad",
		},
		{
			name:     "AWS-style key derivation (first step)",
			key:      []byte("AWS4wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"),
			data:     []byte("20130524"),
			expected: "a5a91d94fa9a905c91e89aa51df0d86aef33adf77e97d146ae28e8d85d0df909", // Correct HMAC-SHA256
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hmacSHA256(tt.key, tt.data)
			resultHex := hex.EncodeToString(result)
			if resultHex != tt.expected {
				t.Errorf("hmacSHA256() = %s, want %s", resultHex, tt.expected)
			}
		})
	}
}

func TestCalculateSignatureV4(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access private methods
	manager := managerInterface.(*authManager)

	tests := []struct {
		name         string
		stringToSign string
		secretKey    string
		date         string
		region       string
		service      string
	}{
		{
			name: "Standard SigV4 signature",
			stringToSign: "AWS4-HMAC-SHA256\n20130524T000000Z\n20130524/us-east-1/s3/aws4_request\n" +
				"7344ae5b7ee6c3e7e6b0fe0640412a37625d1fbfff95c48bbb2dc43964946972",
			secretKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
			date:      "20130524",
			region:    "us-east-1",
			service:   "s3",
		},
		{
			name: "Different region",
			stringToSign: "AWS4-HMAC-SHA256\n20240101T120000Z\n20240101/eu-west-1/s3/aws4_request\n" +
				"abcd1234efgh5678ijkl9012mnop3456qrst7890uvwx1234yzab5678cdef9012",
			secretKey: "testSecretKey123",
			date:      "20240101",
			region:    "eu-west-1",
			service:   "s3",
		},
		{
			name: "Date with timestamp format (should extract YYYYMMDD)",
			stringToSign: "AWS4-HMAC-SHA256\n20240101T120000Z\n20240101/us-east-1/s3/aws4_request\n" +
				"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			secretKey: "mySecretKey",
			date:      "20240101T120000Z", // Should extract just 20240101
			region:    "us-east-1",
			service:   "s3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.calculateSignatureV4(tt.stringToSign, tt.secretKey, tt.date, tt.region, tt.service)

			// Verify signature is valid hex and correct length
			if len(result) != 64 { // SHA256 hex is 64 chars
				t.Errorf("calculateSignatureV4() returned invalid signature length: %d, want 64", len(result))
			}
			// Verify it's valid hex
			if _, err := hex.DecodeString(result); err != nil {
				t.Errorf("calculateSignatureV4() returned invalid hex: %v", err)
			}
			// Verify it's not empty
			if result == "" || result == "0000000000000000000000000000000000000000000000000000000000000000" {
				t.Error("calculateSignatureV4() returned empty or zero signature")
			}
		})
	}
}

func TestCreateCanonicalRequest(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access private methods
	manager := managerInterface.(*authManager)

	tests := []struct {
		name          string
		setupReq      func() *http.Request
		signedHeaders string
		expectedParts struct {
			method        string
			uri           string
			hasQueryStr   bool
			hasHeaders    bool
			payloadHash   string
		}
	}{
		{
			name: "Simple GET request",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
				req.Host = "s3.amazonaws.com"
				req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
				return req
			},
			signedHeaders: "host;x-amz-content-sha256",
			expectedParts: struct {
				method        string
				uri           string
				hasQueryStr   bool
				hasHeaders    bool
				payloadHash   string
			}{
				method:      "GET",
				uri:         "/bucket/object.txt",
				hasQueryStr: false,
				hasHeaders:  true,
				payloadHash: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name: "Request with query parameters",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket?prefix=test&max-keys=100", nil)
				req.Host = "s3.amazonaws.com"
				req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
				return req
			},
			signedHeaders: "host",
			expectedParts: struct {
				method        string
				uri           string
				hasQueryStr   bool
				hasHeaders    bool
				payloadHash   string
			}{
				method:      "GET",
				uri:         "/bucket",
				hasQueryStr: true,
				hasHeaders:  true,
				payloadHash: "UNSIGNED-PAYLOAD",
			},
		},
		{
			name: "PUT request with headers",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("PUT", "/bucket/file.txt", nil)
				req.Host = "bucket.s3.amazonaws.com"
				req.Header.Set("X-Amz-Date", "20240101T120000Z")
				req.Header.Set("X-Amz-Content-Sha256", "1234567890abcdef")
				req.Header.Set("Content-Type", "text/plain")
				return req
			},
			signedHeaders: "content-type;host;x-amz-content-sha256;x-amz-date",
			expectedParts: struct {
				method        string
				uri           string
				hasQueryStr   bool
				hasHeaders    bool
				payloadHash   string
			}{
				method:      "PUT",
				uri:         "/bucket/file.txt",
				hasQueryStr: false,
				hasHeaders:  true,
				payloadHash: "1234567890abcdef",
			},
		},
		{
			name: "Root path becomes /",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "", nil)
				req.Host = "s3.amazonaws.com"
				req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
				return req
			},
			signedHeaders: "host",
			expectedParts: struct {
				method        string
				uri           string
				hasQueryStr   bool
				hasHeaders    bool
				payloadHash   string
			}{
				method:      "GET",
				uri:         "/",
				hasQueryStr: false,
				hasHeaders:  true,
				payloadHash: "UNSIGNED-PAYLOAD",
			},
		},
		{
			name: "Request without X-Amz-Content-Sha256 uses UNSIGNED-PAYLOAD",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket", nil)
				req.Host = "s3.amazonaws.com"
				return req
			},
			signedHeaders: "host",
			expectedParts: struct {
				method        string
				uri           string
				hasQueryStr   bool
				hasHeaders    bool
				payloadHash   string
			}{
				method:      "GET",
				uri:         "/bucket",
				hasQueryStr: false,
				hasHeaders:  true,
				payloadHash: "UNSIGNED-PAYLOAD",
			},
		},
		{
			name: "Path with special characters",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/my file.txt", nil)
				req.Host = "s3.amazonaws.com"
				req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
				return req
			},
			signedHeaders: "host",
			expectedParts: struct {
				method        string
				uri           string
				hasQueryStr   bool
				hasHeaders    bool
				payloadHash   string
			}{
				method:      "GET",
				uri:         "/bucket/my%20file.txt",
				hasQueryStr: false,
				hasHeaders:  true,
				payloadHash: "UNSIGNED-PAYLOAD",
			},
		},
		{
			name: "Virtual-hosted-style: uses original path from context (client signed /, server received /inmutable/)",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "http://inmutable.s3.example.com/?delimiter=%2F&max-keys=1000&prefix=", nil)
				req.Host = "inmutable.s3.example.com"
				req.Header.Set("X-Amz-Date", "20260228T195241Z")
				req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
				// Simulate virtualHostedStyleMiddleware: path was rewritten to /inmutable/
				// but context has original path / that the client signed
				req.URL.Path = "/inmutable/"
				req = req.WithContext(WithOriginalSigV4Path(req.Context(), "/"))
				return req
			},
			signedHeaders: "host;x-amz-content-sha256;x-amz-date",
			expectedParts: struct {
				method        string
				uri           string
				hasQueryStr   bool
				hasHeaders    bool
				payloadHash   string
			}{
				method:      "GET",
				uri:         "/", // Must use original path for verification, not /inmutable/
				hasQueryStr: true,
				hasHeaders:  true,
				payloadHash: "UNSIGNED-PAYLOAD",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			result := manager.createCanonicalRequest(req, tt.signedHeaders)

			// Verify structure: method\nuri\nquery\nheaders\nsignedHeaders\npayloadHash
			parts := strings.Split(result, "\n")
			if len(parts) < 6 {
				t.Errorf("createCanonicalRequest() returned %d parts, want at least 6", len(parts))
				t.Logf("Result:\n%s", result)
				return
			}

			// Verify method
			if parts[0] != tt.expectedParts.method {
				t.Errorf("Method = %q, want %q", parts[0], tt.expectedParts.method)
			}

			// Verify URI
			if parts[1] != tt.expectedParts.uri {
				t.Errorf("URI = %q, want %q", parts[1], tt.expectedParts.uri)
			}

			// Verify query string presence
			if tt.expectedParts.hasQueryStr && parts[2] == "" {
				t.Error("Expected query string but got empty")
			}

			// Verify signed headers (second to last part)
			if parts[len(parts)-2] != tt.signedHeaders {
				t.Errorf("SignedHeaders = %q, want %q", parts[len(parts)-2], tt.signedHeaders)
			}

			// Verify payload hash (last part)
			if parts[len(parts)-1] != tt.expectedParts.payloadHash {
				t.Errorf("PayloadHash = %q, want %q", parts[len(parts)-1], tt.expectedParts.payloadHash)
			}
		})
	}
}

func TestCreateStringToSignV2(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access private methods
	manager := managerInterface.(*authManager)

	tests := []struct {
		name     string
		setupReq func() *http.Request
		expected string
	}{
		{
			name: "Simple GET request",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
				req.Header.Set("Date", "Tue, 27 Mar 2007 19:36:42 +0000")
				return req
			},
			expected: "GET\n\n\nTue, 27 Mar 2007 19:36:42 +0000\n/bucket/object.txt",
		},
		{
			name: "PUT request with Content-Type and Content-MD5",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("PUT", "/bucket/file.txt", nil)
				req.Header.Set("Date", "Tue, 27 Mar 2007 19:44:46 +0000")
				req.Header.Set("Content-Type", "text/plain")
				req.Header.Set("Content-MD5", "c8fdb181845a4ca6b8fec737b3581d76")
				return req
			},
			expected: "PUT\nc8fdb181845a4ca6b8fec737b3581d76\ntext/plain\nTue, 27 Mar 2007 19:44:46 +0000\n/bucket/file.txt",
		},
		{
			name: "Request without optional headers",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("DELETE", "/bucket/object", nil)
				req.Header.Set("Date", "Tue, 27 Mar 2007 20:00:00 +0000")
				return req
			},
			expected: "DELETE\n\n\nTue, 27 Mar 2007 20:00:00 +0000\n/bucket/object",
		},
		{
			name: "Root path",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("Date", "Tue, 27 Mar 2007 20:00:00 +0000")
				return req
			},
			expected: "GET\n\n\nTue, 27 Mar 2007 20:00:00 +0000\n/",
		},
		{
			name: "Empty path becomes /",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "", nil)
				req.Header.Set("Date", "Tue, 27 Mar 2007 20:00:00 +0000")
				return req
			},
			expected: "GET\n\n\nTue, 27 Mar 2007 20:00:00 +0000\n/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			result := manager.createStringToSignV2(req)
			if result != tt.expected {
				t.Errorf("createStringToSignV2() mismatch:\nGot:\n%s\nWant:\n%s", result, tt.expected)
			}
		})
	}
}

// ========================================
// Test Parsing Functions
// ========================================

func TestParseS3SignatureV4(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access private methods
	manager := managerInterface.(*authManager)

	tests := []struct {
		name       string
		authHeader string
		setupReq   func() *http.Request
		wantSig    *S3SignatureV4
		wantErr    bool
	}{
		{
			name: "Valid SigV4 header",
			authHeader: "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
				"SignedHeaders=host;range;x-amz-date, " +
				"Signature=fe5f80f77d5fa3beca038a248ff027d0445342fe2855ddc963176630326f1024",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("X-Amz-Date", "20130524T000000Z")
				return req
			},
			wantSig: &S3SignatureV4{
				Algorithm:     "AWS4-HMAC-SHA256",
				Credential:    "AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request",
				AccessKey:     "AKIAIOSFODNN7EXAMPLE",
				Date:          "20130524",
				Region:        "us-east-1",
				Service:       "s3",
				SignedHeaders: "host;range;x-amz-date",
				Signature:     "fe5f80f77d5fa3beca038a248ff027d0445342fe2855ddc963176630326f1024",
			},
			wantErr: false,
		},
		{
			name: "Valid SigV4 header without spaces after commas",
			authHeader: "AWS4-HMAC-SHA256 Credential=TESTKEY/20240101/eu-west-1/s3/aws4_request," +
				"SignedHeaders=host;x-amz-date," +
				"Signature=abcd1234",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("X-Amz-Date", "20240101T120000Z")
				return req
			},
			wantSig: &S3SignatureV4{
				Algorithm:     "AWS4-HMAC-SHA256",
				Credential:    "TESTKEY/20240101/eu-west-1/s3/aws4_request",
				AccessKey:     "TESTKEY",
				Date:          "20240101",
				Region:        "eu-west-1",
				Service:       "s3",
				SignedHeaders: "host;x-amz-date",
				Signature:     "abcd1234",
			},
			wantErr: false,
		},
		{
			name: "Date extracted from X-Amz-Date header when not in credential",
			authHeader: "AWS4-HMAC-SHA256 Credential=KEY123//us-east-1/s3/aws4_request, " +
				"SignedHeaders=host, Signature=sig123",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("X-Amz-Date", "20240101T120000Z")
				return req
			},
			wantSig: &S3SignatureV4{
				Algorithm:     "AWS4-HMAC-SHA256",
				Credential:    "KEY123//us-east-1/s3/aws4_request",
				AccessKey:     "KEY123",
				Date:          "20240101T120000Z", // Full X-Amz-Date value when credential date is empty
				Region:        "us-east-1",
				Service:       "s3",
				SignedHeaders: "host",
				Signature:     "sig123",
			},
			wantErr: false,
		},
		{
			name:       "Invalid prefix",
			authHeader: "AWS2 AKIAIOSFODNN7EXAMPLE:signature",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			wantSig: nil,
			wantErr: true,
		},
		{
			name:       "Empty header",
			authHeader: "",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			wantSig: nil,
			wantErr: true,
		},
		{
			name:       "Malformed header (missing parameters)",
			authHeader: "AWS4-HMAC-SHA256 Credential=KEY123",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			wantSig: &S3SignatureV4{
				Algorithm:     "AWS4-HMAC-SHA256",
				Credential:    "KEY123",
				AccessKey:     "", // Not extracted when no "/" in credential
				Date:          "",
				Region:        "us-east-1", // Default
				Service:       "s3",        // Default
				SignedHeaders: "",
				Signature:     "",
			},
			wantErr: false, // Parser is lenient
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			sig, err := manager.parseS3SignatureV4(tt.authHeader, req)

			if tt.wantErr {
				if err == nil {
					t.Error("parseS3SignatureV4() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("parseS3SignatureV4() unexpected error: %v", err)
				return
			}

			if sig.Algorithm != tt.wantSig.Algorithm {
				t.Errorf("Algorithm = %q, want %q", sig.Algorithm, tt.wantSig.Algorithm)
			}
			if sig.AccessKey != tt.wantSig.AccessKey {
				t.Errorf("AccessKey = %q, want %q", sig.AccessKey, tt.wantSig.AccessKey)
			}
			if sig.Date != tt.wantSig.Date {
				t.Errorf("Date = %q, want %q", sig.Date, tt.wantSig.Date)
			}
			if sig.Region != tt.wantSig.Region {
				t.Errorf("Region = %q, want %q", sig.Region, tt.wantSig.Region)
			}
			if sig.Service != tt.wantSig.Service {
				t.Errorf("Service = %q, want %q", sig.Service, tt.wantSig.Service)
			}
			if sig.SignedHeaders != tt.wantSig.SignedHeaders {
				t.Errorf("SignedHeaders = %q, want %q", sig.SignedHeaders, tt.wantSig.SignedHeaders)
			}
			if sig.Signature != tt.wantSig.Signature {
				t.Errorf("Signature = %q, want %q", sig.Signature, tt.wantSig.Signature)
			}
		})
	}
}

func TestParseS3SignatureV2(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access private methods
	manager := managerInterface.(*authManager)

	tests := []struct {
		name       string
		authHeader string
		setupReq   func() *http.Request
		wantSig    *S3SignatureV2
		wantErr    bool
	}{
		{
			name:       "Valid SigV2 header",
			authHeader: "AWS AKIAIOSFODNN7EXAMPLE:frJIUN8DYpKDtOLCwo//yllqDzg=",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			wantSig: &S3SignatureV2{
				AccessKey: "AKIAIOSFODNN7EXAMPLE",
				Signature: "frJIUN8DYpKDtOLCwo//yllqDzg=",
			},
			wantErr: false,
		},
		{
			name:       "Valid SigV2 with short key",
			authHeader: "AWS KEY123:abc123signature==",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			wantSig: &S3SignatureV2{
				AccessKey: "KEY123",
				Signature: "abc123signature==",
			},
			wantErr: false,
		},
		{
			name:       "Invalid prefix",
			authHeader: "AWS4-HMAC-SHA256 KEY:sig",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			wantSig: nil,
			wantErr: true,
		},
		{
			name:       "Missing colon",
			authHeader: "AWS KEYWITHOUTSIGNATURE",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			wantSig: nil,
			wantErr: true,
		},
		{
			name:       "Empty after AWS prefix",
			authHeader: "AWS ",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			wantSig: nil,
			wantErr: true,
		},
		{
			name:       "Multiple colons in signature",
			authHeader: "AWS KEY:sig:part:two",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			wantSig: &S3SignatureV2{
				AccessKey: "KEY",
				Signature: "sig:part:two", // Everything after first colon
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			sig, err := manager.parseS3SignatureV2(tt.authHeader, req)

			if tt.wantErr {
				if err == nil {
					t.Error("parseS3SignatureV2() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("parseS3SignatureV2() unexpected error: %v", err)
				return
			}

			if sig.AccessKey != tt.wantSig.AccessKey {
				t.Errorf("AccessKey = %q, want %q", sig.AccessKey, tt.wantSig.AccessKey)
			}
			if sig.Signature != tt.wantSig.Signature {
				t.Errorf("Signature = %q, want %q", sig.Signature, tt.wantSig.Signature)
			}
		})
	}
}

// ========================================
// Test Signature Verification Functions
// ========================================

func TestVerifyS3SignatureV4(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access private methods
	manager := managerInterface.(*authManager)

	// Test with known AWS example
	t.Run("Valid SigV4 signature verification", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/test.txt", nil)
		req.Host = "examplebucket.s3.amazonaws.com"
		req.Header.Set("X-Amz-Date", "20130524T000000Z")
		req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

		sig := &S3SignatureV4{
			AccessKey:     "AKIAIOSFODNN7EXAMPLE",
			Date:          "20130524",
			Region:        "us-east-1",
			Service:       "s3",
			SignedHeaders: "host;x-amz-content-sha256;x-amz-date",
			Signature:     "", // Will be calculated
		}

		secretKey := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

		// Calculate the expected signature
		canonicalRequest := manager.createCanonicalRequest(req, sig.SignedHeaders)
		canonicalRequestHash := fmt.Sprintf("%x", sha256.Sum256([]byte(canonicalRequest)))
		stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s/%s/%s/aws4_request\n%s",
			req.Header.Get("X-Amz-Date"),
			sig.Date,
			sig.Region,
			sig.Service,
			canonicalRequestHash)
		expectedSig := manager.calculateSignatureV4(stringToSign, secretKey, sig.Date, sig.Region, sig.Service)

		// Set the calculated signature
		sig.Signature = expectedSig

		// Verify
		result := manager.verifyS3SignatureV4(req, sig, secretKey)
		if !result {
			t.Error("verifyS3SignatureV4() = false, want true for valid signature")
		}
	})

	t.Run("Invalid signature fails verification", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/test.txt", nil)
		req.Host = "examplebucket.s3.amazonaws.com"
		req.Header.Set("X-Amz-Date", "20130524T000000Z")
		req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

		sig := &S3SignatureV4{
			AccessKey:     "AKIAIOSFODNN7EXAMPLE",
			Date:          "20130524",
			Region:        "us-east-1",
			Service:       "s3",
			SignedHeaders: "host;x-amz-content-sha256;x-amz-date",
			Signature:     "invalid_signature_12345",
		}

		secretKey := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

		result := manager.verifyS3SignatureV4(req, sig, secretKey)
		if result {
			t.Error("verifyS3SignatureV4() = true, want false for invalid signature")
		}
	})

	t.Run("Wrong secret key fails verification", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/test.txt", nil)
		req.Host = "examplebucket.s3.amazonaws.com"
		req.Header.Set("X-Amz-Date", "20130524T000000Z")
		req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

		sig := &S3SignatureV4{
			AccessKey:     "AKIAIOSFODNN7EXAMPLE",
			Date:          "20130524",
			Region:        "us-east-1",
			Service:       "s3",
			SignedHeaders: "host;x-amz-content-sha256;x-amz-date",
			Signature:     "", // Will be calculated with correct key
		}

		correctSecretKey := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
		wrongSecretKey := "wrongSecretKey123"

		// Calculate signature with correct key
		canonicalRequest := manager.createCanonicalRequest(req, sig.SignedHeaders)
		canonicalRequestHash := fmt.Sprintf("%x", sha256.Sum256([]byte(canonicalRequest)))
		stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s/%s/%s/aws4_request\n%s",
			req.Header.Get("X-Amz-Date"),
			sig.Date,
			sig.Region,
			sig.Service,
			canonicalRequestHash)
		sig.Signature = manager.calculateSignatureV4(stringToSign, correctSecretKey, sig.Date, sig.Region, sig.Service)

		// Verify with wrong key
		result := manager.verifyS3SignatureV4(req, sig, wrongSecretKey)
		if result {
			t.Error("verifyS3SignatureV4() = true, want false when using wrong secret key")
		}
	})
}

func TestVerifyS3SignatureV2(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access private methods
	manager := managerInterface.(*authManager)

	t.Run("Valid SigV2 signature verification", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
		req.Header.Set("Date", "Tue, 27 Mar 2007 19:36:42 +0000")

		secretKey := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

		// Calculate expected signature
		stringToSign := manager.createStringToSignV2(req)
		hash := hmac.New(sha256.New, []byte(secretKey))
		hash.Write([]byte(stringToSign))
		expectedSig := base64.StdEncoding.EncodeToString(hash.Sum(nil))

		sig := &S3SignatureV2{
			AccessKey: "AKIAIOSFODNN7EXAMPLE",
			Signature: expectedSig,
		}

		result := manager.verifyS3SignatureV2(req, sig, secretKey)
		if !result {
			t.Error("verifyS3SignatureV2() = false, want true for valid signature")
		}
	})

	t.Run("Invalid signature fails verification", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
		req.Header.Set("Date", "Tue, 27 Mar 2007 19:36:42 +0000")

		sig := &S3SignatureV2{
			AccessKey: "AKIAIOSFODNN7EXAMPLE",
			Signature: "invalid_signature",
		}

		secretKey := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

		result := manager.verifyS3SignatureV2(req, sig, secretKey)
		if result {
			t.Error("verifyS3SignatureV2() = true, want false for invalid signature")
		}
	})

	t.Run("Wrong secret key fails verification", func(t *testing.T) {
		req, _ := http.NewRequest("PUT", "/bucket/file.txt", nil)
		req.Header.Set("Date", "Tue, 27 Mar 2007 19:44:46 +0000")
		req.Header.Set("Content-Type", "text/plain")

		correctSecretKey := "correctSecret123"
		wrongSecretKey := "wrongSecret456"

		// Calculate signature with correct key
		stringToSign := manager.createStringToSignV2(req)
		hash := hmac.New(sha256.New, []byte(correctSecretKey))
		hash.Write([]byte(stringToSign))
		expectedSig := base64.StdEncoding.EncodeToString(hash.Sum(nil))

		sig := &S3SignatureV2{
			AccessKey: "TESTKEY",
			Signature: expectedSig,
		}

		// Verify with wrong key
		result := manager.verifyS3SignatureV2(req, sig, wrongSecretKey)
		if result {
			t.Error("verifyS3SignatureV2() = true, want false when using wrong secret key")
		}
	})
}

// ========================================
// Test Full Validation Flow
// ========================================

func TestValidateS3Signature_AutoDetect(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	ctx := context.Background()

	tests := []struct {
		name       string
		setupReq   func() *http.Request
		wantV4     bool
		wantErr    bool
		errType    error
	}{
		{
			name: "Detects SigV4 from Authorization header",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=KEY/20240101/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=sig")
				return req
			},
			wantV4:  true,
			wantErr: true, // Will fail because key doesn't exist in DB
			errType: ErrInvalidCredentials,
		},
		{
			name: "Detects SigV4 from X-Amz-Algorithm header",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("Authorization", "something")
				req.Header.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
				return req
			},
			wantV4:  true,
			wantErr: true,
		},
		{
			name: "Detects SigV2 from AWS prefix",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("Authorization", "AWS KEY:signature")
				return req
			},
			wantV4:  false,
			wantErr: true, // Will fail because key doesn't exist in DB
			errType: ErrInvalidCredentials,
		},
		{
			name: "Missing Authorization header",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				return req
			},
			wantV4:  false,
			wantErr: true,
			errType: ErrMissingSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			user, err := manager.ValidateS3Signature(ctx, req)

			if tt.wantErr {
				if err == nil {
					t.Error("ValidateS3Signature() expected error but got nil")
				}
				if tt.errType != nil && err != tt.errType {
					t.Errorf("ValidateS3Signature() error = %v, want %v", err, tt.errType)
				}
				return
			}

			if err != nil {
				t.Errorf("ValidateS3Signature() unexpected error: %v", err)
				return
			}

			if user == nil {
				t.Error("ValidateS3Signature() returned nil user")
			}
		})
	}
}

func TestValidateS3SignatureV4_FullFlow(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access private methods
	manager := managerInterface.(*authManager)

	ctx := context.Background()

	// Create test user
	user := &User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
		Roles:    []string{"user"},
	}
	err := managerInterface.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Generate access key
	accessKeyObj, err := managerInterface.GenerateAccessKey(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to create access key: %v", err)
	}
	accessKey := accessKeyObj.AccessKeyID
	secretKey := accessKeyObj.SecretAccessKey

	tests := []struct {
		name    string
		setupReq func() *http.Request
		wantErr bool
		errType error
	}{
		{
			name: "Valid SigV4 request",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
				req.Host = "s3.amazonaws.com"
				req.Header.Set("X-Amz-Date", "20240101T120000Z")
				req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

				sig := &S3SignatureV4{
					AccessKey:     accessKey,
					Date:          "20240101",
					Region:        "us-east-1",
					Service:       "s3",
					SignedHeaders: "host;x-amz-content-sha256;x-amz-date",
				}

				// Calculate correct signature
				canonicalRequest := manager.createCanonicalRequest(req, sig.SignedHeaders)
				canonicalRequestHash := fmt.Sprintf("%x", sha256.Sum256([]byte(canonicalRequest)))
				stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s/%s/%s/aws4_request\n%s",
					req.Header.Get("X-Amz-Date"),
					sig.Date,
					sig.Region,
					sig.Service,
					canonicalRequestHash)
				signature := manager.calculateSignatureV4(stringToSign, secretKey, sig.Date, sig.Region, sig.Service)

				authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s/%s/%s/aws4_request, SignedHeaders=%s, Signature=%s",
					accessKey, sig.Date, sig.Region, sig.Service, sig.SignedHeaders, signature)
				req.Header.Set("Authorization", authHeader)

				return req
			},
			wantErr: false,
		},
		{
			name: "Invalid signature",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
				req.Host = "s3.amazonaws.com"
				req.Header.Set("X-Amz-Date", "20240101T120000Z")
				req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

				authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/20240101/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=invalidsignature",
					accessKey)
				req.Header.Set("Authorization", authHeader)

				return req
			},
			wantErr: true,
			errType: ErrInvalidSignature,
		},
		{
			name: "Non-existent access key",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
				req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=NONEXISTENT/20240101/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=sig")
				return req
			},
			wantErr: true,
			errType: ErrInvalidCredentials,
		},
		{
			name: "Missing Authorization header",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
				return req
			},
			wantErr: true,
			errType: ErrMissingSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			resultUser, err := managerInterface.ValidateS3SignatureV4(ctx, req)

			if tt.wantErr {
				if err == nil {
					t.Error("ValidateS3SignatureV4() expected error but got nil")
				}
				if tt.errType != nil && err != tt.errType {
					t.Errorf("ValidateS3SignatureV4() error = %v, want %v", err, tt.errType)
				}
				return
			}

			if err != nil {
				t.Errorf("ValidateS3SignatureV4() unexpected error: %v", err)
				return
			}

			if resultUser == nil {
				t.Error("ValidateS3SignatureV4() returned nil user")
				return
			}

			if resultUser.ID != user.ID {
				t.Errorf("ValidateS3SignatureV4() returned user ID = %s, want %s", resultUser.ID, user.ID)
			}
		})
	}
}

func TestValidateS3SignatureV2_FullFlow(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)

	// Cast to *authManager to access private methods
	manager := managerInterface.(*authManager)

	ctx := context.Background()

	// Create test user
	user := &User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
		Roles:    []string{"user"},
	}
	err := managerInterface.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Generate access key
	accessKeyObj, err := managerInterface.GenerateAccessKey(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to create access key: %v", err)
	}
	accessKey := accessKeyObj.AccessKeyID
	secretKey := accessKeyObj.SecretAccessKey

	tests := []struct {
		name    string
		setupReq func() *http.Request
		wantErr bool
		errType error
	}{
		{
			name: "Valid SigV2 request",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
				req.Header.Set("Date", "Tue, 27 Mar 2007 19:36:42 +0000")

				// Calculate correct signature
				stringToSign := manager.createStringToSignV2(req)
				hash := hmac.New(sha256.New, []byte(secretKey))
				hash.Write([]byte(stringToSign))
				signature := base64.StdEncoding.EncodeToString(hash.Sum(nil))

				authHeader := fmt.Sprintf("AWS %s:%s", accessKey, signature)
				req.Header.Set("Authorization", authHeader)

				return req
			},
			wantErr: false,
		},
		{
			name: "Invalid signature",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
				req.Header.Set("Date", "Tue, 27 Mar 2007 19:36:42 +0000")

				authHeader := fmt.Sprintf("AWS %s:invalidsignature", accessKey)
				req.Header.Set("Authorization", authHeader)

				return req
			},
			wantErr: true,
			errType: ErrInvalidSignature,
		},
		{
			name: "Non-existent access key",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
				req.Header.Set("Authorization", "AWS NONEXISTENT:signature")
				return req
			},
			wantErr: true,
			errType: ErrInvalidCredentials,
		},
		{
			name: "Missing Authorization header",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
				return req
			},
			wantErr: true,
			errType: ErrMissingSignature,
		},
		{
			name: "Malformed Authorization header",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
				req.Header.Set("Authorization", "AWS MALFORMED")
				return req
			},
			wantErr: true,
			errType: ErrInvalidSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			resultUser, err := managerInterface.ValidateS3SignatureV2(ctx, req)

			if tt.wantErr {
				if err == nil {
					t.Error("ValidateS3SignatureV2() expected error but got nil")
				}
				if tt.errType != nil && err != tt.errType {
					t.Errorf("ValidateS3SignatureV2() error = %v, want %v", err, tt.errType)
				}
				return
			}

			if err != nil {
				t.Errorf("ValidateS3SignatureV2() unexpected error: %v", err)
				return
			}

			if resultUser == nil {
				t.Error("ValidateS3SignatureV2() returned nil user")
				return
			}

			if resultUser.ID != user.ID {
				t.Errorf("ValidateS3SignatureV2() returned user ID = %s, want %s", resultUser.ID, user.ID)
			}
		})
	}
}

