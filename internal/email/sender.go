package email

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
	"time"
)

// TLS connection modes for SMTP.
const (
	TLSModeNone     = "none"     // plain SMTP, no TLS at all
	TLSModeSTARTTLS = "starttls" // plain connect, then upgrade with STARTTLS
	TLSModeSSL      = "ssl"      // implicit TLS from the start (port 465)
)

// Config holds SMTP configuration
type Config struct {
	Host               string
	Port               int
	User               string
	Password           string
	From               string
	TLSMode            string // "none", "starttls", or "ssl"
	InsecureSkipVerify bool   // skip TLS certificate verification (self-signed certs)
}

// Sender sends emails via SMTP
type Sender struct {
	cfg Config
}

// NewSender creates a new SMTP sender
func NewSender(cfg Config) *Sender {
	return &Sender{cfg: cfg}
}

// IsConfigured returns true if the sender has the minimum required fields set
func (s *Sender) IsConfigured() bool {
	return s.cfg.Host != "" && s.cfg.Port > 0 && s.cfg.From != ""
}

// Send sends an email to the given recipients
func (s *Sender) Send(to []string, subject, body string) error {
	if !s.IsConfigured() {
		return fmt.Errorf("SMTP not configured")
	}
	if len(to) == 0 {
		return fmt.Errorf("no recipients specified")
	}

	msg := buildMessage(s.cfg.From, to, subject, body)
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	switch s.cfg.TLSMode {
	case TLSModeSSL:
		return s.sendImplicitTLS(addr, to, msg)
	case TLSModeSTARTTLS:
		return s.sendSTARTTLS(addr, to, msg)
	default: // TLSModeNone or empty — plain SMTP, no TLS
		return s.sendPlain(addr, to, msg)
	}
}

// sendPlain connects over plain TCP without any TLS negotiation.
func (s *Sender) sendPlain(addr string, to []string, msg []byte) error {
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("SMTP dial: %w", err)
	}
	defer client.Close()

	return s.deliver(client, to, msg)
}

// sendImplicitTLS connects with TLS from the start (port 465).
func (s *Sender) sendImplicitTLS(addr string, to []string, msg []byte) error {
	tlsCfg := &tls.Config{
		ServerName:         s.cfg.Host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: s.cfg.InsecureSkipVerify, //nolint:gosec
	}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("TLS dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("SMTP client: %w", err)
	}
	defer client.Close()

	return s.deliver(client, to, msg)
}

// sendSTARTTLS connects plaintext then upgrades with STARTTLS.
// Returns an error if the server does not advertise STARTTLS support.
func (s *Sender) sendSTARTTLS(addr string, to []string, msg []byte) error {
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("SMTP dial: %w", err)
	}
	defer client.Close()

	ok, _ := client.Extension("STARTTLS")
	if !ok {
		return fmt.Errorf("STARTTLS: server does not advertise STARTTLS support")
	}

	tlsCfg := &tls.Config{
		ServerName:         s.cfg.Host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: s.cfg.InsecureSkipVerify, //nolint:gosec
	}
	if err := client.StartTLS(tlsCfg); err != nil {
		return fmt.Errorf("STARTTLS: %w", err)
	}

	return s.deliver(client, to, msg)
}

// deliver authenticates (if credentials provided) and sends the message
func (s *Sender) deliver(client *smtp.Client, to []string, msg []byte) error {
	if s.cfg.User != "" && s.cfg.Password != "" {
		auth := smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}

	if err := client.Mail(s.cfg.From); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}

	for _, r := range to {
		if err := client.Rcpt(r); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", r, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	defer w.Close()

	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

func buildMessage(from string, to []string, subject, body string) []byte {
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("Date: " + time.Now().Format("Mon, 02 Jan 2006 15:04:05 -0700") + "\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return []byte(sb.String())
}
