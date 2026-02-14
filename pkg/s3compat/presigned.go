package s3compat

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

// PresignedURLConfig holds configuration for presigned URL generation
type PresignedURLConfig struct {
	AccessKey  string
	SecretKey  string
	BucketName string
	ObjectKey  string
	Method     string
	Expiration time.Duration
	Headers    map[string]string
}

// GeneratePresignedURL generates a presigned URL for S3 operations
// Supports both V2 and V4 signature methods
func (h *Handler) GeneratePresignedURL(config PresignedURLConfig) (string, error) {
	if config.Expiration == 0 {
		config.Expiration = 15 * time.Minute // Default 15 minutes
	}

	if config.Expiration > 7*24*time.Hour {
		return "", fmt.Errorf("expiration cannot exceed 7 days")
	}

	if config.Method == "" {
		config.Method = "GET"
	}

	// Generate V4 presigned URL (modern method)
	return h.generatePresignedURLV4(config)
}

// generatePresignedURLV4 generates AWS Signature V4 presigned URL
func (h *Handler) generatePresignedURLV4(config PresignedURLConfig) (string, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(config.Expiration)
	expiresInSeconds := int64(config.Expiration.Seconds())

	// Date strings for V4 signing
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	// Construct the canonical request
	region := "us-east-1"
	service := "s3"

	// Build URL path
	path := fmt.Sprintf("/%s/%s", config.BucketName, config.ObjectKey)

	// Build query parameters for V4
	queryParams := url.Values{}
	queryParams.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	queryParams.Set("X-Amz-Credential", fmt.Sprintf("%s/%s/%s/%s/aws4_request",
		config.AccessKey, dateStamp, region, service))
	queryParams.Set("X-Amz-Date", amzDate)
	queryParams.Set("X-Amz-Expires", strconv.FormatInt(expiresInSeconds, 10))
	queryParams.Set("X-Amz-SignedHeaders", "host")

	// Extract host from public API URL (remove protocol)
	host := strings.TrimPrefix(h.publicAPIURL, "http://")
	host = strings.TrimPrefix(host, "https://")

	// Create canonical request
	canonicalQueryString := queryParams.Encode()
	canonicalHeaders := fmt.Sprintf("host:%s\n", host)
	signedHeaders := "host"
	payloadHash := "UNSIGNED-PAYLOAD"

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		config.Method,
		path,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	)

	// Create string to sign
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	hashedCanonicalRequest := sha256Hash(canonicalRequest)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate,
		credentialScope,
		hashedCanonicalRequest,
	)

	// Calculate signature
	signature := h.calculateSignatureV4(stringToSign, config.SecretKey, dateStamp, region, service)

	// Build final URL
	queryParams.Set("X-Amz-Signature", signature)
	finalURL := fmt.Sprintf("%s%s?%s", h.publicAPIURL, path, queryParams.Encode())

	logrus.Debugf("Generated presigned URL valid until %s", expiresAt.Format(time.RFC3339))

	return finalURL, nil
}

// generatePresignedURLV2 generates AWS Signature V2 presigned URL (legacy)
func (h *Handler) generatePresignedURLV2(config PresignedURLConfig) (string, error) {
	expiresAt := time.Now().UTC().Add(config.Expiration)
	expires := expiresAt.Unix()

	// Build string to sign for V2
	path := fmt.Sprintf("/%s/%s", config.BucketName, config.ObjectKey)
	stringToSign := fmt.Sprintf("%s\n\n\n%d\n%s", config.Method, expires, path)

	// Calculate signature
	signature := h.calculateSignatureV2(stringToSign, config.SecretKey)

	// Build query parameters
	queryParams := url.Values{}
	queryParams.Set("AWSAccessKeyId", config.AccessKey)
	queryParams.Set("Expires", strconv.FormatInt(expires, 10))
	queryParams.Set("Signature", signature)

	// Build final URL
	finalURL := fmt.Sprintf("%s%s?%s", h.publicAPIURL, path, queryParams.Encode())

	return finalURL, nil
}

