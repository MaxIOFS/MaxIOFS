package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A legacy replica transfer carries the primary's stored lock state. The
// replica keeps it as it is: no bucket default added, no date refused.
func TestHAReceivePutKeepsTheSourceObjectLock(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	days := 1
	locked, plain := "ha-replica-lock", "ha-replica-lock-plain"
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: locked, OwnerID: "admin",
		Versioning: &metadata.VersioningMetadata{Enabled: true, Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true, Rule: &metadata.ObjectLockRuleMetadata{
			DefaultRetention: &metadata.RetentionMetadata{Mode: object.RetentionModeGovernance, Days: &days},
		}},
	}))
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: plain, OwnerID: "admin"}))

	replicate := func(t *testing.T, bucket, key string, marked bool, lock map[string]string) *object.Object {
		t.Helper()
		req := httptest.NewRequest("PUT", "/api/internal/ha/objects/"+key, strings.NewReader("replica"))
		req = mux.SetURLVars(req, map[string]string{"key": key})
		req.Header.Set(cluster.HABucketHeader, bucket)
		if marked {
			req.Header.Set(cluster.HAObjectLockHeader, "true")
		}
		for k, v := range lock {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		server.handleHAReceivePut(w, req)
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
		obj, reader, err := server.objectManager.GetObject(ctx, bucket, key)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		return obj
	}
	retention := func(mode string, until time.Time) map[string]string {
		return map[string]string{
			"x-amz-object-lock-mode":              mode,
			"x-amz-object-lock-retain-until-date": until.UTC().Format(time.RFC3339Nano),
		}
	}

	t.Run("retention and legal hold", func(t *testing.T) {
		until := time.Now().Add(48*time.Hour + 123456789)
		lock := retention(object.RetentionModeCompliance, until)
		lock["x-amz-object-lock-legal-hold"] = object.LegalHoldStatusOn
		obj := replicate(t, locked, "held", true, lock)
		require.NotNil(t, obj.Retention)
		assert.Equal(t, object.RetentionModeCompliance, obj.Retention.Mode)
		assert.True(t, obj.Retention.RetainUntilDate.Equal(until), "stored %v for %v", obj.Retention.RetainUntilDate, until)
		require.NotNil(t, obj.LegalHold)
		assert.Equal(t, object.LegalHoldStatusOn, obj.LegalHold.Status)
	})

	t.Run("a date that passed in transit", func(t *testing.T) {
		until := time.Now().Add(-time.Second)
		obj := replicate(t, locked, "expired", true, retention(object.RetentionModeCompliance, until))
		require.NotNil(t, obj.Retention)
		assert.True(t, obj.Retention.RetainUntilDate.Equal(until))
	})

	t.Run("no lock state adds no bucket default", func(t *testing.T) {
		obj := replicate(t, locked, "unlocked", true, nil)
		assert.Nil(t, obj.Retention)
		require.NotNil(t, obj.LegalHold)
		assert.Equal(t, object.LegalHoldStatusOff, obj.LegalHold.Status)
	})

	t.Run("bucket config not caught up yet", func(t *testing.T) {
		until := time.Now().Add(time.Hour)
		obj := replicate(t, plain, "held", true, retention(object.RetentionModeGovernance, until))
		require.NotNil(t, obj.Retention)
		assert.True(t, obj.Retention.RetainUntilDate.Equal(until))
	})

	t.Run("an older primary sends no state: the bucket default applies", func(t *testing.T) {
		obj := replicate(t, locked, "legacy", false, nil)
		require.NotNil(t, obj.Retention)
		assert.Equal(t, object.RetentionModeGovernance, obj.Retention.Mode)
	})
}

