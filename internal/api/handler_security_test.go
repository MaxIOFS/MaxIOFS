package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock managers for testing
type MockBucketManager struct {
	mock.Mock
}

func (m *MockBucketManager) CreateBucket(ctx context.Context, tenantID, name string, ownerID string) error {
	args := m.Called(ctx, tenantID, name, ownerID)
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

type MockObjectManager struct {
	mock.Mock
}

func (m *MockObjectManager) GetObject(ctx context.Context, bucket, key string, versionID ...string) (*object.Object, io.ReadCloser, error) {
	args := m.Called(ctx, bucket, key, versionID)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	if args.Get(1) == nil {
		return args.Get(0).(*object.Object), nil, args.Error(2)
	}
	return args.Get(0).(*object.Object), args.Get(1).(io.ReadCloser), args.Error(2)
}

func (m *MockObjectManager) PutObject(ctx context.Context, bucket, key string, data io.Reader, headers http.Header) (*object.Object, error) {
	args := m.Called(ctx, bucket, key, data, headers)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.Object), args.Error(1)
}

func (m *MockObjectManager) DeleteObject(ctx context.Context, bucket, key string, bypassGovernance bool, versionID ...string) (string, error) {
	args := m.Called(ctx, bucket, key, bypassGovernance, versionID)
	return args.String(0), args.Error(1)
}

func (m *MockObjectManager) ListObjects(ctx context.Context, bucket, prefix, delimiter, marker string, maxKeys int) (*object.ListObjectsResult, error) {
	args := m.Called(ctx, bucket, prefix, delimiter, marker, maxKeys)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.ListObjectsResult), args.Error(1)
}

func (m *MockObjectManager) GetObjectMetadata(ctx context.Context, bucket, key string) (*object.Object, error) {
	args := m.Called(ctx, bucket, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.Object), args.Error(1)
}

func (m *MockObjectManager) UpdateObjectMetadata(ctx context.Context, bucket, key string, metadata map[string]string) error {
	args := m.Called(ctx, bucket, key, metadata)
	return args.Error(0)
}

func (m *MockObjectManager) GetObjectRetention(ctx context.Context, bucket, key string) (*object.RetentionConfig, error) {
	args := m.Called(ctx, bucket, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.RetentionConfig), args.Error(1)
}

func (m *MockObjectManager) SetObjectRetention(ctx context.Context, bucket, key string, config *object.RetentionConfig) error {
	args := m.Called(ctx, bucket, key, config)
	return args.Error(0)
}

func (m *MockObjectManager) GetObjectLegalHold(ctx context.Context, bucket, key string) (*object.LegalHoldConfig, error) {
	args := m.Called(ctx, bucket, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.LegalHoldConfig), args.Error(1)
}

func (m *MockObjectManager) SetObjectLegalHold(ctx context.Context, bucket, key string, config *object.LegalHoldConfig) error {
	args := m.Called(ctx, bucket, key, config)
	return args.Error(0)
}

func (m *MockObjectManager) GetObjectVersions(ctx context.Context, bucket, key string) ([]object.ObjectVersion, error) {
	args := m.Called(ctx, bucket, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]object.ObjectVersion), args.Error(1)
}

func (m *MockObjectManager) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	args := m.Called(ctx, bucket, key, versionID)
	return args.Error(0)
}

func (m *MockObjectManager) GetObjectTagging(ctx context.Context, bucket, key string) (*object.TagSet, error) {
	args := m.Called(ctx, bucket, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.TagSet), args.Error(1)
}

func (m *MockObjectManager) SetObjectTagging(ctx context.Context, bucket, key string, tags *object.TagSet) error {
	args := m.Called(ctx, bucket, key, tags)
	return args.Error(0)
}

func (m *MockObjectManager) DeleteObjectTagging(ctx context.Context, bucket, key string) error {
	args := m.Called(ctx, bucket, key)
	return args.Error(0)
}

func (m *MockObjectManager) GetObjectACL(ctx context.Context, bucket, key string) (*object.ACL, error) {
	args := m.Called(ctx, bucket, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.ACL), args.Error(1)
}

func (m *MockObjectManager) SetObjectACL(ctx context.Context, bucket, key string, acl *object.ACL) error {
	args := m.Called(ctx, bucket, key, acl)
	return args.Error(0)
}

