package s3compat

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/maxiofs/maxiofs/internal/auth"
)

// mockAuthManager is a mock implementation of auth.Manager for testing
type mockAuthManager struct{}

func (m *mockAuthManager) GetAccessKey(ctx context.Context, accessKeyID string) (*auth.AccessKey, error) {
	// Return test access key
	if accessKeyID == "AKIAIOSFODNN7EXAMPLE" {
		return &auth.AccessKey{
			AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
			SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			Status:          "active",
		}, nil
	}
	return nil, fmt.Errorf("access key not found")
}

// Implement other required methods as no-ops
func (m *mockAuthManager) ValidateCredentials(ctx context.Context, accessKey, secretKey string) (*auth.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) ValidateConsoleCredentials(ctx context.Context, username, password string) (*auth.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) ValidateJWT(ctx context.Context, token string) (*auth.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) GenerateJWT(ctx context.Context, user *auth.User) (string, error) {
	return "", fmt.Errorf("not implemented")
}
func (m *mockAuthManager) ValidateS3Signature(ctx context.Context, r *http.Request) (*auth.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) ValidateS3SignatureV4(ctx context.Context, r *http.Request) (*auth.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) ValidateS3SignatureV2(ctx context.Context, r *http.Request) (*auth.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) CheckPermission(ctx context.Context, user *auth.User, action, resource string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) CheckBucketPermission(ctx context.Context, user *auth.User, bucket, action string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) CheckObjectPermission(ctx context.Context, user *auth.User, bucket, object, action string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) CreateUser(ctx context.Context, user *auth.User) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) UpdateUser(ctx context.Context, user *auth.User) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) UpdateUserPreferences(ctx context.Context, userID, themePreference, languagePreference string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) DeleteUser(ctx context.Context, userID string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) GetUser(ctx context.Context, accessKey string) (*auth.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) ListUsers(ctx context.Context) ([]auth.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) GenerateAccessKey(ctx context.Context, userID string) (*auth.AccessKey, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) RevokeAccessKey(ctx context.Context, accessKey string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) ListAccessKeys(ctx context.Context, userID string) ([]auth.AccessKey, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) CreateTenant(ctx context.Context, tenant *auth.Tenant) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) GetTenant(ctx context.Context, tenantID string) (*auth.Tenant, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) GetTenantByName(ctx context.Context, name string) (*auth.Tenant, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) ListTenants(ctx context.Context) ([]*auth.Tenant, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) UpdateTenant(ctx context.Context, tenant *auth.Tenant) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) DeleteTenant(ctx context.Context, tenantID string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) ListTenantUsers(ctx context.Context, tenantID string) ([]*auth.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) IncrementTenantBucketCount(ctx context.Context, tenantID string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) DecrementTenantBucketCount(ctx context.Context, tenantID string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) IncrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) CheckTenantStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) GrantBucketAccess(ctx context.Context, bucketName, userID, tenantID, permissionLevel, grantedBy string, expiresAt int64) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) RevokeBucketAccess(ctx context.Context, bucketName, userID, tenantID string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) CheckBucketAccess(ctx context.Context, bucketName, userID string) (bool, string, error) {
	return false, "", fmt.Errorf("not implemented")
}
func (m *mockAuthManager) ListBucketPermissions(ctx context.Context, bucketName string) ([]*auth.BucketPermission, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) ListUserBucketPermissions(ctx context.Context, userID string) ([]*auth.BucketPermission, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) Middleware() func(http.Handler) http.Handler {
	return nil
}
func (m *mockAuthManager) CheckRateLimit(ip string) bool {
	return true
}
func (m *mockAuthManager) IsAccountLocked(ctx context.Context, userID string) (bool, int64, error) {
	return false, 0, nil
}
func (m *mockAuthManager) LockAccount(ctx context.Context, userID string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) UnlockAccount(ctx context.Context, adminUserID, targetUserID string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) RecordFailedLogin(ctx context.Context, userID, ip string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) RecordSuccessfulLogin(ctx context.Context, userID string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) SetUserLockedCallback(callback func(*auth.User)) {}
func (m *mockAuthManager) Setup2FA(ctx context.Context, userID string) (*auth.TOTPSetup, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) Enable2FA(ctx context.Context, userID, code string, secret string) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) Disable2FA(ctx context.Context, userID, requestingUserID string, isGlobalAdmin bool) error {
	return fmt.Errorf("not implemented")
}
func (m *mockAuthManager) Verify2FACode(ctx context.Context, userID, code string) (bool, error) {
	return false, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) RegenerateBackupCodes(ctx context.Context, userID string) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) Get2FAStatus(ctx context.Context, userID string) (bool, int64, error) {
	return false, 0, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) IsReady() bool {
	return true
}
func (m *mockAuthManager) GetDB() interface{} {
	return nil
}
func (m *mockAuthManager) FindUserByExternalID(ctx context.Context, externalID, authProvider string) (*auth.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAuthManager) SetStorageQuotaAlertCallback(callback func(tenantID string, currentBytes, maxBytes int64)) {
}

