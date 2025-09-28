package api

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/pkg/s3compat"
)

// Handler handles S3 API requests
type Handler struct {
	bucketManager  bucket.Manager
	objectManager  object.Manager
	authManager    auth.Manager
	metricsManager *metrics.Manager
	s3Handler      *s3compat.Handler
}

// NewHandler creates a new API handler
func NewHandler(
	bucketManager bucket.Manager,
	objectManager object.Manager,
	authManager auth.Manager,
	metricsManager *metrics.Manager,
) *Handler {
	s3Handler := s3compat.NewHandler(bucketManager, objectManager)

	return &Handler{
		bucketManager:  bucketManager,
		objectManager:  objectManager,
		authManager:    authManager,
		metricsManager: metricsManager,
		s3Handler:      s3Handler,
	}
}

// RegisterRoutes registers all S3 API routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	// Health check endpoint
	router.HandleFunc("/health", h.handleHealth).Methods("GET")
	router.HandleFunc("/ready", h.handleReady).Methods("GET")

	// S3 API endpoints
	s3Router := router.PathPrefix("/").Subrouter()

	// Service operations
	s3Router.HandleFunc("/", h.s3Handler.ListBuckets).Methods("GET")

	// Bucket operations
	bucketRouter := s3Router.PathPrefix("/{bucket}").Subrouter()

	// Bucket management
	bucketRouter.HandleFunc("", h.s3Handler.HeadBucket).Methods("HEAD")
	bucketRouter.HandleFunc("", h.s3Handler.CreateBucket).Methods("PUT")
	bucketRouter.HandleFunc("", h.s3Handler.DeleteBucket).Methods("DELETE")
	bucketRouter.HandleFunc("", h.s3Handler.ListObjects).Methods("GET")

	// Bucket configuration endpoints
	bucketRouter.HandleFunc("", h.s3Handler.GetBucketLocation).Methods("GET").Queries("location", "")
	bucketRouter.HandleFunc("", h.s3Handler.GetBucketVersioning).Methods("GET").Queries("versioning", "")
	bucketRouter.HandleFunc("", h.s3Handler.PutBucketVersioning).Methods("PUT").Queries("versioning", "")
	bucketRouter.HandleFunc("", h.s3Handler.GetBucketPolicy).Methods("GET").Queries("policy", "")
	bucketRouter.HandleFunc("", h.s3Handler.PutBucketPolicy).Methods("PUT").Queries("policy", "")
	bucketRouter.HandleFunc("", h.s3Handler.DeleteBucketPolicy).Methods("DELETE").Queries("policy", "")

	// Object Lock configuration
	bucketRouter.HandleFunc("", h.s3Handler.GetObjectLockConfiguration).Methods("GET").Queries("object-lock", "")
	bucketRouter.HandleFunc("", h.s3Handler.PutObjectLockConfiguration).Methods("PUT").Queries("object-lock", "")

	// Lifecycle configuration
	bucketRouter.HandleFunc("", h.s3Handler.GetBucketLifecycle).Methods("GET").Queries("lifecycle", "")
	bucketRouter.HandleFunc("", h.s3Handler.PutBucketLifecycle).Methods("PUT").Queries("lifecycle", "")
	bucketRouter.HandleFunc("", h.s3Handler.DeleteBucketLifecycle).Methods("DELETE").Queries("lifecycle", "")

	// CORS configuration
	bucketRouter.HandleFunc("", h.s3Handler.GetBucketCORS).Methods("GET").Queries("cors", "")
	bucketRouter.HandleFunc("", h.s3Handler.PutBucketCORS).Methods("PUT").Queries("cors", "")
	bucketRouter.HandleFunc("", h.s3Handler.DeleteBucketCORS).Methods("DELETE").Queries("cors", "")

	// Object operations
	objectRouter := bucketRouter.PathPrefix("/{object:.+}").Subrouter()

	// Basic object operations
	objectRouter.HandleFunc("", h.s3Handler.HeadObject).Methods("HEAD")
	objectRouter.HandleFunc("", h.s3Handler.GetObject).Methods("GET")
	objectRouter.HandleFunc("", h.s3Handler.PutObject).Methods("PUT")
	objectRouter.HandleFunc("", h.s3Handler.DeleteObject).Methods("DELETE")

	// Object versioning
	objectRouter.HandleFunc("", h.s3Handler.GetObjectVersions).Methods("GET").Queries("versions", "")
	objectRouter.HandleFunc("", h.s3Handler.DeleteObjectVersion).Methods("DELETE").Queries("versionId", "{versionId}")

	// Object Lock operations
	objectRouter.HandleFunc("", h.s3Handler.GetObjectRetention).Methods("GET").Queries("retention", "")
	objectRouter.HandleFunc("", h.s3Handler.PutObjectRetention).Methods("PUT").Queries("retention", "")
	objectRouter.HandleFunc("", h.s3Handler.GetObjectLegalHold).Methods("GET").Queries("legal-hold", "")
	objectRouter.HandleFunc("", h.s3Handler.PutObjectLegalHold).Methods("PUT").Queries("legal-hold", "")

	// Object metadata operations
	objectRouter.HandleFunc("", h.s3Handler.GetObjectACL).Methods("GET").Queries("acl", "")
	objectRouter.HandleFunc("", h.s3Handler.PutObjectACL).Methods("PUT").Queries("acl", "")
	objectRouter.HandleFunc("", h.s3Handler.GetObjectTagging).Methods("GET").Queries("tagging", "")
	objectRouter.HandleFunc("", h.s3Handler.PutObjectTagging).Methods("PUT").Queries("tagging", "")
	objectRouter.HandleFunc("", h.s3Handler.DeleteObjectTagging).Methods("DELETE").Queries("tagging", "")

	// Multipart upload operations
	objectRouter.HandleFunc("", h.s3Handler.CreateMultipartUpload).Methods("POST").Queries("uploads", "")
	objectRouter.HandleFunc("", h.s3Handler.ListMultipartUploads).Methods("GET").Queries("uploads", "")
	objectRouter.HandleFunc("", h.s3Handler.UploadPart).Methods("PUT").Queries("partNumber", "{partNumber}", "uploadId", "{uploadId}")
	objectRouter.HandleFunc("", h.s3Handler.ListParts).Methods("GET").Queries("uploadId", "{uploadId}")
	objectRouter.HandleFunc("", h.s3Handler.CompleteMultipartUpload).Methods("POST").Queries("uploadId", "{uploadId}")
	objectRouter.HandleFunc("", h.s3Handler.AbortMultipartUpload).Methods("DELETE").Queries("uploadId", "{uploadId}")

	// Copy operations
	objectRouter.HandleFunc("", h.s3Handler.CopyObject).Methods("PUT").Headers("x-amz-copy-source", "{source}")

	// Presigned URL support (for compatibility)
	router.HandleFunc("/{bucket}/{object:.+}", h.s3Handler.PresignedOperation).Methods("GET", "PUT", "DELETE").Queries("X-Amz-Algorithm", "{algorithm}")
}

// Health check handlers
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "healthy", "service": "maxiofs"}`))
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check if all components are ready
	if !h.bucketManager.IsReady() || !h.objectManager.IsReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status": "not ready"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ready", "service": "maxiofs"}`))
}