package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/inventory"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/pkg/s3compat"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/sirupsen/logrus"
)

// diskUsage returns partition usage stats for the given path.
func diskUsage(path string) (*disk.UsageStat, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	return disk.Usage(path)
}

// Handler handles S3 API requests
type Handler struct {
	bucketManager    bucket.Manager
	objectManager    object.Manager
	authManager      auth.Manager
	metricsManager   metrics.Manager
	s3Handler        *s3compat.Handler
	publicAPIURL     string
	publicConsoleURL string
	consoleListen    string // e.g. ":8081" — used to redirect direct-access browsers to the console port
	dataDir          string
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
	consoleListen string,
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
		consoleListen:    consoleListen,
		dataDir:          dataDir,
	}
}

// RegisterRoutes registers all S3 API routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	// Health check endpoint
	router.HandleFunc("/health", h.handleHealth).Methods("GET")
	router.HandleFunc("/ready", h.handleReady).Methods("GET")

	// S3 API endpoints (BucketCORSMiddleware + S3ClientMiddleware are applied in
	// server.setupRoutes BEFORE auth so browsers are redirected to the console before
	// JWT/Bearer checks reject the request with 401.)

	// Service operations - root handler
	// HEAD / is required: Veeam makes HEAD to the root before GET to detect whether
	// the endpoint is a valid S3 service. A 404 on HEAD / causes Veeam to treat the
	// storage as generic S3-compatible and activate multi-bucket mode.
	router.HandleFunc("/", h.handleRoot).Methods("GET", "HEAD")

	// Bucket operations (support both with and without trailing slash)
	bucketRouter := router.PathPrefix("/{bucket}").Subrouter()

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
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucketNotification).Methods("DELETE").Queries("notification", "")

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

		// Public access block
		bucketRouter.HandleFunc(path, h.s3Handler.GetPublicAccessBlock).Methods("GET").Queries("publicAccessBlock", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutPublicAccessBlock).Methods("PUT").Queries("publicAccessBlock", "")
		bucketRouter.HandleFunc(path, h.s3Handler.DeletePublicAccessBlock).Methods("DELETE").Queries("publicAccessBlock", "")

		// Ownership controls
		bucketRouter.HandleFunc(path, h.s3Handler.GetOwnershipControls).Methods("GET").Queries("ownershipControls", "")
		bucketRouter.HandleFunc(path, h.s3Handler.PutOwnershipControls).Methods("PUT").Queries("ownershipControls", "")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteOwnershipControls).Methods("DELETE").Queries("ownershipControls", "")

		// Inventory configuration (id-specific routes BEFORE list route)
		bucketRouter.HandleFunc(path, h.s3Handler.GetBucketInventoryConfiguration).Methods("GET").Queries("inventory", "", "id", "{id}")
		bucketRouter.HandleFunc(path, h.s3Handler.PutBucketInventoryConfiguration).Methods("PUT").Queries("inventory", "", "id", "{id}")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucketInventoryConfiguration).Methods("DELETE").Queries("inventory", "", "id", "{id}")
		bucketRouter.HandleFunc(path, h.s3Handler.ListBucketInventoryConfigurations).Methods("GET").Queries("inventory", "")

		// Multipart uploads list — must be before ListObjects (no query constraint) so
		// GET /{bucket}?uploads is not captured by the generic ListObjects route.
		bucketRouter.HandleFunc(path, h.s3Handler.ListMultipartUploads).Methods("GET").Queries("uploads", "")

		// Generic bucket operations (without query parameters - registered last)
		bucketRouter.HandleFunc(path, h.s3Handler.HeadBucket).Methods("HEAD")
		bucketRouter.HandleFunc(path, h.s3Handler.CreateBucket).Methods("PUT")
		bucketRouter.HandleFunc(path, h.s3Handler.DeleteBucket).Methods("DELETE")
		bucketRouter.HandleFunc(path, h.s3Handler.ListObjects).Methods("GET")
	}

	// Batch operations
	bucketRouter.HandleFunc("", h.s3Handler.DeleteObjects).Methods("POST").Queries("delete", "")
	bucketRouter.HandleFunc("/", h.s3Handler.DeleteObjects).Methods("POST").Queries("delete", "")

	// POST presigned form upload (must be after query-param routes so ?delete is matched first)
	bucketRouter.HandleFunc("", h.s3Handler.HandlePresignedPost).Methods("POST")
	bucketRouter.HandleFunc("/", h.s3Handler.HandlePresignedPost).Methods("POST")

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
	objectRouter.HandleFunc("", h.s3Handler.DeleteObjectVersion).Methods("DELETE").Queries("versionId", "{versionId}")

	// Restore object
	objectRouter.HandleFunc("", h.s3Handler.RestoreObject).Methods("POST").Queries("restore", "")

	// S3 Select
	objectRouter.HandleFunc("", h.s3Handler.SelectObjectContent).Methods("POST").Queries("select", "")

	// GetObjectTorrent — BitTorrent manifests. Not implemented; returns NotImplemented.
	objectRouter.HandleFunc("", h.s3Handler.GetObjectTorrent).Methods("GET").Queries("torrent", "")

	// Object Lock operations (with query parameters)
	objectRouter.HandleFunc("", h.s3Handler.GetObjectRetention).Methods("GET").Queries("retention", "")
	objectRouter.HandleFunc("", h.s3Handler.PutObjectRetention).Methods("PUT").Queries("retention", "")
	objectRouter.HandleFunc("", h.s3Handler.GetObjectLegalHold).Methods("GET").Queries("legal-hold", "")
	objectRouter.HandleFunc("", h.s3Handler.PutObjectLegalHold).Methods("PUT").Queries("legal-hold", "")

	// Object attributes (must be before generic GET)
	objectRouter.HandleFunc("", h.s3Handler.GetObjectAttributes).Methods("GET").Queries("attributes", "")

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

	// OPTIONS for CORS preflight — BucketCORSMiddleware handles the response;
	// these routes exist so gorilla/mux does not return 405 before middleware runs.
	noop := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	for _, path := range []string{"", "/"} {
		bucketRouter.HandleFunc(path, noop).Methods("OPTIONS")
	}
	objectRouter.HandleFunc("", noop).Methods("OPTIONS")
}

