package presigned

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// ValidatePresignedURL validates a presigned URL from an HTTP request
// Returns true if valid, false otherwise
func ValidatePresignedURL(r *http.Request, secretAccessKey string) (bool, error) {
	query := r.URL.Query()

	// Check if this is a presigned URL request
	if !IsPresignedURL(r) {
		return false, fmt.Errorf("not a presigned URL request")
	}

	// Extract parameters
	algorithm := query.Get("X-Amz-Algorithm")
	credential := query.Get("X-Amz-Credential")
	amzDate := query.Get("X-Amz-Date")
	expires := query.Get("X-Amz-Expires")
	signedHeaders := query.Get("X-Amz-SignedHeaders")
	providedSignature := query.Get("X-Amz-Signature")

	// Validate algorithm
	if algorithm != "AWS4-HMAC-SHA256" {
		return false, fmt.Errorf("invalid algorithm: %s", algorithm)
	}

	// Parse credential (format: accessKeyID/dateStamp/region/service/aws4_request)
	credParts := strings.Split(credential, "/")
	if len(credParts) != 5 {
		return false, fmt.Errorf("invalid credential format")
	}

	accessKeyID := credParts[0]
	dateStamp := credParts[1]
	region := credParts[2]
	svc := credParts[3]
	reqType := credParts[4]

	if svc != "s3" || reqType != "aws4_request" {
		return false, fmt.Errorf("invalid service or request type")
	}

	// Parse date
	requestTime, err := time.Parse("20060102T150405Z", amzDate)
	if err != nil {
		return false, fmt.Errorf("invalid X-Amz-Date format: %w", err)
	}

	// Parse expiration
	expiresIn, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid X-Amz-Expires: %w", err)
	}

	// Check if expired
	expirationTime := requestTime.Add(time.Duration(expiresIn) * time.Second)
	if time.Now().UTC().After(expirationTime) {
		logrus.WithFields(logrus.Fields{
			"requestTime":    requestTime,
			"expiresIn":      expiresIn,
			"expirationTime": expirationTime,
			"now":            time.Now().UTC(),
		}).Debug("Presigned URL has expired")
		return false, fmt.Errorf("presigned URL has expired")
	}

	// Build canonical query string (without signature)
	canonicalQuery := buildCanonicalQueryStringForValidation(query)

	// Build credential scope
	credentialScope := fmt.Sprintf("%s/%s/%s/%s", dateStamp, region, svc, reqType)

	// Build canonical request
	canonicalHeaders := fmt.Sprintf("host:%s\n", r.Host)
	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\nUNSIGNED-PAYLOAD",
		r.Method,
		r.URL.Path,
		canonicalQuery,
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

	// Calculate expected signature
	signingKey := getSignatureKey(secretAccessKey, dateStamp, region, svc)
	expectedSignature := hmacSHA256(signingKey, []byte(stringToSign))
	expectedSignatureHex := strings.ToLower(fmt.Sprintf("%x", expectedSignature))

	// Compare signatures
	if strings.ToLower(providedSignature) != expectedSignatureHex {
		logrus.WithFields(logrus.Fields{
			"accessKeyID":       accessKeyID,
			"providedSignature": providedSignature,
			"expectedSignature": expectedSignatureHex,
		}).Debug("Signature mismatch")
		return false, fmt.Errorf("signature does not match")
	}

	logrus.WithFields(logrus.Fields{
		"accessKeyID": accessKeyID,
		"method":      r.Method,
		"path":        r.URL.Path,
		"expiresIn":   expiresIn,
	}).Info("Presigned URL validated successfully")

	return true, nil
}

// IsPresignedURL checks if a request contains presigned URL parameters
func IsPresignedURL(r *http.Request) bool {
	query := r.URL.Query()
	return query.Get("X-Amz-Algorithm") != "" &&
		query.Get("X-Amz-Credential") != "" &&
		query.Get("X-Amz-Date") != "" &&
		query.Get("X-Amz-Expires") != "" &&
		query.Get("X-Amz-Signature") != ""
}

// ExtractAccessKeyID extracts the access key ID from a presigned URL
func ExtractAccessKeyID(r *http.Request) string {
	credential := r.URL.Query().Get("X-Amz-Credential")
	if credential == "" {
		return ""
	}

	parts := strings.Split(credential, "/")
	if len(parts) > 0 {
		return parts[0]
	}

	return ""
}

// buildCanonicalQueryStringForValidation builds canonical query string excluding signature
func buildCanonicalQueryStringForValidation(query url.Values) string {
	// Get all parameters except signature
	params := make(map[string]string)
	for k, v := range query {
		if k != "X-Amz-Signature" && len(v) > 0 {
			params[k] = v[0]
		}
	}

	return buildCanonicalQueryString(params)
}
