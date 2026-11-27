// Package emailtest is an SMTP server for tests: it takes what is sent to it
// and keeps it, over plain SMTP, STARTTLS or TLS from the start.
package emailtest

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Modes of a server, as the sender names them.
const (
	ModeNone     = "none"
	ModeSTARTTLS = "starttls"
	ModeSSL      = "ssl"
)

// Options say how a server behaves.
type Options struct {
	Mode       string // ModeNone (default), ModeSTARTTLS or ModeSSL
	User       string // with Password, the only credentials AUTH PLAIN takes
	Password   string
	RejectData bool // refuse every message once its data is sent
	Silent     bool // take connections and never answer
}

// Message is a message the server took.
type Message struct {
	From string
	To   []string
	Data string
}

// Server is a running test SMTP server.
type Server struct {
	Host string
	Port int

	opts     Options
	tls      *tls.Config
	listener net.Listener
	mu       sync.Mutex
	messages []Message
}

// Start runs a server on 127.0.0.1 until the test ends.
func Start(t testing.TB, opts Options) *Server {
	t.Helper()
	s := &Server{opts: opts, tls: selfSigned(t)}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("emailtest: listen: %v", err)
	}
	if opts.Mode == ModeSSL {
		listener = tls.NewListener(listener, s.tls)
	}
	s.listener = listener
	addr := listener.Addr().(*net.TCPAddr)
	s.Host, s.Port = "127.0.0.1", addr.Port
	go s.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return s
}

// Messages are the messages taken so far.
func (s *Server) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.messages...)
}

func (s *Server) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.session(conn)
	}
}

func (s *Server) session(conn net.Conn) {
	defer conn.Close()
	if s.opts.Silent {
		_ = conn.SetReadDeadline(time.Now().Add(time.Minute))
		_, _ = bufio.NewReader(conn).ReadString(0)
		return
	}
	r, w := bufio.NewReader(conn), conn
	reply := func(lines ...string) {
		for _, line := range lines {
			_, _ = w.Write([]byte(line + "\r\n"))
		}
	}
	reply("220 emailtest ESMTP")
	secure := s.opts.Mode == ModeSSL
	var msg Message
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch verb {
		case "EHLO", "HELO":
			lines := []string{"250-emailtest", "250-AUTH PLAIN"}
			if s.opts.Mode == ModeSTARTTLS && !secure {
				lines = append(lines, "250-STARTTLS")
			}
			reply(append(lines, "250 OK")...)
		case "STARTTLS":
			reply("220 Ready")
			tlsConn := tls.Server(conn, s.tls)
			if tlsConn.Handshake() != nil {
				return
			}
			conn, r, w, secure = tlsConn, bufio.NewReader(tlsConn), tlsConn, true
		case "AUTH":
			reply(s.auth(line))
		case "MAIL":
			msg = Message{From: address(line)}
			reply("250 OK")
		case "RCPT":
			msg.To = append(msg.To, address(line))
			reply("250 OK")
		case "DATA":
			reply("354 Go ahead")
			data, ok := readData(r)
			if !ok {
				return
			}
			if s.opts.RejectData {
				reply("554 Message rejected")
				continue
			}
			msg.Data = data
			s.mu.Lock()
			s.messages = append(s.messages, msg)
			s.mu.Unlock()
			reply("250 OK")
		case "QUIT":
			reply("221 Bye")
			return
		default:
			reply("250 OK")
		}
	}
}

// auth answers an AUTH PLAIN with its initial response.
func (s *Server) auth(line string) string {
	fields := strings.Fields(line)
	if len(fields) != 3 || !strings.EqualFold(fields[1], "PLAIN") {
		return "504 Unrecognized authentication type"
	}
	decoded, err := base64.StdEncoding.DecodeString(fields[2])
	parts := strings.Split(string(decoded), "\x00")
	if err != nil || len(parts) != 3 || parts[1] != s.opts.User || parts[2] != s.opts.Password {
		return "535 Authentication failed"
	}
	return "235 Authenticated"
}

func address(line string) string {
	_, after, _ := strings.Cut(line, ":")
	return strings.Trim(strings.TrimSpace(after), "<>")
}

func readData(r *bufio.Reader) (string, bool) {
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return "", false
		}
		if line == ".\r\n" {
			return b.String(), true
		}
		b.WriteString(strings.TrimPrefix(line, "."))
	}
}

func selfSigned(t testing.TB) *tls.Config {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("emailtest: key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "emailtest"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("emailtest: certificate: %v", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
}

// PortString is the port as text, as settings take it.
func (s *Server) PortString() string { return strconv.Itoa(s.Port) }