// S3ClientMiddleware redirects non-S3 client requests to the web console.
// Applied in server.setupRoutes before S3 auth so Authorization: Bearer (console JWT)
// on the same host does not yield 401 before the redirect runs.
func (h *Handler) S3ClientMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/health" || p == "/ready" {
			next.ServeHTTP(w, r)
			return
		}
		if !h.isS3Client(r) {
			loc := h.effectiveConsoleRedirectURL(r)
			if strings.TrimSpace(loc) == "" {
				logrus.Error("public_console_url is empty and console redirect URL could not be derived")
				http.Error(w, "console URL not configured", http.StatusServiceUnavailable)
				return
			}
			http.Redirect(w, r, loc, http.StatusTemporaryRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// effectiveConsoleRedirectURL returns where browser requests on the S3 API port
// should be redirected.
//
//   - Behind a reverse proxy (X-Forwarded-For / X-Forwarded-Host / X-Real-IP present):
//     use public_console_url, which includes whatever subpath the operator configured.
//   - Direct access by IP/hostname (no proxy headers): redirect to the same host but
//     on the console listen port — the same way MinIO redirects to its console.
func (h *Handler) effectiveConsoleRedirectURL(r *http.Request) string {
	behindProxy := r.Header.Get("X-Forwarded-For") != "" ||
		r.Header.Get("X-Forwarded-Host") != "" ||
		r.Header.Get("X-Real-IP") != ""

	if behindProxy {
		if raw := strings.TrimSpace(h.publicConsoleURL); raw != "" {
			return raw
		}
	}

	return h.fallbackConsoleURLFromRequest(r)
}

// fallbackConsoleURLFromRequest builds a redirect URL for direct (non-proxy) access:
// same host as the incoming request, but on the console listen port.
func (h *Handler) fallbackConsoleURLFromRequest(r *http.Request) string {
	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}

	// Extract just the hostname (strip any port from the Host header).
	hostname := r.Host
	if host, _, err := net.SplitHostPort(r.Host); err == nil {
		hostname = host
	}

	// Derive the console port from consoleListen (e.g. ":8081" → "8081").
	consolePort := "8081"
	if h.consoleListen != "" {
		if _, p, err := net.SplitHostPort(h.consoleListen); err == nil && p != "" {
			consolePort = p
		}
	}

	return proto + "://" + hostname + ":" + consolePort + "/"
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

// BucketCORSMiddleware applies per-bucket CORS rules to S3 requests.
// It must be registered before S3ClientMiddleware so that browser preflight
// OPTIONS requests (which have no auth headers) can be answered without being
// redirected to the web console.
func (h *Handler) BucketCORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Extract bucket name from path: /{bucket} or /{bucket}/{object...}
		trimmed := strings.TrimPrefix(r.URL.Path, "/")
		bucketName := strings.SplitN(trimmed, "/", 2)[0]
		if bucketName == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Determine tenant — auth middleware may have already set the user in context.
		tenantID := ""
		if user, ok := auth.GetUserFromContext(r.Context()); ok {
			tenantID = user.TenantID
		}

		corsConfig, err := h.bucketManager.GetCORS(r.Context(), tenantID, bucketName)
		if err != nil || corsConfig == nil || len(corsConfig.CORSRules) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		// The method to check for rule matching.  For OPTIONS preflight use
		// Access-Control-Request-Method; otherwise use the actual method.
		requestMethod := r.Header.Get("Access-Control-Request-Method")
		if requestMethod == "" {
			requestMethod = r.Method
		}

		var matched *bucket.CORSRule
		for i := range corsConfig.CORSRules {
			rule := &corsConfig.CORSRules[i]
			if corsOriginMatches(origin, rule.AllowedOrigins) && corsMethodAllowed(requestMethod, rule.AllowedMethods) {
				matched = rule
				break
			}
		}

		if matched == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Apply matched rule headers.
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(matched.AllowedMethods, ", "))
		if len(matched.AllowedHeaders) > 0 {
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(matched.AllowedHeaders, ", "))
		}
		if len(matched.ExposeHeaders) > 0 {
			w.Header().Set("Access-Control-Expose-Headers", strings.Join(matched.ExposeHeaders, ", "))
		}
		if matched.MaxAgeSeconds != nil {
			w.Header().Set("Access-Control-Max-Age", strconv.Itoa(*matched.MaxAgeSeconds))
		}

		// Respond to preflight immediately — don't pass to S3ClientMiddleware.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// corsOriginMatches reports whether origin matches any entry in the allowed list.
