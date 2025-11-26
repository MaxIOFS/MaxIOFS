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

	// Register S3 API routes (bucket operations)
	router.HandleFunc("/{bucket}", handler.CreateBucket).Methods("PUT")
	router.HandleFunc("/{bucket}", handler.DeleteBucket).Methods("DELETE")
	router.HandleFunc("/{bucket}", handler.HeadBucket).Methods("HEAD")
	router.HandleFunc("/", handler.ListBuckets).Methods("GET")

	// Object operations
	router.HandleFunc("/{bucket}/{object:.+}", handler.PutObject).Methods("PUT")
	router.HandleFunc("/{bucket}/{object:.+}", handler.GetObject).Methods("GET")
	router.HandleFunc("/{bucket}/{object:.+}", handler.DeleteObject).Methods("DELETE")
	router.HandleFunc("/{bucket}/{object:.+}", handler.HeadObject).Methods("HEAD")
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
		// Register policy route
		env.router.HandleFunc("/{bucket}", env.handler.PutBucketPolicy).Methods("PUT").Queries("policy", "")

		req, w := env.makeS3Request("PUT", "/"+bucketName+"?policy", policyJSON)
		req.Header.Set("Content-Type", "application/json")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code, "Should set bucket policy")
	})

	t.Run("Get bucket policy", func(t *testing.T) {
		// Register policy route
		env.router.HandleFunc("/{bucket}", env.handler.GetBucketPolicy).Methods("GET").Queries("policy", "")

		req, w := env.makeS3Request("GET", "/"+bucketName+"?policy", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get bucket policy")
		assert.Contains(t, w.Body.String(), "AllowPublicRead", "Should contain policy statement")
	})

	t.Run("Delete bucket policy", func(t *testing.T) {
		// Register policy route
		env.router.HandleFunc("/{bucket}", env.handler.DeleteBucketPolicy).Methods("DELETE").Queries("policy", "")

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
		// Register lifecycle route
		env.router.HandleFunc("/{bucket}", env.handler.PutBucketLifecycle).Methods("PUT").Queries("lifecycle", "")

		req, w := env.makeS3Request("PUT", "/"+bucketName+"?lifecycle", []byte(lifecycleXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should set bucket lifecycle")
	})

	t.Run("Get bucket lifecycle", func(t *testing.T) {
		// Register lifecycle route
		env.router.HandleFunc("/{bucket}", env.handler.GetBucketLifecycle).Methods("GET").Queries("lifecycle", "")

		req, w := env.makeS3Request("GET", "/"+bucketName+"?lifecycle", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get bucket lifecycle")
		assert.Contains(t, w.Body.String(), "delete-old-logs", "Should contain lifecycle rule")
		assert.Contains(t, w.Body.String(), "<Days>30</Days>", "Should contain expiration days")
	})

	t.Run("Delete bucket lifecycle", func(t *testing.T) {
		// Register lifecycle route
		env.router.HandleFunc("/{bucket}", env.handler.DeleteBucketLifecycle).Methods("DELETE").Queries("lifecycle", "")

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
		// Register CORS route
		env.router.HandleFunc("/{bucket}", env.handler.PutBucketCORS).Methods("PUT").Queries("cors", "")

		req, w := env.makeS3Request("PUT", "/"+bucketName+"?cors", []byte(corsXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should set bucket CORS")
	})

	t.Run("Get bucket CORS", func(t *testing.T) {
		// Register CORS route
		env.router.HandleFunc("/{bucket}", env.handler.GetBucketCORS).Methods("GET").Queries("cors", "")

		req, w := env.makeS3Request("GET", "/"+bucketName+"?cors", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get bucket CORS")
		assert.Contains(t, w.Body.String(), "https://example.com", "Should contain allowed origin")
		assert.Contains(t, w.Body.String(), "MaxAgeSeconds", "Should contain max age")
	})

	t.Run("Delete bucket CORS", func(t *testing.T) {
		// Register CORS route
		env.router.HandleFunc("/{bucket}", env.handler.DeleteBucketCORS).Methods("DELETE").Queries("cors", "")

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
		// Register tagging route
		env.router.HandleFunc("/{bucket}/{object:.+}", env.handler.PutObjectTagging).Methods("PUT").Queries("tagging", "")

		req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey+"?tagging", []byte(taggingXML))
		req.Header.Set("Content-Type", "application/xml")
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should set object tagging")
	})

	t.Run("Get object tagging", func(t *testing.T) {
		// Register tagging route
		env.router.HandleFunc("/{bucket}/{object:.+}", env.handler.GetObjectTagging).Methods("GET").Queries("tagging", "")

		req, w := env.makeS3Request("GET", "/"+bucketName+"/"+objectKey+"?tagging", nil)
		env.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should get object tagging")
		assert.Contains(t, w.Body.String(), "Environment", "Should contain Environment tag")
		assert.Contains(t, w.Body.String(), "Production", "Should contain Production value")
	})

	t.Run("Delete object tagging", func(t *testing.T) {
		// Register tagging route
		env.router.HandleFunc("/{bucket}/{object:.+}", env.handler.DeleteObjectTagging).Methods("DELETE").Queries("tagging", "")

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
		// Register multipart route
		env.router.HandleFunc("/{bucket}/{object:.+}", env.handler.CreateMultipartUpload).Methods("POST").Queries("uploads", "")

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
		// Register upload part route
		env.router.HandleFunc("/{bucket}/{object:.+}", env.handler.UploadPart).Methods("PUT").Queries("partNumber", "{partNumber}", "uploadId", "{uploadId}")

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
