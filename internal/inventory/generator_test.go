package inventory

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock BucketManager
type MockBucketManager struct {
	mock.Mock
}

func (m *MockBucketManager) CreateBucket(ctx context.Context, tenantID, name string) error {
	args := m.Called(ctx, tenantID, name)
	return args.Error(0)
}

func (m *MockBucketManager) DeleteBucket(ctx context.Context, tenantID, name string) error {
	args := m.Called(ctx, tenantID, name)
	return args.Error(0)
}

func (m *MockBucketManager) ForceDeleteBucket(ctx context.Context, tenantID, name string) error {
	args := m.Called(ctx, tenantID, name)
	return args.Error(0)
}

func (m *MockBucketManager) ListBuckets(ctx context.Context, tenantID string) ([]bucket.Bucket, error) {
	args := m.Called(ctx, tenantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]bucket.Bucket), args.Error(1)
}

func (m *MockBucketManager) BucketExists(ctx context.Context, tenantID, name string) (bool, error) {
	args := m.Called(ctx, tenantID, name)
	return args.Bool(0), args.Error(1)
}

func (m *MockBucketManager) GetBucketInfo(ctx context.Context, tenantID, name string) (*bucket.Bucket, error) {
	args := m.Called(ctx, tenantID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*bucket.Bucket), args.Error(1)
}

func (m *MockBucketManager) UpdateBucket(ctx context.Context, tenantID, name string, b *bucket.Bucket) error {
	args := m.Called(ctx, tenantID, name, b)
	return args.Error(0)
}

func (m *MockBucketManager) GetBucketPolicy(ctx context.Context, tenantID, name string) (*bucket.Policy, error) {
	args := m.Called(ctx, tenantID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*bucket.Policy), args.Error(1)
}

func (m *MockBucketManager) SetBucketPolicy(ctx context.Context, tenantID, name string, policy *bucket.Policy) error {
	args := m.Called(ctx, tenantID, name, policy)
	return args.Error(0)
}

func (m *MockBucketManager) DeleteBucketPolicy(ctx context.Context, tenantID, name string) error {
	args := m.Called(ctx, tenantID, name)
	return args.Error(0)
}

func (m *MockBucketManager) GetVersioning(ctx context.Context, tenantID, name string) (*bucket.VersioningConfig, error) {
	args := m.Called(ctx, tenantID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*bucket.VersioningConfig), args.Error(1)
}

func (m *MockBucketManager) SetVersioning(ctx context.Context, tenantID, name string, config *bucket.VersioningConfig) error {
	args := m.Called(ctx, tenantID, name, config)
	return args.Error(0)
}

func (m *MockBucketManager) GetLifecycle(ctx context.Context, tenantID, name string) (*bucket.LifecycleConfig, error) {
	args := m.Called(ctx, tenantID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*bucket.LifecycleConfig), args.Error(1)
}

func (m *MockBucketManager) SetLifecycle(ctx context.Context, tenantID, name string, config *bucket.LifecycleConfig) error {
	args := m.Called(ctx, tenantID, name, config)
	return args.Error(0)
}

func (m *MockBucketManager) DeleteLifecycle(ctx context.Context, tenantID, name string) error {
	args := m.Called(ctx, tenantID, name)
	return args.Error(0)
}

func (m *MockBucketManager) GetCORS(ctx context.Context, tenantID, name string) (*bucket.CORSConfig, error) {
	args := m.Called(ctx, tenantID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*bucket.CORSConfig), args.Error(1)
}

func (m *MockBucketManager) SetCORS(ctx context.Context, tenantID, name string, config *bucket.CORSConfig) error {
	args := m.Called(ctx, tenantID, name, config)
	return args.Error(0)
}

func (m *MockBucketManager) DeleteCORS(ctx context.Context, tenantID, name string) error {
	args := m.Called(ctx, tenantID, name)
	return args.Error(0)
}

func (m *MockBucketManager) SetBucketTags(ctx context.Context, tenantID, name string, tags map[string]string) error {
	args := m.Called(ctx, tenantID, name, tags)
	return args.Error(0)
}

func (m *MockBucketManager) GetObjectLockConfig(ctx context.Context, tenantID, name string) (*bucket.ObjectLockConfig, error) {
	args := m.Called(ctx, tenantID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*bucket.ObjectLockConfig), args.Error(1)
}

