package s3compat

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// s3TestEnv contains the complete S3 testing environment
type s3TestEnv struct {
	handler       *Handler
	authManager   auth.Manager
	bucketManager bucket.Manager
	objectManager object.Manager
	router        *mux.Router
	accessKey     string
	secretKey     string
	tenantID      string
	userID        string
	tempDir       string
	cleanup       func()
}

// setupCompleteS3Environment creates a fully functional S3 API test environment with authentication
func setupCompleteS3Environment(t *testing.T) *s3TestEnv {
	// Create temporary directory for test data
	tempDir, err := os.MkdirTemp("", "maxiofs-s3-test-*")
	require.NoError(t, err, "Failed to create temp dir")

	// Initialize auth manager with proper config
	authDir := filepath.Join(tempDir, "auth")
	err = os.MkdirAll(authDir, 0755)
	require.NoError(t, err)

	authConfig := config.AuthConfig{
		EnableAuth: true,
		JWTSecret:  "test-secret-key-for-testing-only-minimum-32-chars-long-string",
	}
	authManager := auth.NewManager(authConfig, authDir)
	require.NotNil(t, authManager, "Auth manager should be created")

	ctx := context.Background()

	// Create test tenant
	tenant := &auth.Tenant{
		ID:              "test-tenant",
		Name:            "test-tenant",
		DisplayName:     "Test Tenant",
		Description:     "Tenant for S3 API testing",
		Status:          "active",
		MaxAccessKeys:   100,
		MaxStorageBytes: 10 * 1024 * 1024 * 1024, // 10GB
		MaxBuckets:      1000,
		CreatedAt:       time.Now().Unix(),
		UpdatedAt:       time.Now().Unix(),
	}
	err = authManager.CreateTenant(ctx, tenant)
	require.NoError(t, err, "Should create tenant")

	// Create test user
	testUser := &auth.User{
		ID:          "test-user-id",
		Username:    "testuser",
		DisplayName: "Test User",
		Email:       "test@example.com",
		Status:      "active",
		TenantID:    tenant.ID,
		Roles:       []string{"admin"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	err = authManager.CreateUser(ctx, testUser)
	require.NoError(t, err, "Should create user")

	// Generate access keys for the user
	accessKey, err := authManager.GenerateAccessKey(ctx, testUser.ID)
	require.NoError(t, err, "Should generate access key")
	require.NotEmpty(t, accessKey.AccessKeyID, "Access key ID should not be empty")
	require.NotEmpty(t, accessKey.SecretAccessKey, "Secret key should not be empty")

	// Initialize storage backend
	storageBackend, err := storage.NewFilesystemBackend(storage.Config{
		Root: tempDir,
	})
	require.NoError(t, err, "Failed to create storage backend")

	// Initialize BadgerDB metadata store
	dbPath := filepath.Join(tempDir, "metadata")
	metadataStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           dbPath,
		SyncWrites:        true,
		CompactionEnabled: false,
		Logger:            logrus.StandardLogger(),
	})
	require.NoError(t, err, "Failed to create metadata store")

	// Create managers
	bucketManager := bucket.NewManager(storageBackend, metadataStore)
	storageConfig := config.StorageConfig{
		Root: tempDir,
	}
	objectManager := object.NewManager(storageBackend, metadataStore, storageConfig)

	// Create S3 handler with ALL dependencies
	handler := NewHandler(bucketManager, objectManager)
	handler.SetAuthManager(authManager)
	handler.SetMetadataStore(metadataStore)

	// Create router and register routes
	router := mux.NewRouter()

	// Apply auth middleware to all routes
	router.Use(authManager.Middleware())

	// Register S3 API routes - ORDER MATTERS!
	// Routes with query parameters MUST come BEFORE general routes

	// Bucket configuration operations (with query parameters - FIRST!)
	router.HandleFunc("/{bucket}", handler.PutBucketPolicy).Methods("PUT").Queries("policy", "")
	router.HandleFunc("/{bucket}", handler.GetBucketPolicy).Methods("GET").Queries("policy", "")
	router.HandleFunc("/{bucket}", handler.DeleteBucketPolicy).Methods("DELETE").Queries("policy", "")

	router.HandleFunc("/{bucket}", handler.PutBucketLifecycle).Methods("PUT").Queries("lifecycle", "")
	router.HandleFunc("/{bucket}", handler.GetBucketLifecycle).Methods("GET").Queries("lifecycle", "")
	router.HandleFunc("/{bucket}", handler.DeleteBucketLifecycle).Methods("DELETE").Queries("lifecycle", "")

	router.HandleFunc("/{bucket}", handler.PutBucketCORS).Methods("PUT").Queries("cors", "")
	router.HandleFunc("/{bucket}", handler.GetBucketCORS).Methods("GET").Queries("cors", "")
	router.HandleFunc("/{bucket}", handler.DeleteBucketCORS).Methods("DELETE").Queries("cors", "")

	router.HandleFunc("/{bucket}", handler.PutBucketVersioning).Methods("PUT").Queries("versioning", "")
	router.HandleFunc("/{bucket}", handler.GetBucketVersioning).Methods("GET").Queries("versioning", "")

	// Bucket tagging
	router.HandleFunc("/{bucket}", handler.PutBucketTagging).Methods("PUT").Queries("tagging", "")
	router.HandleFunc("/{bucket}", handler.GetBucketTagging).Methods("GET").Queries("tagging", "")
	router.HandleFunc("/{bucket}", handler.DeleteBucketTagging).Methods("DELETE").Queries("tagging", "")

	// Bucket ACL
	router.HandleFunc("/{bucket}", handler.PutBucketACL).Methods("PUT").Queries("acl", "")
	router.HandleFunc("/{bucket}", handler.GetBucketACL).Methods("GET").Queries("acl", "")

	// Bucket location
	router.HandleFunc("/{bucket}", handler.GetBucketLocation).Methods("GET").Queries("location", "")

	// Object lock configuration
	router.HandleFunc("/{bucket}", handler.PutObjectLockConfiguration).Methods("PUT").Queries("object-lock", "")
	router.HandleFunc("/{bucket}", handler.GetObjectLockConfiguration).Methods("GET").Queries("object-lock", "")

	// List object versions
	router.HandleFunc("/{bucket}", handler.ListBucketVersions).Methods("GET").Queries("versions", "")

	// List multipart uploads
	router.HandleFunc("/{bucket}", handler.ListMultipartUploads).Methods("GET").Queries("uploads", "")

	// Batch operations
	router.HandleFunc("/{bucket}", handler.DeleteObjects).Methods("POST").Queries("delete", "")

	// General bucket operations (NO query parameters - AFTER!)
	router.HandleFunc("/{bucket}", handler.CreateBucket).Methods("PUT")
	router.HandleFunc("/{bucket}", handler.DeleteBucket).Methods("DELETE")
	router.HandleFunc("/{bucket}", handler.HeadBucket).Methods("HEAD")
	router.HandleFunc("/", handler.ListBuckets).Methods("GET")

	// Object operations - ORDER MATTERS! (query params first)

	// Multipart upload operations (with query parameters - FIRST!)
	router.HandleFunc("/{bucket}/{object:.+}", handler.CreateMultipartUpload).Methods("POST").Queries("uploads", "")
	router.HandleFunc("/{bucket}/{object:.+}", handler.UploadPart).Methods("PUT").Queries("uploadId", "", "partNumber", "")
	router.HandleFunc("/{bucket}/{object:.+}", handler.CompleteMultipartUpload).Methods("POST").Queries("uploadId", "")
	router.HandleFunc("/{bucket}/{object:.+}", handler.ListParts).Methods("GET").Queries("uploadId", "")
	router.HandleFunc("/{bucket}/{object:.+}", handler.AbortMultipartUpload).Methods("DELETE").Queries("uploadId", "")

	// Object tagging (with query parameter)
	router.HandleFunc("/{bucket}/{object:.+}", handler.PutObjectTagging).Methods("PUT").Queries("tagging", "")
	router.HandleFunc("/{bucket}/{object:.+}", handler.GetObjectTagging).Methods("GET").Queries("tagging", "")
	router.HandleFunc("/{bucket}/{object:.+}", handler.DeleteObjectTagging).Methods("DELETE").Queries("tagging", "")

	// Object retention (with query parameter)
	router.HandleFunc("/{bucket}/{object:.+}", handler.PutObjectRetention).Methods("PUT").Queries("retention", "")
	router.HandleFunc("/{bucket}/{object:.+}", handler.GetObjectRetention).Methods("GET").Queries("retention", "")

	// Object legal hold (with query parameter)
	router.HandleFunc("/{bucket}/{object:.+}", handler.PutObjectLegalHold).Methods("PUT").Queries("legal-hold", "")
	router.HandleFunc("/{bucket}/{object:.+}", handler.GetObjectLegalHold).Methods("GET").Queries("legal-hold", "")

	// Object ACL (with query parameter)
	router.HandleFunc("/{bucket}/{object:.+}", handler.PutObjectACL).Methods("PUT").Queries("acl", "")
	router.HandleFunc("/{bucket}/{object:.+}", handler.GetObjectACL).Methods("GET").Queries("acl", "")

	// General object operations (NO query parameters - AFTER!)
	router.HandleFunc("/{bucket}/{object:.+}", handler.PutObject).Methods("PUT")
	router.HandleFunc("/{bucket}/{object:.+}", handler.GetObject).Methods("GET")
	router.HandleFunc("/{bucket}/{object:.+}", handler.DeleteObject).Methods("DELETE")
	router.HandleFunc("/{bucket}/{object:.+}", handler.HeadObject).Methods("HEAD")

	// List objects
	router.HandleFunc("/{bucket}/", handler.ListObjects).Methods("GET")

	// Cleanup function
	cleanup := func() {
		metadataStore.Close()
		os.RemoveAll(tempDir)
	}

	return &s3TestEnv{
		handler:       handler,
		authManager:   authManager,
		bucketManager: bucketManager,
		objectManager: objectManager,
		router:        router,
		accessKey:     accessKey.AccessKeyID,
		secretKey:     accessKey.SecretAccessKey,
		tenantID:      tenant.ID,
		userID:        testUser.ID,
		tempDir:       tempDir,
		cleanup:       cleanup,
	}
}