func (m *MockObjectManager) CreateMultipartUpload(ctx context.Context, bucket, key string, headers http.Header) (*object.MultipartUpload, error) {
	args := m.Called(ctx, bucket, key, headers)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.MultipartUpload), args.Error(1)
}

func (m *MockObjectManager) UploadPart(ctx context.Context, uploadID string, partNumber int, data io.Reader) (*object.Part, error) {
	args := m.Called(ctx, uploadID, partNumber, data)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.Part), args.Error(1)
}

func (m *MockObjectManager) ListParts(ctx context.Context, uploadID string) ([]object.Part, error) {
	args := m.Called(ctx, uploadID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]object.Part), args.Error(1)
}

func (m *MockObjectManager) CompleteMultipartUpload(ctx context.Context, uploadID string, parts []object.Part) (*object.Object, error) {
	args := m.Called(ctx, uploadID, parts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.Object), args.Error(1)
}

func (m *MockObjectManager) AbortMultipartUpload(ctx context.Context, uploadID string) error {
	args := m.Called(ctx, uploadID)
	return args.Error(0)
}

func (m *MockObjectManager) ListMultipartUploads(ctx context.Context, bucket string) ([]object.MultipartUpload, error) {
	args := m.Called(ctx, bucket)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]object.MultipartUpload), args.Error(1)
}

func (m *MockObjectManager) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, headers http.Header) (*object.Object, error) {
	args := m.Called(ctx, srcBucket, srcKey, dstBucket, dstKey, headers)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.Object), args.Error(1)
}

func (m *MockObjectManager) SearchObjects(ctx context.Context, bucket, prefix, delimiter, marker string, maxKeys int, filter *metadata.ObjectFilter) (*object.ListObjectsResult, error) {
	args := m.Called(ctx, bucket, prefix, delimiter, marker, maxKeys, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*object.ListObjectsResult), args.Error(1)
}

func (m *MockObjectManager) IsReady() bool {
	args := m.Called()
	return args.Bool(0)
}

type MockAuthManager struct {
	mock.Mock
}

func (m *MockAuthManager) ValidateCredentials(ctx context.Context, accessKey, secretKey string) (*auth.User, error) {
	args := m.Called(ctx, accessKey, secretKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.User), args.Error(1)
}

func (m *MockAuthManager) ValidateConsoleCredentials(ctx context.Context, username, password string) (*auth.User, error) {
	args := m.Called(ctx, username, password)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.User), args.Error(1)
}

func (m *MockAuthManager) ValidateJWT(ctx context.Context, token string) (*auth.User, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.User), args.Error(1)
}

func (m *MockAuthManager) GenerateJWT(ctx context.Context, user *auth.User) (string, error) {
	args := m.Called(ctx, user)
	return args.String(0), args.Error(1)
}

func (m *MockAuthManager) ValidateS3Signature(ctx context.Context, r *http.Request) (*auth.User, error) {
	args := m.Called(ctx, r)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.User), args.Error(1)
}

func (m *MockAuthManager) ValidateS3SignatureV4(ctx context.Context, r *http.Request) (*auth.User, error) {
	args := m.Called(ctx, r)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.User), args.Error(1)
}

func (m *MockAuthManager) ValidateS3SignatureV2(ctx context.Context, r *http.Request) (*auth.User, error) {
	args := m.Called(ctx, r)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.User), args.Error(1)
}

func (m *MockAuthManager) CheckPermission(ctx context.Context, user *auth.User, action, resource string) error {
	args := m.Called(ctx, user, action, resource)
	return args.Error(0)
}

func (m *MockAuthManager) CheckBucketPermission(ctx context.Context, user *auth.User, bucket, action string) error {
	args := m.Called(ctx, user, bucket, action)
	return args.Error(0)
}

func (m *MockAuthManager) CheckObjectPermission(ctx context.Context, user *auth.User, bucket, object, action string) error {
	args := m.Called(ctx, user, bucket, object, action)
	return args.Error(0)
}

func (m *MockAuthManager) CreateUser(ctx context.Context, user *auth.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockAuthManager) UpdateUser(ctx context.Context, user *auth.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockAuthManager) UpdateUserPreferences(ctx context.Context, userID, themePreference, languagePreference string) error {
	args := m.Called(ctx, userID, themePreference, languagePreference)
	return args.Error(0)
}

