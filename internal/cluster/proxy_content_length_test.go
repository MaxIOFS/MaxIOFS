package cluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A body forwarded without its length goes out chunked, and the node receiving
// it sees ContentLength -1 — which its tenant quota check reads as "nothing to
// account for". Both proxy paths must carry the length across.
func TestProxy_CarriesContentLengthToTheTargetNode(t *testing.T) {
	const payload = "twenty-one characters"

	var seen struct {
		length   int64
		encoding []string
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.length = r.ContentLength
		seen.encoding = r.TransferEncoding
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	client := NewProxyClient(nil)

	t.Run("ProxyRequest", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/bucket/key", strings.NewReader(payload))
		req.ContentLength = int64(len(payload))

		resp, err := client.ProxyRequest(context.Background(),
			&Node{ID: "n1", Name: "n1", Endpoint: target.URL}, req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.EqualValues(t, len(payload), seen.length)
		assert.Empty(t, seen.encoding, "the body should not be chunked")
	})

	t.Run("ProxyToNodeAPIURL", func(t *testing.T) {
		seen.length, seen.encoding = 0, nil
		req := httptest.NewRequest("PUT", "/bucket/key", strings.NewReader(payload))
		req.ContentLength = int64(len(payload))

		resp, err := client.ProxyToNodeAPIURL(context.Background(),
			&Node{ID: "n1", Name: "n1", APIURL: target.URL}, req, "n1", "token", "user-1", "", "admin")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.EqualValues(t, len(payload), seen.length)
		assert.Empty(t, seen.encoding, "the body should not be chunked")
	})
}

func TestBuildInternalS3URL_PreservesExplicitAPIURL(t *testing.T) {
	got := buildInternalS3URL(&Node{
		Endpoint: "http://cluster-peer.internal:8082",
		APIURL:   "https://s3-peer.internal:9443/",
	})

	assert.Equal(t, "https://s3-peer.internal:9443", got)
}
