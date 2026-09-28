package s3compat

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rememberingRouter routes every bucket to node and records what it forgets.
type rememberingRouter struct {
	node      *cluster.Node
	forgotten []string
}

func (r *rememberingRouter) RouteRequest(context.Context, string) (*cluster.Node, bool, error) {
	return r.node, false, nil
}

func (r *rememberingRouter) InvalidateCache(bucket string) {
	r.forgotten = append(r.forgotten, bucket)
}

type forwardingClusterManager struct{}

func (forwardingClusterManager) IsClusterEnabled() bool { return true }
func (forwardingClusterManager) SelectReadNode(context.Context, string) (*cluster.Node, error) {
	return nil, nil
}
func (forwardingClusterManager) SelectReadNodes(context.Context, string) ([]*cluster.Node, error) {
	return nil, nil
}
func (forwardingClusterManager) ProxyRead(context.Context, http.ResponseWriter, *http.Request, *cluster.Node) error {
	return nil
}
func (forwardingClusterManager) TryProxyRead(context.Context, http.ResponseWriter, *http.Request, *cluster.Node) (bool, error) {
	return false, nil
}
func (forwardingClusterManager) GetLocalNodeID(context.Context) (string, error)    { return "here", nil }
func (forwardingClusterManager) GetLocalNodeToken(context.Context) (string, error) { return "token", nil }
func (forwardingClusterManager) GetTLSConfig() *tls.Config                          { return nil }

// A node told by the node it forwarded to that the bucket is not there forgets
// where it thought the bucket was, and passes the retryable answer on without
// the internal header. Any other answer changes nothing.
func TestForwardingNodeForgetsAStaleLocation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stale  bool
		status int
	}{
		{"stale", true, http.StatusServiceUnavailable},
		{"served", false, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.stale {
					w.Header().Set(cluster.BucketNotHereHeader, "true")
				}
				w.WriteHeader(tc.status)
			}))
			defer remote.Close()
			router := &rememberingRouter{node: &cluster.Node{Name: "remote", APIURL: remote.URL}}
			h := &Handler{}
			h.SetClusterRouter(router)
			h.SetClusterManager(forwardingClusterManager{})

			w := httptest.NewRecorder()
			require.True(t, h.proxyBucketRequest(w, httptest.NewRequest(http.MethodGet, "/moved/k", nil), "moved"))
			assert.Equal(t, tc.status, w.Code)
			assert.Empty(t, w.Header().Get(cluster.BucketNotHereHeader))
			if tc.stale {
				assert.Equal(t, []string{"moved"}, router.forgotten)
			} else {
				assert.Empty(t, router.forgotten)
			}
		})
	}
}
