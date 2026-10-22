package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

// ErrChunkSignatureMismatch: a chunk of a signed aws-chunked body whose
// signature is not the one its bytes have.
var ErrChunkSignatureMismatch = errors.New("the signature of a chunk does not match its bytes")

// streamingSignedPayload is the x-amz-content-sha256 of a body sent in signed
// chunks, with or without signed trailers.
const streamingSignedPayload = "STREAMING-AWS4-HMAC-SHA256-PAYLOAD"

// emptySHA256 is the hex SHA-256 of nothing.
var emptySHA256 = hex.EncodeToString(func() []byte { s := sha256.Sum256(nil); return s[:] }())

// ChunkSigning checks the chunks of a body sent in signed chunks: the
// signature of each is chained from the one before it, the first from the
// request's.
type ChunkSigning struct {
	key      []byte
	amzDate  string
	scope    string
	previous string
}

// VerifyChunk checks the signature of the next chunk, data its bytes.
func (c *ChunkSigning) VerifyChunk(signature string, data []byte) error {
	sum := sha256.Sum256(data)
	return c.verify("AWS4-HMAC-SHA256-PAYLOAD", signature, emptySHA256+"\n"+hex.EncodeToString(sum[:]))
}

// VerifyTrailer checks the signature of the trailers, each "name:value\n".
func (c *ChunkSigning) VerifyTrailer(signature string, trailers []byte) error {
	sum := sha256.Sum256(trailers)
	return c.verify("AWS4-HMAC-SHA256-TRAILER", signature, hex.EncodeToString(sum[:]))
}

func (c *ChunkSigning) verify(algorithm, signature, hashes string) error {
	toSign := algorithm + "\n" + c.amzDate + "\n" + c.scope + "\n" + c.previous + "\n" + hashes
	want := hex.EncodeToString(hmacSHA256(c.key, []byte(toSign)))
	if !hmac.Equal([]byte(want), []byte(signature)) {
		return ErrChunkSignatureMismatch
	}
	c.previous = want
	return nil
}

// chunkSigningHolder takes the chunk signing of a request whose signature
// was found good, for the request that follows.
type chunkSigningHolder struct{ signing *ChunkSigning }

type chunkSigningHolderKey struct{}

type chunkSigningKey struct{}

// WithChunkSigning returns a context whose body is checked by signing.
func WithChunkSigning(ctx context.Context, signing *ChunkSigning) context.Context {
	return context.WithValue(ctx, chunkSigningKey{}, signing)
}

// ChunkSigningFrom is the chunk signing a request's body is checked by, when
// it is sent in signed chunks.
func ChunkSigningFrom(ctx context.Context) (*ChunkSigning, bool) {
	signing, ok := ctx.Value(chunkSigningKey{}).(*ChunkSigning)
	return signing, ok && signing != nil
}

// holdChunkSigning keeps the chunk signing of r, signed with signingKey and
// the request's signature, when its body is sent in signed chunks.
func holdChunkSigning(r *http.Request, sig *S3SignatureV4, signingKey []byte, signature string) {
	holder, ok := r.Context().Value(chunkSigningHolderKey{}).(*chunkSigningHolder)
	if !ok || !strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), streamingSignedPayload) {
		return
	}
	holder.signing = &ChunkSigning{
		key:      signingKey,
		amzDate:  r.Header.Get("X-Amz-Date"),
		scope:    sig.Date + "/" + sig.Region + "/" + sig.Service + "/aws4_request",
		previous: signature,
	}
}
