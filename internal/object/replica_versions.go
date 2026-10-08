package object

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// ErrReplicaDigestMismatch: the bytes of a copy do not hash to the ETag of the
// version they copy.
var ErrReplicaDigestMismatch = errors.New("replica data does not match the ETag of its version")

type replicaCopyKey struct{}

// WithReplicaCopy marks a write as a copy of another node's version. The copy
// is stored as a version when it carries a version ID and as the object
// without one otherwise, whatever this bucket's versioning status is now: a
// bucket keeps the versions written before its versioning was suspended.
func WithReplicaCopy(ctx context.Context) context.Context {
	return context.WithValue(ctx, replicaCopyKey{}, true)
}

func isReplicaCopy(ctx context.Context) bool {
	v, _ := ctx.Value(replicaCopyKey{}).(bool)
	return v
}

// ReplicatedAttributes is what a copy of a version carries besides its data,
// headers and lock state.
type ReplicatedAttributes struct {
	ETag             string     `json:"etag,omitempty"`
	Tags             *TagSet    `json:"tags,omitempty"`
	ACL              *ACL       `json:"acl,omitempty"`
	RestoreStatus    string     `json:"restore_status,omitempty"`
	RestoreExpiresAt *time.Time `json:"restore_expires_at,omitempty"`
}

// AttributesOf returns the attributes of obj a copy has to carry.
func AttributesOf(obj *Object) ReplicatedAttributes {
	return ReplicatedAttributes{
		ETag:             obj.ETag,
		Tags:             obj.Tags,
		ACL:              obj.ACL,
		RestoreStatus:    obj.RestoreStatus,
		RestoreExpiresAt: obj.RestoreExpiresAt,
	}
}

type replicatedAttributesKey struct{}

// WithReplicatedAttributes gives the next write the attributes of the version
// it copies.
func WithReplicatedAttributes(ctx context.Context, a ReplicatedAttributes) context.Context {
	return context.WithValue(ctx, replicatedAttributesKey{}, a)
}

func replicatedAttributesFromContext(ctx context.Context) (ReplicatedAttributes, bool) {
	a, ok := ctx.Value(replicatedAttributesKey{}).(ReplicatedAttributes)
	return a, ok
}

// checkReplicaDigest refuses a copy whose bytes are not the version's. A
// multipart ETag is not a digest of the bytes and cannot be checked.
func checkReplicaDigest(ctx context.Context, etag string) error {
	a, ok := replicatedAttributesFromContext(ctx)
	if !ok || a.ETag == "" || strings.Contains(a.ETag, "-") || a.ETag == etag {
		return nil
	}
	return ErrReplicaDigestMismatch
}

// replicatedETag is the ETag of the write a copy carries: the one it was sent
// with, or the digest of its data.
func replicatedETag(ctx context.Context, digest string) string {
	if a, ok := replicatedAttributesFromContext(ctx); ok && a.ETag != "" {
		return a.ETag
	}
	return digest
}

// applyReplicatedAttributes gives obj the attributes of the version it copies.
func applyReplicatedAttributes(ctx context.Context, obj *Object) {
	a, ok := replicatedAttributesFromContext(ctx)
	if !ok {
		return
	}
	if a.ETag != "" {
		obj.ETag = a.ETag
	}
	obj.Tags = a.Tags
	obj.ACL = a.ACL
	obj.RestoreStatus = a.RestoreStatus
	obj.RestoreExpiresAt = a.RestoreExpiresAt
}

// versionLanding is how a copy of a version from another node lands here.
type versionLanding struct {
	latest   bool                     // it becomes the key's latest version
	replaced *metadata.ObjectMetadata // the same version, already stored here
}

// landReplicatedVersion places a copy of versionID, modified at modified, among
// the versions this node holds. Copies can arrive out of order — an initial
// sync or a catch-up racing live writes — so a copy older than the current
// latest is kept as an older version, and a version already here is replaced
// rather than counted twice. Version IDs order writes within one second; a
// current object without one, written while versioning was suspended, stays
// current against a copy of the same second.
func (om *objectManager) landReplicatedVersion(ctx context.Context, bucket, key, versionID string, modified time.Time, latest *metadata.ObjectMetadata) (versionLanding, error) {
	landing := versionLanding{latest: true}
	existing, err := om.metadataStore.GetObject(ctx, bucket, key, versionID)
	switch {
	case err == nil:
		landing.replaced = existing
	case errors.Is(err, metadata.ErrObjectNotFound), errors.Is(err, metadata.ErrVersionNotFound):
	default:
		return landing, err
	}
	if latest != nil && latest.VersionID != versionID {
		l, m := latest.LastModified.Unix(), modified.Unix()
		newer := latest.VersionID == "" || latest.VersionID > versionID
		landing.latest = !(l > m || (l == m && newer))
	}
	return landing, nil
}

// updateUsageAfterReplicatedVersion updates bucket and tenant usage for a
// replicated version as it landed: a replacement adds only its size change,
// an older version adds its bytes and no object.
func (om *objectManager) updateUsageAfterReplicatedVersion(ctx context.Context, bucket, key string, size int64, landing versionLanding, before *metadata.ObjectMetadata) {
	tenantID, bucketName := om.parseBucketPath(bucket)
	switch {
	case landing.replaced != nil:
		om.adjustUsage(ctx, tenantID, bucketName, size-landing.replaced.Size)
	case !landing.latest:
		om.adjustUsage(ctx, tenantID, bucketName, size)
	default:
		om.updateBucketMetricsAfterPut(ctx, tenantID, bucketName, bucket, key, size, true, before)
		om.updateTenantQuotaAfterPut(ctx, tenantID, key, size, true, before)
	}
}

func (om *objectManager) adjustUsage(ctx context.Context, tenantID, bucketName string, delta int64) {
	if delta == 0 {
		return
	}
	if om.bucketManager != nil {
		if err := om.bucketManager.AdjustBucketSize(ctx, tenantID, bucketName, delta); err != nil {
			logrus.WithError(err).WithField("bucket", bucketName).Warn("Failed to adjust bucket size for a replicated version")
		}
	}
	if om.authManager != nil && tenantID != "" {
		if err := om.authManager.IncrementTenantStorage(ctx, tenantID, delta); err != nil {
			logrus.WithError(err).WithField("tenant_id", tenantID).Warn("Failed to adjust tenant storage for a replicated version")
		}
	}
}
