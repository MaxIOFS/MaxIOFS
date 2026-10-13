package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A factor changed while a repair runs is acted on once that repair ends. It
// waited for the next anti-entropy cycle, hours later: the repair asked for
// was dropped because one was running, with the factor it had started with.
func TestAFactorChangedDuringARepairIsActedOnAfterIt(t *testing.T) {
	c := newHACluster(t, 3, 1)
	ctx := context.Background()
	// The node with the lowest ID repairs the writes it holds.
	a := firstOf(c.nodes...)
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "during", routingAdminID))
	_, err := a.objectManager.PutObject(ctx, "during", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)

	// The first copy waits until the factor is raised again.
	release := make(chan struct{})
	inFlight := make(chan struct{})
	var once sync.Once
	block := func() {
		once.Do(func() {
			close(inFlight)
			<-release
		})
	}
	for _, n := range c.nodes {
		n.onCopy.Store(&block)
	}
	setFactor := func(factor int) {
		w := httptest.NewRecorder()
		a.handleSetClusterHA(w, asGlobalAdmin(httptest.NewRequest(http.MethodPut, "/api/v1/cluster/ha",
			strings.NewReader(fmt.Sprintf(`{"factor":%d}`, factor))), nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	setFactor(2)
	select {
	case <-inFlight:
	case <-time.After(15 * time.Second):
		t.Fatal("the repair for a factor of 2 made no copy")
	}
	setFactor(3)
	close(release)

	require.Eventually(t, func() bool {
		holders := 0
		for _, n := range c.nodes {
			if n.holds(t, "during", "k", "") {
				holders++
			}
		}
		return holders == 3
	}, 15*time.Second, 50*time.Millisecond, "the write gets the copies of the factor raised during the repair")
}