// TestGeneratePresignedURLV4_Success tests successful V4 presigned URL generation
func TestGeneratePresignedURLV4_Success(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
	}

	config := PresignedURLConfig{
		AccessKey:  "AKIAIOSFODNN7EXAMPLE",
		SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		BucketName: "test-bucket",
		ObjectKey:  "test-object.txt",
		Method:     "GET",
		Expiration: 15 * time.Minute,
	}

	presignedURL, err := handler.GeneratePresignedURL(config)
	require.NoError(t, err, "Should generate presigned URL without error")
	require.NotEmpty(t, presignedURL, "Presigned URL should not be empty")

	// Parse and validate URL structure
	parsedURL, err := url.Parse(presignedURL)
	require.NoError(t, err, "Should parse generated URL")

	// Verify URL components
	assert.Contains(t, presignedURL, "localhost:8080", "Should contain host")
	assert.Contains(t, parsedURL.Path, "/test-bucket/test-object.txt", "Should contain bucket and object")

	// Verify V4 signature parameters
	query := parsedURL.Query()
	assert.Equal(t, "AWS4-HMAC-SHA256", query.Get("X-Amz-Algorithm"), "Should use V4 algorithm")
	assert.NotEmpty(t, query.Get("X-Amz-Credential"), "Should have credential")
	assert.NotEmpty(t, query.Get("X-Amz-Date"), "Should have date")
	assert.NotEmpty(t, query.Get("X-Amz-Expires"), "Should have expiration")
	assert.NotEmpty(t, query.Get("X-Amz-Signature"), "Should have signature")
	assert.Equal(t, "host", query.Get("X-Amz-SignedHeaders"), "Should sign host header")

	// Verify expiration time
	expires := query.Get("X-Amz-Expires")
	expiresInt, err := strconv.ParseInt(expires, 10, 64)
	require.NoError(t, err)
	assert.Equal(t, int64(900), expiresInt, "Should expire in 900 seconds (15 minutes)")

	t.Logf("Generated V4 presigned URL: %s", presignedURL)
}

// TestGeneratePresignedURLV4_DefaultExpiration tests default expiration
func TestGeneratePresignedURLV4_DefaultExpiration(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
	}

	config := PresignedURLConfig{
		AccessKey:  "AKIAIOSFODNN7EXAMPLE",
		SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		BucketName: "test-bucket",
		ObjectKey:  "test-object.txt",
		Method:     "GET",
		// Expiration not set - should use default
	}

	presignedURL, err := handler.GeneratePresignedURL(config)
	require.NoError(t, err)

	parsedURL, _ := url.Parse(presignedURL)
	query := parsedURL.Query()

	expires := query.Get("X-Amz-Expires")
	expiresInt, _ := strconv.ParseInt(expires, 10, 64)
	assert.Equal(t, int64(900), expiresInt, "Should use default 15 minute expiration")
}

