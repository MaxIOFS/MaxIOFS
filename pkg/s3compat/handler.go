package s3compat

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bandwidth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/inventory"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/presigned"
	"github.com/maxiofs/maxiofs/internal/share"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

// getObjectKey extracts object key from mux vars (already decoded by Gorilla Mux)
func getObjectKey(r *http.Request) string {
	vars := mux.Vars(r)
	return vars["object"]
}

// s3URLEncode performs percent-encoding compatible with the S3 encoding-type=url
func s3URLEncode(s string) string {
	const unreservedAndSlash = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~/"
	var buf strings.Builder
	buf.Grow(len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		if strings.IndexByte(unreservedAndSlash, b) >= 0 {
			buf.WriteByte(b)
		} else {
			fmt.Fprintf(&buf, "%%%02X", b)
		}
	}
	return buf.String()
}

// generateRequestID generates a SHORT request ID (like MaxIOFS does)
// MaxIOFS uses 16 character hex strings, not 32
func generateRequestID() string {
	b := make([]byte, 8) // 8 bytes = 16 hex chars
	rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

// generateAmzId2 generates a host ID for x-amz-id-2 (base64-encoded like AWS S3 and MinIO)
func generateAmzId2() string {
	b := make([]byte, 48) // 48 bytes → 64-char base64
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
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
	inventoryManager interface {
		GetConfigByID(ctx context.Context, id, tenantID string) (*inventory.InventoryConfig, error)
		ListConfigsByBucket(ctx context.Context, bucketName, tenantID string) ([]*inventory.InventoryConfig, error)
		UpsertConfigByID(ctx context.Context, config *inventory.InventoryConfig) error
		DeleteConfigByID(ctx context.Context, id, tenantID string) error
	}
	metadataStore interface {
		ListAllObjectVersions(ctx context.Context, bucket, prefix string, maxKeys int) ([]*metadata.ObjectVersion, error)
		GetBucketByName(ctx context.Context, name string) (*metadata.BucketMetadata, error)
		GetMultipartUpload(ctx context.Context, uploadID string) (*metadata.MultipartUploadMetadata, error)
	}
	clusterManager interface {
		IsClusterEnabled() bool
		SelectReadNode(ctx context.Context, bucket string) (*cluster.Node, error)
		SelectReadNodes(ctx context.Context, bucket string) ([]*cluster.Node, error)
		ProxyRead(ctx context.Context, w http.ResponseWriter, r *http.Request, node *cluster.Node) error
		TryProxyRead(ctx context.Context, w http.ResponseWriter, r *http.Request, node *cluster.Node) (bool, error)
		GetLocalNodeID(ctx context.Context) (string, error)
		GetLocalNodeToken(ctx context.Context) (string, error)
		GetTLSConfig() *tls.Config
	}
	bucketAggregator interface {
		ListAllBuckets(ctx context.Context, tenantID string) ([]cluster.BucketWithLocation, error)
		ListAllBucketsFromAllNodes(ctx context.Context, tenantID string) ([]cluster.BucketWithLocation, error)
	}
	clusterRouter interface {
		RouteRequest(ctx context.Context, bucket string) (*cluster.Node, bool, error)
	}
	replicationManager interface {
		QueueRealtimeObject(ctx context.Context, tenantID, bucket, objectKey, action string) error
	}
	publicAPIURL string
	iamSTSEndpoint   func() string
	dataDir          string             // For calculating disk capacity in SOSAPI
	notifHTTPClient  *http.Client       // HTTP client for notification webhooks; defaults to SSRF-blocking client
	bandwidthManager *bandwidth.Manager // Per-tenant aggregate transfer throttling; nil = disabled
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

// SetBandwidthManager sets the per-tenant bandwidth throttling manager.
func (h *Handler) SetBandwidthManager(m *bandwidth.Manager) {
	h.bandwidthManager = m
}

// tenantBandwidthLimiter returns the shared bandwidth limiter for the tenant that
// owns bucketName, or nil when there is no tenant, no configured cap, or no
// manager. Used to throttle object up/downloads to the tenant's aggregate budget.
func (h *Handler) tenantBandwidthLimiter(ctx context.Context, r *http.Request, bucketName string) *rate.Limiter {
	if h.bandwidthManager == nil || h.authManager == nil {
		return nil
	}
	tenantID := h.resolveBucketTenantID(r, bucketName)
	if tenantID == "" {
		return nil
	}
	tenant, err := h.authManager.GetTenant(ctx, tenantID)
	if err != nil || tenant == nil || tenant.MaxBandwidthBytesPerSec <= 0 {
		return nil
	}
	return h.bandwidthManager.Limiter(tenantID, tenant.MaxBandwidthBytesPerSec)
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

// SetIAMSTSEndpointResolver wires the resolver SOSAPI uses to decide whether to
// advertise IAM and STS to Veeam, and at which URL.
func (h *Handler) SetIAMSTSEndpointResolver(resolve func() string) {
	h.iamSTSEndpoint = resolve
}

// SetDataDir sets the data directory for disk capacity calculations
func (h *Handler) SetDataDir(dataDir string) {
	h.dataDir = dataDir
}

// SetInventoryManager sets the inventory manager for S3 BucketInventory operations
func (h *Handler) SetInventoryManager(im interface {
	GetConfigByID(ctx context.Context, id, tenantID string) (*inventory.InventoryConfig, error)
	ListConfigsByBucket(ctx context.Context, bucketName, tenantID string) ([]*inventory.InventoryConfig, error)
	UpsertConfigByID(ctx context.Context, config *inventory.InventoryConfig) error
	DeleteConfigByID(ctx context.Context, id, tenantID string) error
}) {
	h.inventoryManager = im
}

// buildLocationURL constructs the absolute <Location> URL for CompleteMultipartUpload
func (h *Handler) buildLocationURL(r *http.Request, bucketName, objectKey string) string {
	// Determine scheme: use https unless we know the server is plain http.
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(h.publicAPIURL, "https") {
		scheme = "http"
	}

	host := r.Host
	if host != "" {
		// Strip port for the prefix comparison only.
		hostname := host
		if h2, _, err := net.SplitHostPort(host); err == nil {
			hostname = h2
		}
		// Virtual-hosted-style: the Host header is "{bucket}.{anything}".
		if strings.HasPrefix(hostname, bucketName+".") {
			return fmt.Sprintf("%s://%s/%s", scheme, host, objectKey)
		}
		// Path-style: preserve the host as-is (including any port).
		return fmt.Sprintf("%s://%s/%s/%s", scheme, host, bucketName, objectKey)
	}

	// Fallback: use the configured publicAPIURL.
	if h.publicAPIURL != "" {
		base := strings.TrimRight(h.publicAPIURL, "/")
		return fmt.Sprintf("%s/%s/%s", base, bucketName, objectKey)
	}

	// Last resort: relative path (should never be reached in production).
	return "/" + bucketName + "/" + objectKey
}

// SetClusterManager sets the cluster manager for checking cluster status and read routing.
func (h *Handler) SetClusterManager(cm interface {
	IsClusterEnabled() bool
	SelectReadNode(ctx context.Context, bucket string) (*cluster.Node, error)
	SelectReadNodes(ctx context.Context, bucket string) ([]*cluster.Node, error)
	ProxyRead(ctx context.Context, w http.ResponseWriter, r *http.Request, node *cluster.Node) error
	TryProxyRead(ctx context.Context, w http.ResponseWriter, r *http.Request, node *cluster.Node) (bool, error)
	GetLocalNodeID(ctx context.Context) (string, error)
	GetLocalNodeToken(ctx context.Context) (string, error)
	GetTLSConfig() *tls.Config
}) {
	h.clusterManager = cm
}

// SetBucketAggregator sets the bucket aggregator for cross-node bucket listing
func (h *Handler) SetBucketAggregator(ba interface {
	ListAllBuckets(ctx context.Context, tenantID string) ([]cluster.BucketWithLocation, error)
	ListAllBucketsFromAllNodes(ctx context.Context, tenantID string) ([]cluster.BucketWithLocation, error)
}) {
	h.bucketAggregator = ba
}

// SetClusterRouter sets the cluster router for routing bucket operations to the correct node.
func (h *Handler) SetClusterRouter(cr interface {
	RouteRequest(ctx context.Context, bucket string) (*cluster.Node, bool, error)
}) {
	h.clusterRouter = cr
}

// SetReplicationManager sets the replication manager for realtime object replication
func (h *Handler) SetReplicationManager(rm interface {
	QueueRealtimeObject(ctx context.Context, tenantID, bucket, objectKey, action string) error
}) {
	h.replicationManager = rm
}

// proxyBucketRequest checks if the given bucket should be routed to a remote cluster node
// and, if so, proxies the request there, writing the response to w and returning true.
// Returns false when the request should be handled locally.
func (h *Handler) proxyBucketRequest(w http.ResponseWriter, r *http.Request, bucketName string) bool {
	if h.clusterRouter == nil || h.clusterManager == nil {
		return false
	}
	// Prevent infinite proxy loops
	if r.Header.Get("X-MaxIOFS-Proxied") == "true" {
		return false
	}

	node, isLocal, err := h.clusterRouter.RouteRequest(r.Context(), bucketName)
	if err != nil || isLocal || node == nil {
		// Error, local bucket, or no node found — handle locally
		return false
	}

	// Get local node credentials for HMAC signing
	nodeID, err := h.clusterManager.GetLocalNodeID(r.Context())
	if err != nil {
		logrus.WithError(err).Warn("proxyBucketRequest: failed to get local node ID, handling locally")
		return false
	}
	clusterToken, err := h.clusterManager.GetLocalNodeToken(r.Context())
	if err != nil {
		logrus.WithError(err).Warn("proxyBucketRequest: failed to get cluster token, handling locally")
		return false
	}

	// Extract user context for forwarding
	user, _ := auth.GetUserFromContext(r.Context())
	var userID, tenantID, roles string
	if user != nil {
		userID = user.ID
		tenantID = user.TenantID
		roles = strings.Join(user.Roles, ",")
	}

	// Build the proxy client and forward the request to node.APIURL
	proxyClient := cluster.NewProxyClient(h.clusterManager.GetTLSConfig())
	resp, err := proxyClient.ProxyToNodeAPIURL(r.Context(), node, r, nodeID, clusterToken, userID, tenantID, roles)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"node":   node.Name,
			"error":  err.Error(),
		}).Error("proxyBucketRequest: failed to proxy to remote node")

		if r.Body != nil && r.ContentLength != 0 {
			h.writeError(w, "ServiceUnavailable",
				"The node that owns this bucket could not be reached", bucketName, r)
			return true
		}
		return false
	}
	defer resp.Body.Close()

	proxyClient.CopyResponseToWriter(w, resp) //nolint:errcheck
	return true
}

// SetMetadataStore sets the metadata store for accessing object versions
func (h *Handler) SetMetadataStore(ms interface {
	ListAllObjectVersions(ctx context.Context, bucket, prefix string, maxKeys int) ([]*metadata.ObjectVersion, error)
	GetBucketByName(ctx context.Context, name string) (*metadata.BucketMetadata, error)
	GetMultipartUpload(ctx context.Context, uploadID string) (*metadata.MultipartUploadMetadata, error)
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
	// BucketRegion: AWS includes this in ListBuckets; some clients (e.g. VEEAM) may use it
	// to detect region-aware backends and adjust behavior (e.g. multi-bucket option).
	BucketRegion string `xml:"BucketRegion,omitempty"`
}

// MarshalXML serializes CreationDate in UTC with Z suffix and optional BucketRegion, matching AWS S3 format.
func (b BucketInfo) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	type bucketInfoAlias struct {
		Name         string `xml:"Name"`
		CreationDate string `xml:"CreationDate"`
		BucketRegion string `xml:"BucketRegion,omitempty"`
	}
	out := bucketInfoAlias{
		Name:         b.Name,
		CreationDate: b.CreationDate.UTC().Format("2006-01-02T15:04:05.000Z"),
		BucketRegion: b.BucketRegion,
	}
	return e.EncodeElement(out, start)
}

type ListBucketResult struct {
	XMLName        xml.Name       `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListBucketResult"`
	Name           string         `xml:"Name"`
	Prefix         string         `xml:"Prefix"`
	Marker         string         `xml:"Marker"`
	MaxKeys        int            `xml:"MaxKeys"`
	Delimiter      string         `xml:"Delimiter,omitempty"`
	IsTruncated    bool           `xml:"IsTruncated"`
	NextMarker     string         `xml:"NextMarker,omitempty"`
	EncodingType   string         `xml:"EncodingType,omitempty"`
	Contents       []ObjectInfo   `xml:"Contents"`
	CommonPrefixes []CommonPrefix `xml:"CommonPrefixes"`
}

