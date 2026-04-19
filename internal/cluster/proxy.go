package cluster

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// ProxyClient handles proxying S3 requests to remote nodes
type ProxyClient struct {
	httpClient *http.Client
	log        *logrus.Entry
}

// NewProxyClient creates a new proxy client.
// If tlsConfig is non-nil, inter-node requests use TLS with that config.
func NewProxyClient(tlsConfig *tls.Config) *ProxyClient {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig
	}
	return &ProxyClient{
		httpClient: &http.Client{
			Timeout:   60 * time.Second,
			Transport: transport,
		},
		log: logrus.WithField("component", "cluster-proxy"),
	}
}

// ProxyRequest proxies an HTTP request to a remote node.
// If the incoming request already carries the X-MaxIOFS-Proxied header, the call is rejected
// to prevent infinite proxy loops (A→B→A→...).
func (p *ProxyClient) ProxyRequest(ctx context.Context, node *Node, originalReq *http.Request) (*http.Response, error) {
	// Guard against proxy loops: if this request was already forwarded by another node, refuse to
	// forward it again. The caller must handle it locally or return an error to the client.
	if originalReq.Header.Get("X-MaxIOFS-Proxied") == "true" {
		return nil, fmt.Errorf("refusing to proxy an already-proxied request (loop prevention): %s %s",
			originalReq.Method, originalReq.URL.RequestURI())
	}

	// Build remote URL
	remoteURL := fmt.Sprintf("%s%s", node.Endpoint, originalReq.URL.RequestURI())

	p.log.WithFields(logrus.Fields{
		"target_node": node.Name,
		"target_url":  remoteURL,
		"method":      originalReq.Method,
	}).Debug("Proxying request to remote node")

	// Buffer the original request body so it can be read by the proxy request
	// (originalReq.Body is single-read; if already consumed upstream, it would be empty)
	var bodyReader io.Reader
	if originalReq.Body != nil {
		bodyBytes, err := io.ReadAll(originalReq.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		originalReq.Body.Close()
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Create new request to remote node
	proxyReq, err := http.NewRequestWithContext(ctx, originalReq.Method, remoteURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy request: %w", err)
	}

	// Copy headers from original request
	copyHeaders(proxyReq.Header, originalReq.Header)

	// Add custom header to indicate this is a proxied request
	proxyReq.Header.Set("X-MaxIOFS-Proxied", "true")
	proxyReq.Header.Set("X-MaxIOFS-Proxy-Node", node.ID)

	// Execute request
	startTime := time.Now()
	resp, err := p.httpClient.Do(proxyReq)
	duration := time.Since(startTime)

	if err != nil {
		p.log.WithFields(logrus.Fields{
			"target_node": node.Name,
			"error":       err.Error(),
			"duration_ms": duration.Milliseconds(),
		}).Error("Failed to proxy request")
		return nil, fmt.Errorf("failed to proxy request to %s: %w", node.Name, err)
	}

	p.log.WithFields(logrus.Fields{
		"target_node": node.Name,
		"status_code": resp.StatusCode,
		"duration_ms": duration.Milliseconds(),
	}).Debug("Proxy request completed")

	return resp, nil
}

// CopyResponseToWriter copies the proxied response back to the original response writer
func (p *ProxyClient) CopyResponseToWriter(w http.ResponseWriter, resp *http.Response) error {
	// Copy headers
	copyHeaders(w.Header(), resp.Header)

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Copy body
	_, err := io.Copy(w, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to copy response body: %w", err)
	}

	return nil
}

// ProxyAndWrite proxies a request and writes the response in one call
func (p *ProxyClient) ProxyAndWrite(ctx context.Context, w http.ResponseWriter, r *http.Request, node *Node) error {
	// Proxy the request
	resp, err := p.ProxyRequest(ctx, node, r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Copy response to writer
	return p.CopyResponseToWriter(w, resp)
}

// copyHeaders copies HTTP headers from src to dst
func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		// Skip hop-by-hop headers
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// isHopByHopHeader checks if a header is hop-by-hop (should not be forwarded)
func isHopByHopHeader(header string) bool {
	hopByHopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	}

	for _, h := range hopByHopHeaders {
		if header == h {
			return true
		}
	}
	return false
}

// SignClusterRequest adds HMAC authentication headers to a cluster replication request
// This is used when making authenticated requests to other nodes for replication
func (p *ProxyClient) SignClusterRequest(req *http.Request, localNodeID, nodeToken string) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	// Generate a cryptographically secure random nonce
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		// Fallback — still better than pure UnixNano due to timestamp addition
		nonce := fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixMicro())
		p.signWithNonce(req, localNodeID, nodeToken, timestamp, nonce)
		return
	}
	nonce := hex.EncodeToString(nonceBytes)
	p.signWithNonce(req, localNodeID, nodeToken, timestamp, nonce)
}

