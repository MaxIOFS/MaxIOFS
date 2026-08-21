package clusterauth

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseRequest() Request {
	return Request{
		NodeID:    "node-1",
		Timestamp: "1700000000",
		Nonce:     "nonce-1",
		Method:    "PUT",
		Path:      "/internal/objects",
		Query:     "bucket=a&key=x",
		BodyHash:  EmptyBodyHash,
		User:      "admin",
		Tenant:    "",
		Roles:     "admin",
	}
}

// Every field of Request must be bound. Walking the struct means a field added
// later and left out of Payload fails here rather than silently going unsigned,
// which is what happened to the query string.
func TestSign_BindsEveryField(t *testing.T) {
	const key = "cluster-token"
	original := Sign(key, baseRequest())
	require.NotEmpty(t, original)

	v := reflect.ValueOf(baseRequest())
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name

		altered := baseRequest()
		f := reflect.ValueOf(&altered).Elem().Field(i)
		require.Equal(t, reflect.String, f.Kind(), "Request gained a non-string field: %s", name)
		f.SetString(v.Field(i).String() + "-changed")

		assert.NotEqual(t, original, Sign(key, altered),
			"changing %s left the signature unchanged, so it is not covered", name)
	}

	assert.NotEqual(t, original, Sign("another-token", baseRequest()),
		"a different key produced the same signature")
}

// Fields are joined, so a value must not be able to impersonate a boundary and
// shift meaning between components.
func TestPayload_ComponentsCannotBeConfused(t *testing.T) {
	a := baseRequest()
	a.Path, a.Query = "/x", "b=1"

	b := baseRequest()
	b.Path, b.Query = "/x\nb=1", ""

	assert.NotEqual(t, a.Payload(), b.Payload(),
		"a path containing a separator collides with the query field")
}

func TestNewNonce_IsUnique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		n := NewNonce()
		require.NotEmpty(t, n)
		require.False(t, seen[n], "nonce repeated after %d draws", i)
		seen[n] = true
	}
}