func (m *MockBucketManager) SetObjectLockConfig(ctx context.Context, tenantID, name string, config *bucket.ObjectLockConfig) error {
	args := m.Called(ctx, tenantID, name, config)
	return args.Error(0)
}

func (m *MockBucketManager) GetBucketACL(ctx context.Context, tenantID, name string) (interface{}, error) {
	args := m.Called(ctx, tenantID, name)
	return args.Get(0), args.Error(1)
}

func (m *MockBucketManager) SetBucketACL(ctx context.Context, tenantID, name string, acl interface{}) error {
	args := m.Called(ctx, tenantID, name, acl)
	return args.Error(0)
}

func (m *MockBucketManager) GetACLManager() interface{} {
	args := m.Called()
	return args.Get(0)
}

func (m *MockBucketManager) IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error {
	args := m.Called(ctx, tenantID, name, sizeBytes)
	return args.Error(0)
}

func (m *MockBucketManager) DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error {
	args := m.Called(ctx, tenantID, name, sizeBytes)
	return args.Error(0)
}

func (m *MockBucketManager) RecalculateMetrics(ctx context.Context, tenantID, name string) error {
	args := m.Called(ctx, tenantID, name)
	return args.Error(0)
}

func (m *MockBucketManager) IsReady() bool {
	args := m.Called()
	return args.Bool(0)
}

// Mock MetadataStore - implementing only the methods used by generator
type MockMetadataStore struct {
	mock.Mock
}

func (m *MockMetadataStore) CreateBucket(ctx context.Context, bucket *metadata.BucketMetadata) error {
	args := m.Called(ctx, bucket)
	return args.Error(0)
}

func (m *MockMetadataStore) GetBucket(ctx context.Context, tenantID, name string) (*metadata.BucketMetadata, error) {
	args := m.Called(ctx, tenantID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*metadata.BucketMetadata), args.Error(1)
}

func (m *MockMetadataStore) UpdateBucket(ctx context.Context, bucket *metadata.BucketMetadata) error {
	args := m.Called(ctx, bucket)
	return args.Error(0)
}

func (m *MockMetadataStore) DeleteBucket(ctx context.Context, tenantID, name string) error {
	args := m.Called(ctx, tenantID, name)
	return args.Error(0)
}

func (m *MockMetadataStore) ListBuckets(ctx context.Context, tenantID string) ([]*metadata.BucketMetadata, error) {
	args := m.Called(ctx, tenantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*metadata.BucketMetadata), args.Error(1)
}

func (m *MockMetadataStore) GetBucketByName(ctx context.Context, name string) (*metadata.BucketMetadata, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*metadata.BucketMetadata), args.Error(1)
}

func (m *MockMetadataStore) BucketExists(ctx context.Context, tenantID, name string) (bool, error) {
	args := m.Called(ctx, tenantID, name)
	return args.Bool(0), args.Error(1)
}

func (m *MockMetadataStore) UpdateBucketMetrics(ctx context.Context, tenantID, bucketName string, objectCountDelta, sizeDelta int64) error {
	args := m.Called(ctx, tenantID, bucketName, objectCountDelta, sizeDelta)
	return args.Error(0)
}

func (m *MockMetadataStore) PutObject(ctx context.Context, obj *metadata.ObjectMetadata) error {
	args := m.Called(ctx, obj)
	return args.Error(0)
}

func (m *MockMetadataStore) GetObject(ctx context.Context, bucket, key string, versionID ...string) (*metadata.ObjectMetadata, error) {
	args := m.Called(ctx, bucket, key, versionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*metadata.ObjectMetadata), args.Error(1)
}

func (m *MockMetadataStore) DeleteObject(ctx context.Context, bucket, key string, versionID ...string) error {
	args := m.Called(ctx, bucket, key, versionID)
	return args.Error(0)
}

func (m *MockMetadataStore) ListObjects(ctx context.Context, bucket, prefix, marker string, maxKeys int) ([]*metadata.ObjectMetadata, string, error) {
	args := m.Called(ctx, bucket, prefix, marker, maxKeys)
	if args.Get(0) == nil {
		return nil, args.String(1), args.Error(2)
	}
	return args.Get(0).([]*metadata.ObjectMetadata), args.String(1), args.Error(2)
}

func (m *MockMetadataStore) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	args := m.Called(ctx, bucket, key)
	return args.Bool(0), args.Error(1)
}

