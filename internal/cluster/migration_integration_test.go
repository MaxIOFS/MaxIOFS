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

// MigrationTestNode represents a node for migration testing
type MigrationTestNode struct {
	NodeID       string
	NodeName     string
	NodeToken    string
	DataDir      string
	DB           *sql.DB
	HTTPServer   *httptest.Server
	Objects      map[string][]byte // bucket/key -> data
	BucketACLs   map[string]string // bucket -> acl_json
	BucketPerms  map[string]map[string]bool // bucket -> user -> has_permission
	BucketConfigs map[string]map[string]interface{} // bucket -> config
	Ctx          context.Context
	Cancel       context.CancelFunc
}

// setupMigrationNode creates a test node for migration
func setupMigrationNode(t *testing.T, nodeName string) *MigrationTestNode {
	dataDir, err := os.MkdirTemp("", fmt.Sprintf("maxiofs-migration-test-%s-*", nodeName))
	require.NoError(t, err)

	dbPath := filepath.Join(dataDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	// Create all necessary tables
	err = initMigrationTestSchema(db)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	nodeID := uuid.New().String()
	nodeToken := uuid.New().String()

	node := &MigrationTestNode{
		NodeID:        nodeID,
		NodeName:      nodeName,
		NodeToken:     nodeToken,
		DataDir:       dataDir,
		DB:            db,
		Objects:       make(map[string][]byte),
		BucketACLs:    make(map[string]string),
		BucketPerms:   make(map[string]map[string]bool),
		BucketConfigs: make(map[string]map[string]interface{}),
		Ctx:           ctx,
		Cancel:        cancel,
	}

	node.HTTPServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node.handleMigrationRequest(w, r)
	}))

	return node
}

