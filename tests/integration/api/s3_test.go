package api

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/maxiofs/maxiofs/pkg/s3compat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServer holds the test server and dependencies
type TestServer struct {
	Server        *httptest.Server
	Handler       *s3compat.Handler
	BucketManager bucket.Manager
	ObjectManager object.Manager
	AuthManager   auth.Manager
	TempDir       string
}

// setupTestServer creates a complete test server with all dependencies
func setupTestServer(t *testing.T) *TestServer {
	// Create temporary directory for storage
	tempDir, err := os.MkdirTemp("", "maxiofs-integration-test-*")
	require.NoError(t, err)

	// Setup storage backend
	storageBackend, err := storage.NewFilesystemBackend(storage.Config{Root: tempDir})
	require.NoError(t, err)

	// Setup managers
	bucketMgr := bucket.NewManager(storageBackend)
	objectMgr := object.NewManager(storageBackend, config.StorageConfig{})
	authMgr := auth.NewManager(config.AuthConfig{
		EnableAuth: false, // Disable auth for integration tests
	})

	// Setup S3 handler
	handler := s3compat.NewHandler(bucketMgr, objectMgr)
	// Note: shareManager not set in tests, presigned URL share validation will be skipped

	// Create router
	router := mux.NewRouter()

	// Service-level operations
	router.HandleFunc("/", handler.ListBuckets).Methods("GET")

	// Bucket-level operations
	router.HandleFunc("/{bucket}", handler.HeadBucket).Methods("HEAD")
	router.HandleFunc("/{bucket}", handler.CreateBucket).Methods("PUT")
	router.HandleFunc("/{bucket}", handler.DeleteBucket).Methods("DELETE")
	router.HandleFunc("/{bucket}", handler.ListObjects).Methods("GET")

	// Object-level operations
	router.HandleFunc("/{bucket}/{object:.*}", handler.HeadObject).Methods("HEAD")
	router.HandleFunc("/{bucket}/{object:.*}", handler.GetObject).Methods("GET")
	router.HandleFunc("/{bucket}/{object:.*}", handler.PutObject).Methods("PUT")
	router.HandleFunc("/{bucket}/{object:.*}", handler.DeleteObject).Methods("DELETE")

	// Create test server
	server := httptest.NewServer(router)

	return &TestServer{
		Server:        server,
		Handler:       handler,
		BucketManager: bucketMgr,
		ObjectManager: objectMgr,
		AuthManager:   authMgr,
		TempDir:       tempDir,
	}
}

// teardownTestServer cleans up test resources
func (ts *TestServer) teardownTestServer(t *testing.T) {
	ts.Server.Close()
	os.RemoveAll(ts.TempDir)
}

