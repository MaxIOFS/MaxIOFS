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

// A server that cannot be reached keeps the output, and each write says why
// until it is reached.
func TestNewSyslogOutputInvalidHost(t *testing.T) {
	output, err := NewSyslogOutput("tcp", "invalid.host.example", 514, "maxiofs")
	require.NoError(t, err)
	defer output.Close()
	err = output.Write(&LogEntry{Timestamp: time.Now(), Level: "info", Message: "x"})
	assert.ErrorContains(t, err, "invalid.host.example")
}

// syslogServer accepts connections on addr and passes what they carry to
// received; stop closes it and every connection, as a server that restarts.
func syslogServer(t *testing.T, addr string, received chan<- string) (stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			go func() {
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						received <- string(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return func() {
		l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	}
}

func freeAddr(t *testing.T) (string, int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().String(), l.Addr().(*net.TCPAddr).Port
}

// writeUntilReceived writes until the server gets an entry, for 5 seconds.
func writeUntilReceived(t *testing.T, output *SyslogOutput, received chan string, what string) {
	t.Helper()
	entry := &LogEntry{Timestamp: time.Now(), Level: "info", Message: "reached"}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		_ = output.Write(entry)
		select {
		case msg := <-received:
			if strings.Contains(msg, "reached") {
				return
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("no entry reached the server %s", what)
}

// A server down when the target is made is connected once it listens.
func TestSyslogConnectsToAServerThatWasDown(t *testing.T) {
	addr, port := freeAddr(t)

	output, err := NewSyslogOutput("tcp", "127.0.0.1", port, "maxiofs")
	require.NoError(t, err, "a server down when the target is made keeps the target")
	defer output.Close()
	output.redialEvery = 50 * time.Millisecond
	assert.Error(t, output.Write(&LogEntry{Timestamp: time.Now(), Level: "info", Message: "x"}))

	received := make(chan string, 1000)
	stop := syslogServer(t, addr, received)
	defer stop()
	writeUntilReceived(t, output, received, "once it listened")
}

// A server that restarts is connected again: a failed reconnection does not end
// the output.
func TestSyslogConnectsAgainAfterTheServerRestarts(t *testing.T) {
	addr, port := freeAddr(t)
	received := make(chan string, 1000)

	stop := syslogServer(t, addr, received)
	output, err := NewSyslogOutput("tcp", "127.0.0.1", port, "maxiofs")
	require.NoError(t, err)
	defer output.Close()
	output.redialEvery = 50 * time.Millisecond
	writeUntilReceived(t, output, received, "before the restart")

	stop()
	entry := &LogEntry{Timestamp: time.Now(), Level: "info", Message: "lost"}
	for i := 0; i < 20; i++ {
		_ = output.Write(entry)
		time.Sleep(5 * time.Millisecond)
	}

	stop = syslogServer(t, addr, received)
	defer stop()
	writeUntilReceived(t, output, received, "after the restart")
}

// A server that accepts and stops reading holds a write writeTimeout at most,
// so the entries behind it are not held for ever.
func TestSyslogWriteToAServerThatStopsReadingReturns(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			c.Close()
		}
	}()

	output, err := NewSyslogOutput("tcp", "127.0.0.1", l.Addr().(*net.TCPAddr).Port, "maxiofs")
	require.NoError(t, err)
	defer output.Close()
	output.writeTimeout = 100 * time.Millisecond

	entry := &LogEntry{Timestamp: time.Now(), Level: "info", Message: strings.Repeat("x", 1<<20)}
	for i := 0; i < 100; i++ {
		done := make(chan struct{})
		go func() {
			_ = output.Write(entry)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("write %d still blocked on a server that does not read", i)
		}
	}
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