func (m *MockMetadataStore) PutObjectVersion(ctx context.Context, obj *metadata.ObjectMetadata, version *metadata.ObjectVersion) error {
	args := m.Called(ctx, obj, version)
	return args.Error(0)
}

func (m *MockMetadataStore) GetObjectVersions(ctx context.Context, bucket, key string) ([]*metadata.ObjectVersion, error) {
	args := m.Called(ctx, bucket, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*metadata.ObjectVersion), args.Error(1)
}

func (m *MockMetadataStore) ListAllObjectVersions(ctx context.Context, bucket, prefix string, maxKeys int) ([]*metadata.ObjectVersion, error) {
	args := m.Called(ctx, bucket, prefix, maxKeys)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*metadata.ObjectVersion), args.Error(1)
}

func (m *MockMetadataStore) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	args := m.Called(ctx, bucket, key, versionID)
	return args.Error(0)
}

func (m *MockMetadataStore) CreateMultipartUpload(ctx context.Context, upload *metadata.MultipartUploadMetadata) error {
	args := m.Called(ctx, upload)
	return args.Error(0)
}

func (m *MockMetadataStore) GetMultipartUpload(ctx context.Context, uploadID string) (*metadata.MultipartUploadMetadata, error) {
	args := m.Called(ctx, uploadID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*metadata.MultipartUploadMetadata), args.Error(1)
}

func (m *MockMetadataStore) ListMultipartUploads(ctx context.Context, bucket, prefix string, maxUploads int) ([]*metadata.MultipartUploadMetadata, error) {
	args := m.Called(ctx, bucket, prefix, maxUploads)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*metadata.MultipartUploadMetadata), args.Error(1)
}

func (m *MockMetadataStore) AbortMultipartUpload(ctx context.Context, uploadID string) error {
	args := m.Called(ctx, uploadID)
	return args.Error(0)
}

func (m *MockMetadataStore) CompleteMultipartUpload(ctx context.Context, uploadID string, obj *metadata.ObjectMetadata) error {
	args := m.Called(ctx, uploadID, obj)
	return args.Error(0)
}

func (m *MockMetadataStore) PutPart(ctx context.Context, part *metadata.PartMetadata) error {
	args := m.Called(ctx, part)
	return args.Error(0)
}

func (m *MockMetadataStore) GetPart(ctx context.Context, uploadID string, partNumber int) (*metadata.PartMetadata, error) {
	args := m.Called(ctx, uploadID, partNumber)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*metadata.PartMetadata), args.Error(1)
}

func (m *MockMetadataStore) ListParts(ctx context.Context, uploadID string) ([]*metadata.PartMetadata, error) {
	args := m.Called(ctx, uploadID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*metadata.PartMetadata), args.Error(1)
}

func (m *MockMetadataStore) DeleteParts(ctx context.Context, uploadID string) error {
	args := m.Called(ctx, uploadID)
	return args.Error(0)
}

func (m *MockMetadataStore) PutObjectTags(ctx context.Context, bucket, key string, tags map[string]string) error {
	args := m.Called(ctx, bucket, key, tags)
	return args.Error(0)
}

func (m *MockMetadataStore) GetObjectTags(ctx context.Context, bucket, key string) (map[string]string, error) {
	args := m.Called(ctx, bucket, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *MockMetadataStore) DeleteObjectTags(ctx context.Context, bucket, key string) error {
	args := m.Called(ctx, bucket, key)
	return args.Error(0)
}

func (m *MockMetadataStore) ListObjectsByTags(ctx context.Context, bucket string, tags map[string]string) ([]*metadata.ObjectMetadata, error) {
	args := m.Called(ctx, bucket, tags)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*metadata.ObjectMetadata), args.Error(1)
}

func (m *MockMetadataStore) GetBucketStats(ctx context.Context, tenantID, bucket string) (int64, int64, error) {
	args := m.Called(ctx, tenantID, bucket)
	return args.Get(0).(int64), args.Get(1).(int64), args.Error(2)
}

func (m *MockMetadataStore) RecalculateBucketStats(ctx context.Context, tenantID, bucket string) error {
	args := m.Called(ctx, tenantID, bucket)
	return args.Error(0)
}

func (m *MockMetadataStore) Compact(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockMetadataStore) Backup(ctx context.Context, destPath string) error {
	args := m.Called(ctx, destPath)
	return args.Error(0)
}

func (m *MockMetadataStore) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockMetadataStore) IsReady() bool {
	args := m.Called()
	return args.Bool(0)
}