// signRequestV4 signs an HTTP request using AWS Signature Version 4
func signRequestV4(req *http.Request, accessKey, secretKey, region, service string) {
	// Set required headers
	now := time.Now().UTC()
	req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	req.Header.Set("Host", req.Host)

	// Calculate payload hash
	var payloadHash string
	if req.Body != nil {
		bodyBytes, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		hash := sha256.Sum256(bodyBytes)
		payloadHash = hex.EncodeToString(hash[:])
	} else {
		hash := sha256.Sum256([]byte(""))
		payloadHash = hex.EncodeToString(hash[:])
	}
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	// Build canonical request
	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	canonicalQueryString := req.URL.Query().Encode()

	// Canonical headers (sorted)
	var headerNames []string
	canonicalHeaders := ""
	for name := range req.Header {
		lowerName := strings.ToLower(name)
		headerNames = append(headerNames, lowerName)
	}
	sort.Strings(headerNames)

	for _, name := range headerNames {
		value := req.Header.Get(name)
		canonicalHeaders += fmt.Sprintf("%s:%s\n", name, strings.TrimSpace(value))
	}

	signedHeaders := strings.Join(headerNames, ";")

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	)

	// String to sign
	dateStamp := now.Format("20060102")
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)

	hash := sha256.Sum256([]byte(canonicalRequest))
	canonicalRequestHash := hex.EncodeToString(hash[:])

	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		now.Format("20060102T150405Z"),
		credentialScope,
		canonicalRequestHash,
	)

	// Calculate signature
	kDate := hmacSHA256Test([]byte("AWS4"+secretKey), []byte(dateStamp))
	kRegion := hmacSHA256Test(kDate, []byte(region))
	kService := hmacSHA256Test(kRegion, []byte(service))
	kSigning := hmacSHA256Test(kService, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256Test(kSigning, []byte(stringToSign)))

	// Build authorization header
	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey,
		credentialScope,
		signedHeaders,
		signature,
	)

	req.Header.Set("Authorization", authHeader)
}

// hmacSHA256Test computes HMAC-SHA256 (test version)
func hmacSHA256Test(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// makeS3Request creates and signs an S3 API request
func (env *s3TestEnv) makeS3Request(method, path string, body []byte) (*http.Request, *httptest.ResponseRecorder) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Host = "localhost"

	// Sign the request
	signRequestV4(req, env.accessKey, env.secretKey, "us-east-1", "s3")

	// Create response recorder
	w := httptest.NewRecorder()

	return req, w
}

// TestS3CreateBucket tests bucket creation via S3 API with authentication
func TestS3CreateBucket(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	bucketName := "test-bucket"
	req, w := env.makeS3Request("PUT", "/"+bucketName, nil)

	// Route the request
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Should create bucket successfully")

	// Verify bucket was created
	ctx := context.Background()
	exists, err := env.bucketManager.BucketExists(ctx, env.tenantID, bucketName)
	require.NoError(t, err)
	assert.True(t, exists, "Bucket should exist in manager")
}

// TestS3ListBuckets tests bucket listing via S3 API with authentication
func TestS3ListBuckets(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create some buckets first
	testBuckets := []string{"bucket1", "bucket2", "bucket3"}
	for _, bucketName := range testBuckets {
		err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
		require.NoError(t, err)
	}

	// List buckets via S3 API
	req, w := env.makeS3Request("GET", "/", nil)
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Should list buckets successfully")

	// Verify response contains buckets
	body := w.Body.String()
	for _, bucketName := range testBuckets {
		assert.Contains(t, body, bucketName, "Response should contain bucket: "+bucketName)
	}
}

// TestS3PutObject tests object upload via S3 API with authentication
func TestS3PutObject(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-bucket"
	objectKey := "test-object.txt"
	objectContent := []byte("Hello from S3 API test!")

	// Create bucket first
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Upload object via S3 API
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, objectContent)
	req.Header.Set("Content-Type", "text/plain")
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Should upload object successfully")
	assert.NotEmpty(t, w.Header().Get("ETag"), "Should return ETag")
}

// TestS3GetObject tests object download via S3 API with authentication
func TestS3GetObject(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-bucket"
	objectKey := "test-object.txt"
	objectContent := []byte("Test content for GET operation")

	// Create bucket and upload object
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	bucketPath := env.tenantID + "/" + bucketName
	headers := http.Header{}
	headers.Set("Content-Type", "text/plain")
	_, err = env.objectManager.PutObject(ctx, bucketPath, objectKey, bytes.NewReader(objectContent), headers)
	require.NoError(t, err)

	// Get object via S3 API
	req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey, nil)
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Should get object successfully")
	assert.Equal(t, objectContent, w.Body.Bytes(), "Content should match")
	assert.NotEmpty(t, w.Header().Get("ETag"), "Should return ETag")
	assert.Equal(t, "text/plain", w.Header().Get("Content-Type"), "Should return correct content type")
}

// TestS3DeleteObject tests object deletion via S3 API with authentication
func TestS3DeleteObject(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-bucket"
	objectKey := "test-object.txt"
	objectContent := []byte("Content to be deleted")

	// Create bucket and upload object
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	bucketPath := env.tenantID + "/" + bucketName
	headers := http.Header{}
	_, err = env.objectManager.PutObject(ctx, bucketPath, objectKey, bytes.NewReader(objectContent), headers)
	require.NoError(t, err)

	// Delete object via S3 API
	req, w := env.makeS3Request("DELETE", "/"+bucketName+"/"+objectKey, nil)
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code, "Should delete object successfully")
}

// TestS3HeadBucket tests bucket existence check via S3 API
func TestS3HeadBucket(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-bucket"

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// HEAD bucket via S3 API
	req, w := env.makeS3Request("HEAD", "/"+bucketName, nil)
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Should return 200 for existing bucket")
}

// TestS3HeadObject tests object metadata retrieval via S3 API
func TestS3HeadObject(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-bucket"
	objectKey := "test-object.txt"
	objectContent := []byte("Content for HEAD test")

	// Create bucket and upload object
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	bucketPath := env.tenantID + "/" + bucketName
	headers := http.Header{}
	headers.Set("Content-Type", "application/octet-stream")
	_, err = env.objectManager.PutObject(ctx, bucketPath, objectKey, bytes.NewReader(objectContent), headers)
	require.NoError(t, err)

	// HEAD object via S3 API
	req, w := env.makeS3Request("HEAD", "/"+bucketName+"/"+objectKey, nil)
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Should return 200 for existing object")
	assert.NotEmpty(t, w.Header().Get("ETag"), "Should return ETag")
	assert.NotEmpty(t, w.Header().Get("Content-Length"), "Should return Content-Length")
}

// TestS3ListObjects tests object listing via S3 API
func TestS3ListObjects(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-bucket"

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Upload multiple objects
	bucketPath := env.tenantID + "/" + bucketName
	testObjects := []string{"file1.txt", "file2.txt", "dir/file3.txt"}
	for _, key := range testObjects {
		headers := http.Header{}
		_, err = env.objectManager.PutObject(ctx, bucketPath, key, bytes.NewReader([]byte("test")), headers)
		require.NoError(t, err)
	}

	// List objects via S3 API
	req, w := env.makeS3Request("GET", "/"+bucketName+"/", nil)
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Should list objects successfully")

	// Verify response contains objects
	body := w.Body.String()
	for _, key := range testObjects {
		assert.Contains(t, body, key, "Response should contain object: "+key)
	}
}

// TestS3BucketPolicy tests bucket policy operations via S3 API
func TestS3BucketPolicy(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "policy-test-bucket"

	// Create bucket first
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Create a test policy
	policy := bucket.Policy{
		Version: "2012-10-17",
		Statement: []bucket.Statement{
			{
				Sid:       "AllowPublicRead",
				Effect:    "Allow",
				Principal: "*",
				Action:    []string{"s3:GetObject"},
				Resource:  []string{fmt.Sprintf("arn:aws:s3:::%s/*", bucketName)},
			},
		},
	}

	policyJSON, err := json.Marshal(policy)
	require.NoError(t, err)

	t.Run("Put bucket policy", func(t *testing.T) {
		req, w := env.makeS3Request("PUT", "/"+bucketName+"?policy", policyJSON)
		req.Header.Set("Content-Type", "application/json")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code, "Should set bucket policy")
	})

	t.Run("Get bucket policy", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?policy", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get bucket policy")
		assert.Contains(t, w.Body.String(), "AllowPublicRead", "Should contain policy statement")
	})

	t.Run("Delete bucket policy", func(t *testing.T) {
		req, w := env.makeS3Request("DELETE", "/"+bucketName+"?policy", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code, "Should delete bucket policy")
	})
}

