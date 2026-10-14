package api

import (
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
