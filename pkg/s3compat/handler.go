package s3compat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/share"
	"github.com/sirupsen/logrus"
)

// generateRequestID generates a SHORT request ID (like MinIO does)
// MinIO uses 16 character hex strings, not 32
func generateRequestID() string {
	b := make([]byte, 8) // 8 bytes = 16 hex chars
	rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

// generateAmzId2 generates a LONG hash for x-amz-id-2 (like MinIO does)
// MinIO uses 64 character hex strings
func generateAmzId2() string {
	b := make([]byte, 32) // 32 bytes = 64 hex chars
	rand.Read(b)
	return hex.EncodeToString(b)
}

// addMinIOHeaders adds MinIO-compatible headers to all S3 responses
// This is critical for Veeam to recognize the server as MinIO
func addMinIOHeaders(w http.ResponseWriter) {
	// x-amz-request-id: SHORT request ID (16 chars like MinIO)
	w.Header().Set("X-Amz-Request-Id", generateRequestID())

	// x-amz-id-2: LONG host ID hash (64 chars like MinIO)
	w.Header().Set("X-Amz-Id-2", generateAmzId2())

	// Server header identifying as MinIO
	w.Header().Set("Server", "MinIO")

	// Accept ranges for partial content
	w.Header().Set("Accept-Ranges", "bytes")

	// Security headers (exactly like MinIO)
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
		GetShareByObject(ctx context.Context, bucketName, objectKey string) (interface{}, error)
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
	GetShareByObject(ctx context.Context, bucketName, objectKey string) (interface{}, error)
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
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource"`
	RequestId string   `xml:"RequestId"`
	HostId    string   `xml:"HostId"` // MinIO includes this field
}

// Service operations
func (h *Handler) ListBuckets(w http.ResponseWriter, r *http.Request) {
	// Add MinIO headers FIRST
	addMinIOHeaders(w)

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
		}).Warn("VEEAM ListBuckets - RESPONSE HEADERS - Compare with MinIO")
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

	// Add MinIO-compatible headers (CRITICAL for Veeam recognition)
	addMinIOHeaders(w)

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

	tenantID := h.getTenantIDFromRequest(r)
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

	// Add MinIO-compatible headers (CRITICAL for Veeam recognition)
	addMinIOHeaders(w)

	// Detect Veeam and log ALL response headers
	userAgent := r.Header.Get("User-Agent")
	if isVeeamClient(userAgent) {
		logrus.WithFields(logrus.Fields{
			"bucket":           bucketName,
			"user_agent":       userAgent,
			"method":           r.Method,
			"uri":              r.RequestURI,
			"response_headers": w.Header(),
		}).Warn("VEEAM HeadBucket - RESPONSE HEADERS - Compare with MinIO")
	}

	logrus.WithField("bucket", bucketName).Debug("S3 API: HeadBucket")

	tenantID := h.getTenantIDFromRequest(r)
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
	objectKey := vars["object"]

	// Add MinIO-compatible headers (CRITICAL for Veeam recognition)
	addMinIOHeaders(w)

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: GetObject")

	// Check if user is authenticated
	_, userExists := auth.GetUserFromContext(r.Context())

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

	// If NOT authenticated, check if object has an active share
	var shareTenantID string
	if !userExists && h.shareManager != nil {
		shareInterface, err := h.shareManager.GetShareByObject(r.Context(), bucketName, objectKey)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"bucket": bucketName,
				"object": objectKey,
			}).Warn("Unauthenticated access denied - no active share found")
			h.writeError(w, "AccessDenied", "Access denied. Object is not shared.", objectKey, r)
			return
		}

		// Type assert to *share.Share
		if s, ok := shareInterface.(*share.Share); ok {
			shareTenantID = s.TenantID
		}

		logrus.WithFields(logrus.Fields{
			"bucket":   bucketName,
			"object":   objectKey,
			"tenantID": shareTenantID,
		}).Info("Shared object access - bypassing authentication")
	}

	// Build bucket path: use shareTenantID if available, otherwise use auth-based tenant
	var bucketPath string
	if shareTenantID != "" {
		bucketPath = shareTenantID + "/" + bucketName
	} else if !userExists && shareTenantID == "" {
		// Share exists but with empty tenantID (global bucket)
		bucketPath = bucketName
	} else {
		bucketPath = h.getBucketPath(r, bucketName)
	}
	obj, reader, err := h.objectManager.GetObject(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}
	defer reader.Close()

	// Set response headers
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

	// Copy object data to response
	if _, err := io.Copy(w, reader); err != nil {
		logrus.WithError(err).Error("Failed to write object data")
	}
}

func (h *Handler) PutObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: PutObject")

	// Leer headers de Object Lock si están presentes (para Veeam)
	lockMode := r.Header.Get("x-amz-object-lock-mode")
	retainUntilDateStr := r.Header.Get("x-amz-object-lock-retain-until-date")
	legalHoldStatus := r.Header.Get("x-amz-object-lock-legal-hold")

	bucketPath := h.getBucketPath(r, bucketName)
	obj, err := h.objectManager.PutObject(r.Context(), bucketPath, objectKey, r.Body, r.Header)
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
			}
		} else {
			logrus.WithError(parseErr).Warn("Failed to parse retain-until-date header")
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

	w.Header().Set("ETag", obj.ETag)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) DeleteObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: DeleteObject")

	bucketPath := h.getBucketPath(r, bucketName)
	if err := h.objectManager.DeleteObject(r.Context(), bucketPath, objectKey); err != nil {
		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}

		// S3 spec: DELETE on non-existent object should return success (idempotent)
		if err == object.ErrObjectNotFound {
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

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) HeadObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: HeadObject")

	// Check if this is a VEEAM SOSAPI virtual object
	if isVeeamSOSAPIObject(objectKey) {
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

	bucketPath := h.getBucketPath(r, bucketName)
	obj, err := h.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
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
	// Add MinIO-compatible headers (CRITICAL for Veeam recognition)
	addMinIOHeaders(w)

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
	h.writeXMLResponse(w, http.StatusOK, `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`)
}

func (h *Handler) PutBucketVersioning(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// Additional placeholder methods for object lock, policies, etc.
func (h *Handler) GetObjectLockConfiguration(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
	}).Debug("S3 API: GetObjectLockConfiguration")

	tenantID := h.getTenantIDFromRequest(r)
	// Obtener bucket metadata
	bkt, err := h.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

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
	w.WriteHeader(http.StatusOK)
}

// Utility methods
func (h *Handler) writeXMLResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/xml")
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
	tenantID := h.getTenantIDFromRequest(r)
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
	case "NoSuchBucket", "NoSuchKey", "NoSuchUpload":
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

	w.WriteHeader(statusCode)

	// Write XML declaration (like MinIO does)
	w.Write([]byte(xml.Header))

	errorResponse := Error{
		Code:      code,
		Message:   message,
		Resource:  resource,
		RequestId: requestID, // Use the generated ID
		HostId:    hostID,    // Use the generated ID
	}

	xml.NewEncoder(w).Encode(errorResponse)
}

// Placeholder stubs for future implementation
func (h *Handler) GetObjectVersions(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}
func (h *Handler) DeleteObjectVersion(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}
func (h *Handler) PresignedOperation(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}

// Note: The following operations are now implemented in separate files:
// - bucket_ops.go: Bucket Policy, Lifecycle, CORS operations
// - object_ops.go: Object Lock, Tagging, ACL, CopyObject operations
// - multipart.go: Multipart Upload operations
