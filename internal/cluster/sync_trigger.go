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

// runDetached performs work in the component's tracked background worker. The
// sync is detached from the caller's request cancellation, but still cancels
// when the component stops so shutdown can wait deterministically.
func runDetached(w *bgWorker, ctx context.Context, work func(context.Context)) {
	w.spawn(func() {
		syncCtx, cancel := detachedSyncContext(ctx)
		defer cancel()
		done := make(chan struct{})
		go func() {
			select {
			case <-w.stopped():
				cancel()
			case <-done:
			}
		}()
		defer close(done)
		work(syncCtx)
	})
}
