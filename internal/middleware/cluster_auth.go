package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/maxiofs/maxiofs/internal/clusterauth"

	"github.com/sirupsen/logrus"
)

// emptyBodyHash is the hex-encoded SHA-256 of an empty byte slice.
// Used as the body hash for requests with no body (GET, DELETE, HEAD).
const emptyBodyHash = clusterauth.EmptyBodyHash

const (
	clusterBodySHA256Header    = "X-MaxIOFS-Body-SHA256"
	clusterUnsignedPayloadHash = "UNSIGNED-PAYLOAD"
	maxSignedClusterBody       = 8 << 20
	clusterMaxSkew             = 30 * time.Second
)

// readAndHashBody drains req.Body, restores it, and returns the hex SHA-256.
// Returns emptyBodyHash for nil bodies.
func readAndHashBody(req *http.Request) (string, error) {
	// A declared digest is signed in place of the body and enforced while the
	// body streams, so a large transfer is authenticated without buffering it.
	if declared := req.Header.Get(ClusterBodyDigestHeader); declared != "" {
		if req.Body != nil {
			verified, err := newVerifyingBody(req.Body, declared)
			if err != nil {
				return "", err
			}
			req.Body = verified
		}
		return declared, nil
	}
	if req.Header.Get(clusterBodySHA256Header) == clusterUnsignedPayloadHash {
		return clusterUnsignedPayloadHash, nil
	}
	if req.Body == nil {
		return emptyBodyHash, nil
	}
	data, err := io.ReadAll(io.LimitReader(req.Body, maxSignedClusterBody+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxSignedClusterBody {
		return "", fmt.Errorf("cluster request body too large")
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// ClusterAuthMiddleware provides HMAC-based authentication for cluster-internal endpoints
type ClusterAuthMiddleware struct {
	db     *sql.DB
	nonces *clusterauth.NonceCache
}

// NewClusterAuthMiddleware creates a new cluster authentication middleware
func NewClusterAuthMiddleware(db *sql.DB) *ClusterAuthMiddleware {
	return &ClusterAuthMiddleware{
		db:     db,
		nonces: clusterauth.NewNonceCache(clusterMaxSkew * 2),
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
		maxSkew := int64(clusterMaxSkew / time.Second) // inter-node clocks are NTP-synced
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
		bodyHash, err := readAndHashBody(r)
		if err != nil {
			logrus.WithError(err).WithField("node_id", nodeID).Warn("Cluster authentication failed: invalid body")
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		// Compute expected signature
		expectedSignature := clusterauth.Sign(nodeToken, clusterauth.Request{
			NodeID:    nodeID,
			Timestamp: timestamp,
			Nonce:     nonce,
			Method:    r.Method,
			Path:      r.URL.Path,
			Query:     r.URL.RawQuery,
			BodyHash:  bodyHash,
		})

		// Compare signatures (constant time to prevent timing attacks)
		if !clusterauth.Equal(signature, expectedSignature) {
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

		// A signature stays valid for the whole skew window, so the nonce is what
		// stops the same request being sent twice inside it.
		if !m.nonces.Observe(nodeID+"\n"+nonce, time.Now()) {
			logrus.WithFields(logrus.Fields{
				"node_id": nodeID,
				"method":  r.Method,
				"path":    r.URL.Path,
			}).Warn("Cluster authentication failed: nonce already used")
			http.Error(w, "Replayed request", http.StatusUnauthorized)
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