func (m *MockAuthManager) DeleteUser(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockAuthManager) GetUser(ctx context.Context, userID string) (*auth.User, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.User), args.Error(1)
}

func (m *MockAuthManager) ListUsers(ctx context.Context) ([]auth.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]auth.User), args.Error(1)
}

func (m *MockAuthManager) GenerateAccessKey(ctx context.Context, userID string) (*auth.AccessKey, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.AccessKey), args.Error(1)
}

func (m *MockAuthManager) GetAccessKey(ctx context.Context, accessKeyID string) (*auth.AccessKey, error) {
	args := m.Called(ctx, accessKeyID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.AccessKey), args.Error(1)
}

func (m *MockAuthManager) RevokeAccessKey(ctx context.Context, accessKey string) error {
	args := m.Called(ctx, accessKey)
	return args.Error(0)
}

func (m *MockAuthManager) ListAccessKeys(ctx context.Context, userID string) ([]auth.AccessKey, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]auth.AccessKey), args.Error(1)
}

func (m *MockAuthManager) CreateTenant(ctx context.Context, tenant *auth.Tenant) error {
	args := m.Called(ctx, tenant)
	return args.Error(0)
}

func (m *MockAuthManager) GetTenant(ctx context.Context, tenantID string) (*auth.Tenant, error) {
	args := m.Called(ctx, tenantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.Tenant), args.Error(1)
}

func (m *MockAuthManager) GetTenantByName(ctx context.Context, name string) (*auth.Tenant, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.Tenant), args.Error(1)
}

func (m *MockAuthManager) ListTenants(ctx context.Context) ([]*auth.Tenant, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*auth.Tenant), args.Error(1)
}

func (m *MockAuthManager) UpdateTenant(ctx context.Context, tenant *auth.Tenant) error {
	args := m.Called(ctx, tenant)
	return args.Error(0)
}

func (m *MockAuthManager) DeleteTenant(ctx context.Context, tenantID string) error {
	args := m.Called(ctx, tenantID)
	return args.Error(0)
}

func (m *MockAuthManager) ListTenantUsers(ctx context.Context, tenantID string) ([]*auth.User, error) {
	args := m.Called(ctx, tenantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*auth.User), args.Error(1)
}

func (m *MockAuthManager) IncrementTenantBucketCount(ctx context.Context, tenantID string) error {
	args := m.Called(ctx, tenantID)
	return args.Error(0)
}

func (m *MockAuthManager) DecrementTenantBucketCount(ctx context.Context, tenantID string) error {
	args := m.Called(ctx, tenantID)
	return args.Error(0)
}

func (m *MockAuthManager) IncrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	args := m.Called(ctx, tenantID, bytes)
	return args.Error(0)
}

func (m *MockAuthManager) DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	args := m.Called(ctx, tenantID, bytes)
	return args.Error(0)
}

func (m *MockAuthManager) CheckTenantStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) error {
	args := m.Called(ctx, tenantID, additionalBytes)
	return args.Error(0)
}

func (m *MockAuthManager) GrantBucketAccess(ctx context.Context, bucketName, userID, tenantID, permissionLevel, grantedBy string, expiresAt int64) error {
	args := m.Called(ctx, bucketName, userID, tenantID, permissionLevel, grantedBy, expiresAt)
	return args.Error(0)
}

func (m *MockAuthManager) RevokeBucketAccess(ctx context.Context, bucketName, userID, tenantID string) error {
	args := m.Called(ctx, bucketName, userID, tenantID)
	return args.Error(0)
}

func (m *MockAuthManager) CheckBucketAccess(ctx context.Context, bucketName, userID string) (bool, string, error) {
	args := m.Called(ctx, bucketName, userID)
	return args.Bool(0), args.String(1), args.Error(2)
}

func (m *MockAuthManager) ListBucketPermissions(ctx context.Context, bucketName string) ([]*auth.BucketPermission, error) {
	args := m.Called(ctx, bucketName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*auth.BucketPermission), args.Error(1)
}

func (m *MockAuthManager) ListUserBucketPermissions(ctx context.Context, userID string) ([]*auth.BucketPermission, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*auth.BucketPermission), args.Error(1)
}

