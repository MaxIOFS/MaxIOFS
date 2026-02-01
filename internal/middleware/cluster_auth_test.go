package middleware

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDB creates a test database with cluster_nodes table
func setupClusterAuthTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	// Create cluster_nodes table
	_, err = db.Exec(`
		CREATE TABLE cluster_nodes (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			endpoint TEXT NOT NULL,
			node_token TEXT NOT NULL,
			health_status TEXT NOT NULL DEFAULT 'healthy',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	require.NoError(t, err)

	return db
}

// insertTestNode inserts a test node into the database
func insertTestNode(t *testing.T, db *sql.DB, id, name, token, healthStatus string) {
	_, err := db.Exec(`
		INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status)
		VALUES (?, ?, ?, ?, ?)
	`, id, name, "http://"+name+":8080", token, healthStatus)
	require.NoError(t, err)
}

func TestClusterAuth_ValidSignature(t *testing.T) {
	db := setupClusterAuthTestDB(t)
	defer db.Close()

	// Insert a test node
	nodeID := "test-node-1"
	nodeToken := "test-token-123"
	insertTestNode(t, db, nodeID, "node-1", nodeToken, "healthy")

	middleware := NewClusterAuthMiddleware(db)

	// Create a test handler
	handlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		// Verify node ID was added to context
		nodeIDFromCtx := r.Context().Value("cluster_node_id")
		assert.Equal(t, nodeID, nodeIDFromCtx)
		w.WriteHeader(http.StatusOK)
	})

	// Create authenticated request
	req := httptest.NewRequest("GET", "/api/internal/cluster/test", nil)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := "test-nonce-123"
	signature := computeSignature(nodeToken, "GET", "/api/internal/cluster/test", timestamp, nonce)

	req.Header.Set("X-MaxIOFS-Node-ID", nodeID)
	req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
	req.Header.Set("X-MaxIOFS-Nonce", nonce)
	req.Header.Set("X-MaxIOFS-Signature", signature)

	rr := httptest.NewRecorder()
	handler := middleware.ClusterAuth(nextHandler)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, handlerCalled, "Next handler should have been called")
}

func TestClusterAuth_InvalidSignature(t *testing.T) {
	db := setupClusterAuthTestDB(t)
	defer db.Close()

	nodeID := "test-node-2"
	nodeToken := "test-token-456"
	insertTestNode(t, db, nodeID, "node-2", nodeToken, "healthy")

	middleware := NewClusterAuthMiddleware(db)

	handlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/internal/cluster/test", nil)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := "test-nonce-456"

	// Use wrong signature
	wrongSignature := "wrong-signature-hash"

	req.Header.Set("X-MaxIOFS-Node-ID", nodeID)
	req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
	req.Header.Set("X-MaxIOFS-Nonce", nonce)
	req.Header.Set("X-MaxIOFS-Signature", wrongSignature)

	rr := httptest.NewRecorder()
	handler := middleware.ClusterAuth(nextHandler)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, handlerCalled, "Next handler should not have been called")
	assert.Contains(t, rr.Body.String(), "Invalid signature")
}

func TestClusterAuth_MissingHeaders(t *testing.T) {
	db := setupClusterAuthTestDB(t)
	defer db.Close()

	middleware := NewClusterAuthMiddleware(db)

	testCases := []struct {
		name        string
		nodeID      string
		timestamp   string
		nonce       string
		signature   string
		expectedMsg string
	}{
		{
			name:        "Missing Node ID",
			nodeID:      "",
			timestamp:   fmt.Sprintf("%d", time.Now().Unix()),
			nonce:       "nonce",
			signature:   "sig",
			expectedMsg: "Missing authentication headers",
		},
		{
			name:        "Missing Timestamp",
			nodeID:      "node-1",
			timestamp:   "",
			nonce:       "nonce",
			signature:   "sig",
			expectedMsg: "Missing authentication headers",
		},
		{
			name:        "Missing Nonce",
			nodeID:      "node-1",
			timestamp:   fmt.Sprintf("%d", time.Now().Unix()),
			nonce:       "",
			signature:   "sig",
			expectedMsg: "Missing authentication headers",
		},
		{
			name:        "Missing Signature",
			nodeID:      "node-1",
			timestamp:   fmt.Sprintf("%d", time.Now().Unix()),
			nonce:       "nonce",
			signature:   "",
			expectedMsg: "Missing authentication headers",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handlerCalled := false
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
			})

			req := httptest.NewRequest("GET", "/test", nil)
			if tc.nodeID != "" {
				req.Header.Set("X-MaxIOFS-Node-ID", tc.nodeID)
			}
			if tc.timestamp != "" {
				req.Header.Set("X-MaxIOFS-Timestamp", tc.timestamp)
			}
			if tc.nonce != "" {
				req.Header.Set("X-MaxIOFS-Nonce", tc.nonce)
			}
			if tc.signature != "" {
				req.Header.Set("X-MaxIOFS-Signature", tc.signature)
			}

			rr := httptest.NewRecorder()
			handler := middleware.ClusterAuth(nextHandler)
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusUnauthorized, rr.Code)
			assert.Contains(t, rr.Body.String(), tc.expectedMsg)
			assert.False(t, handlerCalled, "Next handler should not have been called")
		})
	}
}

func TestClusterAuth_TimestampSkew(t *testing.T) {
	db := setupClusterAuthTestDB(t)
	defer db.Close()

	nodeID := "test-node-3"
	nodeToken := "test-token-789"
	insertTestNode(t, db, nodeID, "node-3", nodeToken, "healthy")

	middleware := NewClusterAuthMiddleware(db)

	testCases := []struct {
		name          string
		timestampFunc func() string
		shouldPass    bool
	}{
		{
			name: "Current timestamp (should pass)",
			timestampFunc: func() string {
				return fmt.Sprintf("%d", time.Now().Unix())
			},
			shouldPass: true,
		},
		{
			name: "4 minutes in past (should pass)",
			timestampFunc: func() string {
				return fmt.Sprintf("%d", time.Now().Add(-4*time.Minute).Unix())
			},
			shouldPass: true,
		},
		{
			name: "4 minutes in future (should pass)",
			timestampFunc: func() string {
				return fmt.Sprintf("%d", time.Now().Add(4*time.Minute).Unix())
			},
			shouldPass: true,
		},
		{
			name: "6 minutes in past (should fail)",
			timestampFunc: func() string {
				return fmt.Sprintf("%d", time.Now().Add(-6*time.Minute).Unix())
			},
			shouldPass: false,
		},
		{
			name: "6 minutes in future (should fail)",
			timestampFunc: func() string {
				return fmt.Sprintf("%d", time.Now().Add(6*time.Minute).Unix())
			},
			shouldPass: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handlerCalled := false
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest("GET", "/test", nil)
			timestamp := tc.timestampFunc()
			nonce := "nonce"
			signature := computeSignature(nodeToken, "GET", "/test", timestamp, nonce)

			req.Header.Set("X-MaxIOFS-Node-ID", nodeID)
			req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
			req.Header.Set("X-MaxIOFS-Nonce", nonce)
			req.Header.Set("X-MaxIOFS-Signature", signature)

			rr := httptest.NewRecorder()
			handler := middleware.ClusterAuth(nextHandler)
			handler.ServeHTTP(rr, req)

			if tc.shouldPass {
				assert.Equal(t, http.StatusOK, rr.Code)
				assert.True(t, handlerCalled)
			} else {
				assert.Equal(t, http.StatusUnauthorized, rr.Code)
				assert.Contains(t, rr.Body.String(), "Timestamp skew too large")
				assert.False(t, handlerCalled)
			}
		})
	}
}

func TestClusterAuth_InvalidTimestamp(t *testing.T) {
	db := setupClusterAuthTestDB(t)
	defer db.Close()

	middleware := NewClusterAuthMiddleware(db)

	handlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-MaxIOFS-Node-ID", "node-1")
	req.Header.Set("X-MaxIOFS-Timestamp", "not-a-number")
	req.Header.Set("X-MaxIOFS-Nonce", "nonce")
	req.Header.Set("X-MaxIOFS-Signature", "sig")

	rr := httptest.NewRecorder()
	handler := middleware.ClusterAuth(nextHandler)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "Invalid timestamp")
	assert.False(t, handlerCalled)
}

func TestClusterAuth_NodeNotFound(t *testing.T) {
	db := setupClusterAuthTestDB(t)
	defer db.Close()

	middleware := NewClusterAuthMiddleware(db)

	handlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	req := httptest.NewRequest("GET", "/test", nil)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := "nonce"
	signature := computeSignature("some-token", "GET", "/test", timestamp, nonce)

	req.Header.Set("X-MaxIOFS-Node-ID", "non-existent-node")
	req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
	req.Header.Set("X-MaxIOFS-Nonce", nonce)
	req.Header.Set("X-MaxIOFS-Signature", signature)

	rr := httptest.NewRecorder()
	handler := middleware.ClusterAuth(nextHandler)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "Node not found")
	assert.False(t, handlerCalled)
}

func TestClusterAuth_RemovedNode(t *testing.T) {
	db := setupClusterAuthTestDB(t)
	defer db.Close()

	// Insert a node with 'removed' health_status - this is the critical test
	// to ensure we're using health_status column correctly (not 'status')
	nodeID := "removed-node"
	nodeToken := "removed-token"
	insertTestNode(t, db, nodeID, "removed-node", nodeToken, "removed")

	middleware := NewClusterAuthMiddleware(db)

	handlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	req := httptest.NewRequest("GET", "/test", nil)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := "nonce"
	signature := computeSignature(nodeToken, "GET", "/test", timestamp, nonce)

	req.Header.Set("X-MaxIOFS-Node-ID", nodeID)
	req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
	req.Header.Set("X-MaxIOFS-Nonce", nonce)
	req.Header.Set("X-MaxIOFS-Signature", signature)

	rr := httptest.NewRecorder()
	handler := middleware.ClusterAuth(nextHandler)
	handler.ServeHTTP(rr, req)

	// Should fail because node is removed
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "Node not found")
	assert.False(t, handlerCalled, "Next handler should not be called for removed node")
}

func TestClusterAuth_HealthyNodeStatuses(t *testing.T) {
	db := setupClusterAuthTestDB(t)
	defer db.Close()

	// Test that nodes with various healthy statuses can authenticate
	testCases := []struct {
		healthStatus string
		shouldPass   bool
	}{
		{"healthy", true},
		{"degraded", true},
		{"unavailable", true},
		{"removed", false}, // Only 'removed' should be rejected
	}

	for _, tc := range testCases {
		t.Run("health_status="+tc.healthStatus, func(t *testing.T) {
			nodeID := "node-" + tc.healthStatus
			nodeToken := "token-" + tc.healthStatus
			insertTestNode(t, db, nodeID, nodeID, nodeToken, tc.healthStatus)

			middleware := NewClusterAuthMiddleware(db)

			handlerCalled := false
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest("GET", "/test", nil)
			timestamp := fmt.Sprintf("%d", time.Now().Unix())
			nonce := "nonce"
			signature := computeSignature(nodeToken, "GET", "/test", timestamp, nonce)

			req.Header.Set("X-MaxIOFS-Node-ID", nodeID)
			req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
			req.Header.Set("X-MaxIOFS-Nonce", nonce)
			req.Header.Set("X-MaxIOFS-Signature", signature)

			rr := httptest.NewRecorder()
			handler := middleware.ClusterAuth(nextHandler)
			handler.ServeHTTP(rr, req)

			if tc.shouldPass {
				assert.Equal(t, http.StatusOK, rr.Code)
				assert.True(t, handlerCalled, "Should allow authentication for "+tc.healthStatus)
			} else {
				assert.Equal(t, http.StatusUnauthorized, rr.Code)
				assert.False(t, handlerCalled, "Should reject authentication for "+tc.healthStatus)
			}
		})
	}
}

func TestClusterAuth_DifferentHTTPMethods(t *testing.T) {
	db := setupClusterAuthTestDB(t)
	defer db.Close()

	nodeID := "test-node-methods"
	nodeToken := "test-token-methods"
	insertTestNode(t, db, nodeID, "node-methods", nodeToken, "healthy")

	middleware := NewClusterAuthMiddleware(db)

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			handlerCalled := false
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusOK)
			})

			path := "/api/internal/cluster/test"
			req := httptest.NewRequest(method, path, nil)
			timestamp := fmt.Sprintf("%d", time.Now().Unix())
			nonce := fmt.Sprintf("nonce-%s", method)
			signature := computeSignature(nodeToken, method, path, timestamp, nonce)

			req.Header.Set("X-MaxIOFS-Node-ID", nodeID)
			req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
			req.Header.Set("X-MaxIOFS-Nonce", nonce)
			req.Header.Set("X-MaxIOFS-Signature", signature)

			rr := httptest.NewRecorder()
			handler := middleware.ClusterAuth(nextHandler)
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code, "Should authenticate "+method+" request")
			assert.True(t, handlerCalled)
		})
	}
}

func TestClusterAuth_DifferentPaths(t *testing.T) {
	db := setupClusterAuthTestDB(t)
	defer db.Close()

	nodeID := "test-node-paths"
	nodeToken := "test-token-paths"
	insertTestNode(t, db, nodeID, "node-paths", nodeToken, "healthy")

	middleware := NewClusterAuthMiddleware(db)

	paths := []string{
		"/api/internal/cluster/buckets",
		"/api/internal/cluster/tenant/123/storage",
		"/api/internal/cluster/sync/access-keys",
		"/api/internal/cluster/sync/bucket-permissions",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			handlerCalled := false
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest("GET", path, nil)
			timestamp := fmt.Sprintf("%d", time.Now().Unix())
			nonce := fmt.Sprintf("nonce-%d", time.Now().UnixNano())
			signature := computeSignature(nodeToken, "GET", path, timestamp, nonce)

			req.Header.Set("X-MaxIOFS-Node-ID", nodeID)
			req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
			req.Header.Set("X-MaxIOFS-Nonce", nonce)
			req.Header.Set("X-MaxIOFS-Signature", signature)

			rr := httptest.NewRecorder()
			handler := middleware.ClusterAuth(nextHandler)
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code, "Should authenticate request to "+path)
			assert.True(t, handlerCalled)
		})
	}
}

func TestComputeSignature(t *testing.T) {
	token := "test-secret-token"
	method := "POST"
	path := "/api/internal/cluster/test"
	timestamp := "1234567890"
	nonce := "random-nonce"

	// Compute signature twice with same inputs
	sig1 := computeSignature(token, method, path, timestamp, nonce)
	sig2 := computeSignature(token, method, path, timestamp, nonce)

	// Should be deterministic
	assert.Equal(t, sig1, sig2, "Signature should be deterministic")
	assert.NotEmpty(t, sig1, "Signature should not be empty")
	assert.Len(t, sig1, 64, "SHA256 hex signature should be 64 characters")

	// Different inputs should produce different signatures
	sig3 := computeSignature(token, "GET", path, timestamp, nonce)
	assert.NotEqual(t, sig1, sig3, "Different method should produce different signature")

	sig4 := computeSignature(token, method, "/different/path", timestamp, nonce)
	assert.NotEqual(t, sig1, sig4, "Different path should produce different signature")

	sig5 := computeSignature(token, method, path, "9999999999", nonce)
	assert.NotEqual(t, sig1, sig5, "Different timestamp should produce different signature")

	sig6 := computeSignature(token, method, path, timestamp, "different-nonce")
	assert.NotEqual(t, sig1, sig6, "Different nonce should produce different signature")
}

func TestSignRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/internal/cluster/test", nil)
	nodeID := "test-node"
	nodeToken := "test-token"

	SignRequest(req, nodeID, nodeToken)

	// Verify headers were added
	assert.Equal(t, nodeID, req.Header.Get("X-MaxIOFS-Node-ID"))
	assert.NotEmpty(t, req.Header.Get("X-MaxIOFS-Timestamp"))
	assert.NotEmpty(t, req.Header.Get("X-MaxIOFS-Nonce"))
	assert.NotEmpty(t, req.Header.Get("X-MaxIOFS-Signature"))

	// Verify signature is valid
	timestamp := req.Header.Get("X-MaxIOFS-Timestamp")
	nonce := req.Header.Get("X-MaxIOFS-Nonce")
	signature := req.Header.Get("X-MaxIOFS-Signature")

	expectedSignature := computeSignature(nodeToken, "GET", "/api/internal/cluster/test", timestamp, nonce)
	assert.Equal(t, expectedSignature, signature, "Signature should match expected value")
}

func TestSignRequest_PreservesExistingHeaders(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Custom-Header", "custom-value")

	SignRequest(req, "node-1", "token-1")

	// Verify existing headers are preserved
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "custom-value", req.Header.Get("X-Custom-Header"))

	// Verify auth headers were added
	assert.NotEmpty(t, req.Header.Get("X-MaxIOFS-Node-ID"))
	assert.NotEmpty(t, req.Header.Get("X-MaxIOFS-Signature"))
}

func TestGetNodeToken(t *testing.T) {
	db := setupClusterAuthTestDB(t)
	defer db.Close()

	nodeID := "test-node-token"
	nodeToken := "secret-token-12345"
	insertTestNode(t, db, nodeID, "node-token", nodeToken, "healthy")

	middleware := NewClusterAuthMiddleware(db)

	// Test successful retrieval
	token, err := middleware.getNodeToken(context.Background(), nodeID)
	assert.NoError(t, err)
	assert.Equal(t, nodeToken, token)

	// Test non-existent node
	_, err = middleware.getNodeToken(context.Background(), "non-existent")
	assert.Error(t, err)

	// Test removed node (should not be found due to health_status filter)
	removedNodeID := "removed-node-token"
	insertTestNode(t, db, removedNodeID, "removed", "removed-token", "removed")
	_, err = middleware.getNodeToken(context.Background(), removedNodeID)
	assert.Error(t, err, "Should not find token for removed node")
}

func TestGenerateNonce(t *testing.T) {
	// Generate multiple nonces
	nonces := make(map[string]bool)
	for i := 0; i < 100; i++ {
		nonce := generateNonce()
		assert.NotEmpty(t, nonce)

		// Check for uniqueness
		assert.False(t, nonces[nonce], "Nonce should be unique")
		nonces[nonce] = true

		// Small delay to ensure different timestamps
		time.Sleep(time.Microsecond)
	}

	assert.Equal(t, 100, len(nonces), "All nonces should be unique")
}