// ValidatePresignedURL validates a presigned URL request
func (h *Handler) ValidatePresignedURL(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()

	// Check if it's V4 or V2 presigned URL
	if query.Get("X-Amz-Algorithm") == "AWS4-HMAC-SHA256" {
		return h.validatePresignedURLV4(r)
	} else if query.Get("AWSAccessKeyId") != "" {
		return h.validatePresignedURLV2(r)
	}

	return fmt.Errorf("not a presigned URL request")
}

// validatePresignedURLV4 validates AWS Signature V4 presigned URL
func (h *Handler) validatePresignedURLV4(r *http.Request) error {
	query := r.URL.Query()

	// Extract query parameters
	algorithm := query.Get("X-Amz-Algorithm")
	credential := query.Get("X-Amz-Credential")
	date := query.Get("X-Amz-Date")
	expires := query.Get("X-Amz-Expires")
	signedHeaders := query.Get("X-Amz-SignedHeaders")
	providedSignature := query.Get("X-Amz-Signature")

	// Validate required parameters
	if algorithm != "AWS4-HMAC-SHA256" {
		return fmt.Errorf("invalid algorithm: %s", algorithm)
	}

	if credential == "" || date == "" || expires == "" || providedSignature == "" {
		return fmt.Errorf("missing required presigned URL parameters")
	}

	// Parse and validate expiration
	expiresIn, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid expires parameter: %v", err)
	}

	// Parse date
	requestTime, err := time.Parse("20060102T150405Z", date)
	if err != nil {
		return fmt.Errorf("invalid date format: %v", err)
	}

	// Check if URL has expired
	expirationTime := requestTime.Add(time.Duration(expiresIn) * time.Second)
	if time.Now().UTC().After(expirationTime) {
		return fmt.Errorf("presigned URL has expired")
	}

	// Extract access key and credential components
	credParts := strings.Split(credential, "/")
	if len(credParts) < 5 {
		return fmt.Errorf("invalid credential format")
	}
	accessKey := credParts[0]
	dateStamp := credParts[1]
	region := credParts[2]
	service := credParts[3]

	// Get the secret key for this access key
	secretKey, err := h.getSecretKeyForAccessKey(r.Context(), accessKey)
	if err != nil {
		return fmt.Errorf("invalid access key: %v", err)
	}

	// Reconstruct the canonical request
	// Extract host from request
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}

	// Build canonical query string (without signature)
	canonicalQuery := url.Values{}
	for k, v := range query {
		if k != "X-Amz-Signature" {
			canonicalQuery[k] = v
		}
	}
	canonicalQueryString := canonicalQuery.Encode()

	// Build canonical headers
	canonicalHeaders := fmt.Sprintf("host:%s\n", host)

	// Build canonical request
	payloadHash := "UNSIGNED-PAYLOAD"
	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		r.Method,
		r.URL.Path,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	)

	// Create string to sign
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	hashedCanonicalRequest := sha256Hash(canonicalRequest)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		date,
		credentialScope,
		hashedCanonicalRequest,
	)

	// Calculate expected signature
	expectedSignature := h.calculateSignatureV4(stringToSign, secretKey, dateStamp, region, service)

	// Compare signatures using constant-time comparison
	if !hmac.Equal([]byte(expectedSignature), []byte(providedSignature)) {
		logrus.Warnf("Signature validation failed for access key: %s (expected: %s, got: %s)",
			accessKey, expectedSignature, providedSignature)
		return fmt.Errorf("signature does not match")
	}

	logrus.Debugf("Presigned URL V4 validation passed for access key: %s", accessKey)

	return nil
}

