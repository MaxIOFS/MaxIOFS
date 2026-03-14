package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/metadata"
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
	metadataStore metadata.Store,
	shareManager interface {
		GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (interface{}, error)
	},
	publicAPIURL string,
	publicConsoleURL string,
	dataDir string,
	clusterManager *cluster.Manager,
	bucketAggregator *cluster.BucketAggregator,
) *Handler {
	s3Handler := s3compat.NewHandler(bucketManager, objectManager)

	// Configure auth manager for permission checking
	s3Handler.SetAuthManager(authManager)

	// Configure metadata store for versioning support
	if metadataStore != nil {
		s3Handler.SetMetadataStore(metadataStore)
	}

	// Configure share manager for presigned URL validation
	if shareManager != nil {
		s3Handler.SetShareManager(shareManager)
	}

	// Configure public URLs for presigned URL generation
	s3Handler.SetPublicAPIURL(publicAPIURL)

	// Configure dataDir for SOSAPI capacity calculations
	s3Handler.SetDataDir(dataDir)

	// Configure cluster manager for cluster mode detection
	if clusterManager != nil {
		s3Handler.SetClusterManager(clusterManager)
	}

	// Configure bucket aggregator for cross-node bucket listing
	if bucketAggregator != nil {
		s3Handler.SetBucketAggregator(bucketAggregator)
	}

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
	s3Router.Use(h.s3ClientMiddleware)

	// Service operations - root handler
	s3Router.HandleFunc("/", h.handleRoot).Methods("GET")

	// Bucket operations (support both with and without trailing slash)
	bucketRouter := s3Router.PathPrefix("/{bucket}").Subrouter()

	// Bucket management - register both "" and "/" to handle trailing slash
	// IMPORTANT: Register routes with query parameters FIRST, before generic routes
	// Gorilla Mux matches routes in order, first match wins
	for _, path := range []string{"", "/"} {
		// Bucket configuration endpoints (with query parameters - must be registered first)
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

		// Bucket Tagging
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketTagging).Methods("GET").Queries("tagging", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketTagging).Methods("PUT").Queries("tagging", "")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucketTagging).Methods("DELETE").Queries("tagging", "")

		// Bucket ACL
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketACL).Methods("GET").Queries("acl", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketACL).Methods("PUT").Queries("acl", "")

		// Versioning - list all object versions in bucket
		bucketRouter.HandleFunc(path, h.s3Handler.ListBucketVersions).Methods("GET").Queries("versions", "")

		// ListObjectsV2 (list-type=2) - must be registered BEFORE generic ListObjects
		bucketRouter.HandleFunc(path, h.s3Handler.ListObjectsV2).Methods("GET").Queries("list-type", "2")

		// Bucket notification
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketNotification).Methods("GET").Queries("notification", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketNotification).Methods("PUT").Queries("notification", "")

		// Bucket website
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketWebsite).Methods("GET").Queries("website", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketWebsite).Methods("PUT").Queries("website", "")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucketWebsite).Methods("DELETE").Queries("website", "")

		// Transfer acceleration
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketAccelerateConfiguration).Methods("GET").Queries("accelerate", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketAccelerateConfiguration).Methods("PUT").Queries("accelerate", "")

		// Request payment
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketRequestPayment).Methods("GET").Queries("requestPayment", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketRequestPayment).Methods("PUT").Queries("requestPayment", "")

		// Bucket encryption
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketEncryption).Methods("GET").Queries("encryption", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketEncryption).Methods("PUT").Queries("encryption", "")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucketEncryption).Methods("DELETE").Queries("encryption", "")

		// Bucket replication
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketReplication).Methods("GET").Queries("replication", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketReplication).Methods("PUT").Queries("replication", "")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucketReplication).Methods("DELETE").Queries("replication", "")

		// Bucket logging
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketLogging).Methods("GET").Queries("logging", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketLogging).Methods("PUT").Queries("logging", "")

		// Generic bucket operations (without query parameters - registered last)
		bucketRouter.HandleFunc(path, h.s3Handler.HeadBucket).Methods("HEAD")
		bucketRouter.HandleFunc(path, h.s3Handler.CreateBucket).Methods("PUT")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucket).Methods("DELETE")
		bucketRouter.HandleFunc(path, h.s3Handler.ListObjects).Methods("GET")
	}

	// Batch operations
	bucketRouter.HandleFunc("", h.s3Handler.DeleteObjects).Methods("POST").Queries("delete", "")
	bucketRouter.HandleFunc("/", h.s3Handler.DeleteObjects).Methods("POST").Queries("delete", "")

	// Multipart uploads
	bucketRouter.HandleFunc("", h.s3Handler.ListMultipartUploads).Methods("GET").Queries("uploads", "")
	bucketRouter.HandleFunc("/", h.s3Handler.ListMultipartUploads).Methods("GET").Queries("uploads", "")

	// Object operations
	objectRouter := bucketRouter.PathPrefix("/{object:.+}").Subrouter()

	// IMPORTANT: Register routes with query parameters FIRST (Gorilla Mux matches in order)

	// Multipart upload operations (with query parameters - must be first)
	objectRouter.HandleFunc("", h.s3Handler.CreateMultipartUpload).Methods("POST").Queries("uploads", "")
	objectRouter.HandleFunc("", h.s3Handler.ListMultipartUploads).Methods("GET").Queries("uploads", "")
	objectRouter.HandleFunc("", h.s3Handler.UploadPart).Methods("PUT").Queries("partNumber", "{partNumber}", "uploadId", "{uploadId}")
	objectRouter.HandleFunc("", h.s3Handler.ListParts).Methods("GET").Queries("uploadId", "{uploadId}")
	objectRouter.HandleFunc("", h.s3Handler.CompleteMultipartUpload).Methods("POST").Queries("uploadId", "{uploadId}")
	objectRouter.HandleFunc("", h.s3Handler.AbortMultipartUpload).Methods("DELETE").Queries("uploadId", "{uploadId}")

	// Object versioning (with query parameters)
	objectRouter.HandleFunc("", h.s3Handler.GetObjectVersions).Methods("GET").Queries("versions", "")
	objectRouter.HandleFunc("", h.s3Handler.DeleteObjectVersion).Methods("DELETE").Queries("versionId", "{versionId}")

	// Object Lock operations (with query parameters)
	objectRouter.HandleFunc("", h.s3Handler.GetObjectRetention).Methods("GET").Queries("retention", "")
	objectRouter.HandleFunc("", h.s3Handler.PutObjectRetention).Methods("PUT").Queries("retention", "")
	objectRouter.HandleFunc("", h.s3Handler.GetObjectLegalHold).Methods("GET").Queries("legal-hold", "")
	objectRouter.HandleFunc("", h.s3Handler.PutObjectLegalHold).Methods("PUT").Queries("legal-hold", "")

	// Object metadata operations (with query parameters)
	objectRouter.HandleFunc("", h.s3Handler.GetObjectACL).Methods("GET").Queries("acl", "")
	objectRouter.HandleFunc("", h.s3Handler.PutObjectACL).Methods("PUT").Queries("acl", "")
	objectRouter.HandleFunc("", h.s3Handler.GetObjectTagging).Methods("GET").Queries("tagging", "")
	objectRouter.HandleFunc("", h.s3Handler.PutObjectTagging).Methods("PUT").Queries("tagging", "")
	objectRouter.HandleFunc("", h.s3Handler.DeleteObjectTagging).Methods("DELETE").Queries("tagging", "")

	// Copy operations (with header filter - must be before PutObject)
	objectRouter.HandleFunc("", h.s3Handler.CopyObject).Methods("PUT").Headers("x-amz-copy-source", "{source}")

	// Presigned URL operations (must be before basic object operations)
	// V4 presigned URLs contain X-Amz-Algorithm query parameter
	objectRouter.HandleFunc("", h.s3Handler.HandlePresignedRequest).Methods("GET", "PUT", "DELETE", "HEAD").Queries("X-Amz-Algorithm", "{algorithm}")
	// V2 presigned URLs contain AWSAccessKeyId query parameter
	objectRouter.HandleFunc("", h.s3Handler.HandlePresignedRequest).Methods("GET", "PUT", "DELETE", "HEAD").Queries("AWSAccessKeyId", "{keyid}")

	// Basic object operations (without query parameters - registered LAST)
	objectRouter.HandleFunc("", h.s3Handler.HeadObject).Methods("HEAD")
	objectRouter.HandleFunc("", h.s3Handler.GetObject).Methods("GET")
	objectRouter.HandleFunc("", h.s3Handler.PutObject).Methods("PUT")
	objectRouter.HandleFunc("", h.s3Handler.DeleteObject).Methods("DELETE")
}

