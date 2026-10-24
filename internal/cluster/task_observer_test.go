package cluster

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The cluster's background tasks are told to the observer set on the
// manager; none is told before one is set.
func TestClusterTasksAreObserved(t *testing.T) {
	m := &Manager{}
	assert.NoError(t, m.observeTask("anti_entropy", func() error { return nil }))

	var runs []string
	m.SetTaskObserver(func(task string, _ time.Duration, failed bool) {
		runs = append(runs, fmt.Sprintf("%s:%v", task, failed))
	})
	_ = m.observeTask("anti_entropy", func() error { return nil })
	_ = m.observeTask("dead_node_check", func() error { return errors.New("store closed") })

	assert.Equal(t, []string{"anti_entropy:false", "dead_node_check:true"}, runs)
}
