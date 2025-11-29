package logging

import (
	"time"
)

// Output represents a log output destination
type Output interface {
	Write(entry *LogEntry) error
	Close() error
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}
