package bucket

import (
	"context"
	"os"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupBucketTest(t *testing.T) (Manager, func()) {
	tmpDir, err := os.MkdirTemp("", "bucket-test-*")
	require.NoError(t, err)

	// Create storage backend
	storageBackend, err := storage.NewFilesystemBackend(config.StorageConfig{
		Root: tmpDir + "/storage",
	})
	require.NoError(t, err)

	// Create metadata store
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	metadataStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           tmpDir + "/metadata",
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logger,
	})
	require.NoError(t, err)

	// Create bucket manager
	manager := NewManager(storageBackend, metadataStore)
	require.NotNil(t, manager)

	cleanup := func() {
		storageBackend.Close()
		metadataStore.Close()
		os.RemoveAll(tmpDir)
	}

	return manager, cleanup
}

// TestValidateBucketName tests bucket name validation
func TestValidateBucketName(t *testing.T) {
	tests := []struct {
		name      string
		bucket    string
		wantError bool
	}{
		{"Valid simple name", "mybucket", false},
		{"Valid with numbers", "mybucket123", false},
		{"Valid with hyphens", "my-bucket-name", false},
		{"Valid minimum length", "abc", false},
		{"Valid maximum length", "a123456789012345678901234567890123456789012345678901234567890bc", false}, // 63 chars
		{"Too short", "ab", true},
		{"Too long", "a1234567890123456789012345678901234567890123456789012345678901234", true}, // 64 chars
		{"Uppercase letters", "MyBucket", true},
		{"Starts with hyphen", "-mybucket", true},
		{"Ends with hyphen", "mybucket-", true},
		{"Consecutive hyphens", "my--bucket", true},
		{"IP address format", "192.168.1.1", true},
		{"Starts with xn--", "xn--bucket", true},
		{"Ends with -s3alias", "mybucket-s3alias", true},
		{"Empty name", "", true},
		{"Special characters", "my_bucket", true},
		{"Spaces", "my bucket", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBucketName(tt.bucket)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestCreateBucket tests bucket creation
func TestCreateBucket(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("Create bucket successfully", func(t *testing.T) {
		err := manager.CreateBucket(ctx, "tenant-1", "test-bucket", "")
		assert.NoError(t, err)

		// Verify bucket exists
		exists, err := manager.BucketExists(ctx, "tenant-1", "test-bucket")
		assert.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("Create bucket with invalid name", func(t *testing.T) {
		err := manager.CreateBucket(ctx, "tenant-1", "Invalid-Bucket", "")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidBucketName)
	})

	t.Run("Create duplicate bucket", func(t *testing.T) {
		err := manager.CreateBucket(ctx, "tenant-1", "duplicate-bucket", "")
		require.NoError(t, err)

		// Try to create again
		err = manager.CreateBucket(ctx, "tenant-1", "duplicate-bucket", "")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrBucketAlreadyExists)
	})

	t.Run("Create bucket for different tenants", func(t *testing.T) {
		// Bucket names must be globally unique (like real S3)
		err := manager.CreateBucket(ctx, "tenant-a", "shared-name", "")
		assert.NoError(t, err)

		// Same name, different tenant should fail
		err = manager.CreateBucket(ctx, "tenant-b", "shared-name", "")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrBucketAlreadyExists)

		// But different names work fine
		err = manager.CreateBucket(ctx, "tenant-b", "tenant-b-bucket", "")
		assert.NoError(t, err)
	})

	t.Run("Create global bucket (no tenant)", func(t *testing.T) {
		err := manager.CreateBucket(ctx, "", "global-bucket", "")
		assert.NoError(t, err)

		exists, err := manager.BucketExists(ctx, "", "global-bucket")
		assert.NoError(t, err)
		assert.True(t, exists)
	})
}

// TestGetBucketInfo tests getting bucket information
func TestGetBucketInfo(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("Get existing bucket info", func(t *testing.T) {
		err := manager.CreateBucket(ctx, "tenant-1", "info-bucket", "")
		require.NoError(t, err)

		info, err := manager.GetBucketInfo(ctx, "tenant-1", "info-bucket")
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, "info-bucket", info.Name)
		assert.Equal(t, "tenant-1", info.TenantID)
		assert.NotZero(t, info.CreatedAt)
	})

	t.Run("Get non-existent bucket", func(t *testing.T) {
		info, err := manager.GetBucketInfo(ctx, "tenant-1", "missing-bucket")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrBucketNotFound)
		assert.Nil(t, info)
	})
}

