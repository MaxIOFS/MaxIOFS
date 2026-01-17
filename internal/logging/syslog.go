package logging

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
)

// SyslogOutput sends logs to a syslog server using raw TCP/UDP
// This implementation works on all platforms (Windows, Linux, macOS)
type SyslogOutput struct {
	conn     net.Conn
	protocol string
	addr     string
	tag      string
	mu       sync.Mutex
}

// Syslog severity levels (RFC 5424)
const (
	severityEmergency = 0
	severityAlert     = 1
	severityCritical  = 2
	severityError     = 3
	severityWarning   = 4
	severityNotice    = 5
	severityInfo      = 6
	severityDebug     = 7
)

// Syslog facility (LOG_DAEMON = 3)
const facilityDaemon = 3

// NewSyslogOutput creates a new syslog output
func NewSyslogOutput(protocol, host string, port int, tag string) (*SyslogOutput, error) {
	// Use net.JoinHostPort to properly handle both IPv4 and IPv6 addresses
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	// Connect to syslog server
	conn, err := net.Dial(protocol, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to syslog: %w", err)
	}

	return &SyslogOutput{
		conn:     conn,
		protocol: protocol,
		addr:     addr,
		tag:      tag,
	}, nil
}

// Write sends a log entry to syslog
func (s *SyslogOutput) Write(entry *LogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return fmt.Errorf("syslog connection is closed")
	}

	// Convert entry to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	// Determine syslog severity based on log level
	severity := severityInfo
	switch entry.Level {
	case "debug":
		severity = severityDebug
	case "info":
		severity = severityInfo
	case "warn", "warning":
		severity = severityWarning
	case "error":
		severity = severityError
	case "fatal", "panic":
		severity = severityCritical
	}

	// Calculate priority: facility * 8 + severity
	priority := facilityDaemon*8 + severity

	// Format syslog message (RFC 3164 format)
	// <priority>timestamp hostname tag: message
	message := fmt.Sprintf("<%d>%s %s[%d]: %s\n",
		priority,
		entry.Timestamp.Format("Jan 2 15:04:05"),
		s.tag,
		0, // PID, use 0 for now
		string(data),
	)

	// Send to syslog server
	_, err = s.conn.Write([]byte(message))
	if err != nil {
		// Try to reconnect on write failure
		s.conn.Close()
		conn, reconnectErr := net.Dial(s.protocol, s.addr)
		if reconnectErr != nil {
			s.conn = nil
			return fmt.Errorf("failed to write to syslog and reconnect failed: %w", err)
		}
		s.conn = conn

		// Retry write
		_, err = s.conn.Write([]byte(message))
		if err != nil {
			return fmt.Errorf("failed to write to syslog after reconnect: %w", err)
		}
	}

	return nil
}

// Close closes the syslog connection
func (s *SyslogOutput) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn != nil {
		err := s.conn.Close()
		s.conn = nil
		return err
	}

	return nil
}