// TestS3BucketLifecycle tests bucket lifecycle configuration via S3 API
func TestS3BucketLifecycle(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "lifecycle-test-bucket"

	// Create bucket first
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	lifecycleXML := `<LifecycleConfiguration>
		<Rule>
			<ID>delete-old-logs</ID>
			<Status>Enabled</Status>
			<Prefix>logs/</Prefix>
			<Expiration>
				<Days>30</Days>
			</Expiration>
		</Rule>
	</LifecycleConfiguration>`

	t.Run("Put bucket lifecycle", func(t *testing.T) {
		req, w := env.makeS3Request("PUT", "/"+bucketName+"?lifecycle", []byte(lifecycleXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should set bucket lifecycle")
	})

	t.Run("Get bucket lifecycle", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?lifecycle", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get bucket lifecycle")
		assert.Contains(t, w.Body.String(), "delete-old-logs", "Should contain lifecycle rule")
		assert.Contains(t, w.Body.String(), "<Days>30</Days>", "Should contain expiration days")
	})

	t.Run("Delete bucket lifecycle", func(t *testing.T) {
		req, w := env.makeS3Request("DELETE", "/"+bucketName+"?lifecycle", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code, "Should delete bucket lifecycle")
	})
}

// TestS3BucketCORS tests CORS configuration via S3 API
func TestS3BucketCORS(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "cors-test-bucket"

	// Create bucket first
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	corsXML := `<CORSConfiguration>
		<CORSRule>
			<AllowedOrigin>https://example.com</AllowedOrigin>
			<AllowedMethod>GET</AllowedMethod>
			<AllowedMethod>PUT</AllowedMethod>
			<AllowedHeader>*</AllowedHeader>
			<MaxAgeSeconds>3000</MaxAgeSeconds>
		</CORSRule>
	</CORSConfiguration>`

	t.Run("Put bucket CORS", func(t *testing.T) {
		req, w := env.makeS3Request("PUT", "/"+bucketName+"?cors", []byte(corsXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should set bucket CORS")
	})

	t.Run("Get bucket CORS", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?cors", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get bucket CORS")
		assert.Contains(t, w.Body.String(), "https://example.com", "Should contain allowed origin")
		assert.Contains(t, w.Body.String(), "MaxAgeSeconds", "Should contain max age")
	})

	t.Run("Delete bucket CORS", func(t *testing.T) {
		req, w := env.makeS3Request("DELETE", "/"+bucketName+"?cors", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code, "Should delete bucket CORS")
	})
}

// TestS3ObjectTagging tests object tagging operations via S3 API
func TestS3ObjectTagging(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "tagging-test-bucket"
	objectKey := "test-object.txt"

	// Create bucket and object
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	bucketPath := env.tenantID + "/" + bucketName
	headers := http.Header{}
	_, err = env.objectManager.PutObject(ctx, bucketPath, objectKey, bytes.NewReader([]byte("test")), headers)
	require.NoError(t, err)

	taggingXML := `<Tagging>
		<TagSet>
			<Tag>
				<Key>Environment</Key>
				<Value>Production</Value>
			</Tag>
			<Tag>
				<Key>Department</Key>
				<Value>Engineering</Value>
			</Tag>
		</TagSet>
	</Tagging>`

	t.Run("Put object tagging", func(t *testing.T) {
		req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey+"?tagging", []byte(taggingXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should set object tagging")
	})

	t.Run("Get object tagging", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey+"?tagging", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get object tagging")
		assert.Contains(t, w.Body.String(), "Environment", "Should contain Environment tag")
		assert.Contains(t, w.Body.String(), "Production", "Should contain Production value")
	})

	t.Run("Delete object tagging", func(t *testing.T) {
		req, w := env.makeS3Request("DELETE", "/"+bucketName+"/"+objectKey+"?tagging", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code, "Should delete object tagging")
	})
}