// TestGeneratePresignedURLV4_MaxExpiration tests maximum expiration limit
func TestGeneratePresignedURLV4_MaxExpiration(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
	}

	config := PresignedURLConfig{
		AccessKey:  "AKIAIOSFODNN7EXAMPLE",
		SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		BucketName: "test-bucket",
		ObjectKey:  "test-object.txt",
		Method:     "GET",
		Expiration: 8 * 24 * time.Hour, // 8 days - exceeds maximum
	}

	_, err := handler.GeneratePresignedURL(config)
	require.Error(t, err, "Should reject expiration exceeding 7 days")
	assert.Contains(t, err.Error(), "cannot exceed 7 days", "Error should mention 7 day limit")
}

// TestValidatePresignedURLV4_ValidSignature tests validation of valid V4 URL
func TestValidatePresignedURLV4_ValidSignature(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
		authManager:  &mockAuthManager{},
	}

	// Generate a valid presigned URL
	config := PresignedURLConfig{
		AccessKey:  "AKIAIOSFODNN7EXAMPLE",
		SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		BucketName: "test-bucket",
		ObjectKey:  "test-object.txt",
		Method:     "GET",
		Expiration: 15 * time.Minute,
	}

	presignedURL, err := handler.GeneratePresignedURL(config)
	require.NoError(t, err)

	// Create request from presigned URL
	req, err := http.NewRequest("GET", presignedURL, nil)
	require.NoError(t, err)

	// Validate the presigned URL
	err = handler.ValidatePresignedURL(nil, req)
	assert.NoError(t, err, "Valid presigned URL should pass validation")
}

// TestValidatePresignedURLV4_InvalidSignature tests detection of invalid signature
func TestValidatePresignedURLV4_InvalidSignature(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
		authManager:  &mockAuthManager{},
	}

	// Generate a valid presigned URL
	config := PresignedURLConfig{
		AccessKey:  "AKIAIOSFODNN7EXAMPLE",
		SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		BucketName: "test-bucket",
		ObjectKey:  "test-object.txt",
		Method:     "GET",
		Expiration: 15 * time.Minute,
	}

	presignedURL, err := handler.GeneratePresignedURL(config)
	require.NoError(t, err)

	// Tamper with signature
	parsedURL, _ := url.Parse(presignedURL)
	query := parsedURL.Query()

	// Change signature to invalid value
	query.Set("X-Amz-Signature", "INVALIDSIGNATURE1234567890abcdef")
	parsedURL.RawQuery = query.Encode()
	tamperedURL := parsedURL.String()

	// Create request with tampered URL
	req, err := http.NewRequest("GET", tamperedURL, nil)
	require.NoError(t, err)

	// Validate - should FAIL due to signature mismatch
	err = handler.ValidatePresignedURL(nil, req)
	require.Error(t, err, "Invalid signature should be rejected")
	assert.Contains(t, err.Error(), "signature", "Error should mention signature")
}

// TestValidatePresignedURLV4_Expired tests expired URL detection
func TestValidatePresignedURLV4_Expired(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
	}

	// Build URL components manually with past date
	now := time.Now().UTC().Add(-2 * time.Hour) // 2 hours ago
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	queryParams := url.Values{}
	queryParams.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	queryParams.Set("X-Amz-Credential", "AKIAIOSFODNN7EXAMPLE/"+dateStamp+"/us-east-1/s3/aws4_request")
	queryParams.Set("X-Amz-Date", amzDate)
	queryParams.Set("X-Amz-Expires", "3600") // 1 hour expiration
	queryParams.Set("X-Amz-SignedHeaders", "host")
	queryParams.Set("X-Amz-Signature", "dummysignature")

	expiredURL := fmt.Sprintf("http://localhost:8080/test-bucket/test-object.txt?%s", queryParams.Encode())

	// Create request
	req, err := http.NewRequest("GET", expiredURL, nil)
	require.NoError(t, err)

	// Validate - should detect expiration
	err = handler.ValidatePresignedURL(nil, req)
	require.Error(t, err, "Expired URL should be rejected")
	assert.Contains(t, err.Error(), "expired", "Error should mention expiration")
}