// initMigrationTestSchema creates all necessary tables
func initMigrationTestSchema(db *sql.DB) error {
	schemas := []string{
		// Cluster migrations table
		`CREATE TABLE IF NOT EXISTS cluster_migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bucket_name TEXT NOT NULL,
			source_node_id TEXT NOT NULL,
			target_node_id TEXT NOT NULL,
			status TEXT NOT NULL,
			objects_total INTEGER DEFAULT 0,
			objects_migrated INTEGER DEFAULT 0,
			bytes_total INTEGER DEFAULT 0,
			bytes_migrated INTEGER DEFAULT 0,
			delete_source BOOLEAN DEFAULT 0,
			verify_data BOOLEAN DEFAULT 1,
			error_message TEXT,
			started_at INTEGER,
			completed_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		// Bucket inventory configs
		`CREATE TABLE IF NOT EXISTS bucket_inventory_configs (
			id TEXT PRIMARY KEY,
			bucket_name TEXT NOT NULL,
			tenant_id TEXT,
			enabled BOOLEAN DEFAULT 1,
			frequency TEXT NOT NULL,
			format TEXT NOT NULL,
			destination_bucket TEXT NOT NULL,
			destination_prefix TEXT DEFAULT '',
			included_fields TEXT NOT NULL,
			schedule_time TEXT NOT NULL,
			last_run_at INTEGER,
			next_run_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(bucket_name, tenant_id)
		)`,
		// Access keys sync table
		`CREATE TABLE IF NOT EXISTS cluster_access_key_sync (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			access_key TEXT NOT NULL,
			user_id TEXT NOT NULL,
			tenant_id TEXT,
			operation TEXT NOT NULL,
			synced_at INTEGER NOT NULL,
			UNIQUE(access_key, operation)
		)`,
		// Bucket permissions sync table
		`CREATE TABLE IF NOT EXISTS cluster_bucket_permission_sync (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bucket_name TEXT NOT NULL,
			user_id TEXT NOT NULL,
			tenant_id TEXT,
			operation TEXT NOT NULL,
			synced_at INTEGER NOT NULL,
			UNIQUE(bucket_name, user_id, operation)
		)`,
	}

	for _, schema := range schemas {
		if _, err := db.Exec(schema); err != nil {
			return fmt.Errorf("failed to create schema: %v", err)
		}
	}

	return nil
}

// handleMigrationRequest handles HTTP requests for migration testing
func (n *MigrationTestNode) handleMigrationRequest(w http.ResponseWriter, r *http.Request) {
	// Verify HMAC if present
	if !n.verifyHMAC(r) {
		http.Error(w, "Invalid HMAC signature", http.StatusUnauthorized)
		return
	}

	switch {
	case strings.HasPrefix(r.URL.Path, "/api/internal/cluster/migration/objects/"):
		n.handleObjectMigration(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/internal/cluster/migration/acl/"):
		n.handleACLMigration(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/internal/cluster/migration/permissions/"):
		n.handlePermissionsMigration(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/internal/cluster/migration/config/"):
		n.handleConfigMigration(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/internal/cluster/migration/inventory/"):
		n.handleInventoryMigration(w, r)
	default:
		http.NotFound(w, r)
	}
}

// verifyHMAC verifies HMAC signature
func (n *MigrationTestNode) verifyHMAC(r *http.Request) bool {
	signature := r.Header.Get("X-MaxIOFS-Signature")
	if signature == "" {
		return true // No signature required for test
	}

	timestamp := r.Header.Get("X-MaxIOFS-Timestamp")
	nonce := r.Header.Get("X-MaxIOFS-Nonce")

	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))

	message := fmt.Sprintf("%s%s%s%s%s", r.Method, r.URL.Path, timestamp, nonce, string(body))
	mac := hmac.New(sha256.New, []byte(n.NodeToken))
	mac.Write([]byte(message))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	return signature == expectedSignature
}

// handleObjectMigration handles object data migration
func (n *MigrationTestNode) handleObjectMigration(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/internal/cluster/migration/objects/{bucket}/{key}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/internal/cluster/migration/objects/"), "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	objectPath := strings.Join(parts, "/")

	if r.Method == http.MethodPut {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusInternalServerError)
			return
		}

		n.Objects[objectPath] = data
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleACLMigration handles bucket ACL migration
func (n *MigrationTestNode) handleACLMigration(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/internal/cluster/migration/acl/{bucket}
	bucket := strings.TrimPrefix(r.URL.Path, "/api/internal/cluster/migration/acl/")

	if r.Method == http.MethodPut {
		var aclData map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&aclData); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		aclJSON, _ := json.Marshal(aclData)
		n.BucketACLs[bucket] = string(aclJSON)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePermissionsMigration handles bucket permissions migration
func (n *MigrationTestNode) handlePermissionsMigration(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/internal/cluster/migration/permissions/{bucket}
	bucket := strings.TrimPrefix(r.URL.Path, "/api/internal/cluster/migration/permissions/")

	if r.Method == http.MethodPut {
		var permsData struct {
			UserID   string `json:"user_id"`
			TenantID string `json:"tenant_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&permsData); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if n.BucketPerms[bucket] == nil {
			n.BucketPerms[bucket] = make(map[string]bool)
		}
		n.BucketPerms[bucket][permsData.UserID] = true

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleConfigMigration handles bucket configuration migration
func (n *MigrationTestNode) handleConfigMigration(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/internal/cluster/migration/config/{bucket}
	bucket := strings.TrimPrefix(r.URL.Path, "/api/internal/cluster/migration/config/")

	if r.Method == http.MethodPut {
		var configData map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&configData); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		n.BucketConfigs[bucket] = configData

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleInventoryMigration handles inventory configuration migration
func (n *MigrationTestNode) handleInventoryMigration(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/internal/cluster/migration/inventory/{bucket}
	_ = strings.TrimPrefix(r.URL.Path, "/api/internal/cluster/migration/inventory/")

	if r.Method == http.MethodPut {
		var inventoryConfig struct {
			ID                string   `json:"id"`
			BucketName        string   `json:"bucket_name"`
			TenantID          string   `json:"tenant_id"`
			Enabled           bool     `json:"enabled"`
			Frequency         string   `json:"frequency"`
			Format            string   `json:"format"`
			DestinationBucket string   `json:"destination_bucket"`
			DestinationPrefix string   `json:"destination_prefix"`
			IncludedFields    []string `json:"included_fields"`
			ScheduleTime      string   `json:"schedule_time"`
			LastRunAt         *int64   `json:"last_run_at"`
			NextRunAt         *int64   `json:"next_run_at"`
			CreatedAt         int64    `json:"created_at"`
			UpdatedAt         int64    `json:"updated_at"`
		}

		if err := json.NewDecoder(r.Body).Decode(&inventoryConfig); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Store in database
		fieldsJSON, _ := json.Marshal(inventoryConfig.IncludedFields)
		_, err := n.DB.ExecContext(n.Ctx, `
			INSERT OR REPLACE INTO bucket_inventory_configs (
				id, bucket_name, tenant_id, enabled, frequency, format,
				destination_bucket, destination_prefix, included_fields,
				schedule_time, last_run_at, next_run_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, inventoryConfig.ID, inventoryConfig.BucketName, inventoryConfig.TenantID,
			inventoryConfig.Enabled, inventoryConfig.Frequency, inventoryConfig.Format,
			inventoryConfig.DestinationBucket, inventoryConfig.DestinationPrefix,
			string(fieldsJSON), inventoryConfig.ScheduleTime, inventoryConfig.LastRunAt,
			inventoryConfig.NextRunAt, inventoryConfig.CreatedAt, inventoryConfig.UpdatedAt)

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to save inventory config: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// cleanup cleans up the test node
func (n *MigrationTestNode) cleanup(t *testing.T) {
	n.Cancel()
	n.HTTPServer.Close()
	n.DB.Close()
	os.RemoveAll(n.DataDir)
}

// TestBucketMigrationEndToEnd tests complete bucket migration flow
func TestBucketMigrationEndToEnd(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	node1 := setupMigrationNode(t, "source-node")
	defer node1.cleanup(t)

	node2 := setupMigrationNode(t, "target-node")
	defer node2.cleanup(t)

	bucketName := "migration-test-bucket"

	t.Run("MigrateObjects", func(t *testing.T) {
		// Create objects on node1
		objects := map[string][]byte{
			bucketName + "/file1.txt": []byte("Hello from file 1"),
			bucketName + "/file2.txt": []byte("Hello from file 2"),
			bucketName + "/dir/file3.txt": []byte("Hello from file 3 in directory"),
		}

		for path, data := range objects {
			node1.Objects[path] = data
		}

		// Simulate object migration
		for path, data := range objects {
			timestamp := fmt.Sprintf("%d", time.Now().Unix())
			nonce := uuid.New().String()
			apiPath := "/api/internal/cluster/migration/objects/" + path

			message := fmt.Sprintf("%s%s%s%s%s", "PUT", apiPath, timestamp, nonce, string(data))
			mac := hmac.New(sha256.New, []byte(node2.NodeToken))
			mac.Write([]byte(message))
			signature := hex.EncodeToString(mac.Sum(nil))

			req := httptest.NewRequest("PUT", apiPath, bytes.NewReader(data))
			req.Header.Set("X-MaxIOFS-Signature", signature)
			req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
			req.Header.Set("X-MaxIOFS-Nonce", nonce)

			rec := httptest.NewRecorder()
			node2.HTTPServer.Config.Handler.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code, "Object migration should succeed for %s", path)
		}

		// Verify all objects migrated
		assert.Equal(t, len(objects), len(node2.Objects), "All objects should be migrated")
		for path, originalData := range objects {
			migratedData, exists := node2.Objects[path]
			require.True(t, exists, "Object %s should exist on target node", path)
			assert.Equal(t, originalData, migratedData, "Object data should match for %s", path)
		}

		t.Logf("✅ Successfully migrated %d objects", len(objects))
	})

	t.Run("MigrateBucketACL", func(t *testing.T) {
		// Create ACL on node1
		acl := map[string]interface{}{
			"owner": "test-user",
			"grants": []map[string]string{
				{"grantee": "user1", "permission": "READ"},
				{"grantee": "user2", "permission": "WRITE"},
			},
		}
		aclJSON, _ := json.Marshal(acl)
		node1.BucketACLs[bucketName] = string(aclJSON)

		// Migrate ACL
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		nonce := uuid.New().String()
		apiPath := "/api/internal/cluster/migration/acl/" + bucketName

		message := fmt.Sprintf("%s%s%s%s%s", "PUT", apiPath, timestamp, nonce, string(aclJSON))
		mac := hmac.New(sha256.New, []byte(node2.NodeToken))
		mac.Write([]byte(message))
		signature := hex.EncodeToString(mac.Sum(nil))

		req := httptest.NewRequest("PUT", apiPath, bytes.NewReader(aclJSON))
		req.Header.Set("X-MaxIOFS-Signature", signature)
		req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
		req.Header.Set("X-MaxIOFS-Nonce", nonce)

		rec := httptest.NewRecorder()
		node2.HTTPServer.Config.Handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "ACL migration should succeed")

		// Verify ACL migrated
		migratedACL, exists := node2.BucketACLs[bucketName]
		require.True(t, exists, "Bucket ACL should exist on target node")
		assert.JSONEq(t, string(aclJSON), migratedACL, "ACL data should match")

		t.Logf("✅ Successfully migrated bucket ACL")
	})

	t.Run("MigrateBucketPermissions", func(t *testing.T) {
		// Create permissions on node1
		users := []string{"user1", "user2", "user3"}
		node1.BucketPerms[bucketName] = make(map[string]bool)
		for _, user := range users {
			node1.BucketPerms[bucketName][user] = true
		}

		// Migrate permissions
		for _, user := range users {
			permData, _ := json.Marshal(map[string]string{
				"user_id":   user,
				"tenant_id": "test-tenant",
			})

			timestamp := fmt.Sprintf("%d", time.Now().Unix())
			nonce := uuid.New().String()
			apiPath := "/api/internal/cluster/migration/permissions/" + bucketName

			message := fmt.Sprintf("%s%s%s%s%s", "PUT", apiPath, timestamp, nonce, string(permData))
			mac := hmac.New(sha256.New, []byte(node2.NodeToken))
			mac.Write([]byte(message))
			signature := hex.EncodeToString(mac.Sum(nil))

			req := httptest.NewRequest("PUT", apiPath, bytes.NewReader(permData))
			req.Header.Set("X-MaxIOFS-Signature", signature)
			req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
			req.Header.Set("X-MaxIOFS-Nonce", nonce)

			rec := httptest.NewRecorder()
			node2.HTTPServer.Config.Handler.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code, "Permission migration should succeed for %s", user)
		}

		// Verify permissions migrated
		require.NotNil(t, node2.BucketPerms[bucketName], "Bucket permissions should exist on target node")
		assert.Equal(t, len(users), len(node2.BucketPerms[bucketName]), "All permissions should be migrated")
		for _, user := range users {
			assert.True(t, node2.BucketPerms[bucketName][user], "User %s should have permission", user)
		}

		t.Logf("✅ Successfully migrated %d bucket permissions", len(users))
	})

	t.Run("MigrateBucketConfiguration", func(t *testing.T) {
		// Create bucket config on node1
		config := map[string]interface{}{
			"versioning": map[string]interface{}{
				"enabled": true,
				"mfa_delete": false,
			},
			"lifecycle": []map[string]interface{}{
				{
					"id": "expire-old-versions",
					"enabled": true,
					"expiration_days": 30,
				},
			},
			"tags": map[string]string{
				"environment": "production",
				"team": "backend",
			},
			"cors": []map[string]interface{}{
				{
					"allowed_origins": []string{"*"},
					"allowed_methods": []string{"GET", "PUT"},
				},
			},
			"encryption": map[string]interface{}{
				"enabled": true,
				"algorithm": "AES256",
			},
		}
		node1.BucketConfigs[bucketName] = config

		// Migrate config
		configJSON, _ := json.Marshal(config)
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		nonce := uuid.New().String()
		apiPath := "/api/internal/cluster/migration/config/" + bucketName

		message := fmt.Sprintf("%s%s%s%s%s", "PUT", apiPath, timestamp, nonce, string(configJSON))
		mac := hmac.New(sha256.New, []byte(node2.NodeToken))
		mac.Write([]byte(message))
		signature := hex.EncodeToString(mac.Sum(nil))

		req := httptest.NewRequest("PUT", apiPath, bytes.NewReader(configJSON))
		req.Header.Set("X-MaxIOFS-Signature", signature)
		req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
		req.Header.Set("X-MaxIOFS-Nonce", nonce)

		rec := httptest.NewRecorder()
		node2.HTTPServer.Config.Handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "Config migration should succeed")

		// Verify config migrated
		migratedConfig, exists := node2.BucketConfigs[bucketName]
		require.True(t, exists, "Bucket config should exist on target node")

		// Verify specific config elements
		assert.Equal(t, config["versioning"], migratedConfig["versioning"], "Versioning config should match")
		assert.Equal(t, config["encryption"], migratedConfig["encryption"], "Encryption config should match")

		// Compare tags by converting to JSON
		originalTagsJSON, _ := json.Marshal(config["tags"])
		migratedTagsJSON, _ := json.Marshal(migratedConfig["tags"])
		assert.JSONEq(t, string(originalTagsJSON), string(migratedTagsJSON), "Tags should match")

		t.Logf("✅ Successfully migrated bucket configuration")
	})

	t.Run("MigrateInventoryConfiguration", func(t *testing.T) {
		// Create inventory config on node1
		now := time.Now().Unix()
		inventoryConfig := map[string]interface{}{
			"id":                 uuid.New().String(),
			"bucket_name":        bucketName,
			"tenant_id":          "test-tenant",
			"enabled":            true,
			"frequency":          "daily",
			"format":             "csv",
			"destination_bucket": "inventory-reports",
			"destination_prefix": "reports/",
			"included_fields":    []string{"bucket_name", "object_key", "size", "last_modified"},
			"schedule_time":      "02:00",
			"last_run_at":        nil,
			"next_run_at":        nil,
			"created_at":         now,
			"updated_at":         now,
		}

		// Migrate inventory config
		inventoryJSON, _ := json.Marshal(inventoryConfig)
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		nonce := uuid.New().String()
		apiPath := "/api/internal/cluster/migration/inventory/" + bucketName

		message := fmt.Sprintf("%s%s%s%s%s", "PUT", apiPath, timestamp, nonce, string(inventoryJSON))
		mac := hmac.New(sha256.New, []byte(node2.NodeToken))
		mac.Write([]byte(message))
		signature := hex.EncodeToString(mac.Sum(nil))

		req := httptest.NewRequest("PUT", apiPath, bytes.NewReader(inventoryJSON))
		req.Header.Set("X-MaxIOFS-Signature", signature)
		req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
		req.Header.Set("X-MaxIOFS-Nonce", nonce)

		rec := httptest.NewRecorder()
		node2.HTTPServer.Config.Handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "Inventory config migration should succeed")

		// Verify inventory config in database
		var retrievedBucketName, frequency, format string
		var enabled bool
		err := node2.DB.QueryRowContext(node2.Ctx, `
			SELECT bucket_name, enabled, frequency, format
			FROM bucket_inventory_configs
			WHERE bucket_name = ?
		`, bucketName).Scan(&retrievedBucketName, &enabled, &frequency, &format)

		require.NoError(t, err, "Inventory config should exist in database")
		assert.Equal(t, bucketName, retrievedBucketName, "Bucket name should match")
		assert.True(t, enabled, "Inventory should be enabled")
		assert.Equal(t, "daily", frequency, "Frequency should match")
		assert.Equal(t, "csv", format, "Format should match")

		t.Logf("✅ Successfully migrated inventory configuration")
	})

	t.Logf("✅ Complete end-to-end bucket migration test passed")
}

// TestMigrationDataIntegrity tests data integrity during migration
func TestMigrationDataIntegrity(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	node1 := setupMigrationNode(t, "integrity-source")
	defer node1.cleanup(t)

	node2 := setupMigrationNode(t, "integrity-target")
	defer node2.cleanup(t)

	bucketName := "integrity-test-bucket"

	// Create large object with specific content
	largeContent := bytes.Repeat([]byte("Test data integrity! "), 10000) // ~200KB
	objectPath := bucketName + "/large-file.bin"

	node1.Objects[objectPath] = largeContent

	// Migrate object
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := uuid.New().String()
	apiPath := "/api/internal/cluster/migration/objects/" + objectPath

	message := fmt.Sprintf("%s%s%s%s%s", "PUT", apiPath, timestamp, nonce, string(largeContent))
	mac := hmac.New(sha256.New, []byte(node2.NodeToken))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("PUT", apiPath, bytes.NewReader(largeContent))
	req.Header.Set("X-MaxIOFS-Signature", signature)
	req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
	req.Header.Set("X-MaxIOFS-Nonce", nonce)

	rec := httptest.NewRecorder()
	node2.HTTPServer.Config.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "Large object migration should succeed")

	// Verify data integrity
	migratedData, exists := node2.Objects[objectPath]
	require.True(t, exists, "Large object should exist on target node")
	assert.Equal(t, len(largeContent), len(migratedData), "Data size should match")
	assert.Equal(t, largeContent, migratedData, "Data content should match exactly")

	t.Logf("✅ Data integrity verified for %d bytes", len(largeContent))
}

// TestMigrationErrorHandling tests error scenarios during migration
func TestMigrationErrorHandling(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	node := setupMigrationNode(t, "error-test-node")
	defer node.cleanup(t)

	t.Run("InvalidPath", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/internal/cluster/migration/objects/", bytes.NewReader([]byte("test")))
		rec := httptest.NewRecorder()
		node.HTTPServer.Config.Handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code, "Should reject invalid path")
	})

	t.Run("InvalidACLJSON", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/internal/cluster/migration/acl/test-bucket",
			bytes.NewReader([]byte("invalid json")))
		rec := httptest.NewRecorder()
		node.HTTPServer.Config.Handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code, "Should reject invalid JSON")
	})

	t.Run("InvalidHMACSignature", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/internal/cluster/migration/objects/bucket/key",
			bytes.NewReader([]byte("test data")))
		req.Header.Set("X-MaxIOFS-Signature", "invalid-signature")
		req.Header.Set("X-MaxIOFS-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
		req.Header.Set("X-MaxIOFS-Nonce", uuid.New().String())

		rec := httptest.NewRecorder()
		node.HTTPServer.Config.Handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, "Should reject invalid HMAC")
	})

	t.Logf("✅ Error handling tests passed")
}