// ListBucketResultV2 is the response struct for ListObjectsV2 (list-type=2).
// The root element is still <ListBucketResult> per the AWS S3 spec.
type ListBucketResultV2 struct {
	XMLName               xml.Name       `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListBucketResult"`
	Name                  string         `xml:"Name"`
	Prefix                string         `xml:"Prefix"`
	Delimiter             string         `xml:"Delimiter,omitempty"`
	MaxKeys               int            `xml:"MaxKeys"`
	KeyCount              int            `xml:"KeyCount"`
	IsTruncated           bool           `xml:"IsTruncated"`
	ContinuationToken     string         `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string         `xml:"NextContinuationToken,omitempty"`
	StartAfter            string         `xml:"StartAfter,omitempty"`
	EncodingType          string         `xml:"EncodingType,omitempty"`
	Contents              []ObjectInfo   `xml:"Contents"`
	CommonPrefixes        []CommonPrefix `xml:"CommonPrefixes"`
}

type ObjectInfo struct {
	Key          string    `xml:"Key"`
	LastModified time.Time `xml:"LastModified"`
	ETag         string    `xml:"ETag"`
	Size         int64     `xml:"Size"`
	StorageClass string    `xml:"StorageClass"`
	Owner        *Owner    `xml:"Owner,omitempty"`
}

type CommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

// LocationConstraintResponse is the XML response for GetBucketLocation.
// AWS S3 returns <LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">region</LocationConstraint>.
type LocationConstraintResponse struct {
	XMLName  xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ LocationConstraint"`
	Location string   `xml:",chardata"`
}

