package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Replication reads a rule's source objects, and lists its bucket, under the
// rule's tenant; the listing goes page after page.
func TestReplicationAdaptersFindTheTenantsBucket(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	const tenant = "t-repl-adapter"
	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{ID: tenant, Name: tenant, Status: "active"}))
	require.NoError(t, server.bucketManager.CreateBucket(ctx, tenant, "repl-src", "u"))
	for _, k := range []string{"a", "b", "c"} {
		_, err := server.objectManager.PutObject(ctx, tenant+"/repl-src", k, strings.NewReader("data-"+k), http.Header{})
		require.NoError(t, err)
	}

	objects := &objectManagerAdapter{mgr: server.objectManager}
	reader, size, _, _, err := objects.GetObject(ctx, tenant, "repl-src", "b")
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	reader.Close()
	require.NoError(t, err)
	assert.Equal(t, "data-b", string(body))
	assert.EqualValues(t, 6, size)
	size, _, _, err = objects.GetObjectMetadata(ctx, tenant, "repl-src", "c")
	require.NoError(t, err)
	assert.EqualValues(t, 6, size)

	lister := &bucketListerAdapter{mgr: server.objectManager}
	var listed []string
	marker := ""
	for pages := 0; pages < 10; pages++ {
		keys, next, err := lister.ListObjects(ctx, tenant, "repl-src", "", marker, 2)
		require.NoError(t, err)
		listed = append(listed, keys...)
		if next == "" {
			break
		}
		marker = next
	}
	assert.Equal(t, []string{"a", "b", "c"}, listed)
}
