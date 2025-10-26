package presigned

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	// AWS signature version
	algorithm = "AWS4-HMAC-SHA256"

	// Service and request type
	service     = "s3"
	requestType = "aws4_request"

	// Default region (configurable)
	defaultRegion = "us-east-1"

	// Maximum expiration time (7 days)
	maxExpiration = 604800
)

// PresignedURLParams contains parameters for generating a presigned URL
type PresignedURLParams struct {
	Endpoint        string            // Base URL (e.g., "http://localhost:8080")
	Bucket          string            // Bucket name
	Key             string            // Object key
	TenantID        string            // Tenant ID (optional, for multi-tenant)
	AccessKeyID     string            // AWS access key ID
	SecretAccessKey string            // AWS secret access key
	Method          string            // HTTP method (GET, PUT, etc.)
	ExpiresIn       int64             // Expiration time in seconds (max 604800)
	Region          string            // AWS region (default: us-east-1)
	QueryParams     map[string]string // Additional query parameters (optional)
}

// GeneratePresignedURL generates an AWS S3 compatible presigned URL
func GeneratePresignedURL(params PresignedURLParams) (string, error) {
	// Validate parameters
	if params.Bucket == "" || params.Key == "" {
		return "", fmt.Errorf("bucket and key are required")
	}
	if params.AccessKeyID == "" || params.SecretAccessKey == "" {
		return "", fmt.Errorf("access key credentials are required")
	}
	if params.ExpiresIn <= 0 {
		params.ExpiresIn = 3600 // Default: 1 hour
	}
	if params.ExpiresIn > maxExpiration {
		return "", fmt.Errorf("expiration time cannot exceed %d seconds (7 days)", maxExpiration)
	}
	if params.Method == "" {
		params.Method = "GET"
	}
	if params.Region == "" {
		params.Region = defaultRegion
	}

	// Current timestamp
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	// Build credential scope
	credentialScope := fmt.Sprintf("%s/%s/%s/%s", dateStamp, params.Region, service, requestType)
	credential := fmt.Sprintf("%s/%s", params.AccessKeyID, credentialScope)

	// Build URL path
	var urlPath string
	if params.TenantID != "" {
		urlPath = fmt.Sprintf("/%s/%s/%s", params.TenantID, params.Bucket, params.Key)
	} else {
		urlPath = fmt.Sprintf("/%s/%s", params.Bucket, params.Key)
	}

	// Build canonical query string
	queryParams := make(map[string]string)

	// Add user-provided query parameters first
	for k, v := range params.QueryParams {
		queryParams[k] = v
	}

	// Add presigned URL parameters (these override any user-provided values)
	queryParams["X-Amz-Algorithm"] = algorithm
	queryParams["X-Amz-Credential"] = credential
	queryParams["X-Amz-Date"] = amzDate
	queryParams["X-Amz-Expires"] = fmt.Sprintf("%d", params.ExpiresIn)
	queryParams["X-Amz-SignedHeaders"] = "host"

	canonicalQueryString := buildCanonicalQueryString(queryParams)

	// Build canonical request
	host := extractHost(params.Endpoint)
	canonicalHeaders := fmt.Sprintf("host:%s\n", host)
	signedHeaders := "host"

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\nUNSIGNED-PAYLOAD",
		params.Method,
		urlPath,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
	)

	// Build string to sign
	requestHash := sha256Hash([]byte(canonicalRequest))
	stringToSign := fmt.Sprintf("%s\n%s\n%s\n%s",
		algorithm,
		amzDate,
		credentialScope,
		requestHash,
	)

	// Calculate signature
	signingKey := getSignatureKey(params.SecretAccessKey, dateStamp, params.Region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	// Add signature to query parameters
	queryParams["X-Amz-Signature"] = signature

	// Build final URL
	finalQueryString := buildCanonicalQueryString(queryParams)
	presignedURL := fmt.Sprintf("%s%s?%s", params.Endpoint, urlPath, finalQueryString)

	return presignedURL, nil
}

// buildCanonicalQueryString builds a canonical query string from parameters
func buildCanonicalQueryString(params map[string]string) string {
	// Sort keys
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build query string
	var parts []string
	for _, k := range keys {
		// URL encode both key and value
		encodedKey := url.QueryEscape(k)
		encodedValue := url.QueryEscape(params[k])
		parts = append(parts, fmt.Sprintf("%s=%s", encodedKey, encodedValue))
	}

	return strings.Join(parts, "&")
}

// extractHost extracts the host from an endpoint URL
func extractHost(endpoint string) string {
	// Remove protocol
	host := strings.TrimPrefix(endpoint, "http://")
	host = strings.TrimPrefix(host, "https://")

	// Remove trailing slash
	host = strings.TrimSuffix(host, "/")

	return host
}

// sha256Hash calculates the SHA256 hash of data
func sha256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// hmacSHA256 calculates HMAC-SHA256
func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// getSignatureKey derives the signing key
func getSignatureKey(secretKey, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte(requestType))
	return kSigning
}