type Error struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`

	// Resource-specific context fields (only one is set per error)
	Key        string `xml:"Key,omitempty"`        // object errors: NoSuchKey, etc.
	BucketName string `xml:"BucketName,omitempty"` // bucket errors: NoSuchBucket, etc.
	Resource   string `xml:"Resource,omitempty"`   // generic fallback

	// Auth error context — populated by AWS for credential/signature errors
	AWSAccessKeyId string `xml:"AWSAccessKeyId,omitempty"` // InvalidAccessKeyId, SignatureDoesNotMatch

	// Request-expiry context — populated by AWS for RequestExpired
	ExpiresDate string `xml:"ExpiresDate,omitempty"` // ISO-8601 expiration time
	ServerTime  string `xml:"ServerTime,omitempty"`  // ISO-8601 server time at rejection

	RequestId string `xml:"RequestId"`
	HostId    string `xml:"HostId"`
}

// Service operations
func (h *Handler) ListBuckets(w http.ResponseWriter, r *http.Request) {
	// Log every ListBuckets request at Info so the "first" probe (e.g. VEEAM wizard screen 2) is visible.
	// If this never appears when adding the repo, the request is not reaching the S3 API (check URL/port).
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
		"host":   r.Host,
		"remote": r.RemoteAddr,
		"ua":     r.Header.Get("User-Agent"),
	}).Info("S3 API: ListBuckets request")

	// Add S3-compatible headers FIRST
	addS3CompatHeaders(w)

	// Get tenant ID from authenticated user
	// Empty string for global admins (who can see all tenants)
	tenantID := h.getTenantIDFromRequest(r)

	// Filter buckets by tenant ownership and user permissions
	user, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		h.writeError(w, "AccessDenied", "Access Denied.", "", r)
		return
	}

	// Check if cluster mode is enabled
	isClusterEnabled := h.clusterManager != nil && h.clusterManager.IsClusterEnabled()

	var buckets []bucket.Bucket

	if isClusterEnabled && h.bucketAggregator != nil {
		// Cluster mode: aggregate buckets from all healthy nodes (shows remote buckets too)
		bucketsWithLocation, err := h.bucketAggregator.ListAllBucketsFromAllNodes(r.Context(), tenantID)
		if err != nil {
			h.writeError(w, "InternalError", "Failed to list buckets from cluster: "+err.Error(), "", r)
			return
		}

		// Convert BucketWithLocation to bucket.Bucket for filtering
		buckets = make([]bucket.Bucket, len(bucketsWithLocation))
		for i, bwl := range bucketsWithLocation {
			buckets[i] = bucket.Bucket{
				Name:        bwl.Name,
				TenantID:    bwl.TenantID,
				OwnerID:     bwl.OwnerID,
				OwnerType:   bwl.OwnerType,
				CreatedAt:   bwl.CreatedAt,
				ObjectCount: bwl.ObjectCount,
				TotalSize:   bwl.SizeBytes,
				Metadata:    bwl.Metadata,
				Tags:        bwl.Tags,
			}
		}
	} else {
		// Standalone mode: list buckets from local node only
		localBuckets, err := h.bucketManager.ListBuckets(r.Context(), tenantID)
		if err != nil {
			h.writeError(w, "InternalError", err.Error(), "", r)
			return
		}
		buckets = localBuckets
	}

	if !h.userCanPerformS3ActionAnywhere(r.Context(), user, auth.ActionListAllMyBuckets) {
		h.writeError(w, "AccessDenied", "Access Denied", "", r)
		return
	}

	var filteredBuckets []bucket.Bucket
	for _, b := range buckets {
		bucketTenantID := b.TenantID
		if b.OwnerType == "tenant" && bucketTenantID == "" {
			bucketTenantID = b.OwnerID
		}
		if h.userCanPerformS3ActionInTenant(r.Context(), user, bucketTenantID,
			auth.ActionListBucket, bucketARN(b.Name)) {
			filteredBuckets = append(filteredBuckets, b)
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
			BucketRegion: "us-east-1",
		}
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}


// checkBucketPolicyPermission evaluates bucket policy for a specific action.
// r may be nil (e.g. from internal callers); when non-nil, IP and TLS context
// are extracted so that aws:SourceIp and aws:SecureTransport conditions work.
func (h *Handler) checkBucketPolicyPermission(r *http.Request, tenantID, bucketName, userID, action string) bool {
	if h.bucketManager == nil {
		return false
	}

	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}

	// Get bucket policy
	policy, err := h.bucketManager.GetBucketPolicy(ctx, tenantID, bucketName)
	if err != nil {
		// No policy or error retrieving it = no policy-based permission
		return false
	}

	resource := fmt.Sprintf("arn:aws:s3:::%s", bucketName)
	if strings.Contains(action, "Object") {
		// Object-level actions need /* wildcard
		resource = fmt.Sprintf("arn:aws:s3:::%s/*", bucketName)
	}

	// Build request context with IP and TLS info from the HTTP request
	request := bucket.PolicyEvaluationRequest{
		Principal: userID,
		Action:    action,
		Resource:  resource,
		Bucket:    bucketName,
	}
	if r != nil {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			request.SourceIP = host
		} else {
			request.SourceIP = r.RemoteAddr
		}
		request.SecureTransport = r.TLS != nil
	}

	return bucket.IsActionAllowed(ctx, policy, request)
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

	if !h.userCanPerformS3ActionInTenant(r.Context(), user, user.TenantID, auth.ActionCreateBucket, bucketARN(bucketName)) {
		h.writeError(w, "AccessDenied", "You do not have permission to create buckets", bucketName, r)
		return
	}

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

	if err := h.bucketManager.CreateBucket(r.Context(), tenantID, bucketName, user.ID); err != nil {
		if err == bucket.ErrBucketAlreadyExists {
			errorCode := "BucketAlreadyExists"
			errorMsg := "The requested bucket name is not available"
			if existingBucket, infoErr := h.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName); infoErr == nil {
				// The bucket manager stores user.ID as OwnerID regardless of tenantID.
				if existingBucket.OwnerID == user.ID {
					errorCode = "BucketAlreadyOwnedByYou"
					errorMsg = "Your previous request to create the named bucket succeeded and you already own it"
				}
			}
			h.writeError(w, errorCode, errorMsg, bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	// AWS S3: if the request carries "x-amz-bucket-object-lock-enabled: true",
	// Object Lock must be enabled at creation time (it cannot be enabled later).
	if strings.EqualFold(r.Header.Get("x-amz-bucket-object-lock-enabled"), "true") {
		if err := h.bucketManager.SetObjectLockConfig(r.Context(), tenantID, bucketName, &bucket.ObjectLockConfig{
			ObjectLockEnabled: true,
		}); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"bucket":   bucketName,
				"tenantID": tenantID,
			}).Error("CreateBucket: failed to enable Object Lock")
			// Bucket was created; roll back by deleting it to avoid an inconsistent state.
			_ = h.bucketManager.DeleteBucket(r.Context(), tenantID, bucketName)
			h.writeError(w, "InternalError", "Failed to enable Object Lock on bucket", bucketName, r)
			return
		}
		logrus.WithFields(logrus.Fields{
			"bucket":   bucketName,
			"tenantID": tenantID,
		}).Info("CreateBucket: Object Lock enabled via x-amz-bucket-object-lock-enabled header")
	}

	// AWS S3 requires a Location header on successful bucket creation.
	// Value is always "/{bucketName}" regardless of addressing style.
	w.Header().Set("Location", "/"+bucketName)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) DeleteBucket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: DeleteBucket")

	tenantID := h.resolveBucketTenantID(r, bucketName)
	if _, err := h.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName); err != nil {
		if err == bucket.ErrBucketNotFound {
			if h.authManager != nil && !auth.CheckCapabilityInContext(r.Context(), h.authManager, auth.CapBucketDelete) {
				h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
				return
			}
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	if !h.requireBucketS3Action(w, r, bucketName, auth.ActionDeleteBucket) {
		return
	}

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

	logrus.WithField("bucket", bucketName).Debug("S3 API: HeadBucket")

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}

	tenantID := h.resolveBucketTenantID(r, bucketName)

	// Permission check: Verify user has READ permission via ACL
	user, userExists := auth.GetUserFromContext(r.Context())

	if userExists {
		if !h.userCanPerformS3ActionInTenant(r.Context(), user, tenantID, auth.ActionListBucket, bucketARN(bucketName)) {
			logrus.WithFields(logrus.Fields{
				"bucket":       bucketName,
				"userID":       user.ID,
				"userTenantID": user.TenantID,
				"bucketTenant": tenantID,
			}).Warn("Permission denied for HeadBucket")
			h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
			return
		}
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

	bkt, err := h.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
		return
	}

	w.Header().Set("x-amz-bucket-region", "us-east-1")

	if bkt.ObjectLock != nil && bkt.ObjectLock.ObjectLockEnabled {
		w.Header().Set("x-amz-bucket-object-lock-enabled", "true")
	}

	// Log all response headers AFTER they are fully set (for Veeam debugging)
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

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) ListObjects(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: ListObjects")

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}

	// Permission check: Verify user has READ permission via ACL
	user, userExists := auth.GetUserFromContext(r.Context())
	tenantID := h.resolveBucketTenantID(r, bucketName)

	// An authenticated request is decided by its policies, and by nothing else.
	if userExists {
		if !h.userCanPerformS3ActionInTenant(r.Context(), user, tenantID, auth.ActionListBucket, bucketARN(bucketName)) {
			logrus.WithFields(logrus.Fields{
				"bucket": bucketName,
				"userID": user.ID,
			}).Warn("Permission denied for ListObjects")
			h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
			return
		}
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
		parsed, err := strconv.Atoi(maxKeysStr)
		if err != nil || parsed < 0 {
			h.writeError(w, "InvalidArgument", "The specified value for max-keys is not valid. It must be between 0 and 1000.", bucketName, r)
			return
		}
		maxKeys = parsed
	}

	if maxKeys > 1000 {
		h.writeError(w, "InvalidArgument", "The specified value for max-keys is not valid. It must be between 0 and 1000.", bucketName, r)
		return
	}

	// Parse encoding-type — only "url" is valid per the S3 spec.
	encodingType := r.URL.Query().Get("encoding-type")
	if encodingType != "" && encodingType != "url" {
		h.writeError(w, "InvalidArgument", "Invalid Encoding Method specified in Request", bucketName, r)
		return
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

	// Apply URL encoding to string fields when requested.
	encodeStr := func(s string) string {
		if encodingType == "url" {
			return s3URLEncode(s)
		}
		return s
	}

	// Convert common prefixes to S3 format.
	var commonPrefixes []CommonPrefix
	for _, cp := range listResult.CommonPrefixes {
		commonPrefixes = append(commonPrefixes, CommonPrefix{Prefix: encodeStr(cp.Prefix)})
	}

	result := ListBucketResult{
		Name:           bucketName,
		Prefix:         encodeStr(prefix),
		Marker:         encodeStr(marker),
		MaxKeys:        maxKeys,
		Delimiter:      encodeStr(delimiter),
		IsTruncated:    listResult.IsTruncated,
		NextMarker:     encodeStr(listResult.NextMarker),
		EncodingType:   encodingType,
		CommonPrefixes: commonPrefixes,
		Contents:       make([]ObjectInfo, len(listResult.Objects)),
	}

	for i, obj := range listResult.Objects {
		result.Contents[i] = ObjectInfo{
			Key:          encodeStr(obj.Key),
			LastModified: obj.LastModified,
			ETag:         obj.ETag,
			Size:         obj.Size,
			StorageClass: storageClassOrStandard(obj.StorageClass),
			Owner: &Owner{
				ID:          "maxiofs",
				DisplayName: "MaxIOFS",
			},
		}
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}

// ListObjectsV2 handles GET /{bucket}?list-type=2 per the AWS S3 ListObjectsV2 API.
func (h *Handler) ListObjectsV2(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: ListObjectsV2")

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}

	// Permission check: identical logic to ListObjects
	user, userExists := auth.GetUserFromContext(r.Context())
	tenantID := h.resolveBucketTenantID(r, bucketName)

	if userExists {
		if !h.userCanPerformS3ActionInTenant(r.Context(), user, tenantID, auth.ActionListBucket, bucketARN(bucketName)) {
			logrus.WithFields(logrus.Fields{
				"bucket": bucketName,
				"userID": user.ID,
			}).Warn("Permission denied for ListObjectsV2")
			h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
			return
		}
	} else {
		if !h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead) {
			logrus.WithField("bucket", bucketName).Warn("Public access denied for ListObjectsV2")
			h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
			return
		}
	}

	q := r.URL.Query()

	// V2-specific parameters
	continuationToken := q.Get("continuation-token")
	startAfter := q.Get("start-after")
	fetchOwner := strings.EqualFold(q.Get("fetch-owner"), "true")

	// Shared parameters
	prefix := q.Get("prefix")
	delimiter := q.Get("delimiter")
	maxKeys := 1000

	if s := q.Get("max-keys"); s != "" {
		parsed, err := strconv.Atoi(s)
		if err != nil || parsed < 0 {
			h.writeError(w, "InvalidArgument", "The specified value for max-keys is not valid. It must be between 0 and 1000.", bucketName, r)
			return
		}
		maxKeys = parsed
	}
	if maxKeys > 1000 {
		h.writeError(w, "InvalidArgument", "The specified value for max-keys is not valid. It must be between 0 and 1000.", bucketName, r)
		return
	}

	// Parse encoding-type — only "url" is valid per the S3 spec.
	encodingTypeV2 := q.Get("encoding-type")
	if encodingTypeV2 != "" && encodingTypeV2 != "url" {
		h.writeError(w, "InvalidArgument", "Invalid Encoding Method specified in Request", bucketName, r)
		return
	}

	// Resolve the internal pagination marker.
	// continuation-token takes precedence over start-after.
	marker := startAfter
	if continuationToken != "" {
		decoded, err := base64.StdEncoding.DecodeString(continuationToken)
		if err != nil {
			h.writeError(w, "InvalidArgument", "The continuation token provided is invalid.", bucketName, r)
			return
		}
		marker = string(decoded)
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

	// Apply URL encoding to string fields when requested.
	encodeStrV2 := func(s string) string {
		if encodingTypeV2 == "url" {
			return s3URLEncode(s)
		}
		return s
	}

	var commonPrefixes []CommonPrefix
	for _, cp := range listResult.CommonPrefixes {
		commonPrefixes = append(commonPrefixes, CommonPrefix{Prefix: encodeStrV2(cp.Prefix)})
	}

	result := ListBucketResultV2{
		Name:              bucketName,
		Prefix:            encodeStrV2(prefix),
		Delimiter:         encodeStrV2(delimiter),
		MaxKeys:           maxKeys,
		KeyCount:          len(listResult.Objects) + len(commonPrefixes),
		IsTruncated:       listResult.IsTruncated,
		ContinuationToken: continuationToken,
		StartAfter:        encodeStrV2(startAfter),
		EncodingType:      encodingTypeV2,
		CommonPrefixes:    commonPrefixes,
		Contents:          make([]ObjectInfo, len(listResult.Objects)),
	}

	// NextContinuationToken is a base64-encoded opaque marker for the next page.
	if listResult.IsTruncated && listResult.NextMarker != "" {
		result.NextContinuationToken = base64.StdEncoding.EncodeToString([]byte(listResult.NextMarker))
	}

	for i, obj := range listResult.Objects {
		info := ObjectInfo{
			Key:          encodeStrV2(obj.Key),
			LastModified: obj.LastModified,
			ETag:         obj.ETag,
			Size:         obj.Size,
			StorageClass: storageClassOrStandard(obj.StorageClass),
		}
		// Owner is only included when explicitly requested via fetch-owner=true
		if fetchOwner {
			info.Owner = &Owner{
				ID:          "maxiofs",
				DisplayName: "MaxIOFS",
			}
		}
		result.Contents[i] = info
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

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}

	// Check if user is authenticated
	user, userExists := auth.GetUserFromContext(r.Context())

	if isVeeamSOSAPIObject(objectKey) {
		if !userExists {
			h.writeError(w, "AccessDenied", "Authentication required", objectKey, r)
			return
		}
		if h.handleVeeamSOSAPIObject(w, r, objectKey) {
			return
		}
	}

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

	// Extract actual bucket tenant ID for permission checking. Global buckets
	// keep an empty tenant even when the requester is tenant-scoped.
	tenantID := h.resolveBucketTenantID(r, bucketName)

	// If NOT authenticated, check if this is a presigned URL request
	if !userExists && presigned.IsPresignedURL(r) {
		accessKeyID, valid, err := h.validatePresignedURLAccess(r, objectKey)
		if err != nil {
			h.handlePresignedURLError(w, err, objectKey, r)
			return
		}

		if valid {
			allowedByPresignedURL = true
			logrus.WithFields(logrus.Fields{
				"bucket":      bucketName,
				"object":      objectKey,
				"accessKeyID": accessKeyID,
			}).Info("Presigned URL access allowed")
		}
	}

	// If NOT authenticated and NOT allowed by presigned URL, check if object has an active share (Public Link)
	var shareTenantID string
	allowedByShare := false
	if !userExists && !allowedByPresignedURL && h.shareManager != nil {
		realBucket, realObject, tenantFromShare, err := h.validateShareAccess(r, bucketName, objectKey)
		if err != nil {
			h.writeError(w, "AccessDenied", "Access denied. Object is not shared.", objectKey, r)
			return
		}

		shareTenantID = tenantFromShare
		allowedByShare = true // access granted via share (shareTenantID may be "" for global bucket)
		// Override vars for subsequent processing
		bucketName = realBucket
		objectKey = realObject
	}

	// Build bucket path: use shareTenantID if available, otherwise use auth-based tenant
	// IMPORTANT: Use same logic as PutObject to ensure consistency
	bucketPath := h.resolveBucketPath(r, bucketName, shareTenantID)

	logrus.WithFields(logrus.Fields{
		"bucket":        bucketName,
		"object":        objectKey,
		"bucketPath":    bucketPath,
		"shareTenantID": shareTenantID,
		"tenantID":      tenantID,
	}).Info("GetObject: Using bucketPath")

	// 1. Verificar permiso de BUCKET únicamente (NO verificar ACL de objeto aún)
	// El objeto puede no existir, así que solo verificamos permisos de bucket
	if !h.validateBucketReadPermission(w, r, user, userExists, allowedByPresignedURL, allowedByShare, shareTenantID, tenantID, bucketName, objectKey) {
		return
	}

	if h.clusterManager != nil {
		nodes, _ := h.clusterManager.SelectReadNodes(r.Context(), bucketPath)
		for _, node := range nodes {
			served, tryErr := h.clusterManager.TryProxyRead(r.Context(), w, r, node)
			if served {
				return
			}
			if tryErr != nil {
				logrus.WithError(tryErr).WithField("node_id", node.ID).
					Debug("read fallback: replica did not serve, trying next")
			}
		}
	}

	// 3. Intentar obtener el objeto
	// Si el objeto NO existe, devolver NoSuchKey (404) - esto es correcto para S3
	versionID := r.URL.Query().Get("versionId")
	obj, reader, err := h.objectManager.GetObject(r.Context(), bucketPath, objectKey, versionID)
	if err != nil {
		if err == object.ErrObjectNotFound {
			if h.handleVersionedObjectNotFound(w, r, bucketPath, objectKey, versionID) {
				return
			}
			// Objeto no existe - devolver 404 (comportamiento correcto de S3)
			// VEEAM usa esto para detectar si smart-entity-status.xml existe (primer backup)
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}
	defer reader.Close()

	// Handle conditional requests (If-Match, If-None-Match, If-Modified-Since, If-Unmodified-Since)
	if !h.validateConditionalHeaders(w, r, obj.ETag, obj.LastModified) {
		return
	}

	// 3. El objeto existe - ahora verificar ACL de objeto (solo para cross-tenant)
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

	// Set common response headers
	h.setGetObjectResponseHeaders(w, obj)

	// Throttle the download to the owning tenant's aggregate bandwidth budget
	// (nil = unlimited; only the bytes actually streamed to the client count).
	dlLimiter := h.tenantBandwidthLimiter(r.Context(), r, bucketName)

	// Handle range request
	if isRangeRequest {
		if err := h.sendRangeResponse(r.Context(), w, reader, rangeStart, rangeEnd, obj.Size, dlLimiter); err != nil {
			return
		}
	} else {
		// Send entire object (no range request)
		if err := h.sendFullResponse(r.Context(), w, reader, obj.Size, dlLimiter); err != nil {
			return
		}
	}
}

func (h *Handler) PutObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := getObjectKey(r)

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}

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
	tenantID := h.resolveBucketTenantID(r, bucketName)

	// Check tenant storage quota before accepting upload
	if err := h.validateTenantQuota(r, user, userExists, bucketName, objectKey, decodedContentLength); err != nil {
		h.writeError(w, "QuotaExceeded", err.Error(), objectKey, r)
		return
	}

	// Validate WRITE permission via ACL cascading
	if !h.validateBucketWritePermission(r, user, userExists, tenantID, bucketName, objectKey) {
		logrus.WithFields(logrus.Fields{
			"bucket":        bucketName,
			"object":        objectKey,
			"userID":        getUserIDOrAnonymous(user),
			"authenticated": userExists,
			"userTenantID": func() string {
				if user != nil {
					return user.TenantID
				}
				return ""
			}(),
			"bucketTenant": tenantID,
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

	// Conditional write: If-None-Match: * means "write only if the object does not exist"
	if r.Header.Get("If-None-Match") == "*" {
		if existing, err := h.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey); err == nil && existing != nil {
			h.writeError(w, "PreconditionFailed", "At least one of the pre-conditions you specified did not hold", objectKey, r)
			return
		}
	}

	// Leer headers de Object Lock si están presentes (para Veeam)
	lockMode := r.Header.Get("x-amz-object-lock-mode")
	retainUntilDateStr := r.Header.Get("x-amz-object-lock-retain-until-date")
	legalHoldStatus := r.Header.Get("x-amz-object-lock-legal-hold")

	// Detect and decode AWS chunked encoding
	bodyReader := h.detectAndDecodeAwsChunked(r, bucketName, objectKey, contentEncoding, decodedContentLength)

	// Throttle the upload to the owning tenant's aggregate bandwidth budget
	// (no-op when the tenant has no cap / bucket is global).
	bodyReader = bandwidth.ThrottleReader(r.Context(), bodyReader, h.tenantBandwidthLimiter(r.Context(), r, bucketName))

	requestTags, tagErr := parseS3TaggingHeader(r.Header.Get("x-amz-tagging"))
	if tagErr != nil {
		h.writeError(w, "InvalidTag", tagErr.Error(), objectKey, r)
		return
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

		if errors.Is(err, cluster.ErrClusterDegraded) {
			w.Header().Set("Retry-After", "30")
			h.writeError(w, "ServiceUnavailable", "Cluster degraded — replication quorum unavailable, retry later", objectKey, r)
			return
		}
		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		if errors.Is(err, object.ErrBucketQuotaExceeded) {
			h.writeError(w, "QuotaExceeded", err.Error(), objectKey, r)
			return
		}
		if strings.HasPrefix(err.Error(), "BadDigest:") {
			h.writeError(w, "BadDigest", err.Error(), objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Apply Object Lock retention from headers (Veeam compatibility)
	retentionApplied := h.applyObjectLockFromHeaders(r, bucketPath, bucketName, objectKey, lockMode, retainUntilDateStr)

	// If no retention was applied from headers, apply default bucket retention
	if !retentionApplied {
		h.applyDefaultBucketRetention(r, bucketPath, bucketName, objectKey, tenantID)
	}

	// Apply legal hold if specified (Veeam compatibility)
	h.applyLegalHold(r, bucketPath, bucketName, objectKey, legalHoldStatus)

	// Apply canned ACL from x-amz-acl header if present.
	// Must be done after object is stored so SetObjectACL has something to act on.
	if cannedACL := r.Header.Get("x-amz-acl"); cannedACL != "" {
		h.applyObjectCannedACLHeader(r.Context(), bucketPath, objectKey, cannedACL)
	}

	if requestTags != nil {
		if err := h.objectManager.SetObjectTagging(r.Context(), bucketPath, objectKey, requestTags); err != nil {
			h.writeError(w, "InternalError", err.Error(), objectKey, r)
			return
		}
	}

	// Note: Bucket metrics and tenant storage are updated by objectManager.PutObject()
	// No need to increment here to avoid double-counting on overwrites

	h.setPutObjectResponseHeaders(w, obj)
	w.WriteHeader(http.StatusOK)

	// Fire s3:ObjectCreated:Put notification asynchronously.
	h.fireNotifications(r.Context(), bucketName, tenantID, objectKey, "s3:ObjectCreated:Put", obj.ETag, obj.Size)

	// Queue object for realtime replication (async, best-effort)
	if h.replicationManager != nil {
		go func() {
			if err := h.replicationManager.QueueRealtimeObject(context.Background(), tenantID, bucketName, objectKey, "PUT"); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"bucket": bucketName,
					"object": objectKey,
				}).Debug("Replication queue skipped (no matching rules or error)")
			}
		}()
	}
}

func (h *Handler) DeleteObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := getObjectKey(r)

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}

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
	tenantID := h.resolveBucketTenantID(r, bucketName)
	bucketPath := h.getBucketPath(r, bucketName)

	hasPermission := h.checkDeleteObjectPermission(r.Context(), user, userExists, tenantID, bucketName, bucketPath, objectKey, versionID)
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
		if err := h.validateBypassGovernance(r.Context(), user, userExists, tenantID, bucketName, objectKey); err != nil {
			h.writeError(w, "AccessDenied", err.Error(), objectKey, r)
			return
		}
	}

	// Get object info before deletion to track size for metrics
	objectSize := h.getObjectSizeBeforeDeletion(r.Context(), bucketPath, objectKey, versionID)

	deleteMarkerVersionID, err := h.objectManager.DeleteObject(r.Context(), bucketPath, objectKey, bypassGovernance, versionID)
	if h.handleDeleteObjectErrors(w, r, err, bucketName, objectKey, versionID) {
		return
	}

	// Update bucket metrics after successful deletion
	h.updateMetricsAfterDeletion(r.Context(), user, userExists, tenantID, bucketName, objectSize, deleteMarkerVersionID)

	// Set response headers
	h.setDeleteResponseHeaders(w, deleteMarkerVersionID, versionID)
	w.WriteHeader(http.StatusNoContent)

	// Fire notification: permanent version delete → ObjectRemoved:Delete,
	// delete marker creation → ObjectRemoved:DeleteMarkerCreated.
	eventName := "s3:ObjectRemoved:Delete"
	if deleteMarkerVersionID != "" && versionID == "" {
		eventName = "s3:ObjectRemoved:DeleteMarkerCreated"
	}
	h.fireNotifications(r.Context(), bucketName, tenantID, objectKey, eventName, "", 0)

	// Queue delete for realtime replication (async, best-effort)
	if h.replicationManager != nil {
		go func() {
			if err := h.replicationManager.QueueRealtimeObject(context.Background(), tenantID, bucketName, objectKey, "DELETE"); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"bucket": bucketName,
					"object": objectKey,
				}).Debug("Replication delete queue skipped (no matching rules or error)")
			}
		}()
	}
}

func (h *Handler) HeadObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := getObjectKey(r)

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: HeadObject")

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}

	// Permission check: Verify user has READ permission on BUCKET (not object yet)
	user, userExists := auth.GetUserFromContext(r.Context())
	tenantID := h.resolveBucketTenantID(r, bucketName)
	bucketPath := h.getBucketPath(r, bucketName)

	// Check if this is a VEEAM SOSAPI virtual object (handle early, no ACL needed)
	if isVeeamSOSAPIObject(objectKey) {
		// SOSAPI requires authentication - Veeam sends credentials
		if !userExists {
			h.writeError(w, "AccessDenied", "Authentication required", objectKey, r)
			return
		}
		if !h.userCanPerformS3ActionInTenant(r.Context(), user, tenantID, auth.ActionGetObject, objectARN(bucketName, objectKey)) {
			h.writeError(w, "AccessDenied", "Access denied", objectKey, r)
			return
		}

		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"object": objectKey,
		}).Info("HeadObject for VEEAM SOSAPI virtual object")

		data, contentType, err := h.getSOSAPIVirtualObject(r.Context(), bucketName, tenantID, objectKey)
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
	if !h.validateHeadBucketReadPermission(w, r, user, userExists, tenantID, bucketName, objectKey) {
		return
	}

	// Try to get object metadata - may return NoSuchKey if doesn't exist
	// If versionId is specified, use GetObject which supports version lookup
	versionID := r.URL.Query().Get("versionId")
	var obj *object.Object
	var err error
	if versionID != "" {
		var reader io.ReadCloser
		obj, reader, err = h.objectManager.GetObject(r.Context(), bucketPath, objectKey, versionID)
		if reader != nil {
			reader.Close()
		}
	} else {
		obj, err = h.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey)
	}
	if err != nil {
		if err == object.ErrObjectNotFound {
			if h.handleVersionedObjectNotFound(w, r, bucketPath, objectKey, versionID) {
				return
			}
			// Object doesn't exist - return 404 (VEEAM uses this to detect missing files)
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Handle conditional requests (If-Match, If-None-Match, If-Modified-Since, If-Unmodified-Since)
	if !h.validateConditionalHeaders(w, r, obj.ETag, obj.LastModified) {
		return
	}

	// Object exists - now check object-level ACLs for cross-tenant access
	h.setHeadObjectResponseHeaders(w, obj)
	w.WriteHeader(http.StatusOK)
}

// Placeholder implementations for other S3 operations
func (h *Handler) GetBucketLocation(w http.ResponseWriter, r *http.Request) {
	// Add S3-compatible headers (CRITICAL for Veeam recognition)
	addS3CompatHeaders(w)

	// Detect Veeam and log
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}
	if !h.requireBucketS3Action(w, r, bucketName, auth.ActionGetBucketLocation) {
		return
	}
	userAgent := r.Header.Get("User-Agent")
	if isVeeamClient(userAgent) {
		logrus.WithFields(logrus.Fields{
			"bucket":     bucketName,
			"user_agent": userAgent,
			"method":     r.Method,
			"uri":        r.RequestURI,
		}).Warn("VEEAM GetBucketLocation - DETECTION PHASE - May determine auto-provisioning")
	}

	// x-amz-bucket-region: Veeam reads this header from both HeadBucket and
	// GetBucketLocation to determine the bucket region and decide multi-bucket mode.
	w.Header().Set("x-amz-bucket-region", "us-east-1")

	// AWS S3 spec: buckets in the default region return an empty LocationConstraint.
	h.writeXMLResponse(w, http.StatusOK, LocationConstraintResponse{Location: ""})
}

func (h *Handler) GetBucketVersioning(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}
	if !h.requireBucketS3Action(w, r, bucketName, auth.ActionGetBucketVersioning) {
		return
	}
	tenantID := h.resolveBucketTenantID(r, bucketName)

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

	versioningStatus := ""
	if bkt.Versioning != nil {
		versioningStatus = bkt.Versioning.Status
	}
	if (versioningStatus == "" || versioningStatus == "Suspended") && bkt.ObjectLock != nil && bkt.ObjectLock.ObjectLockEnabled {
		versioningStatus = "Enabled"
	}

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"status": versioningStatus,
	}).Info("GetBucketVersioning - returning status")

	var statusXML string
	if versioningStatus == "" {
		statusXML = `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></VersioningConfiguration>`
	} else {
		statusXML = fmt.Sprintf(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>%s</Status></VersioningConfiguration>`, versioningStatus)
	}

	h.writeXMLResponse(w, http.StatusOK, statusXML)
}