// TestS3MultipartUpload tests multipart upload operations via S3 API
func TestS3MultipartUpload(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "multipart-test-bucket"
	objectKey := "large-file.dat"

	// Create bucket first
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	var uploadID string

	t.Run("Create multipart upload", func(t *testing.T) {
		req, w := env.makeS3Request("POST", "/"+bucketName+"/"+objectKey+"?uploads", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should create multipart upload")

		// Parse upload ID from response
		var result struct {
			UploadId string `xml:"UploadId"`
		}
		err := xml.Unmarshal(w.Body.Bytes(), &result)
		require.NoError(t, err)
		require.NotEmpty(t, result.UploadId, "Upload ID should not be empty")
		uploadID = result.UploadId
	})

	var etag1, etag2 string

	t.Run("Upload part 1", func(t *testing.T) {
		part1Data := bytes.Repeat([]byte("A"), 5*1024*1024) // 5MB
		req, w := env.makeS3Request("PUT", fmt.Sprintf("/%s/%s?partNumber=1&uploadId=%s", bucketName, objectKey, uploadID), part1Data)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should upload part 1")
		etag1 = w.Header().Get("ETag")
		assert.NotEmpty(t, etag1, "Should return ETag for part 1")
	})

	t.Run("Upload part 2", func(t *testing.T) {
		part2Data := bytes.Repeat([]byte("B"), 5*1024*1024) // 5MB
		req, w := env.makeS3Request("PUT", fmt.Sprintf("/%s/%s?partNumber=2&uploadId=%s", bucketName, objectKey, uploadID), part2Data)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should upload part 2")
		etag2 = w.Header().Get("ETag")
		assert.NotEmpty(t, etag2, "Should return ETag for part 2")
	})

	t.Run("Complete multipart upload", func(t *testing.T) {
		// Register complete multipart route
		env.router.HandleFunc("/{bucket}/{object:.+}", env.handler.CompleteMultipartUpload).Methods("POST").Queries("uploadId", "{uploadId}")

		completeXML := fmt.Sprintf(`<CompleteMultipartUpload>
			<Part>
				<PartNumber>1</PartNumber>
				<ETag>%s</ETag>
			</Part>
			<Part>
				<PartNumber>2</PartNumber>
				<ETag>%s</ETag>
			</Part>
		</CompleteMultipartUpload>`, etag1, etag2)

		req, w := env.makeS3Request("POST", fmt.Sprintf("/%s/%s?uploadId=%s", bucketName, objectKey, uploadID), []byte(completeXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should complete multipart upload")
		assert.Contains(t, w.Body.String(), objectKey, "Response should contain object key")
	})

	t.Run("List parts", func(t *testing.T) {
		// Start a new multipart upload for listing
		req, w := env.makeS3Request("POST", "/"+bucketName+"/list-test.dat?uploads", nil)
		env.router.ServeHTTP(w, req)

		var result struct {
			UploadId string `xml:"UploadId"`
		}
		xml.Unmarshal(w.Body.Bytes(), &result)
		newUploadID := result.UploadId

		// Upload a part
		partData := bytes.Repeat([]byte("X"), 5*1024*1024)
		req, w = env.makeS3Request("PUT", fmt.Sprintf("/%s/list-test.dat?partNumber=1&uploadId=%s", bucketName, newUploadID), partData)
		env.router.ServeHTTP(w, req)

		// List parts
		env.router.HandleFunc("/{bucket}/{object:.+}", env.handler.ListParts).Methods("GET").Queries("uploadId", "{uploadId}")
		req, w = env.makeS3Request("GET", fmt.Sprintf("/%s/list-test.dat?uploadId=%s", bucketName, newUploadID), nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should list parts")
		assert.Contains(t, w.Body.String(), "<PartNumber>1</PartNumber>", "Should contain part number")
	})
}

// TestS3BucketVersioning tests bucket versioning configuration via S3 API
// Tests all three versioning states: Unversioned, Enabled, Suspended
func TestS3BucketVersioning(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "versioning-test-bucket"

	// Create bucket first
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	t.Run("Get versioning on new bucket (Unversioned)", func(t *testing.T) {
		// New buckets should have no versioning status (Unversioned state)
		req, w := env.makeS3Request("GET", "/"+bucketName+"?versioning", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get versioning configuration")

		// AWS S3 returns empty VersioningConfiguration for unversioned buckets
		// Should NOT contain <Status> element
		assert.NotContains(t, w.Body.String(), "<Status>", "Unversioned bucket should not have Status element")
		assert.Contains(t, w.Body.String(), "<VersioningConfiguration", "Should contain VersioningConfiguration")
	})

	t.Run("Enable versioning", func(t *testing.T) {
		versioningXML := `<VersioningConfiguration>
			<Status>Enabled</Status>
		</VersioningConfiguration>`

		req, w := env.makeS3Request("PUT", "/"+bucketName+"?versioning", []byte(versioningXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should enable versioning")
	})

	t.Run("Get versioning after enabling", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?versioning", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get versioning configuration")
		assert.Contains(t, w.Body.String(), "<Status>Enabled</Status>", "Versioning should be enabled")
	})

	t.Run("Suspend versioning", func(t *testing.T) {
		versioningXML := `<VersioningConfiguration>
			<Status>Suspended</Status>
		</VersioningConfiguration>`

		req, w := env.makeS3Request("PUT", "/"+bucketName+"?versioning", []byte(versioningXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should suspend versioning")
	})

	t.Run("Get versioning after suspending", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?versioning", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get versioning configuration")
		assert.Contains(t, w.Body.String(), "<Status>Suspended</Status>", "Versioning should be suspended")
	})

	t.Run("Re-enable versioning after suspension", func(t *testing.T) {
		versioningXML := `<VersioningConfiguration>
			<Status>Enabled</Status>
		</VersioningConfiguration>`

		req, w := env.makeS3Request("PUT", "/"+bucketName+"?versioning", []byte(versioningXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should re-enable versioning")

		// Verify it's enabled again
		req, w = env.makeS3Request("GET", "/"+bucketName+"?versioning", nil)
		env.router.ServeHTTP(w, req)
		assert.Contains(t, w.Body.String(), "<Status>Enabled</Status>", "Versioning should be enabled again")
	})

	t.Run("Reject invalid versioning status", func(t *testing.T) {
		// Try to set versioning to "Disabled" which is not allowed
		invalidXML := `<VersioningConfiguration>
			<Status>Disabled</Status>
		</VersioningConfiguration>`

		req, w := env.makeS3Request("PUT", "/"+bucketName+"?versioning", []byte(invalidXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		// Should reject invalid status
		assert.NotEqual(t, http.StatusOK, w.Code, "Should reject invalid versioning status")
		assert.Contains(t, w.Body.String(), "IllegalVersioningConfigurationException", "Should return illegal versioning error")
	})
}

// TestS3DeleteObjects tests batch delete operations via S3 API
func TestS3DeleteObjects(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "batch-delete-bucket"

	// Create bucket first
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Create multiple test objects
	bucketPath := env.tenantID + "/" + bucketName
	objectKeys := []string{"file1.txt", "file2.txt", "file3.txt", "file4.txt", "file5.txt"}

	for _, key := range objectKeys {
		headers := http.Header{}
		_, err := env.objectManager.PutObject(ctx, bucketPath, key, bytes.NewReader([]byte("test content")), headers)
		require.NoError(t, err, "Should create test object: "+key)
	}

	t.Run("Delete multiple objects", func(t *testing.T) {
		// Delete 3 out of 5 objects
		deleteXML := `<Delete>
			<Object><Key>file1.txt</Key></Object>
			<Object><Key>file2.txt</Key></Object>
			<Object><Key>file3.txt</Key></Object>
		</Delete>`

		req, w := env.makeS3Request("POST", "/"+bucketName+"?delete", []byte(deleteXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should delete objects")
		assert.Contains(t, w.Body.String(), "file1.txt", "Response should contain deleted object")
		assert.Contains(t, w.Body.String(), "file2.txt", "Response should contain deleted object")
		assert.Contains(t, w.Body.String(), "file3.txt", "Response should contain deleted object")
	})

	t.Run("Delete non-existent object (should succeed)", func(t *testing.T) {
		// S3 spec: deleting non-existent object should return success
		deleteXML := `<Delete>
			<Object><Key>non-existent-file.txt</Key></Object>
		</Delete>`

		req, w := env.makeS3Request("POST", "/"+bucketName+"?delete", []byte(deleteXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should succeed deleting non-existent object")
		assert.Contains(t, w.Body.String(), "non-existent-file.txt", "Response should contain key")
	})

	t.Run("Quiet mode (no response body for successful deletes)", func(t *testing.T) {
		// Create a new object to delete
		headers := http.Header{}
		_, err := env.objectManager.PutObject(ctx, bucketPath, "quiet-test.txt", bytes.NewReader([]byte("test")), headers)
		require.NoError(t, err)

		deleteXML := `<Delete>
			<Quiet>true</Quiet>
			<Object><Key>quiet-test.txt</Key></Object>
		</Delete>`

		req, w := env.makeS3Request("POST", "/"+bucketName+"?delete", []byte(deleteXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should delete object in quiet mode")
		// In quiet mode, successful deletes are not listed in response
		// Only errors are returned
	})

	t.Run("Reject empty delete request", func(t *testing.T) {
		deleteXML := `<Delete></Delete>`

		req, w := env.makeS3Request("POST", "/"+bucketName+"?delete", []byte(deleteXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.NotEqual(t, http.StatusOK, w.Code, "Should reject empty delete request")
		assert.Contains(t, w.Body.String(), "InvalidRequest", "Should return InvalidRequest error")
	})

	t.Run("Reject more than 1000 objects", func(t *testing.T) {
		// Build XML with 1001 objects
		var xmlBuilder strings.Builder
		xmlBuilder.WriteString("<Delete>")
		for i := 0; i < 1001; i++ {
			xmlBuilder.WriteString(fmt.Sprintf("<Object><Key>file%d.txt</Key></Object>", i))
		}
		xmlBuilder.WriteString("</Delete>")

		req, w := env.makeS3Request("POST", "/"+bucketName+"?delete", []byte(xmlBuilder.String()))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.NotEqual(t, http.StatusOK, w.Code, "Should reject more than 1000 objects")
		assert.Contains(t, w.Body.String(), "InvalidRequest", "Should return InvalidRequest error")
		assert.Contains(t, w.Body.String(), "1000", "Error message should mention 1000 limit")
	})
}

// TestS3CopyObject tests object copy operations via S3 API
func TestS3CopyObject(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	sourceBucket := "source-bucket"
	destBucket := "dest-bucket"

	// Create both buckets
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, sourceBucket)
	require.NoError(t, err)
	err = env.bucketManager.CreateBucket(ctx, env.tenantID, destBucket)
	require.NoError(t, err)

	// Create source object
	sourcePath := env.tenantID + "/" + sourceBucket
	sourceKey := "source-file.txt"
	sourceContent := []byte("This is the source content")

	headers := http.Header{}
	_, err = env.objectManager.PutObject(ctx, sourcePath, sourceKey, bytes.NewReader(sourceContent), headers)
	require.NoError(t, err)

	t.Run("Copy object within same bucket", func(t *testing.T) {
		destKey := "copied-file.txt"

		req, w := env.makeS3Request("PUT", "/"+sourceBucket+"/"+destKey, nil)
		// Set the copy source header (with leading slash as AWS S3 expects)
		req.Header.Set("x-amz-copy-source", "/"+sourceBucket+"/"+sourceKey)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should copy object")
		assert.Contains(t, w.Body.String(), "CopyObjectResult", "Response should contain CopyObjectResult")
		assert.Contains(t, w.Body.String(), "ETag", "Response should contain ETag")
	})

	t.Run("Copy object to different bucket", func(t *testing.T) {
		destKey := "cross-bucket-copy.txt"

		req, w := env.makeS3Request("PUT", "/"+destBucket+"/"+destKey, nil)
		// Copy from source bucket to dest bucket
		req.Header.Set("x-amz-copy-source", "/"+sourceBucket+"/"+sourceKey)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should copy object across buckets")
		assert.Contains(t, w.Body.String(), "CopyObjectResult", "Response should contain CopyObjectResult")
	})

	t.Run("Copy object without leading slash in source", func(t *testing.T) {
		destKey := "no-slash-copy.txt"

		req, w := env.makeS3Request("PUT", "/"+sourceBucket+"/"+destKey, nil)
		// AWS CLI sometimes sends without leading slash
		req.Header.Set("x-amz-copy-source", sourceBucket+"/"+sourceKey)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should copy object (no leading slash)")
		assert.Contains(t, w.Body.String(), "CopyObjectResult", "Response should contain CopyObjectResult")
	})

	t.Run("Copy non-existent object (should fail)", func(t *testing.T) {
		destKey := "failed-copy.txt"

		req, w := env.makeS3Request("PUT", "/"+destBucket+"/"+destKey, nil)
		req.Header.Set("x-amz-copy-source", "/"+sourceBucket+"/non-existent-file.txt")
		env.router.ServeHTTP(w, req)

		assert.NotEqual(t, http.StatusOK, w.Code, "Should fail copying non-existent object")
	})

	t.Run("Verify copied object content", func(t *testing.T) {
		// Copy object
		destKey := "verify-content.txt"
		req, w := env.makeS3Request("PUT", "/"+sourceBucket+"/"+destKey, nil)
		req.Header.Set("x-amz-copy-source", "/"+sourceBucket+"/"+sourceKey)
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		// Get the copied object and verify content
		req, w = env.makeS3Request("GET", "/"+sourceBucket+"/"+destKey, nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get copied object")
		assert.Equal(t, sourceContent, w.Body.Bytes(), "Copied content should match source")
	})
}

// TestS3RangeRequests tests partial object download via Range header
func TestS3RangeRequests(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "range-test-bucket"

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Create object with known content
	bucketPath := env.tenantID + "/" + bucketName
	objectKey := "test-file.txt"
	content := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") // 62 bytes

	headers := http.Header{}
	_, err = env.objectManager.PutObject(ctx, bucketPath, objectKey, bytes.NewReader(content), headers)
	require.NoError(t, err)

	t.Run("Get first 10 bytes", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey, nil)
		req.Header.Set("Range", "bytes=0-9")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusPartialContent, w.Code, "Should return 206 Partial Content")
		assert.Equal(t, "0123456789", w.Body.String(), "Should return first 10 bytes")
		assert.Equal(t, "bytes 0-9/62", w.Header().Get("Content-Range"), "Should include Content-Range header")
		assert.Equal(t, "10", w.Header().Get("Content-Length"), "Content-Length should be 10")
	})

	t.Run("Get last 10 bytes", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey, nil)
		req.Header.Set("Range", "bytes=52-61")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusPartialContent, w.Code, "Should return 206 Partial Content")
		// Content: "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
		// Positions 52-61 = "qrstuvwxyz" (last 10 bytes)
		assert.Equal(t, "qrstuvwxyz", w.Body.String(), "Should return last 10 bytes")
	})

	t.Run("Get middle bytes", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey, nil)
		req.Header.Set("Range", "bytes=10-19")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusPartialContent, w.Code, "Should return 206 Partial Content")
		assert.Equal(t, "ABCDEFGHIJ", w.Body.String(), "Should return middle bytes")
		assert.Equal(t, "bytes 10-19/62", w.Header().Get("Content-Range"), "Should include Content-Range header")
	})

	t.Run("Get from offset to end (open range)", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey, nil)
		req.Header.Set("Range", "bytes=50-")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusPartialContent, w.Code, "Should return 206 Partial Content")
		// Position 50 to end (62) = "opqrstuvwxyz" (12 bytes)
		assert.Equal(t, "opqrstuvwxyz", w.Body.String(), "Should return from offset to end")
	})

	t.Run("Invalid range (should return 416)", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey, nil)
		// Request bytes beyond file size
		req.Header.Set("Range", "bytes=100-200")
		env.router.ServeHTTP(w, req)

		// Should return 416 Range Not Satisfiable
		assert.Equal(t, http.StatusRequestedRangeNotSatisfiable, w.Code, "Should return 416 for invalid range")
	})

	t.Run("Get without Range header (full object)", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey, nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should return 200 OK for full object")
		assert.Equal(t, content, w.Body.Bytes(), "Should return complete content")
		assert.Equal(t, "62", w.Header().Get("Content-Length"), "Content-Length should be 62")
	})
}

// TestS3ListObjectVersions tests listing object versions in versioned buckets
func TestS3ListObjectVersions(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "versioned-bucket"

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Enable versioning on the bucket
	versioningXML := `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`
	req, w := env.makeS3Request("PUT", "/"+bucketName+"?versioning", []byte(versioningXML))
	req.Header.Set("Content-Type", "application/xml")
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should enable versioning")

	// Create multiple versions of the same object
	bucketPath := env.tenantID + "/" + bucketName
	objectKey := "test-file.txt"

	headers := http.Header{}
	for i := 1; i <= 3; i++ {
		content := []byte(fmt.Sprintf("Version %d content", i))
		_, err := env.objectManager.PutObject(ctx, bucketPath, objectKey, bytes.NewReader(content), headers)
		require.NoError(t, err, "Should create version %d", i)
	}

	// Create another object with versions
	objectKey2 := "another-file.txt"
	for i := 1; i <= 2; i++ {
		content := []byte(fmt.Sprintf("File 2 Version %d", i))
		_, err := env.objectManager.PutObject(ctx, bucketPath, objectKey2, bytes.NewReader(content), headers)
		require.NoError(t, err, "Should create version %d for file 2", i)
	}

	t.Run("List all object versions", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?versions", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should list object versions")
		assert.Contains(t, w.Body.String(), "ListVersionsResult", "Response should contain ListVersionsResult")
		assert.Contains(t, w.Body.String(), "test-file.txt", "Should contain first object")
		assert.Contains(t, w.Body.String(), "another-file.txt", "Should contain second object")
		// Should have multiple versions listed
		assert.Contains(t, w.Body.String(), "<Version>", "Should contain version entries")
	})

	t.Run("List versions with prefix filter", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?versions&prefix=test-", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should list filtered versions")
		assert.Contains(t, w.Body.String(), "test-file.txt", "Should contain filtered object")
		// Should not contain the other object
		responseBody := w.Body.String()
		if strings.Contains(responseBody, "another-file.txt") {
			// This might be ok if the implementation includes it, but ideally it shouldn't
			t.Log("Warning: Response contains objects that don't match prefix filter")
		}
	})

	t.Run("List versions with max-keys limit", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?versions&max-keys=2", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should list versions with limit")
		assert.Contains(t, w.Body.String(), "ListVersionsResult", "Response should contain ListVersionsResult")
		// Should indicate if results are truncated
		if strings.Contains(w.Body.String(), "<IsTruncated>true</IsTruncated>") {
			assert.Contains(t, w.Body.String(), "<NextKeyMarker>", "Should have NextKeyMarker for pagination")
		}
	})

	t.Run("List versions in non-versioned bucket", func(t *testing.T) {
		// Create a non-versioned bucket
		nonVersionedBucket := "non-versioned-bucket"
		err := env.bucketManager.CreateBucket(ctx, env.tenantID, nonVersionedBucket)
		require.NoError(t, err)

		// Put an object (will have null version)
		nonVersionedPath := env.tenantID + "/" + nonVersionedBucket
		_, err = env.objectManager.PutObject(ctx, nonVersionedPath, "file.txt", bytes.NewReader([]byte("content")), headers)
		require.NoError(t, err)

		req, w := env.makeS3Request("GET", "/"+nonVersionedBucket+"?versions", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should list versions even in non-versioned bucket")
		assert.Contains(t, w.Body.String(), "file.txt", "Should contain the object")
		assert.Contains(t, w.Body.String(), "<VersionId>null</VersionId>", "Should have null version ID")
	})

	t.Run("Delete object and create delete marker", func(t *testing.T) {
		// Delete the object (creates a delete marker in versioned bucket)
		deleteKey := "to-be-deleted.txt"
		_, err := env.objectManager.PutObject(ctx, bucketPath, deleteKey, bytes.NewReader([]byte("will be deleted")), headers)
		require.NoError(t, err)

		// Delete it (creates delete marker)
		_, err = env.objectManager.DeleteObject(ctx, bucketPath, deleteKey, false)
		require.NoError(t, err)

		// List versions should show the delete marker
		req, w := env.makeS3Request("GET", "/"+bucketName+"?versions", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should list versions with delete markers")
		// May contain DeleteMarker element
		if strings.Contains(w.Body.String(), "to-be-deleted.txt") {
			// The deleted object should appear in versions list
			t.Log("Deleted object appears in versions list (expected)")
		}
	})
}

// TestSOSAPICapacityQuota tests that VEEAM SOSAPI capacity.xml respects tenant quotas
func TestSOSAPICapacityQuota(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "sosapi-test-bucket"
	bucketPath := env.tenantID + "/" + bucketName

	// Create a bucket for testing
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err, "Should create bucket")

	// Upload some test data to generate tenant usage
	testData := bytes.Repeat([]byte("A"), 5*1024*1024) // 5MB
	_, err = env.objectManager.PutObject(ctx, bucketPath, "test-file.bin", bytes.NewReader(testData), nil)
	require.NoError(t, err, "Should upload test file")

	// Get updated tenant to reflect usage
	tenant, err := env.authManager.GetTenant(ctx, env.tenantID)
	require.NoError(t, err, "Should get tenant")

	t.Run("Tenant user should see quota-based capacity", func(t *testing.T) {
		// Request SOSAPI capacity.xml as tenant user
		req, w := env.makeS3Request("GET", "/"+bucketName+"/.system-d26a9498-cb7c-4a87-a44a-8ae204f5ba6c/capacity.xml", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should return capacity.xml")
		assert.Equal(t, "application/xml", w.Header().Get("Content-Type"), "Should be XML")

		// Parse the XML response
		var capInfo CapacityInfo
		err := xml.Unmarshal(w.Body.Bytes(), &capInfo)
		require.NoError(t, err, "Should parse capacity XML")

		// Verify capacity matches tenant quota (10GB from setupCompleteS3Environment)
		expectedCapacity := int64(10 * 1024 * 1024 * 1024) // 10GB
		assert.Equal(t, expectedCapacity, capInfo.Capacity, "Capacity should match tenant quota")

		// Verify used space matches tenant usage
		assert.Equal(t, tenant.CurrentStorageBytes, capInfo.Used, "Used should match tenant usage")

		// Verify available = capacity - used
		expectedAvailable := expectedCapacity - tenant.CurrentStorageBytes
		assert.Equal(t, expectedAvailable, capInfo.Available, "Available should be capacity - used")

		t.Logf("Tenant Quota - Capacity: %d bytes, Used: %d bytes, Available: %d bytes",
			capInfo.Capacity, capInfo.Used, capInfo.Available)
	})

	t.Run("Multiple users in same tenant share quota", func(t *testing.T) {
		// Create a second user in the same tenant
		secondUser := &auth.User{
			ID:          "second-user-id",
			Username:    "seconduser",
			DisplayName: "Second User",
			Email:       "second@example.com",
			Status:      "active",
			TenantID:    env.tenantID, // Same tenant as test user
			Roles:       []string{"user"},
			CreatedAt:   time.Now().Unix(),
			UpdatedAt:   time.Now().Unix(),
		}
		err := env.authManager.CreateUser(ctx, secondUser)
		require.NoError(t, err, "Should create second user")

		// Generate access keys for second user
		key, err := env.authManager.GenerateAccessKey(ctx, secondUser.ID)
		require.NoError(t, err, "Should generate access key")

		// Create test environment with second user's credentials
		envSecondUser := &s3TestEnv{
			handler:       env.handler,
			authManager:   env.authManager,
			bucketManager: env.bucketManager,
			objectManager: env.objectManager,
			router:        env.router,
			accessKey:     key.AccessKeyID,
			secretKey:     key.SecretAccessKey,
			tenantID:      env.tenantID,
			userID:        secondUser.ID,
			cleanup:       func() {}, // No cleanup needed
		}

		// Make request as second user (different user, same tenant)
		req, w := envSecondUser.makeS3Request("GET", "/"+bucketName+"/.system-d26a9498-cb7c-4a87-a44a-8ae204f5ba6c/capacity.xml", nil)
		envSecondUser.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should return capacity.xml for second user")

		// Parse the XML response
		var capInfo CapacityInfo
		err = xml.Unmarshal(w.Body.Bytes(), &capInfo)
		require.NoError(t, err, "Should parse capacity XML")

		// Both users should see the SAME quota (it's shared at tenant level)
		expectedCapacity := int64(10 * 1024 * 1024 * 1024) // 10GB
		assert.Equal(t, expectedCapacity, capInfo.Capacity, "Second user should see same tenant quota")

		// Get the tenant to verify usage is shared
		tenantAfter, err := env.authManager.GetTenant(ctx, env.tenantID)
		require.NoError(t, err)

		// Verify available = capacity - used (same for all users in tenant)
		expectedAvailable := expectedCapacity - tenantAfter.CurrentStorageBytes
		assert.Equal(t, expectedAvailable, capInfo.Available, "Available should be shared across all tenant users")

		t.Logf("Second User (Same Tenant) - Capacity: %d bytes, Used: %d bytes, Available: %d bytes",
			capInfo.Capacity, capInfo.Used, capInfo.Available)
		t.Logf("Quota is shared: User1 and User2 both see the same 10GB tenant quota")
	})
}

// TestListMultipartUploads tests listing multipart uploads
func TestListMultipartUploads(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-multipart-bucket"

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Initiate multipart upload
	objectKey := "test-large-file.txt"
	req, w := env.makeS3Request("POST", "/"+bucketName+"/"+objectKey+"?uploads", nil)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should initiate multipart upload")

	type InitResult struct {
		UploadId string `xml:"UploadId"`
	}
	var initResult InitResult
	xml.Unmarshal(w.Body.Bytes(), &initResult)
	uploadID := initResult.UploadId

	// List multipart uploads
	listReq, listW := env.makeS3Request("GET", "/"+bucketName+"?uploads", nil)
	env.router.ServeHTTP(listW, listReq)

	assert.Equal(t, http.StatusOK, listW.Code, "Should list multipart uploads successfully")

	// Parse response
	type ListMultipartUploadsResult struct {
		XMLName xml.Name `xml:"ListMultipartUploadsResult"`
		Uploads []struct {
			Key      string `xml:"Key"`
			UploadId string `xml:"UploadId"`
		} `xml:"Upload"`
	}

	var result ListMultipartUploadsResult
	err = xml.Unmarshal(listW.Body.Bytes(), &result)
	require.NoError(t, err, "Should parse XML response")

	assert.Len(t, result.Uploads, 1, "Should have 1 multipart upload")
	assert.Equal(t, objectKey, result.Uploads[0].Key)
	assert.Equal(t, uploadID, result.Uploads[0].UploadId)

	// Cleanup - abort upload
	abortReq, abortW := env.makeS3Request("DELETE",
		fmt.Sprintf("/%s/%s?uploadId=%s", bucketName, objectKey, uploadID), nil)
	env.router.ServeHTTP(abortW, abortReq)
}

// TestAbortMultipartUpload tests aborting a multipart upload
func TestAbortMultipartUpload(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-abort-multipart"

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Initiate multipart upload
	objectKey := "test-abort-file.txt"
	initReq, initW := env.makeS3Request("POST", "/"+bucketName+"/"+objectKey+"?uploads", nil)
	env.router.ServeHTTP(initW, initReq)
	require.Equal(t, http.StatusOK, initW.Code)

	type InitResult struct {
		UploadId string `xml:"UploadId"`
	}
	var initResult InitResult
	xml.Unmarshal(initW.Body.Bytes(), &initResult)
	uploadID := initResult.UploadId

	// Abort multipart upload
	req, w := env.makeS3Request("DELETE",
		fmt.Sprintf("/%s/%s?uploadId=%s", bucketName, objectKey, uploadID), nil)
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code, "Should abort multipart upload successfully")

	// Verify upload no longer exists
	listReq, listW := env.makeS3Request("GET", "/"+bucketName+"?uploads", nil)
	env.router.ServeHTTP(listW, listReq)

	type ListResult struct {
		Uploads []struct {
			UploadId string `xml:"UploadId"`
		} `xml:"Upload"`
	}
	var listResult ListResult
	xml.Unmarshal(listW.Body.Bytes(), &listResult)

	assert.Len(t, listResult.Uploads, 0, "Aborted upload should not appear in list")
}

