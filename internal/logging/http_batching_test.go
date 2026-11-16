package logging

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An HTTP target's batch is allocated whole and its interval drives a ticker:
// a mistyped size would take the node's memory, a mistyped interval overflows
// to a negative one and panics. Neither is saved nor tested.
func TestHTTPTarget_BatchingOutOfRangeIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		batchSize, flushInterval int
		wantErr                  string
	}{
		{"batch size", 1 << 40, 0, "batch size"},
		{"flush interval", 0, 9_300_000_000, "flush interval"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := TargetConfig{
				Name: "siem", Type: "http", Enabled: true, FilterLevel: "info",
				URL:       "https://logs.example.com/ingest",
				BatchSize: tc.batchSize, FlushInterval: tc.flushInterval,
			}

			saved := cfg
			err := newTestStore(t).Create(&saved)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)

			tested := cfg
			err = NewManager(logrus.New()).TestTargetConfig(&tested)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// The largest batching is saved, and 0 still means the default.
func TestHTTPTarget_BatchingInRangeIsSaved(t *testing.T) {
	store := newTestStore(t)
	for i, cfg := range []TargetConfig{
		{BatchSize: maxHTTPBatchSize, FlushInterval: maxHTTPFlushInterval},
		{},
	} {
		cfg.Name, cfg.Type, cfg.Enabled, cfg.FilterLevel = "siem"+string(rune('a'+i)), "http", true, "info"
		cfg.URL = "https://logs.example.com/ingest"
		require.NoError(t, store.Create(&cfg))
	}
}
