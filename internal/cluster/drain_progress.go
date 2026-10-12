package cluster

import (
	"context"
	"slices"
	"sync"
	"time"
)

// drainProgress follows the move of this node's objects while it is drained.
type drainProgress struct {
	mu sync.Mutex
	p  *DrainProgress
}

// started reports whether the move was counted.
func (d *drainProgress) started() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.p != nil
}

// begin starts following a move of objects that hold bytes.
func (d *drainProgress) begin(objects, bytes int64) {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.p = &DrainProgress{StartedAt: now, TotalObjects: objects, TotalBytes: bytes,
		RemainingObjects: objects, RemainingBytes: bytes, UpdatedAt: now}
}

// moved counts one object of bytes moved off this node.
func (d *drainProgress) moved(bytes int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.p == nil {
		return
	}
	d.p.RemainingObjects = max(d.p.RemainingObjects-1, 0)
	d.p.RemainingBytes = max(d.p.RemainingBytes-bytes, 0)
	d.p.UpdatedAt = time.Now()
}

// settle sets what a whole pass found this node still holds.
func (d *drainProgress) settle(objects, bytes int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.p == nil {
		return
	}
	d.p.RemainingObjects, d.p.RemainingBytes = objects, bytes
	d.p.TotalObjects = max(d.p.TotalObjects, objects)
	d.p.TotalBytes = max(d.p.TotalBytes, bytes)
	d.p.UpdatedAt = time.Now()
}

// reset stops following the move.
func (d *drainProgress) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.p = nil
}

// snapshot is the progress now, with the time left at the pace so far.
func (d *drainProgress) snapshot() *DrainProgress {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.p == nil {
		return nil
	}
	p := *d.p
	p.EtaSeconds = eta(p.TotalBytes-p.RemainingBytes, p.RemainingBytes, time.Since(p.StartedAt))
	if p.TotalBytes == 0 {
		p.EtaSeconds = eta(p.TotalObjects-p.RemainingObjects, p.RemainingObjects, time.Since(p.StartedAt))
	}
	return &p
}

// eta is the time to move left at the pace of done in elapsed, rounded up to
// the second; 0 while nothing is done.
func eta(done, left int64, elapsed time.Duration) int64 {
	if done <= 0 || left <= 0 {
		return 0
	}
	return int64(elapsed.Seconds()*float64(left)/float64(done)) + 1
}

// DrainProgress is how far this node, drained, is in moving its objects; nil
// when it is not drained or has not counted them yet.
func (w *HASyncWorker) DrainProgress() *DrainProgress {
	return w.drain.snapshot()
}

// heldHere counts the writes that name this node and the bytes they hold.
func (w *HASyncWorker) heldHere(ctx context.Context, localID string) (objects, bytes int64, err error) {
	err = w.eachPage(ctx, func(ctx context.Context, bucket string, keys []string) error {
		for _, key := range keys {
			writes, err := keyWrites(ctx, w.objMgr, bucket, key)
			if err != nil {
				return err
			}
			for _, v := range writes {
				if !v.IsDeleteMarker && slices.Contains(v.Locations, localID) {
					objects++
					bytes += v.Size
				}
			}
		}
		return nil
	})
	return objects, bytes, err
}
