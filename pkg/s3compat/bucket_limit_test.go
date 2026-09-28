package s3compat

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

// A tenant's bucket limit counts every bucket the tenant has, including those
// created through the S3 API by its users.
func TestCreateBucketCountsTheTenantsBuckets(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()
	tenant, err := env.authManager.GetTenant(ctx, env.tenantID)
	require.NoError(t, err)
	tenant.MaxBuckets = 2
	require.NoError(t, env.authManager.UpdateTenant(ctx, tenant))

	create := func(name string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/"+name, nil)
		req = req.WithContext(context.WithValue(req.Context(), "user", &auth.User{ID: env.userID, TenantID: env.tenantID, Roles: []string{"admin"}}))
		req = mux.SetURLVars(req, map[string]string{"bucket": name})
		w := httptest.NewRecorder()
		env.handler.CreateBucket(w, req)
		return w
	}
	require.Equal(t, http.StatusOK, create("limit-one").Code)
	require.Equal(t, http.StatusOK, create("limit-two").Code)
	w := create("limit-three")
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "QuotaExceeded")
}
