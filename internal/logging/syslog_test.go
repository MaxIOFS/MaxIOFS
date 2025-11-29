package logging

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSyslogOutput(t *testing.T) {
	// Start a mock syslog server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	addr := listener.Addr().String()
	parts := strings.Split(addr, ":")
	host := parts[0]
	port := parts[1]

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	// Test TCP connection
	output, err := NewSyslogOutput("tcp", host, mustParsePort(port), "maxiofs")
	require.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "tcp", output.protocol)
	assert.Equal(t, "maxiofs", output.tag)
	output.Close()
}

func TestNewSyslogOutputInvalidHost(t *testing.T) {
	output, err := NewSyslogOutput("tcp", "invalid.host.example", 514, "maxiofs")
	assert.Error(t, err)
	assert.Nil(t, output)
}

func TestSyslogOutputWrite(t *testing.T) {
	// Mock syslog server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	received := make(chan string, 1)
	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			buf := make([]byte, 4096)
			n, _ := conn.Read(buf)
			received <- string(buf[:n])
		}
	}()

	addr := listener.Addr().String()
	parts := strings.Split(addr, ":")
	host := parts[0]
	port := parts[1]

	output, err := NewSyslogOutput("tcp", host, mustParsePort(port), "maxiofs")
	require.NoError(t, err)
	defer output.Close()

	// Write log entry
	entry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "Test syslog message",
		Fields: map[string]interface{}{
			"key": "value",
		},
	}

	err = output.Write(entry)
	require.NoError(t, err)

	// Wait for message
	select {
	case msg := <-received:
		assert.Contains(t, msg, "maxiofs")
		assert.Contains(t, msg, "Test syslog message")
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for syslog message")
	}
}

func TestSyslogOutputLevels(t *testing.T) {
	// Test that different log levels are accepted
	levels := []string{"debug", "info", "warn", "error", "fatal"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()

			received := make(chan string, 1)
			go func() {
				conn, _ := listener.Accept()
				if conn != nil {
					defer conn.Close()
					buf := make([]byte, 4096)
					n, _ := conn.Read(buf)
					received <- string(buf[:n])
				}
			}()

			addr := listener.Addr().String()
			parts := strings.Split(addr, ":")
			host := parts[0]
			port := parts[1]

			output, err := NewSyslogOutput("tcp", host, mustParsePort(port), "test")
			require.NoError(t, err)
			defer output.Close()

			entry := &LogEntry{
				Timestamp: time.Now(),
				Level:     level,
				Message:   "Test message",
			}

			err = output.Write(entry)
			assert.NoError(t, err)

			select {
			case msg := <-received:
				assert.Contains(t, msg, "Test message")
			case <-time.After(time.Second):
				t.Fatal("Timeout waiting for message")
			}
		})
	}
}

func TestSyslogOutputClose(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	addr := listener.Addr().String()
	parts := strings.Split(addr, ":")
	host := parts[0]
	port := parts[1]

	output, err := NewSyslogOutput("tcp", host, mustParsePort(port), "maxiofs")
	require.NoError(t, err)

	err = output.Close()
	assert.NoError(t, err)

	// Writing after close should return error
	entry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "Test",
	}
	err = output.Write(entry)
	assert.Error(t, err)
}

func TestSyslogOutputMultipleWrites(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	received := make([]string, 0)
	var mu sync.Mutex

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					if n > 0 {
						mu.Lock()
						// Count newlines as each syslog message ends with \n
						for _, b := range buf[:n] {
							if b == '\n' {
								received = append(received, "msg")
							}
						}
						mu.Unlock()
					}
				}
			}(conn)
		}
	}()

	addr := listener.Addr().String()
	parts := strings.Split(addr, ":")
	host := parts[0]
	port := parts[1]

	output, err := NewSyslogOutput("tcp", host, mustParsePort(port), "maxiofs")
	require.NoError(t, err)
	defer output.Close()

	// Write multiple entries
	for i := 0; i < 3; i++ {
		entry := &LogEntry{
			Timestamp: time.Now(),
			Level:     "info",
			Message:   fmt.Sprintf("Message %d", i),
		}
		err = output.Write(entry)
		assert.NoError(t, err)
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, len(received))
}

// Helper function
func mustParsePort(s string) int {
	var port int
	_, err := fmt.Sscanf(s, "%d", &port)
	if err != nil {
		panic(err)
	}
	return port
}