func (p *ProxyClient) signWithNonce(req *http.Request, localNodeID, nodeToken, timestamp, nonce string) {
	// Compute signature: HMAC-SHA256(nodeToken, method + path + timestamp + nonce)
	payload := fmt.Sprintf("%s\n%s\n%s\n%s", req.Method, req.URL.Path, timestamp, nonce)
	h := hmac.New(sha256.New, []byte(nodeToken))
	h.Write([]byte(payload))
	signature := hex.EncodeToString(h.Sum(nil))

	// Add authentication headers
	req.Header.Set("X-MaxIOFS-Node-ID", localNodeID)
	req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
	req.Header.Set("X-MaxIOFS-Nonce", nonce)
	req.Header.Set("X-MaxIOFS-Signature", signature)

	p.log.WithFields(logrus.Fields{
		"node_id":   localNodeID,
		"method":    req.Method,
		"path":      req.URL.Path,
		"timestamp": timestamp,
	}).Debug("Signed cluster request")
}

// CreateAuthenticatedRequest creates a new HTTP request with HMAC authentication headers
// This is a convenience method for cluster replication operations
func (p *ProxyClient) CreateAuthenticatedRequest(ctx context.Context, method, url string, body io.Reader, localNodeID, nodeToken string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Sign the request
	p.SignClusterRequest(req, localNodeID, nodeToken)

	return req, nil
}

// DoAuthenticatedRequest executes an authenticated cluster request and returns the response
func (p *ProxyClient) DoAuthenticatedRequest(req *http.Request) (*http.Response, error) {
	startTime := time.Now()
	resp, err := p.httpClient.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		p.log.WithFields(logrus.Fields{
			"url":         req.URL.String(),
			"method":      req.Method,
			"error":       err.Error(),
			"duration_ms": duration.Milliseconds(),
		}).Error("Authenticated request failed")
		return nil, fmt.Errorf("request failed: %w", err)
	}

	p.log.WithFields(logrus.Fields{
		"url":         req.URL.String(),
		"method":      req.Method,
		"status_code": resp.StatusCode,
		"duration_ms": duration.Milliseconds(),
	}).Debug("Authenticated request completed")

	return resp, nil
}

// AddClusterProxyHeaders adds inter-node proxy authentication headers to an outgoing S3 request.
// It replaces the original Authorization header (which was SigV4-signed for the client's host)
// with cluster HMAC auth so the target node can validate the request without re-verifying SigV4.
//
// Parameters:
//   - req: the outgoing request to be modified
//   - nodeID: local node's ID
//   - clusterToken: local node's cluster token (used as HMAC key)
//   - userID, tenantID, roles: forwarded user context
func AddClusterProxyHeaders(req *http.Request, nodeID, clusterToken, userID, tenantID, roles string) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	// Compute HMAC-SHA256(clusterToken, "proxy:{nodeID}:{timestamp}")
	payload := fmt.Sprintf("proxy:%s:%s", nodeID, timestamp)
	h := hmac.New(sha256.New, []byte(clusterToken))
	h.Write([]byte(payload))
	sig := hex.EncodeToString(h.Sum(nil))

	// Remove original Authorization (SigV4 was signed for the client's host — invalid on the target node)
	req.Header.Del("Authorization")

	req.Header.Set("X-MaxIOFS-Node-ID", nodeID)
	req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
	req.Header.Set("X-MaxIOFS-Node-Hmac", sig)
	req.Header.Set("X-MaxIOFS-Forwarded-User", userID)
	req.Header.Set("X-MaxIOFS-Forwarded-Tenant", tenantID)
	req.Header.Set("X-MaxIOFS-Forwarded-Roles", roles)
}

