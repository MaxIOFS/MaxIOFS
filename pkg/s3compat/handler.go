package s3compat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/presigned"
	"github.com/maxiofs/maxiofs/internal/share"
	"github.com/sirupsen/logrus"
)

// getObjectKey extracts object key from mux vars (already decoded by Gorilla Mux)
func getObjectKey(r *http.Request) string {
	vars := mux.Vars(r)
	return vars["object"]
}

// generateRequestID generates a SHORT request ID (like MaxIOFS does)
// MaxIOFS uses 16 character hex strings, not 32
func generateRequestID() string {
	b := make([]byte, 8) // 8 bytes = 16 hex chars
	rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

// generateAmzId2 generates a LONG hash for x-amz-id-2 (like MaxIOFS does)
// MaxIOFS uses 64 character hex strings
func generateAmzId2() string {
	b := make([]byte, 32) // 32 bytes = 64 hex chars
	rand.Read(b)
	return hex.EncodeToString(b)
}

// addS3CompatHeaders adds S3-compatible headers to all responses
// This ensures compatibility with Veeam and other S3 clients
func addS3CompatHeaders(w http.ResponseWriter) {
	// x-amz-request-id: SHORT request ID (16 chars like MaxIOFS)
	w.Header().Set("X-Amz-Request-Id", generateRequestID())

	// x-amz-id-2: LONG host ID hash (64 chars like MaxIOFS)
	w.Header().Set("X-Amz-Id-2", generateAmzId2())

	// Server header identifying as MaxIOFS
	w.Header().Set("Server", "MaxIOFS")

	// Accept ranges for partial content
	w.Header().Set("Accept-Ranges", "bytes")

	// Security headers (S3-compatible)
	w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Xss-Protection", "1; mode=block")

	// Vary headers for caching
	w.Header().Set("Vary", "Origin")
	w.Header().Add("Vary", "Accept-Encoding")

	// CRITICAL: Rate limit headers - may disable auto-provisioning
	w.Header().Set("X-Ratelimit-Limit", "18299")
	w.Header().Set("X-Ratelimit-Remaining", "18299")
}

// Handler implements S3-compatible API handlers
type Handler struct {
	bucketManager bucket.Manager
	objectManager object.Manager
	authManager   auth.Manager
	shareManager  interface {
		GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (interface{}, error)
	}
	metadataStore interface {
		ListAllObjectVersions(ctx context.Context, bucket, prefix string, maxKeys int) ([]*metadata.ObjectVersion, error)
		GetBucketByName(ctx context.Context, name string) (*metadata.BucketMetadata, error)
	}
	publicAPIURL string
	dataDir      string // For calculating disk capacity in SOSAPI
}

// NewHandler creates a new S3 compatibility handler
func NewHandler(bucketManager bucket.Manager, objectManager object.Manager) *Handler {
	return &Handler{
		bucketManager: bucketManager,
		objectManager: objectManager,
		shareManager:  nil, // Optional, will be set via SetShareManager
	}
}

// SetAuthManager sets the auth manager for permission checking
func (h *Handler) SetAuthManager(am auth.Manager) {
	h.authManager = am
}

// SetShareManager sets the share manager for validating presigned URLs
func (h *Handler) SetShareManager(sm interface {
	GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (interface{}, error)
}) {
	h.shareManager = sm
}

// SetPublicAPIURL sets the public API URL for presigned URL generation
func (h *Handler) SetPublicAPIURL(url string) {
	h.publicAPIURL = url
}

// SetDataDir sets the data directory for disk capacity calculations
func (h *Handler) SetDataDir(dataDir string) {
	h.dataDir = dataDir
}

// SetMetadataStore sets the metadata store for accessing object versions
func (h *Handler) SetMetadataStore(ms interface {
	ListAllObjectVersions(ctx context.Context, bucket, prefix string, maxKeys int) ([]*metadata.ObjectVersion, error)
	GetBucketByName(ctx context.Context, name string) (*metadata.BucketMetadata, error)
}) {
	h.metadataStore = ms
}

// S3 XML response structures
type ListAllMyBucketsResult struct {
	XMLName xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListAllMyBucketsResult"`
	Owner   Owner    `xml:"Owner"`
	Buckets Buckets  `xml:"Buckets"`
}

type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

type Buckets struct {
	Bucket []BucketInfo `xml:"Bucket"`
}

type BucketInfo struct {
	Name         string    `xml:"Name"`
	CreationDate time.Time `xml:"CreationDate"`
}

type ListBucketResult struct {
	XMLName        xml.Name       `xml:"ListBucketResult"`
	Name           string         `xml:"Name"`
	Prefix         string         `xml:"Prefix"`
	Marker         string         `xml:"Marker"`
	MaxKeys        int            `xml:"MaxKeys"`
	Delimiter      string         `xml:"Delimiter,omitempty"`
	IsTruncated    bool           `xml:"IsTruncated"`
	NextMarker     string         `xml:"NextMarker,omitempty"`
	Contents       []ObjectInfo   `xml:"Contents"`
	CommonPrefixes []CommonPrefix `xml:"CommonPrefixes"`
}

type ObjectInfo struct {
	Key          string    `xml:"Key"`
	LastModified time.Time `xml:"LastModified"`
	ETag         string    `xml:"ETag"`
	Size         int64     `xml:"Size"`
	StorageClass string    `xml:"StorageClass"`
	Owner        Owner     `xml:"Owner"`
}

type CommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

type Error struct {
	XMLName    xml.Name `xml:"Error"`
	Code       string   `xml:"Code"`
	Message    string   `xml:"Message"`
	Key        string   `xml:"Key,omitempty"`        // For object errors (NoSuchKey, etc.)
	BucketName string   `xml:"BucketName,omitempty"` // For bucket errors (NoSuchBucket, etc.)
	Resource   string   `xml:"Resource,omitempty"`   // For other errors
	RequestId  string   `xml:"RequestId"`
	HostId     string   `xml:"HostId"`
}

// Service operations
func (h *Handler) ListBuckets(w http.ResponseWriter, r *http.Request) {
	// Add S3-compatible headers FIRST
	addS3CompatHeaders(w)

	// Detect Veeam and log extensively
	userAgent := r.Header.Get("User-Agent")
	if isVeeamClient(userAgent) {
		// Log ALL response headers that we're sending
		logrus.WithFields(logrus.Fields{
			"user_agent":       userAgent,
			"method":           r.Method,
			"uri":              r.RequestURI,
			"request_headers":  r.Header,
			"response_headers": w.Header(),
		}).Warn("VEEAM ListBuckets - RESPONSE HEADERS - MaxIOFS S3-compatible")
	}

	logrus.Debug("S3 API: ListBuckets")

	// Get tenant ID from authenticated user
	// Empty string for global admins (who can see all tenants)
	tenantID := h.getTenantIDFromRequest(r)

	// List buckets for this tenant (or all if tenantID is empty for global admin)
	buckets, err := h.bucketManager.ListBuckets(r.Context(), tenantID)
	if err != nil {
		h.writeError(w, "InternalError", err.Error(), "", r)
		return
	}

	// Filter buckets by tenant ownership and user permissions
	user, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		h.writeError(w, "AccessDenied", "Access Denied.", "", r)
		return
	}

	// Global admin = admin WITHOUT tenant
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""

	var filteredBuckets []bucket.Bucket

	if isGlobalAdmin {
		// ONLY global admins see all buckets (already filtered by tenantID="" at manager level)
		filteredBuckets = buckets
	} else if user.TenantID != "" {
		// Tenant users (including tenant admins) see their tenant's buckets + buckets where they have permissions
		for _, b := range buckets {
			// Include if bucket belongs to tenant or user owns it
			if (b.OwnerType == "tenant" && b.OwnerID == user.TenantID) ||
				(b.OwnerType == "user" && b.OwnerID == user.ID) {
				filteredBuckets = append(filteredBuckets, b)
				continue
			}

			// Include if user has permissions in bucket policy
			if h.userHasBucketPermission(r.Context(), tenantID, b.Name, user.ID) {
				filteredBuckets = append(filteredBuckets, b)
			}
		}
	} else {
		// Non-admin users without tenant: see their buckets + buckets where they have permissions
		for _, b := range buckets {
			// Include if user owns the bucket
			if b.OwnerType == "user" && b.OwnerID == user.ID {
				filteredBuckets = append(filteredBuckets, b)
				continue
			}

			// Include if user has permissions in bucket policy
			if h.userHasBucketPermission(r.Context(), tenantID, b.Name, user.ID) {
				filteredBuckets = append(filteredBuckets, b)
			}
		}
	}

	result := ListAllMyBucketsResult{
		Owner: Owner{
			ID:          user.ID,
			DisplayName: user.DisplayName,
		},
		Buckets: Buckets{
			Bucket: make([]BucketInfo, len(filteredBuckets)),
		},
	}

	for i, bucket := range filteredBuckets {
		result.Buckets.Bucket[i] = BucketInfo{
			Name:         bucket.Name,
			CreationDate: bucket.CreatedAt,
		}
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}

// userHasBucketPermission checks if user has explicit permissions (ACLs or Policy)
func (h *Handler) userHasBucketPermission(ctx context.Context, tenantID, bucketName, userID string) bool {
	// Check bucket permissions table (frontend ACLs)
	if h.authManager != nil {
		hasAccess, _, err := h.authManager.CheckBucketAccess(ctx, bucketName, userID)
		if err == nil && hasAccess {
			return true
		}
	}

	// Also check bucket policy (S3 style)
	// TODO: Implement bucket policy checking if needed (would need tenantID for scoped lookup)
	return false
}

// Bucket operations
func (h *Handler) CreateBucket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Add S3-compatible headers (CRITICAL for Veeam recognition)
	addS3CompatHeaders(w)

	// Detect if request is from Veeam client
	// Detect if request is from Veeam client
	userAgent := r.Header.Get("User-Agent")
	isVeeam := isVeeamClient(userAgent)

	// CRITICAL: Block Veeam test bucket creation with MethodNotAllowed
	// This tells Veeam that bucket creation is NOT supported, disabling auto-provisioning
	if isVeeam && (strings.HasPrefix(bucketName, "veeamtest-") || strings.HasPrefix(bucketName, "veeam-test-bucket")) {
		// Build XML response body
		errorBody := `<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>MethodNotAllowed</Code>
  <Message>Bucket creation not supported</Message>
  <Resource>/` + bucketName + `</Resource>
  <RequestId>veeam-disable-autoprov</RequestId>
</Error>`

		// Set all headers before writing status
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("Content-Length", strconv.Itoa(len(errorBody)))
		w.Header().Set("x-amz-request-id", "veeam-disable-autoprov")
		w.Header().Set("x-amz-id-2", "localserver")
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("Connection", "close")
		w.Header().Set("Server", "S3Compatible")

		// Write status code
		w.WriteHeader(http.StatusMethodNotAllowed)

		// Write body
		w.Write([]byte(errorBody))

		logrus.WithFields(logrus.Fields{
			"bucket":     bucketName,
			"user_agent": userAgent,
		}).Warn("Blocked Veeam test bucket — returned 405 with headers to disable auto-provisioning")
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":     bucketName,
		"user_agent": userAgent,
		"is_veeam":   isVeeam,
	}).Debug("S3 API: CreateBucket")
	if isVeeam {
		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
		}).Info("Veeam normal bucket creation - allowing")
	}

	// CRITICAL: Bucket creation requires authentication
	// Get authenticated user from context
	user, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"method": "CreateBucket",
		}).Warn("Unauthenticated bucket creation attempt")
		h.writeError(w, "AccessDenied", "Authentication required to create buckets", bucketName, r)
		return
	}

	// Determine tenantID - use user's tenantID
	// Global admins (TenantID="") can create global buckets
	// Tenant users/admins create buckets within their tenant
	tenantID := user.TenantID

	// Check tenant bucket quota before creation (for tenant users)
	if tenantID != "" {
		tenant, err := h.authManager.GetTenant(r.Context(), tenantID)
		if err != nil {
			logrus.WithError(err).WithField("tenantID", tenantID).Error("Failed to get tenant for quota check")
			h.writeError(w, "InternalError", "Failed to verify tenant quota", bucketName, r)
			return
		}

		// Check if tenant has reached max buckets
		if tenant.MaxBuckets > 0 && tenant.CurrentBuckets >= tenant.MaxBuckets {
			logrus.WithFields(logrus.Fields{
				"bucket":         bucketName,
				"tenantID":       tenantID,
				"currentBuckets": tenant.CurrentBuckets,
				"maxBuckets":     tenant.MaxBuckets,
			}).Warn("Tenant bucket quota exceeded")
			h.writeError(w, "QuotaExceeded",
				fmt.Sprintf("Tenant bucket quota exceeded (%d/%d). Cannot create more buckets.",
					tenant.CurrentBuckets, tenant.MaxBuckets), bucketName, r)
			return
		}
	}

	if err := h.bucketManager.CreateBucket(r.Context(), tenantID, bucketName); err != nil {
		if err == bucket.ErrBucketAlreadyExists {
			h.writeError(w, "BucketAlreadyExists", "The requested bucket name is not available", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) DeleteBucket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: DeleteBucket")

	tenantID := h.getTenantIDFromRequest(r)
	if err := h.bucketManager.DeleteBucket(r.Context(), tenantID, bucketName); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		if err == bucket.ErrBucketNotEmpty {
			h.writeError(w, "BucketNotEmpty", "The bucket you tried to delete is not empty", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) HeadBucket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Add S3-compatible headers (CRITICAL for Veeam recognition)
	addS3CompatHeaders(w)

	// Detect Veeam and log ALL response headers
	userAgent := r.Header.Get("User-Agent")
	if isVeeamClient(userAgent) {
		logrus.WithFields(logrus.Fields{
			"bucket":           bucketName,
			"user_agent":       userAgent,
			"method":           r.Method,
			"uri":              r.RequestURI,
			"response_headers": w.Header(),
		}).Warn("VEEAM HeadBucket - RESPONSE HEADERS - MaxIOFS S3-compatible")
	}

	logrus.WithField("bucket", bucketName).Debug("S3 API: HeadBucket")

	tenantID := h.getTenantIDFromRequest(r)

	// Permission check: Verify user has READ permission via ACL
	user, userExists := auth.GetUserFromContext(r.Context())

	if userExists {
		// If user belongs to the same tenant as the bucket, allow access automatically
		if user.TenantID != tenantID {
			// Cross-tenant access - check ACL permissions
			hasPermission := h.checkBucketACLPermission(r.Context(), tenantID, bucketName, user.ID, acl.PermissionRead)

			// If no explicit ACL permission, check if authenticated users have read access
			if !hasPermission {
				hasPermission = h.checkAuthenticatedBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)
			}

			// If still no permission, check if public access is allowed
			if !hasPermission {
				hasPermission = h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)
			}

			if !hasPermission {
				logrus.WithFields(logrus.Fields{
					"bucket":       bucketName,
					"userID":       user.ID,
					"userTenantID": user.TenantID,
					"bucketTenant": tenantID,
				}).Warn("ACL permission denied for HeadBucket - cross-tenant access")
				h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
				return
			}
		}
		// Same tenant - allow access automatically
	} else {
		// Unauthenticated access - check if bucket is public
		hasPublicAccess := h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)

		if !hasPublicAccess {
			logrus.WithFields(logrus.Fields{
				"bucket": bucketName,
			}).Warn("Public access denied for HeadBucket - bucket not publicly readable")
			h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
			return
		}
	}

	exists, err := h.bucketManager.BucketExists(r.Context(), tenantID, bucketName)
	if err != nil {
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	if !exists {
		h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) ListObjects(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: ListObjects")

	// Permission check: Verify user has READ permission via ACL
	user, userExists := auth.GetUserFromContext(r.Context())
	tenantID := h.getTenantIDFromRequest(r)

	// Check if user is authenticated
	if userExists {
		// If user belongs to the same tenant as the bucket, allow access automatically
		// ACLs only apply for cross-tenant or public access
		if user.TenantID != tenantID {
			// Cross-tenant access - check ACL permissions
			hasPermission := h.checkBucketACLPermission(r.Context(), tenantID, bucketName, user.ID, acl.PermissionRead)

			// If no explicit ACL permission, check if authenticated users have read access
			if !hasPermission {
				hasPermission = h.checkAuthenticatedBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)
			}

			// If still no permission, check if public access is allowed
			if !hasPermission {
				hasPermission = h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)
			}

			if !hasPermission {
				logrus.WithFields(logrus.Fields{
					"bucket":       bucketName,
					"userID":       user.ID,
					"userTenantID": user.TenantID,
					"bucketTenant": tenantID,
				}).Warn("ACL permission denied for ListObjects - cross-tenant access")
				h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
				return
			}
		}
		// Same tenant - allow access automatically
	} else {
		// Unauthenticated access - check if bucket is public
		hasPublicAccess := h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)

		if !hasPublicAccess {
			logrus.WithFields(logrus.Fields{
				"bucket": bucketName,
			}).Warn("Public access denied for ListObjects - bucket not publicly readable")
			h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
			return
		}
	}

	// Parse query parameters
	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")
	marker := r.URL.Query().Get("marker")
	maxKeys := 1000

	if maxKeysStr := r.URL.Query().Get("max-keys"); maxKeysStr != "" {
		if parsed, err := strconv.Atoi(maxKeysStr); err == nil && parsed > 0 {
			maxKeys = parsed
		}
	}

	bucketPath := h.getBucketPath(r, bucketName)
	listResult, err := h.objectManager.ListObjects(r.Context(), bucketPath, prefix, delimiter, marker, maxKeys)
	if err != nil {
		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	// Convert common prefixes to S3 format
	var commonPrefixes []CommonPrefix
	for _, cp := range listResult.CommonPrefixes {
		commonPrefixes = append(commonPrefixes, CommonPrefix{Prefix: cp.Prefix})
	}

	result := ListBucketResult{
		Name:           bucketName,
		Prefix:         prefix,
		Marker:         marker,
		MaxKeys:        maxKeys,
		Delimiter:      delimiter,
		IsTruncated:    listResult.IsTruncated,
		NextMarker:     listResult.NextMarker,
		CommonPrefixes: commonPrefixes,
		Contents:       make([]ObjectInfo, len(listResult.Objects)),
	}

	for i, obj := range listResult.Objects {
		result.Contents[i] = ObjectInfo{
			Key:          obj.Key,
			LastModified: obj.LastModified,
			ETag:         obj.ETag,
			Size:         obj.Size,
			StorageClass: "STANDARD",
			Owner: Owner{
				ID:          "maxiofs",
				DisplayName: "MaxIOFS",
			},
		}
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}

// Object operations
func (h *Handler) GetObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := getObjectKey(r)

	// Add S3-compatible headers (CRITICAL for Veeam recognition)
	addS3CompatHeaders(w)

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: GetObject")

	// Check if user is authenticated
	user, userExists := auth.GetUserFromContext(r.Context())

	logrus.WithFields(logrus.Fields{
		"bucket":     bucketName,
		"object":     objectKey,
		"userExists": userExists,
		"userID": func() string {
			if user != nil {
				return user.ID
			} else {
				return "nil"
			}
		}(),
		"hasAuth": r.Header.Get("Authorization") != "",
	}).Info("GetObject: User authentication status")

	// Track if access is allowed via presigned URL
	allowedByPresignedURL := false

	// Extract tenant ID for permission checking
	tenantID := h.getTenantIDFromRequest(r)

	// If NOT authenticated, check if this is a presigned URL request
	if !userExists && presigned.IsPresignedURL(r) {
		// Extract access key from presigned URL
		accessKeyID := presigned.ExtractAccessKeyID(r)
		if accessKeyID == "" {
			h.writeError(w, "InvalidRequest", "Invalid presigned URL: missing access key", objectKey, r)
			return
		}

		// Get access key to retrieve secret
		if h.authManager == nil {
			h.writeError(w, "InternalError", "Auth manager not configured", objectKey, r)
			return
		}

		accessKey, err := h.authManager.GetAccessKey(r.Context(), accessKeyID)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"accessKeyID": accessKeyID,
				"error":       err.Error(),
			}).Warn("Presigned URL: access key not found")
			h.writeError(w, "InvalidAccessKeyId", "The AWS access key ID you provided does not exist in our records", objectKey, r)
			return
		}

		// Validate presigned URL signature
		valid, err := presigned.ValidatePresignedURL(r, accessKey.SecretAccessKey)
		if err != nil || !valid {
			logrus.WithFields(logrus.Fields{
				"accessKeyID": accessKeyID,
				"error":       err,
			}).Warn("Presigned URL validation failed")
			h.writeError(w, "SignatureDoesNotMatch", "The request signature we calculated does not match the signature you provided", objectKey, r)
			return
		}

		// Presigned URL is valid - mark as allowed
		allowedByPresignedURL = true
		logrus.WithFields(logrus.Fields{
			"bucket":      bucketName,
			"object":      objectKey,
			"accessKeyID": accessKeyID,
		}).Info("Presigned URL validated successfully")
	}

	// Check if this is a VEEAM SOSAPI virtual object (after authentication check)
	if isVeeamSOSAPIObject(objectKey) {
		// SOSAPI requires authentication - Veeam sends credentials
		if !userExists {
			h.writeError(w, "AccessDenied", "Authentication required", objectKey, r)
			return
		}

		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"object": objectKey,
		}).Info("Serving VEEAM SOSAPI virtual object (authenticated)")

		data, contentType, err := h.getSOSAPIVirtualObject(objectKey)
		if err != nil {
			h.writeError(w, "InternalError", err.Error(), objectKey, r)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("ETag", `"sosapi-virtual-object"`)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		w.Write(data)
		return
	}

	// If NOT authenticated and NOT allowed by presigned URL, check if object has an active share (Public Link)
	// We need to handle two URL formats:
	// 1. /bucket/object (global bucket)
	// 2. /tenant-xxx/bucket/object (tenant bucket)
	var shareTenantID string
	if !userExists && !allowedByPresignedURL && h.shareManager != nil {
		// Extract tenant from bucket name if present
		realBucket := bucketName
		realObject := objectKey
		extractedTenant := ""

		// If bucketName starts with "tenant-", it's actually the tenant ID
		if strings.HasPrefix(bucketName, "tenant-") {
			extractedTenant = bucketName
			// The object key contains bucket/object, split it
			parts := strings.SplitN(objectKey, "/", 2)
			if len(parts) == 2 {
				realBucket = parts[0]
				realObject = parts[1]
			} else {
				realBucket = objectKey
				realObject = ""
			}

			logrus.WithFields(logrus.Fields{
				"originalBucket":  bucketName,
				"originalObject":  objectKey,
				"extractedTenant": extractedTenant,
				"realBucket":      realBucket,
				"realObject":      realObject,
			}).Debug("Extracted tenant from URL path")
		}

		// Try to find share with extracted tenant ID
		shareInterface, err := h.shareManager.GetShareByObject(r.Context(), realBucket, realObject, extractedTenant)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"bucket": realBucket,
				"object": realObject,
				"tenant": extractedTenant,
				"error":  err.Error(),
			}).Warn("Unauthenticated access denied - no active share found")
			h.writeError(w, "AccessDenied", "Access denied. Object is not shared.", objectKey, r)
			return
		}

		// Type assert to *share.Share
		if s, ok := shareInterface.(*share.Share); ok {
			shareTenantID = s.TenantID
			// Override vars for subsequent processing
			bucketName = realBucket
			objectKey = realObject
		}

		logrus.WithFields(logrus.Fields{
			"bucket":   bucketName,
			"object":   objectKey,
			"tenantID": shareTenantID,
		}).Info("Shared object access - bypassing authentication")
	}

	// Build bucket path: use shareTenantID if available, otherwise use auth-based tenant
	// IMPORTANT: Use same logic as PutObject to ensure consistency
	var bucketPath string
	if shareTenantID != "" {
		// Share is active - use tenant from share
		bucketPath = shareTenantID + "/" + bucketName
	} else {
		// No share - use standard bucket path (same as PutObject/ListObjects/DeleteObject)
		bucketPath = h.getBucketPath(r, bucketName)
	}

	logrus.WithFields(logrus.Fields{
		"bucket":        bucketName,
		"object":        objectKey,
		"bucketPath":    bucketPath,
		"shareTenantID": shareTenantID,
		"tenantID":      tenantID,
	}).Info("GetObject: Using bucketPath")

	// 1. Verificar permiso de BUCKET únicamente (NO verificar ACL de objeto aún)
	// El objeto puede no existir, así que solo verificamos permisos de bucket

	var hasBucketRead bool = true
	if userExists && !allowedByPresignedURL && shareTenantID == "" {
		logrus.WithFields(logrus.Fields{
			"userTenantID":   user.TenantID,
			"bucketTenantID": tenantID,
			"isCrossTenant":  user.TenantID != tenantID,
			"userID":         user.ID,
			"bucket":         bucketName,
		}).Info("GetObject: ACL check - comparing tenant IDs")

		// Si es mismo tenant, permitir; si no, verificar permiso de bucket
		if user.TenantID != tenantID {
			// Cross-tenant: verificar permisos de bucket (NO de objeto específico)
			hasBucketRead = h.checkBucketACLPermission(r.Context(), tenantID, bucketName, user.ID, acl.PermissionRead)

			// Si no tiene permiso explícito, verificar si es authenticated-read
			if !hasBucketRead {
				hasBucketRead = h.checkAuthenticatedBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)
			}

			// Si aún no tiene permiso, verificar si el bucket es público
			if !hasBucketRead {
				hasBucketRead = h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)
			}

			if !hasBucketRead {
				logrus.WithFields(logrus.Fields{
					"bucket":       bucketName,
					"object":       objectKey,
					"userID":       user.ID,
					"userTenantID": user.TenantID,
					"bucketTenant": tenantID,
				}).Warn("ACL permission denied for GetObject - cross-tenant access (bucket-level)")
				h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
				return
			}
		}
		// Mismo tenant: acceso permitido
	} else if !userExists && !allowedByPresignedURL && shareTenantID == "" {
		// Sin autenticación: solo verificar si el BUCKET es público (NO el objeto específico)
		hasBucketRead = h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)

		if !hasBucketRead {
			logrus.WithFields(logrus.Fields{
				"bucket": bucketName,
				"object": objectKey,
			}).Warn("Public access denied - bucket not publicly readable (bucket-level)")
			h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
			return
		}
	}

	// 2. Intentar obtener el objeto
	// Si el objeto NO existe, devolver NoSuchKey (404) - esto es correcto para S3
	versionID := r.URL.Query().Get("versionId")
	obj, reader, err := h.objectManager.GetObject(r.Context(), bucketPath, objectKey, versionID)
	if err != nil {
		if err == object.ErrObjectNotFound {
			// Objeto no existe - devolver 404 (comportamiento correcto de S3)
			// VEEAM usa esto para detectar si smart-entity-status.xml existe (primer backup)
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}
	defer reader.Close()

	// Handle conditional requests (If-Match, If-None-Match)
	ifMatch := r.Header.Get("If-Match")
	ifNoneMatch := r.Header.Get("If-None-Match")

	if ifMatch != "" {
		// If-Match: Return 412 if ETag doesn't match
		if obj.ETag != strings.Trim(ifMatch, "\"") {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
	}

	if ifNoneMatch != "" {
		// If-None-Match: Return 304 if ETag matches (resource not modified)
		if obj.ETag == strings.Trim(ifNoneMatch, "\"") {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	// 3. El objeto existe - ahora verificar ACL de objeto (solo para cross-tenant)
	if userExists && !allowedByPresignedURL && shareTenantID == "" && user.TenantID != tenantID {
		hasObjectACL := h.checkObjectACLPermission(r.Context(), bucketPath, objectKey, user.ID, acl.PermissionRead)
		if !hasObjectACL {
			logrus.WithFields(logrus.Fields{
				"bucket":       bucketName,
				"object":       objectKey,
				"userID":       user.ID,
				"userTenantID": user.TenantID,
				"bucketTenant": tenantID,
			}).Warn("ACL permission denied for GetObject - cross-tenant access (object-level)")
			h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
			return
		}
	}

	// Parse Range header if present (for parallel/resumable downloads)
	rangeHeader := r.Header.Get("Range")
	var rangeStart, rangeEnd int64
	var isRangeRequest bool

	if rangeHeader != "" {
		// Parse Range header: "bytes=start-end" or "bytes=start-"
		var parseErr error
		rangeStart, rangeEnd, parseErr = parseRangeHeader(rangeHeader, obj.Size)
		if parseErr != nil {
			// Invalid range - return 416 Range Not Satisfiable
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", obj.Size))
			h.writeError(w, "InvalidRange", parseErr.Error(), objectKey, r)
			return
		}
		isRangeRequest = true

		logrus.WithFields(logrus.Fields{
			"bucket":     bucketName,
			"object":     objectKey,
			"rangeStart": rangeStart,
			"rangeEnd":   rangeEnd,
			"totalSize":  obj.Size,
		}).Debug("GetObject: Range request detected")
	} else {
		// No range header - send entire object
		rangeStart = 0
		rangeEnd = obj.Size - 1
		isRangeRequest = false
	}

	// Calculate content length for this range
	contentLength := rangeEnd - rangeStart + 1

	// Set common response headers
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))

	// Add version ID if available
	if obj.VersionID != "" {
		w.Header().Set("x-amz-version-id", obj.VersionID)
	}

	// Agregar headers de Object Lock si existen (Veeam compatibility)
	if obj.Retention != nil {
		w.Header().Set("x-amz-object-lock-mode", obj.Retention.Mode)
		w.Header().Set("x-amz-object-lock-retain-until-date", obj.Retention.RetainUntilDate.UTC().Format(time.RFC3339))
	}

	if obj.LegalHold != nil && obj.LegalHold.Status == "ON" {
		w.Header().Set("x-amz-object-lock-legal-hold", "ON")
	}

	// Handle range request
	if isRangeRequest {
		// Set 206 Partial Content headers
		w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeStart, rangeEnd, obj.Size))
		w.WriteHeader(http.StatusPartialContent)

		// Skip to start position if needed
		if rangeStart > 0 {
			if seeker, ok := reader.(io.Seeker); ok {
				// Use Seek if available (more efficient)
				if _, err := seeker.Seek(rangeStart, io.SeekStart); err != nil {
					logrus.WithError(err).Error("Failed to seek to range start")
					return
				}
			} else {
				// Fall back to reading and discarding bytes
				if _, err := io.CopyN(io.Discard, reader, rangeStart); err != nil {
					logrus.WithError(err).Error("Failed to skip to range start")
					return
				}
			}
		}

		// Copy only the requested range
		if _, err := io.CopyN(w, reader, contentLength); err != nil && err != io.EOF {
			logrus.WithError(err).Error("Failed to write partial object data")
		}
	} else {
		// Send entire object (no range request)
		w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))

		// Copy object data to response
		if _, err := io.Copy(w, reader); err != nil {
			logrus.WithError(err).Error("Failed to write object data")
		}
	}
}