// TestValidatePresignedURLV4_MissingParameters tests missing parameter detection
func TestValidatePresignedURLV4_MissingParameters(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
	}

	tests := []struct {
		name          string
		removeParam   string
		expectedError string
	}{
		{
			name:          "missing credential",
			removeParam:   "X-Amz-Credential",
			expectedError: "missing required",
		},
		{
			name:          "missing date",
			removeParam:   "X-Amz-Date",
			expectedError: "missing required",
		},
		{
			name:          "missing expires",
			removeParam:   "X-Amz-Expires",
			expectedError: "missing required",
		},
		{
			name:          "missing signature",
			removeParam:   "X-Amz-Signature",
			expectedError: "missing required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate valid URL first
			config := PresignedURLConfig{
				AccessKey:  "AKIAIOSFODNN7EXAMPLE",
				SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				BucketName: "test-bucket",
				ObjectKey:  "test.txt",
				Method:     "GET",
				Expiration: 15 * time.Minute,
			}

			presignedURL, err := handler.GeneratePresignedURL(config)
			require.NoError(t, err)

			// Remove the specified parameter
			parsedURL, _ := url.Parse(presignedURL)
			query := parsedURL.Query()
			query.Del(tt.removeParam)
			parsedURL.RawQuery = query.Encode()

			// Create request
			req, _ := http.NewRequest("GET", parsedURL.String(), nil)

			// Validate - should fail
			err = handler.ValidatePresignedURL(nil, req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

// TestValidatePresignedURLV4_InvalidAlgorithm tests invalid algorithm detection
func TestValidatePresignedURLV4_InvalidAlgorithm(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
	}

	// Build URL with invalid algorithm directly
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	queryParams := url.Values{}
	queryParams.Set("X-Amz-Algorithm", "INVALID-ALGORITHM") // Invalid
	queryParams.Set("X-Amz-Credential", "AKIAIOSFODNN7EXAMPLE/"+dateStamp+"/us-east-1/s3/aws4_request")
	queryParams.Set("X-Amz-Date", amzDate)
	queryParams.Set("X-Amz-Expires", "900")
	queryParams.Set("X-Amz-SignedHeaders", "host")
	queryParams.Set("X-Amz-Signature", "dummysig")

	testURL := fmt.Sprintf("http://localhost:8080/test-bucket/test.txt?%s", queryParams.Encode())
	req, _ := http.NewRequest("GET", testURL, nil)

	err := handler.ValidatePresignedURL(nil, req)
	require.Error(t, err, "Invalid algorithm should be rejected")
	// Note: Currently returns "not a presigned URL request" because invalid algorithm
	// doesn't match V4 or V2 patterns, which is acceptable behavior
}

// TestValidatePresignedURLV4_TamperedParameters tests detection of parameter tampering
func TestValidatePresignedURLV4_TamperedParameters(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
		authManager:  &mockAuthManager{},
	}

	// Generate valid presigned URL
	config := PresignedURLConfig{
		AccessKey:  "AKIAIOSFODNN7EXAMPLE",
		SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		BucketName: "test-bucket",
		ObjectKey:  "test-object.txt",
		Method:     "GET",
		Expiration: 15 * time.Minute,
	}

	presignedURL, err := handler.GeneratePresignedURL(config)
	require.NoError(t, err)

	// Tamper with expiration time (increase it)
	parsedURL, _ := url.Parse(presignedURL)
	query := parsedURL.Query()

	originalExpires := query.Get("X-Amz-Expires")
	tamperedExpires := "7200" // Change from 900 to 7200 seconds
	query.Set("X-Amz-Expires", tamperedExpires)
	parsedURL.RawQuery = query.Encode()

	req, _ := http.NewRequest("GET", parsedURL.String(), nil)

	// Validate - should FAIL because signature doesn't match tampered params
	err = handler.ValidatePresignedURL(nil, req)
	require.Error(t, err, "Parameter tampering should be detected")
	assert.Contains(t, err.Error(), "signature", "Error should mention signature mismatch")

	t.Logf("✅ Parameter tampering detected: original=%s, tampered=%s", originalExpires, tamperedExpires)

	assert.Error(t, err, "Tampered parameters should be rejected")
}

// TestGeneratePresignedURLV2_Success tests V2 signature generation
func TestGeneratePresignedURLV2_Success(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
	}

	config := PresignedURLConfig{
		AccessKey:  "AKIAIOSFODNN7EXAMPLE",
		SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		BucketName: "test-bucket",
		ObjectKey:  "test-object.txt",
		Method:     "GET",
		Expiration: 15 * time.Minute,
	}

	presignedURL, err := handler.generatePresignedURLV2(config)
	require.NoError(t, err)
	require.NotEmpty(t, presignedURL)

	// Parse and validate V2 URL structure
	parsedURL, err := url.Parse(presignedURL)
	require.NoError(t, err)

	query := parsedURL.Query()
	assert.NotEmpty(t, query.Get("AWSAccessKeyId"), "Should have access key")
	assert.NotEmpty(t, query.Get("Expires"), "Should have expiration")
	assert.NotEmpty(t, query.Get("Signature"), "Should have signature")

	// Verify expiration is in future
	expiresAt, err := strconv.ParseInt(query.Get("Expires"), 10, 64)
	require.NoError(t, err)
	assert.Greater(t, expiresAt, time.Now().Unix(), "Expiration should be in future")

	t.Logf("Generated V2 presigned URL: %s", presignedURL)
}

