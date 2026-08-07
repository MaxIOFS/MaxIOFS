package s3compat

// Overriding governance-mode retention.
//
// This is the strongest thing a caller can ask for on a WORM bucket: it deletes
// an object that is still under protection. It used to be granted by holding
// the admin role, which meant the catalogue permission for it granted nothing,
// and any administrator could override retention on a compliance target whether
// or not their policies said so.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGovernanceBypass_IsDecidedByPolicy pins what now decides: the permission,
// asked of the caller's policies against the object.
//
// A caller with no policy reaching this bucket is refused, and broad policies
// are still bounded by the bucket tenant.
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
