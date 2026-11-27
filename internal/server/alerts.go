package server

import (
	"context"
	"strings"
	"sync"

	emailpkg "github.com/maxiofs/maxiofs/internal/email"
	"github.com/sirupsen/logrus"
)

// alertLevel is the severity of a usage alert.
type alertLevel int

const (
	alertLevelNone     alertLevel = 0
	alertLevelWarning  alertLevel = 1
	alertLevelCritical alertLevel = 2
)

// alertThresholds are the usage percentages of a warning and of a critical
// alert. Every usage alert takes the disk ones.
func (s *Server) alertThresholds() (warn, crit int) {
	warn, crit = 80, 90
	if v, err := s.settingsManager.GetInt("system.disk_warning_threshold"); err == nil && v > 0 {
		warn = v
	}
	if v, err := s.settingsManager.GetInt("system.disk_critical_threshold"); err == nil && v > 0 {
		crit = v
	}
	return warn, crit
}

// alertLevelOf is the alert level of a usage, in percent.
func alertLevelOf(usedPct float64, warn, crit int) alertLevel {
	switch {
	case usedPct >= float64(crit):
		return alertLevelCritical
	case usedPct >= float64(warn):
		return alertLevelWarning
	}
	return alertLevelNone
}

// alertWords name a level in a notification type, a subject and a message.
func alertWords(level alertLevel) (kind, title, shout string) {
	if level == alertLevelCritical {
		return "critical", "CRITICAL", "CRITICAL"
	}
	return "warning", "Warning", "WARNING"
}

// alertLevels holds the last level of each thing alerted on. A level is read
// and written at once: of writes that cross a threshold together, one alerts.
type alertLevels struct {
	levels sync.Map // key -> alertLevel
}

// change sets the level of key and gives the one it had.
func (l *alertLevels) change(key string, level alertLevel) alertLevel {
	prev, _ := l.levels.Swap(key, level)
	was, _ := prev.(alertLevel)
	return was
}

// emailAdmins emails an alert, when email is on, to the global administrators
// and to those of tenantID when it concerns a tenant. what names the alert in
// the log.
func (s *Server) emailAdmins(tenantID, subject, body, what string, fields logrus.Fields) {
	if enabled, _ := s.settingsManager.GetBool("email.enabled"); !enabled {
		return
	}
	sender := s.buildEmailSender()
	if sender == nil || !sender.IsConfigured() {
		return
	}
	recipients, err := s.adminEmails(context.Background(), tenantID)
	if err != nil {
		logrus.WithError(err).Errorf("%s: failed to list users for email", what)
		return
	}
	if len(recipients) == 0 {
		logrus.Debugf("%s: no admin emails configured, skipping email", what)
		return
	}
	if err := sender.Send(recipients, subject, body); err != nil {
		logrus.WithError(err).Errorf("Failed to send %s email", strings.ToLower(what))
		return
	}
	logrus.WithFields(fields).WithField("recipients", len(recipients)).Infof("%s email sent", what)
}

// buildEmailSender constructs an email.Sender from current settings.
// Returns nil if email.smtp_host is not configured.
func (s *Server) buildEmailSender() *emailpkg.Sender {
	host, _ := s.settingsManager.Get("email.smtp_host")
	if host == "" {
		return nil
	}
	port, err := s.settingsManager.GetInt("email.smtp_port")
	if err != nil || port == 0 {
		port = 587
	}
	user, _ := s.settingsManager.Get("email.smtp_user")
	password, _ := s.settingsManager.Get("email.smtp_password")
	from, _ := s.settingsManager.Get("email.from_address")
	tlsMode, _ := s.settingsManager.Get("email.tls_mode")
	skipVerify, _ := s.settingsManager.GetBool("email.skip_tls_verify")

	return emailpkg.NewSender(emailpkg.Config{
		Host:               host,
		Port:               port,
		User:               user,
		Password:           password,
		From:               from,
		TLSMode:            tlsMode,
		InsecureSkipVerify: skipVerify,
	})
}
