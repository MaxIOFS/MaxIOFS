package logging

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SyslogOutput sends logs to a syslog server using raw TCP/UDP
// This implementation works on all platforms (Windows, Linux, macOS)
// and supports TLS, RFC 3164 and RFC 5424 formats.
type SyslogOutput struct {
	conn      net.Conn
	protocol  string
	addr      string
	tag       string
	format    string // "rfc3164" or "rfc5424"
	tlsConfig *tls.Config
	mu        sync.Mutex
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

// SyslogConfig holds configuration for creating a syslog output
type SyslogConfig struct {
	Protocol      string
	Host          string
	Port          int
	Tag           string
	Format        string // "rfc3164" or "rfc5424"
	TLSEnabled    bool
	TLSCert       string // PEM certificate for mTLS
	TLSKey        string // PEM key for mTLS
	TLSCA         string // CA certificate PEM
	TLSSkipVerify bool
}

// NewSyslogOutput creates a new syslog output (backward-compatible constructor)
func NewSyslogOutput(protocol, host string, port int, tag string) (*SyslogOutput, error) {
	return NewSyslogOutputWithConfig(SyslogConfig{
		Protocol: protocol,
		Host:     host,
		Port:     port,
		Tag:      tag,
		Format:   "rfc3164",
	})
}

// NewSyslogOutputWithConfig creates a syslog output from a full config
func NewSyslogOutputWithConfig(cfg SyslogConfig) (*SyslogOutput, error) {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))

	if cfg.Format == "" {
		cfg.Format = "rfc3164"
	}

	output := &SyslogOutput{
		protocol: cfg.Protocol,
		addr:     addr,
		tag:      cfg.Tag,
		format:   cfg.Format,
	}

	// Build TLS config if needed
	if cfg.TLSEnabled || cfg.Protocol == "tcp+tls" {
		tlsCfg, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to build TLS config: %w", err)
		}
		output.tlsConfig = tlsCfg
	}

	// Connect
	conn, err := output.dial()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to syslog: %w", err)
	}
	output.conn = conn

	return output, nil
}

// dial establishes a connection to the syslog server
func (s *SyslogOutput) dial() (net.Conn, error) {
	if s.tlsConfig != nil {
		return tls.Dial("tcp", s.addr, s.tlsConfig)
	}

	protocol := s.protocol
	if protocol == "tcp+tls" {
		// Fallback: if tlsConfig was nil but protocol is tcp+tls, use plain TCP
		protocol = "tcp"
	}

	return net.DialTimeout(protocol, s.addr, 10*time.Second)
}

// Write sends a log entry to syslog
func (s *SyslogOutput) Write(entry *LogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return fmt.Errorf("syslog connection is closed")
	}

	severity := levelToSeverity(entry.Level)
	priority := facilityDaemon*8 + severity

	var message string
	switch s.format {
	case "rfc5424":
		message = s.formatRFC5424(priority, entry)
	default:
		message = s.formatRFC3164(priority, entry)
	}

	_, err := s.conn.Write([]byte(message))
	if err != nil {
		// Try to reconnect on write failure
		s.conn.Close()
		conn, reconnectErr := s.dial()
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

// formatRFC3164 formats a syslog message per RFC 3164 (BSD format)
// <priority>timestamp tag[pid]: message
func (s *SyslogOutput) formatRFC3164(priority int, entry *LogEntry) string {
	data, _ := json.Marshal(entry)
	return fmt.Sprintf("<%d>%s %s[%d]: %s\n",
		priority,
		entry.Timestamp.Format("Jan  2 15:04:05"),
		s.tag,
		os.Getpid(),
		string(data),
	)
}

// formatRFC5424 formats a syslog message per RFC 5424 (structured)
// <priority>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID [SD] MSG
func (s *SyslogOutput) formatRFC5424(priority int, entry *LogEntry) string {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "-"
	}

	pid := strconv.Itoa(os.Getpid())
	msgID := "-"
	if action, ok := entry.Fields["action"]; ok {
		msgID = fmt.Sprintf("%v", action)
	}

	// Build structured data from fields
	sd := s.buildStructuredData(entry)

	data, _ := json.Marshal(map[string]interface{}{
		"level":   entry.Level,
		"message": entry.Message,
		"fields":  entry.Fields,
	})

	return fmt.Sprintf("<%d>1 %s %s %s %s %s %s %s\n",
		priority,
		entry.Timestamp.Format(time.RFC3339),
		hostname,
		s.tag,
		pid,
		msgID,
		sd,
		string(data),
	)
}

// buildStructuredData builds RFC 5424 structured data from log entry fields
func (s *SyslogOutput) buildStructuredData(entry *LogEntry) string {
	if len(entry.Fields) == 0 {
		return "-"
	}

	var parts []string
	for k, v := range entry.Fields {
		// Escape special chars per RFC 5424 section 6.3.3
		val := fmt.Sprintf("%v", v)
		val = strings.ReplaceAll(val, `\`, `\\`)
		val = strings.ReplaceAll(val, `"`, `\"`)
		val = strings.ReplaceAll(val, `]`, `\]`)
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, val))
	}

	return fmt.Sprintf("[maxiofs@0 %s]", strings.Join(parts, " "))
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

// levelToSeverity converts a log level string to syslog severity
func levelToSeverity(level string) int {
	switch level {
	case "debug":
		return severityDebug
	case "info":
		return severityInfo
	case "warn", "warning":
		return severityWarning
	case "error":
		return severityError
	case "fatal", "panic":
		return severityCritical
	default:
		return severityInfo
	}
}

// buildTLSConfig creates a TLS configuration from syslog config
func buildTLSConfig(cfg SyslogConfig) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.TLSSkipVerify, //nolint:gosec // user-controlled setting
	}

	// Load CA certificate if provided
	if cfg.TLSCA != "" {
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM([]byte(cfg.TLSCA)) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsCfg.RootCAs = caCertPool
	}

	// Load client certificate for mTLS if provided
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		cert, err := tls.X509KeyPair([]byte(cfg.TLSCert), []byte(cfg.TLSKey))
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}
