package middleware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// emptyBodyHash is the hex-encoded SHA-256 of an empty byte slice.
// Used as the body hash for requests with no body (GET, DELETE, HEAD).
const emptyBodyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// readAndHashBody drains req.Body, restores it, and returns the hex SHA-256.
// Returns emptyBodyHash for nil bodies.
func readAndHashBody(req *http.Request) string {
	if req.Body == nil {
		return emptyBodyHash
	}
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return emptyBodyHash
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

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
		maxSkew := int64(30) // 30 seconds — inter-node clocks are NTP-synced
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

		// Read and hash the body so it is covered by the HMAC, then restore it for downstream handlers.
		bodyHash := readAndHashBody(r)

		// Compute expected signature
		expectedSignature := computeSignature(nodeToken, r.Method, r.URL.Path, timestamp, nonce, bodyHash)

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

// computeSignature computes HMAC-SHA256 signature for cluster authentication.
// bodyHash must be the hex-encoded SHA-256 of the request body (use emptyBodyHash for empty/nil bodies).
func computeSignature(nodeToken, method, path, timestamp, nonce, bodyHash string) string {
	payload := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", method, path, timestamp, nonce, bodyHash)
	h := hmac.New(sha256.New, []byte(nodeToken))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

