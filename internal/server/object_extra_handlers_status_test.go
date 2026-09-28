package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A failed console write answers the status that names its cause; a failure to
// check a quota is not a quota refusal.
func TestWriteErrorStatus(t *testing.T) {
	tenantFull := fmt.Errorf("storage quota exceeded: %w", fmt.Errorf("%w: 20/16 bytes", auth.ErrStorageQuotaExceeded))
	checkFailed := fmt.Errorf("storage quota exceeded: %w", errors.New("failed to get tenant"))
	assert.Equal(t, http.StatusForbidden, writeErrorStatus(object.ErrBucketQuotaExceeded))
	assert.Equal(t, http.StatusForbidden, writeErrorStatus(tenantFull))
	assert.Equal(t, http.StatusInternalServerError, writeErrorStatus(checkFailed))
	assert.Equal(t, http.StatusNotFound, writeErrorStatus(object.ErrBucketNotFound))
	assert.Equal(t, http.StatusServiceUnavailable, writeErrorStatus(cluster.ErrClusterDegraded))
}

// A console upload refused by the tenant quota inside the write — after the
// handler's own check let an unknown length through — answers 403, not 500.
func TestConsoleUploadRefusedByTenantQuotaInsideTheWrite(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	tenantID, bucketName := "quota-console-tenant", "quota-console"
	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{
		ID: tenantID, Name: tenantID, Status: "active",
		MaxStorageBytes: 16, MaxBuckets: 10, MaxAccessKeys: 10,
	}))
	require.NoError(t, server.bucketManager.CreateBucket(ctx, tenantID, bucketName, ""))

	req := createAuthenticatedRequest("PUT", "/api/v1/buckets/"+bucketName+"/objects/big", bytes.NewReader(bytes.Repeat([]byte("x"), 32)), tenantID, "user-1", false)
	req.ContentLength = -1
	req = mux.SetURLVars(req, map[string]string{"bucket": bucketName, "object": "big"})
	rr := httptest.NewRecorder()
	server.handleUploadObject(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
}

// A console upload carrying Object Lock headers needs the permission to set
// them, as an S3 PUT does.
func TestConsoleUploadObjectLockHeadersNeedTheirOwnPermission(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	tenantID, bucketName := "lock-console-tenant", "lock-console"
	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{
		ID: tenantID, Name: tenantID, Status: "active", MaxBuckets: 10, MaxAccessKeys: 10,
	}))
	require.NoError(t, server.bucketManager.CreateBucket(ctx, tenantID, bucketName, ""))
	meta, err := server.metadataStore.GetBucket(ctx, tenantID, bucketName)
	require.NoError(t, err)
	meta.Versioning = &metadata.VersioningMetadata{Enabled: true, Status: "Enabled"}
	meta.ObjectLock = &metadata.ObjectLockMetadata{Enabled: true}
	require.NoError(t, server.metadataStore.UpdateBucket(ctx, meta))

	upload := func(key string, lock map[string]string, actions ...string) int {
		req := httptest.NewRequest("PUT", "/api/v1/buckets/"+bucketName+"/objects/"+key, bytes.NewReader([]byte("x")))
		for k, v := range lock {
			req.Header.Set(k, v)
		}
		user := &auth.User{ID: "lock-user", TenantID: tenantID, Username: "lock-user"}
		list := `"` + strings.Join(actions, `","`) + `"`
		doc := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":[` + list + `],"Resource":"*"}]}`
		rctx := context.WithValue(req.Context(), "user", user)
		rctx = auth.WithPolicySet(rctx, &auth.PolicySet{UserID: user.ID, TenantID: tenantID, Documents: []string{doc}, Actions: actions})
		req = mux.SetURLVars(req.WithContext(rctx), map[string]string{"bucket": bucketName, "object": key})
		rr := httptest.NewRecorder()
		server.handleUploadObject(rr, req)
		return rr.Code
	}
	retention := map[string]string{
		"x-amz-object-lock-mode":              "GOVERNANCE",
		"x-amz-object-lock-retain-until-date": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}
	hold := map[string]string{"x-amz-object-lock-legal-hold": "ON"}
	assert.Equal(t, http.StatusForbidden, upload("r1", retention, "s3:PutObject"))
	assert.Equal(t, http.StatusOK, upload("r2", retention, "s3:PutObject", "s3:PutObjectRetention"))
	assert.Equal(t, http.StatusForbidden, upload("h1", hold, "s3:PutObject"))
	assert.Equal(t, http.StatusOK, upload("h2", hold, "s3:PutObject", "s3:PutObjectLegalHold"))
	assert.Equal(t, http.StatusOK, upload("plain", nil, "s3:PutObject"), "a tenant without a storage limit can upload")
}