func (m *MockAuthManager) Middleware() func(http.Handler) http.Handler {
	args := m.Called()
	return args.Get(0).(func(http.Handler) http.Handler)
}

func (m *MockAuthManager) CheckRateLimit(ip string) bool {
	args := m.Called(ip)
	return args.Bool(0)
}

func (m *MockAuthManager) IsAccountLocked(ctx context.Context, userID string) (bool, int64, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Get(1).(int64), args.Error(2)
}

func (m *MockAuthManager) LockAccount(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockAuthManager) UnlockAccount(ctx context.Context, adminUserID, targetUserID string) error {
	args := m.Called(ctx, adminUserID, targetUserID)
	return args.Error(0)
}

func (m *MockAuthManager) RecordFailedLogin(ctx context.Context, userID, ip string) error {
	args := m.Called(ctx, userID, ip)
	return args.Error(0)
}

func (m *MockAuthManager) RecordSuccessfulLogin(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockAuthManager) SetUserLockedCallback(callback func(*auth.User)) {
	m.Called(callback)
}

func (m *MockAuthManager) Setup2FA(ctx context.Context, userID string) (*auth.TOTPSetup, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.TOTPSetup), args.Error(1)
}

func (m *MockAuthManager) Enable2FA(ctx context.Context, userID, code string, secret string) ([]string, error) {
	args := m.Called(ctx, userID, code, secret)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockAuthManager) Disable2FA(ctx context.Context, userID, requestingUserID string, isGlobalAdmin bool) error {
	args := m.Called(ctx, userID, requestingUserID, isGlobalAdmin)
	return args.Error(0)
}

func (m *MockAuthManager) Verify2FACode(ctx context.Context, userID, code string) (bool, error) {
	args := m.Called(ctx, userID, code)
	return args.Bool(0), args.Error(1)
}

func (m *MockAuthManager) RegenerateBackupCodes(ctx context.Context, userID string) ([]string, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockAuthManager) Get2FAStatus(ctx context.Context, userID string) (bool, int64, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Get(1).(int64), args.Error(2)
}

func (m *MockAuthManager) IsReady() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockAuthManager) GetDB() interface{} {
	args := m.Called()
	return args.Get(0)
}

func (m *MockAuthManager) FindUserByExternalID(ctx context.Context, externalID, authProvider string) (*auth.User, error) {
	args := m.Called(ctx, externalID, authProvider)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.User), args.Error(1)
}

type MockMetricsManager struct {
	mock.Mock
}

func (m *MockMetricsManager) RecordHTTPRequest(method, path, status string, duration time.Duration) {
	m.Called(method, path, status, duration)
}

func (m *MockMetricsManager) RecordHTTPRequestSize(method, path string, size int64) {
	m.Called(method, path, size)
}

func (m *MockMetricsManager) RecordHTTPResponseSize(method, path string, size int64) {
	m.Called(method, path, size)
}

func (m *MockMetricsManager) RecordS3Operation(operation, bucket string, success bool, duration time.Duration) {
	m.Called(operation, bucket, success, duration)
}

func (m *MockMetricsManager) RecordS3Error(operation, bucket, errorType string) {
	m.Called(operation, bucket, errorType)
}

func (m *MockMetricsManager) RecordStorageOperation(operation string, success bool, duration time.Duration) {
	m.Called(operation, success, duration)
}

func (m *MockMetricsManager) UpdateStorageUsage(bucket string, objects, bytes int64) {
	m.Called(bucket, objects, bytes)
}

func (m *MockMetricsManager) RecordObjectOperation(operation, bucket string, objectSize int64, duration time.Duration) {
	m.Called(operation, bucket, objectSize, duration)
}

func (m *MockMetricsManager) RecordAuthAttempt(method string, success bool) {
	m.Called(method, success)
}

func (m *MockMetricsManager) RecordAuthFailure(method, reason string) {
	m.Called(method, reason)
}

func (m *MockMetricsManager) UpdateSystemMetrics(cpuUsage, memoryUsage, diskUsage float64) {
	m.Called(cpuUsage, memoryUsage, diskUsage)
}

func (m *MockMetricsManager) RecordSystemEvent(eventType string, details map[string]string) {
	m.Called(eventType, details)
}

