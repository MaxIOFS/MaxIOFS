package cluster

// Every entry point that shares the proxy client has to be bounded.
//
// The client carries object data, so its overall Timeout was removed — it was a
// maximum file size expressed as a maximum duration. What replaced it is a
// progress watchdog, and the replacement has to cover EVERY caller of the
// client: one converted entry point and two left on a raw .Do() is strictly
// worse than the timeout it replaced, because those two then have no bound at
// all and a peer that answers and falls silent holds the goroutine forever.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProxyClient_EveryEntryPointIsBounded reads the source rather than the
// behaviour on purpose: the failure it guards against is a NEW call site added
// later without the watchdog, which no behavioural test of the existing three
// would ever notice.
func TestProxyClient_EveryEntryPointIsBounded(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(".", "proxy.go"))
	require.NoError(t, err)

	raw := regexp.MustCompile(`getHTTPClient\(\)\.Do\(`)
	if locs := raw.FindAllIndex(source, -1); len(locs) > 0 {
		var lines []string
		for _, loc := range locs {
			lines = append(lines, "line "+lineOf(source, loc[0]))
		}
		t.Fatalf("the shared client is used without the progress watchdog at %s; "+
			"it has no overall Timeout, so a raw Do() has no bound at all — "+
			"use transfer.Do(p.getHTTPClient(), req, transfer.DefaultStallTimeout)",
			strings.Join(lines, ", "))
	}

	// And the watchdog is actually in use, so the check above cannot pass by
	// the calls having been deleted.
	assert.GreaterOrEqual(t, strings.Count(string(source), "transfer.Do("), 3,
		"all three entry points route through the watchdog")
}

func lineOf(source []byte, offset int) string {
	return strconv.Itoa(1 + strings.Count(string(source[:offset]), "\n"))
}