// Entries may be exact origins or wildcard patterns like "*.example.com".
func corsOriginMatches(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
		if strings.HasPrefix(a, "*.") {
			domain := strings.TrimPrefix(a, "*.")
			if strings.HasSuffix(origin, "."+domain) {
				return true
			}
		}
	}
	return false
}

// corsMethodAllowed reports whether method is in the allowed list.
func corsMethodAllowed(method string, allowed []string) bool {
	method = strings.ToUpper(method)
	for _, a := range allowed {
		if strings.ToUpper(a) == method {
			return true
		}
	}
	return false
}

// Health check handlers
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	var capacityTotal, capacityUsed uint64
	if usage, err := diskUsage(h.dataDir); err == nil {
		capacityTotal = usage.Total
		capacityUsed = usage.Used
	}

	w.Write([]byte(fmt.Sprintf(
		`{"status":"healthy","service":"maxiofs","capacity_total":%d,"capacity_used":%d}`,
		capacityTotal, capacityUsed,
	)))
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

// SetInventoryManager wires the inventory manager into the S3-compatible handler.
func (h *Handler) SetInventoryManager(m *inventory.Manager) {
	h.s3Handler.SetInventoryManager(m)
}

// SetReplicationManager sets the replication manager for realtime object replication hooks
func (h *Handler) SetReplicationManager(rm interface {
	QueueRealtimeObject(ctx context.Context, tenantID, bucket, objectKey, action string) error
}) {
	h.s3Handler.SetReplicationManager(rm)
}

// SetClusterRouter sets the cluster router for routing S3 bucket operations to the correct node.
func (h *Handler) SetClusterRouter(cr *cluster.Router) {
	h.s3Handler.SetClusterRouter(cr)
}

// handleRoot handles GET / and HEAD /. Non-S3 clients are redirected by S3ClientMiddleware.
// Both GET and HEAD run ListBuckets so that HEAD / returns the same headers (including
// Content-Length) as GET / but without the body. Veeam uses HEAD / to detect a valid S3
// service endpoint and checks Content-Length to confirm the endpoint is functional.
// net/http automatically suppresses the body for HEAD requests.
func (h *Handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	h.s3Handler.ListBuckets(w, r)
}