// A copy served to a peer carries the object's lock state and marks it complete.
func TestHAGetObjectSendsObjectLock(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	bucketName := "ha-get-lock"
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: bucketName, OwnerID: "admin",
		Versioning: &metadata.VersioningMetadata{Enabled: true, Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true},
	}))
	until := time.Now().Add(48 * time.Hour).UTC()
	h := http.Header{}
	h.Set("x-amz-object-lock-mode", object.RetentionModeCompliance)
	h.Set("x-amz-object-lock-retain-until-date", until.Format(time.RFC3339Nano))
	h.Set("x-amz-object-lock-legal-hold", object.LegalHoldStatusOn)
	_, err := server.objectManager.PutObject(ctx, bucketName, "held", strings.NewReader("locked"), h)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/internal/ha/objects/held?bucket="+bucketName, nil)
	req = mux.SetURLVars(req, map[string]string{"key": "held"})
	w := httptest.NewRecorder()
	server.handleHAGetObject(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, "true", w.Header().Get(cluster.HAObjectLockHeader))
	assert.Equal(t, object.RetentionModeCompliance, w.Header().Get("x-amz-object-lock-mode"))
	sent, err := time.Parse(time.RFC3339, w.Header().Get("x-amz-object-lock-retain-until-date"))
	require.NoError(t, err)
	assert.True(t, sent.Equal(until), "sent %v for %v", sent, until)
	assert.Equal(t, object.LegalHoldStatusOn, w.Header().Get("x-amz-object-lock-legal-hold"))
}

// A metadata change a replica cannot apply answers 4xx, so the sender drops it
// instead of retrying: 404 for an object that is gone, 400 for a malformed
// change, 409 for one the object's lock state refuses.
func TestHAMetadataOpAnswersWhetherItCanEverApply(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	bucketName := "ha-metadata-op-status"
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: bucketName, OwnerID: "admin",
		Versioning: &metadata.VersioningMetadata{Enabled: true, Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true},
	}))
	h := http.Header{}
	h.Set("x-amz-object-lock-mode", object.RetentionModeCompliance)
	h.Set("x-amz-object-lock-retain-until-date", time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339))
	_, err := server.objectManager.PutObject(ctx, bucketName, "locked", strings.NewReader("x"), h)
	require.NoError(t, err)

	send := func(op string) int {
		req := httptest.NewRequest("POST", "/api/internal/cluster/ha/metadata-op", strings.NewReader(op))
		req.Header.Set(cluster.HABucketHeader, bucketName)
		w := httptest.NewRecorder()
		server.handleHAReceiveMetadataOp(w, req)
		return w.Code
	}
	shorter := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	assert.Equal(t, http.StatusNotFound, send(`{"op":"set-legal-hold","key":"missing","data":{"status":"ON"}}`))
	assert.Equal(t, http.StatusBadRequest, send(`{"op":"set-legal-hold","key":"locked","data":"not an object"}`))
	assert.Equal(t, http.StatusConflict, send(`{"op":"set-retention","key":"locked","data":{"mode":"COMPLIANCE","retainUntilDate":"`+shorter+`"}}`))
	assert.Equal(t, http.StatusNoContent, send(`{"op":"set-legal-hold","key":"locked","data":{"status":"ON"}}`))
}

// A replicated delete marker carries its time: one older than the key's latest
// version is kept as an older version and leaves the key visible.
func TestHAReceiveDeleteKeepsAnOlderMarkerBehindTheLatest(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	bucketName := "ha-marker-order"
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: bucketName, OwnerID: "admin",
		Versioning: &metadata.VersioningMetadata{Enabled: true, Status: "Enabled"},
	}))
	_, err := server.objectManager.PutObject(ctx, bucketName, "k", strings.NewReader("live"), http.Header{})
	require.NoError(t, err)

	req := httptest.NewRequest("DELETE", "/api/internal/ha/objects/k", nil)
	req = mux.SetURLVars(req, map[string]string{"key": "k"})
	req.Header.Set(cluster.HABucketHeader, bucketName)
	// An ID that sorts after the object's, so only the time can order them.
	req.Header.Set(cluster.HADeleteMarkerVersionHeader, "9000000000000000000.ffffffff")
	req.Header.Set(cluster.HALastModifiedHeader, fmt.Sprintf("%d", time.Now().Add(-24*time.Hour).Unix()))
	w := httptest.NewRecorder()
	server.handleHAReceiveDelete(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	_, reader, err := server.objectManager.GetObject(ctx, bucketName, "k")
	require.NoError(t, err, "an older marker does not hide the key")
	require.NoError(t, reader.Close())
	versions, err := server.objectManager.GetObjectVersions(ctx, bucketName, "k")
	require.NoError(t, err)
	require.Len(t, versions, 2)
}

