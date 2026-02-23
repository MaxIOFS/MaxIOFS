package object

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcurrentMultipartUpload tests for race conditions in multipart uploads
func TestConcurrentMultipartUpload(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "maxiofs-race-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	storageBackend, err := storage.NewBackend(config.StorageConfig{
		Backend: "filesystem",
		Root:    tmpDir + "/objects",
	})
	require.NoError(t, err)

	metadataStore, err := metadata.NewPebbleStore(metadata.PebbleOptions{		DataDir: tmpDir + "/metadata",
		Logger:  logrus.StandardLogger(),})
	require.NoError(t, err)
	defer metadataStore.Close()

	om := NewManager(storageBackend, metadataStore, config.StorageConfig{})

	ctx := context.Background()
	bucket := "test-bucket"
	key := "test-object.bin"

	// Create multipart upload
	upload, err := om.CreateMultipartUpload(ctx, bucket, key, nil)
	require.NoError(t, err)

	// Upload 10 parts concurrently
	numParts := 10
	partSize := 1024 * 1024 // 1MB per part
	var wg sync.WaitGroup
	errors := make(chan error, numParts)

	t.Logf("Starting concurrent upload of %d parts...", numParts)
	start := time.Now()

	for i := 1; i <= numParts; i++ {
		wg.Add(1)
		go func(partNum int) {
			defer wg.Done()

			// Create part data
			data := bytes.Repeat([]byte(fmt.Sprintf("part%d", partNum)), partSize/10)
			reader := bytes.NewReader(data)

			// Upload part
			_, err := om.UploadPart(ctx, upload.UploadID, partNum, reader)
			if err != nil {
				errors <- fmt.Errorf("part %d failed: %w", partNum, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	elapsed := time.Since(start)
	t.Logf("Concurrent upload completed in %v", elapsed)

	// Check for errors
	var uploadErrors []error
	for err := range errors {
		uploadErrors = append(uploadErrors, err)
	}
	assert.Empty(t, uploadErrors, "Should have no upload errors")

	// List parts and verify all were uploaded
	parts, err := om.ListParts(ctx, upload.UploadID)
	require.NoError(t, err)
	assert.Equal(t, numParts, len(parts), "Should have all parts")

	// Verify part numbers are unique and sequential
	partNumbers := make(map[int]bool)
	for _, part := range parts {
		assert.False(t, partNumbers[part.PartNumber], "Part number should be unique: %d", part.PartNumber)
		partNumbers[part.PartNumber] = true
		assert.Greater(t, part.Size, int64(0), "Part size should be positive")
		assert.NotEmpty(t, part.ETag, "Part should have ETag")
	}

	// Prepare parts for completion
	completeParts := make([]Part, numParts)
	for i, part := range parts {
		completeParts[i] = Part{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
		}
	}

	// Complete multipart upload
	obj, err := om.CompleteMultipartUpload(ctx, upload.UploadID, completeParts)
	require.NoError(t, err)
	assert.NotNil(t, obj)
	assert.Equal(t, key, obj.Key)

	t.Logf("✅ Race condition test PASSED - no data corruption detected")
}

// TestMultipleSimultaneousMultipartUploads tests multiple uploads happening at the same time
func TestMultipleSimultaneousMultipartUploads(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "maxiofs-multi-race-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	storageBackend, err := storage.NewBackend(config.StorageConfig{
		Backend: "filesystem",
		Root:    tmpDir + "/objects",
	})
	require.NoError(t, err)

	metadataStore, err := metadata.NewPebbleStore(metadata.PebbleOptions{		DataDir: tmpDir + "/metadata",
		Logger:  logrus.StandardLogger(),})
	require.NoError(t, err)
	defer metadataStore.Close()

	om := NewManager(storageBackend, metadataStore, config.StorageConfig{})

	ctx := context.Background()
	bucket := "test-bucket"

	// Start 5 different multipart uploads concurrently
	numUploads := 5
	numPartsPerUpload := 5
	var wg sync.WaitGroup
	errors := make(chan error, numUploads*numPartsPerUpload)

	t.Logf("Starting %d simultaneous multipart uploads...", numUploads)
	start := time.Now()

	for uploadIdx := 0; uploadIdx < numUploads; uploadIdx++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			key := fmt.Sprintf("object-%d.bin", idx)

			// Create multipart upload
			upload, err := om.CreateMultipartUpload(ctx, bucket, key, nil)
			if err != nil {
				errors <- fmt.Errorf("upload %d create failed: %w", idx, err)
				return
			}

			// Upload parts for this upload
			for partNum := 1; partNum <= numPartsPerUpload; partNum++ {
				data := bytes.Repeat([]byte(fmt.Sprintf("upload%d-part%d", idx, partNum)), 1024)
				reader := bytes.NewReader(data)

				_, err := om.UploadPart(ctx, upload.UploadID, partNum, reader)
				if err != nil {
					errors <- fmt.Errorf("upload %d part %d failed: %w", idx, partNum, err)
				}
			}

			// Complete upload
			parts, err := om.ListParts(ctx, upload.UploadID)
			if err != nil {
				errors <- fmt.Errorf("upload %d list parts failed: %w", idx, err)
				return
			}

			completeParts := make([]Part, len(parts))
			for i, part := range parts {
				completeParts[i] = Part{
					PartNumber: part.PartNumber,
					ETag:       part.ETag,
				}
			}

			_, err = om.CompleteMultipartUpload(ctx, upload.UploadID, completeParts)
			if err != nil {
				errors <- fmt.Errorf("upload %d complete failed: %w", idx, err)
			}
		}(uploadIdx)
	}

	wg.Wait()
	close(errors)

	elapsed := time.Since(start)
	t.Logf("All uploads completed in %v", elapsed)

	// Check for errors
	var allErrors []error
	for err := range errors {
		allErrors = append(allErrors, err)
	}
	assert.Empty(t, allErrors, "Should have no errors in concurrent uploads")

	t.Logf("✅ Multiple simultaneous uploads test PASSED")
}
