package logging

import (
	"runtime"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type stuckOutput struct{ release chan struct{} }

func (s *stuckOutput) Write(*LogEntry) error { <-s.release; return nil }
func (s *stuckOutput) Close() error          { return nil }

// An output that does not take its entries holds a bounded number of them, not
// one goroutine for each entry logged.
func TestDispatchHoldsABoundedNumberOfEntriesForAStuckOutput(t *testing.T) {
	stuck := &stuckOutput{release: make(chan struct{})}
	hook := NewDispatchHook()
	hook.UpdateSnapshot([]outputWithFilter{{output: stuck, filterLevel: "debug"}})

	before := runtime.NumGoroutine()
	for i := 0; i < 5000; i++ {
		_ = hook.Fire(&logrus.Entry{Time: time.Now(), Level: logrus.InfoLevel, Message: "x", Data: logrus.Fields{}})
	}
	held := runtime.NumGoroutine() - before
	close(stuck.release)

	assert.LessOrEqual(t, held, 1100, "goroutines held by a stuck output")
}
