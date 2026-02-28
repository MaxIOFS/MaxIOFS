package auth

import "context"

// contextKey type for SigV4-related context values (avoids collisions with string keys)
type sigv4ContextKey string

const (
	// OriginalSigV4PathKey is the context key for the original request path
	// before virtual-hosted-style to path-style rewriting. When present, it must
	// be used when building the canonical request for SigV4 verification, because
	// the client signed using this path (bucket in host, not in path).
	OriginalSigV4PathKey sigv4ContextKey = "original_sigv4_path"
)

// WithOriginalSigV4Path stores the original path in the request context.
// Call this before rewriting the URL path (e.g. virtual-hosted-style → path-style).
func WithOriginalSigV4Path(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, OriginalSigV4PathKey, path)
}

// OriginalSigV4PathFromContext returns the original path if it was stored for SigV4 verification.
func OriginalSigV4PathFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(OriginalSigV4PathKey).(string)
	return v, ok
}