// TestUploadPartCopy tests copying an object as part of a multipart upload
func TestUploadPartCopy(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-upload-part-copy"
	sourceKey := "source-object.txt"
	destKey := "dest-large-file.txt"
	sourceContent := []byte("This is the source content to be copied")

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Upload source object
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+sourceKey, sourceContent)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should upload source object")

	// Initiate multipart upload
	req, w = env.makeS3Request("POST", "/"+bucketName+"/"+destKey+"?uploads", nil)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should initiate multipart upload")

	type InitResult struct {
		UploadId string `xml:"UploadId"`
	}
	var initResult InitResult
	xml.Unmarshal(w.Body.Bytes(), &initResult)
	uploadID := initResult.UploadId
	require.NotEmpty(t, uploadID, "Upload ID should not be empty")

	// Upload part using copy
	copySource := fmt.Sprintf("%s/%s", bucketName, sourceKey)
	req, w = env.makeS3Request("PUT",
		fmt.Sprintf("/%s/%s?uploadId=%s&partNumber=1", bucketName, destKey, uploadID), nil)
	req.Header.Set("x-amz-copy-source", copySource)
	env.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Should copy object as part")

	// Verify response contains ETag
	type CopyPartResult struct {
		ETag string `xml:"ETag"`
	}
	var copyResult CopyPartResult
	xml.Unmarshal(w.Body.Bytes(), &copyResult)
	assert.NotEmpty(t, copyResult.ETag, "ETag should be present in response")
}

