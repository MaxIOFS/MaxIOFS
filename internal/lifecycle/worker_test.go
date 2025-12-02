package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
)

// mockBucketMgr embeds the real interface to implement it automatically with defaults
type mockBucketMgr struct {
	bucket.Manager
	buckets      []bucket.Bucket
	listErr      error
	getBucket    *bucket.Bucket
	getBucketErr error
}

func (m *mockBucketMgr) ListBuckets(ctx context.Context, tenantID string) ([]bucket.Bucket, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.buckets, nil
}

func (m *mockBucketMgr) GetBucketInfo(ctx context.Context, tenantID, name string) (*bucket.Bucket, error) {
	if m.getBucketErr != nil {
		return nil, m.getBucketErr
	}
	if m.getBucket != nil {
		return m.getBucket, nil
	}
	return &bucket.Bucket{Name: name, TenantID: tenantID}, nil
}

// mockObjectMgr embeds the real interface
type mockObjectMgr struct {
	object.Manager
	listResult   *object.ListObjectsResult
	listErr      error
	versions     []object.ObjectVersion
	versionsErr  error
	deleteErr    error
	deleteCount  int
}

func (m *mockObjectMgr) ListObjects(ctx context.Context, bucketPath, prefix, delimiter, marker string, maxKeys int) (*object.ListObjectsResult, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listResult != nil {
		return m.listResult, nil
	}
	return &object.ListObjectsResult{Objects: []object.Object{}}, nil
}

func (m *mockObjectMgr) GetObjectVersions(ctx context.Context, bucketPath, key string) ([]object.ObjectVersion, error) {
	if m.versionsErr != nil {
		return nil, m.versionsErr
	}
	return m.versions, nil
}

func (m *mockObjectMgr) DeleteObject(ctx context.Context, bucketPath, key string, bypassGovernance bool, versionID ...string) (string, error) {
	if m.deleteErr != nil {
		return "", m.deleteErr
	}
	m.deleteCount++
	if len(versionID) > 0 {
		return versionID[0], nil
	}
	return "", nil
}

func (m *mockObjectMgr) IsReady() bool {
	return true
}

// mockMetaStore embeds the real interface
type mockMetaStore struct {
	metadata.Store
}

// TestNewWorker tests worker creation
func TestNewWorker(t *testing.T) {
	bucketMgr := &mockBucketMgr{}
	objMgr := &mockObjectMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)

	assert.NotNil(t, worker)
	assert.NotNil(t, worker.stopChan)
	assert.Equal(t, bucketMgr, worker.bucketManager)
	assert.Equal(t, objMgr, worker.objectManager)
	assert.Equal(t, metaStore, worker.metadataStore)
}

// TestWorkerStartStop tests worker start and stop
func TestWorkerStartStop(t *testing.T) {
	bucketMgr := &mockBucketMgr{}
	objMgr := &mockObjectMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	// Start worker with short interval
	worker.Start(ctx, 100*time.Millisecond)

	// Wait a bit to ensure it's running
	time.Sleep(50 * time.Millisecond)

	// Stop worker
	worker.Stop()

	// Wait for goroutine to finish
	time.Sleep(50 * time.Millisecond)

	// Worker should be stopped
	assert.NotNil(t, worker.ticker)
}

// TestWorkerContextCancellation tests worker stops on context cancellation
func TestWorkerContextCancellation(t *testing.T) {
	bucketMgr := &mockBucketMgr{}
	objMgr := &mockObjectMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx, cancel := context.WithCancel(context.Background())

	// Start worker
	worker.Start(ctx, 100*time.Millisecond)

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for goroutine to finish
	time.Sleep(100 * time.Millisecond)

	// Worker should have stopped
	assert.NotNil(t, worker.ticker)
}

// TestProcessLifecyclePolicies_NoBuckets tests processing with no buckets
func TestProcessLifecyclePolicies_NoBuckets(t *testing.T) {
	bucketMgr := &mockBucketMgr{
		buckets: []bucket.Bucket{},
	}
	objMgr := &mockObjectMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	// Should not panic with empty bucket list
	worker.processLifecyclePolicies(ctx)
}

// TestProcessLifecyclePolicies_ListBucketsError tests error handling
func TestProcessLifecyclePolicies_ListBucketsError(t *testing.T) {
	bucketMgr := &mockBucketMgr{
		listErr: errors.New("failed to list buckets"),
	}
	objMgr := &mockObjectMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	// Should handle error gracefully
	worker.processLifecyclePolicies(ctx)
}

// TestProcessLifecyclePolicies_NoLifecycleConfig tests bucket without lifecycle
func TestProcessLifecyclePolicies_NoLifecycleConfig(t *testing.T) {
	bucketMgr := &mockBucketMgr{
		buckets: []bucket.Bucket{
			{Name: "test-bucket", TenantID: "tenant-1"},
		},
		getBucket: &bucket.Bucket{
			Name:      "test-bucket",
			TenantID:  "tenant-1",
			Lifecycle: nil, // No lifecycle config
		},
	}
	objMgr := &mockObjectMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	// Should skip bucket without lifecycle config
	worker.processLifecyclePolicies(ctx)
}

// TestProcessLifecyclePolicies_DisabledRule tests disabled lifecycle rule
func TestProcessLifecyclePolicies_DisabledRule(t *testing.T) {
	bucketMgr := &mockBucketMgr{
		buckets: []bucket.Bucket{
			{Name: "test-bucket", TenantID: "tenant-1"},
		},
		getBucket: &bucket.Bucket{
			Name:     "test-bucket",
			TenantID: "tenant-1",
			Lifecycle: &bucket.LifecycleConfig{
				Rules: []bucket.LifecycleRule{
					{
						ID:     "rule-1",
						Status: "Disabled", // Disabled
						Filter: bucket.LifecycleFilter{Prefix: ""},
					},
				},
			},
		},
	}
	objMgr := &mockObjectMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	// Should skip disabled rules
	worker.processLifecyclePolicies(ctx)
}

