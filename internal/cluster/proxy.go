package cluster

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// ProxyClient handles proxying S3 requests to remote nodes
type ProxyClient struct {
	httpClient *http.Client
	log        *logrus.Entry
}

// NewProxyClient creates a new proxy client
func NewProxyClient() *ProxyClient {
	return &ProxyClient{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		log: logrus.WithField("component", "cluster-proxy"),
	}
}

// ProxyRequest proxies an HTTP request to a remote node
func (p *ProxyClient) ProxyRequest(ctx context.Context, node *Node, originalReq *http.Request) (*http.Response, error) {
	// Build remote URL
	remoteURL := fmt.Sprintf("%s%s", node.Endpoint, originalReq.URL.RequestURI())

	p.log.WithFields(logrus.Fields{
		"target_node": node.Name,
		"target_url":  remoteURL,
		"method":      originalReq.Method,
	}).Debug("Proxying request to remote node")

	// Create new request to remote node
	proxyReq, err := http.NewRequestWithContext(ctx, originalReq.Method, remoteURL, originalReq.Body)
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
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())

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
