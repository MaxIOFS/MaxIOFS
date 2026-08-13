package cluster

import (
	"context"
	"time"
)

// syncPushTimeout bounds an immediate push. Long enough for a slow peer to take
// a full table, short enough that a stuck one is not held for ever.
const syncPushTimeout = 2 * time.Minute

// detachedSyncContext returns a context that keeps the caller's values but not
// its cancellation, bounded by syncPushTimeout.
func detachedSyncContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), syncPushTimeout)
}

// runDetached performs work in the background under a detached context.
func runDetached(ctx context.Context, work func(context.Context)) {
	syncCtx, cancel := detachedSyncContext(ctx)
	go func() {
		defer cancel()
		work(syncCtx)
	}()
}
