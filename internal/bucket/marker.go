package bucket

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
)

// markerMu serializes the writes of bucket markers in this process: each
// writes what the store holds when it runs, so the last one holds the
// configuration stored last.
var markerMu sync.Mutex

// markerConfig is what a bucket's marker keeps of it: the settings maxiofs
// recover gives back. The marker's path names the bucket.
type markerConfig struct {
	OwnerID           string                              `json:"owner_id"`
	OwnerType         string                              `json:"owner_type"`
	Region            string                              `json:"region,omitempty"`
	CreatedAt         time.Time                           `json:"created_at"`
	Versioning        *metadata.VersioningMetadata        `json:"versioning,omitempty"`
	ObjectLock        *metadata.ObjectLockMetadata        `json:"object_lock,omitempty"`
	Policy            *metadata.PolicyMetadata            `json:"policy,omitempty"`
	Lifecycle         *metadata.LifecycleMetadata         `json:"lifecycle,omitempty"`
	CORS              *metadata.CORSMetadata              `json:"cors,omitempty"`
	Encryption        *metadata.EncryptionMetadata        `json:"encryption,omitempty"`
	PublicAccessBlock *metadata.PublicAccessBlockMetadata `json:"public_access_block,omitempty"`
	OwnershipControls string                              `json:"ownership_controls,omitempty"`
	Website           *metadata.WebsiteMetadata           `json:"website,omitempty"`
	Notification      *metadata.NotificationMetadata      `json:"notification,omitempty"`
	Logging           *metadata.LoggingMetadata           `json:"logging,omitempty"`
	Tags              map[string]string                   `json:"tags,omitempty"`
	Metadata          map[string]string                   `json:"metadata,omitempty"`
	Quota             *metadata.BucketQuota               `json:"quota,omitempty"`
}

// KeepMarker writes the settings the store holds for a bucket into the
// bucket's marker under root, which maxiofs recover reads them back from. A
// bucket the store no longer holds is left alone.
func KeepMarker(ctx context.Context, store metadata.Store, root, tenantID, name string) error {
	markerMu.Lock()
	defer markerMu.Unlock()

	meta, err := store.GetBucket(ctx, tenantID, name)
	if errors.Is(err, metadata.ErrBucketNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	config, err := json.Marshal(markerConfig{
		OwnerID: meta.OwnerID, OwnerType: meta.OwnerType, Region: meta.Region, CreatedAt: meta.CreatedAt,
		Versioning: meta.Versioning, ObjectLock: meta.ObjectLock, Policy: meta.Policy, Lifecycle: meta.Lifecycle,
		CORS: meta.CORS, Encryption: meta.Encryption, PublicAccessBlock: meta.PublicAccessBlock,
		OwnershipControls: meta.OwnershipControls, Website: meta.Website, Notification: meta.Notification,
		Logging: meta.Logging, Tags: meta.Tags, Metadata: meta.Metadata, Quota: meta.Quota,
	})
	if err != nil {
		return err
	}
	_, err = storage.WriteBucketConfig(root, entryPath(meta), config)
	return err
}
