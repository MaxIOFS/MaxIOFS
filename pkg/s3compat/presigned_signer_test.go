package s3compat

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// A presigned URL only proves who signed it. If that signer cannot be turned
// into a user, the request must be denied: letting it through would drop it on
// the unauthenticated path, where the signature alone stands in for IAM.
func TestPresignedSigner_UnresolvableCredentialIsDenied(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()

	req := httptest.NewRequest("GET", "/test-bucket/test.txt", nil)

	if _, err := env.handler.presignedSigner(req, "", ""); err == nil {
		t.Fatal("a presigned URL with no credential resolved to a signer")
	}

	if _, err := env.handler.presignedSigner(req, "AKIADOESNOTEXIST0000", ""); err == nil {
		t.Fatal("an unknown access key resolved to a signer")
	}
}

func TestPresignedSigner_ResolvesTheOwnerOfAPermanentKey(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()

	key, err := env.authManager.GenerateAccessKey(context.Background(), env.userID)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/test-bucket/test.txt", nil)
	signer, err := env.handler.presignedSigner(req, key.AccessKeyID, "")
	require.NoError(t, err)
	require.NotNil(t, signer)
	require.Equal(t, env.userID, signer.ID)
}

func TestPresignedSigner_ResolvesSTSPrincipalAndRequiresToken(t *testing.T) {
	handler := &Handler{authManager: &mockAuthManager{}}
	req := httptest.NewRequest("GET", "/test-bucket/test.txt", nil)

	signer, err := handler.presignedSigner(req, "ASIAIOSFODNN7EXAMPLE", "sts-token")
	require.NoError(t, err)
	require.NotNil(t, signer)
	require.Equal(t, "sts-user", signer.ID)

	_, err = handler.presignedSigner(req, "ASIAIOSFODNN7EXAMPLE", "wrong-token")
	require.Error(t, err)
}
