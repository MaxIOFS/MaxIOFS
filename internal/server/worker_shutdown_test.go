package server

import (
	"bufio"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Shutdown closes the metadata store, and Pebble panics when it is used after
// close. So every background worker has to be finished before that happens —
// waiting for them is not tidiness, it is what keeps a shutdown from taking the
// process down with it.
func TestGoWorker_ShutdownWaitsForWorkersToReturn(t *testing.T) {
	s := &Server{}

	var finished atomic.Bool
	release := make(chan struct{})

	require.True(t, s.goWorker("slow worker", func() {
		<-release
		finished.Store(true)
	}))

	waited := make(chan struct{})
	go func() {
		s.workers.Wait()
		close(waited)
	}()

	select {
	case <-waited:
		t.Fatal("the wait returned while a worker was still running")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case <-waited:
		assert.True(t, finished.Load(), "the worker finished before the wait returned")
	case <-time.After(5 * time.Second):
		t.Fatal("the wait never returned after the worker finished")
	}
}

// Every worker started through goWorker must be part of the shutdown wait.
func TestGoWorker_TracksEveryWorker(t *testing.T) {
	s := &Server{}
	var started atomic.Int32
	release := make(chan struct{})

	const workerCount = 32
	for i := 0; i < workerCount; i++ {
		require.True(t, s.goWorker("worker", func() {
			started.Add(1)
			<-release
		}))
	}

	require.Eventually(t, func() bool { return started.Load() == workerCount },
		2*time.Second, 10*time.Millisecond)

	close(release)
	s.workers.Wait()
}

func TestGoWorker_RejectsAfterShutdownStarts(t *testing.T) {
	s := &Server{}
	s.stopAcceptingWorkers()

	var ran atomic.Bool
	assert.False(t, s.goWorker("late worker", func() {
		ran.Store(true)
	}))

	s.workers.Wait()
	assert.False(t, ran.Load(), "late worker must not run after shutdown starts")
}

func TestEncryptionWorker_RejectsAfterShutdownStarts(t *testing.T) {
	s := &Server{}
	s.stopAcceptingEncryptionWorkers()

	var ran atomic.Bool
	assert.False(t, s.goEncryptionWorker("late encryption worker", func() {
		ran.Store(true)
	}))

	s.encWorkerWG.Wait()
	assert.False(t, ran.Load(), "late encryption worker must not run after shutdown starts")
}

func TestEnableClusterTLSRejectsAfterShutdownStarts(t *testing.T) {
	s := &Server{}
	s.shuttingDown.Store(true)

	require.ErrorIs(t, s.enableClusterTLS(), errServerShuttingDown)
}

// A shutdown ends the console's notification streams: a browser that keeps
// one open does not hold the process.
func TestAShutdownEndsTheNotificationStreams(t *testing.T) {
	s := newClusterTestNode(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = s.consoleServer.Serve(ln) }()

	admin := &auth.User{ID: "stream-admin", Username: "stream-admin", Status: auth.UserStatusActive,
		Roles: []string{auth.RoleAdmin}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	require.NoError(t, s.authManager.CreateUser(t.Context(), admin))
	token, err := s.authManager.GenerateJWT(t.Context(), admin)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/api/v1/notifications/stream", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	first, err := bufio.NewReader(resp.Body).ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, first, "connected", "the stream is open")

	done := make(chan struct{})
	go func() {
		_ = s.shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the shutdown waits on the open stream")
	}
}
