package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A rejected exchange of a directory or provider identity for temporary
// credentials, by the JSON endpoints or AWS's, is in the audit log with who
// asked and why it was rejected.
func TestARejectedFederationIsAudited(t *testing.T) {
	s := newClusterTestNodeWith(t, func(cfg *config.Config) { cfg.Audit.Enable = true })
	ctx := context.Background()

	rec := httptest.NewRecorder()
	s.denySTSFederation(rec, httptest.NewRequest(http.MethodPost, "/api/v1/sts/ldap-identity", nil), "ldap", "corp", "mallory", "wrong password")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.NotContains(t, rec.Body.String(), "wrong password", "the caller is told nothing of why")
	s.auditSTSXMLDenial(httptest.NewRequest(http.MethodPost, "/", nil), "AssumeRoleWithWebIdentity", "eve", "expired token")

	s.auditManager.Flush()
	logs, total, err := s.auditManager.GetLogs(ctx, &audit.AuditLogFilters{EventType: audit.EventTypeSTSFederationDenied})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	reasons := map[string]any{}
	for _, log := range logs {
		assert.Equal(t, audit.StatusFailed, log.Status)
		reasons[log.Username] = log.Details["reason"]
	}
	assert.Equal(t, map[string]any{"mallory": "wrong password", "eve": "expired token"}, reasons)
}
