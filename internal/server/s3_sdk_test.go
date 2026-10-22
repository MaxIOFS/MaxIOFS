package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sdkClient is an S3 client of s with the SDK's defaults: it sends a
// checksum with each write and checks the one each read gives.
func sdkClient(t *testing.T, s *Server) *s3.Client {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Unix()
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "sdk-admin", Username: "sdk-admin",
		Status: auth.UserStatusActive, Roles: []string{auth.RoleAdmin}, CreatedAt: now, UpdatedAt: now}))
	key, err := s.authManager.GenerateAccessKey(ctx, "sdk-admin")
	require.NoError(t, err)
	api := httptest.NewServer(s.httpServer.Handler)
	t.Cleanup(api.Close)
	return s3.New(s3.Options{
		BaseEndpoint:     aws.String(api.URL),
		Region:           "us-east-1",
		UsePathStyle:     true,
		Credentials:      credentials.NewStaticCredentialsProvider(key.AccessKeyID, key.SecretAccessKey, ""),
		RetryMaxAttempts: 1,
	})
}

func readAll(t *testing.T, out *s3.GetObjectOutput, err error) string {
	t.Helper()
	require.NoError(t, err)
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	require.NoError(t, err)
	return string(data)
}

// A range of an object written with a checksum is sent without it: an SDK that
// checks the checksum it is sent against the bytes, as botocore does, would
// refuse them.
func TestAnSDKReadsARangeOfAnObjectWrittenWithAChecksum(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("ranges")})
	require.NoError(t, err)
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("ranges"), Key: aws.String("k"),
		Body: bytes.NewReader([]byte("0123456789abcdef")), ChecksumAlgorithm: types.ChecksumAlgorithmCrc32})
	require.NoError(t, err)

	got, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("ranges"), Key: aws.String("k"), Range: aws.String("bytes=2-5")})
	require.NoError(t, err)
	assert.Empty(t, aws.ToString(got.ChecksumCRC32))
	assert.Equal(t, "2345", readAll(t, got, nil))
	got, err = c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("ranges"), Key: aws.String("k")})
	assert.Equal(t, "0123456789abcdef", readAll(t, got, err))
	assert.NotEmpty(t, aws.ToString(got.ChecksumCRC32), "a whole read is sent the checksum and checks it")
}

// A key is the bytes it is: a key with an empty or a "." segment is not
// another key. A key S3 would take but MaxIOFS refuses, or one too long, is
// refused as the client's error.
func TestAKeyIsTheBytesItIs(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("keys")})
	require.NoError(t, err)
	keys := []string{"a//double", "a/double", "a/./dot", "a/dot", "trailing//"}
	for _, key := range keys {
		_, err := c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("keys"), Key: aws.String(key),
			Body: strings.NewReader("data of " + key)})
		require.NoError(t, err, key)
	}
	for _, key := range keys {
		got, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("keys"), Key: aws.String(key)})
		assert.Equal(t, "data of "+key, readAll(t, got, err), key)
	}
	listed, err := c.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String("keys")})
	require.NoError(t, err)
	var names []string
	for _, o := range listed.Contents {
		names = append(names, aws.ToString(o.Key))
	}
	assert.ElementsMatch(t, keys, names)

	for key, code := range map[string]string{
		"/leading":                "InvalidArgument",
		"a/../b":                  "InvalidArgument",
		strings.Repeat("k", 1025): "KeyTooLongError",
	} {
		_, err := c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("keys"), Key: aws.String(key), Body: strings.NewReader("x")})
		assert.Equal(t, http.StatusBadRequest, s3Status(err), "%.20s: %v", key, err)
		assert.ErrorContains(t, err, code, "%.20s", key)
	}
}