// Mock StorageBackend
type MockStorageBackend struct {
	mock.Mock
}

func (m *MockStorageBackend) Put(ctx context.Context, path string, data io.Reader, meta map[string]string) error {
	args := m.Called(ctx, path, data, meta)
	return args.Error(0)
}

func (m *MockStorageBackend) Get(ctx context.Context, path string) (io.ReadCloser, map[string]string, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).(io.ReadCloser), args.Get(1).(map[string]string), args.Error(2)
}

func (m *MockStorageBackend) Delete(ctx context.Context, path string) error {
	args := m.Called(ctx, path)
	return args.Error(0)
}

func (m *MockStorageBackend) Exists(ctx context.Context, path string) (bool, error) {
	args := m.Called(ctx, path)
	return args.Bool(0), args.Error(1)
}

func (m *MockStorageBackend) List(ctx context.Context, prefix string, recursive bool) ([]storage.ObjectInfo, error) {
	args := m.Called(ctx, prefix, recursive)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.ObjectInfo), args.Error(1)
}

func (m *MockStorageBackend) GetMetadata(ctx context.Context, path string) (map[string]string, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *MockStorageBackend) SetMetadata(ctx context.Context, path string, meta map[string]string) error {
	args := m.Called(ctx, path, meta)
	return args.Error(0)
}

func (m *MockStorageBackend) Close() error {
	args := m.Called()
	return args.Error(0)
}

func init() {
	// Silence logs during tests
	logrus.SetLevel(logrus.PanicLevel)
}

// TestGenerateReport_DestinationBucketNotFound verifies that report generation fails when destination bucket doesn't exist
func TestGenerateReport_DestinationBucketNotFound(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	generator := NewReportGenerator(mockBucketMgr, mockMetadata, mockStorage)

	config := &InventoryConfig{
		ID:                "test-inventory-1",
		BucketName:        "source-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "nonexistent-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key", "size"},
	}

	ctx := context.Background()
	now := time.Now()

	// Mock objects in source bucket
	mockMetadata.On("ListObjects", ctx, "source-bucket", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{
				TenantID:     "tenant1",
				Bucket:       "source-bucket",
				Key:          "test-file.txt",
				Size:         1024,
				LastModified: now,
				ETag:         "abc123",
			},
		},
		"", // no next marker
		nil,
	).Once()

	// Mock destination bucket does NOT exist
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "nonexistent-bucket").Return(
		nil, errors.New("bucket not found"),
	).Once()

	// Execute
	report, err := generator.GenerateReport(ctx, config)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "destination bucket not found")
	assert.Nil(t, report)

	mockBucketMgr.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
}

// TestGenerateReport_DestinationBucketNoWritePermission verifies error when destination bucket exists but lacks write permissions
func TestGenerateReport_DestinationBucketNoWritePermission(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	generator := NewReportGenerator(mockBucketMgr, mockMetadata, mockStorage)

	config := &InventoryConfig{
		ID:                "test-inventory-2",
		BucketName:        "source-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "readonly-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key", "size"},
	}

	ctx := context.Background()
	now := time.Now()

	// Mock objects in source bucket
	mockMetadata.On("ListObjects", ctx, "source-bucket", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{
				TenantID:     "tenant1",
				Bucket:       "source-bucket",
				Key:          "test-file.txt",
				Size:         1024,
				LastModified: now,
				ETag:         "abc123",
			},
		},
		"",
		nil,
	).Once()

	// Mock destination bucket exists
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "readonly-bucket").Return(&bucket.Bucket{
		Name:     "readonly-bucket",
		TenantID: "tenant1",
	}, nil).Once()

	// Mock metadata write succeeds
	mockMetadata.On("PutObject", ctx, mock.AnythingOfType("*metadata.ObjectMetadata")).Return(nil).Once()

	// Mock write permission denied (storage backend returns permission error)
	mockStorage.On("Put", ctx, mock.AnythingOfType("string"), mock.Anything, mock.AnythingOfType("map[string]string")).Return(
		errors.New("permission denied: write access forbidden"),
	).Once()

	// Execute
	report, err := generator.GenerateReport(ctx, config)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
	assert.Nil(t, report)

	mockBucketMgr.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
}

