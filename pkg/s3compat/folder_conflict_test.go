package s3compat

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/require"
)

func TestPutFolderMarkerOverSameNamedObjectSucceeds(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "folder-coexist"
	bucketPath := env.tenantID + "/" + bucketName
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))

	const payload = "PAYLOAD-THAT-MATTERS"
	_, err := env.objectManager.PutObject(ctx, bucketPath, "report",
		bytes.NewReader([]byte(payload)), http.Header{"Content-Type": []string{"text/plain"}})
	require.NoError(t, err)

	user := &auth.User{
		ID:       env.userID,
		TenantID: env.tenantID,
		Roles:    []string{auth.RoleTenantAdmin},
	}
	req := httptest.NewRequest(http.MethodPut, "/"+bucketName+"/report/", strings.NewReader(""))
	req = mux.SetURLVars(req, map[string]string{"bucket": bucketName, "object": "report/"})
	req = req.WithContext(setUserInContext(req.Context(), user))
	w := httptest.NewRecorder()

	env.handler.PutObject(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	obj, err := env.objectManager.GetObjectMetadata(ctx, bucketPath, "report")
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), obj.Size)
}
