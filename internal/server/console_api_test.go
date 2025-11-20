package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/settings"
	"github.com/maxiofs/maxiofs/internal/share"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestServer creates a test server with temporary database
func setupTestServer(t *testing.T) (*Server, string, func()) {
	// Create temp directory for test data
	tmpDir, err := os.MkdirTemp("", "maxiofs-server-test-*")
	require.NoError(t, err)

	// Create test configuration
	cfg := &config.Config{
		Listen:           "localhost:0",
		ConsoleListen:    "localhost:0",
		DataDir:          tmpDir,
		PublicAPIURL:     "http://localhost:8080",
		PublicConsoleURL: "http://localhost:8081",
		Storage: config.StorageConfig{
			Backend: "filesystem",
			Root:    tmpDir + "/objects",
		},
		Auth: config.AuthConfig{
			EnableAuth: true,
			JWTSecret:  "test-secret-key-for-testing-only-minimum-32-chars",
		},
		Audit: config.AuditConfig{
			Enable: true,
			DBPath: tmpDir + "/audit.db",
		},
		Metrics: config.MetricsConfig{
			Enable:   true,
			Path:     tmpDir + "/metrics",
			Interval: 60,
		},
	}

	// Initialize storage backend
	storageBackend, err := storage.NewBackend(cfg.Storage)
	require.NoError(t, err)

	// Initialize metadata store
	metadataStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           cfg.DataDir,
		SyncWrites:        false,
		CompactionEnabled: true,
		Logger:            logrus.StandardLogger(),
	})
	require.NoError(t, err)

	// Initialize managers
	bucketManager := bucket.NewManager(storageBackend, metadataStore)
	objectManager := object.NewManager(storageBackend, metadataStore, cfg.Storage)

	authManager := auth.NewManager(cfg.Auth, cfg.DataDir)

	// Get SQLite database from auth manager
	db, ok := authManager.GetDB().(*sql.DB)
	if !ok {
		t.Fatal("failed to get SQLite database from auth manager")
	}

	// Initialize settings manager
	settingsManager, err := settings.NewManager(db, logrus.StandardLogger())
	require.NoError(t, err)

	// Initialize audit manager
	var auditManager *audit.Manager
	if cfg.Audit.Enable {
		auditStore, err := audit.NewSQLiteStore(cfg.Audit.DBPath, logrus.StandardLogger())
		require.NoError(t, err)
		auditManager = audit.NewManager(auditStore, logrus.StandardLogger())
	}

	metricsManager := metrics.NewManagerWithStore(cfg.Metrics, cfg.DataDir, metadataStore)

	// Initialize share manager (pass nil if it needs a different store)
	var shareManager share.Manager

	// Create server instance
	server := &Server{
		config:          cfg,
		storageBackend:  storageBackend,
		metadataStore:   metadataStore,
		bucketManager:   bucketManager,
		objectManager:   objectManager,
		authManager:     authManager,
		auditManager:    auditManager,
		metricsManager:  metricsManager,
		settingsManager: settingsManager,
		shareManager:    shareManager,
		startTime:       time.Now(),
		version:         "test",
		commit:          "test",
		buildDate:       "test",
	}

	// Cleanup function
	cleanup := func() {
		if metadataStore != nil {
			metadataStore.Close()
		}
		time.Sleep(100 * time.Millisecond) // Wait for cleanup
		os.RemoveAll(tmpDir)
	}

	return server, tmpDir, cleanup
}

// createTestUser creates a test user and returns the user and a JWT token
func createTestUser(t *testing.T, authManager auth.Manager, username, password string, roles []string) (*auth.User, string) {
	ctx := context.Background()

	user := &auth.User{
		ID:          username + "-id",
		Username:    username,
		Password:    password,
		DisplayName: username + " Test User",
		Email:       username + "@example.com",
		Status:      auth.UserStatusActive,
		Roles:       roles,
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}

	err := authManager.CreateUser(ctx, user)
	require.NoError(t, err)

	// Generate JWT token
	token, err := authManager.GenerateJWT(ctx, user)
	require.NoError(t, err)

	return user, token
}

