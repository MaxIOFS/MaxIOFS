package object

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/maxiofs/maxiofs/internal/metadata"
)

// inFlightUpload is what a multipart upload in progress holds on this node:
// the size of each part it stored. Its parts are on the disk and in no quota
// until the upload completes, so they are counted here.
type inFlightUpload struct {
	bucket, tenant string
	parts          map[int]int64
	total          int64
	// replaces is the size of the object the completion overwrites in place:
	// that much of the upload does not grow the usage.
	replaces int64
}

// counted is what the upload adds to the usage when it completes.
func (u *inFlightUpload) counted() int64 {
	return max(u.total-u.replaces, 0)
}

// inFlightUploads is the bytes of the multipart uploads in progress on this
// node, by bucket and by tenant. It is kept in memory: after a restart an
// upload is counted again, from its stored parts, when its next part arrives.
type inFlightUploads struct {
	mu      sync.Mutex
	uploads map[string]*inFlightUpload
	buckets map[string]int64
	tenants map[string]int64
}

// add changes what bucket and tenant hold by delta. mu is held.
func (f *inFlightUploads) add(u *inFlightUpload, delta int64) {
	if delta == 0 {
		return
	}
	if f.buckets[u.bucket] += delta; f.buckets[u.bucket] <= 0 {
		delete(f.buckets, u.bucket)
	}
	if u.tenant == "" {
		return
	}
	if f.tenants[u.tenant] += delta; f.tenants[u.tenant] <= 0 {
		delete(f.tenants, u.tenant)
	}
}

// trackUpload is the count of an upload, started from the parts it has stored
// when it is not counted yet.
func (om *objectManager) trackUpload(ctx context.Context, uploadID, bucket, key string) (*inFlightUpload, error) {
	f := &om.inFlight
	f.mu.Lock()
	u := f.uploads[uploadID]
	f.mu.Unlock()
	if u != nil {
		return u, nil
	}

	tenant, _ := om.parseBucketPath(bucket)
	if om.authManager == nil {
		tenant = ""
	}
	u = &inFlightUpload{bucket: bucket, tenant: tenant, parts: make(map[int]int64)}
	stored, err := om.metadataStore.ListParts(ctx, uploadID)
	if err != nil && !errors.Is(err, metadata.ErrUploadNotFound) {
		return nil, err
	}
	for _, part := range stored {
		u.parts[part.PartNumber] = part.Size
		u.total += part.Size
	}
	if existing, err := om.metadataStore.GetObject(ctx, bucket, key); err == nil {
		u.replaces = overwrittenInPlace(existing)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if counted := f.uploads[uploadID]; counted != nil {
		return counted, nil
	}
	if f.uploads == nil {
		f.uploads = make(map[string]*inFlightUpload)
		f.buckets = make(map[string]int64)
		f.tenants = make(map[string]int64)
	}
	f.uploads[uploadID] = u
	f.add(u, u.counted())
	return u, nil
}

// admitPart refuses a part of an upload once what its bucket or its tenant
// stores, together with the parts of the uploads in progress, has reached the
// quota: the parts of an upload that could never complete are not taken. The
// part's own size is not known before it is read, so the quota is passed by no
// more than the parts being written at that moment. An upload that overwrites
// an object in place is not refused for the room that object already has.
func (om *objectManager) admitPart(ctx context.Context, uploadID, bucket, key string) error {
	if isBypassQuotaEnforcement(ctx) {
		return nil
	}
	u, err := om.trackUpload(ctx, uploadID, bucket, key)
	if err != nil {
		return err
	}
	f := &om.inFlight
	for retried := false; ; retried = true {
		f.mu.Lock()
		inBucket, inTenant := f.buckets[u.bucket], f.tenants[u.tenant]
		ownRoom := max(u.replaces-u.total, 0)
		f.mu.Unlock()

		meta, err := om.loadBucketMetadata(ctx, bucket)
		if errors.Is(err, ErrBucketNotFound) {
			// No bucket, no quota to hold the part to: the completion answers
			// for the bucket, as it did before parts were counted.
			return nil
		}
		if err != nil {
			return err
		}
		var refusal error
		if q := meta.Quota; q != nil && q.MaxSizeBytes > 0 && inBucket >= q.MaxSizeBytes-meta.TotalSize+ownRoom {
			refusal = ErrBucketQuotaExceeded
		} else if u.tenant != "" && ownRoom == 0 {
			if err := om.authManager.CheckTenantStorageQuota(ctx, u.tenant, inTenant+1); err != nil {
				refusal = fmt.Errorf("storage quota exceeded: %w", err)
			}
		}
		// An upload ended without this manager, as one the store swept, is
		// still counted: a refusal stands once those are gone from the count.
		if refusal == nil || retried || !om.dropEndedUploads(ctx, u) {
			return refusal
		}
	}
}

// partStored counts a part an upload stored, in place of the part of that
// number it had.
func (om *objectManager) partStored(uploadID string, partNumber int, size int64) {
	f := &om.inFlight
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.uploads[uploadID]
	if u == nil {
		return
	}
	before := u.counted()
	u.total += size - u.parts[partNumber]
	u.parts[partNumber] = size
	f.add(u, u.counted()-before)
}

// uploadEnded stops counting an upload completed or aborted.
func (om *objectManager) uploadEnded(uploadID string) {
	f := &om.inFlight
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.uploads[uploadID]
	if u == nil {
		return
	}
	delete(f.uploads, uploadID)
	f.add(u, -u.counted())
}

// dropEndedUploads stops counting the uploads of like's bucket or tenant that
// no longer exist, and reports whether there was any.
func (om *objectManager) dropEndedUploads(ctx context.Context, like *inFlightUpload) bool {
	f := &om.inFlight
	f.mu.Lock()
	var ids []string
	for id, u := range f.uploads {
		if u.bucket == like.bucket || (like.tenant != "" && u.tenant == like.tenant) {
			ids = append(ids, id)
		}
	}
	f.mu.Unlock()

	dropped := false
	for _, id := range ids {
		if _, err := om.metadataStore.GetMultipartUpload(ctx, id); errors.Is(err, metadata.ErrUploadNotFound) {
			om.uploadEnded(id)
			dropped = true
		}
	}
	return dropped
}
