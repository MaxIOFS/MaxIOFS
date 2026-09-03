package s3compat

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Paging ListObjectVersions has to return every version and every folder
// exactly once, and the marker it hands back has to survive the round trip
// unchanged — encoding it twice sends the client somewhere else.
func TestListObjectVersions_PagingCoversEverythingExactlyOnce(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()

	const bucketName = "versions-paging"
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))
	require.NoError(t, env.bucketManager.SetVersioning(ctx, env.tenantID, bucketName, &bucket.VersioningConfig{Status: "Enabled"}))

	bucketPath := env.tenantID + "/" + bucketName
	var wantKeys []string
	var wantPrefixes []string
	for i := 0; i < 4; i++ {
		flat := fmt.Sprintf("a%02d.txt", i)
		for v := 0; v < 2; v++ {
			_, err := env.objectManager.PutObject(ctx, bucketPath, flat,
				bytes.NewReader([]byte(fmt.Sprintf("v%d", v))), http.Header{})
			require.NoError(t, err)
			wantKeys = append(wantKeys, flat)
		}
		folder := fmt.Sprintf("f%02d/", i)
		_, err := env.objectManager.PutObject(ctx, bucketPath, folder+"inner.txt",
			bytes.NewReader([]byte("x")), http.Header{})
		require.NoError(t, err)
		wantPrefixes = append(wantPrefixes, folder)
	}

	for _, pageSize := range []int{1, 2, 3, 5, 100} {
		t.Run(fmt.Sprintf("pageSize=%d", pageSize), func(t *testing.T) {
			var gotKeys, gotPrefixes []string
			keyMarker, versionMarker := "", ""

			for pages := 0; ; pages++ {
				require.Less(t, pages, 40, "paging did not terminate")

				url := fmt.Sprintf("/%s?versions&delimiter=/&max-keys=%d", bucketName, pageSize)
				if keyMarker != "" {
					url += "&key-marker=" + keyMarker + "&version-id-marker=" + versionMarker
				}
				req, w := env.makeS3Request("GET", url, nil)
				env.router.ServeHTTP(w, req)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())

				var res ListVersionsResult
				require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &res))

				for _, v := range res.Versions {
					gotKeys = append(gotKeys, v.Key)
				}
				for _, p := range res.CommonPrefixes {
					gotPrefixes = append(gotPrefixes, p.Prefix)
				}

				if !res.IsTruncated {
					break
				}
				require.NotEmpty(t, res.NextKeyMarker, "a truncated listing must say where to resume")
				require.NotEqual(t, keyMarker+"|"+versionMarker, res.NextKeyMarker+"|"+res.NextVersionIdMarker,
					"the marker did not advance, so paging would loop")
				keyMarker, versionMarker = res.NextKeyMarker, res.NextVersionIdMarker
			}

			assert.ElementsMatch(t, wantKeys, gotKeys, "every version must appear exactly once")
			assert.ElementsMatch(t, wantPrefixes, gotPrefixes, "every folder must appear exactly once")
		})
	}
}

// With encoding-type=url the marker must be encoded once. Encoding it twice
// turns a space into %2520, and the client resumes at a key that does not exist.
func TestListObjectVersions_NextKeyMarkerIsEncodedOnce(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()

	const bucketName = "versions-encoding"
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))
	require.NoError(t, env.bucketManager.SetVersioning(ctx, env.tenantID, bucketName, &bucket.VersioningConfig{Status: "Enabled"}))

	bucketPath := env.tenantID + "/" + bucketName
	for _, k := range []string{"my file a.txt", "my file b.txt"} {
		_, err := env.objectManager.PutObject(ctx, bucketPath, k,
			bytes.NewReader([]byte("1")), http.Header{})
		require.NoError(t, err)
	}

	req, w := env.makeS3Request("GET",
		fmt.Sprintf("/%s?versions&max-keys=1&encoding-type=url", bucketName), nil)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var res ListVersionsResult
	require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &res))
	require.True(t, res.IsTruncated)
	assert.NotContains(t, res.NextKeyMarker, "%25", "the marker was encoded twice")
	assert.Contains(t, res.NextKeyMarker, "%20", "the marker should be url-encoded once")
}

func TestListObjectVersions_WithoutDelimiterPagingCoversEverythingExactlyOnce(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()

	const bucketName = "versions-no-delimiter"
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))
	require.NoError(t, env.bucketManager.SetVersioning(ctx, env.tenantID, bucketName, &bucket.VersioningConfig{Status: "Enabled"}))

	bucketPath := env.tenantID + "/" + bucketName
	for _, key := range []string{"same-key.txt", "second-key.txt"} {
		for i := 0; i < 3; i++ {
			_, err := env.objectManager.PutObject(ctx, bucketPath, key,
				bytes.NewReader([]byte(fmt.Sprintf("%s-%d", key, i))), http.Header{})
			require.NoError(t, err)
		}
	}

	for _, pageSize := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("pageSize=%d", pageSize), func(t *testing.T) {
			seen := map[string]bool{}
			keyMarker, versionMarker := "", ""

			for pages := 0; ; pages++ {
				require.Less(t, pages, 20, "paging did not terminate")

				url := fmt.Sprintf("/%s?versions&max-keys=%d", bucketName, pageSize)
				if keyMarker != "" {
					url += "&key-marker=" + keyMarker + "&version-id-marker=" + versionMarker
				}
				req, w := env.makeS3Request("GET", url, nil)
				env.router.ServeHTTP(w, req)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())

				var res ListVersionsResult
				require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &res))
				for _, v := range res.Versions {
					id := v.Key + "|" + v.VersionId
					require.False(t, seen[id], "duplicate version %s", id)
					seen[id] = true
				}

				if !res.IsTruncated {
					break
				}
				require.NotEmpty(t, res.NextKeyMarker, "a truncated listing must say where to resume")
				require.NotEqual(t, keyMarker+"|"+versionMarker, res.NextKeyMarker+"|"+res.NextVersionIdMarker,
					"the marker did not advance, so paging would loop")
				keyMarker, versionMarker = res.NextKeyMarker, res.NextVersionIdMarker
			}

			require.Len(t, seen, 6)
		})
	}
}