// TestProcessNoncurrentVersionExpiration_SkipLatestVersion tests that latest versions are never deleted
func TestProcessNoncurrentVersionExpiration_SkipLatestVersion(t *testing.T) {
	bucketMgr := &mockBucketMgr{}
	objMgr := &mockObjectMgr{
		listResult: &object.ListObjectsResult{
			Objects: []object.Object{{Key: "file.txt"}},
		},
		versions: []object.ObjectVersion{
			{
				Object: object.Object{
					VersionID:    "v1",
					LastModified: time.Now().AddDate(0, 0, -60), // 60 days old but latest
				},
				IsLatest: true,
			},
		},
	}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	rule := bucket.LifecycleRule{
		ID:     "expire-old-versions",
		Status: "Enabled",
		Filter: bucket.LifecycleFilter{Prefix: ""},
		NoncurrentVersionExpiration: &bucket.NoncurrentVersionExpiration{
			NoncurrentDays: 30,
		},
	}

	worker.processNoncurrentVersionExpiration(ctx, "test-bucket", rule)

	// Should not delete latest version even if old
	assert.Equal(t, 0, objMgr.deleteCount)
}

// TestProcessNoncurrentVersionExpiration_HandleError tests error handling
func TestProcessNoncurrentVersionExpiration_HandleError(t *testing.T) {
	bucketMgr := &mockBucketMgr{}
	objMgr := &mockObjectMgr{
		listErr: errors.New("failed to list objects"),
	}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	rule := bucket.LifecycleRule{
		ID:     "expire-old-versions",
		Status: "Enabled",
		Filter: bucket.LifecycleFilter{Prefix: ""},
		NoncurrentVersionExpiration: &bucket.NoncurrentVersionExpiration{
			NoncurrentDays: 30,
		},
	}

	// Should handle error gracefully
	worker.processNoncurrentVersionExpiration(ctx, "test-bucket", rule)
}

// TestProcessExpiredDeleteMarkers_DeleteExpiredMarker tests deletion of expired delete markers
func TestProcessExpiredDeleteMarkers_DeleteExpiredMarker(t *testing.T) {
	bucketMgr := &mockBucketMgr{}
	objMgr := &mockObjectMgr{
		listResult: &object.ListObjectsResult{
			Objects: []object.Object{
				{Key: "deleted-file.txt"},
			},
		},
		versions: []object.ObjectVersion{
			{
				Object: object.Object{
					VersionID: "dm1",
				},
				IsLatest:       true,
				IsDeleteMarker: true, // Only a delete marker remains
			},
		},
	}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	trueVal := true
	rule := bucket.LifecycleRule{
		ID:     "cleanup-delete-markers",
		Status: "Enabled",
		Filter: bucket.LifecycleFilter{Prefix: ""},
		Expiration: &bucket.LifecycleExpiration{
			ExpiredObjectDeleteMarker: &trueVal,
		},
	}

	worker.processExpiredDeleteMarkers(ctx, "test-bucket", rule)

	// Should delete the expired delete marker
	assert.Equal(t, 1, objMgr.deleteCount)
}

// TestProcessExpiredDeleteMarkers_KeepMarkerWithVersions tests that markers with other versions are kept
func TestProcessExpiredDeleteMarkers_KeepMarkerWithVersions(t *testing.T) {
	bucketMgr := &mockBucketMgr{}
	objMgr := &mockObjectMgr{
		listResult: &object.ListObjectsResult{
			Objects: []object.Object{{Key: "file.txt"}},
		},
		versions: []object.ObjectVersion{
			{
				Object: object.Object{
					VersionID: "dm1",
				},
				IsLatest:       true,
				IsDeleteMarker: true,
			},
			{
				Object: object.Object{
					VersionID: "v1",
				},
				IsLatest:       false,
				IsDeleteMarker: false, // There's a real version underneath
			},
		},
	}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	trueVal := true
	rule := bucket.LifecycleRule{
		ID:     "cleanup-delete-markers",
		Status: "Enabled",
		Filter: bucket.LifecycleFilter{Prefix: ""},
		Expiration: &bucket.LifecycleExpiration{
			ExpiredObjectDeleteMarker: &trueVal,
		},
	}

	worker.processExpiredDeleteMarkers(ctx, "test-bucket", rule)

	// Should NOT delete - there are other versions
	assert.Equal(t, 0, objMgr.deleteCount)
}

// TestProcessExpiredDeleteMarkers_KeepNonDeleteMarker tests that regular objects are not deleted
func TestProcessExpiredDeleteMarkers_KeepNonDeleteMarker(t *testing.T) {
	bucketMgr := &mockBucketMgr{}
	objMgr := &mockObjectMgr{
		listResult: &object.ListObjectsResult{
			Objects: []object.Object{{Key: "file.txt"}},
		},
		versions: []object.ObjectVersion{
			{
				Object: object.Object{
					VersionID: "v1",
				},
				IsLatest:       true,
				IsDeleteMarker: false, // Not a delete marker
			},
		},
	}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	trueVal := true
	rule := bucket.LifecycleRule{
		ID:     "cleanup-delete-markers",
		Status: "Enabled",
		Filter: bucket.LifecycleFilter{Prefix: ""},
		Expiration: &bucket.LifecycleExpiration{
			ExpiredObjectDeleteMarker: &trueVal,
		},
	}

	worker.processExpiredDeleteMarkers(ctx, "test-bucket", rule)

	// Should not delete - it's not a delete marker
	assert.Equal(t, 0, objMgr.deleteCount)
}
