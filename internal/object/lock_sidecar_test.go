package object

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lockedBucket(t *testing.T, store metadata.Store, name string) {
	t.Helper()
	require.NoError(t, store.CreateBucket(context.Background(), &metadata.BucketMetadata{
		Name: name, OwnerID: "u",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true},
	}))
}

func sidecarOf(t *testing.T, om *objectManager, bucket, key, versionID string) map[string]string {
	t.Helper()
	meta, err := om.storage.GetMetadata(context.Background(), om.versionRef(bucket, key, versionID))
	require.NoError(t, err)
	return meta
}

// The sidecar of a version keeps its Object Lock state: from a multipart
// completion, and through a metadata update, which cannot change it.
func TestTheSidecarKeepsTheObjectLock(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	lockedBucket(t, store, "locked")
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	headers := http.Header{}
	headers.Set("x-amz-object-lock-mode", RetentionModeGovernance)
	headers.Set("x-amz-object-lock-retain-until-date", until.Format(time.RFC3339))
	headers.Set("x-amz-object-lock-legal-hold", LegalHoldStatusOn)
	upload, err := om.CreateMultipartUpload(ctx, "locked", "mp", headers)
	require.NoError(t, err)
	part, err := om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader([]byte("parts")))
	require.NoError(t, err)
	obj, err := om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{{PartNumber: 1, ETag: part.ETag}})
	require.NoError(t, err)
	meta := sidecarOf(t, om, "locked", "mp", obj.VersionID)
	assert.Equal(t, RetentionModeGovernance, meta[storage.MetadataLockModeField])
	assert.Equal(t, until.Format(time.RFC3339Nano), meta[storage.MetadataLockUntilField])
	assert.Equal(t, LegalHoldStatusOn, meta[storage.MetadataLegalHoldField])

	require.NoError(t, om.UpdateObjectMetadata(ctx, "locked", "mp", map[string]string{
		storage.MetadataLockModeField:  RetentionModeCompliance,
		storage.MetadataLegalHoldField: "",
	}))
	meta = sidecarOf(t, om, "locked", "mp", obj.VersionID)
	assert.Equal(t, RetentionModeGovernance, meta[storage.MetadataLockModeField], "a metadata update cannot change the lock")
	assert.Equal(t, LegalHoldStatusOn, meta[storage.MetadataLegalHoldField])
}

// The encryption pass gives the sidecars of versions stored before they kept
// the Object Lock state the state of their entries.
func TestTheEncryptionPassWritesTheObjectLockOfOlderObjects(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	lockedBucket(t, store, "locked")
	held := http.Header{}
	held.Set("x-amz-object-lock-legal-hold", LegalHoldStatusOn)
	obj, err := om.PutObject(ctx, "locked", "old", bytes.NewReader([]byte("old")), held)
	require.NoError(t, err)
	ref := om.versionRef("locked", "old", obj.VersionID)
	meta, err := om.storage.GetMetadata(ctx, ref)
	require.NoError(t, err)
	for _, f := range lockFields {
		delete(meta, f)
	}
	require.NoError(t, om.storage.SetMetadata(ctx, ref, meta))

	_, _, err = om.EncryptExistingObject(ctx, "locked", "old")
	require.NoError(t, err)
	assert.Equal(t, LegalHoldStatusOn, sidecarOf(t, om, "locked", "old", obj.VersionID)[storage.MetadataLegalHoldField])
}

// A raw copy keeps the Object Lock state of the entry it carries, whatever its
// sidecar says.
func TestARawCopyKeepsTheObjectLockOfItsEntry(t *testing.T) {
	ctx := context.Background()
	kekA := newKEKStore(t)
	clusterKeys, err := kekA.EnsureClusterKey()
	require.NoError(t, err)
	kekB := newKEKStore(t)
	require.NoError(t, kekB.AdoptClusterKeys(clusterKeys))
	nodeA, metaA := newNodeManager(t, kekA)
	nodeB, metaB := newNodeManager(t, kekB)
	lockedBucket(t, metaA, "locked")
	lockedBucket(t, metaB, "locked")
	held := http.Header{}
	held.Set("x-amz-object-lock-legal-hold", LegalHoldStatusOn)
	obj, err := nodeA.PutObject(ctx, "locked", "k", bytes.NewReader([]byte("data")), held)
	require.NoError(t, err)

	raw, sidecar, entry, err := nodeA.GetObjectRaw(ctx, "locked", "k", obj.VersionID)
	require.NoError(t, err)
	data, err := io.ReadAll(raw)
	raw.Close()
	require.NoError(t, err)
	delete(sidecar, storage.MetadataLegalHoldField)
	require.NoError(t, nodeB.PutObjectRaw(ctx, "locked", "k", bytes.NewReader(data), sidecar, entry))
	assert.Equal(t, LegalHoldStatusOn, sidecarOf(t, nodeB, "locked", "k", obj.VersionID)[storage.MetadataLegalHoldField])
}