// A copy sent by the transfer that carries headers keeps the time the node
// that took the write wrote it, an object and a delete marker alike.
func TestHAReceiveKeepsTheTimeOfTheWrite(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	bucketName := "ha-written-at"
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: bucketName, OwnerID: "admin",
		Versioning: &metadata.VersioningMetadata{Enabled: true, Status: "Enabled"},
	}))
	const writtenAt = int64(1_700_000_000_123_456_789)

	req := httptest.NewRequest("PUT", "/api/internal/ha/objects/k", strings.NewReader("copy"))
	req = mux.SetURLVars(req, map[string]string{"key": "k"})
	req.Header.Set(cluster.HABucketHeader, bucketName)
	req.Header.Set(cluster.HAObjectVersionHeader, "1700000000123456789.aaaaaaaa")
	req.Header.Set(cluster.HALastModifiedHeader, "1700000000")
	req.Header.Set(cluster.HAWrittenAtHeader, fmt.Sprintf("%d", writtenAt))
	w := httptest.NewRecorder()
	server.handleHAReceivePut(w, req)
	require.Less(t, w.Code, 300, w.Body.String())
	stored, err := server.metadataStore.GetObject(ctx, bucketName, "k")
	require.NoError(t, err)
	assert.Equal(t, writtenAt, stored.WrittenAt)

	req = httptest.NewRequest("DELETE", "/api/internal/ha/objects/k", nil)
	req = mux.SetURLVars(req, map[string]string{"key": "k"})
	req.Header.Set(cluster.HABucketHeader, bucketName)
	req.Header.Set(cluster.HADeleteMarkerVersionHeader, "1700000001000000005.bbbbbbbb")
	req.Header.Set(cluster.HALastModifiedHeader, "1700000001")
	req.Header.Set(cluster.HAWrittenAtHeader, "1700000001000000005")
	w = httptest.NewRecorder()
	server.handleHAReceiveDelete(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
	versions, err := server.metadataStore.GetObjectVersions(ctx, bucketName, "k")
	require.NoError(t, err)
	var marker int64
	for _, v := range versions {
		if v.VersionID == "1700000001000000005.bbbbbbbb" {
			marker = v.WrittenAt
		}
	}
	assert.EqualValues(t, 1700000001000000005, marker)
}

// The locations of a copy's write and the number of the change that set them
// are kept when it is received and sent with it when it is served.
func TestTheLocationsOfACopyTravelWithIt(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	bucketName := "ha-locations-gen"
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucketName, OwnerID: "admin"}))
	// The copy names this node, whether or not another test put it in a cluster.
	self := "b"
	if config, err := server.clusterManager.GetConfig(ctx); err == nil && config.IsClusterEnabled {
		self = config.NodeID
	}

	req := httptest.NewRequest("PUT", "/api/internal/ha/objects/k", strings.NewReader("copy"))
	req = mux.SetURLVars(req, map[string]string{"key": "k"})
	req.Header.Set(cluster.HABucketHeader, bucketName)
	req.Header.Set(cluster.HALastModifiedHeader, "1700000000")
	req.Header.Set(cluster.HALocationsHeader, "a,"+self)
	req.Header.Set(cluster.HALocationsGenHeader, "2")
	w := httptest.NewRecorder()
	server.handleHAReceivePut(w, req)
	require.Less(t, w.Code, 300, w.Body.String())
	stored, err := server.metadataStore.GetObject(ctx, bucketName, "k")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", self}, stored.Locations)
	assert.EqualValues(t, 2, stored.LocationsGen)

	req = httptest.NewRequest("GET", "/api/internal/ha/objects/k?bucket="+bucketName, nil)
	req = mux.SetURLVars(req, map[string]string{"key": "k"})
	w = httptest.NewRecorder()
	server.handleHAGetObject(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "a,"+self, w.Header().Get(cluster.HALocationsHeader))
	assert.Equal(t, "2", w.Header().Get(cluster.HALocationsGenHeader))
}

