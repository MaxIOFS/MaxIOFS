package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A global administrator shares an object of a tenant's bucket named with its
// tenant, and the share is that tenant's; a bucket of no tenant is found
// whatever tenant is named.
func TestAGlobalAdministratorSharesInTheBucketsTenant(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	tenantAdmin, _, _, _ := userUpdateFixture(t, s)
	root := consoleUserOf(t, s, "root", "", auth.RoleAdmin)
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "acme", "files", tenantAdmin.ID))
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "common", root.ID))
	for _, bucketPath := range []string{"acme/files", "common"} {
		_, err := s.objectManager.PutObject(ctx, bucketPath, "doc", bytes.NewReader([]byte("x")), http.Header{})
		require.NoError(t, err)
	}
	_, err := s.authManager.GenerateAccessKey(ctx, root.ID)
	require.NoError(t, err)
	share := func(bucketName, query string) int {
		req := mux.SetURLVars(consoleRequestWith(root, `{"expiresIn":60}`, "s3:*"), map[string]string{"bucket": bucketName, "object": "doc"})
		req.URL.RawQuery = query
		rec := httptest.NewRecorder()
		s.handleShareObject(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusNotFound, share("files", ""), "a tenant's bucket is named with its tenant")
	assert.Equal(t, http.StatusNotFound, share("files", "tenantId=beta"))
	require.Equal(t, http.StatusOK, share("files", "tenantId=acme"))
	stored, err := s.shareManager.GetShareByObject(ctx, "files", "doc", "acme")
	require.NoError(t, err)
	assert.Equal(t, "acme", stored.TenantID)
	assert.Equal(t, http.StatusOK, share("common", "tenantId=beta"), "a bucket of no tenant")
}
