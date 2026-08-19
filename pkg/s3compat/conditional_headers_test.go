package s3compat

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// If-Match and If-None-Match accept "*", one entity tag, or a comma-separated
// list. Comparing the header as a single opaque string fails every list a
// client sends, which is how a common "read it only if unchanged" guard broke.
func TestETagSatisfies(t *testing.T) {
	const etag = `"abc123"`

	cases := []struct {
		condition string
		satisfied bool
	}{
		{`*`, true},
		{`"abc123"`, true},
		{`abc123`, true},
		{`"other"`, false},
		{`"other", "abc123"`, true},
		{`"abc123", "other"`, true},
		{`"one", "two", "three"`, false},
		{`W/"abc123"`, true},
		{`  "abc123"  `, true},
		{``, false},
	}

	for _, tc := range cases {
		t.Run(tc.condition, func(t *testing.T) {
			assert.Equal(t, tc.satisfied, etagSatisfies(etag, tc.condition))
		})
	}
}
