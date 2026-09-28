package object

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

func replicaCtx(versionID string, modified time.Time) context.Context {
	return WithReplicatedLastModified(WithReplicatedVersionID(context.Background(), versionID), modified)
}

func latestVersionID(t *testing.T, s metadata.Store, bucket, key string) string {
	t.Helper()
	obj, err := s.GetObject(context.Background(), bucket, key)
	require.NoError(t, err)
	return obj.VersionID
}

// Copies of versions from another node may arrive out of order or twice. The
// newest stays the latest, every version is kept, and usage counts each once.
func TestReplicatedVersionsLandInOrderWhateverOrderTheyArrive(t *testing.T) {
	m, s := setupAccountingManager(t)
	ctx := context.Background()
	const b = "landing"
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: b, OwnerID: "u", Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
	}))
	t0 := time.Now().Add(-time.Hour).Truncate(time.Second)
	put := func(versionID string, at time.Time, body string) {
		t.Helper()
		_, err := m.PutObject(replicaCtx(versionID, at), b, "k", strings.NewReader(body), http.Header{})
		require.NoError(t, err)
	}

	put("1700000002000000000.bbbbbbbb", t0.Add(2*time.Second), "newer")
	put("1700000001000000000.aaaaaaaa", t0.Add(time.Second), "older")
	require.Equal(t, "1700000002000000000.bbbbbbbb", latestVersionID(t, s, b, "k"), "an older copy does not replace the latest")
	versions, err := s.GetObjectVersions(ctx, b, "k")
	require.NoError(t, err)
	require.Len(t, versions, 2)

	// Same second: the version ID orders them.
	put("1700000002000000000.aaaaaaaa", t0.Add(2*time.Second), "same second, earlier ID")
	require.Equal(t, "1700000002000000000.bbbbbbbb", latestVersionID(t, s, b, "k"))

	// The same version again replaces itself; it is not counted twice.
	put("1700000002000000000.bbbbbbbb", t0.Add(2*time.Second), "newer")
	assertAccountingAgrees(t, m, s, b)

	// A delete marker older than the latest is kept as an older version.
	_, err = m.DeleteObject(replicaCtx("1700000001500000000.cccccccc", t0.Add(time.Second+500*time.Millisecond)), b, "k", false)
	require.NoError(t, err)
	require.Equal(t, "1700000002000000000.bbbbbbbb", latestVersionID(t, s, b, "k"))
	_, reader, err := m.GetObject(ctx, b, "k")
	require.NoError(t, err, "the key stays visible")
	data, err := io.ReadAll(reader)
	require.NoError(t, reader.Close())
	require.NoError(t, err)
	require.Equal(t, "newer", string(data))
	assertAccountingAgrees(t, m, s, b)

	// A newer delete marker hides the key, once.
	_, err = m.DeleteObject(replicaCtx("1700000003000000000.dddddddd", t0.Add(3*time.Second)), b, "k", false)
	require.NoError(t, err)
	_, err = m.DeleteObject(replicaCtx("1700000003000000000.dddddddd", t0.Add(3*time.Second)), b, "k", false)
	require.NoError(t, err)
	_, _, err = m.GetObject(ctx, b, "k")
	require.ErrorIs(t, err, ErrObjectNotFound)
	assertAccountingAgrees(t, m, s, b)

	// An older version arriving behind that marker leaves the key hidden and
	// adds bytes, not an object.
	put("1700000000500000000.eeeeeeee", t0.Add(500*time.Millisecond), "oldest")
	_, _, err = m.GetObject(ctx, b, "k")
	require.ErrorIs(t, err, ErrObjectNotFound)
	assertAccountingAgrees(t, m, s, b)
}

// Raw copies land the same way.
func TestRawReplicatedVersionsLandInOrder(t *testing.T) {
	src, _, srcStore := setupManagerWithConfigKey(t)
	dst, dstStore := setupAccountingManager(t)
	ctx := context.Background()
	const b = "rawlanding"
	for _, st := range []metadata.Store{srcStore, dstStore} {
		require.NoError(t, st.CreateBucket(ctx, &metadata.BucketMetadata{
			Name: b, OwnerID: "u", Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		}))
	}
	older, err := src.PutObject(ctx, b, "k", strings.NewReader("older"), http.Header{})
	require.NoError(t, err)
	time.Sleep(1100 * time.Millisecond)
	newer, err := src.PutObject(ctx, b, "k", strings.NewReader("newer"), http.Header{})
	require.NoError(t, err)

	copyRaw := func(versionID string) {
		t.Helper()
		reader, sidecar, metaObj, err := src.GetObjectRaw(ctx, b, "k", versionID)
		require.NoError(t, err)
		defer reader.Close()
		require.NoError(t, dst.PutObjectRaw(ctx, b, "k", reader, sidecar, metaObj))
	}
	copyRaw(newer.VersionID)
	copyRaw(older.VersionID)
	copyRaw(older.VersionID)
	require.Equal(t, newer.VersionID, latestVersionID(t, dstStore, b, "k"))
	versions, err := dstStore.GetObjectVersions(ctx, b, "k")
	require.NoError(t, err)
	require.Len(t, versions, 2)
	assertAccountingAgrees(t, dst, dstStore, b)
}
