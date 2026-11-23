package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node takes the settings and logging targets another node sends that
// changed after its own copy, and those it does not hold; a deleted target
// does not come back.
func TestSettingsReachEveryNode(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	_, err := s.db.Exec(`UPDATE system_settings SET value = 'local', updated_at = 100 WHERE key = 'metrics.token'`)
	require.NoError(t, err)

	send := func(payload cluster.SettingsSyncPayload) {
		t.Helper()
		w := httptest.NewRecorder()
		s.handleReceiveSettingsSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/settings-sync", payload))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	setting := func(key, value string, at int64) cluster.SettingRow {
		return cluster.SettingRow{Key: key, Value: value, Type: "string", Category: "metrics", Editable: true, UpdatedAt: at}
	}
	value := func(key string) string {
		t.Helper()
		var v string
		require.NoError(t, s.db.QueryRow(`SELECT value FROM system_settings WHERE key = ?`, key).Scan(&v))
		return v
	}

	send(cluster.SettingsSyncPayload{Settings: []cluster.SettingRow{setting("metrics.token", "older", 99), setting("metrics.token", "same", 100)}})
	assert.Equal(t, "local", value("metrics.token"), "a copy not newer")
	send(cluster.SettingsSyncPayload{Settings: []cluster.SettingRow{setting("metrics.token", "newer", 101)}})
	assert.Equal(t, "newer", value("metrics.token"))
	send(cluster.SettingsSyncPayload{Settings: []cluster.SettingRow{setting("metrics.sent", "x", 1)}})
	assert.Equal(t, "x", value("metrics.sent"), "a setting this node did not hold")

	host := func(id string) string {
		t.Helper()
		var h string
		if err := s.db.QueryRow(`SELECT host FROM logging_targets WHERE id = ?`, id).Scan(&h); err != nil {
			return ""
		}
		return h
	}
	target := cluster.LoggingTargetRow{ID: "t1", Name: "siem", Type: "syslog", Protocol: "tcp", Host: "10.0.0.9", Port: 514,
		Tag: "maxiofs", Format: "rfc3164", FilterLevel: "info", CreatedAt: 1, UpdatedAt: 10}
	send(cluster.SettingsSyncPayload{LoggingTargets: []cluster.LoggingTargetRow{target}})
	assert.Equal(t, "10.0.0.9", host("t1"))
	target.Host, target.UpdatedAt = "10.0.0.10", 11
	send(cluster.SettingsSyncPayload{LoggingTargets: []cluster.LoggingTargetRow{target}})
	assert.Equal(t, "10.0.0.10", host("t1"))

	require.NoError(t, cluster.RecordDeletion(ctx, s.db, cluster.EntityTypeLoggingTarget, "t2", "peer", 50))
	deleted := target
	deleted.ID, deleted.Name, deleted.UpdatedAt = "t2", "gone", 40
	send(cluster.SettingsSyncPayload{LoggingTargets: []cluster.LoggingTargetRow{deleted}})
	assert.Empty(t, host("t2"), "a target older than its deletion")
}

// Two targets of the same name under different ids, as the migration of the
// old settings leaves one on each node, end as one on every node: the later,
// or the one of the smaller id when both are as late.
func TestLoggingTargetsOfTheSameNameConverge(t *testing.T) {
	s := newClusterTestNode(t)
	send := func(targets ...cluster.LoggingTargetRow) {
		t.Helper()
		w := httptest.NewRecorder()
		s.handleReceiveSettingsSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/settings-sync", cluster.SettingsSyncPayload{LoggingTargets: targets}))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	ids := func() []string {
		t.Helper()
		rows, err := s.db.Query(`SELECT id FROM logging_targets WHERE name = 'Syslog (migrated)' ORDER BY id`)
		require.NoError(t, err)
		defer rows.Close() //nolint:errcheck
		var got []string
		for rows.Next() {
			var id string
			require.NoError(t, rows.Scan(&id))
			got = append(got, id)
		}
		return got
	}
	migrated := func(id string, at int64) cluster.LoggingTargetRow {
		return cluster.LoggingTargetRow{ID: id, Name: "Syslog (migrated)", Type: "syslog", Protocol: "tcp", Host: "10.0.0.9", Port: 514,
			Tag: "maxiofs", Format: "rfc3164", FilterLevel: "info", CreatedAt: 1, UpdatedAt: at}
	}
	_, err := s.db.Exec(`INSERT INTO logging_targets (id, name, type, enabled, protocol, host, port, tag, format, filter_level, created_at, updated_at)
		VALUES ('local', 'Syslog (migrated)', 'syslog', 1, 'tcp', '10.0.0.9', 514, 'maxiofs', 'rfc3164', 'info', 1, 100)`)
	require.NoError(t, err)

	send(migrated("older", 99))
	assert.Equal(t, []string{"local"}, ids(), "an older one of the same name")
	send(migrated("later", 101))
	assert.Equal(t, []string{"later"}, ids(), "a later one replaces it")
	send(migrated("a-tie", 101))
	assert.Equal(t, []string{"a-tie"}, ids(), "as late, the smaller id")
	send(migrated("z-tie", 101))
	assert.Equal(t, []string{"a-tie"}, ids())
}
