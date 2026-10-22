package s3compat

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// A conditional write replaces the object only as its condition allows:
// If-Match of another ETag, or of a key with no object, is refused; of its
// own ETag, quoted or not, it replaces it. Of the writes of one new key with
// If-None-Match *, one is taken.
func TestS3AConditionalWriteHoldsToItsCondition(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "cond", env.userID))
	put := func(key, body string, headers map[string]string) int {
		t.Helper()
		return env.sendSigned("PUT", "/cond/"+key, []byte(body), headers).Code
	}
	read := func(key string) string { return env.sendSigned("GET", "/cond/"+key, nil, nil).Body.String() }

	require.Equal(t, http.StatusOK, put("k", "first", nil))
	etag := env.sendSigned("HEAD", "/cond/k", nil, nil).Header().Get("ETag")
	require.NotEmpty(t, etag)

	assert.Equal(t, http.StatusPreconditionFailed, put("k", "second", map[string]string{"If-Match": `"00000000000000000000000000000000"`}))
	assert.Equal(t, "first", read("k"), "a refused write leaves it as it was")
	assert.Equal(t, http.StatusNotFound, put("absent", "x", map[string]string{"If-Match": etag}))
	assert.Equal(t, http.StatusOK, put("k", "second", map[string]string{"If-Match": `"` + etag + `"`}))
	assert.Equal(t, "second", read("k"))
	assert.Equal(t, http.StatusPreconditionFailed, put("k", "third", map[string]string{"If-None-Match": "*"}))

	codes := make([]int, 10)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = put("once", fmt.Sprintf("writer %d", i), map[string]string{"If-None-Match": "*"})
		}(i)
	}
	wg.Wait()
	taken := 0
	for _, code := range codes {
		if code == http.StatusOK {
			taken++
		} else {
			assert.Equal(t, http.StatusPreconditionFailed, code)
		}
	}
	assert.Equal(t, 1, taken, "one write of a new key with If-None-Match * is taken")
}
