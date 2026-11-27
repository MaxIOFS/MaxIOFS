package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bucketSetting is one setting of a bucket the console reads, sets and
// removes.
type bucketSetting struct {
	name                string
	get, put, remove    http.HandlerFunc
	body                string
	shown               string // what the setting shows once set
	stored, removedOnly func(*bucket.Bucket) bool
}

// A bucket's encryption, public access block and website are read, set and
// removed by who may configure the bucket; one that does not exist is not
// found, and a user who may not configure it changes nothing.
func TestABucketsSettingsAreKeptByWhoConfiguresIt(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "configured", tenantAdmin.ID))
	stored := func() *bucket.Bucket {
		info, err := s.bucketManager.GetBucketInfo(ctx, "acme", "configured")
		require.NoError(t, err)
		return info
	}
	for _, setting := range []bucketSetting{
		{
			name: "encryption", get: s.handleGetBucketEncryption, put: s.handlePutBucketEncryption, remove: s.handleDeleteBucketEncryption,
			body: `{"type":"AES256"}`, shown: "AES256",
			stored:      func(b *bucket.Bucket) bool { return b.Encryption != nil && b.Encryption.Type == "AES256" },
			removedOnly: func(b *bucket.Bucket) bool { return b.Encryption == nil },
		},
		{
			name: "public access block", get: s.handleGetPublicAccessBlock, put: s.handlePutPublicAccessBlock, remove: s.handleDeletePublicAccessBlock,
			body: `{"blockPublicAcls":true,"ignorePublicAcls":false,"blockPublicPolicy":true,"restrictPublicBuckets":false}`, shown: "blockPublicPolicy",
			stored: func(b *bucket.Bucket) bool {
				return b.PublicAccessBlock != nil && b.PublicAccessBlock.BlockPublicPolicy && !b.PublicAccessBlock.IgnorePublicAcls
			},
			removedOnly: func(b *bucket.Bucket) bool {
				return b.PublicAccessBlock == nil || !b.PublicAccessBlock.BlockPublicPolicy || b.PublicAccessBlock.IgnorePublicAcls
			},
		},
		{
			name: "website", get: s.handleGetBucketWebsite, put: s.handlePutBucketWebsite, remove: s.handleDeleteBucketWebsite,
			body: `{"indexDocument":"index.html","errorDocument":"404.html"}`, shown: "index.html",
			stored:      func(b *bucket.Bucket) bool { return b.Website != nil && b.Website.IndexDocument == "index.html" },
			removedOnly: func(b *bucket.Bucket) bool { return b.Website == nil },
		},
	} {
		t.Run(setting.name, func(t *testing.T) {
			call := func(user *auth.User, handler http.HandlerFunc, name, body string, actions ...string) *httptest.ResponseRecorder {
				req := mux.SetURLVars(consoleRequestWith(user, body, actions...), map[string]string{"bucket": name})
				rec := httptest.NewRecorder()
				handler(rec, req)
				return rec
			}
			rec := httptest.NewRecorder()
			setting.put(rec, mux.SetURLVars(httptest.NewRequest(http.MethodPut, "/", nil), map[string]string{"bucket": "configured"}))
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "nobody signed in")
			assert.Equal(t, http.StatusForbidden, call(tenantAdmin, setting.put, "configured", setting.body).Code, "allowed nothing")
			assert.Equal(t, http.StatusForbidden, call(tenantAdmin, setting.remove, "configured", "").Code, "allowed nothing")
			assert.False(t, setting.stored(stored()), "a refused request sets nothing")

			rec = call(tenantAdmin, setting.put, "configured", setting.body, "s3:*")
			require.Less(t, rec.Code, 300, rec.Body.String())
			assert.True(t, setting.stored(stored()))
			rec = call(tenantAdmin, setting.get, "configured", "", "s3:*")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), setting.shown)

			rec = call(tenantAdmin, setting.remove, "configured", "", "s3:*")
			require.Less(t, rec.Code, 300, rec.Body.String())
			assert.True(t, setting.removedOnly(stored()))

			assert.Equal(t, http.StatusNotFound, call(tenantAdmin, setting.put, "nowhere", setting.body, "s3:*").Code)
			assert.Equal(t, http.StatusBadRequest, call(tenantAdmin, setting.put, "configured", `not json`, "s3:*").Code)
		})
	}
}
