package server

// A global administrator reads a tenant's buckets and does not change them.
// The console mutation routes used to ask only for the bucket:configure
// capability, which says nothing about whose bucket it is.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tenantOwnedBucket creates a tenant and one bucket of its own.
func tenantOwnedBucket(t *testing.T, tenantID, bucketName string) {
	t.Helper()
	server := getSharedServer()
	ctx := t.Context()

	cleanupTestData(t, tenantID, bucketName)
	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{
		ID: tenantID, Name: tenantID, Status: "active",
		MaxStorageBytes: 1 << 30, MaxBuckets: 10, MaxAccessKeys: 10,
	}))
	require.NoError(t, server.bucketManager.CreateBucket(ctx, tenantID, bucketName, ""))
}

// consoleMutation is one console route that changes a bucket's configuration.
type consoleMutation struct {
	name    string
	body    string
	handler func(*Server) http.HandlerFunc
}

func consoleMutations() []consoleMutation {
	return []consoleMutation{
		{"website", `{"indexDocument":"index.html"}`,
			func(s *Server) http.HandlerFunc { return s.handlePutBucketWebsite }},
		{"lifecycle", `<LifecycleConfiguration><Rule><ID>r</ID><Status>Enabled</Status><Prefix></Prefix><Expiration><Days>30</Days></Expiration></Rule></LifecycleConfiguration>`,
			func(s *Server) http.HandlerFunc { return s.handlePutBucketLifecycle }},
		{"versioning", `{"status":"Enabled"}`,
			func(s *Server) http.HandlerFunc { return s.handlePutBucketVersioning }},
		{"encryption", `{"algorithm":"AES256"}`,
			func(s *Server) http.HandlerFunc { return s.handlePutBucketEncryption }},
		{"public-access-block", `{"blockPublicAcls":true}`,
			func(s *Server) http.HandlerFunc { return s.handlePutPublicAccessBlock }},
	}
}

func TestConsole_GlobalAdminCannotChangeATenantsBucket(t *testing.T) {
	server := getSharedServer()
	tenantID := "tenant-boundary-a"
	bucketName := "boundary-bucket-a"
	tenantOwnedBucket(t, tenantID, bucketName)

	for _, m := range consoleMutations() {
		t.Run(m.name, func(t *testing.T) {
			// No tenant of its own, and the admin role: a global administrator.
			req := createAuthenticatedRequest("PUT", "/api/v1/buckets/"+bucketName+"/"+m.name,
				strings.NewReader(m.body), "", "global-admin-1", true)
			req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})

			rr := httptest.NewRecorder()
			m.handler(server)(rr, req)

			assert.Equal(t, http.StatusForbidden, rr.Code,
				"a global administrator changed a tenant's bucket: %s", rr.Body.String())
		})
	}
}

// The same routes, for the tenant that owns the bucket: anything but a refusal.
// Tying the routes to an S3 action would take the owner's access away with it if
// the action were not one the tenant administrator holds.
func TestConsole_TenantAdminStillConfiguresItsOwnBucket(t *testing.T) {
	server := getSharedServer()
	tenantID := "tenant-boundary-b"
	bucketName := "boundary-bucket-b"
	tenantOwnedBucket(t, tenantID, bucketName)

	for _, m := range consoleMutations() {
		t.Run(m.name, func(t *testing.T) {
			req := createAuthenticatedRequest("PUT", "/api/v1/buckets/"+bucketName+"/"+m.name,
				strings.NewReader(m.body), tenantID, "tenant-admin-1", true)
			req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})

			rr := httptest.NewRecorder()
			m.handler(server)(rr, req)

			assert.NotEqual(t, http.StatusForbidden, rr.Code,
				"the bucket's own tenant was refused: %s", rr.Body.String())
		})
	}
}

// A global bucket belongs to the deployment, so the global administrator owns it.
func TestConsole_GlobalAdminStillConfiguresAGlobalBucket(t *testing.T) {
	server := getSharedServer()
	bucketName := "boundary-bucket-global"

	cleanupTestData(t, "", bucketName)
	require.NoError(t, server.bucketManager.CreateBucket(t.Context(), "", bucketName, ""))

	for _, m := range consoleMutations() {
		t.Run(m.name, func(t *testing.T) {
			req := createAuthenticatedRequest("PUT", "/api/v1/buckets/"+bucketName+"/"+m.name,
				strings.NewReader(m.body), "", "global-admin-2", true)
			req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})

			rr := httptest.NewRecorder()
			m.handler(server)(rr, req)

			assert.NotEqual(t, http.StatusForbidden, rr.Code,
				"the deployment's own bucket was refused: %s", rr.Body.String())
		})
	}
}