// LoginResponse represents the response from handleLogin
type LoginResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token"`
	User    struct {
		ID               string   `json:"id"`
		Username         string   `json:"username"`
		DisplayName      string   `json:"displayName"`
		Email            string   `json:"email"`
		Status           string   `json:"status"`
		Roles            []string `json:"roles"`
		TwoFactorEnabled bool     `json:"twoFactorEnabled"`
		CreatedAt        int64    `json:"createdAt"`
	} `json:"user"`
}

// getAdminToken logs in with default admin user and returns a valid JWT token
func getAdminToken(t *testing.T, server *Server) string {
	loginBody, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "admin",
	})
	loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")

	loginRR := httptest.NewRecorder()
	server.handleLogin(loginRR, loginReq)

	require.Equal(t, http.StatusOK, loginRR.Code, "Login should succeed")

	var loginResponse LoginResponse
	err := json.NewDecoder(loginRR.Body).Decode(&loginResponse)
	require.NoError(t, err, "Should decode login response")
	require.True(t, loginResponse.Success, "Login response should be successful")
	require.NotEmpty(t, loginResponse.Token, "Token should not be empty")

	return loginResponse.Token
}

// TestHandleLogin tests the login endpoint with valid credentials
func TestHandleLogin(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Test valid login with default admin
	body, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "admin",
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleLogin(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response body
	var response LoginResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
	assert.NotEmpty(t, response.Token)
	assert.Equal(t, "admin", response.User.Username)
}

// TestHandleLoginInvalidCredentials tests login with invalid credentials
func TestHandleLoginInvalidCredentials(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Test invalid password
	body, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "wrongpassword",
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleLogin(rr, req)

	// Check status code
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	// Check response
	var response APIResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.False(t, response.Success)
}

// TestHandleGetCurrentUser tests the /auth/me endpoint
func TestHandleGetCurrentUser(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token
	token := getAdminToken(t, server)

	// Validate the JWT and get the user
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err, "Token should be valid")
	require.NotNil(t, user, "User should not be nil")

	// Create request with user in context
	req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleGetCurrentUser(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)

	userData, ok := response.Data.(map[string]interface{})
	if ok {
		assert.Equal(t, "admin", userData["username"])
	}
}

// TestHandleListUsers tests the /users endpoint
func TestHandleListUsers(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create request with user in context
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleListUsers(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)

	// Should have at least the admin user
	users, ok := response.Data.([]interface{})
	if ok {
		assert.Greater(t, len(users), 0, "Should have at least one user")
	}
}

// TestHandleCreateUser tests the POST /users endpoint
func TestHandleCreateUser(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create test user data
	newUser := map[string]interface{}{
		"username":    "testuser",
		"password":    "TestPassword123!",
		"displayName": "Test User",
		"email":       "test@example.com",
		"roles":       []string{"user"},
	}

	body, _ := json.Marshal(newUser)
	req := httptest.NewRequest("POST", "/api/v1/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add user to context
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleCreateUser(rr, req)

	// Check status code (200 OK for successful creation)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleListBuckets tests the GET /buckets endpoint
func TestHandleListBuckets(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create request with user in context
	req := httptest.NewRequest("GET", "/api/v1/buckets", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleListBuckets(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response - should be empty array initially
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleCreateBucket tests the POST /buckets endpoint
func TestHandleCreateBucket(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create test bucket data
	newBucket := map[string]interface{}{
		"name": "test-bucket",
	}

	body, _ := json.Marshal(newBucket)
	req := httptest.NewRequest("POST", "/api/v1/buckets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add user to context
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleCreateBucket(rr, req)

	// Check status code (200 OK for successful creation)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleAPIHealth tests the /health endpoint
func TestHandleAPIHealth(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Create request
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	// Call handler
	server.handleAPIHealth(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)

	data := response.Data.(map[string]interface{})
	assert.Equal(t, "healthy", data["status"])
}

// TestHandleGetMetrics tests the /metrics endpoint
func TestHandleGetMetrics(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create request with user in context
	req := httptest.NewRequest("GET", "/api/v1/metrics", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleGetMetrics(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)

	// Verify metrics data structure
	metricsData, ok := response.Data.(map[string]interface{})
	if ok {
		assert.Contains(t, metricsData, "totalBuckets")
		assert.Contains(t, metricsData, "totalObjects")
		assert.Contains(t, metricsData, "totalSize")
	}
}
