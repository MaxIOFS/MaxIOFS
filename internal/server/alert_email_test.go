package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/email/emailtest"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alertMailFixture is a node that emails its alerts to a test SMTP server, with
// global administrators, the administrators of two tenants, and users who get
// no alert: one inactive, one not an administrator.
func alertMailFixture(t *testing.T, smtp emailtest.Options) (*Server, *emailtest.Server) {
	t.Helper()
	s := newClusterTestNode(t)
	ctx := context.Background()
	mail := emailtest.Start(t, smtp)
	for key, value := range map[string]string{
		"email.enabled": "true", "email.smtp_host": mail.Host, "email.smtp_port": mail.PortString(),
		"email.from_address": "maxiofs@example.com", "email.tls_mode": "none",
	} {
		require.NoError(t, s.settingsManager.Set(key, value), key)
	}
	for _, id := range []string{"acme", "beta"} {
		require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: id, Name: id, DisplayName: strings.ToUpper(id), Status: "active"}))
	}
	now := time.Now().Unix()
	for _, u := range []auth.User{
		{ID: "root", TenantID: "", Roles: []string{auth.RoleAdmin}, Status: auth.UserStatusActive},
		{ID: "acme-admin", TenantID: "acme", Roles: []string{auth.RoleTenantAdmin}, Status: auth.UserStatusActive},
		{ID: "beta-admin", TenantID: "beta", Roles: []string{auth.RoleTenantAdmin}, Status: auth.UserStatusActive},
		{ID: "acme-user", TenantID: "acme", Roles: []string{auth.RoleUser}, Status: auth.UserStatusActive},
		{ID: "old-root", TenantID: "", Roles: []string{auth.RoleAdmin}, Status: auth.UserStatusInactive},
	} {
		u.Username, u.Email, u.CreatedAt, u.UpdatedAt = u.ID, u.ID+"@example.com", now, now
		require.NoError(t, s.authManager.CreateUser(ctx, &u), u.ID)
	}
	return s, mail
}

// mailsTo waits for the server to hold n messages and gives each one's
// recipients and subject.
func mailsTo(t *testing.T, mail *emailtest.Server, n int) []emailtest.Message {
	t.Helper()
	require.Eventually(t, func() bool { return len(mail.Messages()) >= n }, 5*time.Second, 10*time.Millisecond, "%d messages", n)
	time.Sleep(50 * time.Millisecond)
	messages := mail.Messages()
	require.Len(t, messages, n)
	for _, m := range messages {
		sort.Strings(m.To)
	}
	return messages
}

// A storage alert is emailed when its level rises, to the global
// administrators and to the administrators of the tenant it concerns: never
// again at the same level, never when it falls, again once it rises anew.
func TestAStorageAlertIsEmailedToWhoAdministersIt(t *testing.T) {
	s, mail := alertMailFixture(t, emailtest.Options{})
	tenantAdmins := []string{"acme-admin@example.com", "admin@maxiofs.local", "root@example.com"}

	s.checkQuotaAlert("acme", 85, 100)
	m := mailsTo(t, mail, 1)
	assert.Equal(t, tenantAdmins, m[0].To)
	assert.Contains(t, m[0].Data, "Subject: [MaxIOFS] Warning: Storage Quota Alert")
	assert.Contains(t, m[0].Data, "Tenant: ACME")
	s.checkQuotaAlert("acme", 86, 100)
	s.checkQuotaAlert("acme", 95, 100)
	m = mailsTo(t, mail, 2)
	assert.Contains(t, m[1].Data, "CRITICAL")
	s.checkQuotaAlert("acme", 50, 100)
	s.checkQuotaAlert("acme", 85, 100)
	mailsTo(t, mail, 3)
	s.checkQuotaAlert("acme", 0, 0)

	s.checkBucketQuotaAlert("beta", "backups", 950, 1000)
	m = mailsTo(t, mail, 4)
	assert.Equal(t, []string{"admin@maxiofs.local", "beta-admin@example.com", "root@example.com"}, m[3].To)
	assert.Contains(t, m[3].Data, "Bucket: backups")
	assert.Contains(t, m[3].Data, "CRITICAL: Bucket Quota Alert")

	s.sendCorruptionAlertEmail("backups", &object.IntegrityResult{Key: "k", Status: "corrupted", StoredETag: "aaa", ComputedETag: "bbb", Reason: "bit rot"})
	m = mailsTo(t, mail, 5)
	assert.Equal(t, []string{"admin@maxiofs.local", "root@example.com"}, m[4].To, "what concerns the deployment, to its administrators")
	for _, detail := range []string{"Key:         k", "Stored ETag: aaa", "Actual ETag: bbb", "Reason:      bit rot"} {
		assert.Contains(t, m[4].Data, detail)
	}

	require.NoError(t, s.settingsManager.Set("email.enabled", "false"))
	s.checkBucketQuotaAlert("acme", "other", 99, 100)
	time.Sleep(200 * time.Millisecond)
	assert.Len(t, mail.Messages(), 5, "email disabled")
}

