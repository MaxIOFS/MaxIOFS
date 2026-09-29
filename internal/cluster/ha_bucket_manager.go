package cluster

import (
	"context"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
)

// HABucketManager sends every change to a bucket to the other nodes of a
// cluster whose replication factor is above 1. Usage counters are each node's
// own and are not sent.
type HABucketManager struct {
	bucket.Manager
	states *BucketStates
}

// NewHABucketManager wraps m.
func NewHABucketManager(m bucket.Manager, states *BucketStates) bucket.Manager {
	return &HABucketManager{Manager: m, states: states}
}

func (h *HABucketManager) changed(ctx context.Context, tenantID, name string, err error) error {
	if err == nil {
		h.states.Publish(ctx, tenantID, name)
	}
	return err
}

func (h *HABucketManager) removed(ctx context.Context, tenantID, name string, err error) error {
	if err == nil {
		h.states.PublishDeletion(ctx, tenantID, name)
	}
	return err
}

func (h *HABucketManager) CreateBucket(ctx context.Context, tenantID, name, ownerID string) error {
	return h.changed(ctx, tenantID, name, h.Manager.CreateBucket(ctx, tenantID, name, ownerID))
}

func (h *HABucketManager) DeleteBucket(ctx context.Context, tenantID, name string) error {
	return h.removed(ctx, tenantID, name, h.Manager.DeleteBucket(ctx, tenantID, name))
}

func (h *HABucketManager) ForceDeleteBucket(ctx context.Context, tenantID, name string) error {
	return h.removed(ctx, tenantID, name, h.Manager.ForceDeleteBucket(ctx, tenantID, name))
}

func (h *HABucketManager) UpdateBucket(ctx context.Context, tenantID, name string, b *bucket.Bucket) error {
	return h.changed(ctx, tenantID, name, h.Manager.UpdateBucket(ctx, tenantID, name, b))
}

func (h *HABucketManager) SetBucketPolicy(ctx context.Context, tenantID, name string, policy *bucket.Policy) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetBucketPolicy(ctx, tenantID, name, policy))
}

func (h *HABucketManager) DeleteBucketPolicy(ctx context.Context, tenantID, name string) error {
	return h.changed(ctx, tenantID, name, h.Manager.DeleteBucketPolicy(ctx, tenantID, name))
}

func (h *HABucketManager) SetVersioning(ctx context.Context, tenantID, name string, config *bucket.VersioningConfig) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetVersioning(ctx, tenantID, name, config))
}

func (h *HABucketManager) SetLifecycle(ctx context.Context, tenantID, name string, config *bucket.LifecycleConfig) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetLifecycle(ctx, tenantID, name, config))
}

func (h *HABucketManager) DeleteLifecycle(ctx context.Context, tenantID, name string) error {
	return h.changed(ctx, tenantID, name, h.Manager.DeleteLifecycle(ctx, tenantID, name))
}

func (h *HABucketManager) SetCORS(ctx context.Context, tenantID, name string, config *bucket.CORSConfig) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetCORS(ctx, tenantID, name, config))
}

func (h *HABucketManager) DeleteCORS(ctx context.Context, tenantID, name string) error {
	return h.changed(ctx, tenantID, name, h.Manager.DeleteCORS(ctx, tenantID, name))
}

func (h *HABucketManager) SetWebsite(ctx context.Context, tenantID, name string, config *bucket.WebsiteConfig) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetWebsite(ctx, tenantID, name, config))
}

func (h *HABucketManager) DeleteWebsite(ctx context.Context, tenantID, name string) error {
	return h.changed(ctx, tenantID, name, h.Manager.DeleteWebsite(ctx, tenantID, name))
}

func (h *HABucketManager) SetEncryption(ctx context.Context, tenantID, name string, config *bucket.EncryptionConfig) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetEncryption(ctx, tenantID, name, config))
}

func (h *HABucketManager) DeleteEncryption(ctx context.Context, tenantID, name string) error {
	return h.changed(ctx, tenantID, name, h.Manager.DeleteEncryption(ctx, tenantID, name))
}

func (h *HABucketManager) SetPublicAccessBlock(ctx context.Context, tenantID, name string, config *bucket.PublicAccessBlock) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetPublicAccessBlock(ctx, tenantID, name, config))
}

func (h *HABucketManager) DeletePublicAccessBlock(ctx context.Context, tenantID, name string) error {
	return h.changed(ctx, tenantID, name, h.Manager.DeletePublicAccessBlock(ctx, tenantID, name))
}

func (h *HABucketManager) SetOwnershipControls(ctx context.Context, tenantID, name string, config *bucket.OwnershipControlsConfig) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetOwnershipControls(ctx, tenantID, name, config))
}

func (h *HABucketManager) DeleteOwnershipControls(ctx context.Context, tenantID, name string) error {
	return h.changed(ctx, tenantID, name, h.Manager.DeleteOwnershipControls(ctx, tenantID, name))
}

func (h *HABucketManager) SetLogging(ctx context.Context, tenantID, name string, config *bucket.LoggingConfig) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetLogging(ctx, tenantID, name, config))
}

func (h *HABucketManager) DeleteLogging(ctx context.Context, tenantID, name string) error {
	return h.changed(ctx, tenantID, name, h.Manager.DeleteLogging(ctx, tenantID, name))
}

func (h *HABucketManager) SetNotification(ctx context.Context, tenantID, name string, config *bucket.NotificationConfig) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetNotification(ctx, tenantID, name, config))
}

func (h *HABucketManager) DeleteNotification(ctx context.Context, tenantID, name string) error {
	return h.changed(ctx, tenantID, name, h.Manager.DeleteNotification(ctx, tenantID, name))
}

func (h *HABucketManager) SetBucketTags(ctx context.Context, tenantID, name string, tags map[string]string) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetBucketTags(ctx, tenantID, name, tags))
}

func (h *HABucketManager) SetObjectLockConfig(ctx context.Context, tenantID, name string, config *bucket.ObjectLockConfig) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetObjectLockConfig(ctx, tenantID, name, config))
}

func (h *HABucketManager) SetQuota(ctx context.Context, tenantID, name string, quota *metadata.BucketQuota) error {
	return h.changed(ctx, tenantID, name, h.Manager.SetQuota(ctx, tenantID, name, quota))
}

func (h *HABucketManager) DeleteQuota(ctx context.Context, tenantID, name string) error {
	return h.changed(ctx, tenantID, name, h.Manager.DeleteQuota(ctx, tenantID, name))
}

// SetBucketACL dates the change on the bucket, since the ACL is stored apart
// from its configuration and the two are sent as one version.
func (h *HABucketManager) SetBucketACL(ctx context.Context, tenantID, name string, a interface{}) error {
	err := h.Manager.SetBucketACL(ctx, tenantID, name, a)
	if err == nil && h.states.active(ctx) {
		err = h.states.touch(ctx, tenantID, name)
	}
	return h.changed(ctx, tenantID, name, err)
}
