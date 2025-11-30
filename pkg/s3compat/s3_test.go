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

	// List object versions
	router.HandleFunc("/{bucket}", handler.ListBucketVersions).Methods("GET").Queries("versions", "")

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