// ValidateClusterProxyAuth validates inter-node proxy authentication headers.
// Returns the forwarded user context if valid, or ok=false if validation fails.
//
// Validation checks:
//   - X-MaxIOFS-Proxied: true header present
//   - HMAC-SHA256(clusterToken, "proxy:{nodeID}:{timestamp}") matches X-MaxIOFS-Node-Hmac
//   - Timestamp within ±5 minutes of current time
func ValidateClusterProxyAuth(req *http.Request, clusterToken string) (userID, tenantID, roles string, ok bool) {
	if req.Header.Get("X-MaxIOFS-Proxied") != "true" {
		return "", "", "", false
	}

	nodeID := req.Header.Get("X-MaxIOFS-Node-ID")
	timestamp := req.Header.Get("X-MaxIOFS-Timestamp")
	providedSig := req.Header.Get("X-MaxIOFS-Node-Hmac")

	if nodeID == "" || timestamp == "" || providedSig == "" {
		return "", "", "", false
	}

	// Validate timestamp (within 5 minutes)
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return "", "", "", false
	}
	diff := time.Now().Unix() - ts
	if diff < 0 {
		diff = -diff
	}
	if diff > int64(5*time.Minute.Seconds()) {
		return "", "", "", false
	}

	// Validate HMAC
	payload := fmt.Sprintf("proxy:%s:%s", nodeID, timestamp)
	h := hmac.New(sha256.New, []byte(clusterToken))
	h.Write([]byte(payload))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
		return "", "", "", false
	}

	return req.Header.Get("X-MaxIOFS-Forwarded-User"),
		req.Header.Get("X-MaxIOFS-Forwarded-Tenant"),
		req.Header.Get("X-MaxIOFS-Forwarded-Roles"),
		true
}

// buildInternalS3URL derives the inter-node S3 API URL from node.Endpoint.
// node.Endpoint is always https://<internal-ip>:<clusterPort> (IP validated at cluster init).
// We extract the IP and build http://<ip>:<s3Port> to avoid using node.APIURL's public
// hostname, which may carry a certificate not signed by the cluster CA.
// s3Port is taken from node.APIURL's explicit port if set (and not a default port), otherwise 8080.
func buildInternalS3URL(node *Node) string {
	if node.Endpoint == "" {
		return ""
	}
	endpointParsed, err := url.Parse(node.Endpoint)
	if err != nil || endpointParsed.Hostname() == "" {
		return ""
	}
	host := endpointParsed.Hostname()

	// Determine S3 port: prefer explicit port from node.APIURL (skip default 80/443), otherwise 8080.
	apiPort := "8080"
	if node.APIURL != "" {
		if apiParsed, err2 := url.Parse(node.APIURL); err2 == nil {
			if p := apiParsed.Port(); p != "" && p != "80" && p != "443" {
				apiPort = p
			}
		}
	}
	return "http://" + host + ":" + apiPort
}

// ProxyToNodeAPIURL proxies an S3 request to a remote node's S3 API URL.
// It adds cluster auth headers, derives the internal target URL from node.Endpoint
// (always an internal IP), and executes the request.
// Returns the raw HTTP response (caller must close Body).
func (p *ProxyClient) ProxyToNodeAPIURL(ctx context.Context, node *Node, originalReq *http.Request, nodeID, clusterToken, userID, tenantID, roles string) (*http.Response, error) {
	// Always derive the internal S3 URL from node.Endpoint (guaranteed internal IP).
	// node.APIURL is the public-facing URL and may use a public cert not trusted by the
	// cluster CA — using it for inter-node proxying causes x509 certificate errors.
	baseURL := buildInternalS3URL(node)
	if baseURL == "" {
		// Fallback for standalone/test scenarios without Endpoint.
		baseURL = node.APIURL
		if baseURL == "" {
			baseURL = node.Endpoint
		}
	}
	targetURL := fmt.Sprintf("%s%s", strings.TrimRight(baseURL, "/"), originalReq.URL.RequestURI())

	p.log.WithFields(logrus.Fields{
		"target_node": node.Name,
		"target_url":  targetURL,
		"method":      originalReq.Method,
	}).Debug("Proxying S3 request to remote node S3 API")

	// Buffer request body
	var bodyReader io.Reader
	if originalReq.Body != nil {
		bodyBytes, err := io.ReadAll(originalReq.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		originalReq.Body.Close()
		bodyReader = bytes.NewReader(bodyBytes)
	}

	proxyReq, err := http.NewRequestWithContext(ctx, originalReq.Method, targetURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy request: %w", err)
	}

	// Copy headers from original request
	copyHeaders(proxyReq.Header, originalReq.Header)

	// Mark as proxied (loop prevention)
	proxyReq.Header.Set("X-MaxIOFS-Proxied", "true")
	proxyReq.Header.Set("X-MaxIOFS-Proxy-Node", nodeID)

	// Add cluster auth headers (replaces Authorization)
	AddClusterProxyHeaders(proxyReq, nodeID, clusterToken, userID, tenantID, roles)

	resp, err := p.httpClient.Do(proxyReq)
	if err != nil {
		return nil, fmt.Errorf("failed to proxy request to %s: %w", node.Name, err)
	}

	return resp, nil
}
