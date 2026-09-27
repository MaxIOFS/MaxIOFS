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
