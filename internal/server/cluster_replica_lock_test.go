package server

import (
	"context"
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