// TestListBuckets tests bucket listing
func TestListBuckets(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	// Create buckets for different tenants
	buckets := []struct {
		tenantID string
		name     string
	}{
		{"tenant-1", "bucket-a"},
		{"tenant-1", "bucket-b"},
		{"tenant-2", "bucket-c"},
		{"tenant-2", "bucket-d"},
		{"", "global-bucket"},
	}

	for _, b := range buckets {
		err := manager.CreateBucket(ctx, b.tenantID, b.name, "")
		require.NoError(t, err)
	}

	t.Run("List buckets for tenant-1", func(t *testing.T) {
		list, err := manager.ListBuckets(ctx, "tenant-1")
		assert.NoError(t, err)
		assert.Len(t, list, 2)

		names := []string{list[0].Name, list[1].Name}
		assert.Contains(t, names, "bucket-a")
		assert.Contains(t, names, "bucket-b")
	})

	t.Run("List buckets for tenant-2", func(t *testing.T) {
		list, err := manager.ListBuckets(ctx, "tenant-2")
		assert.NoError(t, err)
		assert.Len(t, list, 2)

		names := []string{list[0].Name, list[1].Name}
		assert.Contains(t, names, "bucket-c")
		assert.Contains(t, names, "bucket-d")
	})

	t.Run("List global buckets", func(t *testing.T) {
		list, err := manager.ListBuckets(ctx, "")
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(list), 1)

		found := false
		for _, b := range list {
			if b.Name == "global-bucket" {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("List buckets for non-existent tenant", func(t *testing.T) {
		list, err := manager.ListBuckets(ctx, "nonexistent")
		assert.NoError(t, err)
		assert.Empty(t, list)
	})
}

// TestDeleteBucket tests bucket deletion
func TestDeleteBucket(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("Delete empty bucket", func(t *testing.T) {
		err := manager.CreateBucket(ctx, "tenant-1", "delete-bucket", "")
		require.NoError(t, err)

		err = manager.DeleteBucket(ctx, "tenant-1", "delete-bucket")
		assert.NoError(t, err)

		// Verify bucket is gone
		exists, err := manager.BucketExists(ctx, "tenant-1", "delete-bucket")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("Delete non-existent bucket", func(t *testing.T) {
		err := manager.DeleteBucket(ctx, "tenant-1", "never-existed")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrBucketNotFound)
	})
}

// TestBucketExists tests bucket existence check
func TestBucketExists(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("Check existing bucket", func(t *testing.T) {
		err := manager.CreateBucket(ctx, "tenant-1", "exists-bucket", "")
		require.NoError(t, err)

		exists, err := manager.BucketExists(ctx, "tenant-1", "exists-bucket")
		assert.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("Check non-existent bucket", func(t *testing.T) {
		exists, err := manager.BucketExists(ctx, "tenant-1", "not-exists")
		assert.NoError(t, err)
		assert.False(t, exists)
	})
}

// TestBucketPolicy tests bucket policy operations
func TestBucketPolicy(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket first
	err := manager.CreateBucket(ctx, "tenant-1", "policy-bucket", "")
	require.NoError(t, err)

	t.Run("Set and get bucket policy", func(t *testing.T) {
		policy := &Policy{
			Version: "2012-10-17",
			Statement: []Statement{
				{
					Effect:   "Allow",
					Action:   "s3:GetObject",
					Resource: "arn:aws:s3:::policy-bucket/*",
				},
			},
		}

		err := manager.SetBucketPolicy(ctx, "tenant-1", "policy-bucket", policy)
		assert.NoError(t, err)

		retrieved, err := manager.GetBucketPolicy(ctx, "tenant-1", "policy-bucket")
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, "2012-10-17", retrieved.Version)
		assert.Len(t, retrieved.Statement, 1)
	})

	t.Run("Delete bucket policy", func(t *testing.T) {
		err := manager.DeleteBucketPolicy(ctx, "tenant-1", "policy-bucket")
		assert.NoError(t, err)

		policy, err := manager.GetBucketPolicy(ctx, "tenant-1", "policy-bucket")
		assert.Error(t, err)
		assert.Nil(t, policy)
	})

	t.Run("Get policy for bucket without policy", func(t *testing.T) {
		err := manager.CreateBucket(ctx, "tenant-1", "no-policy-bucket", "")
		require.NoError(t, err)

		policy, err := manager.GetBucketPolicy(ctx, "tenant-1", "no-policy-bucket")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrPolicyNotFound)
		assert.Nil(t, policy)
	})
}

// TestBucketVersioning tests versioning operations
func TestBucketVersioning(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	err := manager.CreateBucket(ctx, "tenant-1", "versioning-bucket", "")
	require.NoError(t, err)

	t.Run("Set and get versioning config", func(t *testing.T) {
		config := &VersioningConfig{
			Status: "Enabled",
		}

		err := manager.SetVersioning(ctx, "tenant-1", "versioning-bucket", config)
		assert.NoError(t, err)

		retrieved, err := manager.GetVersioning(ctx, "tenant-1", "versioning-bucket")
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, "Enabled", retrieved.Status)
	})

	t.Run("Suspend versioning", func(t *testing.T) {
		config := &VersioningConfig{
			Status: "Suspended",
		}

		err := manager.SetVersioning(ctx, "tenant-1", "versioning-bucket", config)
		assert.NoError(t, err)

		retrieved, err := manager.GetVersioning(ctx, "tenant-1", "versioning-bucket")
		assert.NoError(t, err)
		assert.Equal(t, "Suspended", retrieved.Status)
	})
}

// TestBucketLifecycle tests lifecycle operations
func TestBucketLifecycle(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	err := manager.CreateBucket(ctx, "tenant-1", "lifecycle-bucket", "")
	require.NoError(t, err)

	t.Run("Set and get lifecycle config", func(t *testing.T) {
		days := 30
		lifecycle := &LifecycleConfig{
			Rules: []LifecycleRule{
				{
					ID:     "expire-old-objects",
					Status: "Enabled",
					Filter: LifecycleFilter{
						Prefix: "logs/",
					},
					Expiration: &LifecycleExpiration{
						Days: &days,
					},
				},
			},
		}

		err := manager.SetLifecycle(ctx, "tenant-1", "lifecycle-bucket", lifecycle)
		assert.NoError(t, err)

		retrieved, err := manager.GetLifecycle(ctx, "tenant-1", "lifecycle-bucket")
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Len(t, retrieved.Rules, 1)
		assert.Equal(t, "expire-old-objects", retrieved.Rules[0].ID)
	})

	t.Run("Delete lifecycle config", func(t *testing.T) {
		err := manager.DeleteLifecycle(ctx, "tenant-1", "lifecycle-bucket")
		assert.NoError(t, err)

		lifecycle, err := manager.GetLifecycle(ctx, "tenant-1", "lifecycle-bucket")
		assert.Error(t, err)
		assert.Nil(t, lifecycle)
	})
}

// TestBucketCORS tests CORS operations
func TestBucketCORS(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	err := manager.CreateBucket(ctx, "tenant-1", "cors-bucket", "")
	require.NoError(t, err)

	t.Run("Set and get CORS config", func(t *testing.T) {
		cors := &CORSConfig{
			CORSRules: []CORSRule{
				{
					AllowedMethods: []string{"GET", "PUT", "POST"},
					AllowedOrigins: []string{"https://example.com"},
					AllowedHeaders: []string{"*"},
				},
			},
		}

		err := manager.SetCORS(ctx, "tenant-1", "cors-bucket", cors)
		assert.NoError(t, err)

		retrieved, err := manager.GetCORS(ctx, "tenant-1", "cors-bucket")
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Len(t, retrieved.CORSRules, 1)
		assert.Equal(t, []string{"GET", "PUT", "POST"}, retrieved.CORSRules[0].AllowedMethods)
	})

	t.Run("Delete CORS config", func(t *testing.T) {
		err := manager.DeleteCORS(ctx, "tenant-1", "cors-bucket")
		assert.NoError(t, err)

		cors, err := manager.GetCORS(ctx, "tenant-1", "cors-bucket")
		assert.Error(t, err)
		assert.Nil(t, cors)
	})
}

// TestBucketTags tests bucket tagging
func TestBucketTags(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	err := manager.CreateBucket(ctx, "tenant-1", "tags-bucket", "")
	require.NoError(t, err)

	t.Run("Set bucket tags", func(t *testing.T) {
		tags := map[string]string{
			"environment": "production",
			"team":        "backend",
		}

		err := manager.SetBucketTags(ctx, "tenant-1", "tags-bucket", tags)
		assert.NoError(t, err)

		// Verify tags in bucket info
		info, err := manager.GetBucketInfo(ctx, "tenant-1", "tags-bucket")
		assert.NoError(t, err)
		assert.Equal(t, "production", info.Tags["environment"])
		assert.Equal(t, "backend", info.Tags["team"])
	})
}

// TestBucketMetrics tests object count and size metrics
func TestBucketMetrics(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	err := manager.CreateBucket(ctx, "tenant-1", "metrics-bucket", "")
	require.NoError(t, err)

	t.Run("Increment object count", func(t *testing.T) {
		err := manager.IncrementObjectCount(ctx, "tenant-1", "metrics-bucket", 1024)
		assert.NoError(t, err)

		info, err := manager.GetBucketInfo(ctx, "tenant-1", "metrics-bucket")
		assert.NoError(t, err)
		assert.Equal(t, int64(1), info.ObjectCount)
		assert.Equal(t, int64(1024), info.TotalSize)
	})

	t.Run("Increment multiple times", func(t *testing.T) {
		err := manager.IncrementObjectCount(ctx, "tenant-1", "metrics-bucket", 2048)
		assert.NoError(t, err)

		err = manager.IncrementObjectCount(ctx, "tenant-1", "metrics-bucket", 512)
		assert.NoError(t, err)

		info, err := manager.GetBucketInfo(ctx, "tenant-1", "metrics-bucket")
		assert.NoError(t, err)
		assert.Equal(t, int64(3), info.ObjectCount)
		assert.Equal(t, int64(3584), info.TotalSize) // 1024 + 2048 + 512
	})

	t.Run("Decrement object count", func(t *testing.T) {
		err := manager.DecrementObjectCount(ctx, "tenant-1", "metrics-bucket", 1024)
		assert.NoError(t, err)

		info, err := manager.GetBucketInfo(ctx, "tenant-1", "metrics-bucket")
		assert.NoError(t, err)
		assert.Equal(t, int64(2), info.ObjectCount)
		assert.Equal(t, int64(2560), info.TotalSize) // 3584 - 1024
	})
}

// TestUpdateBucket tests bucket updates
func TestUpdateBucket(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()

	err := manager.CreateBucket(ctx, "tenant-1", "update-bucket", "")
	require.NoError(t, err)

	t.Run("Update bucket metadata", func(t *testing.T) {
		info, err := manager.GetBucketInfo(ctx, "tenant-1", "update-bucket")
		require.NoError(t, err)

		info.Metadata = map[string]string{
			"updated": "true",
		}

		err = manager.UpdateBucket(ctx, "tenant-1", "update-bucket", info)
		assert.NoError(t, err)

		updated, err := manager.GetBucketInfo(ctx, "tenant-1", "update-bucket")
		assert.NoError(t, err)
		assert.Equal(t, "true", updated.Metadata["updated"])
	})
}

// TestIsReady tests health check
func TestIsReady(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()

	ready := manager.IsReady()
	assert.True(t, ready)
}
