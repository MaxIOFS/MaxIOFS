package logging

import (
	"sync/atomic"

	"github.com/sirupsen/logrus"
)

// DispatchHook is a single logrus hook that dispatches log entries
// to all active outputs managed by the Manager. This replaces the
// old pattern of adding a new hook per output (which leaked hooks
// since logrus has no RemoveHook).
//
// The outputs snapshot is stored atomically, so Fire() never acquires
// any mutex — this prevents deadlocks when Reconfigure() holds the
// write lock and logs internally via logrus.
type DispatchHook struct {
	snapshot atomic.Pointer[[]outputWithFilter]
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
		le := logEntry
		go func() {
			if err := out.Write(le); err != nil {
				// Avoid recursion: do NOT use logrus here
				// Errors are silently dropped to prevent log storms
			}
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