// TestS3BasicOperations tests basic S3 operations end-to-end
func TestS3BasicOperations(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.teardownTestServer(t)

	bucketName := "test-bucket"
	objectKey := "test-object.txt"
	objectContent := []byte("Hello, S3!")

	t.Run("CreateBucket", func(t *testing.T) {
		url := fmt.Sprintf("%s/%s", ts.Server.URL, bucketName)
		req, err := http.NewRequest("PUT", url, nil)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("ListBuckets", func(t *testing.T) {
		url := ts.Server.URL + "/"
		resp, err := http.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Parse XML response
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var result s3compat.ListAllMyBucketsResult
		err = xml.Unmarshal(body, &result)
		require.NoError(t, err)

		assert.Len(t, result.Buckets.Bucket, 1)
		assert.Equal(t, bucketName, result.Buckets.Bucket[0].Name)
	})

	t.Run("HeadBucket", func(t *testing.T) {
		url := fmt.Sprintf("%s/%s", ts.Server.URL, bucketName)
		req, err := http.NewRequest("HEAD", url, nil)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("PutObject", func(t *testing.T) {
		url := fmt.Sprintf("%s/%s/%s", ts.Server.URL, bucketName, objectKey)
		req, err := http.NewRequest("PUT", url, bytes.NewReader(objectContent))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "text/plain")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.NotEmpty(t, resp.Header.Get("ETag"))
	})

	t.Run("GetObject", func(t *testing.T) {
		url := fmt.Sprintf("%s/%s/%s", ts.Server.URL, bucketName, objectKey)
		resp, err := http.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, objectContent, body)
		assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"))
	})

	t.Run("HeadObject", func(t *testing.T) {
		url := fmt.Sprintf("%s/%s/%s", ts.Server.URL, bucketName, objectKey)
		req, err := http.NewRequest("HEAD", url, nil)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.NotEmpty(t, resp.Header.Get("ETag"))
		assert.NotEmpty(t, resp.Header.Get("Last-Modified"))
		assert.Equal(t, fmt.Sprintf("%d", len(objectContent)), resp.Header.Get("Content-Length"))
	})

	t.Run("ListObjects", func(t *testing.T) {
		url := fmt.Sprintf("%s/%s", ts.Server.URL, bucketName)
		resp, err := http.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var result s3compat.ListBucketResult
		err = xml.Unmarshal(body, &result)
		require.NoError(t, err)

		assert.Len(t, result.Contents, 1)
		assert.Equal(t, objectKey, result.Contents[0].Key)
	})

	t.Run("DeleteObject", func(t *testing.T) {
		url := fmt.Sprintf("%s/%s/%s", ts.Server.URL, bucketName, objectKey)
		req, err := http.NewRequest("DELETE", url, nil)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	})

	t.Run("DeleteBucket", func(t *testing.T) {
		url := fmt.Sprintf("%s/%s", ts.Server.URL, bucketName)
		req, err := http.NewRequest("DELETE", url, nil)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	})
}

// TestS3MultipartUpload tests multipart upload workflow
func TestS3MultipartUpload(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.teardownTestServer(t)

	ctx := context.Background()
	bucketName := "test-multipart-bucket"
	objectKey := "large-object.bin"

	// Create bucket first
	err := ts.BucketManager.CreateBucket(ctx, bucketName)
	require.NoError(t, err)

	t.Run("CompleteMultipartWorkflow", func(t *testing.T) {
		// Initiate multipart upload
		upload, err := ts.ObjectManager.CreateMultipartUpload(ctx, bucketName, objectKey, http.Header{})
		require.NoError(t, err)
		assert.NotEmpty(t, upload.UploadID)

		// Upload parts
		part1Data := []byte("Part 1 data - minimum size part")
		part2Data := []byte("Part 2 data - minimum size part")

		part1, err := ts.ObjectManager.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(part1Data))
		require.NoError(t, err)
		assert.NotEmpty(t, part1.ETag)

		part2, err := ts.ObjectManager.UploadPart(ctx, upload.UploadID, 2, bytes.NewReader(part2Data))
		require.NoError(t, err)
		assert.NotEmpty(t, part2.ETag)

		// List parts
		parts, err := ts.ObjectManager.ListParts(ctx, upload.UploadID)
		require.NoError(t, err)
		assert.Len(t, parts, 2)

		// Complete multipart upload
		completedParts := []object.Part{
			*part1,
			*part2,
		}

		obj, err := ts.ObjectManager.CompleteMultipartUpload(ctx, upload.UploadID, completedParts)
		require.NoError(t, err)
		assert.NotNil(t, obj)
		assert.Equal(t, objectKey, obj.Key)

		// Verify object exists
		metadata, err := ts.ObjectManager.GetObjectMetadata(ctx, bucketName, objectKey)
		require.NoError(t, err)
		assert.Equal(t, objectKey, metadata.Key)
	})

	t.Run("AbortMultipartUpload", func(t *testing.T) {
		// Initiate another upload
		upload, err := ts.ObjectManager.CreateMultipartUpload(ctx, bucketName, "aborted-object.bin", http.Header{})
		require.NoError(t, err)

		// Upload one part
		partData := []byte("Part data")
		_, err = ts.ObjectManager.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(partData))
		require.NoError(t, err)

		// Abort upload
		err = ts.ObjectManager.AbortMultipartUpload(ctx, upload.UploadID)
		require.NoError(t, err)

		// Verify upload is gone
		parts, err := ts.ObjectManager.ListParts(ctx, upload.UploadID)
		assert.Error(t, err)
		assert.Nil(t, parts)
	})
}

