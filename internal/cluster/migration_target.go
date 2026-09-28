package cluster

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
)

// Errors a migration step answers with, by what the caller can do about them.
var (
	ErrMigrationNotFound = errors.New("not found")
	ErrMigrationConflict = errors.New("conflict")
	ErrMigrationInvalid  = errors.New("invalid")
)

// MigrationStage asks the target of a migration to prepare the copy of a
// bucket: its configuration and ACL, no objects.
type MigrationStage struct {
	JobID        int64                    `json:"job_id"`
	SourceNodeID string                   `json:"source_node_id"`
	Bucket       *metadata.BucketMetadata `json:"bucket"`
	ACL          *acl.ACL                 `json:"acl,omitempty"`
}

// MigrationCommit hands a copied bucket over to the target, with the bucket's
// rows in the source node's database.
type MigrationCommit struct {
	JobID        int64      `json:"job_id"`
	SourceNodeID string     `json:"source_node_id"`
	Bucket       string     `json:"bucket"`
	Rows         BucketRows `json:"rows"`
}

// MigrationAbort asks the target to remove the copy a migration was filling.
type MigrationAbort struct {
	JobID        int64  `json:"job_id"`
	SourceNodeID string `json:"source_node_id"`
	Bucket       string `json:"bucket"`
}

// MigrationManifestRequest asks the target how it holds keys of a bucket.
type MigrationManifestRequest struct {
	Bucket string   `json:"bucket"`
	Keys   []string `json:"keys"`
}

// VersionManifest is one version of a key as both ends of a migration must
// hold it: its ID and a digest of what a client can read of it.
type VersionManifest struct {
	VersionID string `json:"version_id"`
	Digest    string `json:"digest"`
}

func incomingMarker(sourceNodeID string, jobID int64) string {
	return "incoming:" + sourceNodeID + ":" + strconv.FormatInt(jobID, 10)
}

func outgoingMarker(targetNodeID string, jobID int64) string {
	return "outgoing:" + targetNodeID + ":" + strconv.FormatInt(jobID, 10)
}

func pathOf(b *metadata.BucketMetadata) string {
	if b.TenantID == "" {
		return b.Name
	}
	return b.TenantID + "/" + b.Name
}

// KeyManifest describes every version this node holds of key: the stored
// versions and delete markers, and the current object when it has no version
// ID. Ordered by version ID.
func KeyManifest(ctx context.Context, objects object.Manager, bucket, key string) ([]VersionManifest, error) {
	versions, err := objects.GetObjectVersions(ctx, bucket, key)
	if err != nil && !errors.Is(err, object.ErrObjectNotFound) {
		return nil, err
	}
	out := make([]VersionManifest, 0, len(versions)+1)
	for i := range versions {
		v := &versions[i]
		out = append(out, VersionManifest{VersionID: v.VersionID, Digest: versionDigest(&v.Object, v.IsDeleteMarker)})
	}
	current, err := objects.GetObjectMetadata(ctx, bucket, key)
	switch {
	case errors.Is(err, object.ErrObjectNotFound):
	case err != nil:
		return nil, err
	case current.VersionID == "":
		out = append(out, VersionManifest{Digest: versionDigest(current, false)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].VersionID < out[j].VersionID })
	return out, nil
}

