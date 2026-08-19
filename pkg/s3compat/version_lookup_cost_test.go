package s3compat

import (
	"context"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingVersionStore records which lookup a caller reaches for. Reading a
// single object's versions must not go through the bucket-wide listing: a
// backup tool probes with HEAD before every write, and each miss would then
// scan and unmarshal every version in the bucket.
type countingVersionStore struct {
	perKeyCalls     int
	bucketWideCalls int
	versions        []*metadata.ObjectVersion
}

func (c *countingVersionStore) GetObjectVersions(ctx context.Context, bucket, key string) ([]*metadata.ObjectVersion, error) {
	c.perKeyCalls++
	var out []*metadata.ObjectVersion
	for _, v := range c.versions {
		if v.Key == key {
			out = append(out, v)
		}
	}
	return out, nil
}

func (c *countingVersionStore) ListAllObjectVersions(ctx context.Context, bucket, prefix string, maxKeys int) ([]*metadata.ObjectVersion, error) {
	c.bucketWideCalls++
	return c.versions, nil
}

func (c *countingVersionStore) GetBucketByName(ctx context.Context, name string) (*metadata.BucketMetadata, error) {
	return nil, nil
}

func (c *countingVersionStore) GetMultipartUpload(ctx context.Context, uploadID string) (*metadata.MultipartUploadMetadata, error) {
	return nil, nil
}

func TestFindExactObjectVersion_ReadsOnlyTheKeysOwnVersions(t *testing.T) {
	store := &countingVersionStore{
		versions: []*metadata.ObjectVersion{
			{Key: "other/a.txt", VersionID: "v1", IsLatest: true},
			{Key: "wanted.txt", VersionID: "v7", IsLatest: true},
			{Key: "other/b.txt", VersionID: "v2", IsLatest: true},
		},
	}

	h := &Handler{}
	h.SetMetadataStore(store)

	version, found := h.findExactObjectVersion(context.Background(), "tenant/bucket", "wanted.txt", "v7")
	require.True(t, found)
	assert.Equal(t, "v7", version.VersionID)

	_, found = h.findExactObjectVersion(context.Background(), "tenant/bucket", "missing.txt", "")
	assert.False(t, found, "a miss must still be a miss")

	assert.Equal(t, 2, store.perKeyCalls, "each lookup reads one key's versions")
	assert.Zero(t, store.bucketWideCalls,
		"a single-object lookup must never scan the whole bucket's versions")
}
