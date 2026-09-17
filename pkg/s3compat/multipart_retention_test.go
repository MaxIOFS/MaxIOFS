package s3compat

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/require"
)

func TestS3MultipartDefaultRetention(t *testing.T) {
	for _, mode := range []string{"COMPLIANCE", "GOVERNANCE"} {
		t.Run(mode, func(t *testing.T) {
			env := setupCompleteS3Environment(t)
			defer env.cleanup()
			ctx := t.Context()
			require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "locked", env.userID))
			require.NoError(t, env.bucketManager.SetVersioning(ctx, env.tenantID, "locked", &bucket.VersioningConfig{Status: "Enabled"}))
			days := 30
			require.NoError(t, env.bucketManager.SetObjectLockConfig(ctx, env.tenantID, "locked", &bucket.ObjectLockConfig{
				ObjectLockEnabled: true, Rule: &bucket.ObjectLockRule{DefaultRetention: &bucket.DefaultRetention{Mode: mode, Days: &days}},
			}))
			sess, err := env.authManager.IssueSTSSession(ctx, env.userID, 3600, "")
			require.NoError(t, err)
			created := signedSessionRequest(env, sess, "POST", "/locked/key?uploads", "", "")
			require.Equal(t, http.StatusOK, created.Code, created.Body.String())
			var upload InitiateMultipartUploadResult
			require.NoError(t, xml.Unmarshal(created.Body.Bytes(), &upload))
			part := signedSessionRequest(env, sess, "PUT", "/locked/key?uploadId="+upload.UploadId+"&partNumber=1", "protected", "")
			require.Equal(t, http.StatusOK, part.Code, part.Body.String())
			body := fmt.Sprintf("<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>", part.Header().Get("ETag"))
			before := time.Now()
			completed := signedSessionRequest(env, sess, "POST", "/locked/key?uploadId="+upload.UploadId, body, "")
			require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
			require.Contains(t, completed.Body.String(), "CompleteMultipartUploadResult")
			o, err := env.objectManager.GetObjectMetadata(ctx, env.tenantID+"/locked", "key")
			require.NoError(t, err)
			require.NotNil(t, o.Retention)
			require.Equal(t, mode, o.Retention.Mode)
			require.False(t, o.Retention.RetainUntilDate.Before(before.AddDate(0, 0, days)))
			deleted := signedSessionRequest(env, sess, "DELETE", "/locked/key?versionId="+o.VersionID, "", "")
			require.Equal(t, http.StatusForbidden, deleted.Code, deleted.Body.String())
			read := signedSessionRequest(env, sess, "GET", "/locked/key?versionId="+o.VersionID, "", "")
			require.Equal(t, http.StatusOK, read.Code, read.Body.String())
			require.Equal(t, "protected", read.Body.String())
		})
	}
}
