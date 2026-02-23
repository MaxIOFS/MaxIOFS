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

	"github.com/gorilla/mux"
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
	metadataStore, err := metadata.NewPebbleStore(metadata.PebbleOptions{		DataDir: cfg.DataDir,
		Logger:  logrus.StandardLogger(),})
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

	// Create HTTP servers (required for setupRoutes)
	httpServer := &http.Server{
		Addr:         cfg.Listen,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	consoleServer := &http.Server{
		Addr:         cfg.ConsoleListen,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Create server instance
	server := &Server{
		config:          cfg,
		httpServer:      httpServer,
		consoleServer:   consoleServer,
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

// TestHandleSetup2FA tests the POST /auth/2fa/setup endpoint
func TestHandleSetup2FA(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create request with user in context
	req := httptest.NewRequest("POST", "/api/v1/auth/2fa/setup", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleSetup2FA(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)

	// Check data contains 2FA setup information
	data, ok := response.Data.(map[string]interface{})
	if ok {
		assert.Contains(t, data, "secret")
		assert.Contains(t, data, "qr_code")
		assert.Contains(t, data, "url")
		assert.NotEmpty(t, data["secret"])
	}
}

// TestHandleGet2FAStatus tests the GET /auth/2fa/status endpoint
func TestHandleGet2FAStatus(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create request with user in context
	req := httptest.NewRequest("GET", "/api/v1/auth/2fa/status", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleGet2FAStatus(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)

	// Check data contains enabled status
	data, ok := response.Data.(map[string]interface{})
	if ok {
		assert.Contains(t, data, "enabled")
		assert.False(t, data["enabled"].(bool)) // Should be false initially
	}
}

// TestHandleDisable2FA tests the POST /auth/2fa/disable endpoint
func TestHandleDisable2FA(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create empty request body
	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest("POST", "/api/v1/auth/2fa/disable", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleDisable2FA(rr, req)

	// Check status code - should succeed even if 2FA not enabled
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleListTenants tests the GET /tenants endpoint
func TestHandleListTenants(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create request with user in context
	req := httptest.NewRequest("GET", "/api/v1/tenants", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleListTenants(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleCreateTenant tests the POST /tenants endpoint
func TestHandleCreateTenant(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create test tenant data
	newTenant := map[string]interface{}{
		"name":              "Test Tenant",
		"max_storage_bytes": 10737418240, // 10GB
		"max_buckets":       10,
	}

	body, _ := json.Marshal(newTenant)
	req := httptest.NewRequest("POST", "/api/v1/tenants", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add user to context
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleCreateTenant(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleGetTenant tests the GET /tenants/{tenant} endpoint
func TestHandleGetTenant(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// First create a tenant
	newTenant := map[string]interface{}{
		"name":              "Get Test Tenant",
		"max_storage_bytes": 10737418240, // 10GB
		"max_buckets":       5,
	}

	body, _ := json.Marshal(newTenant)
	createReq := httptest.NewRequest("POST", "/api/v1/tenants", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	ctxWithUser := context.WithValue(createReq.Context(), "user", user)
	createReq = createReq.WithContext(ctxWithUser)

	createRR := httptest.NewRecorder()
	server.handleCreateTenant(createRR, createReq)
	require.Equal(t, http.StatusOK, createRR.Code)

	var createResponse APIResponse
	err = json.NewDecoder(createRR.Body).Decode(&createResponse)
	require.NoError(t, err)
	require.True(t, createResponse.Success)

	// Extract tenant ID from response
	tenantData, ok := createResponse.Data.(map[string]interface{})
	require.True(t, ok)
	tenantID, ok := tenantData["id"].(string)
	require.True(t, ok)

	// Now get the tenant
	getReq := httptest.NewRequest("GET", "/api/v1/tenants/"+tenantID, nil)
	ctxWithUser = context.WithValue(getReq.Context(), "user", user)
	getReq = getReq.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	getReq = mux.SetURLVars(getReq, map[string]string{"tenant": tenantID})

	getRR := httptest.NewRecorder()
	server.handleGetTenant(getRR, getReq)

	// Check status code
	assert.Equal(t, http.StatusOK, getRR.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(getRR.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleDeleteTenant tests the DELETE /tenants/{tenant} endpoint
func TestHandleDeleteTenant(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// First create a tenant
	newTenant := map[string]interface{}{
		"name":              "Delete Test Tenant",
		"max_storage_bytes": 10737418240,
		"max_buckets":       5,
	}

	body, _ := json.Marshal(newTenant)
	createReq := httptest.NewRequest("POST", "/api/v1/tenants", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	ctxWithUser := context.WithValue(createReq.Context(), "user", user)
	createReq = createReq.WithContext(ctxWithUser)

	createRR := httptest.NewRecorder()
	server.handleCreateTenant(createRR, createReq)
	require.Equal(t, http.StatusOK, createRR.Code)

	var createResponse APIResponse
	err = json.NewDecoder(createRR.Body).Decode(&createResponse)
	require.NoError(t, err)
	require.True(t, createResponse.Success)

	// Extract tenant ID from response
	tenantData, ok := createResponse.Data.(map[string]interface{})
	require.True(t, ok)
	tenantID, ok := tenantData["id"].(string)
	require.True(t, ok)

	// Now delete the tenant
	deleteReq := httptest.NewRequest("DELETE", "/api/v1/tenants/"+tenantID, nil)
	ctxWithUser = context.WithValue(deleteReq.Context(), "user", user)
	deleteReq = deleteReq.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	deleteReq = mux.SetURLVars(deleteReq, map[string]string{"tenant": tenantID})

	deleteRR := httptest.NewRecorder()
	server.handleDeleteTenant(deleteRR, deleteReq)

	// Check status code - delete returns 204 No Content
	assert.Equal(t, http.StatusNoContent, deleteRR.Code)
}

// TestHandleListAccessKeys tests the GET /users/{user}/access-keys endpoint
func TestHandleListAccessKeys(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create request with user in context
	req := httptest.NewRequest("GET", "/api/v1/users/"+user.ID+"/access-keys", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	req = mux.SetURLVars(req, map[string]string{"user": user.ID})

	rr := httptest.NewRecorder()
	server.handleListAccessKeys(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleCreateAccessKey tests the POST /users/{user}/access-keys endpoint
func TestHandleCreateAccessKey(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create request body
	body, _ := json.Marshal(map[string]interface{}{
		"description": "Test Access Key",
	})

	req := httptest.NewRequest("POST", "/api/v1/users/"+user.ID+"/access-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add user to context
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	req = mux.SetURLVars(req, map[string]string{"user": user.ID})

	rr := httptest.NewRecorder()
	server.handleCreateAccessKey(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)

	// Verify access key and secret key are present
	keyData, ok := response.Data.(map[string]interface{})
	if ok {
		assert.Contains(t, keyData, "accessKey")
		assert.Contains(t, keyData, "secretKey")
	}
}

// TestHandleDeleteAccessKey tests the DELETE /users/{user}/access-keys/{accessKey} endpoint
func TestHandleDeleteAccessKey(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// First create an access key
	body, _ := json.Marshal(map[string]interface{}{
		"description": "Test Key to Delete",
	})

	createReq := httptest.NewRequest("POST", "/api/v1/users/"+user.ID+"/access-keys", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	ctxWithUser := context.WithValue(createReq.Context(), "user", user)
	createReq = createReq.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	createReq = mux.SetURLVars(createReq, map[string]string{"user": user.ID})

	createRR := httptest.NewRecorder()
	server.handleCreateAccessKey(createRR, createReq)
	require.Equal(t, http.StatusOK, createRR.Code)

	var createResponse APIResponse
	err = json.NewDecoder(createRR.Body).Decode(&createResponse)
	require.NoError(t, err)
	require.True(t, createResponse.Success)

	// Extract access key from response
	keyData, ok := createResponse.Data.(map[string]interface{})
	require.True(t, ok)
	accessKey, ok := keyData["accessKey"].(string)
	require.True(t, ok)

	// Now delete the access key
	deleteReq := httptest.NewRequest("DELETE", "/api/v1/users/"+user.ID+"/access-keys/"+accessKey, nil)
	ctxWithUser = context.WithValue(deleteReq.Context(), "user", user)
	deleteReq = deleteReq.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	deleteReq = mux.SetURLVars(deleteReq, map[string]string{"user": user.ID, "accessKey": accessKey})

	deleteRR := httptest.NewRecorder()
	server.handleDeleteAccessKey(deleteRR, deleteReq)

	// Check status code
	assert.Equal(t, http.StatusOK, deleteRR.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(deleteRR.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleGetUser tests the GET /users/{user} endpoint
func TestHandleGetUser(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Get the admin user
	req := httptest.NewRequest("GET", "/api/v1/users/"+user.ID, nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	req = mux.SetURLVars(req, map[string]string{"user": user.ID})

	rr := httptest.NewRecorder()
	server.handleGetUser(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleUpdateUser tests the PUT /users/{user} endpoint
func TestHandleUpdateUser(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create a test user first
	testUser := map[string]interface{}{
		"username":    "updatetest",
		"password":    "UpdateTest123!",
		"displayName": "Update Test User",
		"email":       "updatetest@example.com",
		"roles":       []string{"user"},
	}

	body, _ := json.Marshal(testUser)
	createReq := httptest.NewRequest("POST", "/api/v1/users", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	ctxWithUser := context.WithValue(createReq.Context(), "user", user)
	createReq = createReq.WithContext(ctxWithUser)

	createRR := httptest.NewRecorder()
	server.handleCreateUser(createRR, createReq)
	require.Equal(t, http.StatusOK, createRR.Code)

	var createResponse APIResponse
	err = json.NewDecoder(createRR.Body).Decode(&createResponse)
	require.NoError(t, err)
	require.True(t, createResponse.Success)

	// Extract user ID
	userData, ok := createResponse.Data.(map[string]interface{})
	require.True(t, ok)
	userID, ok := userData["id"].(string)
	require.True(t, ok)

	// Update the user
	updateData := map[string]interface{}{
		"displayName": "Updated Display Name",
		"email":       "updated@example.com",
	}

	updateBody, _ := json.Marshal(updateData)
	updateReq := httptest.NewRequest("PUT", "/api/v1/users/"+userID, bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	ctxWithUser = context.WithValue(updateReq.Context(), "user", user)
	updateReq = updateReq.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	updateReq = mux.SetURLVars(updateReq, map[string]string{"user": userID})

	updateRR := httptest.NewRecorder()
	server.handleUpdateUser(updateRR, updateReq)

	// Check status code
	assert.Equal(t, http.StatusOK, updateRR.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(updateRR.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleDeleteUser tests the DELETE /users/{user} endpoint
func TestHandleDeleteUser(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create a test user first
	testUser := map[string]interface{}{
		"username":    "deletetest",
		"password":    "DeleteTest123!",
		"displayName": "Delete Test User",
		"email":       "deletetest@example.com",
		"roles":       []string{"user"},
	}

	body, _ := json.Marshal(testUser)
	createReq := httptest.NewRequest("POST", "/api/v1/users", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	ctxWithUser := context.WithValue(createReq.Context(), "user", user)
	createReq = createReq.WithContext(ctxWithUser)

	createRR := httptest.NewRecorder()
	server.handleCreateUser(createRR, createReq)
	require.Equal(t, http.StatusOK, createRR.Code)

	var createResponse APIResponse
	err = json.NewDecoder(createRR.Body).Decode(&createResponse)
	require.NoError(t, err)
	require.True(t, createResponse.Success)

	// Extract user ID
	userData, ok := createResponse.Data.(map[string]interface{})
	require.True(t, ok)
	userID, ok := userData["id"].(string)
	require.True(t, ok)

	// Delete the user
	deleteReq := httptest.NewRequest("DELETE", "/api/v1/users/"+userID, nil)
	ctxWithUser = context.WithValue(deleteReq.Context(), "user", user)
	deleteReq = deleteReq.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	deleteReq = mux.SetURLVars(deleteReq, map[string]string{"user": userID})

	deleteRR := httptest.NewRecorder()
	server.handleDeleteUser(deleteRR, deleteReq)

	// Check status code - delete returns 204 No Content
	assert.Equal(t, http.StatusNoContent, deleteRR.Code)
}

// TestHandleChangePassword tests the PUT /users/{user}/password endpoint
func TestHandleChangePassword(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Change password data
	passwordData := map[string]interface{}{
		"currentPassword": "admin",
		"newPassword":     "NewAdminPassword123!",
	}

	body, _ := json.Marshal(passwordData)
	req := httptest.NewRequest("PUT", "/api/v1/users/"+user.ID+"/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	req = mux.SetURLVars(req, map[string]string{"user": user.ID})

	rr := httptest.NewRecorder()
	server.handleChangePassword(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleUpdateUserPreferences tests the PATCH /users/{user}/preferences endpoint
func TestHandleUpdateUserPreferences(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Update preferences
	preferencesData := map[string]interface{}{
		"themePreference":    "dark",
		"languagePreference": "es",
	}

	body, _ := json.Marshal(preferencesData)
	req := httptest.NewRequest("PATCH", "/api/v1/users/"+user.ID+"/preferences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	req = mux.SetURLVars(req, map[string]string{"user": user.ID})

	rr := httptest.NewRecorder()
	server.handleUpdateUserPreferences(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleGetBucket tests the GET /buckets/{bucket} endpoint
func TestHandleGetBucket(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create a test bucket first
	newBucket := map[string]interface{}{
		"name": "get-test-bucket",
	}

	body, _ := json.Marshal(newBucket)
	createReq := httptest.NewRequest("POST", "/api/v1/buckets", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	ctxWithUser := context.WithValue(createReq.Context(), "user", user)
	createReq = createReq.WithContext(ctxWithUser)

	createRR := httptest.NewRecorder()
	server.handleCreateBucket(createRR, createReq)
	require.Equal(t, http.StatusOK, createRR.Code)

	// Get the bucket
	getReq := httptest.NewRequest("GET", "/api/v1/buckets/get-test-bucket", nil)
	ctxWithUser = context.WithValue(getReq.Context(), "user", user)
	getReq = getReq.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	getReq = mux.SetURLVars(getReq, map[string]string{"bucket": "get-test-bucket"})

	getRR := httptest.NewRecorder()
	server.handleGetBucket(getRR, getReq)

	// Check status code
	assert.Equal(t, http.StatusOK, getRR.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(getRR.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleDeleteBucket tests the DELETE /buckets/{bucket} endpoint
func TestHandleDeleteBucket(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// Create a test bucket first
	newBucket := map[string]interface{}{
		"name": "delete-test-bucket",
	}

	body, _ := json.Marshal(newBucket)
	createReq := httptest.NewRequest("POST", "/api/v1/buckets", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	ctxWithUser := context.WithValue(createReq.Context(), "user", user)
	createReq = createReq.WithContext(ctxWithUser)

	createRR := httptest.NewRecorder()
	server.handleCreateBucket(createRR, createReq)
	require.Equal(t, http.StatusOK, createRR.Code)

	// Delete the bucket
	deleteReq := httptest.NewRequest("DELETE", "/api/v1/buckets/delete-test-bucket", nil)
	ctxWithUser = context.WithValue(deleteReq.Context(), "user", user)
	deleteReq = deleteReq.WithContext(ctxWithUser)

	// Set mux vars for path parameters
	deleteReq = mux.SetURLVars(deleteReq, map[string]string{"bucket": "delete-test-bucket"})

	deleteRR := httptest.NewRecorder()
	server.handleDeleteBucket(deleteRR, deleteReq)

	// Check status code - delete returns 204 No Content
	assert.Equal(t, http.StatusNoContent, deleteRR.Code)
}

// TestHandleListSettings tests the GET /settings endpoint
func TestHandleListSettings(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// List settings
	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleListSettings(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}

// TestHandleListAuditLogs tests the GET /audit-logs endpoint
func TestHandleListAuditLogs(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get a valid admin token and validate it
	token := getAdminToken(t, server)
	user, err := server.authManager.ValidateJWT(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, user)

	// List audit logs
	req := httptest.NewRequest("GET", "/api/v1/audit-logs", nil)
	ctxWithUser := context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctxWithUser)

	rr := httptest.NewRecorder()
	server.handleListAuditLogs(rr, req)

	// Check status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check response
	var response APIResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
}