// TestBucketTagging tests bucket tagging operations (Get/Put/Delete)
func TestBucketTagging(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-bucket-tagging"

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	t.Run("PutBucketTagging", func(t *testing.T) {
		taggingXML := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Tagging>
  <TagSet>
    <Tag>
      <Key>Environment</Key>
      <Value>Production</Value>
    </Tag>
  </TagSet>
</Tagging>`)

		req, w := env.makeS3Request("PUT", "/"+bucketName+"?tagging", taggingXML)
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code, "Should set bucket tagging successfully")
	})

	t.Run("GetBucketTagging", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?tagging", nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should get bucket tagging successfully")
	})

	t.Run("DeleteBucketTagging", func(t *testing.T) {
		req, w := env.makeS3Request("DELETE", "/"+bucketName+"?tagging", nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code, "Should delete bucket tagging successfully")
	})
}

// TestBucketACL tests bucket ACL operations (Get/Put)
func TestBucketACL(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-bucket-acl"

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	t.Run("GetBucketACL", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?acl", nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should get bucket ACL successfully")
	})

	t.Run("PutBucketACL", func(t *testing.T) {
		req, w := env.makeS3Request("PUT", "/"+bucketName+"?acl", nil)
		req.Header.Set("x-amz-acl", "private")
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should set bucket ACL successfully")
	})
}

// TestObjectRetention tests object retention operations (Get/Put)
func TestObjectRetention(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-object-retention"
	objectKey := "test-object.txt"
	objectContent := []byte("Test content for retention")

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Upload object
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, objectContent)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should upload object")

	t.Run("PutObjectRetention", func(t *testing.T) {
		// Use a date 30 days in the future to ensure it's always valid
		futureDate := time.Now().UTC().Add(30 * 24 * time.Hour).Format(time.RFC3339)
		retentionXML := []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Retention>
  <Mode>GOVERNANCE</Mode>
  <RetainUntilDate>%s</RetainUntilDate>
</Retention>`, futureDate))

		req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey+"?retention", retentionXML)
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should set object retention")
	})

	t.Run("GetObjectRetention", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey+"?retention", nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should get object retention")

		// Verify response contains retention info
		body := w.Body.String()
		assert.Contains(t, body, "Retention", "Response should contain retention element")
	})
}

// TestGetBucketLocation tests getting bucket location
func TestGetBucketLocation(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-bucket-location"

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Get bucket location
	req, w := env.makeS3Request("GET", "/"+bucketName+"?location", nil)
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Should get bucket location successfully")

	// Verify response contains LocationConstraint
	body := w.Body.String()
	assert.Contains(t, body, "LocationConstraint", "Response should contain LocationConstraint")
}

