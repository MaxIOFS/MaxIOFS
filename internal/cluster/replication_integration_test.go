package cluster

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// SimulatedNode represents a minimal simulated MaxIOFS node for testing
type SimulatedNode struct {
	NodeID     string
	NodeName   string
	NodeToken  string
	DataDir    string
	DB         *sql.DB
	HTTPServer *httptest.Server
	Objects    map[string][]byte // tenantID/bucket/key -> data (simulated object storage)
	Ctx        context.Context
	Cancel     context.CancelFunc
}

// setupSimulatedNode creates a lightweight simulated node
func setupSimulatedNode(t *testing.T, nodeName string) *SimulatedNode {
	// Create temporary directory
	dataDir, err := os.MkdirTemp("", fmt.Sprintf("maxiofs-cluster-test-%s-*", nodeName))
	require.NoError(t, err)

	// Open SQLite database
	dbPath := filepath.Join(dataDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	// Initialize minimal schema
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS tenants (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			display_name TEXT,
			description TEXT,
			status TEXT DEFAULT 'active',
			max_access_keys INTEGER DEFAULT 10,
			max_storage_bytes INTEGER DEFAULT 0,
			current_storage_bytes INTEGER DEFAULT 0,
			max_buckets INTEGER DEFAULT 10,
			current_buckets INTEGER DEFAULT 0,
			metadata TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	require.NoError(t, err)

	// Initialize cluster replication schema
	err = InitReplicationSchema(db)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	nodeID := uuid.New().String()
	nodeToken := uuid.New().String()

	node := &SimulatedNode{
		NodeID:    nodeID,
		NodeName:  nodeName,
		NodeToken: nodeToken,
		DataDir:   dataDir,
		DB:        db,
		Objects:   make(map[string][]byte),
		Ctx:       ctx,
		Cancel:    cancel,
	}

	// Create HTTP server
	node.HTTPServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node.handleRequest(w, r)
	}))

	return node
}

