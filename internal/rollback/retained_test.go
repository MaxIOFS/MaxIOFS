package rollback

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A copy retained while the clock reads no later than the last one, in the
// same nanosecond or after the clock stepped back, still comes after it.
func TestACopyIsRetainedAfterTheOneBefore(t *testing.T) {
	ahead := time.Now().Add(time.Hour).UnixNano()
	lastRetained.Store(ahead)
	first := retainedNow()
	second := retainedNow()
	require.Greater(t, first, ahead)
	require.Greater(t, second, first)
}