// s3ClientMiddleware redirects non-S3 client requests to the web console.
func (h *Handler) s3ClientMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.isS3Client(r) {
			http.Redirect(w, r, h.publicConsoleURL, http.StatusTemporaryRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isS3Client detects whether the request is from an S3 client (CLI, SDK, GUI) vs browser/curl.
func (h *Handler) isS3Client(r *http.Request) bool {
	q := r.URL.Query()
	if q.Has("X-Amz-Algorithm") || q.Has("AWSAccessKeyId") {
		return true
	}

	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "AWS4-HMAC-SHA256") ||
		strings.HasPrefix(auth, "AWS ") ||
		strings.HasPrefix(auth, "AWS4 ") {
		return true
	}

	for key := range r.Header {
		if strings.HasPrefix(strings.ToLower(key), "x-amz-") {
			return true
		}
	}

	// Allow GET/HEAD to /{bucket}/{object} so shared/public objects work in browsers.
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && isObjectPath(r.URL.Path) {
		return true
	}

	ua := strings.ToLower(r.Header.Get("User-Agent"))
	clients := []string{
		"aws-cli", "aws-sdk", "boto", "boto3", "s3cmd", "s5cmd", "mc", "rclone",
		"cyberduck", "mountainduck", "s3 browser", "s3browser",
		"cloudberry", "msp360", "winscp", "transmit", "dragondisk", "dragon disk",
		"aws-sdk-java", "aws-sdk-go", "aws-sdk-php", "aws-sdk-ruby", "aws-sdk-net", "awssdk",
		"minio", "aws", "s3",
	}
	for _, c := range clients {
		if strings.Contains(ua, c) {
			return true
		}
	}

	return false
}

// isObjectPath returns true if path is /{bucket}/{object...} (at least two segments).
func isObjectPath(path string) bool {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return false
	}
	parts := strings.Split(trimmed, "/")
	return len(parts) >= 2
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

// handleRoot handles GET / (ListBuckets). Non-S3 clients are redirected by s3ClientMiddleware.
func (h *Handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	h.s3Handler.ListBuckets(w, r)
}