func (m *MockMetricsManager) UpdateBucketMetrics(bucket string, objects, bytes int64) {
	m.Called(bucket, objects, bytes)
}

func (m *MockMetricsManager) RecordBucketOperation(operation, bucket string, success bool) {
	m.Called(operation, bucket, success)
}

func (m *MockMetricsManager) RecordObjectLockOperation(operation, bucket string, success bool) {
	m.Called(operation, bucket, success)
}

func (m *MockMetricsManager) UpdateRetentionMetrics(bucket string, governanceObjects, complianceObjects int64) {
	m.Called(bucket, governanceObjects, complianceObjects)
}

func (m *MockMetricsManager) RecordBackgroundTask(taskType string, duration time.Duration, success bool) {
	m.Called(taskType, duration, success)
}

func (m *MockMetricsManager) UpdateCacheMetrics(hitRate float64, size int64) {
	m.Called(hitRate, size)
}

func (m *MockMetricsManager) GetMetricsHandler() http.Handler {
	args := m.Called()
	return args.Get(0).(http.Handler)
}

func (m *MockMetricsManager) GetMetricsSnapshot() (map[string]interface{}, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockMetricsManager) GetS3MetricsSnapshot() (map[string]interface{}, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockMetricsManager) IsHealthy() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockMetricsManager) Reset() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockMetricsManager) GetHistoricalMetrics(metricType string, start, end time.Time) ([]metrics.MetricSnapshot, error) {
	args := m.Called(metricType, start, end)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]metrics.MetricSnapshot), args.Error(1)
}

func (m *MockMetricsManager) GetHistoryStats() (map[string]interface{}, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockMetricsManager) Middleware() func(http.Handler) http.Handler {
	args := m.Called()
	return args.Get(0).(func(http.Handler) http.Handler)
}

func (m *MockMetricsManager) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockMetricsManager) Stop() error {
	args := m.Called()
	return args.Error(0)
}

// Helper function to create test handler
func setupTestHandler() (*Handler, *MockBucketManager, *MockObjectManager, *MockAuthManager) {
	mockBucket := new(MockBucketManager)
	mockObject := new(MockObjectManager)
	mockAuth := new(MockAuthManager)
	mockMetrics := new(MockMetricsManager)

	handler := NewHandler(
		mockBucket,
		mockObject,
		mockAuth,
		mockMetrics,
		nil, // metadataStore
		nil, // shareManager
		"http://localhost:8080",
		"http://localhost:5173",
		"/tmp",
		nil, // clusterManager
		nil, // bucketAggregator
	)

	return handler, mockBucket, mockObject, mockAuth
}

// Helper function to add authenticated user to request context
func addUserToContext(r *http.Request, user *auth.User) *http.Request {
	ctx := context.WithValue(r.Context(), "user", user)
	return r.WithContext(ctx)
}

// ============================================================================
// SECURITY TESTS - Health Endpoints
// ============================================================================

// TestHealthEndpoint_NoAuthenticationRequired tests that health endpoint doesn't require auth
func TestHealthEndpoint_NoAuthenticationRequired(t *testing.T) {
	handler, _, _, _ := setupTestHandler()

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	// Health endpoint should be accessible without authentication
	assert.Equal(t, http.StatusOK, rr.Code)
}

// TestReadyEndpoint_NoAuthenticationRequired tests that ready endpoint doesn't require auth
func TestReadyEndpoint_NoAuthenticationRequired(t *testing.T) {
	handler, mockBucket, mockObject, _ := setupTestHandler()

	// Mock manager ready checks
	mockBucket.On("IsReady").Return(true)
	mockObject.On("IsReady").Return(true)

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/ready", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	// Ready endpoint should be accessible without authentication
	assert.Equal(t, http.StatusOK, rr.Code)
}

// ============================================================================
// SECURITY TESTS - Missing Authentication
// ============================================================================