// A delete from another node answers done when the key is gone here, refused
// when its lock keeps it, failed otherwise.
func TestHAReceiveDeleteSaysWhatCameOfIt(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	bucketName := "ha-delete-outcome"
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: bucketName, OwnerID: "admin",
		Versioning: &metadata.VersioningMetadata{Enabled: true, Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true},
	}))
	held, err := server.objectManager.PutObject(ctx, bucketName, "held", strings.NewReader("data"), http.Header{"X-Amz-Object-Lock-Legal-Hold": {"ON"}})
	require.NoError(t, err)
	send := func(key, versionID string) int {
		req := httptest.NewRequest("DELETE", "/api/internal/ha/objects/"+key, nil)
		req = mux.SetURLVars(req, map[string]string{"key": key})
		req.Header.Set(cluster.HABucketHeader, bucketName)
		if versionID != "" {
			req.Header.Set(cluster.HAObjectVersionHeader, versionID)
		}
		w := httptest.NewRecorder()
		server.handleHAReceiveDelete(w, req)
		return w.Code
	}
	assert.Equal(t, http.StatusConflict, send("held", held.VersionID), "held by its legal hold")
	assert.Equal(t, http.StatusNoContent, send("never-written", ""), "already gone")
	assert.Equal(t, http.StatusInternalServerError, haDeleteStatus(errors.New("disk failure")))
}

// A copy of a write made before this node deleted the key, one without a
// version, is not stored: the node that sends it missed the delete. A copy of
// a write made in the second of the delete or later is, whole or entry only.
func TestACopyOfAWriteBeforeADeletionIsNotStored(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "deleted-keys", "admin"))
	deletedAt := time.Now().Unix()
	for _, key := range []string{"whole", "entry"} {
		require.NoError(t, cluster.RecordDeletion(ctx, s.db, cluster.EntityTypeObject,
			cluster.ObjectTombstoneID("deleted-keys", key), "peer", deletedAt))
	}

	put := func(at int64) int {
		req := httptest.NewRequest(http.MethodPut, "/api/internal/ha/objects/whole", strings.NewReader("copy"))
		req = mux.SetURLVars(req, map[string]string{"key": "whole"})
		req.Header.Set(cluster.HABucketHeader, "deleted-keys")
		req.Header.Set(cluster.HALastModifiedHeader, fmt.Sprintf("%d", at))
		w := httptest.NewRecorder()
		s.handleHAReceivePut(w, req)
		return w.Code
	}
	require.Equal(t, http.StatusNoContent, put(deletedAt-60))
	assert.False(t, holdsObject(s, "deleted-keys", "whole"), "written before the deletion")
	require.Equal(t, http.StatusNoContent, put(deletedAt))
	assert.True(t, holdsObject(s, "deleted-keys", "whole"), "written in the second of the deletion")

	entry := func(at int64) int {
		body := fmt.Sprintf(`{"bucket":"deleted-keys","key":"entry","size":4,"etag":"e4","last_modified":%q,"locations":["peer"]}`,
			time.Unix(at, 0).UTC().Format(time.RFC3339))
		w := httptest.NewRecorder()
		s.handleHAReceiveObjectEntry(w, httptest.NewRequest(http.MethodPut, "/api/internal/cluster/ha/object-entry", strings.NewReader(body)))
		return w.Code
	}
	w := httptest.NewRecorder()
	s.handleHAReceiveObjectEntry(w, httptest.NewRequest(http.MethodPut, "/api/internal/cluster/ha/object-entry",
		strings.NewReader(`{"bucket":"deleted-keys","key":"entry","size":4,"etag":"e4"}`)))
	require.Equal(t, http.StatusBadRequest, w.Code, "an entry names the nodes holding its data")
	require.Equal(t, http.StatusNoContent, entry(deletedAt-60))
	assert.False(t, holdsObject(s, "deleted-keys", "entry"))
	require.Equal(t, http.StatusNoContent, entry(deletedAt+1))
	assert.True(t, holdsObject(s, "deleted-keys", "entry"))
}
