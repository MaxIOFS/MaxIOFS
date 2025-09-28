package s3compat

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// Handler implements S3-compatible API handlers
type Handler struct {
	bucketManager bucket.Manager
	objectManager object.Manager
}

// NewHandler creates a new S3 compatibility handler
func NewHandler(bucketManager bucket.Manager, objectManager object.Manager) *Handler {
	return &Handler{
		bucketManager: bucketManager,
		objectManager: objectManager,
	}
}

// S3 XML response structures
type ListAllMyBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
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
	XMLName     xml.Name      `xml:"ListBucketResult"`
	Name        string        `xml:"Name"`
	Prefix      string        `xml:"Prefix"`
	Marker      string        `xml:"Marker"`
	MaxKeys     int           `xml:"MaxKeys"`
	IsTruncated bool          `xml:"IsTruncated"`
	Contents    []ObjectInfo  `xml:"Contents"`
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
}

// Service operations
func (h *Handler) ListBuckets(w http.ResponseWriter, r *http.Request) {
	logrus.Debug("S3 API: ListBuckets")

	buckets, err := h.bucketManager.ListBuckets(r.Context())
	if err != nil {
		h.writeError(w, "InternalError", err.Error(), "", r)
		return
	}

	result := ListAllMyBucketsResult{
		Owner: Owner{
			ID:          "maxiofs",
			DisplayName: "MaxIOFS",
		},
		Buckets: Buckets{
			Bucket: make([]BucketInfo, len(buckets)),
		},
	}

	for i, bucket := range buckets {
		result.Buckets.Bucket[i] = BucketInfo{
			Name:         bucket.Name,
			CreationDate: bucket.CreatedAt,
		}
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}

// Bucket operations
func (h *Handler) CreateBucket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: CreateBucket")

	if err := h.bucketManager.CreateBucket(r.Context(), bucketName); err != nil {
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

	if err := h.bucketManager.DeleteBucket(r.Context(), bucketName); err != nil {
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

	logrus.WithField("bucket", bucketName).Debug("S3 API: HeadBucket")

	exists, err := h.bucketManager.BucketExists(r.Context(), bucketName)
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

	objects, truncated, err := h.objectManager.ListObjects(r.Context(), bucketName, prefix, delimiter, marker, maxKeys)
	if err != nil {
		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	result := ListBucketResult{
		Name:        bucketName,
		Prefix:      prefix,
		Marker:      marker,
		MaxKeys:     maxKeys,
		IsTruncated: truncated,
		Contents:    make([]ObjectInfo, len(objects)),
	}

	for i, obj := range objects {
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

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: GetObject")

	obj, reader, err := h.objectManager.GetObject(r.Context(), bucketName, objectKey)
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

	// Copy object data to response
	if _, err := reader.WriteTo(w); err != nil {
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

	obj, err := h.objectManager.PutObject(r.Context(), bucketName, objectKey, r.Body, r.Header)
	if err != nil {
		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
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

	if err := h.objectManager.DeleteObject(r.Context(), bucketName, objectKey); err != nil {
		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
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

	obj, err := h.objectManager.GetObjectMetadata(r.Context(), bucketName, objectKey)
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
	w.WriteHeader(http.StatusOK)
}

// Placeholder implementations for other S3 operations
func (h *Handler) GetBucketLocation(w http.ResponseWriter, r *http.Request) {
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
	h.writeXMLResponse(w, http.StatusOK, `<ObjectLockConfiguration><ObjectLockEnabled>Enabled</ObjectLockEnabled></ObjectLockConfiguration>`)
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

func (h *Handler) writeError(w http.ResponseWriter, code, message, resource string, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml")

	statusCode := http.StatusInternalServerError
	switch code {
	case "NoSuchBucket", "NoSuchKey":
		statusCode = http.StatusNotFound
	case "BucketAlreadyExists":
		statusCode = http.StatusConflict
	case "BucketNotEmpty":
		statusCode = http.StatusConflict
	}

	w.WriteHeader(statusCode)

	errorResponse := Error{
		Code:      code,
		Message:   message,
		Resource:  resource,
		RequestId: r.Header.Get("X-Request-ID"),
	}

	xml.NewEncoder(w).Encode(errorResponse)
}

// Additional method stubs - these would need full implementation
func (h *Handler) GetBucketPolicy(w http.ResponseWriter, r *http.Request)     { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) PutBucketPolicy(w http.ResponseWriter, r *http.Request)     { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) DeleteBucketPolicy(w http.ResponseWriter, r *http.Request)  { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) GetBucketLifecycle(w http.ResponseWriter, r *http.Request)  { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) PutBucketLifecycle(w http.ResponseWriter, r *http.Request)  { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) DeleteBucketLifecycle(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) GetBucketCORS(w http.ResponseWriter, r *http.Request)       { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) PutBucketCORS(w http.ResponseWriter, r *http.Request)       { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) DeleteBucketCORS(w http.ResponseWriter, r *http.Request)    { w.WriteHeader(http.StatusNotImplemented) }

// Object-specific operations that need implementation
func (h *Handler) GetObjectVersions(w http.ResponseWriter, r *http.Request)    { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) DeleteObjectVersion(w http.ResponseWriter, r *http.Request)  { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) GetObjectRetention(w http.ResponseWriter, r *http.Request)   { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) PutObjectRetention(w http.ResponseWriter, r *http.Request)   { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) GetObjectLegalHold(w http.ResponseWriter, r *http.Request)   { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) PutObjectLegalHold(w http.ResponseWriter, r *http.Request)   { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) GetObjectACL(w http.ResponseWriter, r *http.Request)         { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) PutObjectACL(w http.ResponseWriter, r *http.Request)         { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) GetObjectTagging(w http.ResponseWriter, r *http.Request)     { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) PutObjectTagging(w http.ResponseWriter, r *http.Request)     { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) DeleteObjectTagging(w http.ResponseWriter, r *http.Request)  { w.WriteHeader(http.StatusNotImplemented) }

// Multipart upload operations
func (h *Handler) CreateMultipartUpload(w http.ResponseWriter, r *http.Request)   { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) ListMultipartUploads(w http.ResponseWriter, r *http.Request)    { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) UploadPart(w http.ResponseWriter, r *http.Request)             { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) ListParts(w http.ResponseWriter, r *http.Request)              { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) CompleteMultipartUpload(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) AbortMultipartUpload(w http.ResponseWriter, r *http.Request)    { w.WriteHeader(http.StatusNotImplemented) }

// Copy and presigned operations
func (h *Handler) CopyObject(w http.ResponseWriter, r *http.Request)        { w.WriteHeader(http.StatusNotImplemented) }
func (h *Handler) PresignedOperation(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotImplemented) }