package auth

import (
	"context"
	"sync"
)

// Outcome is how an S3 request was answered, for its log line: the error it
// was given and, when its authorization refused it, who asked for what and
// why it was refused.
type Outcome struct {
	Code, Message                  string
	User, Action, Resource, Reason string
}

type outcomeKey struct{}

type outcomeHolder struct {
	mu      sync.Mutex
	outcome Outcome
}

// WithOutcome gives ctx a place for the outcome of its request.
func WithOutcome(ctx context.Context) context.Context {
	return context.WithValue(ctx, outcomeKey{}, &outcomeHolder{})
}

func noteOutcome(ctx context.Context, note func(*Outcome)) {
	if h, ok := ctx.Value(outcomeKey{}).(*outcomeHolder); ok {
		h.mu.Lock()
		note(&h.outcome)
		h.mu.Unlock()
	}
}

// NoteError records the S3 error the request of ctx is answered.
func NoteError(ctx context.Context, code, message string) {
	noteOutcome(ctx, func(o *Outcome) { o.Code, o.Message = code, message })
}

// NoteRefusal records that the authorization of the request of ctx refused
// user action on resource, and why.
func NoteRefusal(ctx context.Context, user, action, resource, reason string) {
	noteOutcome(ctx, func(o *Outcome) { o.User, o.Action, o.Resource, o.Reason = user, action, resource, reason })
}

// OutcomeOf is what was recorded of the request of ctx.
func OutcomeOf(ctx context.Context) Outcome {
	h, ok := ctx.Value(outcomeKey{}).(*outcomeHolder)
	if !ok {
		return Outcome{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.outcome
}
