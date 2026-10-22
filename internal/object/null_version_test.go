package object

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nullVersionBucket is a bucket with no versioning, whose status set changes.
type nullVersionBucket struct {
	t     *testing.T
	om    *objectManager
	store metadata.Store
	name  string
}

func newNullVersionBucket(t *testing.T) *nullVersionBucket {
	om, store, cleanup := setupTestManagerWithStore(t)
	t.Cleanup(cleanup)
	require.NoError(t, store.CreateBucket(context.Background(), &metadata.BucketMetadata{Name: "nullv", OwnerID: "user-1"}))
	return &nullVersionBucket{t: t, om: om, store: store, name: "nullv"}
}

func (b *nullVersionBucket) versioning(status string) {
	b.t.Helper()
	ctx := context.Background()
	meta, err := b.store.GetBucket(ctx, "", b.name)
	require.NoError(b.t, err)
	meta.Versioning = &metadata.VersioningMetadata{Status: status, Enabled: status == "Enabled"}
	require.NoError(b.t, b.store.UpdateBucket(ctx, meta))
}

func (b *nullVersionBucket) put(key, data string) string {
	b.t.Helper()
	obj, err := b.om.PutObject(context.Background(), b.name, key, bytes.NewReader([]byte(data)), http.Header{})
	require.NoError(b.t, err)
	return obj.VersionID
}

// read is the data of a key's version, "" for its current object.
func (b *nullVersionBucket) read(key, versionID string) (string, error) {
	_, r, err := b.om.GetObject(context.Background(), b.name, key, versionID)
	if err != nil {
		return "", err
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	return string(data), err
}

// versions are the version IDs of a key, the null version as "null", and
// which one is latest.
func (b *nullVersionBucket) versions(key string) ([]string, string) {
	b.t.Helper()
	versions, err := b.om.GetObjectVersions(context.Background(), b.name, key)
	require.NoError(b.t, err)
	var ids []string
	latest := ""
	for _, v := range versions {
		ids = append(ids, v.VersionID)
		if v.IsLatest {
			latest = v.VersionID
		}
	}
	current, err := b.store.GetObject(context.Background(), b.name, key)
	if err == nil && current.VersionID == "" {
		ids = append(ids, "null")
		require.Empty(b.t, latest, "a version is latest beside a current null object")
		latest = "null"
	}
	sort.Strings(ids)
	return ids, latest
}

// An object written without a version is kept as the null version when a
// versioned write replaces it: once versioning is enabled, the first overwrite
// or delete of an existing object keeps it.
func TestTheNullVersionIsKeptWhenAVersionReplacesIt(t *testing.T) {
	for _, before := range []string{"", "Suspended"} {
		t.Run("versioning "+map[string]string{"": "never set", "Suspended": "suspended"}[before], func(t *testing.T) {
			b := newNullVersionBucket(t)
			if before != "" {
				b.versioning("Enabled")
				b.versioning(before)
			}
			b.put("k", "written before")
			b.put("other", "written before")
			b.versioning("Enabled")
			v1 := b.put("k", "versioned")

			ids, latest := b.versions("k")
			assert.ElementsMatch(t, []string{"null", v1}, ids)
			assert.Equal(t, v1, latest)
			data, err := b.read("k", "null")
			require.NoError(t, err)
			assert.Equal(t, "written before", data)
			data, err = b.read("k", "")
			require.NoError(t, err)
			assert.Equal(t, "versioned", data)

			_, err = b.om.DeleteObject(context.Background(), b.name, "other", false)
			require.NoError(t, err)
			data, err = b.read("other", "null")
			require.NoError(t, err)
			assert.Equal(t, "written before", data, "a delete marker keeps it too")
		})
	}
}

// While versioning is suspended a write replaces the null version, wherever it
// is, and keeps the others; the null version is latest while it is current.
func TestASuspendedWriteReplacesTheNullVersion(t *testing.T) {
	b := newNullVersionBucket(t)
	b.put("k", "first null")
	b.versioning("Enabled")
	v1 := b.put("k", "v1")
	b.versioning("Suspended")
	b.put("k", "second null")

	ids, latest := b.versions("k")
	assert.ElementsMatch(t, []string{"null", v1}, ids, "the first null version is replaced")
	assert.Equal(t, "null", latest)
	data, err := b.read("k", "null")
	require.NoError(t, err)
	assert.Equal(t, "second null", data)
	data, err = b.read("k", v1)
	require.NoError(t, err)
	assert.Equal(t, "v1", data)
}

// Deleting the null version by its ID removes it alone: when it is current the
// version before it becomes current.
func TestDeletingTheNullVersionByItsID(t *testing.T) {
	ctx := context.Background()
	b := newNullVersionBucket(t)
	b.versioning("Enabled")
	v1 := b.put("current", "v1")
	b.versioning("Suspended")
	b.put("current", "null")
	_, err := b.om.DeleteObject(ctx, b.name, "current", false, "null")
	require.NoError(t, err)
	ids, latest := b.versions("current")
	assert.Equal(t, []string{v1}, ids)
	assert.Equal(t, v1, latest)
	data, err := b.read("current", "")
	require.NoError(t, err)
	assert.Equal(t, "v1", data)

	b.versioning("Suspended")
	b.put("noncurrent", "null")
	b.versioning("Enabled")
	v2 := b.put("noncurrent", "v2")
	_, err = b.om.DeleteObject(ctx, b.name, "noncurrent", false, "null")
	require.NoError(t, err)
	ids, latest = b.versions("noncurrent")
	assert.Equal(t, []string{v2}, ids)
	assert.Equal(t, v2, latest)
	data, err = b.read("noncurrent", "")
	require.NoError(t, err)
	assert.Equal(t, "v2", data, "the current object keeps its data")
	_, err = b.read("noncurrent", "null")
	assert.ErrorIs(t, err, ErrObjectNotFound)
}

// A delete while versioning is suspended puts a null delete marker over the
// null version and keeps the others; deleting the marker by its ID makes the
// version before it current. A copy of such a delete made before a null
// version written here does not remove it.
func TestASuspendedDeleteIsANullDeleteMarker(t *testing.T) {
	ctx := context.Background()
	b := newNullVersionBucket(t)
	b.versioning("Enabled")
	v1 := b.put("k", "v1")
	b.versioning("Suspended")
	b.put("k", "null")
	marker, err := b.om.DeleteObject(ctx, b.name, "k", false)
	require.NoError(t, err)
	assert.Equal(t, "null", marker)
	ids, latest := b.versions("k")
	assert.ElementsMatch(t, []string{"null", v1}, ids)
	assert.Equal(t, "null", latest)
	_, err = b.read("k", "")
	assert.Error(t, err, "the marker hides the key")
	_, err = b.om.DeleteObject(ctx, b.name, "k", false, "null")
	require.NoError(t, err)
	data, err := b.read("k", "")
	require.NoError(t, err)
	assert.Equal(t, "v1", data)

	b.put("later", "written after the delete")
	before := WithReplicatedLastModified(WithReplicatedVersionID(ctx, "null"), time.Now().Add(-time.Hour))
	_, err = b.om.DeleteObject(before, b.name, "later", false)
	require.NoError(t, err)
	data, err = b.read("later", "")
	require.NoError(t, err)
	assert.Equal(t, "written after the delete", data, "an older delete does not remove a null version written after it")
}
