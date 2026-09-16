package cluster

import (
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/require"
)

func TestClusterProxyPreservesSTSSession(t *testing.T) {
	am := auth.NewManager(config.AuthConfig{EnableAuth: true, JWTSecret: "test-secret-with-more-than-thirty-two-characters"}, t.TempDir())
	t.Cleanup(func() { require.NoError(t, am.(interface{ Close() error }).Close()) })
	user := &auth.User{ID: "proxy-sts-user", Username: "proxy-sts-user", Roles: []string{"admin"}, Status: auth.UserStatusActive}
	require.NoError(t, am.CreateUser(t.Context(), user))
	session, err := am.IssueSTSSession(t.Context(), user.ID, 3600, "{\"Statement\":[{\"Effect\":\"Allow\",\"Action\":\"s3:*\",\"Resource\":\"arn:aws:s3:::allowed/*\"}]}")
	require.NoError(t, err)
	req := httptest.NewRequest("PUT", "/allowed/key", nil)
	req.Header.Set("X-Amz-Security-Token", session.SessionToken)
	_, err = am.AuthorizeSTSRequest(req.Context(), session.TempAccessKeyID, session.SessionToken, req)
	require.NoError(t, err)
	req.Header.Set("X-MaxIOFS-Proxied", "true")
	AddClusterProxyHeaders(req, "node", "cluster-secret", user.ID, "", "admin")
	require.Equal(t, session.TempAccessKeyID, req.Header.Get("X-MaxIOFS-STS-Access-Key"))
	for _, header := range []string{"X-MaxIOFS-STS-Access-Key", "X-Amz-Security-Token"} {
		tampered := req.Clone(req.Context())
		tampered.Header.Del(header)
		_, _, _, ok := ValidateClusterProxyAuth(tampered, "cluster-secret")
		require.False(t, ok, "removing %s must invalidate the signature", header)
	}
	_, _, _, ok := ValidateClusterProxyAuth(req, "cluster-secret")
	require.True(t, ok)
	forwarded := httptest.NewRequest("PUT", "/allowed/key", nil)
	_, err = am.AuthorizeSTSRequest(forwarded.Context(), req.Header.Get("X-MaxIOFS-STS-Access-Key"), req.Header.Get("X-Amz-Security-Token"), forwarded)
	require.NoError(t, err)
	set, ok := auth.PolicySetFromContext(forwarded.Context())
	require.True(t, ok)
	require.True(t, set.AllowsOwnAccount(auth.ActionPutObject, "arn:aws:s3:::allowed/key"))
	require.False(t, set.AllowsOwnAccount(auth.ActionGetObject, "arn:aws:s3:::secret/key"))
}
