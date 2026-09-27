package object

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sync"

	"github.com/maxiofs/maxiofs/internal/metadata"
)

// quotaHold is the room a write in flight holds against its bucket quota and,
// for its bytes, against its tenant quota.
type quotaHold struct {
	bytes   int64
	objects int64
	checked chan struct{} // closed once the write is admitted or refused
}

// holdsAhead sums the holds listed up to and including h, oldest first, and
// returns those of them still being checked. A nil h sums the whole list.
func holdsAhead(holds []*quotaHold, h *quotaHold) (bytes, objects int64, n int, unchecked []chan struct{}) {
	for _, o := range holds {
		bytes += o.bytes
		objects += o.objects
		n++
		if o == h {
			return
		}
		select {
		case <-o.checked:
		default:
			unchecked = append(unchecked, o.checked)
		}
	}
	return
}

func dropHold(holds map[string][]*quotaHold, key string, h *quotaHold) {
	rest := slices.DeleteFunc(holds[key], func(o *quotaHold) bool { return o == h })
	if len(rest) == 0 {
		delete(holds, key)
	} else {
		holds[key] = rest
	}
}

// reserveWriteQuota reserves room for a write in flight. The hold is taken
// first and the check counts it together with every hold taken before it, so
// two concurrent writes on this node never both pass against the same free
// space. No lock is held across the bucket read or the tenant check, which in
// a cluster goes over the network.
//
// Quotas stay soft: holds are local to the node, and a write is in neither its
// hold nor the committed usage between releasing and updating usage. A write
// that does not grow usage is never refused, and deletes do not reserve at all.
//
// The returned release runs once. It must run after the write is committed and
// before its usage is updated, so no check counts the write twice.
func (om *objectManager) reserveWriteQuota(ctx context.Context, bucket string, size int64, previous *metadata.ObjectMetadata, versioned bool) (func(), error) {
	if isBypassQuotaEnforcement(ctx) {
		return func() {}, nil
	}
	delta := size
	if !versioned && previous != nil {
		delta -= previous.Size
	}
	h := &quotaHold{bytes: max(delta, 0), checked: make(chan struct{})}
	if previous == nil || isMetadataDeleteMarker(previous) {
		h.objects = 1
	}
	if h.bytes == 0 && h.objects == 0 {
		return func() {}, nil
	}
	tenant, _ := om.parseBucketPath(bucket)
	if h.bytes == 0 || om.authManager == nil {
		tenant = "" // no tenant check, no tenant hold
	}

	om.quotaMu.Lock()
	if om.pendingQuotas == nil {
		om.pendingQuotas = make(map[string][]*quotaHold)
	}
	if om.pendingTenants == nil {
		om.pendingTenants = make(map[string][]*quotaHold)
	}
	bytes, objects, _, _ := holdsAhead(om.pendingQuotas[bucket], nil)
	tenantBytes, _, _, _ := holdsAhead(om.pendingTenants[tenant], nil)
	if h.bytes > math.MaxInt64-bytes || h.objects > math.MaxInt64-objects {
		om.quotaMu.Unlock()
		return nil, ErrBucketQuotaExceeded
	}
	if h.bytes > math.MaxInt64-tenantBytes {
		om.quotaMu.Unlock()
		return nil, fmt.Errorf("storage quota reservation overflow")
	}
	om.pendingQuotas[bucket] = append(om.pendingQuotas[bucket], h)
	if tenant != "" {
		om.pendingTenants[tenant] = append(om.pendingTenants[tenant], h)
	}
	om.quotaMu.Unlock()

	drop := func() {
		dropHold(om.pendingQuotas, bucket, h)
		if tenant != "" {
			dropHold(om.pendingTenants, tenant, h)
		}
	}
	refused := true
	defer func() {
		om.quotaMu.Lock()
		defer om.quotaMu.Unlock()
		if refused {
			drop()
		}
		close(h.checked) // with the drop: no write sees this one decided but still held
	}()
	if err := om.checkWriteQuota(ctx, bucket, tenant, h); err != nil {
		return nil, err
	}
	refused = false
	var once sync.Once
	return func() {
		once.Do(func() {
			om.quotaMu.Lock()
			defer om.quotaMu.Unlock()
			drop()
		})
	}, nil
}

// checkWriteQuota checks the committed usage plus every hold up to h. A hold
// counted may belong to a write that finishes during the check, and is then in
// the committed usage too, or to a write that is refused. A refusal stands only
// once the writes ahead are checked and none of them has left; otherwise the
// check runs again. Each run has fewer writes ahead, so it ends.
func (om *objectManager) checkWriteQuota(ctx context.Context, bucket, tenant string, h *quotaHold) error {
	for {
		om.quotaMu.Lock()
		bytes, objects, n, _ := holdsAhead(om.pendingQuotas[bucket], h)
		tenantBytes, _, tenantN, _ := holdsAhead(om.pendingTenants[tenant], h)
		om.quotaMu.Unlock()

		meta, err := om.loadBucketMetadata(ctx, bucket)
		if err != nil {
			return err
		}
		if q := meta.Quota; q != nil &&
			(q.MaxSizeBytes > 0 && h.bytes > 0 && bytes > q.MaxSizeBytes-meta.TotalSize ||
				q.MaxObjectCount > 0 && h.objects > 0 && objects > q.MaxObjectCount-meta.ObjectCount) {
			if om.holdsLeft(ctx, false, bucket, h, n) {
				continue
			}
			return ErrBucketQuotaExceeded
		}
		if tenant == "" {
			return nil
		}
		if err := om.authManager.CheckTenantStorageQuota(ctx, tenant, tenantBytes); err != nil {
			if om.holdsLeft(ctx, true, tenant, h, tenantN) {
				continue
			}
			return fmt.Errorf("storage quota exceeded: %w", err)
		}
		return nil
	}
}

// holdsLeft waits for the writes ahead of h to be checked and reports whether
// fewer than n holds, h included, remain up to h.
func (om *objectManager) holdsLeft(ctx context.Context, tenantHolds bool, key string, h *quotaHold, n int) bool {
	ahead := func() (int, []chan struct{}) {
		om.quotaMu.Lock()
		defer om.quotaMu.Unlock()
		holds := om.pendingQuotas[key]
		if tenantHolds {
			holds = om.pendingTenants[key]
		}
		_, _, held, unchecked := holdsAhead(holds, h)
		return held, unchecked
	}
	_, unchecked := ahead()
	for _, c := range unchecked {
		select {
		case <-c:
		case <-ctx.Done():
			return false
		}
	}
	now, _ := ahead()
	return now < n
}
