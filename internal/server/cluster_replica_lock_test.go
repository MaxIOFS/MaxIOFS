package server

import (
	"context"
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
