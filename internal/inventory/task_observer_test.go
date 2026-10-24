package inventory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Each inventory pass is told to the task observer; a node the gate holds
// back runs none.
func TestAnInventoryPassIsObserved(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	worker := NewWorker(NewManager(db), new(MockBucketManager), new(MockMetadataStore), new(MockObjectWriter))
	var runs []string
	worker.SetTaskObserver(func(task string, _ time.Duration, failed bool) {
		runs = append(runs, fmt.Sprintf("%s:%v", task, failed))
	})

	worker.runPass(context.Background())
	worker.SetRunGate(func() bool { return false })
	worker.runPass(context.Background())

	assert.Equal(t, []string{"inventory:false"}, runs)
}
