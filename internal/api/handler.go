package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/pkg/s3compat"
)

// Handler handles S3 API requests
type Handler struct {
	bucketManager    bucket.Manager
	objectManager    object.Manager
	authManager      auth.Manager
	metricsManager   metrics.Manager
	s3Handler        *s3compat.Handler
	publicAPIURL     string
	publicConsoleURL string
}

// NewHandler creates a new API handler
func NewHandler(
	bucketManager bucket.Manager,
	objectManager object.Manager,
	authManager auth.Manager,
	metricsManager metrics.Manager,
	shareManager interface {
		GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (interface{}, error)
	},
	publicAPIURL string,
	publicConsoleURL string,
	dataDir string,
) *Handler {
	s3Handler := s3compat.NewHandler(bucketManager, objectManager)

	// Configure auth manager for permission checking
	s3Handler.SetAuthManager(authManager)

	// Configure share manager for presigned URL validation
	if shareManager != nil {
		s3Handler.SetShareManager(shareManager)
	}

	// Configure public URLs for presigned URL generation
	s3Handler.SetPublicAPIURL(publicAPIURL)

	// Configure dataDir for SOSAPI capacity calculations
	s3Handler.SetDataDir(dataDir)

	return &Handler{
		bucketManager:    bucketManager,
		objectManager:    objectManager,
		authManager:      authManager,
		metricsManager:   metricsManager,
		s3Handler:        s3Handler,
		publicAPIURL:     publicAPIURL,
		publicConsoleURL: publicConsoleURL,
	}
}

// RegisterRoutes registers all S3 API routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	// Health check endpoint
	router.HandleFunc("/health", h.handleHealth).Methods("GET")
	router.HandleFunc("/ready", h.handleReady).Methods("GET")

	// S3 API endpoints
	s3Router := router.PathPrefix("/").Subrouter()

	// Service operations - root handler with browser detection
	s3Router.HandleFunc("/", h.handleRoot).Methods("GET")

	// Bucket operations (support both with and without trailing slash)
	bucketRouter := s3Router.PathPrefix("/{bucket}").Subrouter()

	// Bucket management - register both "" and "/" to handle trailing slash
	for _, path := range []string{"", "/"} {
		bucketRouter.HandleFunc(path, h.s3Handler.HeadBucket).Methods("HEAD")
		bucketRouter.HandleFunc(path, h.s3Handler.CreateBucket).Methods("PUT")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucket).Methods("DELETE")
		bucketRouter.HandleFunc(path, h.s3Handler.ListObjects).Methods("GET")

		// Bucket configuration endpoints
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketLocation).Methods("GET").Queries("location", "")
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketVersioning).Methods("GET").Queries("versioning", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketVersioning).Methods("PUT").Queries("versioning", "")
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketPolicy).Methods("GET").Queries("policy", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketPolicy).Methods("PUT").Queries("policy", "")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucketPolicy).Methods("DELETE").Queries("policy", "")

		// Object Lock configuration
		bucketRouter.HandleFunc(path, h.s3Handler.GetObjectLockConfiguration).Methods("GET").Queries("object-lock", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutObjectLockConfiguration).Methods("PUT").Queries("object-lock", "")

		// Lifecycle configuration
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketLifecycle).Methods("GET").Queries("lifecycle", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketLifecycle).Methods("PUT").Queries("lifecycle", "")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucketLifecycle).Methods("DELETE").Queries("lifecycle", "")

		// CORS configuration
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketCORS).Methods("GET").Queries("cors", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketCORS).Methods("PUT").Queries("cors", "")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucketCORS).Methods("DELETE").Queries("cors", "")
	}

	// Batch operations
	bucketRouter.HandleFunc("", h.s3Handler.DeleteObjects).Methods("POST").Queries("delete", "")
	bucketRouter.HandleFunc("/", h.s3Handler.DeleteObjects).Methods("POST").Queries("delete", "")

	// Multipart uploads
	bucketRouter.HandleFunc("", h.s3Handler.ListMultipartUploads).Methods("GET").Queries("uploads", "")
	bucketRouter.HandleFunc("/", h.s3Handler.ListMultipartUploads).Methods("GET").Queries("uploads", "")

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

// handleRoot handles requests to the root path with browser detection
func (h *Handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	userAgent := strings.ToLower(r.Header.Get("User-Agent"))

	// Detect if request is from a web browser
	isBrowser := strings.Contains(userAgent, "mozilla") ||
		strings.Contains(userAgent, "chrome") ||
		strings.Contains(userAgent, "safari") ||
		strings.Contains(userAgent, "firefox") ||
		strings.Contains(userAgent, "edge")

	// If it's a browser, redirect to the web console
	if isBrowser {
		http.Redirect(w, r, h.publicConsoleURL, http.StatusTemporaryRedirect)
		return
	}

	// Otherwise, handle as S3 API (ListBuckets)
	h.s3Handler.ListBuckets(w, r)
}
