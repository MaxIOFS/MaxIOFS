package logging

import (
	"database/sql"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	// Create logging_targets table
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS logging_targets (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		type TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		protocol TEXT NOT NULL DEFAULT 'tcp',
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 514,
		tag TEXT NOT NULL DEFAULT 'maxiofs',
		format TEXT NOT NULL DEFAULT 'rfc3164',
		tls_enabled INTEGER NOT NULL DEFAULT 0,
		tls_cert TEXT,
		tls_key TEXT,
		tls_ca TEXT,
		tls_skip_verify INTEGER NOT NULL DEFAULT 0,
		filter_level TEXT NOT NULL DEFAULT 'info',
		auth_token TEXT,
		url TEXT,
		batch_size INTEGER,
		flush_interval INTEGER,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`)
	require.NoError(t, err)

	t.Cleanup(func() { db.Close() })
	return db
}

func newTestStore(t *testing.T) *TargetStore {
	t.Helper()
	db := setupTestDB(t)
	store, err := NewTargetStore(db, logrus.New())
	require.NoError(t, err)
	return store
}

func TestTargetStoreCRUD(t *testing.T) {
	store := newTestStore(t)

	// Create
	cfg := &TargetConfig{
		Name:        "SIEM Production",
		Type:        "syslog",
		Enabled:     true,
		Protocol:    "tcp",
		Host:        "siem.example.com",
		Port:        514,
		Tag:         "maxiofs",
		Format:      "rfc3164",
		FilterLevel: "info",
	}

	err := store.Create(cfg)
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.ID)
	assert.False(t, cfg.CreatedAt.IsZero())

	// Get
	got, err := store.Get(cfg.ID)
	require.NoError(t, err)
	assert.Equal(t, cfg.Name, got.Name)
	assert.Equal(t, cfg.Host, got.Host)
	assert.Equal(t, cfg.Port, got.Port)
	assert.Equal(t, cfg.Protocol, got.Protocol)
	assert.True(t, got.Enabled)

	// Update
	got.Host = "siem2.example.com"
	got.Port = 1514
	err = store.Update(got)
	require.NoError(t, err)

	updated, err := store.Get(cfg.ID)
	require.NoError(t, err)
	assert.Equal(t, "siem2.example.com", updated.Host)
	assert.Equal(t, 1514, updated.Port)

	// Delete
	err = store.Delete(cfg.ID)
	require.NoError(t, err)

	_, err = store.Get(cfg.ID)
	assert.Error(t, err)
}

func TestTargetStoreListEnabled(t *testing.T) {
	store := newTestStore(t)

	// Create 3 targets, one disabled
	targets := []TargetConfig{
		{Name: "Active Syslog", Type: "syslog", Enabled: true, Protocol: "tcp", Host: "a.example.com", Port: 514, Format: "rfc3164", FilterLevel: "info", Tag: "maxiofs"},
		{Name: "Backup Syslog", Type: "syslog", Enabled: true, Protocol: "udp", Host: "b.example.com", Port: 514, Format: "rfc3164", FilterLevel: "warn", Tag: "maxiofs"},
		{Name: "Disabled One", Type: "syslog", Enabled: false, Protocol: "tcp", Host: "c.example.com", Port: 514, Format: "rfc3164", FilterLevel: "info", Tag: "maxiofs"},
	}

	for i := range targets {
		err := store.Create(&targets[i])
		require.NoError(t, err)
	}

	// List all
	all, err := store.List()
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// List enabled
	enabled, err := store.ListEnabled()
	require.NoError(t, err)
	assert.Len(t, enabled, 2)
}

func TestTargetStoreValidation(t *testing.T) {
	store := newTestStore(t)

	tests := []struct {
		name    string
		cfg     TargetConfig
		wantErr string
	}{
		{
			name:    "empty name",
			cfg:     TargetConfig{Type: "syslog", Host: "a.com", Port: 514, Protocol: "tcp", Format: "rfc3164", FilterLevel: "info"},
			wantErr: "target name is required",
		},
		{
			name:    "invalid type",
			cfg:     TargetConfig{Name: "test", Type: "kafka", Host: "a.com", Port: 514, Protocol: "tcp", Format: "rfc3164", FilterLevel: "info"},
			wantErr: "invalid target type",
		},
		{
			name:    "missing host",
			cfg:     TargetConfig{Name: "test", Type: "syslog", Host: "", Port: 514, Protocol: "tcp", Format: "rfc3164", FilterLevel: "info"},
			wantErr: "host is required",
		},
		{
			name:    "invalid port",
			cfg:     TargetConfig{Name: "test", Type: "syslog", Host: "a.com", Port: 0, Protocol: "tcp", Format: "rfc3164", FilterLevel: "info"},
			wantErr: "port must be between",
		},
		{
			name:    "invalid protocol",
			cfg:     TargetConfig{Name: "test", Type: "syslog", Host: "a.com", Port: 514, Protocol: "ws", Format: "rfc3164", FilterLevel: "info"},
			wantErr: "invalid protocol",
		},
		{
			name:    "invalid format",
			cfg:     TargetConfig{Name: "test", Type: "syslog", Host: "a.com", Port: 514, Protocol: "tcp", Format: "cef", FilterLevel: "info"},
			wantErr: "invalid format",
		},
		{
			name:    "invalid filter level",
			cfg:     TargetConfig{Name: "test", Type: "syslog", Host: "a.com", Port: 514, Protocol: "tcp", Format: "rfc3164", FilterLevel: "trace"},
			wantErr: "invalid filter level",
		},
		{
			name:    "http missing URL",
			cfg:     TargetConfig{Name: "test", Type: "http", FilterLevel: "info"},
			wantErr: "URL is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Create(&tt.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestTargetStoreDeleteNotFound(t *testing.T) {
	store := newTestStore(t)
	err := store.Delete("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTargetStoreUpdateNotFound(t *testing.T) {
	store := newTestStore(t)
	err := store.Update(&TargetConfig{
		ID: "nonexistent", Name: "test", Type: "syslog",
		Host: "a.com", Port: 514, Protocol: "tcp",
		Format: "rfc3164", FilterLevel: "info",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTargetStoreTLSFields(t *testing.T) {
	store := newTestStore(t)

	cfg := &TargetConfig{
		Name:          "TLS Syslog",
		Type:          "syslog",
		Enabled:       true,
		Protocol:      "tcp+tls",
		Host:          "siem.example.com",
		Port:          6514,
		Tag:           "maxiofs",
		Format:        "rfc5424",
		TLSEnabled:    true,
		TLSCert:       "-----BEGIN CERTIFICATE-----\nMIIB...",
		TLSKey:        "-----BEGIN PRIVATE KEY-----\nMIIE...",
		TLSCA:         "-----BEGIN CERTIFICATE-----\nMIIC...",
		TLSSkipVerify: false,
		FilterLevel:   "warn",
	}

	err := store.Create(cfg)
	require.NoError(t, err)

	got, err := store.Get(cfg.ID)
	require.NoError(t, err)
	assert.True(t, got.TLSEnabled)
	assert.Equal(t, cfg.TLSCert, got.TLSCert)
	assert.Equal(t, cfg.TLSKey, got.TLSKey)
	assert.Equal(t, cfg.TLSCA, got.TLSCA)
	assert.False(t, got.TLSSkipVerify)
	assert.Equal(t, "rfc5424", got.Format)
	assert.Equal(t, "tcp+tls", got.Protocol)
}

func TestTargetStoreHTTPTarget(t *testing.T) {
	store := newTestStore(t)

	cfg := &TargetConfig{
		Name:          "Elasticsearch",
		Type:          "http",
		Enabled:       true,
		URL:           "https://elastic.example.com:9200/_bulk",
		AuthToken:     "Bearer abc123",
		BatchSize:     200,
		FlushInterval: 5,
		FilterLevel:   "info",
		// Syslog-specific fields use defaults
		Protocol: "https",
		Host:     "elastic.example.com",
		Port:     9200,
		Tag:      "maxiofs",
		Format:   "rfc3164",
	}

	err := store.Create(cfg)
	require.NoError(t, err)

	got, err := store.Get(cfg.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://elastic.example.com:9200/_bulk", got.URL)
	assert.Equal(t, "Bearer abc123", got.AuthToken)
	assert.Equal(t, 200, got.BatchSize)
	assert.Equal(t, 5, got.FlushInterval)
}

func TestTargetStoreMigrateFromSettings(t *testing.T) {
	store := newTestStore(t)

	sm := &mockSettingsManager{
		settings: map[string]string{
			"logging.syslog_enabled":      "true",
			"logging.syslog_host":         "syslog.old.com",
			"logging.syslog_port":         "514",
			"logging.syslog_protocol":     "tcp",
			"logging.syslog_tag":          "maxiofs",
			"logging.http_enabled":        "true",
			"logging.http_url":            "https://elastic.old.com/_bulk",
			"logging.http_auth_token":     "tok123",
			"logging.http_batch_size":     "100",
			"logging.http_flush_interval": "10",
		},
	}

	err := store.MigrateFromSettings(sm)
	require.NoError(t, err)

	all, err := store.List()
	require.NoError(t, err)
	assert.Len(t, all, 2)

	// Verify syslog was migrated
	var foundSyslog, foundHTTP bool
	for _, target := range all {
		if target.Type == "syslog" {
			foundSyslog = true
			assert.Equal(t, "syslog.old.com", target.Host)
			assert.Equal(t, 514, target.Port)
		}
		if target.Type == "http" {
			foundHTTP = true
			assert.Equal(t, "https://elastic.old.com/_bulk", target.URL)
		}
	}
	assert.True(t, foundSyslog, "syslog target should have been migrated")
	assert.True(t, foundHTTP, "http target should have been migrated")
}

func TestTargetStoreUniqueName(t *testing.T) {
	store := newTestStore(t)

	cfg1 := &TargetConfig{
		Name: "Same Name", Type: "syslog", Enabled: true,
		Protocol: "tcp", Host: "a.com", Port: 514,
		Tag: "maxiofs", Format: "rfc3164", FilterLevel: "info",
	}
	cfg2 := &TargetConfig{
		Name: "Same Name", Type: "syslog", Enabled: true,
		Protocol: "tcp", Host: "b.com", Port: 514,
		Tag: "maxiofs", Format: "rfc3164", FilterLevel: "info",
	}

	err := store.Create(cfg1)
	require.NoError(t, err)

	err = store.Create(cfg2)
	assert.Error(t, err) // UNIQUE constraint on name
}