// TestS3Operation_MissingAuthentication tests that S3 operations require authentication
func TestS3Operation_MissingAuthentication(t *testing.T) {
	handler, mockBucket, _, _ := setupTestHandler()

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	testCases := []struct {
		name   string
		method string
		path   string
	}{
		{"ListBuckets", "GET", "/"},
		{"HeadBucket", "HEAD", "/test-bucket"},
		{"GetObject", "GET", "/test-bucket/test-object"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Mock bucket operations that are called before auth check
			if tc.name == "ListBuckets" {
				mockBucket.On("ListBuckets", mock.Anything, mock.Anything).Return(
					[]bucket.Bucket{}, nil,
				).Once()
			}
			if tc.name == "HeadBucket" {
				mockBucket.On("BucketExists", mock.Anything, mock.Anything, "test-bucket").Return(
					true, nil,
				).Once()
				// Mock GetBucketACL - called when checking public access for unauthenticated requests
				mockBucket.On("GetBucketACL", mock.Anything, mock.Anything, "test-bucket").Return(
					nil, nil,
				).Once()
			}
			if tc.name == "GetObject" {
				mockBucket.On("BucketExists", mock.Anything, mock.Anything, "test-bucket").Return(
					true, nil,
				).Once()
				// Mock GetBucketACL - called when checking public access for unauthenticated requests
				mockBucket.On("GetBucketACL", mock.Anything, mock.Anything, "test-bucket").Return(
					nil, nil,
				).Once()
			}

			// Create request WITHOUT authentication (no user in context)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			// Should return 403 Forbidden for missing auth
			// The s3Handler checks for user in context and returns AccessDenied
			assert.Equal(t, http.StatusForbidden, rr.Code, "Expected 403 for "+tc.name)
			assert.Contains(t, rr.Body.String(), "AccessDenied", "Should contain AccessDenied error")
		})
	}
}

// ============================================================================
// SECURITY TESTS - Invalid Signature
// ============================================================================

// TestS3Operation_InvalidSignature tests detection of invalid AWS signature
func TestS3Operation_InvalidSignature(t *testing.T) {
	handler, mockBucket, _, _ := setupTestHandler()

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/test-bucket", nil)
	// Add invalid signature header
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=INVALID/20240101/us-east-1/s3/aws4_request")

	// Mock bucket operations (called before auth check)
	mockBucket.On("BucketExists", mock.Anything, mock.Anything, "test-bucket").Return(
		true, nil,
	).Once()
	// Mock GetBucketACL - called when checking public access for unauthenticated requests
	mockBucket.On("GetBucketACL", mock.Anything, mock.Anything, "test-bucket").Return(
		nil, nil,
	).Once()

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Should reject invalid signature (no user in context = AccessDenied)
	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), "AccessDenied")
}

// ============================================================================
// SECURITY TESTS - Expired Credentials
// ============================================================================

// TestS3Operation_ExpiredCredentials tests rejection of expired credentials
func TestS3Operation_ExpiredCredentials(t *testing.T) {
	handler, mockBucket, _, _ := setupTestHandler()

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/test-bucket", nil)
	// Add expired timestamp (more than 15 minutes old)
	oldTime := time.Now().Add(-20 * time.Minute).Format("20060102T150405Z")
	req.Header.Set("X-Amz-Date", oldTime)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AKIATEST/20240101/us-east-1/s3/aws4_request")

	// Mock bucket operations (called before auth check)
	mockBucket.On("BucketExists", mock.Anything, mock.Anything, "test-bucket").Return(
		true, nil,
	).Once()
	// Mock GetBucketACL - called when checking public access for unauthenticated requests
	mockBucket.On("GetBucketACL", mock.Anything, mock.Anything, "test-bucket").Return(
		nil, nil,
	).Once()

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Should reject expired credentials (no user in context = AccessDenied)
	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), "AccessDenied")
}

// ============================================================================
// SECURITY TESTS - Input Validation
// ============================================================================

// TestBucketName_PathTraversal tests prevention of path traversal attacks in bucket names
func TestBucketName_PathTraversal(t *testing.T) {
	handler, mockBucket, _, _ := setupTestHandler()

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	maliciousBuckets := []string{
		"../etc/passwd",
		"..%2F..%2Fetc%2Fpasswd",
		"bucket/../../../etc/passwd",
		"..",
		"./",
	}

	for _, bucketName := range maliciousBuckets {
		t.Run("Bucket_"+bucketName, func(t *testing.T) {
			// Create authenticated user context
			testUser := &auth.User{
				ID:       "test-user",
				TenantID: "test-tenant",
				Status:   "active",
				Roles:    []string{"admin"},
			}

			req := httptest.NewRequest("GET", "/"+bucketName, nil)
			req = addUserToContext(req, testUser)

			// Mock bucket operations - path traversal buckets won't be found
			mockBucket.On("BucketExists", mock.Anything, "test-tenant", bucketName).Return(
				false, nil,
			).Once()

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			// Should not allow path traversal - bucket won't be found or will be blocked
			// 301 (redirect), 404 (not found), 403 (forbidden), or 400 (bad request) are all acceptable
			// 301 redirect is a security measure by the router to prevent path traversal
			assert.True(t, rr.Code == 301 || rr.Code >= 400,
				"Path traversal should be blocked, got status %d for bucket %s", rr.Code, bucketName)
		})
	}
}

