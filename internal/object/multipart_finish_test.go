package object

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestACompletionIsLoggedWithItsObject(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "logged", OwnerID: "u",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"}}))
	upload, err := om.CreateMultipartUpload(ctx, "logged", "big.zip", http.Header{})
	require.NoError(t, err)
	part, err := om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader([]byte("data")))
	require.NoError(t, err)

	logger := logrus.StandardLogger()
	hook := new(logtest.Hook)
	formerHooks, formerLevel := logger.ReplaceHooks(logrus.LevelHooks{}), logger.GetLevel()
	logger.AddHook(hook)
	logger.SetLevel(logrus.InfoLevel)
	defer func() {
		logger.ReplaceHooks(formerHooks)
		logger.SetLevel(formerLevel)
	}()
	obj, err := om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{{PartNumber: 1, ETag: part.ETag}})
	require.NoError(t, err)

	var logged *logrus.Entry
	for _, e := range hook.AllEntries() {
		if e.Message == "Multipart upload completed successfully" {
			logged = e
		}
	}
	require.NotNil(t, logged)
	assert.Equal(t, logrus.InfoLevel, logged.Level)
	assert.Equal(t, "logged", logged.Data["bucket"])
	assert.Equal(t, "big.zip", logged.Data["key"])
	assert.Equal(t, 1, logged.Data["parts"])
	assert.Equal(t, obj.VersionID, logged.Data["versionID"])
}

// A completion's If-Match and If-None-Match are decided under the key's lock,
// whatever was checked before: a refused completion writes nothing and leaves
// the upload to be completed.
func TestCompletionConditionsAreDecidedUnderTheLock(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	const bucket = "conditions"
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucket, OwnerID: "u"}))
	first, err := om.PutObject(ctx, bucket, "k", bytes.NewReader([]byte("first")), http.Header{})
	require.NoError(t, err)

	upload, err := om.CreateMultipartUpload(ctx, bucket, "k", http.Header{})
	require.NoError(t, err)
	part, err := om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader([]byte("second")))
	require.NoError(t, err)
	parts := []Part{{PartNumber: 1, ETag: part.ETag}}

	ifNoneMatch := WithWriteConditions(ctx, http.Header{"If-None-Match": []string{"*"}})
	_, err = om.CompleteMultipartUpload(ifNoneMatch, upload.UploadID, parts)
	assert.ErrorIs(t, err, ErrPreconditionFailed)
	current, err := om.GetObjectMetadata(ctx, bucket, "k")
	require.NoError(t, err)
	assert.Equal(t, first.ETag, current.ETag, "nothing is written")

	ifMatch := WithWriteConditions(ctx, http.Header{"If-Match": []string{`"` + first.ETag + `"`}})
	completed, err := om.CompleteMultipartUpload(ifMatch, upload.UploadID, parts)
	require.NoError(t, err, "the refused upload is still there")
	assert.NotEqual(t, first.ETag, completed.ETag)
}

// A finished upload is kept for the same completion or abort sent again: a
// completed one with its result and its part list, an aborted one as aborted.
// Aborting either again is done with nothing to do.
func TestAFinishedUploadIsKept(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	const bucket = "finished"
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucket, OwnerID: "u"}))

	upload, err := om.CreateMultipartUpload(ctx, bucket, "done", http.Header{})
	require.NoError(t, err)
	part, err := om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader([]byte("bytes")))
	require.NoError(t, err)
	parts := []Part{{PartNumber: 1, ETag: part.ETag}}
	completed, err := om.CompleteMultipartUpload(ctx, upload.UploadID, parts)
	require.NoError(t, err)

	finished, err := store.GetFinishedUpload(ctx, upload.UploadID)
	require.NoError(t, err)
	assert.Equal(t, bucket, finished.Bucket)
	assert.Equal(t, "done", finished.Key)
	assert.Equal(t, completed.ETag, finished.ETag)
	assert.Equal(t, PartsDigest(parts), finished.Parts)
	assert.False(t, finished.Aborted)
	require.NoError(t, om.AbortMultipartUpload(ctx, upload.UploadID))
	_, err = om.GetObjectMetadata(ctx, bucket, "done")
	assert.NoError(t, err, "an abort after the completion leaves the object")

	aborted, err := om.CreateMultipartUpload(ctx, bucket, "aborted", http.Header{})
	require.NoError(t, err)
	require.NoError(t, om.AbortMultipartUpload(ctx, aborted.UploadID))
	require.NoError(t, om.AbortMultipartUpload(ctx, aborted.UploadID))
	finished, err = store.GetFinishedUpload(ctx, aborted.UploadID)
	require.NoError(t, err)
	assert.True(t, finished.Aborted)

	assert.ErrorIs(t, om.AbortMultipartUpload(ctx, "no-such-upload"), ErrUploadNotFound)
}
