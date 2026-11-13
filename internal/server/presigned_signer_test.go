package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/presigned"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A presigned URL gives its signer's access and no more, however its query is
// written: a user with no permission on a bucket reads nothing of it.
func TestAPresignedURLGivesOnlyItsSignersAccess(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("private")})
	require.NoError(t, err)
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("private"), Key: aws.String("secret.txt"), Body: strings.NewReader("classified-content")})
	require.NoError(t, err)

	outsider := &auth.User{ID: "outsider", Username: "outsider", Status: auth.UserStatusActive,
		Roles: []string{auth.RoleUser}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	require.NoError(t, s.authManager.CreateUser(ctx, outsider))
	key, err := s.authManager.GenerateAccessKey(ctx, outsider.ID)
	require.NoError(t, err)

	signed, err := presigned.GeneratePresignedURL(presigned.PresignedURLParams{
		Endpoint: aws.ToString(c.Options().BaseEndpoint), Bucket: "private", Key: "secret.txt",
		AccessKeyID: key.AccessKeyID, SecretAccessKey: key.SecretAccessKey, Method: http.MethodGet, ExpiresIn: 300, Region: "us-east-1",
	})
	require.NoError(t, err)
	path, query, found := strings.Cut(signed, "?")
	require.True(t, found)

	urls := map[string]string{
		"as signed": signed,
		// The router reads the first X-Amz-Algorithm, splitting on ";" too; the
		// handlers read the query without the pair that holds a ";".
		"an algorithm the router reads differently": path + "?X-Amz-Algorithm=x%0A;y&" + query,
		"a second algorithm":                        path + "?X-Amz-Algorithm=%0A&" + query,
	}
	for name, url := range urls {
		resp, err := http.Get(url)
		require.NoError(t, err, name)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s: %s", name, body)
		assert.NotContains(t, string(body), "classified-content", name)
	}
}

// A public WRITE grant lets anyone delete an object, which in a versioned
// bucket adds a delete marker; it does not let them remove a version.
func TestAPublicWriteGrantDoesNotRemoveAVersion(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	bucket := aws.String("open")
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: bucket})
	require.NoError(t, err)
	_, err = c.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: bucket,
		VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}})
	require.NoError(t, err)
	_, _ = c.DeletePublicAccessBlock(ctx, &s3.DeletePublicAccessBlockInput{Bucket: bucket})
	_, err = c.PutBucketAcl(ctx, &s3.PutBucketAclInput{Bucket: bucket, ACL: types.BucketCannedACLPublicReadWrite})
	require.NoError(t, err)
	put, err := c.PutObject(ctx, &s3.PutObjectInput{Bucket: bucket, Key: aws.String("kept.txt"), Body: strings.NewReader("kept")})
	require.NoError(t, err)
	version := aws.ToString(put.VersionId)
	require.NotEmpty(t, version)

	anonymousDelete := func(query string) int {
		req, err := http.NewRequest(http.MethodDelete, aws.ToString(c.Options().BaseEndpoint)+"/open/kept.txt"+query, nil)
		require.NoError(t, err)
		req.Header.Set("x-amz-content-sha256", "UNSIGNED-PAYLOAD")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		return resp.StatusCode
	}

	assert.Equal(t, http.StatusForbidden, anonymousDelete("?versionId="+version), "the version is not anyone's to remove")
	assert.Equal(t, http.StatusNoContent, anonymousDelete(""), "the object is deleted, with a delete marker")
	_, err = c.HeadObject(ctx, &s3.HeadObjectInput{Bucket: bucket, Key: aws.String("kept.txt"), VersionId: put.VersionId})
	assert.NoError(t, err, "the version is kept")
}
