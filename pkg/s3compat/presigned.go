package s3compat

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
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
	canonicalQueryString := canonicalQueryStringV4(queryParams, true)
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
	finalURL := fmt.Sprintf("%s%s?%s", h.publicAPIURL, path, canonicalQueryStringV4(queryParams, false))

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
	canonicalQueryString := canonicalQueryStringV4(canonicalQuery, false)

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

// canonicalQueryStringV4 builds an AWS SigV4 canonical query string for url.Values.
// - Sorts by key, then by value.
// - Uses RFC3986 encoding (spaces as %20, not '+').
// - If excludeSignature is true, removes X-Amz-Signature.
func canonicalQueryStringV4(values url.Values, excludeSignature bool) string {
	type pair struct{ k, v string }
	pairs := make([]pair, 0, len(values))
	for k, vals := range values {
		if excludeSignature && k == "X-Amz-Signature" {
			continue
		}
		if len(vals) == 0 {
			pairs = append(pairs, pair{k: k, v: ""})
			continue
		}
		for _, v := range vals {
			pairs = append(pairs, pair{k: k, v: v})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k == pairs[j].k {
			return pairs[i].v < pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})

	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, awsQueryEscapeV4(p.k)+"="+awsQueryEscapeV4(p.v))
	}
	return strings.Join(out, "&")
}

