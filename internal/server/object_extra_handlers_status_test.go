package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
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
