package s3compat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGovernanceBypass_IsDecidedByPolicy pins what now decides: the permission,
func TestGovernanceBypass_IsDecidedByPolicy(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "worm-bucket", env.userID))

	stranger := &auth.User{ID: "stranger", TenantID: "another-tenant", Roles: []string{"user"}}

	err := env.handler.validateBypassGovernance(ctx, stranger, true, env.tenantID, "worm-bucket", "protected.txt")
	assert.Error(t, err,
		"a caller whose policies do not reach this object cannot override its retention")
}

// TestGovernanceBypass_AnonymousIsRefused: no identity, no override.
func TestGovernanceBypass_AnonymousIsRefused(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()

	err := env.handler.validateBypassGovernance(context.Background(), nil, false, "", "worm-bucket", "protected.txt")
	assert.Error(t, err, "an unauthenticated caller cannot override retention")
}

// TestGovernanceBypass_IsRefusedOnTheDeletePath checks the whole request rather
// than the helper, so the header alone cannot carry the privilege.
func TestGovernanceBypass_IsRefusedOnTheDeletePath(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "worm-delete", env.userID))

	outsider := &auth.User{ID: "outsider", TenantID: "another-tenant", Roles: []string{"user"}}

	req := httptest.NewRequest(http.MethodDelete, "/worm-delete/protected.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "worm-delete", "object": "protected.txt"})
	req.Header.Set("x-amz-bypass-governance-retention", "true")
	req = req.WithContext(setUserInContext(req.Context(), outsider))
	req.Header.Set("X-Tenant-ID", env.tenantID)

	w := httptest.NewRecorder()
	env.handler.DeleteObject(w, req)

	assert.NotEqual(t, http.StatusNoContent, w.Code,
		"sending the bypass header does not grant the privilege it asks for")
}

// A retention in force changes through the S3 API as S3 lets it: COMPLIANCE
// is never made GOVERNANCE or shortened, bypassing GOVERNANCE or not; a
// GOVERNANCE one is shortened only bypassing it.
func TestS3ARetentionInForceOnlyGrowsStricter(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := t.Context()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "locked", env.userID))
	require.NoError(t, env.bucketManager.SetVersioning(ctx, env.tenantID, "locked", &bucket.VersioningConfig{Status: "Enabled"}))
	require.NoError(t, env.bucketManager.SetObjectLockConfig(ctx, env.tenantID, "locked", &bucket.ObjectLockConfig{ObjectLockEnabled: true}))
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	retention := func(mode string, at time.Time) []byte {
		return []byte(fmt.Sprintf("<Retention><Mode>%s</Mode><RetainUntilDate>%s</RetainUntilDate></Retention>", mode, at.Format(time.RFC3339)))
	}
	set := func(key string, body []byte, bypass bool) int {
		t.Helper()
		req, w := env.makeS3Request("PUT", "/locked/"+key+"?retention", body)
		if bypass {
			req.Header.Set("x-amz-bypass-governance-retention", "true")
		}
		env.router.ServeHTTP(w, req)
		return w.Code
	}
	for _, key := range []string{"compliance", "governance"} {
		req, w := env.makeS3Request("PUT", "/locked/"+key, []byte("data"))
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	require.Equal(t, http.StatusOK, set("compliance", retention("COMPLIANCE", until), false))
	assert.Equal(t, http.StatusForbidden, set("compliance", retention("GOVERNANCE", until), true), "made GOVERNANCE bypassing it")
	assert.Equal(t, http.StatusForbidden, set("compliance", retention("COMPLIANCE", until.Add(-time.Minute)), true), "shortened")
	o, err := env.objectManager.GetObjectMetadata(ctx, env.tenantID+"/locked", "compliance")
	require.NoError(t, err)
	assert.Equal(t, "COMPLIANCE", o.Retention.Mode)
	assert.True(t, o.Retention.RetainUntilDate.Equal(until))

	require.Equal(t, http.StatusOK, set("governance", retention("GOVERNANCE", until), false))
	assert.Equal(t, http.StatusForbidden, set("governance", retention("GOVERNANCE", until.Add(-time.Minute)), false), "shortened")
	assert.Equal(t, http.StatusOK, set("governance", retention("GOVERNANCE", until.Add(-time.Minute)), true), "shortened bypassing it")
}