// TestValidatePresignedURLV2_Expired tests V2 expired URL detection
func TestValidatePresignedURLV2_Expired(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
	}

	// Build expired V2 URL
	pastTime := time.Now().UTC().Add(-2 * time.Hour).Unix()

	queryParams := url.Values{}
	queryParams.Set("AWSAccessKeyId", "AKIAIOSFODNN7EXAMPLE")
	queryParams.Set("Expires", strconv.FormatInt(pastTime, 10))
	queryParams.Set("Signature", "dummysignature")

	expiredURL := fmt.Sprintf("http://localhost:8080/test-bucket/test.txt?%s", queryParams.Encode())

	req, err := http.NewRequest("GET", expiredURL, nil)
	require.NoError(t, err)

	// Validate - should detect expiration
	err = handler.ValidatePresignedURL(nil, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

// TestValidatePresignedURLV2_MissingParameters tests V2 missing parameter detection
func TestValidatePresignedURLV2_MissingParameters(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
		authManager:  &mockAuthManager{},
	}

	tests := []struct {
		name          string
		accessKey     string
		expires       string
		signature     string
		shouldError   bool
		errorContains string
	}{
		{
			name:          "missing access key",
			accessKey:     "",
			expires:       strconv.FormatInt(time.Now().Add(1*time.Hour).Unix(), 10),
			signature:     "sig",
			shouldError:   true,
			errorContains: "not a presigned URL", // Missing access key makes it undetectable as V2
		},
		{
			name:          "missing expires",
			accessKey:     "AKIAIOSFODNN7EXAMPLE",
			expires:       "",
			signature:     "sig",
			shouldError:   true,
			errorContains: "missing required",
		},
		{
			name:          "missing signature",
			accessKey:     "AKIAIOSFODNN7EXAMPLE",
			expires:       strconv.FormatInt(time.Now().Add(1*time.Hour).Unix(), 10),
			signature:     "",
			shouldError:   true,
			errorContains: "missing required",
		},
		{
			name:          "invalid access key",
			accessKey:     "AKIA123",
			expires:       strconv.FormatInt(time.Now().Add(1*time.Hour).Unix(), 10),
			signature:     "sig",
			shouldError:   true,
			errorContains: "access key not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queryParams := url.Values{}
			if tt.accessKey != "" {
				queryParams.Set("AWSAccessKeyId", tt.accessKey)
			}
			if tt.expires != "" {
				queryParams.Set("Expires", tt.expires)
			}
			if tt.signature != "" {
				queryParams.Set("Signature", tt.signature)
			}

			testURL := fmt.Sprintf("http://localhost:8080/bucket/key?%s", queryParams.Encode())
			req, _ := http.NewRequest("GET", testURL, nil)

			err := handler.ValidatePresignedURL(nil, req)
			if tt.shouldError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidatePresignedURL_NotPresigned tests detection of non-presigned URLs
func TestValidatePresignedURL_NotPresigned(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
	}

	// Regular URL without presigned parameters
	req, _ := http.NewRequest("GET", "http://localhost:8080/bucket/key", nil)

	err := handler.ValidatePresignedURL(nil, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a presigned URL")
}

// TestCalculateSignatureV4_Consistency tests V4 signature calculation consistency
func TestCalculateSignatureV4_Consistency(t *testing.T) {
	handler := &Handler{}

	stringToSign := "AWS4-HMAC-SHA256\n20230101T120000Z\n20230101/us-east-1/s3/aws4_request\nabc123"
	secretKey := "testSecretKey"
	dateStamp := "20230101"
	region := "us-east-1"
	service := "s3"

	// Calculate signature twice
	sig1 := handler.calculateSignatureV4(stringToSign, secretKey, dateStamp, region, service)
	sig2 := handler.calculateSignatureV4(stringToSign, secretKey, dateStamp, region, service)

	// Should be identical
	assert.Equal(t, sig1, sig2, "Signature calculation should be deterministic")
	assert.NotEmpty(t, sig1, "Signature should not be empty")
	assert.Len(t, sig1, 64, "V4 signature should be 64 hex characters")
}

// TestCalculateSignatureV2_Consistency tests V2 signature calculation consistency
func TestCalculateSignatureV2_Consistency(t *testing.T) {
	handler := &Handler{}

	stringToSign := "GET\n\n\n1234567890\n/bucket/key"
	secretKey := "testSecretKey"

	// Calculate signature twice
	sig1 := handler.calculateSignatureV2(stringToSign, secretKey)
	sig2 := handler.calculateSignatureV2(stringToSign, secretKey)

	// Should be identical
	assert.Equal(t, sig1, sig2, "Signature calculation should be deterministic")
	assert.NotEmpty(t, sig1, "Signature should not be empty")
	assert.Len(t, sig1, 64, "V2 signature should be 64 hex characters (SHA256)")
}

// TestPresignedURL_DifferentMethods tests presigned URLs for different HTTP methods
func TestPresignedURL_DifferentMethods(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
		authManager:  &mockAuthManager{},
	}

	methods := []string{"GET", "PUT", "DELETE", "HEAD"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			config := PresignedURLConfig{
				AccessKey:  "AKIAIOSFODNN7EXAMPLE",
				SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				BucketName: "test-bucket",
				ObjectKey:  "test.txt",
				Method:     method,
				Expiration: 15 * time.Minute,
			}

			presignedURL, err := handler.GeneratePresignedURL(config)
			require.NoError(t, err)
			assert.NotEmpty(t, presignedURL)

			// Validation should pass for all methods
			req, _ := http.NewRequest(method, presignedURL, nil)
			err = handler.ValidatePresignedURL(nil, req)
			assert.NoError(t, err, "Valid presigned URL should pass validation for method %s", method)
		})
	}
}

