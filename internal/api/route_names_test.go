package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every S3 route is named after its S3 operation: the metrics count requests
// by that name, and an unnamed route is counted as Unknown.
func TestEveryS3RouteIsNamed(t *testing.T) {
	handler, _, _, _ := setupTestHandler()
	router := mux.NewRouter()
	handler.RegisterRoutes(router)
	routes := 0
	require.NoError(t, router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		if route.GetHandler() == nil {
			return nil // a subrouter's prefix
		}
		routes++
		template, _ := route.GetPathTemplate()
		methods, _ := route.GetMethods()
		assert.NotEmpty(t, route.GetName(), "%v %s has no name", methods, template)
		return nil
	}))
	assert.Greater(t, routes, 90)
}

// A copy is counted as CopyObject; a presigned PUT, a copy too, stays with the
// presigned route, which validates its signature.
func TestACopyIsNamedCopyObject(t *testing.T) {
	handler, _, _, _ := setupTestHandler()
	router := mux.NewRouter()
	handler.RegisterRoutes(router)
	named := func(target string, copySource bool) string {
		r := httptest.NewRequest(http.MethodPut, target, nil)
		if copySource {
			r.Header.Set("x-amz-copy-source", "/src/k")
		}
		var match mux.RouteMatch
		require.True(t, router.Match(r, &match), target)
		return match.Route.GetName()
	}
	assert.Equal(t, "CopyObject", named("/b/k", true))
	assert.Equal(t, "PutObject", named("/b/k", false))
	assert.Equal(t, "UploadPart", named("/b/k?partNumber=1&uploadId=u", true))
	r := httptest.NewRequest(http.MethodPut, "/b/k?X-Amz-Algorithm=AWS4-HMAC-SHA256", nil)
	r.Header.Set("x-amz-copy-source", "/src/k")
	var match mux.RouteMatch
	require.True(t, router.Match(r, &match))
	assert.Equal(t, "PutObject", match.Route.GetName())
	assert.Equal(t, "AWS4-HMAC-SHA256", match.Vars["algorithm"], "the presigned route")
}