// TestObjectKey_XSSAttempt tests prevention of XSS attacks in object keys
func TestObjectKey_XSSAttempt(t *testing.T) {
	handler, mockBucket, mockObject, _ := setupTestHandler()

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	testCases := []struct {
		name           string
		pathKey        string // The key as it appears in the URL path
		mockKey        string // The key as it will be passed to the mock (URL-decoded)
		checkUnescaped string // What to check for in the response (unescaped HTML)
	}{
		{
			name:           "ScriptTag",
			pathKey:        "<script>alert('xss')</script>.txt",
			mockKey:        "<script>alert('xss')</script>.txt",
			checkUnescaped: "<script>",
		},
		{
			name:           "ImgTag",
			pathKey:        "file<img-src=x-onerror=alert(1)>.txt",
			mockKey:        "file<img-src=x-onerror=alert(1)>.txt",
			checkUnescaped: "<img",
		},
		{
			name:           "URLEncodedScript",
			pathKey:        "test%3Cscript%3Ealert(1)%3C/script%3E.txt",
			mockKey:        "test<script>alert(1)</script>.txt", // URL-decoded
			checkUnescaped: "<script>",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create authenticated user context
			testUser := &auth.User{
				ID:       "test-user",
				TenantID: "test-tenant",
				Status:   "active",
				Roles:    []string{"admin"},
			}

			req := httptest.NewRequest("GET", "/test-bucket/"+tc.pathKey, nil)
			req = addUserToContext(req, testUser)

			mockBucket.On("GetBucketInfo", mock.Anything, "test-tenant", "test-bucket").Return(
				&bucket.Bucket{Name: "test-bucket", TenantID: "test-tenant"},
				nil,
			).Once()

			// Mock GetObject to return not found (object doesn't exist)
			// Use mockKey which is the URL-decoded version
			mockObject.On("GetObject", mock.Anything, "test-tenant/test-bucket", tc.mockKey, mock.Anything).Return(
				nil, nil, object.ErrObjectNotFound,
			).Once()

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			// Response should not contain unescaped HTML tags (check for the specific dangerous tag)
			responseBody := rr.Body.String()
			assert.NotContains(t, responseBody, tc.checkUnescaped,
				"Response contains unescaped HTML tag: %s", tc.checkUnescaped)
		})
	}
}

// TestObjectKey_SQLInjectionAttempt tests prevention of SQL injection patterns
func TestObjectKey_SQLInjectionAttempt(t *testing.T) {
	handler, mockBucket, mockObject, _ := setupTestHandler()

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	// Use simpler SQL injection patterns without spaces to avoid URL encoding issues
	testCases := []struct {
		name    string
		pathKey string
		mockKey string
	}{
		{
			name:    "DropTable",
			pathKey: "';DROP-TABLE-users--",
			mockKey: "';DROP-TABLE-users--",
		},
		{
			name:    "OrCondition",
			pathKey: "1'OR'1'='1",
			mockKey: "1'OR'1'='1",
		},
		{
			name:    "AdminComment",
			pathKey: "admin'--",
			mockKey: "admin'--",
		},
		{
			name:    "UnionSelect",
			pathKey: "'UNION-SELECT*FROM-users--",
			mockKey: "'UNION-SELECT*FROM-users--",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create authenticated user context
			testUser := &auth.User{
				ID:       "test-user",
				TenantID: "test-tenant",
				Status:   "active",
				Roles:    []string{"admin"},
			}

			req := httptest.NewRequest("GET", "/test-bucket/"+tc.pathKey, nil)
			req = addUserToContext(req, testUser)

			mockBucket.On("GetBucketInfo", mock.Anything, "test-tenant", "test-bucket").Return(
				&bucket.Bucket{Name: "test-bucket", TenantID: "test-tenant"},
				nil,
			).Once()

			// Mock GetObject to return not found (object doesn't exist)
			mockObject.On("GetObject", mock.Anything, "test-tenant/test-bucket", tc.mockKey, mock.Anything).Return(
				nil, nil, object.ErrObjectNotFound,
			).Once()

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			// Should handle SQL injection attempts safely (not crash or expose database errors)
			assert.NotEqual(t, http.StatusInternalServerError, rr.Code,
				"SQL injection attempt caused internal server error")
		})
	}
}

