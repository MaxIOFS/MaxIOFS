package replication

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each object a worker processes is told to the task observer, failed when
// it could not be replicated.
func TestAReplicatedObjectIsObserved(t *testing.T) {
	db := setupWorkerDB(t)
	queue := make(chan *QueueItem, 1)
	worker := NewWorkerWithS3Factory(1, queue, db, &MockObjectAdapter{}, nil, nil)
	runs := make(chan string, 1)
	worker.observe = func(task string, _ time.Duration, failed bool) { runs <- fmt.Sprintf("%s:%v", task, failed) }

	queue <- insertTestQueueItem(t, db, &QueueItem{RuleID: "no-such-rule", TenantID: "tenant1", Bucket: "src", ObjectKey: "k", Action: "PUT", MaxRetries: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, make(chan struct{}))

	select {
	case run := <-runs:
		assert.Equal(t, "replication:true", run, "the rule does not exist")
	case <-time.After(10 * time.Second):
		require.Fail(t, "the item was not observed")
	}
}