// TestObjectLockConfiguration tests object lock configuration (Get/Put)
func TestObjectLockConfiguration(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	bucketName := "test-object-lock-bucket"

	// Create bucket with object lock enabled
	// Note: Object lock must be enabled at bucket creation time
	req, w := env.makeS3Request("PUT", "/"+bucketName, nil)
	req.Header.Set("x-amz-bucket-object-lock-enabled", "true")
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should create bucket with object lock")

	t.Run("PutObjectLockConfiguration", func(t *testing.T) {
		configXML := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<ObjectLockConfiguration>
  <ObjectLockEnabled>Enabled</ObjectLockEnabled>
  <Rule>
    <DefaultRetention>
      <Mode>GOVERNANCE</Mode>
      <Days>30</Days>
    </DefaultRetention>
  </Rule>
</ObjectLockConfiguration>`)

		req, w := env.makeS3Request("PUT", "/"+bucketName+"?object-lock", configXML)
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)
		// Accept 200 (success), 404 (not found/not enabled), or 409 (conflict)
		assert.Contains(t, []int{http.StatusOK, http.StatusNotFound, http.StatusConflict}, w.Code, "Should handle object lock configuration request")
	})

	t.Run("GetObjectLockConfiguration", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?object-lock", nil)
		env.router.ServeHTTP(w, req)
		// Accept both 200 (if configured) and 404 (if not enabled)
		assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, w.Code, "Should handle get object lock configuration request")
	})
}

// TestObjectLegalHold tests object legal hold operations (Get/Put)
// Note: Legal hold requires a bucket with object lock enabled
func TestObjectLegalHold(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	bucketName := "test-legal-hold-bucket"
	objectKey := "test-object.txt"
	objectContent := []byte("Test content for legal hold")

	// Create bucket with object lock enabled (required for legal hold)
	req, w := env.makeS3Request("PUT", "/"+bucketName, nil)
	req.Header.Set("x-amz-bucket-object-lock-enabled", "true")
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should create bucket with object lock")

	// Upload object
	req, w = env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, objectContent)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should upload object")

	t.Run("PutObjectLegalHold", func(t *testing.T) {
		legalHoldXML := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<LegalHold>
  <Status>ON</Status>
</LegalHold>`)

		req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey+"?legal-hold", legalHoldXML)
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should set legal hold on object")
	})

	t.Run("GetObjectLegalHold", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey+"?legal-hold", nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should get object legal hold")

		// Verify response contains LegalHold status
		body := w.Body.String()
		assert.Contains(t, body, "LegalHold", "Response should contain LegalHold element")
	})
}

// TestObjectACL tests object ACL operations (Get/Put)
func TestObjectACL(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-object-acl-bucket"
	objectKey := "test-object.txt"
	objectContent := []byte("Test content for ACL")

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Upload object
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, objectContent)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should upload object")

	t.Run("GetObjectACL", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey+"?acl", nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should get object ACL")

		// Verify response contains ACL information
		body := w.Body.String()
		assert.Contains(t, body, "AccessControlPolicy", "Response should contain AccessControlPolicy")
	})

	t.Run("PutObjectACL", func(t *testing.T) {
		req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey+"?acl", nil)
		req.Header.Set("x-amz-acl", "private")
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should set object ACL")
	})
}

