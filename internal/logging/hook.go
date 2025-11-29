package logging

import (
	"github.com/sirupsen/logrus"
)

// OutputHook is a logrus hook that sends logs to an Output
type OutputHook struct {
	output Output
}

// NewOutputHook creates a new output hook
func NewOutputHook(output Output) *OutputHook {
	return &OutputHook{
		output: output,
	}
}

// Levels returns the log levels this hook should fire for
func (h *OutputHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire is called when a log event occurs
func (h *OutputHook) Fire(entry *logrus.Entry) error {
	// Convert logrus entry to our LogEntry format
	logEntry := &LogEntry{
		Timestamp: entry.Time,
		Level:     entry.Level.String(),
		Message:   entry.Message,
		Fields:    make(map[string]interface{}),
	}

	// Copy fields
	for k, v := range entry.Data {
		logEntry.Fields[k] = v
	}

	// Send to output (non-blocking, errors are logged internally)
	go func() {
		if err := h.output.Write(logEntry); err != nil {
			// Use standard logrus to avoid recursion
			entry.Logger.WithError(err).Warn("Failed to write to log output")
		}
	}()

	return nil
}
