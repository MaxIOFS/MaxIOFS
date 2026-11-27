package email

import (
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/email/emailtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func senderFor(server *emailtest.Server, mode string) *Sender {
	return NewSender(Config{Host: server.Host, Port: server.Port, From: "maxiofs@example.com", TLSMode: mode,
		InsecureSkipVerify: true, Timeout: 2 * time.Second})
}

// A message is delivered over plain SMTP, STARTTLS and TLS from the start, with
// its headers and body; credentials are given when there are some.
func TestAMessageIsDeliveredInEveryMode(t *testing.T) {
	for _, mode := range []string{emailtest.ModeNone, emailtest.ModeSTARTTLS, emailtest.ModeSSL} {
		server := emailtest.Start(t, emailtest.Options{Mode: mode, User: "u", Password: "p"})
		sender := senderFor(server, mode)
		sender.cfg.User, sender.cfg.Password = "u", "p"
		require.NoError(t, sender.Send([]string{"a@example.com", "b@example.com"}, "Hello\r\nBcc: x@evil", "the body"), mode)
		messages := server.Messages()
		require.Len(t, messages, 1, mode)
		assert.Equal(t, "maxiofs@example.com", messages[0].From)
		assert.Equal(t, []string{"a@example.com", "b@example.com"}, messages[0].To)
		assert.Contains(t, messages[0].Data, "Subject: HelloBcc: x@evil\r\n", "no header is injected")
		assert.True(t, strings.HasSuffix(messages[0].Data, "\r\n\r\nthe body\r\n"), mode)
	}
}

// What the server refuses is a failure to send, never a message sent.
func TestARefusalIsAFailureToSend(t *testing.T) {
	rejecting := emailtest.Start(t, emailtest.Options{RejectData: true})
	assert.ErrorContains(t, senderFor(rejecting, emailtest.ModeNone).Send([]string{"a@example.com"}, "s", "b"), "554")

	authed := emailtest.Start(t, emailtest.Options{User: "u", Password: "p"})
	wrong := senderFor(authed, emailtest.ModeNone)
	wrong.cfg.User, wrong.cfg.Password = "u", "wrong"
	assert.ErrorContains(t, wrong.Send([]string{"a@example.com"}, "s", "b"), "auth")
	assert.Empty(t, authed.Messages())

	plain := emailtest.Start(t, emailtest.Options{})
	assert.ErrorContains(t, senderFor(plain, emailtest.ModeSTARTTLS).Send([]string{"a@example.com"}, "s", "b"), "STARTTLS",
		"STARTTLS asked of a server without it")
	assert.Error(t, NewSender(Config{}).Send([]string{"a@example.com"}, "s", "b"), "not configured")
	assert.Error(t, senderFor(plain, emailtest.ModeNone).Send(nil, "s", "b"), "no recipients")
}

// A server that never answers fails the send within its timeout.
func TestASilentServerFailsWithinTheTimeout(t *testing.T) {
	silent := emailtest.Start(t, emailtest.Options{Silent: true})
	done := make(chan error, 1)
	go func() { done <- senderFor(silent, emailtest.ModeNone).Send([]string{"a@example.com"}, "s", "b") }()
	select {
	case err := <-done:
		assert.Error(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the send waited on a server that never answers")
	}
}

// A sender needs a host, a port and a sender address; with no timeout it waits
// 30 seconds, and with a user but no password it does not authenticate.
func TestASenderTakesWhatItIsGiven(t *testing.T) {
	assert.False(t, NewSender(Config{Port: 25, From: "f"}).IsConfigured())
	assert.False(t, NewSender(Config{Host: "h", From: "f"}).IsConfigured())
	assert.False(t, NewSender(Config{Host: "h", Port: 25}).IsConfigured())
	assert.True(t, NewSender(Config{Host: "h", Port: 1, From: "f"}).IsConfigured())

	server := emailtest.Start(t, emailtest.Options{})
	sender := NewSender(Config{Host: server.Host, Port: server.Port, From: "f@example.com", User: "u"})
	require.NoError(t, sender.Send([]string{"a@example.com"}, "s", "b"), "no timeout set, no password given")
	assert.Len(t, server.Messages(), 1)
}