func (h *Handler) PutBucketVersioning(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}

	if !h.requireBucketS3Action(w, r, bucketName, auth.ActionPutBucketVersioning) {
		return
	}
	tenantID := h.resolveBucketTenantID(r, bucketName)

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

	// Object Lock buckets require versioning to be permanently Enabled.
	// AWS S3 does not allow suspending versioning on an Object Lock bucket.
	if versioningConfig.Status == "Suspended" {
		bkt, err := h.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
		if err == nil && bkt.ObjectLock != nil && bkt.ObjectLock.ObjectLockEnabled {
			h.writeError(w, "InvalidBucketState",
				"Object Lock is enabled on this bucket. Versioning cannot be suspended on a bucket with Object Lock enabled.", bucketName, r)
			return
		}
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

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
	}).Info("S3 API: GetObjectLockConfiguration - START")

	if !h.requireBucketS3Action(w, r, bucketName, auth.ActionGetBucketObjectLockConfiguration) {
		return
	}

	// The tenant that owns the bucket, not the caller's: reading the caller's
	// back made every tenant comparison compare a value with itself.
	tenantID := h.resolveBucketTenantID(r, bucketName)

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

	// Cluster routing: proxy to the node that owns this bucket if not local
	if h.proxyBucketRequest(w, r, bucketName) {
		return
	}

	if !h.requireBucketS3Action(w, r, bucketName, auth.ActionPutBucketObjectLockConfiguration) {
		return
	}

	// The tenant that owns the bucket, not the caller's.
	tenantID := h.resolveBucketTenantID(r, bucketName)
	logrus.WithFields(logrus.Fields{
		"tenantID": tenantID,
		"bucket":   bucketName,
	}).Info("PutObjectLockConfiguration - Got tenantID")

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

	if bucketInfo.ObjectLock == nil || !bucketInfo.ObjectLock.ObjectLockEnabled {
		logrus.Warn("PutObjectLockConfiguration - Object Lock not enabled on bucket")
		h.writeError(w, "InvalidBucketState",
			"Object Lock must be enabled on the bucket at creation time. Create a new bucket with Object Lock enabled.", bucketName, r)
		return
	}

	// Leer y parsear la nueva configuración del body
	newConfig, ok := h.parseObjectLockConfigXML(w, r, bucketName)
	if !ok {
		return
	}

	if newConfig.Rule != nil && newConfig.Rule.DefaultRetention == nil {
		h.writeError(w, "MalformedXML",
			"Object Lock Rule element must include DefaultRetention", bucketName, r)
		return
	}

	if newConfig.Rule == nil {
		bucketInfo.ObjectLock.Rule = nil
		logrus.WithField("bucket", bucketName).Info("PutObjectLockConfiguration - Cleared default retention rule")
	} else {
		newMode := newConfig.Rule.DefaultRetention.Mode
		newDays := calculateRetentionDays(newConfig.Rule.DefaultRetention.Years, newConfig.Rule.DefaultRetention.Days)
		updateBucketRetentionConfig(bucketInfo, newConfig)
		logrus.WithFields(logrus.Fields{
			"bucket":  bucketName,
			"newDays": newDays,
			"mode":    newMode,
		}).Info("PutObjectLockConfiguration - Updated default retention rule")
	}

	// Guardar cambios en el bucket
	if err := h.bucketManager.UpdateBucket(r.Context(), tenantID, bucketName, bucketInfo); err != nil {
		logrus.WithError(err).Error("PutObjectLockConfiguration - Failed to update bucket")
		h.writeError(w, "InternalError", "Failed to update Object Lock configuration", bucketName, r)
		return
	}

	logrus.WithField("bucket", bucketName).Info("PutObjectLockConfiguration - Configuration saved successfully")

	w.WriteHeader(http.StatusOK)
}

// Utility methods
func (h *Handler) writeXMLResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	var buf bytes.Buffer

	if str, ok := data.(string); ok {
		// Prepend XML declaration only if the string doesn't already carry one.
		if !strings.HasPrefix(strings.TrimSpace(str), "<?xml") {
			buf.WriteString(xml.Header)
		}
		buf.WriteString(str)
	} else {
		// Write XML declaration before the encoded struct (AWS S3 always includes it).
		buf.WriteString(xml.Header)
		if err := xml.NewEncoder(&buf).Encode(data); err != nil {
			logrus.WithError(err).Error("Failed to encode XML response")
		}
	}

	body := buf.Bytes()
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	w.WriteHeader(statusCode)
	w.Write(body)
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

