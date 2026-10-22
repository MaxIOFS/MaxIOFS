package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An object gives back on every node the headers it was written with; one
// written in parts, the tags it was started with and each of its parts. Kept
// in whole copies or cut in shards.
func TestAnObjectKeepsItsHeadersAndPartsOnEveryNode(t *testing.T) {
	for _, setup := range []struct {
		name  string
		build func(t *testing.T) *haCluster
	}{
		{"copies", func(t *testing.T) *haCluster {
			c := newHACluster(t, 2, 2)
			require.NoError(t, c.nodes[0].bucketManager.CreateBucket(context.Background(), "", "kept", routingAdminID))
			return c
		}},
		{"shards", func(t *testing.T) *haCluster { return newParityCluster(t, 3, "kept") }},
	} {
		t.Run(setup.name, func(t *testing.T) {
			c := setup.build(t)
			ctx := context.Background()
			expires := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("kept"), Key: aws.String("whole"),
				Body: bytes.NewReader(payload(300 << 10)), ContentType: aws.String("text/x-kept"), CacheControl: aws.String("max-age=60"),
				ContentDisposition: aws.String("inline"), ContentEncoding: aws.String("identity"), ContentLanguage: aws.String("es"), Expires: &expires})
			require.NoError(t, err)

			first, last := payload(5<<20), payload(100<<10)
			last1 := c.nodes[len(c.nodes)-1].s3
			started, err := last1.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String("kept"), Key: aws.String("parts"),
				ContentType: aws.String("text/x-kept"), CacheControl: aws.String("max-age=60"), ContentDisposition: aws.String("inline"),
				ContentEncoding: aws.String("identity"), ContentLanguage: aws.String("es"), Expires: &expires, Tagging: aws.String("a=1&b=dos")})
			require.NoError(t, err)
			var completed []types.CompletedPart
			for i, data := range [][]byte{first, last} {
				part, err := last1.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String("kept"), Key: aws.String("parts"),
					UploadId: started.UploadId, PartNumber: aws.Int32(int32(i + 1)), Body: bytes.NewReader(data)})
				require.NoError(t, err)
				completed = append(completed, types.CompletedPart{PartNumber: aws.Int32(int32(i + 1)), ETag: part.ETag})
			}
			_, err = last1.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String("kept"), Key: aws.String("parts"),
				UploadId: started.UploadId, MultipartUpload: &types.CompletedMultipartUpload{Parts: completed}})
			require.NoError(t, err)

			for _, n := range c.nodes {
				for _, key := range []string{"whole", "parts"} {
					head, err := n.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("kept"), Key: aws.String(key)})
					require.NoError(t, err, "%s on %s", key, n.id)
					assert.Equal(t, "text/x-kept", aws.ToString(head.ContentType), "%s on %s", key, n.id)
					assert.Equal(t, "max-age=60", aws.ToString(head.CacheControl), "%s on %s", key, n.id)
					assert.Equal(t, "inline", aws.ToString(head.ContentDisposition), "%s on %s", key, n.id)
					assert.Equal(t, "identity", aws.ToString(head.ContentEncoding), "%s on %s", key, n.id)
					assert.Equal(t, "es", aws.ToString(head.ContentLanguage), "%s on %s", key, n.id)
					assert.Equal(t, expires.Format(http.TimeFormat), aws.ToString(head.ExpiresString), "%s on %s", key, n.id)
				}
				tags, err := n.s3.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{Bucket: aws.String("kept"), Key: aws.String("parts")})
				require.NoError(t, err, n.id)
				assert.Len(t, tags.TagSet, 2, n.id)
				for i, want := range [][]byte{first, last} {
					got, err := n.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("kept"), Key: aws.String("parts"), PartNumber: aws.Int32(int32(i + 1))})
					require.NoError(t, err, "part %d on %s", i+1, n.id)
					var data bytes.Buffer
					_, err = data.ReadFrom(got.Body)
					got.Body.Close()
					require.NoError(t, err)
					assert.True(t, bytes.Equal(want, data.Bytes()), "part %d on %s", i+1, n.id)
					assert.Equal(t, int32(2), aws.ToInt32(got.PartsCount), "part %d on %s", i+1, n.id)
				}
			}
		})
	}
}

// A copy served to a peer that catches up carries the headers the object was
// written with.
func TestHAGetObjectSendsTheObjectsHeaders(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	bucketName := "ha-get-headers"
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucketName, OwnerID: "admin"}))
	written := http.Header{}
	for name, value := range map[string]string{"Content-Type": "text/x-kept", "Cache-Control": "max-age=60",
		"Content-Disposition": "inline", "Content-Encoding": "identity", "Content-Language": "es",
		"Expires": "Tue, 01 Jan 2030 00:00:00 GMT"} {
		written.Set(name, value)
	}
	_, err := server.objectManager.PutObject(ctx, bucketName, "k", strings.NewReader("data"), written)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/internal/ha/objects/k?bucket="+bucketName, nil)
	req = mux.SetURLVars(req, map[string]string{"key": "k"})
	w := httptest.NewRecorder()
	server.handleHAGetObject(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for name := range written {
		assert.Equal(t, written.Get(name), w.Header().Get(name), name)
	}
}
