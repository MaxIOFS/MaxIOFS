package s3compat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asUserWith runs a request as a user whose only permissions are actions on
// bucket and its objects.
func (env *s3TestEnv) asUserWith(req *http.Request, bucket, key string, actions ...string) *http.Request {
	user := &auth.User{ID: env.userID, TenantID: env.tenantID}
	list, _ := json.Marshal(actions)
	doc := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":%s,"Resource":["arn:aws:s3:::%s","arn:aws:s3:::%s/*"]}]}`,
		list, bucket, bucket)
	ctx := context.WithValue(req.Context(), "user", user)
	ctx = auth.WithPolicySet(ctx, &auth.PolicySet{UserID: user.ID, TenantID: user.TenantID, Documents: []string{doc}})
	return mux.SetURLVars(req.WithContext(ctx), map[string]string{"bucket": bucket, "object": key})
}

func retentionHeaders(r *http.Request) {
	r.Header.Set("x-amz-object-lock-mode", "GOVERNANCE")
	r.Header.Set("x-amz-object-lock-retain-until-date", time.Now().Add(72*time.Hour).UTC().Format(time.RFC3339))
}

// Object Lock headers on a write need the permission to set them, as in AWS S3:
// s3:PutObjectRetention for retention, s3:PutObjectLegalHold for a legal hold.
// The bucket default retention needs neither.
func TestObjectLockHeadersNeedTheirOwnPermission(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()
	const b = "lock-perms"
	days := 1
	require.NoError(t, env.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: b, TenantID: env.tenantID, OwnerID: env.userID,
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true, Rule: &metadata.ObjectLockRuleMetadata{
			DefaultRetention: &metadata.RetentionMetadata{Mode: "GOVERNANCE", Days: &days},
		}},
	}))
	put := func(key string, lock func(*http.Request), actions ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/"+b+"/"+key, bytes.NewReader([]byte("data")))
		if lock != nil {
			lock(req)
		}
		w := httptest.NewRecorder()
		env.handler.PutObject(w, env.asUserWith(req, b, key, actions...))
		return w
	}
	legalHold := func(r *http.Request) { r.Header.Set("x-amz-object-lock-legal-hold", "ON") }

	t.Run("put", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, put("r1", retentionHeaders, "s3:PutObject").Code)
		assert.Equal(t, http.StatusOK, put("r2", retentionHeaders, "s3:PutObject", "s3:PutObjectRetention").Code)
		assert.Equal(t, http.StatusForbidden, put("h1", legalHold, "s3:PutObject", "s3:PutObjectRetention").Code)
		assert.Equal(t, http.StatusOK, put("h2", legalHold, "s3:PutObject", "s3:PutObjectLegalHold").Code)
		assert.Equal(t, http.StatusOK, put("d", nil, "s3:PutObject").Code, "the bucket default needs no extra permission")

		_, err := env.objectManager.GetObjectMetadata(ctx, env.tenantID+"/"+b, "r1")
		assert.Error(t, err, "a refused write stores nothing")
	})

	t.Run("create multipart upload", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/"+b+"/mp?uploads", nil)
		retentionHeaders(req)
		w := httptest.NewRecorder()
		env.handler.CreateMultipartUpload(w, env.asUserWith(req, b, "mp", "s3:PutObject"))
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("copy", func(t *testing.T) {
		copyWith := func(key string, actions ...string) *httptest.ResponseRecorder {
			req := httptest.NewRequest("PUT", "/"+b+"/"+key, nil)
			req.Header.Set("x-amz-copy-source", "/"+b+"/d")
			retentionHeaders(req)
			w := httptest.NewRecorder()
			env.handler.CopyObject(w, env.asUserWith(req, b, key, actions...))
			return w
		}
		assert.Equal(t, http.StatusForbidden, copyWith("c1", "s3:GetObject", "s3:PutObject").Code)
		w := copyWith("c2", "s3:GetObject", "s3:PutObject", "s3:PutObjectRetention")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		copied, err := env.objectManager.GetObjectMetadata(ctx, env.tenantID+"/"+b, "c2")
		require.NoError(t, err)
		require.NotNil(t, copied.Retention)
		assert.True(t, copied.Retention.RetainUntilDate.After(time.Now().Add(48*time.Hour)),
			"the copy carries the retention the request asked for, not the one-day bucket default")
	})
}