// resolveBucketTenantID returns the tenant that actually owns the bucket.
// Authenticated tenant users may access global buckets through ACLs; in that
// case the bucket path must remain unprefixed instead of using user.TenantID.
func (h *Handler) resolveBucketTenantID(r *http.Request, bucketName string) string {
	if h.metadataStore != nil {
		bucketMeta, err := h.metadataStore.GetBucketByName(r.Context(), bucketName)
		if err == nil && bucketMeta != nil {
			return bucketMeta.TenantID
		}
	}

	return h.getTenantIDFromRequest(r)
}

// getBucketPath constructs the full bucket path with tenant prefix for object manager
// Format: "tenantID/bucketName" for tenant buckets, or "bucketName" for global buckets
// This is transparent to S3 clients - they only see "bucketName"
func (h *Handler) getBucketPath(r *http.Request, bucketName string) string {
	tenantID := h.resolveBucketTenantID(r, bucketName)

	if tenantID == "" {
		return bucketName // Global bucket
	}
	return tenantID + "/" + bucketName // Tenant-scoped bucket path
}

func (h *Handler) writeError(w http.ResponseWriter, code, message, resource string, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml")

	statusCode := http.StatusInternalServerError
	switch code {
	// 400 Bad Request (AWS S3 standard)
	case "InvalidArgument", "InvalidBucketName", "InvalidRequest", "MalformedXML", "MalformedPolicy",
		"MalformedPOSTRequest", "InvalidPolicyDocument", "InvalidTag", "InvalidPart",
		"IllegalVersioningConfigurationException", "BadDigest", "EntityTooSmall", "EntityTooLarge",
		"InvalidDigest":
		statusCode = http.StatusBadRequest
	// 401 Unauthorized
	case "Unauthorized":
		statusCode = http.StatusUnauthorized
	// 403 Forbidden — AWS S3 returns 403 (not 401) for signature/credential errors
	case "AccessDenied", "AccountProblem", "AllAccessDisabled", "QuotaExceeded",
		"InvalidAccessKeyId", "SignatureDoesNotMatch", "RequestExpired":
		statusCode = http.StatusForbidden
	// 404 Not Found (AWS S3 standard)
	case "NoSuchBucket", "NoSuchKey", "NoSuchUpload", "ObjectLockConfigurationNotFoundError",
		"NoSuchBucketPolicy", "NoSuchObjectLockConfiguration", "NoSuchLifecycleConfiguration",
		"NoSuchCORSConfiguration", "NoSuchWebsiteConfiguration",
		"ServerSideEncryptionConfigurationNotFoundError", "NoSuchPublicAccessBlockConfiguration",
		"NoSuchConfiguration", "ReplicationConfigurationNotFoundError", "NoSuchVersion":
		statusCode = http.StatusNotFound
	// 405 Method Not Allowed
	case "MethodNotAllowed":
		statusCode = http.StatusMethodNotAllowed
	// 409 Conflict
	case "BucketAlreadyExists", "BucketAlreadyOwnedByYou", "BucketNotEmpty", "OperationAborted", "InvalidBucketState", "RestoreAlreadyInProgress":
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
		// Log the real error internally but never expose server internals to clients.
		// This prevents filesystem paths, hostnames, and other internal details from leaking.
		logrus.WithFields(logrus.Fields{
			"resource": resource,
			"detail":   message,
		}).Error("InternalError: suppressing detail from S3 response")
		message = "We encountered an internal error. Please try again."
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

	if r != nil && r.Method == http.MethodHead {
		return
	}

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
	case "NoSuchKey", "NoSuchVersion", "ObjectNotInActiveTierError", "NoSuchObjectLockConfiguration":
		errorResponse.Key = resource
	case "NoSuchBucket", "BucketAlreadyExists", "BucketNotEmpty", "NoSuchLifecycleConfiguration",
		"NoSuchCORSConfiguration", "NoSuchBucketPolicy", "NoSuchWebsiteConfiguration",
		"ServerSideEncryptionConfigurationNotFoundError", "NoSuchPublicAccessBlockConfiguration",
		"ReplicationConfigurationNotFoundError":
		errorResponse.BucketName = resource

	// Auth errors: AWS includes AWSAccessKeyId in the response body.
	// Extract it from the request so SDK clients can correlate the failed key.
	case "InvalidAccessKeyId", "SignatureDoesNotMatch":
		if r != nil {
			if ak := r.URL.Query().Get("AWSAccessKeyId"); ak != "" {
				errorResponse.AWSAccessKeyId = ak
			} else if cred := r.URL.Query().Get("X-Amz-Credential"); cred != "" {
				if idx := strings.Index(cred, "/"); idx > 0 {
					errorResponse.AWSAccessKeyId = cred[:idx]
				}
			} else if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "AWS ") {
				if parts := strings.SplitN(auth[4:], ":", 2); len(parts) == 2 {
					errorResponse.AWSAccessKeyId = parts[0]
				}
			}
		}

	// RequestExpired: AWS includes ExpiresDate and ServerTime so clients can
	// diagnose clock-skew issues without guessing.
	case "RequestExpired":
		now := time.Now().UTC().Format(time.RFC3339)
		errorResponse.ServerTime = now
		if r != nil {
			// V2 presigned: Expires is a Unix timestamp
			if exp := r.URL.Query().Get("Expires"); exp != "" {
				if ts, err := strconv.ParseInt(exp, 10, 64); err == nil {
					errorResponse.ExpiresDate = time.Unix(ts, 0).UTC().Format(time.RFC3339)
				}
			}
			// V4 presigned: expiration = X-Amz-Date + X-Amz-Expires seconds
			if amzDate := r.URL.Query().Get("X-Amz-Date"); amzDate != "" {
				if expSecs := r.URL.Query().Get("X-Amz-Expires"); expSecs != "" {
					if t, err := time.Parse("20060102T150405Z", amzDate); err == nil {
						if secs, err := strconv.ParseInt(expSecs, 10, 64); err == nil {
							errorResponse.ExpiresDate = t.Add(time.Duration(secs) * time.Second).UTC().Format(time.RFC3339)
						}
					}
				}
			}
		}

	default:
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

	// Split on comma — multi-range: serve the first valid range only (widely accepted behaviour)
	ranges := strings.Split(rangeSpec, ",")

	// Parse start-end
	parts := strings.Split(strings.TrimSpace(ranges[0]), "-")
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
	if h.bucketManager == nil {
		return false
	}

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
	if h.objectManager == nil {
		return false
	}

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
	if h.bucketManager == nil {
		return false
	}

	// PublicAccessBlock overrides ACL — if IgnorePublicAcls or RestrictPublicBuckets is set,
	// deny all public access regardless of ACL grants.
	if pab, err := h.bucketManager.GetPublicAccessBlock(ctx, tenantID, bucketName); err == nil && pab != nil {
		if pab.IgnorePublicAcls || pab.RestrictPublicBuckets {
			return false
		}
	}

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
	if h.objectManager == nil {
		return false
	}

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

// DeleteObjectVersion handles DELETE /{bucket}/{key}?versionId={id}
func (h *Handler) DeleteObjectVersion(w http.ResponseWriter, r *http.Request) {
	// This handler is called when DELETE has versionId query parameter
	// Redirect to DeleteObject which now handles versionId
	h.DeleteObject(w, r)
}


// ========== GetObject Helper Functions (Refactoring for Complexity Reduction) ==========

// validatePresignedURLAccess validates a presigned URL and returns access key ID if valid
func (h *Handler) validatePresignedURLAccess(r *http.Request, objectKey string) (string, bool, error) {
	// Extract access key from presigned URL
	accessKeyID := presigned.ExtractAccessKeyID(r)
	if accessKeyID == "" {
		return "", false, fmt.Errorf("invalid presigned URL: missing access key")
	}

	// Get access key to retrieve secret
	if h.authManager == nil {
		return "", false, fmt.Errorf("auth manager not configured")
	}

	// Resolves permanent access keys and STS temporary credentials alike; for
	// the latter it also enforces session expiry, token match and user status.
	secretAccessKey, err := h.getSecretKeyForCredential(r.Context(), accessKeyID, r.URL.Query().Get("X-Amz-Security-Token"))
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"accessKeyID": accessKeyID,
			"error":       err.Error(),
		}).Warn("Presigned URL: credential not usable")
		return "", false, fmt.Errorf("access key not found")
	}

	// Validate presigned URL signature
	valid, err := presigned.ValidatePresignedURL(r, secretAccessKey)
	if err != nil || !valid {
		logrus.WithFields(logrus.Fields{
			"accessKeyID": accessKeyID,
			"error":       err,
		}).Warn("Presigned URL validation failed")
		return "", false, fmt.Errorf("signature validation failed")
	}

	logrus.WithFields(logrus.Fields{
		"accessKeyID": accessKeyID,
	}).Info("Presigned URL validated successfully")

	return accessKeyID, true, nil
}

// handleVeeamSOSAPIObject handles VEEAM SOSAPI virtual objects
func (h *Handler) handleVeeamSOSAPIObject(w http.ResponseWriter, r *http.Request, objectKey string) bool {
	if !isVeeamSOSAPIObject(objectKey) {
		return false
	}

	bucketName := mux.Vars(r)["bucket"]
	tenantID := h.resolveBucketTenantID(r, bucketName)
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		h.writeError(w, "AccessDenied", "Authentication required", objectKey, r)
		return true
	}
	if !h.userCanPerformS3ActionInTenant(r.Context(), user, tenantID, auth.ActionGetObject, objectARN(bucketName, objectKey)) {
		h.writeError(w, "AccessDenied", "Access denied", objectKey, r)
		return true
	}

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Info("Serving VEEAM SOSAPI virtual object (authenticated)")

	data, contentType, err := h.getSOSAPIVirtualObject(r.Context(), bucketName, tenantID, objectKey)
	if err != nil {
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return true
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("ETag", `"sosapi-virtual-object"`)
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
	return true
}

// validateShareAccess validates if object is shared and returns real bucket/object names and tenant.
// For clean share URLs (no tenant in path), lookup is by bucket+object only; tenantID is passed
// empty so the store finds the share regardless of which tenant owns the bucket.
func (h *Handler) validateShareAccess(r *http.Request, bucketName, objectKey string) (string, string, string, error) {
	if h.shareManager == nil {
		return "", "", "", fmt.Errorf("share manager not available")
	}

	realBucket := bucketName
	realObject := objectKey
	extractedTenant := ""

	// If bucketName starts with "tenant-", it's actually the tenant ID (legacy path-style)
	if strings.HasPrefix(bucketName, "tenant-") {
		extractedTenant = bucketName
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

	// Normalize for lookup: URL path may be percent-encoded (e.g. %2F for /)
	lookupBucket := realBucket
	lookupObject := realObject
	if d, err := url.PathUnescape(realBucket); err == nil {
		lookupBucket = d
	}
	if d, err := url.PathUnescape(realObject); err == nil {
		lookupObject = d
	}

	shareInterface, err := h.shareManager.GetShareByObject(r.Context(), lookupBucket, lookupObject, extractedTenant)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"bucket": lookupBucket,
			"object": lookupObject,
			"tenant": extractedTenant,
			"error":  err.Error(),
		}).Warn("Unauthenticated access denied - no active share found")
		return "", "", "", err
	}

	s, ok := shareInterface.(*share.Share)
	if !ok || s == nil {
		return "", "", "", fmt.Errorf("share manager returned invalid type")
	}

	// Return the share's bucket/object so path resolution uses the canonical stored values
	shareTenantID := s.TenantID
	logrus.WithFields(logrus.Fields{
		"bucket":   s.BucketName,
		"object":   s.ObjectKey,
		"tenantID": shareTenantID,
	}).Info("Shared object access - bypassing authentication")

	return s.BucketName, s.ObjectKey, shareTenantID, nil
}

