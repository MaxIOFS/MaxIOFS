package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestARefusedRequestIsLoggedWithWhy(t *testing.T) {
	logger := logrus.StandardLogger()
	hook := new(logtest.Hook)
	formerHooks := logger.ReplaceHooks(logrus.LevelHooks{})
	formerLevel := logger.GetLevel()
	logger.AddHook(hook)
	logger.SetLevel(logrus.InfoLevel)
	defer func() {
		logger.ReplaceHooks(formerHooks)
		logger.SetLevel(formerLevel)
	}()
	logged := func(path string) *logrus.Entry {
		t.Helper()
		for _, e := range hook.AllEntries() {
			if e.Message == "S3 request" && e.Data["path"] == path {
				return e
			}
		}
		require.Fail(t, "no S3 request line for "+path)
		return nil
	}

	refused := logS3APIRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.NoteRefusal(r.Context(), "admin", auth.ActionPutObject, "arn:aws:s3:::b/k", "the bucket belongs to tenant t1: a global administrator only reads it")
		auth.NoteError(r.Context(), "AccessDenied", "Access Denied")
		w.WriteHeader(http.StatusForbidden)
	}))
	refused.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/b/k?uploads=", nil))
	e := logged("/b/k")
	assert.Equal(t, logrus.WarnLevel, e.Level)
	assert.Equal(t, "AccessDenied", e.Data["error"])
	assert.Equal(t, "admin", e.Data["user"])
	assert.Equal(t, auth.ActionPutObject, e.Data["action"])
	assert.Equal(t, "arn:aws:s3:::b/k", e.Data["resource"])
	assert.Equal(t, "the bucket belongs to tenant t1: a global administrator only reads it", e.Data["reason"])

	missing := logS3APIRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.NoteRefusal(r.Context(), "admin", auth.ActionListBucket, "arn:aws:s3:::other", "no policy of the caller or of the bucket allows it")
		auth.NoteError(r.Context(), "NoSuchKey", "The specified key does not exist")
		w.WriteHeader(http.StatusNotFound)
	}))
	missing.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/b/gone", nil))
	e = logged("/b/gone")
	assert.Equal(t, logrus.InfoLevel, e.Level)
	assert.Equal(t, "NoSuchKey", e.Data["error"])
	assert.NotContains(t, e.Data, "reason", "a refusal that did not answer the request")

	stored := logS3APIRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("ETag", `"8d777f385d3dfec8815d20f7496026dc"`)
		w.Header().Set("x-amz-version-id", "1792880382314306100.85328ef1")
	}))
	stored.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/b/part?partNumber=1&uploadId=u", strings.NewReader("data")))
	e = logged("/b/part")
	assert.EqualValues(t, 4, e.Data["received"])
	assert.Equal(t, `"8d777f385d3dfec8815d20f7496026dc"`, e.Data["etag"])
	assert.Equal(t, "1792880382314306100.85328ef1", e.Data["version_id"])
	assert.NotContains(t, logged("/b/gone").Data, "received", "a request without a body")

	formatter := &logrus.JSONFormatter{FieldMap: logrus.FieldMap{
		logrus.FieldKeyTime: "timestamp", logrus.FieldKeyLevel: "level", logrus.FieldKeyMsg: "message",
	}}
	line, err := formatter.Format(logged("/b/gone"))
	require.NoError(t, err)
	var written map[string]any
	require.NoError(t, json.Unmarshal(line, &written))
	assert.Equal(t, "The specified key does not exist", written["error_message"])
	assert.Equal(t, "S3 request", written["message"])
}
