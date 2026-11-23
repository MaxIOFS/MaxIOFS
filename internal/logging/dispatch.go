package logging

import (
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/sirupsen/logrus"
)

// maxPendingWrites bounds the entries waiting on one output. A target that does
// not take them loses the entries past it, not the node's memory.
const maxPendingWrites = 1000

// DispatchHook is a single logrus hook that dispatches log entries
type DispatchHook struct {
	snapshot atomic.Pointer[[]outputWithFilter]
	pending  sync.Map // Output → *atomic.Int64, entries being written to it
}

// NewDispatchHook creates a dispatch hook
func NewDispatchHook() *DispatchHook {
	h := &DispatchHook{}
	empty := make([]outputWithFilter, 0)
	h.snapshot.Store(&empty)
	return h
}

// UpdateSnapshot replaces the current outputs snapshot (called by Manager under write lock)
func (h *DispatchHook) UpdateSnapshot(outputs []outputWithFilter) {
	h.snapshot.Store(&outputs)
	h.pending.Range(func(key, _ any) bool {
		for _, ow := range outputs {
			if ow.output == key {
				return true
			}
		}
		h.pending.Delete(key)
		return true
	})
}

// pendingOf returns the count of entries being written to out, nil for an
// output that cannot be a map key.
func (h *DispatchHook) pendingOf(out Output) *atomic.Int64 {
	if !reflect.TypeOf(out).Comparable() {
		return nil
	}
	if v, ok := h.pending.Load(out); ok {
		return v.(*atomic.Int64)
	}
	v, _ := h.pending.LoadOrStore(out, new(atomic.Int64))
	return v.(*atomic.Int64)
}

// Levels returns all log levels this hook handles
func (h *DispatchHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire is called for each log entry. It dispatches to all active outputs
// respecting each target's filter level. This method is lock-free.
func (h *DispatchHook) Fire(entry *logrus.Entry) error {
	// Convert logrus entry to our LogEntry format
	logEntry := &LogEntry{
		Timestamp: entry.Time,
		Level:     entry.Level.String(),
		Message:   entry.Message,
		Fields:    make(map[string]interface{}, len(entry.Data)),
	}

	for k, v := range entry.Data {
		logEntry.Fields[k] = v
	}

	// Load snapshot atomically — no lock needed
	snapshot := h.snapshot.Load()
	if snapshot == nil {
		return nil
	}

	for _, ow := range *snapshot {
		if !shouldDispatch(logEntry.Level, ow.filterLevel) {
			continue
		}

		// Send to output asynchronously to avoid blocking the logging pipeline
		out := ow.output
		pending := h.pendingOf(out)
		if pending != nil && pending.Add(1) > maxPendingWrites {
			pending.Add(-1)
			continue
		}
		le := logEntry
		go func() {
			if pending != nil {
				defer pending.Add(-1)
			}
			// Errors are dropped: logging them here would log again.
			_ = out.Write(le)
		}()
	}

	return nil
}

// shouldDispatch checks if a log entry should be sent to a target
// based on the target's minimum filter level
func shouldDispatch(entryLevel, filterLevel string) bool {
	levelOrder := map[string]int{
		"debug":   0,
		"info":    1,
		"warn":    2,
		"warning": 2,
		"error":   3,
		"fatal":   4,
		"panic":   5,
	}

	entryOrd, ok := levelOrder[entryLevel]
	if !ok {
		entryOrd = 1 // default to info
	}

	filterOrd, ok := levelOrder[filterLevel]
	if !ok {
		filterOrd = 1 // default to info
	}

	return entryOrd >= filterOrd
}