// sendRangeResponse sends a partial content response for Range requests
func (h *Handler) sendRangeResponse(ctx context.Context, w http.ResponseWriter, reader io.ReadCloser, rangeStart, rangeEnd, totalSize int64, limiter *rate.Limiter) error {
	contentLength := rangeEnd - rangeStart + 1

	// Set 206 Partial Content headers
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeStart, rangeEnd, totalSize))
	w.WriteHeader(http.StatusPartialContent)

	// Skip to start position if needed
	if rangeStart > 0 {
		if seeker, ok := reader.(io.Seeker); ok {
			// Use Seek if available (more efficient)
			if _, err := seeker.Seek(rangeStart, io.SeekStart); err != nil {
				logrus.WithError(err).Error("Failed to seek to range start")
				return err
			}
		} else {
			// Fall back to reading and discarding bytes
			if _, err := io.CopyN(io.Discard, reader, rangeStart); err != nil {
				logrus.WithError(err).Error("Failed to skip to range start")
				return err
			}
		}
	}

	// Copy only the requested range (throttled to the tenant budget if set).
	// The seek/skip above ran on the raw reader; only the streamed bytes count.
	if _, err := io.CopyN(w, bandwidth.ThrottleReader(ctx, reader, limiter), contentLength); err != nil && err != io.EOF {
		logrus.WithError(err).Error("Failed to write partial object data")
		return err
	}

	return nil
}

// sendFullResponse sends the complete object response
func (h *Handler) sendFullResponse(ctx context.Context, w http.ResponseWriter, reader io.Reader, size int64, limiter *rate.Limiter) error {
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))

	// Copy object data to response (throttled to the tenant budget if set).
	if _, err := io.Copy(w, bandwidth.ThrottleReader(ctx, reader, limiter)); err != nil {
		logrus.WithError(err).Error("Failed to write object data")
		return err
	}

	return nil
}

// ========== DeleteObject Helper Functions (Refactoring for Complexity Reduction) ==========

// checkDeleteObjectPermission checks if user/anonymous has permission to delete object
func (h *Handler) checkDeleteObjectPermission(ctx context.Context, user *auth.User, userExists bool, tenantID, bucketName, bucketPath, objectKey, versionID string) bool {
	hasPermission := false

	if userExists {
		action := auth.ActionDeleteObject
		if versionID != "" {
			action = auth.ActionDeleteObjectVersion
		}

		hasPermission = h.userCanPerformS3ActionInTenant(ctx, user, tenantID, action,
			objectARN(bucketName, objectKey))
	} else {
		// Unauthenticated access - check if bucket/object allows public WRITE
		hasPermission = h.checkPublicObjectAccess(ctx, bucketPath, objectKey, acl.PermissionWrite)
		if !hasPermission {
			hasPermission = h.checkPublicBucketAccess(ctx, tenantID, bucketName, acl.PermissionWrite)
		}
	}

	return hasPermission
}

// validateBypassGovernance authorizes deleting an object that is still under
func (h *Handler) validateBypassGovernance(ctx context.Context, user *auth.User, userExists bool, tenantID, bucketName, objectKey string) error {
	if !userExists {
		return fmt.Errorf("authentication required for bypass governance retention")
	}

	if !h.userCanPerformS3ActionInTenant(ctx, user, tenantID, auth.ActionBypassGovernanceRetention,
		objectARN(bucketName, objectKey)) {
		return fmt.Errorf("you do not have permission to bypass governance retention")
	}
	return nil
}

// getObjectSizeBeforeDeletion gets object size before deletion for metrics tracking
func (h *Handler) getObjectSizeBeforeDeletion(ctx context.Context, bucketPath, objectKey, versionID string) int64 {
	var objectSize int64
	objInfo, reader, err := h.objectManager.GetObject(ctx, bucketPath, objectKey, versionID)
	if err == nil && objInfo != nil {
		objectSize = objInfo.Size
		if reader != nil {
			reader.Close() // Close immediately, we only need the size
		}
	}
	return objectSize
}

// handleDeleteObjectErrors handles specific delete errors and writes appropriate response
func (h *Handler) handleDeleteObjectErrors(w http.ResponseWriter, r *http.Request, err error, bucketName, objectKey, versionID string) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, cluster.ErrClusterDegraded) {
		w.Header().Set("Retry-After", "30")
		h.writeError(w, "ServiceUnavailable", "Cluster degraded — replication quorum unavailable, retry later", objectKey, r)
		return true
	}

	if err == object.ErrBucketNotFound {
		h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
		return true
	}

	if versionID != "" && err == object.ErrObjectNotFound {
		h.writeError(w, "NoSuchVersion", "The specified version does not exist", objectKey, r)
		return true
	}

	// S3 spec: DELETE on non-existent current object should return success (idempotent)
	if err == object.ErrObjectNotFound {
		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"object": objectKey,
		}).Debug("DELETE on non-existent object - returning success (S3 spec)")
		w.WriteHeader(http.StatusNoContent)
		return true
	}

	// Check if it's a retention error with detailed information
	if retErr, ok := err.(*object.RetentionError); ok {
		h.writeError(w, "AccessDenied", retErr.Error(), objectKey, r)
		return true
	}

	// Check for other Object Lock errors
	if err == object.ErrObjectUnderLegalHold {
		h.writeError(w, "AccessDenied", "Object is under legal hold and cannot be deleted", objectKey, r)
		return true
	}

	h.writeError(w, "InternalError", err.Error(), objectKey, r)
	return true
}

// updateMetricsAfterDeletion updates bucket and tenant metrics after successful deletion
func (h *Handler) updateMetricsAfterDeletion(ctx context.Context, user *auth.User, userExists bool, tenantID, bucketName string, objectSize int64, deleteMarkerVersionID string) {
	// Update bucket metrics after successful deletion
	// Only decrement if we actually deleted an object (not just created delete marker)
	if objectSize > 0 && deleteMarkerVersionID == "" {
		if err := h.bucketManager.DecrementObjectCount(ctx, tenantID, bucketName, objectSize); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"bucket":   bucketName,
				"size":     objectSize,
				"tenantID": tenantID,
			}).Warn("Failed to update bucket metrics after DeleteObject")
		}
	}

	// Update tenant storage usage for quota tracking
	if userExists && user.TenantID != "" && objectSize > 0 && deleteMarkerVersionID == "" {
		if err := h.authManager.DecrementTenantStorage(ctx, user.TenantID, objectSize); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"tenantID": user.TenantID,
				"size":     objectSize,
			}).Warn("Failed to decrement tenant storage usage")
		}
	}
}

// setDeleteResponseHeaders sets appropriate response headers for delete operation
func (h *Handler) setDeleteResponseHeaders(w http.ResponseWriter, deleteMarkerVersionID, versionID string) {
	if deleteMarkerVersionID != "" {
		// A delete marker was created
		w.Header().Set("x-amz-version-id", deleteMarkerVersionID)
		w.Header().Set("x-amz-delete-marker", "true")
	} else if versionID != "" {
		// A specific version was permanently deleted
		w.Header().Set("x-amz-version-id", versionID)
		w.Header().Set("x-amz-delete-marker", "false")
	}
}

func (h *Handler) handleVersionedObjectNotFound(w http.ResponseWriter, r *http.Request, bucketPath, objectKey, versionID string) bool {
	version, found := h.findExactObjectVersion(r.Context(), bucketPath, objectKey, versionID)
	if versionID != "" && !found {
		h.writeError(w, "NoSuchVersion", "The specified version does not exist", objectKey, r)
		return true
	}
	if !isS3DeleteMarkerVersion(version) {
		return false
	}

	w.Header().Set("x-amz-delete-marker", "true")
	if version.VersionID != "" {
		w.Header().Set("x-amz-version-id", version.VersionID)
	}
	if !version.LastModified.IsZero() {
		w.Header().Set("Last-Modified", version.LastModified.UTC().Format(http.TimeFormat))
	}

	if versionID != "" {
		h.writeError(w, "MethodNotAllowed", "The specified method is not allowed against this resource", objectKey, r)
		return true
	}

	h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
	return true
}

func (h *Handler) findExactObjectVersion(ctx context.Context, bucketPath, objectKey, versionID string) (*metadata.ObjectVersion, bool) {
	if h.metadataStore == nil {
		return nil, false
	}
	versions, err := h.metadataStore.ListAllObjectVersions(ctx, bucketPath, objectKey, 0)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"bucket":    bucketPath,
			"object":    objectKey,
			"versionID": versionID,
		}).Debug("failed to inspect object versions for S3 delete-marker response")
		return nil, false
	}
	for _, version := range versions {
		if version == nil || version.Key != objectKey {
			continue
		}
		if versionID != "" {
			if version.VersionID == versionID {
				return version, true
			}
			continue
		}
		if version.IsLatest {
			return version, true
		}
	}
	return nil, false
}

func isS3DeleteMarkerVersion(version *metadata.ObjectVersion) bool {
	return version != nil && version.Size == 0 && version.ETag == ""
}

func parseS3TaggingHeader(raw string) (*object.TagSet, error) {
	if raw == "" {
		return nil, nil
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, fmt.Errorf("tagging header is not valid URL-encoded form data")
	}
	if len(values) > 10 {
		return nil, fmt.Errorf("object cannot have more than 10 tags")
	}
	tags := &object.TagSet{Tags: make([]object.Tag, 0, len(values))}
	for key, vals := range values {
		if key == "" {
			return nil, fmt.Errorf("tag key cannot be empty")
		}
		if len(vals) > 1 {
			return nil, fmt.Errorf("duplicate tag key %q", key)
		}
		value := ""
		if len(vals) == 1 {
			value = vals[0]
		}
		tags.Tags = append(tags.Tags, object.Tag{Key: key, Value: value})
	}
	return tags, nil
}

// handlePresignedURLError writes appropriate error response based on presigned URL validation error
func (h *Handler) handlePresignedURLError(w http.ResponseWriter, err error, objectKey string, r *http.Request) {
	if strings.Contains(err.Error(), "missing access key") {
		h.writeError(w, "InvalidRequest", err.Error(), objectKey, r)
	} else if strings.Contains(err.Error(), "auth manager") {
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
	} else if strings.Contains(err.Error(), "access key not found") {
		h.writeError(w, "InvalidAccessKeyId", "The AWS access key ID you provided does not exist in our records", objectKey, r)
	} else {
		h.writeError(w, "SignatureDoesNotMatch", "The request signature we calculated does not match the signature you provided", objectKey, r)
	}
}

// resolveBucketPath determines the correct bucket path based on share tenant or standard path
func (h *Handler) resolveBucketPath(r *http.Request, bucketName, shareTenantID string) string {
	if shareTenantID != "" {
		return shareTenantID + "/" + bucketName
	}
	return h.getBucketPath(r, bucketName)
}

// validateBucketReadPermission validates bucket-level read permissions for GetObject
func (h *Handler) validateBucketReadPermission(
	w http.ResponseWriter,
	r *http.Request,
	user *auth.User,
	userExists bool,
	allowedByPresignedURL bool,
	allowedByShare bool,
	shareTenantID string,
	tenantID string,
	bucketName string,
	objectKey string,
) bool {
	// Allow if access was granted via presigned URL or via clean share (shareTenantID may be "" for global bucket)
	if allowedByPresignedURL || allowedByShare {
		return true
	}

	// Usuario autenticado
	if userExists {
		action := auth.ActionGetObject
		if r.URL.Query().Get("versionId") != "" {
			action = auth.ActionGetObjectVersion
		}

		if h.userCanPerformS3ActionInTenant(r.Context(), user, tenantID, action, objectARN(bucketName, objectKey)) {
			return true
		}


		logrus.WithFields(logrus.Fields{
			"bucket":       bucketName,
			"object":       objectKey,
			"userID":       user.ID,
			"userTenantID": user.TenantID,
			"bucketTenant": tenantID,
		}).Warn("ACL permission denied for GetObject - cross-tenant access (bucket-level)")
		h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
		return false
	}

	// Usuario anónimo - solo público
	if h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead) {
		return true
	}

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Warn("Public access denied - bucket not publicly readable (bucket-level)")
	h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
	return false
}

// validateConditionalHeaders validates If-Match, If-None-Match, If-Modified-Since
// and If-Unmodified-Since headers per the AWS S3 / RFC 7232 spec.
// ETag conditions are evaluated first; date conditions follow.
func (h *Handler) validateConditionalHeaders(w http.ResponseWriter, r *http.Request, etag string, lastModified time.Time) bool {
	ifMatch := r.Header.Get("If-Match")
	ifNoneMatch := r.Header.Get("If-None-Match")

	if ifMatch != "" {
		if normalizeETag(etag) != normalizeETag(ifMatch) {
			w.WriteHeader(http.StatusPreconditionFailed)
			return false
		}
	}

	if ifNoneMatch != "" {
		if normalizeETag(etag) == normalizeETag(ifNoneMatch) {
			w.WriteHeader(http.StatusNotModified)
			return false
		}
	}

	// Date conditions (only evaluated when the corresponding ETag header is absent).
	if ifModifiedSince := r.Header.Get("If-Modified-Since"); ifModifiedSince != "" && ifNoneMatch == "" {
		if t, err := http.ParseTime(ifModifiedSince); err == nil {
			// Object has NOT been modified since the given time → 304.
			if !lastModified.After(t) {
				w.WriteHeader(http.StatusNotModified)
				return false
			}
		}
	}

	if ifUnmodifiedSince := r.Header.Get("If-Unmodified-Since"); ifUnmodifiedSince != "" && ifMatch == "" {
		if t, err := http.ParseTime(ifUnmodifiedSince); err == nil {
			// Object HAS been modified since the given time → 412.
			if lastModified.After(t) {
				w.WriteHeader(http.StatusPreconditionFailed)
				return false
			}
		}
	}

	return true
}

