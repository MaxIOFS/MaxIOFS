package s3compat

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/require"
)

func TestDeleteBucketAllowsOnlyImplicitFolderMarkers(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "delete-implicit-folders"
	bucketPath := env.tenantID + "/" + bucketName
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))

	_, err := env.objectManager.PutObject(ctx, bucketPath, "nested/deep/file.txt",
		bytes.NewReader([]byte("data")), http.Header{"Content-Type": []string{"text/plain"}})
	require.NoError(t, err)

	_, err = env.objectManager.DeleteObject(ctx, bucketPath, "nested/deep/file.txt", false)
	require.NoError(t, err)

	user := &auth.User{
		ID:       env.userID,
		TenantID: env.tenantID,
		Roles:    []string{auth.RoleTenantAdmin},
	}
	req := httptest.NewRequest(http.MethodDelete, "/"+bucketName, nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})
	req = req.WithContext(setUserInContext(req.Context(), user))
	w := httptest.NewRecorder()

	env.handler.DeleteBucket(w, req)

	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
}