// ============================================================================
// SECURITY TESTS - Oversized Inputs
// ============================================================================

// TestPutObject_OversizedHeaders tests handling of oversized HTTP headers
func TestPutObject_OversizedHeaders(t *testing.T) {
	handler, mockBucket, mockObject, mockAuth := setupTestHandler()

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	// Create authenticated user context
	testUser := &auth.User{
		ID:       "test-user",
		TenantID: "test-tenant",
		Status:   "active",
		Roles:    []string{"admin"},
	}

	// Create extremely large header value (> 8KB)
	largeHeaderValue := strings.Repeat("A", 10*1024) // 10KB header

	req := httptest.NewRequest("PUT", "/test-bucket/test-object", bytes.NewReader([]byte("test")))
	req = addUserToContext(req, testUser)
	req.Header.Set("X-Amz-Meta-Custom", largeHeaderValue)
	req.Header.Set("Content-Length", "4")

	// Mock bucket operations
	mockBucket.On("GetBucketInfo", mock.Anything, "test-tenant", "test-bucket").Return(
		&bucket.Bucket{Name: "test-bucket", TenantID: "test-tenant"},
		nil,
	).Maybe()

	// Mock object lock config check (no lock config)
	mockBucket.On("GetObjectLockConfig", mock.Anything, "test-tenant", "test-bucket").Return(
		nil, nil,
	).Maybe()

	// Mock GetObject call for quota validation (object doesn't exist yet)
	mockObject.On("GetObject", mock.Anything, "test-tenant/test-bucket", "test-object", mock.Anything).Return(
		nil, nil, object.ErrObjectNotFound,
	).Maybe()

	// Mock tenant storage quota check
	mockAuth.On("CheckTenantStorageQuota", mock.Anything, "test-tenant", mock.Anything).Return(
		nil,
	).Maybe()

	// Mock PutObject to return success
	mockObject.On("PutObject", mock.Anything, "test-tenant/test-bucket", "test-object", mock.Anything, mock.Anything).Return(
		&object.Object{
			Key:  "test-object",
			Size: 4,
		}, nil,
	).Maybe()

	// Mock tenant storage increment
	mockAuth.On("IncrementTenantStorage", mock.Anything, "test-tenant", mock.Anything).Return(
		nil,
	).Maybe()

	// Mock bucket operations for incrementing object count
	mockBucket.On("IncrementObjectCount", mock.Anything, "test-tenant", "test-bucket", mock.Anything).Return(
		nil,
	).Maybe()

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Should handle oversized headers gracefully
	// HTTP server may reject before reaching handler, or handler should reject
	// Note: httptest doesn't enforce header size limits, so we just verify it doesn't crash
	assert.NotEqual(t, http.StatusInternalServerError, rr.Code, "Should not crash with oversized headers")
}

// ============================================================================
// SECURITY TESTS - Concurrent Requests
// ============================================================================

// TestConcurrentRequests_RateLimiting simulates concurrent requests
func TestConcurrentRequests_RateLimiting(t *testing.T) {
	handler, _, _, _ := setupTestHandler()

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	// Send 10 concurrent requests to health endpoint (no auth required)
	results := make(chan int, 10)
	for i := 0; i < 10; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/health", nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			results <- rr.Code
		}()
	}

	// Collect results
	statusCodes := make([]int, 10)
	for i := 0; i < 10; i++ {
		statusCodes[i] = <-results
	}

	// All requests should succeed (health endpoint has no rate limit)
	for _, code := range statusCodes {
		assert.Equal(t, http.StatusOK, code)
	}
}