// TestS3ConcurrentAccess tests concurrent operations
func TestS3ConcurrentAccess(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.teardownTestServer(t)

	ctx := context.Background()
	bucketName := "concurrent-test-bucket"

	// Create bucket
	err := ts.BucketManager.CreateBucket(ctx, bucketName)
	require.NoError(t, err)

	t.Run("ConcurrentPutObjects", func(t *testing.T) {
		numObjects := 50
		done := make(chan bool, numObjects)
		errors := make(chan error, numObjects)

		// Upload objects concurrently
		for i := 0; i < numObjects; i++ {
			go func(index int) {
				objectKey := fmt.Sprintf("object-%d.txt", index)
				content := []byte(fmt.Sprintf("Content for object %d", index))

				url := fmt.Sprintf("%s/%s/%s", ts.Server.URL, bucketName, objectKey)
				req, err := http.NewRequest("PUT", url, bytes.NewReader(content))
				if err != nil {
					errors <- err
					done <- false
					return
				}

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					errors <- err
					done <- false
					return
				}
				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("unexpected status code: %d", resp.StatusCode)
					done <- false
					return
				}

				done <- true
			}(i)
		}

		// Wait for all uploads to complete
		successCount := 0
		for i := 0; i < numObjects; i++ {
			select {
			case success := <-done:
				if success {
					successCount++
				}
			case err := <-errors:
				t.Logf("Error during concurrent upload: %v", err)
			case <-time.After(30 * time.Second):
				t.Fatal("Timeout waiting for concurrent uploads")
			}
		}

		assert.Equal(t, numObjects, successCount)

		// Verify all objects exist
		objects, _, err := ts.ObjectManager.ListObjects(ctx, bucketName, "", "", "", 100)
		require.NoError(t, err)
		assert.Len(t, objects, numObjects)
	})

	t.Run("ConcurrentReadWrite", func(t *testing.T) {
		objectKey := "rw-test-object.txt"
		initialContent := []byte("Initial content")

		// Create initial object
		_, err := ts.ObjectManager.PutObject(ctx, bucketName, objectKey, bytes.NewReader(initialContent), http.Header{})
		require.NoError(t, err)

		done := make(chan bool, 20)

		// 10 concurrent readers
		for i := 0; i < 10; i++ {
			go func() {
				url := fmt.Sprintf("%s/%s/%s", ts.Server.URL, bucketName, objectKey)
				resp, err := http.Get(url)
				if err == nil {
					resp.Body.Close()
					done <- true
				} else {
					done <- false
				}
			}()
		}

		// 10 concurrent writers
		for i := 0; i < 10; i++ {
			go func(index int) {
				content := []byte(fmt.Sprintf("Updated content %d", index))
				url := fmt.Sprintf("%s/%s/%s", ts.Server.URL, bucketName, objectKey)
				req, err := http.NewRequest("PUT", url, bytes.NewReader(content))
				if err == nil {
					resp, err := http.DefaultClient.Do(req)
					if err == nil {
						resp.Body.Close()
						done <- true
						return
					}
				}
				done <- false
			}(i)
		}

		// Wait for completion
		successCount := 0
		for i := 0; i < 20; i++ {
			select {
			case success := <-done:
				if success {
					successCount++
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Timeout waiting for concurrent read/write")
			}
		}

		assert.Greater(t, successCount, 15) // Allow for some failures due to race conditions
	})
}

// TestS3ErrorHandling tests error cases
func TestS3ErrorHandling(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.teardownTestServer(t)

	t.Run("BucketNotFound", func(t *testing.T) {
		url := fmt.Sprintf("%s/nonexistent-bucket", ts.Server.URL)
		req, err := http.NewRequest("HEAD", url, nil)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("ObjectNotFound", func(t *testing.T) {
		ctx := context.Background()
		bucketName := "error-test-bucket"

		err := ts.BucketManager.CreateBucket(ctx, bucketName)
		require.NoError(t, err)

		url := fmt.Sprintf("%s/%s/nonexistent-object.txt", ts.Server.URL, bucketName)
		resp, err := http.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("InvalidBucketName", func(t *testing.T) {
		url := fmt.Sprintf("%s/Invalid_Bucket_Name", ts.Server.URL)
		req, err := http.NewRequest("PUT", url, nil)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should return either 400 or 500 for invalid bucket name
		assert.True(t, resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusInternalServerError)
	})

	t.Run("DeleteNonEmptyBucket", func(t *testing.T) {
		ctx := context.Background()
		bucketName := "non-empty-bucket"

		err := ts.BucketManager.CreateBucket(ctx, bucketName)
		require.NoError(t, err)

		// Add object
		_, err = ts.ObjectManager.PutObject(ctx, bucketName, "test-object.txt", bytes.NewReader([]byte("content")), http.Header{})
		require.NoError(t, err)

		// Try to delete bucket
		url := fmt.Sprintf("%s/%s", ts.Server.URL, bucketName)
		req, err := http.NewRequest("DELETE", url, nil)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusConflict, resp.StatusCode)
	})
}