// normalizeETag strips surrounding double-quote characters from an ETag value so that
// "abc123" and abc123 compare as equal. This is needed because the stored ETag may or
// may not include quotes while the If-Match / If-None-Match header value may differ.
func normalizeETag(etag string) string {
	return strings.Trim(etag, "\"")
}

// setGetObjectResponseHeaders sets all response headers for GetObject operation
func (h *Handler) setGetObjectResponseHeaders(w http.ResponseWriter, obj *object.Object) {
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	w.Header().Set("x-amz-storage-class", storageClassOrStandard(obj.StorageClass))

	// S3 system response headers stored at upload time
	if obj.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", obj.ContentDisposition)
	}
	if obj.ContentEncoding != "" {
		w.Header().Set("Content-Encoding", obj.ContentEncoding)
	}
	if obj.CacheControl != "" {
		w.Header().Set("Cache-Control", obj.CacheControl)
	}
	if obj.ContentLanguage != "" {
		w.Header().Set("Content-Language", obj.ContentLanguage)
	}

	// User-defined metadata (x-amz-meta-*)
	for k, v := range obj.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}

	// Tag count — returned when object has tags
	if obj.Tags != nil && len(obj.Tags.Tags) > 0 {
		w.Header().Set("x-amz-tag-count", strconv.Itoa(len(obj.Tags.Tags)))
	}

	if obj.VersionID != "" {
		w.Header().Set("x-amz-version-id", obj.VersionID)
	}

	if obj.Retention != nil {
		w.Header().Set("x-amz-object-lock-mode", obj.Retention.Mode)
		w.Header().Set("x-amz-object-lock-retain-until-date", obj.Retention.RetainUntilDate.UTC().Format(time.RFC3339))
	}

	if obj.LegalHold != nil && obj.LegalHold.Status == "ON" {
		w.Header().Set("x-amz-object-lock-legal-hold", "ON")
	}

	if obj.ChecksumAlgorithm != "" && obj.ChecksumValue != "" {
		w.Header().Set("x-amz-checksum-algorithm", obj.ChecksumAlgorithm)
		w.Header().Set("x-amz-checksum-"+strings.ToLower(obj.ChecksumAlgorithm), obj.ChecksumValue)
	}

	if obj.SSEAlgorithm != "" {
		w.Header().Set("x-amz-server-side-encryption", obj.SSEAlgorithm)
	}
}


// validateTenantQuota checks tenant storage quota before accepting upload
// Returns error if quota is exceeded, nil if quota check passes or is skipped
func (h *Handler) validateTenantQuota(
	r *http.Request,
	user *auth.User,
	userExists bool,
	bucketName string,
	objectKey string,
	decodedContentLength string,
) error {
	// Skip quota check if no auth manager, no user, or no tenant ID
	if h.authManager == nil || !userExists || user.TenantID == "" {
		return nil
	}

	bucketTenantID := h.resolveBucketTenantID(r, bucketName)
	if bucketTenantID != user.TenantID {
		return nil
	}

	// Get content length for quota check
	contentLength := r.ContentLength
	if decodedContentLength != "" {
		if size, err := strconv.ParseInt(decodedContentLength, 10, 64); err == nil {
			contentLength = size
		}
	}

	// Skip if no content
	if contentLength <= 0 {
		return nil
	}

	// Calculate actual storage increment (consider existing object size)
	var sizeIncrement int64 = contentLength
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
	}

	// Only check quota if we're adding storage (sizeIncrement > 0)
	if sizeIncrement <= 0 {
		logrus.WithField("sizeIncrement", sizeIncrement).Debug("Skipping quota check (not adding storage)")
		return nil
	}

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
		return err
	}

	logrus.Info("Quota check passed")
	return nil
}

// validateBucketWritePermission checks if user has WRITE permission via ACL
// Uses cascading permission checks: same-tenant, cross-tenant ACL, authenticated users, full control, public access
func (h *Handler) validateBucketWritePermission(
	r *http.Request,
	user *auth.User,
	userExists bool,
	tenantID string,
	bucketName string,
	objectKey string,
) bool {
	if !userExists {
		// Unauthenticated access - check if bucket allows public WRITE
		return h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionWrite)
	}

	if h.userCanPerformS3ActionInTenant(r.Context(), user, tenantID, auth.ActionPutObject, objectARN(bucketName, objectKey)) {
		return true
	}

	return false
}

// detectAndDecodeAwsChunked detects AWS chunked encoding and returns appropriate body reader
// AWS CLI and MinIO-Go use "aws-chunked" encoding - detect by headers or content inspection
func (h *Handler) detectAndDecodeAwsChunked(
	r *http.Request,
	bucketName string,
	objectKey string,
	contentEncoding string,
	decodedContentLength string,
) io.Reader {
	var bodyReader io.Reader = r.Body
	isAwsChunked := strings.Contains(contentEncoding, "aws-chunked") || decodedContentLength != ""

	// If not detected by headers, peek at first bytes to check for aws-chunked format
	if !isAwsChunked {
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

	return bodyReader
}

// applyObjectLockFromHeaders applies Object Lock retention from request headers (Veeam compatibility)
// Returns true if retention was successfully applied, false otherwise
func (h *Handler) applyObjectLockFromHeaders(
	r *http.Request,
	bucketPath string,
	bucketName string,
	objectKey string,
	lockMode string,
	retainUntilDateStr string,
) bool {
	if lockMode == "" || retainUntilDateStr == "" {
		return false
	}

	retainUntilDate, parseErr := time.Parse(time.RFC3339, retainUntilDateStr)
	if parseErr != nil {
		logrus.WithError(parseErr).Warn("Failed to parse retain-until-date header")
		return false
	}

	retention := &object.RetentionConfig{
		Mode:            lockMode,
		RetainUntilDate: retainUntilDate,
	}

	if setErr := h.objectManager.SetObjectRetention(r.Context(), bucketPath, objectKey, retention); setErr != nil {
		logrus.WithError(setErr).Warn("Failed to set retention from headers")
		return false
	}

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
		"mode":   lockMode,
		"until":  retainUntilDate,
	}).Info("Applied Object Lock retention from headers")
	return true
}

// applyDefaultBucketRetention applies default bucket retention if configured
func (h *Handler) applyDefaultBucketRetention(
	r *http.Request,
	bucketPath string,
	bucketName string,
	objectKey string,
	tenantID string,
) {
	lockConfig, err := h.bucketManager.GetObjectLockConfig(r.Context(), tenantID, bucketName)
	if err != nil || lockConfig == nil || !lockConfig.ObjectLockEnabled {
		return
	}

	// Apply default retention if configured
	if lockConfig.Rule == nil || lockConfig.Rule.DefaultRetention == nil {
		return
	}

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
	if retention.RetainUntilDate.IsZero() {
		return
	}

	if setErr := h.objectManager.SetObjectRetention(r.Context(), bucketPath, objectKey, retention); setErr != nil {
		logrus.WithError(setErr).Warn("Failed to apply default bucket retention")
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
		"mode":   retention.Mode,
		"until":  retention.RetainUntilDate,
	}).Info("Applied default bucket retention")
}

// applyLegalHold applies legal hold from request headers (Veeam compatibility)
func (h *Handler) applyLegalHold(
	r *http.Request,
	bucketPath string,
	bucketName string,
	objectKey string,
	legalHoldStatus string,
) {
	if legalHoldStatus != "ON" {
		return
	}

	legalHold := &object.LegalHoldConfig{Status: "ON"}
	if setErr := h.objectManager.SetObjectLegalHold(r.Context(), bucketPath, objectKey, legalHold); setErr != nil {
		logrus.WithError(setErr).Warn("Failed to set legal hold from headers")
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Info("Applied legal hold from headers")
}

// applyObjectCannedACLHeader applies a canned ACL value to an object.
// The authenticated user is resolved from ctx for the owner field.
// Called after successful PutObject and CompleteMultipartUpload when x-amz-acl is present.
func (h *Handler) applyObjectCannedACLHeader(ctx context.Context, bucketPath, objectKey, cannedACL string) {
	if !acl.IsValidCannedACL(cannedACL) {
		logrus.WithFields(logrus.Fields{
			"bucketPath": bucketPath,
			"object":     objectKey,
			"acl":        cannedACL,
		}).Warn("applyObjectCannedACLHeader: invalid canned ACL value, skipping")
		return
	}

	ownerID, ownerDisplayName := "maxiofs", "MaxIOFS"
	if user, ok := auth.GetUserFromContext(ctx); ok && user != nil {
		ownerID = user.ID
		ownerDisplayName = user.Username
	}

	grants := acl.GetCannedACLGrants(cannedACL, ownerID, ownerDisplayName)
	if grants == nil {
		return
	}

	objectGrants := make([]object.Grant, len(grants))
	for i, g := range grants {
		objectGrants[i] = object.Grant{
			Grantee: object.Grantee{
				Type:         string(g.Grantee.Type),
				ID:           g.Grantee.ID,
				DisplayName:  g.Grantee.DisplayName,
				EmailAddress: g.Grantee.EmailAddress,
				URI:          g.Grantee.URI,
			},
			Permission: string(g.Permission),
		}
	}

	aclData := &object.ACL{
		Owner:  object.Owner{ID: ownerID, DisplayName: ownerDisplayName},
		Grants: objectGrants,
	}

	if err := h.objectManager.SetObjectACL(ctx, bucketPath, objectKey, aclData); err != nil {
		logrus.WithFields(logrus.Fields{
			"bucketPath": bucketPath,
			"object":     objectKey,
			"acl":        cannedACL,
		}).WithError(err).Warn("applyObjectCannedACLHeader: failed to apply canned ACL")
	}
}

// setPutObjectResponseHeaders sets response headers for PutObject operation
func (h *Handler) setPutObjectResponseHeaders(w http.ResponseWriter, obj *object.Object) {
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))

	if obj.VersionID != "" {
		w.Header().Set("x-amz-version-id", obj.VersionID)
	}

	if obj.ChecksumAlgorithm != "" && obj.ChecksumValue != "" {
		w.Header().Set("x-amz-checksum-algorithm", obj.ChecksumAlgorithm)
		w.Header().Set("x-amz-checksum-"+strings.ToLower(obj.ChecksumAlgorithm), obj.ChecksumValue)
	}

	if obj.SSEAlgorithm != "" {
		w.Header().Set("x-amz-server-side-encryption", obj.SSEAlgorithm)
	}
}


// validateHeadBucketReadPermission checks if user has READ permission on bucket (not object)
// Similar to validateBucketReadPermission but without presigned URL or share support
func (h *Handler) validateHeadBucketReadPermission(
	w http.ResponseWriter,
	r *http.Request,
	user *auth.User,
	userExists bool,
	tenantID string,
	bucketName string,
	objectKey string,
) bool {
	if !userExists {
		// Unauthenticated access - check if BUCKET is public
		if h.checkPublicBucketAccess(r.Context(), tenantID, bucketName, acl.PermissionRead) {
			return true
		}

		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"object": objectKey,
		}).Warn("Public access denied for HeadObject - bucket not publicly readable")
		h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
		return false
	}

	action := auth.ActionGetObject
	if r.URL.Query().Get("versionId") != "" {
		action = auth.ActionGetObjectVersion
	}

	if h.userCanPerformS3ActionInTenant(r.Context(), user, tenantID, action, objectARN(bucketName, objectKey)) {
		return true
	}

	// No ACL cascade for an authenticated caller — see validateBucketReadPermission.
	// ACLs and public access decide anonymous requests, in the branch above.

	logrus.WithFields(logrus.Fields{
		"bucket":       bucketName,
		"object":       objectKey,
		"userID":       user.ID,
		"userTenantID": user.TenantID,
		"bucketTenant": tenantID,
	}).Warn("ACL permission denied for HeadObject - cross-tenant access (bucket-level)")
	h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
	return false
}