func awsQueryEscapeV4(s string) string {
	escaped := url.QueryEscape(s)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
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

	// Resolve the user associated with the presigned URL access key and inject into context.
	// This ensures PutObject/DeleteObject permission checks see an authenticated user,
	// since those handlers rely on auth.GetUserFromContext() for access control.
	query := r.URL.Query()
	accessKeyID := ""
	if cred := query.Get("X-Amz-Credential"); cred != "" {
		// V4: credential=<ACCESS_KEY>/<date>/<region>/<service>/aws4_request
		if idx := strings.Index(cred, "/"); idx > 0 {
			accessKeyID = cred[:idx]
		}
	} else if query.Get("AWSAccessKeyId") != "" {
		// V2: AWSAccessKeyId=<ACCESS_KEY>&Signature=...
		accessKeyID = query.Get("AWSAccessKeyId")
	}

	if accessKeyID != "" && h.authManager != nil {
		if accessKey, err := h.authManager.GetAccessKey(r.Context(), accessKeyID); err == nil {
			if user, err := h.authManager.GetUser(r.Context(), accessKey.UserID); err == nil {
				enrichedCtx := context.WithValue(r.Context(), "user", user)
				r = r.WithContext(enrichedCtx)
			}
		}
	}

	// Extract bucket and object from URL
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

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

// postPolicy represents the JSON policy document for POST presigned uploads.
type postPolicy struct {
	Expiration string            `json:"expiration"`
	Conditions []json.RawMessage `json:"conditions"`
}

// HandlePresignedPost handles browser-based HTML form uploads to a bucket.
// It validates the POST policy signature and stores the uploaded object.
// Compatible with both AWS Signature V4 (x-amz-*) and V2 (AWSAccessKeyId) forms.
func (h *Handler) HandlePresignedPost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Parse multipart form — 32 MB in memory, rest on disk.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		h.writeError(w, "MalformedPOSTRequest", "Unable to parse multipart form: "+err.Error(), bucketName, r)
		return
	}

	form := r.MultipartForm
	field := func(name string) string {
		if vs := form.Value[name]; len(vs) > 0 {
			return vs[0]
		}
		return ""
	}

	// Determine signature version and extract fields.
	isV4 := field("x-amz-algorithm") == "AWS4-HMAC-SHA256"
	isV2 := !isV4 && field("AWSAccessKeyId") != ""
	if !isV4 && !isV2 {
		h.writeError(w, "InvalidRequest", "Missing signature fields (x-amz-algorithm or AWSAccessKeyId)", bucketName, r)
		return
	}

	policyB64 := field("policy")
	if policyB64 == "" {
		h.writeError(w, "InvalidPolicyDocument", "Missing policy field", bucketName, r)
		return
	}

	// Decode and parse policy.
	policyJSON, err := base64.StdEncoding.DecodeString(policyB64)
	if err != nil {
		h.writeError(w, "InvalidPolicyDocument", "policy is not valid base64", bucketName, r)
		return
	}
	var policy postPolicy
	if err := json.Unmarshal(policyJSON, &policy); err != nil {
		h.writeError(w, "InvalidPolicyDocument", "policy is not valid JSON: "+err.Error(), bucketName, r)
		return
	}

	// Check policy expiration.
	expiry, err := time.Parse(time.RFC3339, policy.Expiration)
	if err != nil {
		expiry, err = time.Parse("2006-01-02T15:04:05.000Z", policy.Expiration)
	}
	if err != nil || time.Now().UTC().After(expiry) {
		h.writeError(w, "AccessDenied", "Policy has expired", bucketName, r)
		return
	}

	// Verify signature.
	var accessKey, secretKey string
	if isV4 {
		credential := field("x-amz-credential")
		if credential == "" {
			h.writeError(w, "InvalidRequest", "Missing x-amz-credential", bucketName, r)
			return
		}
		parts := strings.Split(credential, "/")
		if len(parts) < 5 {
			h.writeError(w, "InvalidRequest", "Malformed x-amz-credential", bucketName, r)
			return
		}
		accessKey = parts[0]
		dateStamp := parts[1]
		region := parts[2]
		service := parts[3]

		secretKey, err = h.getSecretKeyForAccessKey(r.Context(), accessKey)
		if err != nil {
			h.writeError(w, "InvalidAccessKeyId", "The access key does not exist", bucketName, r)
			return
		}

		expectedSig := h.calculateSignatureV4(policyB64, secretKey, dateStamp, region, service)
		if !hmac.Equal([]byte(expectedSig), []byte(field("x-amz-signature"))) {
			h.writeError(w, "SignatureDoesNotMatch", "The request signature does not match", bucketName, r)
			return
		}
	} else {
		// V2: HMAC-SHA256(secret, base64(policy))
		accessKey = field("AWSAccessKeyId")
		secretKey, err = h.getSecretKeyForAccessKey(r.Context(), accessKey)
		if err != nil {
			h.writeError(w, "InvalidAccessKeyId", "The access key does not exist", bucketName, r)
			return
		}
		expectedSig := h.calculateSignatureV2(policyB64, secretKey)
		if !hmac.Equal([]byte(expectedSig), []byte(field("signature"))) {
			h.writeError(w, "SignatureDoesNotMatch", "The request signature does not match", bucketName, r)
			return
		}
	}

	// Resolve the user from the access key and inject into context for permission checks.
	if h.authManager != nil {
		if ak, err := h.authManager.GetAccessKey(r.Context(), accessKey); err == nil {
			if user, err := h.authManager.GetUser(r.Context(), ak.UserID); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), "user", user))
			}
		}
	}

	// Validate policy conditions against the form fields.
	objectKey := field("key")
	contentType := field("Content-Type")
	var minLen, maxLen int64 = 0, 1<<63 - 1

	for _, raw := range policy.Conditions {
		// Try object form first: {"field": "value"}
		var objCond map[string]interface{}
		if json.Unmarshal(raw, &objCond) == nil {
			for k, v := range objCond {
				val, _ := v.(string)
				switch strings.ToLower(k) {
				case "bucket":
					if val != bucketName {
						h.writeError(w, "AccessDenied", "Bucket condition does not match", bucketName, r)
						return
					}
				case "key":
					if val != objectKey {
						h.writeError(w, "AccessDenied", "Key condition does not match", bucketName, r)
						return
					}
				case "content-type":
					if val != contentType {
						h.writeError(w, "AccessDenied", "Content-Type condition does not match", bucketName, r)
						return
					}
				// x-amz-* fields are informational — already validated via signature.
				}
			}
			continue
		}

		// Try array form: ["starts-with", "$field", "prefix"] or ["content-length-range", min, max]
		var arrCond []interface{}
		if json.Unmarshal(raw, &arrCond) != nil || len(arrCond) == 0 {
			continue
		}
		op, _ := arrCond[0].(string)
		switch strings.ToLower(op) {
		case "starts-with":
			if len(arrCond) < 3 {
				continue
			}
			fname, _ := arrCond[1].(string)
			prefix, _ := arrCond[2].(string)
			fname = strings.TrimPrefix(fname, "$")
			var formVal string
			switch strings.ToLower(fname) {
			case "key":
				formVal = objectKey
			case "content-type":
				formVal = contentType
			default:
				formVal = field(fname)
			}
			// Empty prefix means any value is allowed.
			if prefix != "" && !strings.HasPrefix(formVal, prefix) {
				h.writeError(w, "AccessDenied", fmt.Sprintf("Field %s does not start with required prefix", fname), bucketName, r)
				return
			}
		case "content-length-range":
			if len(arrCond) < 3 {
				continue
			}
			minLen = int64(toFloat64(arrCond[1]))
			maxLen = int64(toFloat64(arrCond[2]))
		}
	}

	// Get the uploaded file — the file field must be named "file" or "content".
	var fileHeader *multipart.FileHeader
	for _, name := range []string{"file", "content"} {
		if fhs := form.File[name]; len(fhs) > 0 {
			fileHeader = fhs[0]
			break
		}
	}
	if fileHeader == nil {
		h.writeError(w, "MalformedPOSTRequest", "Missing file field in multipart form", bucketName, r)
		return
	}

	// Validate content length range.
	fileSize := fileHeader.Size
	if fileSize < minLen || fileSize > maxLen {
		h.writeError(w, "EntityTooSmall", fmt.Sprintf("File size %d is outside the allowed range [%d, %d]", fileSize, minLen, maxLen), bucketName, r)
		return
	}

	src, err := fileHeader.Open()
	if err != nil {
		h.writeError(w, "InternalError", "Failed to open uploaded file: "+err.Error(), bucketName, r)
		return
	}
	defer src.Close()

	// Build synthetic headers for PutObject (mirrors what an actual PUT request would carry).
	syntheticHeaders := http.Header{}
	if contentType != "" {
		syntheticHeaders.Set("Content-Type", contentType)
	}
	syntheticHeaders.Set("Content-Length", strconv.FormatInt(fileSize, 10))
	// Forward any x-amz-meta-* fields from the form.
	for k, vs := range form.Value {
		if strings.HasPrefix(strings.ToLower(k), "x-amz-meta-") && len(vs) > 0 {
			syntheticHeaders.Set(k, vs[0])
		}
	}

	// Determine bucket path including tenant.
	bucketPath := h.getBucketPath(r, bucketName)

	result, err := h.objectManager.PutObject(r.Context(), bucketPath, objectKey, src, syntheticHeaders)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucketName, "key": objectKey}).Error("POST presigned: PutObject failed")
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	// Check for success_action_redirect.
	if redirect := field("success_action_redirect"); redirect != "" {
		if !strings.HasPrefix(redirect, "http://") && !strings.HasPrefix(redirect, "https://") {
			h.writeError(w, "InvalidArgument", "success_action_redirect must be an http or https URL", bucketName, r)
			return
		}
		sep := "?"
		if strings.Contains(redirect, "?") {
			sep = "&"
		}
		http.Redirect(w, r, fmt.Sprintf("%s%sbucket=%s&key=%s&etag=%s",
			redirect, sep, url.QueryEscape(bucketName), url.QueryEscape(objectKey), url.QueryEscape(result.ETag)), http.StatusSeeOther)
		return
	}

	// success_action_status controls the response code (200, 201, or default 204).
	statusCode := http.StatusNoContent
	if sas := field("success_action_status"); sas != "" {
		switch sas {
		case "200":
			statusCode = http.StatusOK
		case "201":
			statusCode = http.StatusCreated
		}
	}

	if statusCode == http.StatusCreated {
		// Return XML body per S3 spec for 201.
		type postResponse struct {
			Location string `xml:"Location"`
			Bucket   string `xml:"Bucket"`
			Key      string `xml:"Key"`
			ETag     string `xml:"ETag"`
		}
		w.Header().Set("ETag", result.ETag)
		h.writeXMLResponse(w, http.StatusCreated, postResponse{
			Location: fmt.Sprintf("%s/%s/%s", h.publicAPIURL, bucketName, objectKey),
			Bucket:   bucketName,
			Key:      objectKey,
			ETag:     result.ETag,
		})
		return
	}

	w.Header().Set("ETag", result.ETag)
	w.WriteHeader(statusCode)
}

// toFloat64 converts json.Number / float64 / int values to float64.
func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
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
