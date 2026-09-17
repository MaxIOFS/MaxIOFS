package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

type checkpointBarrierStore struct {
	metadata.Store
	metadata.RawKVStore
	entered chan struct{}
	release chan struct{}
	written chan error
}

func (s *checkpointBarrierStore) PutRaw(ctx context.Context, key string, value []byte) error {
	var state encryptionWorkerState
	if key == encWorkerStateKey && json.Unmarshal(value, &state) == nil && state.Status == "done" {
		close(s.entered)
		<-s.release
		err := s.RawKVStore.PutRaw(ctx, key, value)
		s.written <- err
		return err
	}
	return s.RawKVStore.PutRaw(ctx, key, value)
}

func TestShutdownWaitsForRotatingKEKCheckpoint(t *testing.T) {
	s := newIsolatedEncryptionServer(t)
	ctx := context.Background()
	require.NoError(t, s.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: "rotation", OwnerID: "admin"}))
	_, err := s.objectManager.PutObject(ctx, "rotation", "key", strings.NewReader("content"), http.Header{})
	require.NoError(t, err)
	store := &checkpointBarrierStore{
		Store: s.metadataStore, RawKVStore: s.metadataStore.(metadata.RawKVStore),
		entered: make(chan struct{}), release: make(chan struct{}), written: make(chan error, 1),
	}
	s.metadataStore = store
	s.httpServer = &http.Server{}
	s.consoleServer = &http.Server{}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(store.release) }) })
	req := createAuthenticatedRequest("POST", "/api/v1/settings/encryption/rotate-kek", nil, "", "admin-user", true)
	w := httptest.NewRecorder()
	s.handleRotateKEK(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("rotation never reached its final checkpoint")
	}
	done := make(chan error, 1)
	go func() { done <- s.shutdown() }()
	require.Eventually(t, func() bool {
		s.encWorkerMu.Lock()
		defer s.encWorkerMu.Unlock()
		return s.encWorkersClosed
	}, time.Second, time.Millisecond)
	require.False(t, s.goEncryptionWorker("late rotation", func() { t.Error("late worker ran") }))
	select {
	case err := <-done:
		t.Fatalf("shutdown returned before checkpoint completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.True(t, store.IsReady(), "Pebble must stay open while the checkpoint is pending")
	release.Do(func() { close(store.release) })
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish after releasing the checkpoint")
	}
	require.NoError(t, <-store.written)
	require.False(t, store.IsReady())
}

func TestStopEncryptionWorkersCancelsPeriodicPass(t *testing.T) {
	s := newIsolatedEncryptionServer(t)
	s.startEncryptionWorker(context.Background())
	s.stopEncryptionWorkers()
	require.False(t, s.startEncryptionPass(context.Background()))
	s.startEncryptionWorker(context.Background())
	s.stopEncryptionWorkers()
}