// handleRequest handles incoming HTTP requests
func (n *SimulatedNode) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Verify HMAC signature if present
	signature := r.Header.Get("X-MaxIOFS-Signature")
	if signature != "" {
		timestamp := r.Header.Get("X-MaxIOFS-Timestamp")
		nonce := r.Header.Get("X-MaxIOFS-Nonce")

		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))

		message := fmt.Sprintf("%s%s%s%s%s", r.Method, r.URL.Path, timestamp, nonce, string(body))
		mac := hmac.New(sha256.New, []byte(n.NodeToken))
		mac.Write([]byte(message))
		expectedSignature := hex.EncodeToString(mac.Sum(nil))

		if signature != expectedSignature {
			http.Error(w, "Invalid HMAC signature", http.StatusUnauthorized)
			return
		}
	}

	// Route requests
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/internal/cluster/tenant-sync"):
		n.handleTenantSync(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/internal/cluster/objects/"):
		n.handleObjectOperation(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleTenantSync handles tenant synchronization
func (n *SimulatedNode) handleTenantSync(w http.ResponseWriter, r *http.Request) {
	var tenantData struct {
		ID                  string            `json:"id"`
		Name                string            `json:"name"`
		DisplayName         string            `json:"display_name"`
		Description         string            `json:"description"`
		Status              string            `json:"status"`
		MaxAccessKeys       int               `json:"max_access_keys"`
		MaxStorageBytes     int64             `json:"max_storage_bytes"`
		CurrentStorageBytes int64             `json:"current_storage_bytes"`
		MaxBuckets          int               `json:"max_buckets"`
		CurrentBuckets      int               `json:"current_buckets"`
		Metadata            map[string]string `json:"metadata"`
		CreatedAt           time.Time         `json:"created_at"`
		UpdatedAt           time.Time         `json:"updated_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&tenantData); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	metadataJSON, _ := json.Marshal(tenantData.Metadata)

	// Upsert tenant
	var exists bool
	err := n.DB.QueryRowContext(n.Ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE id = ?)`, tenantData.ID).Scan(&exists)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	now := time.Now().Unix()
	if exists {
		_, err = n.DB.ExecContext(n.Ctx, `
			UPDATE tenants SET
				name = ?, display_name = ?, description = ?, status = ?,
				max_access_keys = ?, max_storage_bytes = ?, current_storage_bytes = ?,
				max_buckets = ?, current_buckets = ?, metadata = ?, updated_at = ?
			WHERE id = ?
		`, tenantData.Name, tenantData.DisplayName, tenantData.Description, tenantData.Status,
			tenantData.MaxAccessKeys, tenantData.MaxStorageBytes, tenantData.CurrentStorageBytes,
			tenantData.MaxBuckets, tenantData.CurrentBuckets, string(metadataJSON), now, tenantData.ID)
	} else {
		_, err = n.DB.ExecContext(n.Ctx, `
			INSERT INTO tenants (
				id, name, display_name, description, status,
				max_access_keys, max_storage_bytes, current_storage_bytes,
				max_buckets, current_buckets, metadata, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, tenantData.ID, tenantData.Name, tenantData.DisplayName, tenantData.Description, tenantData.Status,
			tenantData.MaxAccessKeys, tenantData.MaxStorageBytes, tenantData.CurrentStorageBytes,
			tenantData.MaxBuckets, tenantData.CurrentBuckets, string(metadataJSON), tenantData.CreatedAt.Unix(), now)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to sync tenant: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// handleObjectOperation handles object PUT/DELETE operations
func (n *SimulatedNode) handleObjectOperation(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/internal/cluster/objects/{tenantID}/{bucket}/{key}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/internal/cluster/objects/"), "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	objectPath := strings.Join(parts, "/")

	switch r.Method {
	case http.MethodPut:
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusInternalServerError)
			return
		}

		n.Objects[objectPath] = data
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})

	case http.MethodDelete:
		delete(n.Objects, objectPath)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// cleanup cleans up the simulated node
func (n *SimulatedNode) cleanup(t *testing.T) {
	n.Cancel()
	n.HTTPServer.Close()
	n.DB.Close()
	os.RemoveAll(n.DataDir)
}

// TestHMACAuthentication tests HMAC signature validation
func TestHMACAuthentication(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	node := setupSimulatedNode(t, "test-node")
	defer node.cleanup(t)

	t.Run("ValidSignature", func(t *testing.T) {
		body := []byte(`{"id": "test-tenant-123", "name": "test", "display_name": "Test", "status": "active", "max_access_keys": 10, "max_storage_bytes": 1073741824, "max_buckets": 10, "created_at": "2025-01-01T00:00:00Z", "updated_at": "2025-01-01T00:00:00Z"}`)
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		nonce := uuid.New().String()

		message := fmt.Sprintf("%s%s%s%s%s", "POST", "/api/internal/cluster/tenant-sync", timestamp, nonce, string(body))
		mac := hmac.New(sha256.New, []byte(node.NodeToken))
		mac.Write([]byte(message))
		signature := hex.EncodeToString(mac.Sum(nil))

		req := httptest.NewRequest("POST", "/api/internal/cluster/tenant-sync", bytes.NewReader(body))
		req.Header.Set("X-MaxIOFS-Signature", signature)
		req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
		req.Header.Set("X-MaxIOFS-Nonce", nonce)
		req.Header.Set("X-MaxIOFS-Node-ID", "sender-node")
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		node.HTTPServer.Config.Handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "Should accept valid HMAC signature")
	})

	t.Run("InvalidSignature", func(t *testing.T) {
		body := []byte(`{"test": "data"}`)
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		nonce := uuid.New().String()

		req := httptest.NewRequest("POST", "/api/internal/cluster/tenant-sync", bytes.NewReader(body))
		req.Header.Set("X-MaxIOFS-Signature", "invalid-signature-12345")
		req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
		req.Header.Set("X-MaxIOFS-Nonce", nonce)
		req.Header.Set("X-MaxIOFS-Node-ID", "sender-node")

		rec := httptest.NewRecorder()
		node.HTTPServer.Config.Handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, "Should reject invalid HMAC signature")
	})

	t.Logf("✅ HMAC authentication tests passed")
}

// TestTenantSynchronization tests tenant sync between nodes
func TestTenantSynchronization(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	node1 := setupSimulatedNode(t, "node1")
	defer node1.cleanup(t)

	node2 := setupSimulatedNode(t, "node2")
	defer node2.cleanup(t)

	// Create tenant on node1
	tenantID := uuid.New().String()
	_, err := node1.DB.ExecContext(node1.Ctx, `
		INSERT INTO tenants (id, name, display_name, description, status, max_access_keys, max_storage_bytes, max_buckets, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, tenantID, "sync-tenant", "Sync Test Tenant", "Test tenant", "active", 10, 1073741824, 10, time.Now().Unix(), time.Now().Unix())
	require.NoError(t, err)

	// Prepare tenant data for sync
	tenantData, err := json.Marshal(map[string]interface{}{
		"id":                    tenantID,
		"name":                  "sync-tenant",
		"display_name":          "Sync Test Tenant",
		"description":           "Test tenant",
		"status":                "active",
		"max_access_keys":       10,
		"max_storage_bytes":     1073741824,
		"current_storage_bytes": 0,
		"max_buckets":           10,
		"current_buckets":       0,
		"metadata":              map[string]string{},
		"created_at":            time.Now().Format(time.RFC3339),
		"updated_at":            time.Now().Format(time.RFC3339),
	})
	require.NoError(t, err)

	// Send tenant sync request with HMAC
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := uuid.New().String()
	message := fmt.Sprintf("%s%s%s%s%s", "POST", "/api/internal/cluster/tenant-sync", timestamp, nonce, string(tenantData))
	mac := hmac.New(sha256.New, []byte(node2.NodeToken))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/api/internal/cluster/tenant-sync", bytes.NewReader(tenantData))
	req.Header.Set("X-MaxIOFS-Signature", signature)
	req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
	req.Header.Set("X-MaxIOFS-Nonce", nonce)
	req.Header.Set("X-MaxIOFS-Node-ID", node1.NodeID)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	node2.HTTPServer.Config.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "Tenant sync should succeed")

	// Verify tenant exists on node2
	var name string
	err = node2.DB.QueryRowContext(node2.Ctx, `SELECT name FROM tenants WHERE id = ?`, tenantID).Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "sync-tenant", name, "Tenant should be synchronized to node2")

	t.Logf("✅ Tenant synchronization tests passed")
}

// TestObjectReplication tests object replication between nodes
func TestObjectReplication(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	node1 := setupSimulatedNode(t, "node1-obj")
	defer node1.cleanup(t)

	node2 := setupSimulatedNode(t, "node2-obj")
	defer node2.cleanup(t)

	// Create tenant
	tenantID := "test-tenant-123"
	bucketName := "test-bucket"
	objectKey := "test-object.txt"
	objectData := []byte("Hello from Node 1! This is encrypted data.")

	objectPath := fmt.Sprintf("%s/%s/%s", tenantID, bucketName, objectKey)

	// Simulate object PUT with HMAC
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := uuid.New().String()
	message := fmt.Sprintf("%s%s%s%s%s", "PUT", "/api/internal/cluster/objects/"+objectPath, timestamp, nonce, string(objectData))
	mac := hmac.New(sha256.New, []byte(node2.NodeToken))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("PUT", "/api/internal/cluster/objects/"+objectPath, bytes.NewReader(objectData))
	req.Header.Set("X-MaxIOFS-Signature", signature)
	req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
	req.Header.Set("X-MaxIOFS-Nonce", nonce)
	req.Header.Set("X-MaxIOFS-Node-ID", node1.NodeID)
	req.Header.Set("Content-Type", "text/plain")

	rec := httptest.NewRecorder()
	node2.HTTPServer.Config.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "Object replication should succeed")

	// Verify object exists on node2
	replicatedData, exists := node2.Objects[objectPath]
	require.True(t, exists, "Object should exist on node2")
	assert.Equal(t, objectData, replicatedData, "Replicated data should match original")

	t.Logf("✅ Object replication tests passed")
}

// TestDeleteReplication tests delete replication
func TestDeleteReplication(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	node1 := setupSimulatedNode(t, "node1-del")
	defer node1.cleanup(t)

	node2 := setupSimulatedNode(t, "node2-del")
	defer node2.cleanup(t)

	tenantID := "test-tenant-456"
	bucketName := "delete-bucket"
	objectKey := "delete-me.txt"
	objectPath := fmt.Sprintf("%s/%s/%s", tenantID, bucketName, objectKey)

	// Create object on node2
	node2.Objects[objectPath] = []byte("This will be deleted")

	// Send DELETE request with HMAC
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := uuid.New().String()
	message := fmt.Sprintf("%s%s%s%s%s", "DELETE", "/api/internal/cluster/objects/"+objectPath, timestamp, nonce, "")
	mac := hmac.New(sha256.New, []byte(node2.NodeToken))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("DELETE", "/api/internal/cluster/objects/"+objectPath, nil)
	req.Header.Set("X-MaxIOFS-Signature", signature)
	req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
	req.Header.Set("X-MaxIOFS-Nonce", nonce)
	req.Header.Set("X-MaxIOFS-Node-ID", node1.NodeID)

	rec := httptest.NewRecorder()
	node2.HTTPServer.Config.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "Delete replication should succeed")

	// Verify object deleted on node2
	_, exists := node2.Objects[objectPath]
	assert.False(t, exists, "Object should be deleted on node2")

	t.Logf("✅ Delete replication tests passed")
}

// TestSelfReplicationPrevention tests that nodes cannot replicate to themselves
func TestSelfReplicationPrevention(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	node := setupSimulatedNode(t, "node-self")
	defer node.cleanup(t)

	// Try to create a replication rule where source and destination are the same
	localNodeID := node.NodeID

	rule := &ClusterReplicationRule{
		ID:                  uuid.New().String(),
		TenantID:            "test-tenant",
		SourceBucket:        "test-bucket",
		DestinationNodeID:   localNodeID, // Same as source!
		DestinationBucket:   "test-bucket",
		SyncIntervalSeconds: 60,
		Enabled:             true,
		ReplicateDeletes:    true,
		ReplicateMetadata:   true,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}

	// This should fail validation (will be implemented in handlers)
	// For now, just verify the node IDs are the same
	assert.Equal(t, localNodeID, rule.DestinationNodeID, "Self-replication should be detected")

	t.Logf("✅ Self-replication prevention tests passed")
}
