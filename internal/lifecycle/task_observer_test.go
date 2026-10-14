package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Each lifecycle pass is told to the task observer, failed when the buckets
// cannot be listed.
func TestALifecyclePassIsObserved(t *testing.T) {
	var runs []string
	observe := func(task string, _ time.Duration, failed bool) { runs = append(runs, fmt.Sprintf("%s:%v", task, failed)) }

	worker := NewWorker(&mockBucketMgr{}, &mockObjectMgr{}, &mockMetaStore{})
	worker.SetTaskObserver(observe)
	worker.runPass(context.Background())

	broken := NewWorker(&mockBucketMgr{listErr: errors.New("store closed")}, &mockObjectMgr{}, &mockMetaStore{})
	broken.SetTaskObserver(observe)
	broken.runPass(context.Background())

	assert.Equal(t, []string{"lifecycle:false", "lifecycle:true"}, runs)
}