// setHeadObjectResponseHeaders sets all response headers for HeadObject operation (metadata only, no body)
func (h *Handler) setHeadObjectResponseHeaders(w http.ResponseWriter, obj *object.Object) {
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("x-amz-storage-class", storageClassOrStandard(obj.StorageClass))

	// S3 system response headers stored at upload time
	if obj.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", obj.ContentDisposition)
	}
	if obj.ContentEncoding != "" {
		w.Header().Set("Content-Encoding", obj.ContentEncoding)
	}
	if obj.CacheControl != "" {
		w.Header().Set("Cache-Control", obj.CacheControl)
	}
	if obj.ContentLanguage != "" {
		w.Header().Set("Content-Language", obj.ContentLanguage)
	}

	// User-defined metadata (x-amz-meta-*)
	for k, v := range obj.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}

	// Tag count — returned when object has tags
	if obj.Tags != nil && len(obj.Tags.Tags) > 0 {
		w.Header().Set("x-amz-tag-count", strconv.Itoa(len(obj.Tags.Tags)))
	}

	if obj.VersionID != "" {
		w.Header().Set("x-amz-version-id", obj.VersionID)
	}

	// Object Lock headers (Veeam compatibility)
	if obj.Retention != nil {
		w.Header().Set("x-amz-object-lock-mode", obj.Retention.Mode)
		w.Header().Set("x-amz-object-lock-retain-until-date", obj.Retention.RetainUntilDate.UTC().Format(time.RFC3339))
	}

	if obj.LegalHold != nil && obj.LegalHold.Status == "ON" {
		w.Header().Set("x-amz-object-lock-legal-hold", "ON")
	}

	if obj.ChecksumAlgorithm != "" && obj.ChecksumValue != "" {
		w.Header().Set("x-amz-checksum-algorithm", obj.ChecksumAlgorithm)
		w.Header().Set("x-amz-checksum-"+strings.ToLower(obj.ChecksumAlgorithm), obj.ChecksumValue)
	}

	if obj.SSEAlgorithm != "" {
		w.Header().Set("x-amz-server-side-encryption", obj.SSEAlgorithm)
	}

	// Restore status (S3 Glacier restore)
	if obj.RestoreStatus == "ongoing" {
		w.Header().Set("x-amz-restore", `ongoing-request="true"`)
	} else if obj.RestoreStatus == "restored" && obj.RestoreExpiresAt != nil {
		w.Header().Set("x-amz-restore",
			fmt.Sprintf(`ongoing-request="false", expiry-date="%s"`, obj.RestoreExpiresAt.UTC().Format(http.TimeFormat)))
	}
}



// parseObjectLockConfigXML reads and parses Object Lock configuration XML from request body
func (h *Handler) parseObjectLockConfigXML(
	w http.ResponseWriter,
	r *http.Request,
	bucketName string,
) (*ObjectLockConfiguration, bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logrus.WithError(err).Error("PutObjectLockConfiguration - Failed to read request body")
		h.writeError(w, "InvalidRequest", "Failed to read request body", bucketName, r)
		return nil, false
	}
	defer r.Body.Close()

	var config ObjectLockConfiguration
	if err := xml.Unmarshal(body, &config); err != nil {
		logrus.WithError(err).Error("PutObjectLockConfiguration - Failed to unmarshal XML")
		h.writeError(w, "MalformedXML", "The XML you provided was not well-formed", bucketName, r)
		return nil, false
	}

	return &config, true
}

// calculateRetentionDays converts years/days to total days
func calculateRetentionDays(years int, days int) int {
	if years > 0 {
		return years * 365
	}
	return days
}

// updateBucketRetentionConfig updates bucket's Object Lock retention configuration
func updateBucketRetentionConfig(
	bucketInfo *bucket.Bucket,
	newConfig *ObjectLockConfiguration,
) {
	// Initialize structures if needed
	if bucketInfo.ObjectLock.Rule == nil {
		bucketInfo.ObjectLock.Rule = &bucket.ObjectLockRule{}
	}
	if bucketInfo.ObjectLock.Rule.DefaultRetention == nil {
		bucketInfo.ObjectLock.Rule.DefaultRetention = &bucket.DefaultRetention{}
	}

	// Update mode
	bucketInfo.ObjectLock.Rule.DefaultRetention.Mode = newConfig.Rule.DefaultRetention.Mode

	// Update retention period (years or days)
	if newConfig.Rule.DefaultRetention.Years > 0 {
		years := newConfig.Rule.DefaultRetention.Years
		bucketInfo.ObjectLock.Rule.DefaultRetention.Years = &years
		bucketInfo.ObjectLock.Rule.DefaultRetention.Days = nil
	} else {
		days := newConfig.Rule.DefaultRetention.Days
		bucketInfo.ObjectLock.Rule.DefaultRetention.Days = &days
		bucketInfo.ObjectLock.Rule.DefaultRetention.Years = nil
	}
}

// storageClassOrStandard returns sc if non-empty, otherwise "STANDARD".
func storageClassOrStandard(sc string) string {
	if sc == "" {
		return "STANDARD"
	}
	return sc
}


// RestoreObject handles POST /{bucket}/{object}?restore.
func (h *Handler) RestoreObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := getObjectKey(r)

	if !h.requireObjectS3Action(w, r, bucketName, objectKey, auth.ActionRestoreObject) {
		return
	}
	tenantID := h.resolveBucketTenantID(r, bucketName)
	bucketPath := h.resolveBucketPath(r, bucketName, "")
	versionID := r.URL.Query().Get("versionId")

	// Parse the RestoreRequest XML (Days field is what matters)
	type glacierJobParams struct {
		Tier string `xml:"Tier"`
	}
	type restoreRequestXML struct {
		Days             int               `xml:"Days"`
		GlacierJobParams *glacierJobParams `xml:"GlacierJobParameters"`
	}
	var req restoreRequestXML
	if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, "MalformedXML", "The XML you provided was not well-formed", bucketName, r)
		return
	}

	days := req.Days
	if days <= 0 {
		days = 1 // default to 1 day if not specified
	}

	// Verify the object exists
	var obj *object.Object
	var err error
	if versionID != "" {
		var reader io.ReadCloser
		obj, reader, err = h.objectManager.GetObject(r.Context(), bucketPath, objectKey, versionID)
		if reader != nil {
			reader.Close()
		}
	} else {
		obj, err = h.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey)
	}
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// If already restored and not expired, return 409 RestoreAlreadyInProgress
	if obj.RestoreStatus == "ongoing" ||
		(obj.RestoreStatus == "restored" && obj.RestoreExpiresAt != nil && obj.RestoreExpiresAt.After(time.Now())) {
		h.writeError(w, "RestoreAlreadyInProgress",
			"Object restore is already in progress", objectKey, r)
		return
	}

	// Mark as restored with expiry = now + days
	expiresAt := time.Now().UTC().AddDate(0, 0, days)
	if setErr := h.objectManager.SetRestoreStatus(r.Context(), bucketPath, objectKey, "restored", &expiresAt, versionID); setErr != nil {
		h.writeError(w, "InternalError", setErr.Error(), objectKey, r)
		return
	}

	_ = tenantID // used for future per-tenant restore quota tracking
	w.WriteHeader(http.StatusOK)
}

// GetObjectTorrent handles GET /{bucket}/{object}?torrent.
// BitTorrent manifests are not implemented. Returns 501 NotImplemented.
func (h *Handler) GetObjectTorrent(w http.ResponseWriter, r *http.Request) {
	objectKey := getObjectKey(r)
	h.writeError(w, "NotImplemented", "GetObjectTorrent is not supported", objectKey, r)
}

// userCanPerformS3Action asks the user's policies whether they allow a specific
func bucketARN(bucketName string) string {
	return "arn:aws:s3:::" + bucketName
}

func objectARN(bucketName, objectKey string) string {
	return bucketARN(bucketName) + "/" + objectKey
}

func (h *Handler) requireBucketS3Action(w http.ResponseWriter, r *http.Request, bucketName, action string) bool {
	user, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
		return false
	}
	tenantID := h.resolveBucketTenantID(r, bucketName)
	if h.userCanPerformS3ActionInTenant(r.Context(), user, tenantID, action, bucketARN(bucketName)) {
		return true
	}
	h.writeError(w, "AccessDenied", "Access Denied", bucketName, r)
	return false
}

func (h *Handler) requireObjectS3Action(w http.ResponseWriter, r *http.Request, bucketName, objectKey, action string) bool {
	return h.requireObjectS3ActionOnVersion(w, r, bucketName, objectKey, action,
		r.URL.Query().Get("versionId"))
}

// requireObjectS3ActionOnVersion is the same check with the version stated
func (h *Handler) requireObjectS3ActionOnVersion(w http.ResponseWriter, r *http.Request, bucketName, objectKey, action, versionID string) bool {
	user, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
		return false
	}
	tenantID := h.resolveBucketTenantID(r, bucketName)
	resource := objectARN(bucketName, objectKey)

	if versionID != "" && action != auth.ActionGetObjectVersion &&
		action != auth.ActionDeleteObjectVersion &&
		!h.userCanPerformS3ActionInTenant(r.Context(), user, tenantID,
			auth.ActionGetObjectVersion, resource) {
		h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
		return false
	}

	if h.userCanPerformS3ActionInTenant(r.Context(), user, tenantID, action, resource) {
		return true
	}
	h.writeError(w, "AccessDenied", "Access Denied", objectKey, r)
	return false
}

// requireUploadTarget asserts that a multipart upload is the one the request
func (h *Handler) requireUploadTarget(w http.ResponseWriter, r *http.Request, uploadID, bucketPath, objectKey string) bool {
	if h.uploadMatchesTarget(r, uploadID, bucketPath, objectKey) {
		return true
	}
	h.writeError(w, "NoSuchUpload", "The specified upload does not exist", objectKey, r)
	return false
}

// uploadMatchesTarget is the same question without writing a response, for
func (h *Handler) uploadMatchesTarget(r *http.Request, uploadID, bucketPath, objectKey string) bool {
	if h.metadataStore == nil {
		return false
	}

	upload, err := h.metadataStore.GetMultipartUpload(r.Context(), uploadID)
	if err != nil || upload == nil {
		return false
	}

	if upload.Bucket != bucketPath || upload.Key != objectKey {
		logrus.WithFields(logrus.Fields{
			"uploadId":      uploadID,
			"requestBucket": bucketPath,
			"requestKey":    objectKey,
			"uploadBucket":  upload.Bucket,
			"uploadKey":     upload.Key,
		}).Warn("Multipart upload does not belong to the requested object")
		return false
	}

	return true
}

// BucketOwnerRecorder is how ownership reaches the permission model.
type BucketOwnerRecorder interface {
	GrantBucketOwnerPolicy(bucketName, ownerType, ownerID string) error
	RevokeBucketPolicies(bucketName string) ([]auth.InlinePolicyRef, error)
}

// policySetFor returns the permissions resolved for this request, or resolves
// them if the request never carried a set.
func (h *Handler) policySetFor(ctx context.Context, user *auth.User) *auth.PolicySet {
	if h.authManager == nil || user == nil {
		return nil
	}

	if set, ok := auth.PolicySetFromContext(ctx); ok && set.UserID == user.ID {
		return set
	}

	resolver, ok := h.authManager.(interface {
		ResolvePolicySet(context.Context, *auth.User) (*auth.PolicySet, error)
	})
	if !ok {
		return nil
	}

	set, err := resolver.ResolvePolicySet(ctx, user)
	if err != nil {
		return nil
	}
	return set
}

// userCanPerformS3Action answers for a resource that belongs to no tenant: the
// shared namespace, or a permission that names no bucket.
func (h *Handler) userCanPerformS3Action(ctx context.Context, user *auth.User, action, resource string) bool {
	return h.policySetFor(ctx, user).AllowsOwnAccount(action, resource)
}

// userCanPerformS3ActionInTenant answers the same question inside the tenant
func (h *Handler) userCanPerformS3ActionAnywhere(ctx context.Context, user *auth.User, action string) bool {
	if h.authManager == nil || user == nil {
		return false
	}

	if set, ok := auth.PolicySetFromContext(ctx); ok && set.UserID == user.ID {
		return set.AllowsAnywhere(action)
	}

	resolver, ok := h.authManager.(interface {
		ResolvePolicySet(context.Context, *auth.User) (*auth.PolicySet, error)
	})
	if !ok {
		return false
	}
	set, err := resolver.ResolvePolicySet(ctx, user)
	return err == nil && set.AllowsAnywhere(action)
}

func (h *Handler) userCanPerformS3ActionInTenant(ctx context.Context, user *auth.User, bucketTenantID, action, resource string) bool {
	return h.policySetFor(ctx, user).Allows(auth.AccessRequest{
		Action:   action,
		Resource: resource,
		Owner:    auth.OwnedBy(bucketTenantID),
	})
}
