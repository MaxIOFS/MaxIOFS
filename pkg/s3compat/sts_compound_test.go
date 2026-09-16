package s3compat

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/require"
)

func signedSessionRequest(env *s3TestEnv, sess *auth.STSSession, method, path, body, source string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "localhost"
	req.Header.Set("x-amz-security-token", sess.SessionToken)
	if source != "" {
		req.Header.Set("x-amz-copy-source", source)
	}
	signRequestV4(req, sess.TempAccessKeyID, sess.SecretAccessKey, "us-east-1", "s3")
	rr := httptest.NewRecorder()
	env.router.ServeHTTP(rr, req)
	return rr
}

func TestSTSSessionCompoundOperations(t *testing.T) {
	for _, role := range []bool{false, true} {
		t.Run(fmt.Sprintf("role=%v", role), func(t *testing.T) {
			env := setupCompleteS3Environment(t)
			defer env.cleanup()
			ctx := t.Context()
			for _, name := range []string{"allowed", "secret"} {
				require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, name, env.userID))
			}
			require.NoError(t, env.bucketManager.SetVersioning(ctx, env.tenantID, "secret", &bucket.VersioningConfig{Status: "Enabled"}))
			private, err := env.objectManager.PutObject(ctx, env.tenantID+"/secret", "private", strings.NewReader("private-content"), http.Header{})
			require.NoError(t, err)
			_, err = env.objectManager.PutObject(ctx, env.tenantID+"/allowed", "source", strings.NewReader("allowed-content"), http.Header{})
			require.NoError(t, err)
			doc := "{\"Statement\":[{\"Effect\":\"Allow\",\"Action\":\"s3:*\",\"Resource\":\"arn:aws:s3:::allowed/*\"},{\"Effect\":\"Deny\",\"Action\":\"s3:*\",\"Resource\":\"arn:aws:s3:::secret/*\"},{\"Effect\":\"Deny\",\"Action\":\"s3:DeleteObject\",\"Resource\":\"arn:aws:s3:::allowed/keep\"}]}"
			var sess *auth.STSSession
			if role {
				iam := env.authManager.(auth.IAMManager)
				_, err = iam.CreateIAMRole(ctx, "copy-role", "/", "", "{\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"AWS\":\"*\"},\"Action\":\"sts:AssumeRole\"}]}", 3600, env.tenantID)
				require.NoError(t, err)
				require.NoError(t, iam.PutIAMInlinePolicy(ctx, auth.IAMTargetRole, "copy-role", "scope", "{\"Statement\":[{\"Effect\":\"Allow\",\"Action\":\"s3:*\",\"Resource\":\"*\"}]}"))
				users, err := env.authManager.ListUsers(ctx)
				require.NoError(t, err)
				for _, u := range users {
					if u.ID == env.userID {
						sess, err = iam.AssumeIAMRole(ctx, &u, auth.IAMRoleARN("copy-role"), "test", 3600, doc)
						require.NoError(t, err)
					}
				}
				require.NotNil(t, sess)
			} else {
				sess, err = env.authManager.IssueSTSSession(ctx, env.userID, 3600, doc)
				require.NoError(t, err)
			}
			direct := signedSessionRequest(env, sess, "GET", "/secret/private", "", "")
			require.Equal(t, http.StatusForbidden, direct.Code)
			for _, source := range []string{"/secret/private", "/secret/private?versionId=" + private.VersionID} {
				copied := signedSessionRequest(env, sess, "PUT", "/allowed/copied", "", source)
				require.Equal(t, http.StatusForbidden, copied.Code, copied.Body.String())
			}
			copied := signedSessionRequest(env, sess, "PUT", "/allowed/copied", "", "/allowed/source")
			require.Equal(t, http.StatusOK, copied.Code, copied.Body.String())
			read := signedSessionRequest(env, sess, "GET", "/allowed/copied", "", "")
			require.Equal(t, http.StatusOK, read.Code, read.Body.String())
			require.Equal(t, "allowed-content", read.Body.String())

			upload, err := env.objectManager.CreateMultipartUpload(ctx, env.tenantID+"/allowed", "multipart", http.Header{})
			require.NoError(t, err)
			partPath := "/allowed/multipart?partNumber=1&uploadId=" + upload.UploadID
			part := signedSessionRequest(env, sess, "PUT", partPath, "", "/secret/private")
			require.Equal(t, http.StatusForbidden, part.Code, part.Body.String())
			part = signedSessionRequest(env, sess, "PUT", partPath, "", "/allowed/source")
			require.Equal(t, http.StatusOK, part.Code, part.Body.String())

			for _, key := range []string{"keep", "remove"} {
				_, err = env.objectManager.PutObject(ctx, env.tenantID+"/allowed", key, strings.NewReader(key), http.Header{})
				require.NoError(t, err)
			}
			batch := signedSessionRequest(env, sess, "POST", "/allowed?delete", "<Delete><Object><Key>keep</Key></Object><Object><Key>remove</Key></Object></Delete>", "")
			require.Equal(t, http.StatusOK, batch.Code, batch.Body.String())
			require.Contains(t, batch.Body.String(), "<Code>AccessDenied</Code>")
			require.Contains(t, batch.Body.String(), "<Deleted>")
			keep := signedSessionRequest(env, sess, "GET", "/allowed/keep", "", "")
			require.Equal(t, http.StatusOK, keep.Code, keep.Body.String())
			removed := signedSessionRequest(env, sess, "GET", "/allowed/remove", "", "")
			require.Equal(t, http.StatusNotFound, removed.Code, removed.Body.String())
		})
	}
}
