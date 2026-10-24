package object

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// PartsDigest is the digest of a completion's part list: the part numbers and
// ETags in order, the ETags without quotes. A completion sent again names the
// same list.
func PartsDigest(parts []Part) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:%s\n", p.PartNumber, strings.Trim(p.ETag, `"`))
	}
	return hex.EncodeToString(h.Sum(nil))
}

type writeConditionsKey struct{}

// WithWriteConditions carries a completion's If-Match and If-None-Match: the
// completion decides them under the key's lock, as a PUT does.
func WithWriteConditions(ctx context.Context, headers http.Header) context.Context {
	conditions := http.Header{}
	for _, name := range []string{"If-Match", "If-None-Match"} {
		if value := headers.Get(name); value != "" {
			conditions.Set(name, value)
		}
	}
	if len(conditions) == 0 {
		return ctx
	}
	return context.WithValue(ctx, writeConditionsKey{}, conditions)
}

func writeConditionsFrom(ctx context.Context) http.Header {
	conditions, _ := ctx.Value(writeConditionsKey{}).(http.Header)
	return conditions
}