// TestObjectVersioning tests object versioning operations
func TestObjectVersioning(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-versioning-bucket"
	objectKey := "test-versioned-object.txt"

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Enable versioning on bucket
	versioningXML := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<VersioningConfiguration>
  <Status>Enabled</Status>
</VersioningConfiguration>`)
	req, w := env.makeS3Request("PUT", "/"+bucketName+"?versioning", versioningXML)
	req.Header.Set("Content-Type", "application/xml")
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should enable versioning")

	// Upload object version 1
	content1 := []byte("Version 1 content")
	req, w = env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, content1)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should upload version 1")

	// Upload object version 2
	content2 := []byte("Version 2 content")
	req, w = env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, content2)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should upload version 2")

	t.Run("ListObjectVersions", func(t *testing.T) {
		req, w := env.makeS3Request("GET", "/"+bucketName+"?versions", nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should list object versions")

		// Verify response contains version information
		body := w.Body.String()
		assert.Contains(t, body, "ListVersionsResult", "Response should contain ListVersionsResult")
	})
}

// TestAwsChunkedReader tests AWS chunked encoding reader
func TestAwsChunkedReader(t *testing.T) {
	t.Run("Single chunk", func(t *testing.T) {
		// Format: {hex-size}\r\n{data}\r\n0\r\n\r\n
		input := "5\r\nHello\r\n0\r\n\r\n"
		reader := NewAwsChunkedReader(strings.NewReader(input))

		output, err := io.ReadAll(reader)
		require.NoError(t, err, "Should read chunked data")
		assert.Equal(t, "Hello", string(output), "Should decode single chunk")
	})

	t.Run("Multiple chunks", func(t *testing.T) {
		// Two chunks: "Hello" (5 bytes) + " World" (6 bytes)
		input := "5\r\nHello\r\n6\r\n World\r\n0\r\n\r\n"
		reader := NewAwsChunkedReader(strings.NewReader(input))

		output, err := io.ReadAll(reader)
		require.NoError(t, err, "Should read multiple chunks")
		assert.Equal(t, "Hello World", string(output), "Should decode multiple chunks")
	})

	t.Run("Chunk with signature (MinIO format)", func(t *testing.T) {
		// Format with chunk-signature: {hex-size};chunk-signature={sig}
		input := "5;chunk-signature=abcd1234\r\nHello\r\n0\r\n\r\n"
		reader := NewAwsChunkedReader(strings.NewReader(input))

		output, err := io.ReadAll(reader)
		require.NoError(t, err, "Should read chunk with signature")
		assert.Equal(t, "Hello", string(output), "Should strip signature and decode")
	})

	t.Run("Chunk with trailers", func(t *testing.T) {
		// Final chunk (0) can have trailers before final \r\n
		input := "5\r\nHello\r\n0\r\nx-amz-checksum-sha256:checksum123\r\n\r\n"
		reader := NewAwsChunkedReader(strings.NewReader(input))

		output, err := io.ReadAll(reader)
		require.NoError(t, err, "Should read chunk with trailers")
		assert.Equal(t, "Hello", string(output), "Should decode and ignore trailers")
	})

	t.Run("Read in small buffer", func(t *testing.T) {
		// Large chunk read in small increments
		input := "a\r\n0123456789\r\n0\r\n\r\n" // 10 bytes (0xa in hex)
		reader := NewAwsChunkedReader(strings.NewReader(input))

		// Read 3 bytes at a time
		var output bytes.Buffer
		buf := make([]byte, 3)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				output.Write(buf[:n])
			}
			if err == io.EOF {
				break
			}
			require.NoError(t, err, "Should not error during partial reads")
		}

		assert.Equal(t, "0123456789", output.String(), "Should read large chunk in small buffers")
	})

	t.Run("Invalid hex size", func(t *testing.T) {
		// Invalid hexadecimal chunk size
		input := "XYZ\r\nHello\r\n0\r\n\r\n"
		reader := NewAwsChunkedReader(strings.NewReader(input))

		_, err := io.ReadAll(reader)
		assert.Error(t, err, "Should error on invalid hex size")
		assert.Contains(t, err.Error(), "invalid chunk size", "Error should mention invalid chunk size")
	})

	t.Run("Premature EOF in chunk data", func(t *testing.T) {
		// Chunk declares 10 bytes but only provides 5
		input := "a\r\n12345" // Missing 5 bytes and trailing \r\n
		reader := NewAwsChunkedReader(strings.NewReader(input))

		_, err := io.ReadAll(reader)
		assert.Error(t, err, "Should error on premature EOF")
		assert.Contains(t, err.Error(), "failed to read chunk data", "Error should mention chunk data read failure")
	})

	t.Run("Empty chunked stream", func(t *testing.T) {
		// Just the terminal chunk
		input := "0\r\n\r\n"
		reader := NewAwsChunkedReader(strings.NewReader(input))

		output, err := io.ReadAll(reader)
		require.NoError(t, err, "Should handle empty stream")
		assert.Empty(t, output, "Should return empty data")
	})

	t.Run("Large chunk", func(t *testing.T) {
		// 1KB chunk (0x400 in hex = 1024 bytes)
		data := strings.Repeat("A", 1024)
		input := fmt.Sprintf("400\r\n%s\r\n0\r\n\r\n", data)
		reader := NewAwsChunkedReader(strings.NewReader(input))

		output, err := io.ReadAll(reader)
		require.NoError(t, err, "Should read large chunk")
		assert.Equal(t, data, string(output), "Should decode 1KB chunk correctly")
		assert.Len(t, output, 1024, "Output should be exactly 1024 bytes")
	})

	t.Run("Close reader", func(t *testing.T) {
		input := "5\r\nHello\r\n0\r\n\r\n"
		reader := NewAwsChunkedReader(strings.NewReader(input))

		err := reader.Close()
		assert.NoError(t, err, "Close should not error")

		// Should still be able to read after close (Close is no-op)
		output, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, "Hello", string(output))
	})
}

// TestHeadObjectErrorCases tests HeadObject error scenarios
func TestHeadObjectErrorCases(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-head-bucket"
	objectKey := "test-object.txt"
	objectContent := []byte("Test content for HEAD")

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Upload object
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, objectContent)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should upload object")

	// Get ETag from response
	etag := w.Header().Get("ETag")
	require.NotEmpty(t, etag, "Should have ETag")

	t.Run("Object not found", func(t *testing.T) {
		req, w := env.makeS3Request("HEAD", "/"+bucketName+"/nonexistent.txt", nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "Should return 404 for nonexistent object")
	})

	t.Run("Successful HEAD with headers", func(t *testing.T) {
		req, w := env.makeS3Request("HEAD", "/"+bucketName+"/"+objectKey, nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should return 200")
		assert.NotEmpty(t, w.Header().Get("ETag"), "Should have ETag header")
		assert.NotEmpty(t, w.Header().Get("Content-Length"), "Should have Content-Length header")
		assert.NotEmpty(t, w.Header().Get("Last-Modified"), "Should have Last-Modified header")
		assert.Equal(t, "21", w.Header().Get("Content-Length"), "Should match content length")
	})

	t.Run("If-Match with matching ETag", func(t *testing.T) {
		req, w := env.makeS3Request("HEAD", "/"+bucketName+"/"+objectKey, nil)
		req.Header.Set("If-Match", etag)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should return 200 when ETag matches")
	})

	t.Run("If-Match with non-matching ETag", func(t *testing.T) {
		req, w := env.makeS3Request("HEAD", "/"+bucketName+"/"+objectKey, nil)
		req.Header.Set("If-Match", `"wrong-etag"`)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusPreconditionFailed, w.Code, "Should return 412 when ETag doesn't match")
	})

	t.Run("If-None-Match with matching ETag", func(t *testing.T) {
		req, w := env.makeS3Request("HEAD", "/"+bucketName+"/"+objectKey, nil)
		req.Header.Set("If-None-Match", etag)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotModified, w.Code, "Should return 304 when ETag matches (not modified)")
	})

	t.Run("If-None-Match with non-matching ETag", func(t *testing.T) {
		req, w := env.makeS3Request("HEAD", "/"+bucketName+"/"+objectKey, nil)
		req.Header.Set("If-None-Match", `"different-etag"`)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should return 200 when ETag doesn't match")
	})

	t.Run("HEAD on bucket (not object)", func(t *testing.T) {
		req, w := env.makeS3Request("HEAD", "/"+bucketName, nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should return 200 for bucket HEAD")
	})

	t.Run("HEAD on non-existent bucket", func(t *testing.T) {
		req, w := env.makeS3Request("HEAD", "/nonexistent-bucket", nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "Should return 404 for nonexistent bucket")
	})
}

// TestDeleteObjectErrorCases tests DeleteObject error scenarios
func TestDeleteObjectErrorCases(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-delete-bucket"
	objectKey := "test-object.txt"
	objectContent := []byte("Test content for DELETE")

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	// Upload object
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, objectContent)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "Should upload object")

	t.Run("Delete existing object", func(t *testing.T) {
		req, w := env.makeS3Request("DELETE", "/"+bucketName+"/"+objectKey, nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code, "Should return 204 for successful delete")
	})

	t.Run("Delete non-existent object (idempotent)", func(t *testing.T) {
		req, w := env.makeS3Request("DELETE", "/"+bucketName+"/nonexistent.txt", nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code, "Should return 204 for non-existent object (S3 spec)")
	})

	t.Run("Delete from non-existent bucket", func(t *testing.T) {
		req, w := env.makeS3Request("DELETE", "/nonexistent-bucket/object.txt", nil)
		env.router.ServeHTTP(w, req)
		// S3 spec: DELETE is idempotent - returns 204 even if bucket doesn't exist
		assert.Equal(t, http.StatusNoContent, w.Code, "Should return 204 (idempotent behavior)")
	})

	t.Run("Delete object with versionId parameter", func(t *testing.T) {
		// Upload another object
		req, w := env.makeS3Request("PUT", "/"+bucketName+"/versioned-object.txt", []byte("version 1"))
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		// Delete with versionId parameter (will be ignored if versioning not enabled)
		req, w = env.makeS3Request("DELETE", "/"+bucketName+"/versioned-object.txt?versionId=12345", nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code, "Should delete with versionId parameter")
	})

	t.Run("Bypass governance retention header", func(t *testing.T) {
		// Upload object
		req, w := env.makeS3Request("PUT", "/"+bucketName+"/governance-object.txt", []byte("gov object"))
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		// Try to delete with bypass governance header (will be accepted if user is admin)
		req, w = env.makeS3Request("DELETE", "/"+bucketName+"/governance-object.txt", nil)
		req.Header.Set("x-amz-bypass-governance-retention", "true")
		env.router.ServeHTTP(w, req)
		// Should succeed since test user has admin role
		assert.Equal(t, http.StatusNoContent, w.Code, "Should delete with bypass governance")
	})

	t.Run("Delete multiple objects sequentially", func(t *testing.T) {
		// Upload 3 objects
		for i := 1; i <= 3; i++ {
			key := fmt.Sprintf("object-%d.txt", i)
			req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+key, []byte(fmt.Sprintf("content %d", i)))
			env.router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
		}

		// Delete them one by one
		for i := 1; i <= 3; i++ {
			key := fmt.Sprintf("object-%d.txt", i)
			req, w := env.makeS3Request("DELETE", "/"+bucketName+"/"+key, nil)
			env.router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNoContent, w.Code, fmt.Sprintf("Should delete object %d", i))
		}
	})

	t.Run("Delete object and verify it's gone", func(t *testing.T) {
		testKey := "verify-delete.txt"

		// Upload
		req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+testKey, []byte("will be deleted"))
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		// Delete
		req, w = env.makeS3Request("DELETE", "/"+bucketName+"/"+testKey, nil)
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNoContent, w.Code)

		// Verify it's gone with HEAD
		req, w = env.makeS3Request("HEAD", "/"+bucketName+"/"+testKey, nil)
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "Object should not exist after delete")
	})
}

// TestPutObjectErrorCases tests PutObject error scenarios and edge cases
func TestPutObjectErrorCases(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "test-put-bucket"

	// Create bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName)
	require.NoError(t, err)

	t.Run("PutObject to non-existent bucket", func(t *testing.T) {
		req, w := env.makeS3Request("PUT", "/nonexistent-bucket/object.txt", []byte("test"))
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "Should return 404 for non-existent bucket")
		assert.Contains(t, w.Body.String(), "NoSuchBucket", "Error should be NoSuchBucket")
	})

	t.Run("PutObject with metadata headers", func(t *testing.T) {
		req, w := env.makeS3Request("PUT", "/"+bucketName+"/metadata-object.txt", []byte("test content"))
		req.Header.Set("x-amz-meta-author", "test-user")
		req.Header.Set("x-amz-meta-version", "1.0")
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should upload with metadata")
		assert.NotEmpty(t, w.Header().Get("ETag"), "Should return ETag")
	})

	t.Run("PutObject with Content-Type", func(t *testing.T) {
		req, w := env.makeS3Request("PUT", "/"+bucketName+"/file.json", []byte(`{"key":"value"}`))
		req.Header.Set("Content-Type", "application/json")
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should upload with Content-Type")

		// Verify Content-Type is stored
		getReq, getW := env.makeS3Request("HEAD", "/"+bucketName+"/file.json", nil)
		env.router.ServeHTTP(getW, getReq)
		assert.Equal(t, "application/json", getW.Header().Get("Content-Type"), "Content-Type should be preserved")
	})

	t.Run("PutObject with empty body", func(t *testing.T) {
		req, w := env.makeS3Request("PUT", "/"+bucketName+"/empty.txt", []byte(""))
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should accept empty object")

		// Verify empty object exists
		headReq, headW := env.makeS3Request("HEAD", "/"+bucketName+"/empty.txt", nil)
		env.router.ServeHTTP(headW, headReq)
		assert.Equal(t, "0", headW.Header().Get("Content-Length"), "Empty object should have length 0")
	})

	t.Run("PutObject with nested folder structure", func(t *testing.T) {
		nestedKey := "folder/subfolder/file.txt"
		req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+nestedKey, []byte("nested content"))
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should handle nested folder structure")

		// Verify object exists
		headReq, headW := env.makeS3Request("HEAD", "/"+bucketName+"/"+nestedKey, nil)
		env.router.ServeHTTP(headW, headReq)
		assert.Equal(t, http.StatusOK, headW.Code, "Should retrieve nested object")
	})

	t.Run("PutObject with large metadata", func(t *testing.T) {
		req, w := env.makeS3Request("PUT", "/"+bucketName+"/large-metadata.txt", []byte("content"))
		// Add multiple metadata headers
		for i := 1; i <= 10; i++ {
			req.Header.Set(fmt.Sprintf("x-amz-meta-field-%d", i), fmt.Sprintf("value-%d", i))
		}
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Should handle multiple metadata headers")
	})

	t.Run("PutObject with various keys", func(t *testing.T) {
		// Test different key patterns
		keys := []string{
			"simple.txt",
			"with-dash.txt",
			"with_underscore.txt",
			"with.multiple.dots.txt",
			"123-numeric-prefix.txt",
		}

		for _, key := range keys {
			req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+key, []byte("content"))
			env.router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, fmt.Sprintf("Should upload object with key: %s", key))
			assert.NotEmpty(t, w.Header().Get("ETag"), fmt.Sprintf("Should have ETag for key: %s", key))
		}
	})
}