func (h *Handler) PutObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := getObjectKey(r)

	// IMPORTANT: Detect CopyObject operation by x-amz-copy-source header
	// AWS CLI sends PUT with this header for copy operations
	if copySource := r.Header.Get("x-amz-copy-source"); copySource != "" {
		logrus.WithFields(logrus.Fields{
			"bucket":      bucketName,
			"object":      objectKey,
			"copy-source": copySource,
		}).Debug("S3 API: PutObject detected copy-source header, dispatching to CopyObject")
		h.CopyObject(w, r)
		return
	}

	contentEncoding := r.Header.Get("Content-Encoding")
	decodedContentLength := r.Header.Get("X-Amz-Decoded-Content-Length")

	logrus.WithFields(logrus.Fields{
		"bucket":                 bucketName,
		"object":                 objectKey,
		"content-length":         r.ContentLength,
		"transfer-encoding":      r.Header.Get("Transfer-Encoding"),
		"content-encoding":       contentEncoding,
		"decoded-content-length": decodedContentLength,
		"content-type":           r.Header.Get("Content-Type"),
	}).Debug("S3 API: PutObject - Request headers")

	// Permission check: Verify user has WRITE permission via ACL
	user, userExists := auth.GetUserFromContext(r.Context())
	tenantID := h.getTenantIDFromRequest(r)

	// Check tenant storage quota before accepting upload
	if h.authManager != nil && userExists && user.TenantID != "" {
		// Get content length for quota check
		contentLength := r.ContentLength
		if decodedContentLength != "" {
			if size, err := strconv.ParseInt(decodedContentLength, 10, 64); err == nil {
				contentLength = size
			}
		}

		logrus.WithFields(logrus.Fields{
			"bucket":        bucketName,
			"object":        objectKey,
			"tenantID":      user.TenantID,
			"contentLength": contentLength,
			"hasAuthMgr":    h.authManager != nil,
		}).Debug("Starting quota validation")

		// For quota validation, we need to check the actual storage increment
		// If overwriting an existing object, only validate the size difference
		if contentLength > 0 {
			// Check if object exists to calculate actual storage increment
			var sizeIncrement int64 = contentLength

			// Try to get existing object metadata (use bucketPath for proper tenant isolation)
			bucketPath := h.getBucketPath(r, bucketName)
			existingObj, _, err := h.objectManager.GetObject(r.Context(), bucketPath, objectKey)
			if err == nil && existingObj != nil {
				// Object exists - calculate size difference for quota check
				sizeIncrement = contentLength - existingObj.Size
				logrus.WithFields(logrus.Fields{
					"existingSize":  existingObj.Size,
					"newSize":       contentLength,
					"sizeIncrement": sizeIncrement,
				}).Debug("Object exists, calculating size increment")
			} else {
				logrus.WithField("sizeIncrement", sizeIncrement).Debug("New object, using full content length")
			}

			// Only check quota if we're adding storage (sizeIncrement > 0)
			// If sizeIncrement is negative or zero, we're not adding storage
			if sizeIncrement > 0 {
				logrus.WithFields(logrus.Fields{
					"tenantID":      user.TenantID,
					"sizeIncrement": sizeIncrement,
				}).Info("Checking tenant storage quota")

				if err := h.authManager.CheckTenantStorageQuota(r.Context(), user.TenantID, sizeIncrement); err != nil {
					logrus.WithFields(logrus.Fields{
						"bucket":        bucketName,
						"object":        objectKey,
						"tenantID":      user.TenantID,
						"contentLength": contentLength,
						"existingSize": func() int64 {
							if existingObj != nil {
								return existingObj.Size
							}
							return 0
						}(),
						"sizeIncrement": sizeIncrement,
						"error":         err,
					}).Warn("Tenant storage quota exceeded")
					h.writeError(w, "QuotaExceeded", err.Error(), objectKey, r)
					return
				}
				logrus.Info("Quota check passed")
			} else {
				logrus.WithField("sizeIncrement", sizeIncrement).Debug("Skipping quota check (not adding storage)")
			}
		}
	} else {
		logrus.WithFields(logrus.Fields{
			"hasAuthMgr":  h.authManager != nil,
			"userExists":  userExists,
			"hasTenantID": userExists && user.TenantID != "",
		}).Debug("Skipping quota validation")
	}

	hasPermission := false

	if userExists {
		// If user belongs to the same tenant as the bucket, allow access automatically
		if user.TenantID == tenantID {
			hasPermission = true
		} else {
			// Cross-tenant access - check ACL permissions
			hasPermission = h.checkBucketACLPermission(r.Context(), tenantID, bucketName, user.ID, acl.PermissionWrite)

			// If no explicit ACL permission, check if authenticated users have write access
			if !hasPermission {
				hasPermission = h.checkAuthenticatedBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionWrite)
			}

			// Also check FULL_CONTROL as an alternative
			if !hasPermission {
				hasPermission = h.checkBucketACLPermission(r.Context(), tenantID, bucketName, user.ID, acl.PermissionFullControl)
			}

			// If still no permission, check if public access is allowed
			if !hasPermission {
				hasPermission = h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionWrite)
			}
		}
	} else {
		// Unauthenticated access - check if bucket allows public WRITE
		hasPermission = h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionWrite)
	}

	if !hasPermission {
		logrus.WithFields(logrus.Fields{
			"bucket":        bucketName,
			"object":        objectKey,
			"userID":        getUserIDOrAnonymous(user),
			"authenticated": userExists,
			"userTenantID":  user.TenantID,
			"bucketTenant":  tenantID,
		}).Warn("ACL permission denied for PutObject - cross-tenant access")
		h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
		return
	}

	// CRITICAL: Verify bucket exists before allowing object upload
	// This prevents implicit bucket creation and ensures metadata consistency
	_, err := h.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"bucket":   bucketName,
			"tenantID": tenantID,
			"error":    err,
		}).Warn("PutObject failed: bucket does not exist")
		h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
		return
	}

	bucketPath := h.getBucketPath(r, bucketName)

	// Leer headers de Object Lock si están presentes (para Veeam)
	lockMode := r.Header.Get("x-amz-object-lock-mode")
	retainUntilDateStr := r.Header.Get("x-amz-object-lock-retain-until-date")
	legalHoldStatus := r.Header.Get("x-amz-object-lock-legal-hold")

	// CRITICAL FIX: AWS CLI and warp/MinIO-Go use "aws-chunked" encoding
	// Some clients send the header, others don't - we need to detect by content
	var bodyReader io.Reader = r.Body
	isAwsChunked := strings.Contains(contentEncoding, "aws-chunked") || decodedContentLength != ""

	// If not detected by headers, peek at first bytes to check for aws-chunked format
	if !isAwsChunked {
		// Buffer first 100 bytes to detect aws-chunked format
		buf := make([]byte, 100)
		n, _ := io.ReadFull(r.Body, buf)
		if n > 0 {
			// Check if starts with hex digits followed by ";chunk-signature="
			content := string(buf[:n])
			if strings.Contains(content, ";chunk-signature=") {
				isAwsChunked = true
				logrus.WithFields(logrus.Fields{
					"bucket": bucketName,
					"object": objectKey,
				}).Info("AWS chunked encoding detected by content inspection")
			}
			// Restore the buffer to the reader
			bodyReader = io.MultiReader(bytes.NewReader(buf[:n]), r.Body)
		}
	}

	if isAwsChunked {
		logrus.WithFields(logrus.Fields{
			"bucket":                 bucketName,
			"object":                 objectKey,
			"decoded-content-length": decodedContentLength,
		}).Info("AWS chunked encoding detected - decoding")

		bodyReader = NewAwsChunkedReader(bodyReader)

		// Update Content-Length header for storage layer
		if decodedContentLength != "" {
			if size, err := strconv.ParseInt(decodedContentLength, 10, 64); err == nil {
				r.ContentLength = size
				r.Header.Set("Content-Length", decodedContentLength)
			}
		}

		// Remove aws-chunked from Content-Encoding for storage
		r.Header.Del("Content-Encoding")
	}

	logrus.WithFields(logrus.Fields{
		"bucket":     bucketName,
		"object":     objectKey,
		"bucketPath": bucketPath,
	}).Info("PutObject: Using bucketPath")

	obj, err := h.objectManager.PutObject(r.Context(), bucketPath, objectKey, bodyReader, r.Header)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"bucket": bucketName,
			"object": objectKey,
		}).Error("PutObject failed")

		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Aplicar retención si se especificó en headers (Veeam compatibility)
	retentionApplied := false
	if lockMode != "" && retainUntilDateStr != "" {
		retainUntilDate, parseErr := time.Parse(time.RFC3339, retainUntilDateStr)
		if parseErr == nil {
			retention := &object.RetentionConfig{
				Mode:            lockMode,
				RetainUntilDate: retainUntilDate,
			}
			if setErr := h.objectManager.SetObjectRetention(r.Context(), bucketPath, objectKey, retention); setErr != nil {
				logrus.WithError(setErr).Warn("Failed to set retention from headers")
			} else {
				logrus.WithFields(logrus.Fields{
					"bucket": bucketName,
					"object": objectKey,
					"mode":   lockMode,
					"until":  retainUntilDate,
				}).Info("Applied Object Lock retention from headers")
				retentionApplied = true
			}
		} else {
			logrus.WithError(parseErr).Warn("Failed to parse retain-until-date header")
		}
	}

	// Si no se aplicó retención desde headers, aplicar la retención por defecto del bucket
	if !retentionApplied {
		tenantID := h.getTenantIDFromRequest(r)
		lockConfig, err := h.bucketManager.GetObjectLockConfig(r.Context(), tenantID, bucketName)
		if err == nil && lockConfig != nil && lockConfig.ObjectLockEnabled {
			// Apply default retention if configured
			if lockConfig.Rule != nil && lockConfig.Rule.DefaultRetention != nil {
				retention := &object.RetentionConfig{
					Mode: lockConfig.Rule.DefaultRetention.Mode,
				}

				// Calculate retain until date based on days or years
				if lockConfig.Rule.DefaultRetention.Days != nil {
					retention.RetainUntilDate = time.Now().AddDate(0, 0, *lockConfig.Rule.DefaultRetention.Days)
				} else if lockConfig.Rule.DefaultRetention.Years != nil {
					retention.RetainUntilDate = time.Now().AddDate(*lockConfig.Rule.DefaultRetention.Years, 0, 0)
				}

				// Set retention on the newly uploaded object
				if !retention.RetainUntilDate.IsZero() {
					if setErr := h.objectManager.SetObjectRetention(r.Context(), bucketPath, objectKey, retention); setErr != nil {
						logrus.WithError(setErr).Warn("Failed to apply default bucket retention")
					} else {
						logrus.WithFields(logrus.Fields{
							"bucket": bucketName,
							"object": objectKey,
							"mode":   retention.Mode,
							"until":  retention.RetainUntilDate,
						}).Info("Applied default bucket retention")
					}
				}
			}
		}
	}

	// Aplicar legal hold si se especificó (Veeam compatibility)
	if legalHoldStatus == "ON" {
		legalHold := &object.LegalHoldConfig{Status: "ON"}
		if setErr := h.objectManager.SetObjectLegalHold(r.Context(), bucketPath, objectKey, legalHold); setErr != nil {
			logrus.WithError(setErr).Warn("Failed to set legal hold from headers")
		} else {
			logrus.WithFields(logrus.Fields{
				"bucket": bucketName,
				"object": objectKey,
			}).Info("Applied legal hold from headers")
		}
	}

	// Note: Bucket metrics and tenant storage are updated by objectManager.PutObject()
	// No need to increment here to avoid double-counting on overwrites

	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))

	// Return version ID if versioning is enabled
	if obj.VersionID != "" {
		w.Header().Set("x-amz-version-id", obj.VersionID)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) DeleteObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := getObjectKey(r)

	// Get versionId if specified (for permanent deletion)
	versionID := r.URL.Query().Get("versionId")

	// Check for bypass governance retention header
	bypassGovernance := r.Header.Get("x-amz-bypass-governance-retention") == "true"

	logrus.WithFields(logrus.Fields{
		"bucket":           bucketName,
		"object":           objectKey,
		"versionId":        versionID,
		"bypassGovernance": bypassGovernance,
	}).Debug("S3 API: DeleteObject")

	// Permission check: Verify user has WRITE permission via ACL
	user, userExists := auth.GetUserFromContext(r.Context())
	tenantID := h.getTenantIDFromRequest(r)
	bucketPath := h.getBucketPath(r, bucketName)

	hasPermission := false

	if userExists {
		// If user belongs to the same tenant as the bucket, allow access automatically
		if user.TenantID == tenantID {
			hasPermission = true
		} else {
			// Cross-tenant access - check ACL permissions
			hasPermission = h.checkObjectACLPermission(r.Context(), bucketPath, objectKey, user.ID, acl.PermissionWrite)

			// If no explicit object ACL, check bucket WRITE permission
			if !hasPermission {
				hasPermission = h.checkBucketACLPermission(r.Context(), tenantID, bucketName, user.ID, acl.PermissionWrite)
			}

			// Check if authenticated users have write access
			if !hasPermission {
				hasPermission = h.checkAuthenticatedBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionWrite)
			}

			// Also check FULL_CONTROL as an alternative
			if !hasPermission {
				hasPermission = h.checkBucketACLPermission(r.Context(), tenantID, bucketName, user.ID, acl.PermissionFullControl)
			}

			// If still no permission, check if public access is allowed
			if !hasPermission {
				hasPermission = h.checkPublicObjectAccess(r.Context(), bucketPath, objectKey, acl.PermissionWrite)
				if !hasPermission {
					hasPermission = h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionWrite)
				}
			}
		}
	} else {
		// Unauthenticated access - check if bucket/object allows public WRITE
		hasPermission = h.checkPublicObjectAccess(r.Context(), bucketPath, objectKey, acl.PermissionWrite)
		if !hasPermission {
			hasPermission = h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionWrite)
		}
	}

	if !hasPermission {
		logrus.WithFields(logrus.Fields{
			"bucket":        bucketName,
			"object":        objectKey,
			"userID":        getUserIDOrAnonymous(user),
			"authenticated": userExists,
		}).Warn("ACL permission denied for DeleteObject")
		h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
		return
	}

	// If bypass governance is requested, validate user has admin permission
	if bypassGovernance {
		if !userExists {
			h.writeError(w, "AccessDenied", "Authentication required for bypass governance retention", objectKey, r)
			return
		}

		// Verify user has permission to bypass governance
		isAdmin := false
		for _, role := range user.Roles {
			if role == "admin" {
				isAdmin = true
				break
			}
		}

		if !isAdmin {
			h.writeError(w, "AccessDenied", "Only administrators can bypass governance retention", objectKey, r)
			return
		}
	}

	// Get object info before deletion to track size for metrics
	var objectSize int64
	objInfo, reader, err := h.objectManager.GetObject(r.Context(), bucketPath, objectKey, versionID)
	if err == nil && objInfo != nil {
		objectSize = objInfo.Size
		if reader != nil {
			reader.Close() // Close immediately, we only need the size
		}
	}

	deleteMarkerVersionID, err := h.objectManager.DeleteObject(r.Context(), bucketPath, objectKey, bypassGovernance, versionID)
	if err != nil {
		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}

		// S3 spec: DELETE on non-existent object should return success (idempotent)
		if err == object.ErrObjectNotFound {
			logrus.WithFields(logrus.Fields{
				"bucket": bucketName,
				"object": objectKey,
			}).Debug("DELETE on non-existent object - returning success (S3 spec)")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Check if it's a retention error with detailed information
		if retErr, ok := err.(*object.RetentionError); ok {
			h.writeError(w, "AccessDenied", retErr.Error(), objectKey, r)
			return
		}

		// Check for other Object Lock errors
		if err == object.ErrObjectUnderLegalHold {
			h.writeError(w, "AccessDenied", "Object is under legal hold and cannot be deleted", objectKey, r)
			return
		}

		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Update bucket metrics after successful deletion
	// Only decrement if we actually deleted an object (not just created delete marker)
	if objectSize > 0 && deleteMarkerVersionID == "" {
		if err := h.bucketManager.DecrementObjectCount(r.Context(), tenantID, bucketName, objectSize); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"bucket":   bucketName,
				"object":   objectKey,
				"size":     objectSize,
				"tenantID": tenantID,
			}).Warn("Failed to update bucket metrics after DeleteObject")
		}
	}

	// Update tenant storage usage for quota tracking
	if userExists && user.TenantID != "" && objectSize > 0 && deleteMarkerVersionID == "" {
		if err := h.authManager.DecrementTenantStorage(r.Context(), user.TenantID, objectSize); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"tenantID": user.TenantID,
				"size":     objectSize,
			}).Warn("Failed to decrement tenant storage usage")
		}
	}

	// Set response headers
	if deleteMarkerVersionID != "" {
		// A delete marker was created
		w.Header().Set("x-amz-version-id", deleteMarkerVersionID)
		w.Header().Set("x-amz-delete-marker", "true")
	} else if versionID != "" {
		// A specific version was permanently deleted
		w.Header().Set("x-amz-version-id", versionID)
		w.Header().Set("x-amz-delete-marker", "false")
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) HeadObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := getObjectKey(r)

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: HeadObject")

	// Permission check: Verify user has READ permission on BUCKET (not object yet)
	user, userExists := auth.GetUserFromContext(r.Context())
	tenantID := h.getTenantIDFromRequest(r)
	bucketPath := h.getBucketPath(r, bucketName)

	// Check if this is a VEEAM SOSAPI virtual object (handle early, no ACL needed)
	if isVeeamSOSAPIObject(objectKey) {
		// SOSAPI requires authentication - Veeam sends credentials
		if !userExists {
			h.writeError(w, "AccessDenied", "Authentication required", objectKey, r)
			return
		}

		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"object": objectKey,
		}).Info("HeadObject for VEEAM SOSAPI virtual object")

		data, contentType, err := h.getSOSAPIVirtualObject(objectKey)
		if err != nil {
			h.writeError(w, "InternalError", err.Error(), objectKey, r)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("ETag", `"sosapi-virtual-object"`)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		return
	}

	// Verify BUCKET permissions only (object may not exist)
	var hasBucketRead bool = true
	if userExists {
		// If user belongs to the same tenant as the bucket, allow access automatically
		if user.TenantID != tenantID {
			// Cross-tenant access - check BUCKET ACL permissions only
			hasBucketRead = h.checkBucketACLPermission(r.Context(), tenantID, bucketName, user.ID, acl.PermissionRead)

			// If no explicit ACL permission, check if authenticated users have access
			if !hasBucketRead {
				hasBucketRead = h.checkAuthenticatedBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)
			}

			// If still no permission, check if public access is allowed
			if !hasBucketRead {
				hasBucketRead = h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)
			}

			if !hasBucketRead {
				logrus.WithFields(logrus.Fields{
					"bucket":       bucketName,
					"object":       objectKey,
					"userID":       user.ID,
					"userTenantID": user.TenantID,
					"bucketTenant": tenantID,
				}).Warn("ACL permission denied for HeadObject - cross-tenant access (bucket-level)")
				h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
				return
			}
		}
		// Same tenant - allow access automatically
	} else {
		// Unauthenticated access - check if BUCKET is public (not object specifically)
		hasBucketRead = h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)

		if !hasBucketRead {
			logrus.WithFields(logrus.Fields{
				"bucket": bucketName,
				"object": objectKey,
			}).Warn("Public access denied for HeadObject - bucket not publicly readable")
			h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
			return
		}
	}

	// Try to get object metadata - may return NoSuchKey if doesn't exist
	obj, err := h.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			// Object doesn't exist - return 404 (VEEAM uses this to detect missing files)
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Handle conditional requests (If-Match, If-None-Match) for HeadObject
	ifMatch := r.Header.Get("If-Match")
	ifNoneMatch := r.Header.Get("If-None-Match")

	if ifMatch != "" {
		// If-Match: Return 412 if ETag doesn't match
		if obj.ETag != strings.Trim(ifMatch, "\"") {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
	}

	if ifNoneMatch != "" {
		// If-None-Match: Return 304 if ETag matches (resource not modified)
		if obj.ETag == strings.Trim(ifNoneMatch, "\"") {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	// Object exists - now check object-level ACLs for cross-tenant access
	if userExists && user.TenantID != tenantID {
		hasObjectACL := h.checkObjectACLPermission(r.Context(), bucketPath, objectKey, user.ID, acl.PermissionRead)
		if !hasObjectACL {
			logrus.WithFields(logrus.Fields{
				"bucket":       bucketName,
				"object":       objectKey,
				"userID":       user.ID,
				"userTenantID": user.TenantID,
				"bucketTenant": tenantID,
			}).Warn("ACL permission denied for HeadObject - cross-tenant access (object-level)")
			h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
			return
		}
	}

	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))

	// Agregar headers de Object Lock si existen (Veeam compatibility)
	if obj.Retention != nil {
		w.Header().Set("x-amz-object-lock-mode", obj.Retention.Mode)
		w.Header().Set("x-amz-object-lock-retain-until-date", obj.Retention.RetainUntilDate.UTC().Format(time.RFC3339))
	}

	if obj.LegalHold != nil && obj.LegalHold.Status == "ON" {
		w.Header().Set("x-amz-object-lock-legal-hold", "ON")
	}

	w.WriteHeader(http.StatusOK)
}

