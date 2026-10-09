package server

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

// newClusterTestNode is a whole server on its own data directory, its routes
// set up.
func newClusterTestNode(t *testing.T) *Server {
	t.Helper()
	return newClusterTestNodeWith(t, nil)
}

// newClusterTestNodeWith is newClusterTestNode with its configuration changed
// by tweak before the server is built.
func newClusterTestNodeWith(t *testing.T, tweak func(*config.Config)) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "maxiofs-cluster-node-*")
	require.NoError(t, err)
	cfg := &config.Config{
		Listen:           "127.0.0.1:0",
		ConsoleListen:    "127.0.0.1:0",
		DataDir:          dir,
		LogLevel:         "error",
		PublicAPIURL:     "http://localhost:8080",
		PublicConsoleURL: "http://localhost:8081",
		Storage:          config.StorageConfig{Backend: "filesystem", Root: filepath.Join(dir, "storage")},
		Auth: config.AuthConfig{
			EnableAuth:       true,
			JWTSecret:        "test-jwt-secret-shared",
			EncryptionSecret: "test-secret-key",
		},
		Audit: config.AuditConfig{RetentionDays: 7, DBPath: filepath.Join(dir, "audit.db")},
	}
	if tweak != nil {
		tweak(cfg)
	}
	srv, err := New(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	srv.serverCtx = ctx
	require.NoError(t, srv.setupRoutes())
	t.Cleanup(func() {
		cancel()
		_ = srv.shutdown()
		_ = os.RemoveAll(dir)
	})
	return srv
}

func readBody(t *testing.T, s *Server, path, key string, versionID ...string) string {
	t.Helper()
	_, reader, err := s.objectManager.GetObject(context.Background(), path, key, versionID...)
	require.NoError(t, err)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(data)
}

// hide marks a bucket as a copy that is not where the bucket lives.
func hide(t *testing.T, s *Server, name, marker string) {
	t.Helper()
	b, err := s.metadataStore.GetBucketByName(context.Background(), name)
	require.NoError(t, err)
	if b.Metadata == nil {
		b.Metadata = map[string]string{}
	}
	b.Metadata[metadata.BucketMovingKey] = marker
	require.NoError(t, s.metadataStore.UpdateBucket(context.Background(), b))
}
