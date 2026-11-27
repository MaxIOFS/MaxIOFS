package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The public S3 URL is the base share links and presigned URLs are built on:
// a trailing slash would give their paths a second one.
func TestThePublicAPIURLIsABase(t *testing.T) {
	for given, want := range map[string]string{
		"https://s3.example.com/":  "https://s3.example.com",
		"https://s3.example.com//": "https://s3.example.com",
		"https://s3.example.com":   "https://s3.example.com",
		"https://example.com/s3/":  "https://example.com/s3",
	} {
		cfg := &Config{DataDir: t.TempDir(), PublicAPIURL: given}
		require.NoError(t, validate(cfg))
		assert.Equal(t, want, cfg.PublicAPIURL, given)
	}
}
