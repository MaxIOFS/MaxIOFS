package metadata

import (
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// Two shutdown paths can reach Close, and closing the stop channel twice is a
// panic that takes the process down during an orderly shutdown.
func TestPebbleStore_CloseIsIdempotent(t *testing.T) {
	dir, err := os.MkdirTemp("", "maxiofs-close-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewPebbleStore(PebbleOptions{DataDir: dir, Logger: logrus.StandardLogger()})
	require.NoError(t, err)

	require.NoError(t, store.Close())
	require.NotPanics(t, func() {
		require.NoError(t, store.Close())
		require.NoError(t, store.Close())
	}, "closing an already closed store must not panic")
}
