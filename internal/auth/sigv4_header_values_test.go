package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// A header sent more than once is signed with its values joined by commas,
// and a run of spaces inside a value counts as one: the canonical form of
// SigV4, signed here by the AWS SDK's signer.
func TestSigV4SignsEveryValueOfAHeader(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)
	manager := managerInterface.(*authManager)
	ctx := context.Background()

	user := &User{ID: "multi-value-user", Username: "multi-value-user", Status: UserStatusActive, Roles: []string{"user"}}
	if err := manager.store.CreateUser(user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	key, err := manager.GenerateAccessKey(ctx, user.ID)
	if err != nil {
		t.Fatalf("GenerateAccessKey failed: %v", err)
	}

	req, err := http.NewRequest("PUT", "http://s3.example.com/bucket/object.gz", nil)
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.Header.Add("Content-Encoding", "gzip")
	req.Header.Add("Content-Encoding", "aws-chunked")
	req.Header.Set("X-Amz-Meta-Note", "two  spaces")
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	creds := aws.Credentials{AccessKeyID: key.AccessKeyID, SecretAccessKey: key.SecretAccessKey}
	if err := v4.NewSigner().SignHTTP(ctx, creds, req, "UNSIGNED-PAYLOAD", "s3", "us-east-1", time.Now().UTC()); err != nil {
		t.Fatalf("SignHTTP failed: %v", err)
	}

	if _, err := manager.ValidateS3SignatureV4(ctx, req); err != nil {
		t.Fatalf("ValidateS3SignatureV4 = %v, want the request taken", err)
	}
}
