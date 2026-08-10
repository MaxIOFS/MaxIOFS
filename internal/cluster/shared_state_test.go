package cluster

// State that is written while the servers are already serving.
//
// loadTLSConfig runs from the initialize and join HTTP handlers, and what it
// replaces is read on the path of every inter-node request — by roughly twenty
// dynamic proxy clients and by each health tick. Plain pointer fields raced
// there, and the irony was that ProxyClient carefully guards its own rebuild
// with a mutex while the value it reads through the getter was unsynchronised.

import (
	"crypto/tls"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManagerTLSState_IsRaceFree hammers the getter while the field is being
// replaced, which is the shape of a join landing during live traffic.
func TestManagerTLSState_IsRaceFree(t *testing.T) {
	m := &Manager{}
	m.clusterHTTPClient.Store(&http.Client{})

	const rounds = 500
	var wg sync.WaitGroup
	wg.Add(3)

	// The writer: a cluster being initialised or joined.
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			m.tlsConfig.Store(&tls.Config{MinVersion: tls.VersionTLS12})
			m.clusterHTTPClient.Store(&http.Client{})
		}
	}()

	// The readers: every proxied request, and every health tick.
	for r := 0; r < 2; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if cfg := m.GetTLSConfig(); cfg != nil {
					_ = cfg.MinVersion
				}
				_ = m.clusterHTTPClient.Load()
			}
		}()
	}

	wg.Wait()

	require.NotNil(t, m.GetTLSConfig())
	assert.NotNil(t, m.clusterHTTPClient.Load())
}

// TestLeaderManager_StopIsIdempotent: a bare close panics on the second call,
// and two shutdown paths can both reach it.
func TestLeaderManager_StopIsIdempotent(t *testing.T) {
	m := &LeaderManager{stopChan: make(chan struct{})}

	assert.NotPanics(t, func() {
		m.Stop()
		m.Stop()
		m.Stop()
	}, "stopping twice must not take the process down on its way out")

	select {
	case <-m.stopChan:
	default:
		t.Fatal("Stop must close the channel the loop waits on")
	}
}

// TestStartDoesNotReplaceTheProxyClient: ten Start methods re-created the proxy
// client from the initialize/join handlers, while other goroutines were already
// reading it. The constructor installs it with the same dynamic getter, so the
// reassignment bought nothing and only introduced the race.
func TestStartDoesNotReplaceTheProxyClient(t *testing.T) {
	source := mustReadClusterSources(t)
	assert.NotContains(t, source, "m.proxyClient = NewDynamicProxyClient",
		"the proxy client is installed once, by the constructor")
}

// mustReadClusterSources concatenates the package's non-test sources.
func mustReadClusterSources(t *testing.T) string {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	require.NoError(t, err)

	var all strings.Builder
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		data, err := os.ReadFile(p)
		require.NoError(t, err)
		all.Write(data)
	}
	return all.String()
}