// A test email goes to the global administrator who asks for it, and says so
// only when the server took it.
func TestATestEmailSaysWhetherItWasTaken(t *testing.T) {
	s, mail := alertMailFixture(t, emailtest.Options{})
	ask := func(userID string) *httptest.ResponseRecorder {
		user, err := s.authManager.GetUser(context.Background(), userID)
		require.NoError(t, err)
		req := consoleRequestWith(user, "")
		if user.TenantID == "" && len(user.Roles) > 0 && user.Roles[0] == auth.RoleAdmin {
			req = consoleRequestWith(user, "", "*")
		}
		rec := httptest.NewRecorder()
		s.handleTestEmail(rec, req)
		return rec
	}
	rec := ask("root")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	m := mailsTo(t, mail, 1)
	assert.Equal(t, []string{"root@example.com"}, m[0].To)
	assert.Equal(t, http.StatusForbidden, ask("acme-admin").Code)

	rejecting := emailtest.Start(t, emailtest.Options{RejectData: true})
	require.NoError(t, s.settingsManager.Set("email.smtp_port", rejecting.PortString()))
	assert.Equal(t, http.StatusInternalServerError, ask("root").Code, "a refused message is no test email sent")

	require.NoError(t, s.settingsManager.Set("email.smtp_host", ""))
	assert.Equal(t, http.StatusBadRequest, ask("root").Code, "no SMTP host")
	require.NoError(t, s.settingsManager.Set("email.enabled", "false"))
	assert.Equal(t, http.StatusBadRequest, ask("root").Code, "email disabled")
}

// The disk and a cluster node alert the global administrators when their
// usage rises; one without capacity data does not alert.
func TestADiskOrNodeAlertIsEmailedToTheGlobalAdministrators(t *testing.T) {
	s, mail := alertMailFixture(t, emailtest.Options{})
	globalAdmins := []string{"admin@maxiofs.local", "root@example.com"}
	disk := &alertLevels{}
	s.diskAlert(disk, &metrics.DiskStats{UsedPercent: 92, UsedBytes: 92e9, TotalBytes: 100e9, FreeBytes: 8e9})
	m := mailsTo(t, mail, 1)
	assert.Equal(t, globalAdmins, m[0].To)
	assert.Contains(t, m[0].Data, "Subject: [MaxIOFS] CRITICAL: Disk Space Alert")
	assert.Contains(t, m[0].Data, "Free:  8.0 GB")
	s.diskAlert(disk, &metrics.DiskStats{UsedPercent: 85})
	s.diskAlert(disk, &metrics.DiskStats{UsedPercent: 10})
	s.diskAlert(disk, &metrics.DiskStats{UsedPercent: 81})
	m = mailsTo(t, mail, 2)
	assert.Contains(t, m[1].Data, "Subject: [MaxIOFS] Warning: Disk Space Alert", "falling is no alert, rising again is")

	nodes := &alertLevels{}
	s.clusterNodeAlert(context.Background(), nodes, &cluster.Node{ID: "n1", Name: "east"}, 80, 90)
	s.clusterNodeAlert(context.Background(), nodes, &cluster.Node{ID: "n1", Name: "east", CapacityUsed: 85, CapacityTotal: 100}, 80, 90)
	m = mailsTo(t, mail, 3)
	assert.Equal(t, globalAdmins, m[2].To)
	assert.Contains(t, m[2].Data, `Subject: [MaxIOFS] Warning: Cluster Node "east" Disk Space Alert`)
	s.clusterNodeAlert(context.Background(), nodes, &cluster.Node{ID: "n2", Name: "west", CapacityUsed: 95, CapacityTotal: 100}, 80, 90)
	m = mailsTo(t, mail, 4)
	assert.Contains(t, m[3].Data, "Node: west")
}

// A usage alerts from the thresholds set, 80% and 90% when none is, each
// reached at its value.
func TestAlertThresholdsAreTheSettingsOrTheDefaults(t *testing.T) {
	s := newClusterTestNode(t)
	warn, crit := s.alertThresholds()
	assert.Equal(t, [2]int{80, 90}, [2]int{warn, crit})
	require.NoError(t, s.settingsManager.Set("system.disk_warning_threshold", "50"))
	require.NoError(t, s.settingsManager.Set("system.disk_critical_threshold", "70"))
	warn, crit = s.alertThresholds()
	assert.Equal(t, [2]int{50, 70}, [2]int{warn, crit})
	for used, level := range map[float64]alertLevel{79.9: alertLevelNone, 80: alertLevelWarning, 89.9: alertLevelWarning, 90: alertLevelCritical} {
		assert.Equal(t, level, alertLevelOf(used, 80, 90), used)
	}
}
