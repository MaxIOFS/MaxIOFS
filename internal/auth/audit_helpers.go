package auth

import (
	"context"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/sirupsen/logrus"
)

// logAuditEvent is a helper function to log audit events
// It safely checks if audit manager is available before logging
func (am *authManager) logAuditEvent(ctx context.Context, event *audit.AuditEvent) {
	if am.auditManager == nil {
		return
	}

	if err := am.auditManager.LogEvent(ctx, event); err != nil {
		logrus.WithError(err).Warn("Failed to write audit event")
	}
}