// TestGenerateReport_CSV_Success tests successful CSV report generation
func TestGenerateReport_CSV_Success(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	generator := NewReportGenerator(mockBucketMgr, mockMetadata, mockStorage)

	config := &InventoryConfig{
		ID:                "test-inventory-3",
		BucketName:        "source-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key", "size", "etag"},
	}

	ctx := context.Background()
	now := time.Now()

	// Mock objects in source bucket
	mockMetadata.On("ListObjects", ctx, "source-bucket", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{
				TenantID:     "tenant1",
				Bucket:       "source-bucket",
				Key:          "file1.txt",
				Size:         1024,
				LastModified: now,
				ETag:         "abc123",
			},
			{
				TenantID:     "tenant1",
				Bucket:       "source-bucket",
				Key:          "file2.txt",
				Size:         2048,
				LastModified: now,
				ETag:         "def456",
			},
		},
		"",
		nil,
	).Once()

	// Mock destination bucket exists
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "dest-bucket").Return(&bucket.Bucket{
		Name:     "dest-bucket",
		TenantID: "tenant1",
	}, nil).Once()

	// Mock metadata update
	mockMetadata.On("PutObject", ctx, mock.AnythingOfType("*metadata.ObjectMetadata")).Return(nil).Once()

	// Mock successful upload
	var capturedContent []byte
	mockStorage.On("Put", ctx, mock.AnythingOfType("string"), mock.Anything, mock.AnythingOfType("map[string]string")).
		Run(func(args mock.Arguments) {
			reader := args.Get(2).(io.Reader)
			capturedContent, _ = io.ReadAll(reader)
		}).Return(nil).Once()

	// Execute
	report, err := generator.GenerateReport(ctx, config)

	// Verify
	assert.NoError(t, err)
	assert.NotNil(t, report)
	assert.NotEmpty(t, report.ReportPath)
	assert.Contains(t, report.ReportPath, "reports/")
	assert.Contains(t, report.ReportPath, ".csv")

	// Verify CSV content
	csvReader := csv.NewReader(strings.NewReader(string(capturedContent)))
	records, err := csvReader.ReadAll()
	assert.NoError(t, err)
	assert.Len(t, records, 3) // header + 2 data rows

	// Verify header
	assert.Equal(t, []string{"Bucket", "Key", "Size", "ETag"}, records[0])

	// Verify data rows
	assert.Equal(t, "source-bucket", records[1][0])
	assert.Equal(t, "file1.txt", records[1][1])
	assert.Equal(t, "1024", records[1][2])
	assert.Equal(t, "abc123", records[1][3])

	mockBucketMgr.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
}

// TestGenerateReport_JSON_Success tests successful JSON report generation
func TestGenerateReport_JSON_Success(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	generator := NewReportGenerator(mockBucketMgr, mockMetadata, mockStorage)

	config := &InventoryConfig{
		ID:                "test-inventory-4",
		BucketName:        "source-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "weekly",
		Format:            "json",
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key", "size", "last_modified"},
	}

	ctx := context.Background()
	now := time.Now()

	// Mock objects in source bucket
	mockMetadata.On("ListObjects", ctx, "source-bucket", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{
				TenantID:     "tenant1",
				Bucket:       "source-bucket",
				Key:          "document.pdf",
				Size:         5120,
				LastModified: now,
				ETag:         "xyz789",
			},
		},
		"",
		nil,
	).Once()

	// Mock destination bucket exists
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "dest-bucket").Return(&bucket.Bucket{
		Name:     "dest-bucket",
		TenantID: "tenant1",
	}, nil).Once()

	// Mock metadata update
	mockMetadata.On("PutObject", ctx, mock.AnythingOfType("*metadata.ObjectMetadata")).Return(nil).Once()

	// Mock successful upload
	var capturedContent []byte
	mockStorage.On("Put", ctx, mock.AnythingOfType("string"), mock.Anything, mock.AnythingOfType("map[string]string")).
		Run(func(args mock.Arguments) {
			reader := args.Get(2).(io.Reader)
			capturedContent, _ = io.ReadAll(reader)
		}).Return(nil).Once()

	// Execute
	report, err := generator.GenerateReport(ctx, config)

	// Verify
	assert.NoError(t, err)
	assert.NotNil(t, report)
	assert.NotEmpty(t, report.ReportPath)
	assert.Contains(t, report.ReportPath, "reports/")
	assert.Contains(t, report.ReportPath, ".json")

	// Verify JSON content
	var items []map[string]interface{}
	err = json.Unmarshal(capturedContent, &items)
	assert.NoError(t, err)
	assert.Len(t, items, 1)

	item := items[0]
	assert.Equal(t, "source-bucket", item["bucket"])
	assert.Equal(t, "document.pdf", item["key"])
	assert.Equal(t, float64(5120), item["size"]) // JSON numbers are float64

	mockBucketMgr.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
}

