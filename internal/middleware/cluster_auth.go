package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// ClusterAuthMiddleware provides HMAC-based authentication for cluster-internal endpoints
type ClusterAuthMiddleware struct {
	db *sql.DB
}

// NewClusterAuthMiddleware creates a new cluster authentication middleware
func NewClusterAuthMiddleware(db *sql.DB) *ClusterAuthMiddleware {
	return &ClusterAuthMiddleware{
		db: db,
	}
}

// ClusterAuth is the middleware handler that validates HMAC signatures
func (m *ClusterAuthMiddleware) ClusterAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract authentication headers
		nodeID := r.Header.Get("X-MaxIOFS-Node-ID")
		timestamp := r.Header.Get("X-MaxIOFS-Timestamp")
		nonce := r.Header.Get("X-MaxIOFS-Nonce")
		signature := r.Header.Get("X-MaxIOFS-Signature")

		// Validate required headers
		if nodeID == "" || timestamp == "" || nonce == "" || signature == "" {
			logrus.WithFields(logrus.Fields{
				"node_id":   nodeID,
				"timestamp": timestamp,
				"nonce":     nonce,
				"signature": signature,
			}).Warn("Cluster authentication failed: missing headers")
			http.Error(w, "Missing authentication headers", http.StatusUnauthorized)
			return
		}

		// Validate timestamp (prevent replay attacks)
		ts, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			logrus.WithError(err).Warn("Cluster authentication failed: invalid timestamp")
			http.Error(w, "Invalid timestamp", http.StatusUnauthorized)
			return
		}

		now := time.Now().Unix()
		maxSkew := int64(300) // 5 minutes
		if ts < now-maxSkew || ts > now+maxSkew {
			logrus.WithFields(logrus.Fields{
				"timestamp": ts,
				"now":       now,
				"skew":      now - ts,
			}).Warn("Cluster authentication failed: timestamp skew too large")
			http.Error(w, "Timestamp skew too large", http.StatusUnauthorized)
			return
		}

		// Get node token from database
		nodeToken, err := m.getNodeToken(r.Context(), nodeID)
		if err != nil {
			logrus.WithError(err).WithField("node_id", nodeID).Warn("Cluster authentication failed: node not found")
			http.Error(w, "Node not found", http.StatusUnauthorized)
			return
		}

		// Compute expected signature
		expectedSignature := computeSignature(nodeToken, r.Method, r.URL.Path, timestamp, nonce)

		// Compare signatures (constant time to prevent timing attacks)
		if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
			logrus.WithFields(logrus.Fields{
				"node_id":  nodeID,
				"method":   r.Method,
				"path":     r.URL.Path,
				"expected": expectedSignature,
				"received": signature,
			}).Warn("Cluster authentication failed: signature mismatch")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}

		// Authentication successful - add node ID to context
		ctx := context.WithValue(r.Context(), "cluster_node_id", nodeID)
		r = r.WithContext(ctx)

		logrus.WithFields(logrus.Fields{
			"node_id": nodeID,
			"method":  r.Method,
			"path":    r.URL.Path,
		}).Debug("Cluster authentication successful")

		next.ServeHTTP(w, r)
	})
}

// getNodeToken retrieves the node_token for a given node ID
func (m *ClusterAuthMiddleware) getNodeToken(ctx context.Context, nodeID string) (string, error) {
	var nodeToken string
	query := `SELECT node_token FROM cluster_nodes WHERE id = ? AND health_status != 'removed'`
	err := m.db.QueryRowContext(ctx, query, nodeID).Scan(&nodeToken)
	if err != nil {
		return "", err
	}
	return nodeToken, nil
}

// computeSignature computes HMAC-SHA256 signature for cluster authentication
func computeSignature(nodeToken, method, path, timestamp, nonce string) string {
	// Signature payload: method + path + timestamp + nonce
	payload := fmt.Sprintf("%s\n%s\n%s\n%s", method, path, timestamp, nonce)

	// Compute HMAC-SHA256
	h := hmac.New(sha256.New, []byte(nodeToken))
	h.Write([]byte(payload))
	signature := hex.EncodeToString(h.Sum(nil))

	return signature
}

// SignRequest adds HMAC authentication headers to an outgoing request
// This is used by the cluster manager when making requests to other nodes
func SignRequest(req *http.Request, nodeID, nodeToken string) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := generateNonce()

	// Compute signature
	signature := computeSignature(nodeToken, req.Method, req.URL.Path, timestamp, nonce)

	// Add headers
	req.Header.Set("X-MaxIOFS-Node-ID", nodeID)
	req.Header.Set("X-MaxIOFS-Timestamp", timestamp)
	req.Header.Set("X-MaxIOFS-Nonce", nonce)
	req.Header.Set("X-MaxIOFS-Signature", signature)
}

// generateNonce generates a random nonce for request signatures
func generateNonce() string {
	// Use current timestamp + nanoseconds as nonce
	// This is sufficient for preventing replay attacks within the time window
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
