package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleReceiveSettingsSync takes the settings and logging targets of another
// node, each one that changed after this node's copy.
func (s *Server) handleReceiveSettingsSync(w http.ResponseWriter, r *http.Request) {
	if _, ok := r.Context().Value("cluster_node_id").(string); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var payload cluster.SettingsSyncPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	applied := s.applySettings(ctx, payload.Settings)
	targets := s.applyLoggingTargets(ctx, payload.LoggingTargets)
	if (applied > 0 || targets > 0) && s.loggingManager != nil {
		s.loggingManager.Reconfigure()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"settings": applied, "logging_targets": targets})
}

func (s *Server) applySettings(ctx context.Context, settings []cluster.SettingRow) int {
	applied := 0
	for _, st := range settings {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO system_settings (key, value, type, category, description, editable, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
			WHERE excluded.updated_at > system_settings.updated_at
		`, st.Key, st.Value, st.Type, st.Category, st.Description, boolToInt(st.Editable), st.CreatedAt, st.UpdatedAt)
		if err != nil {
			logrus.WithError(err).WithField("key", st.Key).Warn("Failed to store a synchronized setting")
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			applied++
		}
	}
	return applied
}

func (s *Server) applyLoggingTargets(ctx context.Context, targets []cluster.LoggingTargetRow) int {
	applied := 0
	for _, t := range targets {
		if cluster.DeletionSupersedes(ctx, s.db, cluster.EntityTypeLoggingTarget, t.ID, t.UpdatedAt) {
			continue
		}
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO logging_targets (
				id, name, type, enabled, protocol, host, port, tag, format,
				tls_enabled, tls_cert, tls_key, tls_ca, tls_skip_verify,
				filter_level, auth_token, url, batch_size, flush_interval, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name = excluded.name, type = excluded.type, enabled = excluded.enabled,
				protocol = excluded.protocol, host = excluded.host, port = excluded.port,
				tag = excluded.tag, format = excluded.format, tls_enabled = excluded.tls_enabled,
				tls_cert = excluded.tls_cert, tls_key = excluded.tls_key, tls_ca = excluded.tls_ca,
				tls_skip_verify = excluded.tls_skip_verify, filter_level = excluded.filter_level,
				auth_token = excluded.auth_token, url = excluded.url, batch_size = excluded.batch_size,
				flush_interval = excluded.flush_interval, updated_at = excluded.updated_at
			WHERE excluded.updated_at > logging_targets.updated_at
		`, t.ID, t.Name, t.Type, boolToInt(t.Enabled), t.Protocol, t.Host, t.Port, t.Tag, t.Format,
			boolToInt(t.TLSEnabled), t.TLSCert, t.TLSKey, t.TLSCA, boolToInt(t.TLSSkipVerify),
			t.FilterLevel, t.AuthToken, t.URL, t.BatchSize, t.FlushInterval, t.CreatedAt, t.UpdatedAt)
		if err != nil {
			logrus.WithError(err).WithField("target", t.Name).Warn("Failed to store a synchronized logging target")
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			applied++
		}
	}
	return applied
}

// triggerSettingsSync sends the settings and logging targets to the other
// nodes now.
func (s *Server) triggerSettingsSync(ctx context.Context) {
	if s.settingsSyncMgr != nil {
		s.settingsSyncMgr.TriggerSync(ctx)
	}
}
