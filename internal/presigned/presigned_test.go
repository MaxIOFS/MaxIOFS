package presigned

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePresignedURL(t *testing.T) {
	params := PresignedURLParams{
		Endpoint:        "http://localhost:8080",
		Bucket:          "test-bucket",
		Key:             "test-key.txt",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Method:          "GET",
		ExpiresIn:       3600,
		Region:          "us-east-1",
	}

	url, err := GeneratePresignedURL(params)
	require.NoError(t, err)
	assert.NotEmpty(t, url)
	assert.Contains(t, url, "X-Amz-Algorithm=AWS4-HMAC-SHA256")
	assert.Contains(t, url, "X-Amz-Credential=")
	assert.Contains(t, url, "X-Amz-Date=")
	assert.Contains(t, url, "X-Amz-Expires=3600")
	assert.Contains(t, url, "X-Amz-Signature=")
	assert.Contains(t, url, "test-bucket/test-key.txt")
}

func TestGeneratePresignedURL_MissingBucket(t *testing.T) {
	params := PresignedURLParams{
		Endpoint:        "http://localhost:8080",
		Key:             "test-key.txt",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}

	_, err := GeneratePresignedURL(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bucket and key are required")
}

func TestGeneratePresignedURL_MissingKey(t *testing.T) {
	params := PresignedURLParams{
		Endpoint:        "http://localhost:8080",
		Bucket:          "test-bucket",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}

	_, err := GeneratePresignedURL(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bucket and key are required")
}

func TestGeneratePresignedURL_MissingCredentials(t *testing.T) {
	params := PresignedURLParams{
		Endpoint: "http://localhost:8080",
		Bucket:   "test-bucket",
		Key:      "test-key.txt",
	}

	_, err := GeneratePresignedURL(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "access key credentials are required")
}

func TestGeneratePresignedURL_ExcessiveExpiration(t *testing.T) {
	params := PresignedURLParams{
		Endpoint:        "http://localhost:8080",
		Bucket:          "test-bucket",
		Key:             "test-key.txt",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		ExpiresIn:       604801, // More than 7 days
	}

	_, err := GeneratePresignedURL(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expiration time cannot exceed")
}

func TestGeneratePresignedURL_DefaultExpiration(t *testing.T) {
	params := PresignedURLParams{
		Endpoint:        "http://localhost:8080",
		Bucket:          "test-bucket",
		Key:             "test-key.txt",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}

	url, err := GeneratePresignedURL(params)
	require.NoError(t, err)
	assert.Contains(t, url, "X-Amz-Expires=3600")
}

func TestGeneratePresignedURL_DefaultMethod(t *testing.T) {
	params := PresignedURLParams{
		Endpoint:        "http://localhost:8080",
		Bucket:          "test-bucket",
		Key:             "test-key.txt",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		ExpiresIn:       3600,
	}

	url, err := GeneratePresignedURL(params)
	require.NoError(t, err)
	assert.NotEmpty(t, url)
}

func TestGeneratePresignedURL_PutMethod(t *testing.T) {
	params := PresignedURLParams{
		Endpoint:        "http://localhost:8080",
		Bucket:          "test-bucket",
		Key:             "test-key.txt",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Method:          "PUT",
		ExpiresIn:       3600,
	}

	url, err := GeneratePresignedURL(params)
	require.NoError(t, err)
	assert.NotEmpty(t, url)
}

func TestGeneratePresignedURL_WithQueryParams(t *testing.T) {
	params := PresignedURLParams{
		Endpoint:        "http://localhost:8080",
		Bucket:          "test-bucket",
		Key:             "test-key.txt",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Method:          "GET",
		ExpiresIn:       3600,
		QueryParams: map[string]string{
			"response-content-type": "application/json",
		},
	}

	url, err := GeneratePresignedURL(params)
	require.NoError(t, err)
	assert.Contains(t, url, "response-content-type=")
}

func TestIsPresignedURL(t *testing.T) {
	// Valid presigned URL
	req, _ := http.NewRequest("GET", "http://localhost:8080/bucket/key?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=xxx&X-Amz-Date=20231201T120000Z&X-Amz-Expires=3600&X-Amz-Signature=xxx", nil)
	assert.True(t, IsPresignedURL(req))

	// Not a presigned URL
	req, _ = http.NewRequest("GET", "http://localhost:8080/bucket/key", nil)
	assert.False(t, IsPresignedURL(req))
}

func TestExtractAccessKeyID(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://localhost:8080/bucket/key?X-Amz-Credential=AKIAIOSFODNN7EXAMPLE/20231201/us-east-1/s3/aws4_request", nil)

	accessKeyID := ExtractAccessKeyID(req)
	assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", accessKeyID)
}

func TestExtractAccessKeyID_Empty(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://localhost:8080/bucket/key", nil)

	accessKeyID := ExtractAccessKeyID(req)
	assert.Empty(t, accessKeyID)
}

func TestBuildCanonicalQueryString(t *testing.T) {
	params := map[string]string{
		"b": "value2",
		"a": "value1",
		"c": "value3",
	}

	result := buildCanonicalQueryString(params)

	// Should be alphabetically sorted
	assert.Equal(t, "a=value1&b=value2&c=value3", result)
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		endpoint string
		expected string
	}{
		{"http://localhost:8080", "localhost:8080"},
		{"https://s3.amazonaws.com", "s3.amazonaws.com"},
		{"http://localhost:8080/", "localhost:8080"},
		{"localhost:8080", "localhost:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			result := extractHost(tt.endpoint)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSha256Hash(t *testing.T) {
	data := []byte("test data")
	hash := sha256Hash(data)

	assert.NotEmpty(t, hash)
	assert.Len(t, hash, 64) // SHA256 produces 64 hex chars
}

func TestHmacSHA256(t *testing.T) {
	key := []byte("secret-key")
	data := []byte("test data")

	result := hmacSHA256(key, data)
	assert.NotEmpty(t, result)
	assert.Len(t, result, 32) // SHA256 produces 32 bytes
}

func TestGetSignatureKey(t *testing.T) {
	secretKey := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	dateStamp := "20231201"
	region := "us-east-1"
	service := "s3"

	signingKey := getSignatureKey(secretKey, dateStamp, region, service)
	assert.NotEmpty(t, signingKey)
	assert.Len(t, signingKey, 32)
}

func TestValidatePresignedURL_NotPresigned(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://localhost:8080/bucket/key", nil)

	valid, err := ValidatePresignedURL(req, "secret-key")
	assert.False(t, valid)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a presigned URL request")
}

func TestValidatePresignedURL_InvalidAlgorithm(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://localhost:8080/bucket/key?X-Amz-Algorithm=INVALID&X-Amz-Credential=xxx/20231201/us-east-1/s3/aws4_request&X-Amz-Date=20231201T120000Z&X-Amz-Expires=3600&X-Amz-Signature=xxx", nil)

	valid, err := ValidatePresignedURL(req, "secret-key")
	assert.False(t, valid)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid algorithm")
}

func TestValidatePresignedURL_Expired(t *testing.T) {
	// Create a URL that expired 1 hour ago
	pastTime := time.Now().UTC().Add(-2 * time.Hour)
	amzDate := pastTime.Format("20060102T150405Z")

	reqURL := "http://localhost:8080/bucket/key?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE/20231201/us-east-1/s3/aws4_request&X-Amz-Date=" + amzDate + "&X-Amz-Expires=3600&X-Amz-SignedHeaders=host&X-Amz-Signature=xxx"
	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Host = "localhost:8080"

	valid, err := ValidatePresignedURL(req, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	assert.False(t, valid)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestBuildCanonicalQueryStringForValidation(t *testing.T) {
	queryValues := url.Values{}
	queryValues.Add("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	queryValues.Add("X-Amz-Signature", "shouldbeexcluded")
	queryValues.Add("X-Amz-Date", "20231201T120000Z")

	result := buildCanonicalQueryStringForValidation(queryValues)

	// Should not contain signature
	assert.NotContains(t, result, "X-Amz-Signature")
	assert.Contains(t, result, "X-Amz-Algorithm")
	assert.Contains(t, result, "X-Amz-Date")
}
