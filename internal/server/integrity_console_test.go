package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrityFixture is a bucket of no tenant holding a and b, b changed on
// disk, and acme's bucket pics holding c; acme's administrator is returned.
func integrityFixture(t *testing.T, s *Server) *auth.User {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", Status: "active"}))
	tenantAdmin := consoleUserOf(t, s, "acme-admin", "acme", auth.RoleTenantAdmin)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "plain", ""))
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "pics", tenantAdmin.ID))
	for bucketPath, keys := range map[string][]string{"plain": {"a", "b"}, "acme/pics": {"c"}} {
		for _, key := range keys {
			_, err := s.objectManager.PutObject(ctx, bucketPath, key, bytes.NewReader(bytes.Repeat([]byte(key), 64<<10)), http.Header{})
			require.NoError(t, err)
		}
	}
	fs, ok := s.storageBackend.(*storage.FilesystemBackend)
	require.True(t, ok)
	path := filepath.Join(fs.GetRootPath(), filepath.FromSlash(fs.ObjectPath(storage.ObjectRef{Bucket: "plain", Key: "b"})))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)/2] ^= 0x40
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return tenantAdmin
}

// integrityCall is handler's answer to user, or to a request of no session.
func integrityCall(user *auth.User, handler http.HandlerFunc, method, query, bucket, body string) *httptest.ResponseRecorder {
	req := requestAs(user, map[string]string{"method": method}, map[string]string{"bucket": bucket}, body)
	if user == nil {
		req = mux.SetURLVars(httptest.NewRequest(method, "/", strings.NewReader(body)), map[string]string{"bucket": bucket})
	}
	req.URL.RawQuery = query
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func integrityHistory(t *testing.T, s *Server, bucketPath string) []*LastScanRecord {
	t.Helper()
	history, err := s.getIntegrityHistory(context.Background(), bucketPath)
	require.NoError(t, err)
	return history
}

// A global administrator verifies a bucket, any tenant's, page by page; a
// second scan within the hour of a manual one is refused.
func TestABucketIsVerifiedByAGlobalAdministrator(t *testing.T) {
	s := newClusterTestNode(t)
	tenantAdmin := integrityFixture(t, s)
	admin, err := s.authManager.GetUser(context.Background(), "admin")
	require.NoError(t, err)
	verify := func(user *auth.User, bucket, query string) *httptest.ResponseRecorder {
		return integrityCall(user, s.handleVerifyBucketIntegrity, http.MethodPost, query, bucket, "")
	}

	assert.Equal(t, http.StatusUnauthorized, verify(nil, "plain", "").Code)
	assert.Equal(t, http.StatusForbidden, verify(tenantAdmin, "pics", "").Code)

	report := consoleData[object.BucketIntegrityReport](t, verify(admin, "plain", ""), http.StatusOK)
	assert.Equal(t, 2, report.Checked)
	assert.Equal(t, 1, report.OK)
	assert.Equal(t, 1, report.Corrupted)
	require.Len(t, report.Issues, 1)
	assert.Equal(t, "b", report.Issues[0].Key)
	assert.Equal(t, object.IntegrityCorrupted, report.Issues[0].Status)

	tenants := consoleData[object.BucketIntegrityReport](t, verify(admin, "pics", "tenantId=acme"), http.StatusOK)
	assert.Equal(t, 1, tenants.Checked)
	assert.Equal(t, 1, tenants.OK)

	first := consoleData[object.BucketIntegrityReport](t, verify(admin, "plain", "maxKeys=1"), http.StatusOK)
	assert.Equal(t, 1, first.Checked)
	require.NotEmpty(t, first.NextMarker)
	next := consoleData[object.BucketIntegrityReport](t, verify(admin, "plain", "maxKeys=1&marker="+first.NextMarker), http.StatusOK)
	assert.Equal(t, 1, next.Checked)
	assert.Empty(t, next.NextMarker)
	for _, query := range []string{"maxKeys=0", "maxKeys=x", "maxKeys=99999"} {
		assert.Equal(t, 2, consoleData[object.BucketIntegrityReport](t, verify(admin, "plain", query), http.StatusOK).Checked, query)
	}

	save := func(user *auth.User, body string) *httptest.ResponseRecorder {
		return integrityCall(user, s.handleSaveIntegrityStatus, http.MethodPost, "", "plain", body)
	}
	assert.Equal(t, http.StatusUnauthorized, save(nil, `{}`).Code)
	assert.Equal(t, http.StatusForbidden, save(tenantAdmin, `{}`).Code)
	assert.Equal(t, http.StatusBadRequest, save(admin, `not json`).Code)
	body, err := json.Marshal(report)
	require.NoError(t, err)
	consoleData[map[string]bool](t, save(admin, string(body)), http.StatusOK)
	history := integrityHistory(t, s, "plain")
	require.Len(t, history, 1)
	assert.Equal(t, "manual", history[0].Source)
	assert.Equal(t, 1, history[0].Corrupted)

	assert.Equal(t, http.StatusTooManyRequests, verify(admin, "plain", "").Code, "a scan within the hour")
	assert.Equal(t, http.StatusOK, verify(admin, "plain", "marker="+first.NextMarker).Code, "a scan's next page")
	assert.Equal(t, http.StatusOK, verify(admin, "pics", "tenantId=acme").Code, "another bucket")
}

// verifyFailing is an object manager whose verification of one bucket fails.
type verifyFailing struct {
	object.Manager
	bucket string
}

func (v verifyFailing) VerifyBucketIntegrity(ctx context.Context, bucket, prefix, marker string, maxKeys int) (*object.BucketIntegrityReport, error) {
	if bucket == v.bucket {
		return nil, errors.New("the bucket's objects could not be listed")
	}
	return v.Manager.(interface {
		VerifyBucketIntegrity(ctx context.Context, bucket, prefix, marker string, maxKeys int) (*object.BucketIntegrityReport, error)
	}).VerifyBucketIntegrity(ctx, bucket, prefix, marker, maxKeys)
}

// The scrubber verifies every bucket of every tenant and keeps each result; a
// bucket it could not verify is not kept as a clean one.
func TestTheScrubberVerifiesEveryBucket(t *testing.T) {
	s := newClusterTestNode(t)
	integrityFixture(t, s)
	ctx := context.Background()
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "unreadable", ""))
	s.objectManager = verifyFailing{Manager: s.objectManager, bucket: "unreadable"}

	require.NoError(t, s.runIntegrityScrub(ctx))
	plain := integrityHistory(t, s, "plain")
	require.Len(t, plain, 1)
	assert.Equal(t, "scrubber", plain[0].Source)
	assert.Equal(t, 2, plain[0].Checked)
	assert.Equal(t, 1, plain[0].Corrupted)
	require.Len(t, plain[0].Issues, 1)
	assert.Equal(t, "b", plain[0].Issues[0].Key)
	pics := integrityHistory(t, s, "acme/pics")
	require.Len(t, pics, 1)
	assert.Equal(t, 1, pics[0].OK)
	unreadable := integrityHistory(t, s, "unreadable")
	require.Len(t, unreadable, 1)
	assert.Equal(t, 1, unreadable[0].Errors, "the scan of it failed")

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	assert.ErrorIs(t, s.runIntegrityScrub(cancelled), context.Canceled)
	assert.Len(t, integrityHistory(t, s, "plain"), 1, "a scan cut short keeps nothing")
}