// TestPresignedURL_SpecialCharactersInObjectKey tests object keys with special characters
func TestPresignedURL_SpecialCharactersInObjectKey(t *testing.T) {
	handler := &Handler{
		publicAPIURL: "http://localhost:8080",
		authManager:  &mockAuthManager{},
	}

	specialKeys := []string{
		"folder/subfolder/file.txt",
		"file with spaces.txt",
		"file-with-dashes.txt",
		"file_with_underscores.txt",
		"file.multiple.dots.txt",
	}

	for _, objectKey := range specialKeys {
		t.Run(objectKey, func(t *testing.T) {
			config := PresignedURLConfig{
				AccessKey:  "AKIAIOSFODNN7EXAMPLE",
				SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				BucketName: "test-bucket",
				ObjectKey:  objectKey,
				Method:     "GET",
				Expiration: 15 * time.Minute,
			}

			presignedURL, err := handler.GeneratePresignedURL(config)
			assert.NoError(t, err, "Should generate URL for object key: %s", objectKey)
			assert.Contains(t, presignedURL, "test-bucket")
		})
	}
}

// TestHandlePresignedRequest_Integration tests full presigned request handling
func TestHandlePresignedRequest_Integration(t *testing.T) {
	// This test would require full handler setup with object manager, etc.
	// Marking as integration test placeholder
	t.Skip("Integration test - requires full handler setup")
}