// validatePresignedURLV2 validates AWS Signature V2 presigned URL
func (h *Handler) validatePresignedURLV2(r *http.Request) error {
	query := r.URL.Query()

	// Extract query parameters
	accessKey := query.Get("AWSAccessKeyId")
	expires := query.Get("Expires")
	providedSignature := query.Get("Signature")

	// Validate required parameters
	if accessKey == "" || expires == "" || providedSignature == "" {
		return fmt.Errorf("missing required presigned URL parameters")
	}

	// Parse and validate expiration
	expiresAt, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid expires parameter: %v", err)
	}

	// Check if URL has expired
	if time.Now().UTC().Unix() > expiresAt {
		return fmt.Errorf("presigned URL has expired")
	}

	// Get the secret key for this access key
	secretKey, err := h.getSecretKeyForAccessKey(r.Context(), accessKey)
	if err != nil {
		return fmt.Errorf("invalid access key: %v", err)
	}

	// Reconstruct the string to sign for V2
	// V2 format: METHOD\n\n\nEXPIRES\nPATH
	stringToSign := fmt.Sprintf("%s\n\n\n%s\n%s", r.Method, expires, r.URL.Path)

	// Calculate expected signature
	expectedSignature := h.calculateSignatureV2(stringToSign, secretKey)

	// Compare signatures using constant-time comparison
	if !hmac.Equal([]byte(expectedSignature), []byte(providedSignature)) {
		logrus.Warnf("Signature V2 validation failed for access key: %s (expected: %s, got: %s)",
			accessKey, expectedSignature, providedSignature)
		return fmt.Errorf("signature does not match")
	}

	logrus.Debugf("Presigned URL V2 validation passed for access key: %s", accessKey)

	return nil
}

// HandlePresignedRequest handles presigned URL requests
func (h *Handler) HandlePresignedRequest(w http.ResponseWriter, r *http.Request) {
	// Validate the presigned URL
	if err := h.ValidatePresignedURL(w, r); err != nil {
		h.writeError(w, "InvalidRequest", err.Error(), r.URL.Path, r)
		return
	}

	// Extract bucket and object from URL
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["key"]

	// Route to appropriate handler based on method
	switch r.Method {
	case "GET":
		h.GetObject(w, r)
	case "PUT":
		h.PutObject(w, r)
	case "DELETE":
		h.DeleteObject(w, r)
	case "HEAD":
		h.HeadObject(w, r)
	default:
		h.writeError(w, "MethodNotAllowed", fmt.Sprintf("Method %s not allowed for presigned URLs", r.Method), r.URL.Path, r)
	}

	logrus.Debugf("Presigned request processed: %s %s/%s", r.Method, bucketName, objectKey)
}

// calculateSignatureV4 calculates AWS Signature V4
func (h *Handler) calculateSignatureV4(stringToSign, secretKey, dateStamp, region, service string) string {
	// AWS Signature V4 calculation
	kDate := hmacSHA256([]byte("AWS4"+secretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	signature := hmacSHA256(kSigning, []byte(stringToSign))

	return hex.EncodeToString(signature)
}

// calculateSignatureV2 calculates AWS Signature V2
func (h *Handler) calculateSignatureV2(stringToSign, secretKey string) string {
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(stringToSign))
	return hex.EncodeToString(mac.Sum(nil))
}

// Helper functions

// getSecretKeyForAccessKey retrieves the secret key for a given access key
func (h *Handler) getSecretKeyForAccessKey(ctx context.Context, accessKeyID string) (string, error) {
	// Get access key from auth manager
	accessKey, err := h.authManager.GetAccessKey(ctx, accessKeyID)
	if err != nil {
		return "", fmt.Errorf("access key not found: %w", err)
	}

	if accessKey.Status != "active" {
		return "", fmt.Errorf("access key is inactive")
	}

	return accessKey.SecretAccessKey, nil
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func sha256Hash(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// buildCanonicalQueryString builds canonical query string for signing
func buildCanonicalQueryString(query url.Values) string {
	// Sort query parameters
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build canonical query string
	var parts []string
	for _, k := range keys {
		for _, v := range query[k] {
			parts = append(parts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
		}
	}

	return strings.Join(parts, "&")
}

