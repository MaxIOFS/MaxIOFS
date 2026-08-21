// Package clusterauth holds the one definition of what an inter-node request
// signature covers. Signer and verifier used to build the payload separately —
// twice over, in two different formats — which is how the two drift apart.
package clusterauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// EmptyBodyHash is the SHA-256 of no bytes, used when a request carries no body.
const EmptyBodyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// UnsignedPayload marks a streamed body whose digest is not known in advance.
// It is itself covered by the signature, so a request signed with a real digest
// cannot be downgraded to an unsigned one.
const UnsignedPayload = "UNSIGNED-PAYLOAD"

// Request is everything a signature binds.
//
// Query is separate from Path because the internal API carries the bucket, the
// key and the version there. User, Tenant and Roles are the identity a node
// forwards on behalf of a client; they are empty for node-to-node calls, which
// act as the node itself.
type Request struct {
	NodeID    string
	Timestamp string
	Nonce     string
	Method    string
	Path      string
	Query     string
	BodyHash  string

	User   string
	Tenant string
	Roles  string
}

// Payload is the exact string the signature covers.
func (r Request) Payload() string {
	return strings.Join([]string{
		"maxiofs-cluster-v1",
		r.NodeID,
		r.Timestamp,
		r.Nonce,
		r.Method,
		r.Path,
		r.Query,
		r.BodyHash,
		r.User,
		r.Tenant,
		r.Roles,
	}, "\n")
}

// Sign returns the hex-encoded HMAC-SHA256 of the request under the given key.
func Sign(key string, r Request) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(r.Payload()))
	return hex.EncodeToString(h.Sum(nil))
}

// Equal compares two signatures without leaking their contents through timing.
func Equal(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

// NewNonce returns a random nonce. A nonce only has to be unique within the
// window a signature is accepted for.
func NewNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Never observed in practice; a duplicate here costs a refused request,
		// not an accepted replay, because the cache rejects what it has seen.
		return ""
	}
	return hex.EncodeToString(b)
}
