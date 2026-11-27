package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An inventory is scheduled at a time of day, HH:MM; another is refused as the
// request's fault.
func TestAnInventoryIsScheduledAtATimeOfDay(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	admin := storedUser(t, s, "admin")
	for _, name := range []string{"source", "reports"} {
		require.NoError(t, s.bucketManager.CreateBucket(ctx, "", name, ""))
	}
	put := func(schedule string) *httptest.ResponseRecorder {
		body := `{"enabled":true,"frequency":"daily","format":"csv","destination_bucket":"reports","schedule_time":"` + schedule + `"}`
		req := mux.SetURLVars(consoleRequestWith(admin, body, "s3:*"), map[string]string{"bucket": "source"})
		rec := httptest.NewRecorder()
		s.handlePutBucketInventory(rec, req)
		return rec
	}
	for _, schedule := range []string{"25:99", "noon", "7"} {
		assert.Equal(t, http.StatusBadRequest, put(schedule).Code, schedule)
	}
	rec := put("06:15")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "06:15")
}