// Placeholder implementations for other S3 operations
func (h *Handler) GetBucketLocation(w http.ResponseWriter, r *http.Request) {
	// Add S3-compatible headers (CRITICAL for Veeam recognition)
	addS3CompatHeaders(w)

	// Detect Veeam and log
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	userAgent := r.Header.Get("User-Agent")
	if isVeeamClient(userAgent) {
		logrus.WithFields(logrus.Fields{
			"bucket":     bucketName,
			"user_agent": userAgent,
			"method":     r.Method,
			"uri":        r.RequestURI,
		}).Warn("VEEAM GetBucketLocation - DETECTION PHASE - May determine auto-provisioning")
	}

	h.writeXMLResponse(w, http.StatusOK, `<LocationConstraint>us-east-1</LocationConstraint>`)
}

func (h *Handler) GetBucketVersioning(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"tenant": tenantID,
	}).Debug("S3 API: GetBucketVersioning")

	// Get bucket info
	bkt, err := h.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	// Build versioning response
	status := "Suspended"
	if bkt.Versioning != nil && bkt.Versioning.Status == "Enabled" {
		status = "Enabled"
	}

	h.writeXMLResponse(w, http.StatusOK, fmt.Sprintf(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>%s</Status></VersioningConfiguration>`, status))
}

func (h *Handler) PutBucketVersioning(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"tenant": tenantID,
	}).Debug("S3 API: PutBucketVersioning")

	// Parse versioning configuration from request body
	type VersioningConfiguration struct {
		XMLName xml.Name `xml:"VersioningConfiguration"`
		Status  string   `xml:"Status"`
	}

	var versioningConfig VersioningConfiguration
	if err := xml.NewDecoder(r.Body).Decode(&versioningConfig); err != nil {
		h.writeError(w, "MalformedXML", "The XML you provided was not well-formed", bucketName, r)
		return
	}

	// Validate status
	if versioningConfig.Status != "Enabled" && versioningConfig.Status != "Suspended" {
		h.writeError(w, "IllegalVersioningConfigurationException", "Invalid versioning status", bucketName, r)
		return
	}

	// Set versioning configuration
	config := &bucket.VersioningConfig{
		Status: versioningConfig.Status,
	}

	if err := h.bucketManager.SetVersioning(r.Context(), tenantID, bucketName, config); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Additional placeholder methods for object lock, policies, etc.
func (h *Handler) GetObjectLockConfiguration(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			logrus.WithFields(logrus.Fields{
				"panic": rec,
			}).Error("PANIC in GetObjectLockConfiguration")
		}
	}()

	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
	}).Info("S3 API: GetObjectLockConfiguration - START")

	tenantID := h.getTenantIDFromRequest(r)

	logrus.WithFields(logrus.Fields{
		"bucket":   bucketName,
		"tenantID": tenantID,
	}).Info("GetObjectLockConfiguration - Got tenantID")

	// Permission check: same-tenant users have automatic access
	user, userExists := auth.GetUserFromContext(r.Context())

	logrus.WithFields(logrus.Fields{
		"bucket":     bucketName,
		"userExists": userExists,
		"userID": func() string {
			if userExists {
				return user.ID
			} else {
				return "none"
			}
		}(),
		"userTenant": func() string {
			if userExists {
				return user.TenantID
			} else {
				return "none"
			}
		}(),
	}).Info("GetObjectLockConfiguration - Got user from context")

	if userExists && user.TenantID != tenantID {
		logrus.Info("GetObjectLockConfiguration - Cross-tenant access, checking ACL")
		// Cross-tenant access - check ACL permissions
		hasPermission := h.checkBucketACLPermission(r.Context(), tenantID, bucketName, user.ID, acl.PermissionRead)
		if !hasPermission {
			hasPermission = h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead)
		}
		if !hasPermission {
			logrus.Warn("GetObjectLockConfiguration - Access denied")
			h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
			return
		}
	}

	logrus.Info("GetObjectLockConfiguration - About to call GetBucketInfo")

	// Obtener bucket metadata
	bkt, err := h.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)

	logrus.WithFields(logrus.Fields{
		"bucket":   bucketName,
		"tenantID": tenantID,
		"err":      err,
		"bkt_nil":  bkt == nil,
	}).Info("GetObjectLockConfiguration - Called GetBucketInfo")

	if err != nil {
		if err == bucket.ErrBucketNotFound {
			logrus.Info("GetObjectLockConfiguration - Bucket not found")
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		logrus.WithFields(logrus.Fields{
			"bucket":   bucketName,
			"tenantID": tenantID,
			"error":    err.Error(),
		}).Error("Failed to get bucket info for ObjectLock configuration")
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":         bucketName,
		"objectlock_nil": bkt.ObjectLock == nil,
		"objectlock_enabled": func() bool {
			if bkt.ObjectLock != nil {
				return bkt.ObjectLock.ObjectLockEnabled
			} else {
				return false
			}
		}(),
	}).Info("GetObjectLockConfiguration - Checking ObjectLock status")

	// Verificar si tiene Object Lock habilitado
	if bkt.ObjectLock == nil || !bkt.ObjectLock.ObjectLockEnabled {
		h.writeError(w, "ObjectLockConfigurationNotFoundError",
			"Object Lock configuration does not exist for this bucket", bucketName, r)
		return
	}

	// Construir respuesta XML con configuración real
	config := ObjectLockConfiguration{
		ObjectLockEnabled: "Enabled",
	}

	// Agregar regla de retención por defecto si existe
	if bkt.ObjectLock.Rule != nil && bkt.ObjectLock.Rule.DefaultRetention != nil {
		config.Rule = &ObjectLockRule{
			DefaultRetention: &DefaultRetention{
				Mode: bkt.ObjectLock.Rule.DefaultRetention.Mode,
			},
		}

		if bkt.ObjectLock.Rule.DefaultRetention.Days != nil {
			config.Rule.DefaultRetention.Days = *bkt.ObjectLock.Rule.DefaultRetention.Days
		}
		if bkt.ObjectLock.Rule.DefaultRetention.Years != nil {
			config.Rule.DefaultRetention.Years = *bkt.ObjectLock.Rule.DefaultRetention.Years
		}
	}

	logrus.WithFields(logrus.Fields{
		"bucket":  bucketName,
		"enabled": config.ObjectLockEnabled,
		"hasRule": config.Rule != nil,
	}).Info("Returning Object Lock configuration")

	h.writeXMLResponse(w, http.StatusOK, config)
}

func (h *Handler) PutObjectLockConfiguration(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			logrus.WithFields(logrus.Fields{
				"panic": rec,
			}).Error("PANIC in PutObjectLockConfiguration")
			h.writeError(w, "InternalError", "Internal server error", "", r)
		}
	}()

	bucketName := mux.Vars(r)["bucket"]
	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
	}).Info("S3 API: PutObjectLockConfiguration - START")

	// Obtener tenantID del usuario autenticado
	tenantID := h.getTenantIDFromRequest(r)
	logrus.WithFields(logrus.Fields{
		"tenantID": tenantID,
		"bucket":   bucketName,
	}).Info("PutObjectLockConfiguration - Got tenantID")

	// Verificar permisos
	user, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		logrus.Warn("PutObjectLockConfiguration - No user in context")
		h.writeError(w, "AccessDenied", "Access denied", bucketName, r)
		return
	}

	// Verificar acceso cross-tenant (si no es global admin)
	if user.TenantID != "" && user.TenantID != tenantID {
		logrus.Warn("PutObjectLockConfiguration - Cross-tenant access denied")
		h.writeError(w, "AccessDenied", "Access denied", bucketName, r)
		return
	}

	// Obtener información del bucket
	bucketInfo, err := h.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		if err.Error() == "bucket not found" || err == bucket.ErrBucketNotFound {
			logrus.Info("PutObjectLockConfiguration - Bucket not found")
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		logrus.WithError(err).Error("PutObjectLockConfiguration - Failed to get bucket info")
		h.writeError(w, "InternalError", "Failed to retrieve bucket info", bucketName, r)
		return
	}

	// Verificar que Object Lock esté habilitado
	if bucketInfo.ObjectLock == nil || !bucketInfo.ObjectLock.ObjectLockEnabled {
		logrus.Warn("PutObjectLockConfiguration - Object Lock not enabled")
		h.writeError(w, "ObjectLockConfigurationNotFoundError",
			"Object Lock configuration does not exist for this bucket", bucketName, r)
		return
	}

	// Leer la nueva configuración del body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logrus.WithError(err).Error("PutObjectLockConfiguration - Failed to read request body")
		h.writeError(w, "InvalidRequest", "Failed to read request body", bucketName, r)
		return
	}
	defer r.Body.Close()

	var newConfig ObjectLockConfiguration
	if err := xml.Unmarshal(body, &newConfig); err != nil {
		logrus.WithError(err).Error("PutObjectLockConfiguration - Failed to unmarshal XML")
		h.writeError(w, "MalformedXML", "The XML you provided was not well-formed", bucketName, r)
		return
	}

	// Validar que el modo no haya cambiado (inmutable)
	currentMode := ""
	if bucketInfo.ObjectLock.Rule != nil && bucketInfo.ObjectLock.Rule.DefaultRetention != nil {
		currentMode = bucketInfo.ObjectLock.Rule.DefaultRetention.Mode
	}

	newMode := newConfig.Rule.DefaultRetention.Mode
	if currentMode != "" && newMode != currentMode {
		logrus.WithFields(logrus.Fields{
			"currentMode": currentMode,
			"newMode":     newMode,
		}).Warn("PutObjectLockConfiguration - Attempt to change mode")
		h.writeError(w, "InvalidRequest",
			fmt.Sprintf("Object Lock mode cannot be changed (current: %s)", currentMode),
			bucketName, r)
		return
	}

	// Calcular días actuales
	var currentDays int
	if bucketInfo.ObjectLock.Rule != nil && bucketInfo.ObjectLock.Rule.DefaultRetention != nil {
		if bucketInfo.ObjectLock.Rule.DefaultRetention.Years != nil && *bucketInfo.ObjectLock.Rule.DefaultRetention.Years > 0 {
			currentDays = *bucketInfo.ObjectLock.Rule.DefaultRetention.Years * 365
		} else if bucketInfo.ObjectLock.Rule.DefaultRetention.Days != nil {
			currentDays = *bucketInfo.ObjectLock.Rule.DefaultRetention.Days
		}
	}

	// Calcular días nuevos
	var newDays int
	if newConfig.Rule.DefaultRetention.Years > 0 {
		newDays = newConfig.Rule.DefaultRetention.Years * 365
	} else {
		newDays = newConfig.Rule.DefaultRetention.Days
	}

	// Validar que solo se aumente el período de retención (nunca disminuir)
	if newDays < currentDays {
		logrus.WithFields(logrus.Fields{
			"currentDays": currentDays,
			"newDays":     newDays,
		}).Warn("PutObjectLockConfiguration - Attempt to decrease retention period")
		h.writeError(w, "InvalidRequest",
			fmt.Sprintf("Retention period can only be increased (current: %d days, requested: %d days)",
				currentDays, newDays),
			bucketName, r)
		return
	}

	// Actualizar configuración de Object Lock en el bucket
	if bucketInfo.ObjectLock.Rule == nil {
		bucketInfo.ObjectLock.Rule = &bucket.ObjectLockRule{}
	}
	if bucketInfo.ObjectLock.Rule.DefaultRetention == nil {
		bucketInfo.ObjectLock.Rule.DefaultRetention = &bucket.DefaultRetention{}
	}

	bucketInfo.ObjectLock.Rule.DefaultRetention.Mode = newMode
	if newConfig.Rule.DefaultRetention.Years > 0 {
		years := newConfig.Rule.DefaultRetention.Years
		bucketInfo.ObjectLock.Rule.DefaultRetention.Years = &years
		bucketInfo.ObjectLock.Rule.DefaultRetention.Days = nil
	} else {
		days := newConfig.Rule.DefaultRetention.Days
		bucketInfo.ObjectLock.Rule.DefaultRetention.Days = &days
		bucketInfo.ObjectLock.Rule.DefaultRetention.Years = nil
	}

	// Guardar cambios en el bucket
	if err := h.bucketManager.UpdateBucket(r.Context(), tenantID, bucketName, bucketInfo); err != nil {
		logrus.WithError(err).Error("PutObjectLockConfiguration - Failed to update bucket")
		h.writeError(w, "InternalError", "Failed to update Object Lock configuration", bucketName, r)
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":      bucketName,
		"currentDays": currentDays,
		"newDays":     newDays,
		"mode":        newMode,
	}).Info("PutObjectLockConfiguration - Configuration updated successfully")

	w.WriteHeader(http.StatusOK)
}

// Utility methods
func (h *Handler) writeXMLResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	w.WriteHeader(statusCode)

	if str, ok := data.(string); ok {
		w.Write([]byte(str))
		return
	}

	if err := xml.NewEncoder(w).Encode(data); err != nil {
		logrus.WithError(err).Error("Failed to encode XML response")
	}
}

// getTenantIDFromRequest extracts the tenant ID from the authenticated user in the request context
// Returns empty string for global admin users (who have no tenant) or if user not found
func (h *Handler) getTenantIDFromRequest(r *http.Request) string {
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		return ""
	}

	// Global admins have no tenant ID and can see all buckets
	// Tenant-scoped users/admins have a tenant ID
	return user.TenantID
}

// getBucketPath constructs the full bucket path with tenant prefix for object manager
// Format: "tenantID/bucketName" for tenant buckets, or "bucketName" for global buckets
// This is transparent to S3 clients - they only see "bucketName"
func (h *Handler) getBucketPath(r *http.Request, bucketName string) string {
	// First, try to get tenant from authenticated user
	tenantID := h.getTenantIDFromRequest(r)

	// If no user context (e.g., presigned URL), look up the bucket to find its tenant
	if tenantID == "" && h.metadataStore != nil {
		bucketMeta, err := h.metadataStore.GetBucketByName(r.Context(), bucketName)
		if err == nil && bucketMeta != nil {
			tenantID = bucketMeta.TenantID
		}
	}

	if tenantID == "" {
		return bucketName // Global bucket
	}
	return tenantID + "/" + bucketName // Tenant-scoped bucket path
}

func (h *Handler) writeError(w http.ResponseWriter, code, message, resource string, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml")

	statusCode := http.StatusInternalServerError
	switch code {
	// 400 Bad Request
	case "InvalidArgument", "InvalidBucketName", "InvalidRequest", "MalformedXML":
		statusCode = http.StatusBadRequest
	// 401 Unauthorized
	case "Unauthorized", "InvalidAccessKeyId", "SignatureDoesNotMatch":
		statusCode = http.StatusUnauthorized
	// 403 Forbidden
	case "AccessDenied", "AccountProblem", "AllAccessDisabled":
		statusCode = http.StatusForbidden
	// 404 Not Found
	case "NoSuchBucket", "NoSuchKey", "NoSuchUpload", "ObjectLockConfigurationNotFoundError", "NoSuchBucketPolicy":
		statusCode = http.StatusNotFound
	// 405 Method Not Allowed
	case "MethodNotAllowed":
		statusCode = http.StatusMethodNotAllowed
	// 409 Conflict
	case "BucketAlreadyExists", "BucketNotEmpty", "OperationAborted":
		statusCode = http.StatusConflict
	// 412 Precondition Failed
	case "PreconditionFailed":
		statusCode = http.StatusPreconditionFailed
	// 416 Range Not Satisfiable
	case "InvalidRange":
		statusCode = http.StatusRequestedRangeNotSatisfiable
	// 500 Internal Server Error (default)
	case "InternalError":
		statusCode = http.StatusInternalServerError
	// 501 Not Implemented
	case "NotImplemented":
		statusCode = http.StatusNotImplemented
	// 503 Service Unavailable
	case "ServiceUnavailable", "SlowDown":
		statusCode = http.StatusServiceUnavailable
	}

	// Generate IDs for both headers and XML body BEFORE WriteHeader
	requestID := generateRequestID()
	hostID := generateAmzId2()

	// Set headers BEFORE WriteHeader
	w.Header().Set("X-Amz-Request-Id", requestID)
	w.Header().Set("X-Amz-Id-2", hostID)
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))

	w.WriteHeader(statusCode)

	// Write XML declaration (S3-compatible format)
	w.Write([]byte(xml.Header))

	// Build error response with correct field based on error type
	errorResponse := Error{
		Code:      code,
		Message:   message,
		RequestId: requestID,
		HostId:    hostID,
	}

	// Use correct field based on error type (AWS S3 compatibility)
	switch code {
	case "NoSuchKey", "ObjectNotInActiveTierError":
		// Object errors use Key field
		errorResponse.Key = resource
	case "NoSuchBucket", "BucketAlreadyExists", "BucketNotEmpty":
		// Bucket errors use BucketName field
		errorResponse.BucketName = resource
	default:
		// Other errors use Resource field
		errorResponse.Resource = resource
	}

	// Debug: Log error XML for troubleshooting
	xmlBytes, _ := xml.Marshal(errorResponse)
	logrus.WithFields(logrus.Fields{
		"code":       code,
		"statusCode": statusCode,
		"resource":   resource,
		"xml":        string(xmlBytes),
	}).Debug("writeError: Sending error response")

	xml.NewEncoder(w).Encode(errorResponse)
}

// parseRangeHeader parses HTTP Range header (e.g., "bytes=0-1023" or "bytes=1024-")
// Returns start offset, end offset (inclusive), and error
func parseRangeHeader(rangeHeader string, objectSize int64) (int64, int64, error) {
	// Remove "bytes=" prefix
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, fmt.Errorf("invalid range header format")
	}
	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")

	// Split on comma (we only support single range for now)
	ranges := strings.Split(rangeSpec, ",")
	if len(ranges) > 1 {
		return 0, 0, fmt.Errorf("multiple ranges not supported")
	}

	// Parse start-end
	parts := strings.Split(ranges[0], "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format")
	}

	var start, end int64
	var err error

	// Handle "start-end" format
	if parts[0] != "" && parts[1] != "" {
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range start: %w", err)
		}
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range end: %w", err)
		}
	} else if parts[0] != "" && parts[1] == "" {
		// Handle "start-" format (from start to end of file)
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range start: %w", err)
		}
		end = objectSize - 1
	} else if parts[0] == "" && parts[1] != "" {
		// Handle "-suffix" format (last N bytes)
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range suffix: %w", err)
		}
		start = objectSize - suffix
		if start < 0 {
			start = 0
		}
		end = objectSize - 1
	} else {
		return 0, 0, fmt.Errorf("invalid range format")
	}

	// Validate range
	if start < 0 || start >= objectSize {
		return 0, 0, fmt.Errorf("range start out of bounds")
	}
	if end >= objectSize {
		end = objectSize - 1
	}
	if start > end {
		return 0, 0, fmt.Errorf("range start greater than end")
	}

	return start, end, nil
}

// ACL Permission Checking Helpers

// getUserIDOrAnonymous returns user ID or "anonymous" if user is nil
func getUserIDOrAnonymous(user *auth.User) string {
	if user == nil {
		return "anonymous"
	}
	return user.ID
}

// checkBucketACLPermission checks if a user has permission on a bucket via ACL
// Returns true if access is allowed, false otherwise
func (h *Handler) checkBucketACLPermission(ctx context.Context, tenantID, bucketName, userID string, permission acl.Permission) bool {
	// Get bucket ACL
	bucketACL, err := h.bucketManager.GetBucketACL(ctx, tenantID, bucketName)
	if err != nil {
		// If there's no ACL configured, allow access by default (no restrictions)
		logrus.WithError(err).Debug("No bucket ACL configured, allowing access by default")
		return true
	}

	// Type assert to *acl.ACL
	aclData, ok := bucketACL.(*acl.ACL)
	if !ok || aclData == nil {
		// If ACL is invalid or nil, allow access by default
		logrus.Debug("Invalid or nil bucket ACL, allowing access by default")
		return true
	}

	// Get ACL manager from bucket manager
	aclManager := h.getACLManager()
	if aclManager == nil {
		logrus.Warn("ACL manager not available")
		return false
	}

	// Check if user has permission
	hasPermission := aclManager.CheckPermission(ctx, aclData, userID, permission)

	// If user doesn't have explicit permission, check if AllUsers has permission
	// (authenticated users should inherit public permissions)
	if !hasPermission {
		hasPermission = aclManager.CheckPublicAccess(aclData, permission)
	}

	return hasPermission
}

// checkObjectACLPermission checks if a user has permission on an object via ACL
// Returns true if access is allowed, false otherwise
func (h *Handler) checkObjectACLPermission(ctx context.Context, bucketPath, objectKey, userID string, permission acl.Permission) bool {
	// Get object ACL (bucketPath already contains tenant prefix if needed)
	objectACL, err := h.objectManager.GetObjectACL(ctx, bucketPath, objectKey)
	if err != nil {
		logrus.WithError(err).Warn("Failed to get object ACL for permission check")
		// If object has no ACL, fall back to bucket ACL
		// Extract bucket name from path
		parts := strings.SplitN(bucketPath, "/", 2)
		var tenantID, bucketName string
		if len(parts) == 2 {
			tenantID = parts[0]
			bucketName = parts[1]
		} else {
			tenantID = ""
			bucketName = bucketPath
		}
		return h.checkBucketACLPermission(ctx, tenantID, bucketName, userID, permission)
	}

	// Convert from object.ACL to acl.ACL
	aclData := h.convertObjectACLToInternal(objectACL)
	if aclData == nil {
		logrus.Warn("Failed to convert object ACL")
		// Extract bucket name from path
		parts := strings.SplitN(bucketPath, "/", 2)
		var tenantID, bucketName string
		if len(parts) == 2 {
			tenantID = parts[0]
			bucketName = parts[1]
		} else {
			tenantID = ""
			bucketName = bucketPath
		}
		return h.checkBucketACLPermission(ctx, tenantID, bucketName, userID, permission)
	}

	// Get ACL manager
	aclManager := h.getACLManager()
	if aclManager == nil {
		logrus.Warn("ACL manager not available")
		return false
	}

	// Check if user has permission on object ACL
	hasPermission := aclManager.CheckPermission(ctx, aclData, userID, permission)

	// If user doesn't have explicit permission, check if AllUsers has permission on object
	if !hasPermission {
		hasPermission = aclManager.CheckPublicAccess(aclData, permission)
	}

	// If object ACL doesn't grant permission, fall back to bucket ACL
	// This allows objects to inherit bucket-level permissions
	if !hasPermission {
		parts := strings.SplitN(bucketPath, "/", 2)
		var tenantID, bucketName string
		if len(parts) == 2 {
			tenantID = parts[0]
			bucketName = parts[1]
		} else {
			tenantID = ""
			bucketName = bucketPath
		}
		return h.checkBucketACLPermission(ctx, tenantID, bucketName, userID, permission)
	}

	return hasPermission
}

// checkPublicBucketAccess checks if a bucket allows public access via ACL
func (h *Handler) checkPublicBucketAccess(ctx context.Context, tenantID, bucketName string, permission acl.Permission) bool {
	// Get bucket ACL
	bucketACL, err := h.bucketManager.GetBucketACL(ctx, tenantID, bucketName)
	if err != nil {
		return false
	}

	aclData, ok := bucketACL.(*acl.ACL)
	if !ok || aclData == nil {
		return false
	}

	aclManager := h.getACLManager()
	if aclManager == nil {
		return false
	}

	return aclManager.CheckPublicAccess(aclData, permission)
}

// checkPublicObjectAccess checks if an object allows public access via ACL
func (h *Handler) checkPublicObjectAccess(ctx context.Context, bucketPath, objectKey string, permission acl.Permission) bool {
	// Get object ACL (bucketPath already contains tenant prefix if needed)
	objectACL, err := h.objectManager.GetObjectACL(ctx, bucketPath, objectKey)
	if err != nil {
		// If object has no ACL, check bucket ACL
		// Extract bucket name from path
		parts := strings.SplitN(bucketPath, "/", 2)
		var tenantID, bucketName string
		if len(parts) == 2 {
			tenantID = parts[0]
			bucketName = parts[1]
		} else {
			tenantID = ""
			bucketName = bucketPath
		}
		return h.checkPublicBucketAccess(ctx, tenantID, bucketName, permission)
	}

	aclData := h.convertObjectACLToInternal(objectACL)
	if aclData == nil {
		// Extract bucket name from path
		parts := strings.SplitN(bucketPath, "/", 2)
		var tenantID, bucketName string
		if len(parts) == 2 {
			tenantID = parts[0]
			bucketName = parts[1]
		} else {
			tenantID = ""
			bucketName = bucketPath
		}
		return h.checkPublicBucketAccess(ctx, tenantID, bucketName, permission)
	}

	aclManager := h.getACLManager()
	if aclManager == nil {
		return false
	}

	return aclManager.CheckPublicAccess(aclData, permission)
}

// checkAuthenticatedBucketAccess checks if a bucket allows access to any authenticated user
func (h *Handler) checkAuthenticatedBucketAccess(ctx context.Context, tenantID, bucketName string, permission acl.Permission) bool {
	bucketACL, err := h.bucketManager.GetBucketACL(ctx, tenantID, bucketName)
	if err != nil {
		return false
	}

	aclData, ok := bucketACL.(*acl.ACL)
	if !ok || aclData == nil {
		return false
	}

	aclManager := h.getACLManager()
	if aclManager == nil {
		return false
	}

	return aclManager.CheckAuthenticatedAccess(aclData, permission)
}

// getACLManager extracts the ACL manager from bucket manager
// This is a helper to access the internal ACL manager
func (h *Handler) getACLManager() acl.Manager {
	// Try to extract ACL manager from bucket manager
	if bm, ok := h.bucketManager.(interface{ GetACLManager() interface{} }); ok {
		aclMgr := bm.GetACLManager()
		if mgr, ok := aclMgr.(acl.Manager); ok {
			return mgr
		}
	}
	return nil
}

// convertObjectACLToInternal converts object.ACL to acl.ACL
func (h *Handler) convertObjectACLToInternal(objACL *object.ACL) *acl.ACL {
	if objACL == nil {
		return nil
	}

	aclData := &acl.ACL{
		Owner: acl.Owner{
			ID:          objACL.Owner.ID,
			DisplayName: objACL.Owner.DisplayName,
		},
		Grants: make([]acl.Grant, len(objACL.Grants)),
	}

	for i, grant := range objACL.Grants {
		aclData.Grants[i] = acl.Grant{
			Grantee: acl.Grantee{
				Type:         acl.GranteeType(grant.Grantee.Type),
				ID:           grant.Grantee.ID,
				DisplayName:  grant.Grantee.DisplayName,
				EmailAddress: grant.Grantee.EmailAddress,
				URI:          grant.Grantee.URI,
			},
			Permission: acl.Permission(grant.Permission),
		}
	}

	return aclData
}

// Placeholder stubs for future implementation
func (h *Handler) GetObjectVersions(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}
func (h *Handler) DeleteObjectVersion(w http.ResponseWriter, r *http.Request) {
	// This handler is called when DELETE has versionId query parameter
	// Redirect to DeleteObject which now handles versionId
	h.DeleteObject(w, r)
}
func (h *Handler) PresignedOperation(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}

// Note: The following operations are now implemented in separate files:
// - bucket_ops.go: Bucket Policy, Lifecycle, CORS operations
// - object_ops.go: Object Lock, Tagging, ACL, CopyObject operations
// - multipart.go: Multipart Upload operations