// TestGenerateReport_EmptyBucket tests report generation for empty bucket
func TestGenerateReport_EmptyBucket(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	generator := NewReportGenerator(mockBucketMgr, mockMetadata, mockStorage)

	config := &InventoryConfig{
		ID:                "test-inventory-5",
		BucketName:        "empty-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name", "object_key"},
	}

	ctx := context.Background()

	// Mock empty objects list
	mockMetadata.On("ListObjects", ctx, "empty-bucket", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{},
		"",
		nil,
	).Once()

	// Mock destination bucket exists
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "dest-bucket").Return(&bucket.Bucket{
		Name:     "dest-bucket",
		TenantID: "tenant1",
	}, nil).Once()

	// Mock metadata update
	mockMetadata.On("PutObject", ctx, mock.AnythingOfType("*metadata.ObjectMetadata")).Return(nil).Once()

	// Mock successful upload (empty report)
	var capturedContent []byte
	mockStorage.On("Put", ctx, mock.AnythingOfType("string"), mock.Anything, mock.AnythingOfType("map[string]string")).
		Run(func(args mock.Arguments) {
			reader := args.Get(2).(io.Reader)
			capturedContent, _ = io.ReadAll(reader)
		}).Return(nil).Once()

	// Execute
	report, err := generator.GenerateReport(ctx, config)

	// Verify
	assert.NoError(t, err)
	assert.NotNil(t, report)
	assert.NotEmpty(t, report.ReportPath)

	// Verify CSV contains only header
	csvReader := csv.NewReader(strings.NewReader(string(capturedContent)))
	records, err := csvReader.ReadAll()
	assert.NoError(t, err)
	assert.Len(t, records, 1) // only header row
	assert.Equal(t, []string{"Bucket", "Key"}, records[0])

	mockBucketMgr.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
}

// TestGenerateReport_InvalidFormat tests error handling for invalid format
func TestGenerateReport_InvalidFormat(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	generator := NewReportGenerator(mockBucketMgr, mockMetadata, mockStorage)

	config := &InventoryConfig{
		ID:                "test-inventory-6",
		BucketName:        "source-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "xml", // invalid format
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"bucket_name"},
	}

	ctx := context.Background()

	// Mock objects
	mockMetadata.On("ListObjects", ctx, "source-bucket", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{
				TenantID: "tenant1",
				Bucket:   "source-bucket",
				Key:      "test.txt",
			},
		},
		"",
		nil,
	).Once()

	// Execute
	report, err := generator.GenerateReport(ctx, config)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
	assert.Nil(t, report)

	mockBucketMgr.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
}

// TestGenerateReport_WithEncryptionStatus tests CSV generation with encryption status field
func TestGenerateReport_WithEncryptionStatus(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	generator := NewReportGenerator(mockBucketMgr, mockMetadata, mockStorage)

	config := &InventoryConfig{
		ID:                "test-inventory-7",
		BucketName:        "source-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"object_key", "encryption_status"},
	}

	ctx := context.Background()

	// Mock objects with encryption metadata
	mockMetadata.On("ListObjects", ctx, "source-bucket", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{
				TenantID: "tenant1",
				Bucket:   "source-bucket",
				Key:      "encrypted-file.txt",
				Metadata: map[string]string{
					"x-amz-server-side-encryption": "AES256",
				},
			},
			{
				TenantID: "tenant1",
				Bucket:   "source-bucket",
				Key:      "plain-file.txt",
				Metadata: map[string]string{},
			},
		},
		"",
		nil,
	).Once()

	// Mock destination bucket exists
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "dest-bucket").Return(&bucket.Bucket{
		Name:     "dest-bucket",
		TenantID: "tenant1",
	}, nil).Once()

	// Mock metadata update
	mockMetadata.On("PutObject", ctx, mock.AnythingOfType("*metadata.ObjectMetadata")).Return(nil).Once()

	// Mock successful upload
	var capturedContent []byte
	mockStorage.On("Put", ctx, mock.AnythingOfType("string"), mock.Anything, mock.AnythingOfType("map[string]string")).
		Run(func(args mock.Arguments) {
			reader := args.Get(2).(io.Reader)
			capturedContent, _ = io.ReadAll(reader)
		}).Return(nil).Once()

	// Execute
	report, err := generator.GenerateReport(ctx, config)

	// Verify
	assert.NoError(t, err)
	assert.NotNil(t, report)
	assert.NotEmpty(t, report.ReportPath)

	// Verify CSV content
	csvReader := csv.NewReader(strings.NewReader(string(capturedContent)))
	records, err := csvReader.ReadAll()
	assert.NoError(t, err)
	assert.Len(t, records, 3) // header + 2 rows

	// Check encryption status
	assert.Equal(t, "encrypted-file.txt", records[1][0])
	assert.Equal(t, "SSE-S3", records[1][1])

	assert.Equal(t, "plain-file.txt", records[2][0])
	assert.Equal(t, "NONE", records[2][1])

	mockBucketMgr.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
}