// versionDigest covers what a client reads of a version: its data by ETag and
// size, its headers, metadata, tags, ACL and lock state, and its time.
func versionDigest(obj *object.Object, deleteMarker bool) string {
	facts := struct {
		DeleteMarker bool              `json:"delete_marker"`
		ETag         string            `json:"etag"`
		Size         int64             `json:"size"`
		Modified     int64             `json:"modified"`
		Headers      [6]string         `json:"headers"`
		Checksum     [2]string         `json:"checksum"`
		Metadata     map[string]string `json:"metadata"`
		Tags         map[string]string `json:"tags"`
		ACL          *object.ACL       `json:"acl"`
		Retention    string            `json:"retention"`
		LegalHold    bool              `json:"legal_hold"`
		Restore      string            `json:"restore"`
	}{
		DeleteMarker: deleteMarker,
		ETag:         obj.ETag,
		Size:         obj.Size,
		Modified:     obj.LastModified.Unix(),
		Headers: [6]string{obj.ContentType, obj.ContentDisposition, obj.ContentEncoding,
			obj.CacheControl, obj.ContentLanguage, obj.StorageClass},
		Checksum: [2]string{obj.ChecksumAlgorithm, obj.ChecksumValue},
		Metadata: obj.Metadata,
		ACL:      obj.ACL,
	}
	if obj.Tags != nil {
		facts.Tags = make(map[string]string, len(obj.Tags.Tags))
		for _, t := range obj.Tags.Tags {
			facts.Tags[t.Key] = t.Value
		}
	}
	if r := obj.Retention; r != nil {
		facts.Retention = r.Mode + "@" + r.RetainUntilDate.UTC().Format(time.RFC3339Nano)
	}
	facts.LegalHold = obj.LegalHold != nil && obj.LegalHold.Status == object.LegalHoldStatusOn
	if obj.RestoreStatus != "" {
		facts.Restore = obj.RestoreStatus
		if obj.RestoreExpiresAt != nil {
			facts.Restore += "@" + obj.RestoreExpiresAt.UTC().Format(time.RFC3339Nano)
		}
	}
	data, _ := json.Marshal(facts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// bucketRemover deletes a bucket with everything in it.
type bucketRemover interface {
	ForceDeleteBucket(ctx context.Context, tenantID, name string) error
}

// MigrationTarget is a node a bucket is migrated to. It holds the copy hidden
// until the source commits it.
type MigrationTarget struct {
	store   metadata.Store
	objects object.Manager
	buckets bucketRemover
	acls    acl.Manager
	storage storage.Backend
	db      *sql.DB
}

// NewMigrationTarget returns the target side of migrations on this node.
func NewMigrationTarget(store metadata.Store, objects object.Manager, buckets bucketRemover, acls acl.Manager, backend storage.Backend, db *sql.DB) *MigrationTarget {
	return &MigrationTarget{store: store, objects: objects, buckets: buckets, acls: acls, storage: backend, db: db}
}

// Stage creates the hidden copy of a bucket a migration fills, with the
// bucket's configuration and ACL. A copy an earlier migration left here is
// replaced; a bucket that lives here is not.
func (t *MigrationTarget) Stage(ctx context.Context, st MigrationStage) error {
	if st.Bucket == nil || st.Bucket.Name == "" || st.JobID <= 0 || st.SourceNodeID == "" {
		return fmt.Errorf("%w: the stage names no bucket, job or source", ErrMigrationInvalid)
	}
	marker := incomingMarker(st.SourceNodeID, st.JobID)
	existing, err := t.store.GetBucketByName(ctx, st.Bucket.Name)
	switch {
	case err == nil && existing.Metadata[metadata.BucketMovingKey] == marker:
		return nil
	case err == nil && existing.Moving():
		if err := t.remove(ctx, existing); err != nil {
			return err
		}
	case err == nil:
		return fmt.Errorf("%w: bucket %s lives on this node", ErrMigrationConflict, st.Bucket.Name)
	case !errors.Is(err, metadata.ErrBucketNotFound):
		return err
	}

	b := *st.Bucket
	b.Metadata = make(map[string]string, len(st.Bucket.Metadata)+1)
	for k, v := range st.Bucket.Metadata {
		b.Metadata[k] = v
	}
	b.Metadata[metadata.BucketMovingKey] = marker
	b.ObjectCount, b.TotalSize = 0, 0
	b.HA = nil
	path := pathOf(&b)
	if err := bucket.CreateEntry(ctx, t.store, t.storage, &b); err != nil {
		return err
	}
	if err := t.storage.CreateBucket(ctx, path); err != nil {
		return err
	}
	if st.ACL != nil {
		if err := t.acls.SetBucketACL(ctx, b.TenantID, b.Name, st.ACL); err != nil {
			return err
		}
	}
	return nil
}

// Manifests describes how this node holds keys of bucket.
func (t *MigrationTarget) Manifests(ctx context.Context, req MigrationManifestRequest) (map[string][]VersionManifest, error) {
	out := make(map[string][]VersionManifest, len(req.Keys))
	for _, key := range req.Keys {
		m, err := KeyManifest(ctx, t.objects, req.Bucket, key)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = m
	}
	return out, nil
}

// Commit makes the copy the bucket, with the rows the source holds for it.
// Committing a bucket already committed changes nothing.
func (t *MigrationTarget) Commit(ctx context.Context, c MigrationCommit) error {
	b, err := t.store.GetBucketByName(ctx, c.Bucket)
	if errors.Is(err, metadata.ErrBucketNotFound) {
		return fmt.Errorf("%w: no copy of bucket %s is here", ErrMigrationNotFound, c.Bucket)
	}
	if err != nil {
		return err
	}
	if !b.Moving() {
		return nil
	}
	if b.Metadata[metadata.BucketMovingKey] != incomingMarker(c.SourceNodeID, c.JobID) {
		return fmt.Errorf("%w: the copy of bucket %s here belongs to another migration", ErrMigrationConflict, c.Bucket)
	}
	if err := applyBucketRows(ctx, t.db, c.Bucket, c.Rows); err != nil {
		return err
	}
	delete(b.Metadata, metadata.BucketMovingKey)
	return t.store.UpdateBucket(ctx, b)
}

// Abort removes the copy the migration was filling. A bucket that lives here,
// or a copy of another migration, is left alone.
func (t *MigrationTarget) Abort(ctx context.Context, a MigrationAbort) error {
	b, err := t.store.GetBucketByName(ctx, a.Bucket)
	if errors.Is(err, metadata.ErrBucketNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.Metadata[metadata.BucketMovingKey] != incomingMarker(a.SourceNodeID, a.JobID) {
		return fmt.Errorf("%w: bucket %s here is not the copy of this migration", ErrMigrationConflict, a.Bucket)
	}
	return t.remove(ctx, b)
}

// remove deletes a copy that is not the bucket, with its ACL.
func (t *MigrationTarget) remove(ctx context.Context, b *metadata.BucketMetadata) error {
	if err := t.buckets.ForceDeleteBucket(ctx, b.TenantID, b.Name); err != nil && !errors.Is(err, bucket.ErrBucketNotFound) {
		return err
	}
	return t.acls.DeleteBucketACL(ctx, b.TenantID, b.Name)
}
