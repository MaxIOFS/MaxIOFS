package object

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/recovery"
	"github.com/stretchr/testify/require"
)

func TestReconcileWaitsForObjectCommit(t *testing.T) {
	for _, tenant := range []string{"", "tenant"} {
		t.Run(fmt.Sprintf("tenant=%s", tenant), func(t *testing.T) {
			root := t.TempDir()
			m, s := openFaultManager(t, root)
			defer s.Close()
			bucket := "fault"
			if tenant != "" {
				bucket = tenant + "/" + bucket
			}
			require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "fault", TenantID: tenant, OwnerID: "owner"}))
			require.NoError(t, m.storage.CreateBucket(t.Context(), bucket))
			_, err := m.PutObject(t.Context(), bucket, "key", strings.NewReader(faultOld), http.Header{})
			require.NoError(t, err)
			published, release := make(chan struct{}), make(chan struct{})
			var released, signaled sync.Once
			unblock := func() { released.Do(func() { close(release) }) }
			m.storage = &faultPutBackend{Backend: m.storage, after: func() {
				signaled.Do(func() { close(published) })
				<-release
			}}
			writerDone, writerResult := make(chan struct{}), make(chan error, 1)
			go func() {
				defer close(writerDone)
				_, err := m.PutObject(t.Context(), bucket, "key", strings.NewReader(faultNew), http.Header{})
				writerResult <- err
			}()
			defer func() { unblock(); <-writerDone }()
			select {
			case <-published:
			case <-time.After(10 * time.Second):
				t.Fatal("write did not publish")
			}
			recovered, recoveryResult := make(chan struct{}), make(chan error, 1)
			go func() {
				defer close(recovered)
				r, err := recovery.Reconcile(t.Context(), root, s, nil)
				if err == nil && len(r.Failures) > 0 {
					err = fmt.Errorf("recovery failures: %v", r.Failures)
				}
				recoveryResult <- err
			}()
			defer func() { unblock(); <-recovered }()
			select {
			case <-recovered:
				t.Fatal("recovery raced the uncommitted write")
			case <-time.After(100 * time.Millisecond):
			}
			unblock()
			require.NoError(t, <-writerResult)
			require.NoError(t, <-recoveryResult)
			o, data := readWholeObject(t, m, bucket, "key")
			require.Equal(t, faultNew, data)
			require.Equal(t, int64(len(data)), o.Size)
		})
	}
}