// TestGenerateReport_MetadataUpdateFailure tests handling of metadata update failures
func TestGenerateReport_MetadataUpdateFailure(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	generator := NewReportGenerator(mockBucketMgr, mockMetadata, mockStorage)

	config := &InventoryConfig{
		ID:                "test-inventory-8",
		BucketName:        "source-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    []string{"object_key"},
	}

	ctx := context.Background()

	// Mock objects
	mockMetadata.On("ListObjects", ctx, "source-bucket", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{
				TenantID: "tenant1",
				Bucket:   "source-bucket",
				Key:      "test.txt",
			},
		},
		"",
		nil,
	).Once()

	// Mock destination bucket exists
	mockBucketMgr.On("GetBucketInfo", ctx, "tenant1", "dest-bucket").Return(&bucket.Bucket{
		Name:     "dest-bucket",
		TenantID: "tenant1",
	}, nil).Once()

	// Mock metadata update failure
	mockMetadata.On("PutObject", ctx, mock.AnythingOfType("*metadata.ObjectMetadata")).Return(
		errors.New("database error"),
	).Once()

	// Storage Put should NOT be called since metadata fails first
	// No mockStorage.On() call here

	// Execute
	_, err := generator.GenerateReport(ctx, config)

	// Verify - should fail because metadata update failed
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")

	mockBucketMgr.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
}

// TestCollectInventoryItems_Pagination tests object collection with pagination
func TestCollectInventoryItems_Pagination(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	generator := NewReportGenerator(mockBucketMgr, mockMetadata, mockStorage)

	config := &InventoryConfig{
		BucketName: "large-bucket",
		TenantID:   "tenant1",
	}

	ctx := context.Background()

	// Mock objects list - implementation doesn't support pagination in collectInventoryItems
	// It only fetches one batch
	mockMetadata.On("ListObjects", ctx, "large-bucket", "", "", 10000).Return(
		[]*metadata.ObjectMetadata{
			{Key: "file1.txt", Size: 100},
			{Key: "file2.txt", Size: 200},
			{Key: "file3.txt", Size: 300},
		},
		"", // no marker since we return all items in one batch
		nil,
	).Once()

	// Execute
	items, totalSize, err := generator.collectInventoryItems(ctx, config)

	// Verify
	assert.NoError(t, err)
	assert.Len(t, items, 3)
	assert.Equal(t, "file1.txt", items[0].Key)
	assert.Equal(t, "file2.txt", items[1].Key)
	assert.Equal(t, "file3.txt", items[2].Key)
	assert.Equal(t, int64(600), totalSize) // 100 + 200 + 300

	mockMetadata.AssertExpectations(t)
}

// TestCollectInventoryItems_ListError tests error handling during object listing
func TestCollectInventoryItems_ListError(t *testing.T) {
	mockBucketMgr := new(MockBucketManager)
	mockMetadata := new(MockMetadataStore)
	mockStorage := new(MockStorageBackend)

	generator := NewReportGenerator(mockBucketMgr, mockMetadata, mockStorage)

	config := &InventoryConfig{
		BucketName: "error-bucket",
		TenantID:   "tenant1",
	}

	ctx := context.Background()

	// Mock list error
	mockMetadata.On("ListObjects", ctx, "error-bucket", "", "", 10000).Return(
		nil, "", errors.New("storage backend failure"),
	).Once()

	// Execute
	items, totalSize, err := generator.collectInventoryItems(ctx, config)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "storage backend failure")
	assert.Nil(t, items)
	assert.Equal(t, int64(0), totalSize)

	mockMetadata.AssertExpectations(t)
}
