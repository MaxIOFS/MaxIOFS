package migrations

import "database/sql"

// migration13_v140_Capabilities creates the capability system tables.
// role_capabilities defines which capabilities each role grants by default (editable by global admin).
// user_capability_overrides allows per-user grants or revocations on top of role defaults.
func migration13_v140_Capabilities() Migration {
	return Migration{
		Version:     13,
		Description: "v1.4.0 - Add role_capabilities and user_capability_overrides tables",
		Up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS role_capabilities (
					role       TEXT NOT NULL,
					capability TEXT NOT NULL,
					PRIMARY KEY (role, capability)
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_role_capabilities_role ON role_capabilities(role)`); err != nil {
				return err
			}

			if _, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS user_capability_overrides (
					id         TEXT PRIMARY KEY,
					user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
					capability TEXT NOT NULL,
					granted    INTEGER NOT NULL DEFAULT 1,
					granted_by TEXT NOT NULL,
					created_at INTEGER NOT NULL,
					UNIQUE (user_id, capability)
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_user_cap_overrides_user ON user_capability_overrides(user_id)`); err != nil {
				return err
			}

			// Seed default role capabilities.
			// admin:    all capabilities.
			// user:     everything except bucket:manage_policy.
			// read:     download + console + own API keys.
			// readonly: download + console only.
			// guest:    download only, no console.
			defaults := []struct{ role, cap string }{
				{"admin", "bucket:create"},
				{"admin", "bucket:delete"},
				{"admin", "bucket:configure"},
				{"admin", "bucket:manage_policy"},
				{"admin", "object:upload"},
				{"admin", "object:download"},
				{"admin", "object:delete"},
				{"admin", "object:manage_tags"},
				{"admin", "object:manage_versions"},
				{"admin", "console:access"},
				{"admin", "keys:manage_own"},

				{"user", "bucket:create"},
				{"user", "bucket:delete"},
				{"user", "bucket:configure"},
				{"user", "object:upload"},
				{"user", "object:download"},
				{"user", "object:delete"},
				{"user", "object:manage_tags"},
				{"user", "object:manage_versions"},
				{"user", "console:access"},
				{"user", "keys:manage_own"},

				{"read", "object:download"},
				{"read", "console:access"},
				{"read", "keys:manage_own"},

				{"readonly", "object:download"},
				{"readonly", "console:access"},

				{"guest", "object:download"},
			}
			for _, d := range defaults {
				if _, err := tx.Exec(
					`INSERT OR IGNORE INTO role_capabilities (role, capability) VALUES (?, ?)`,
					d.role, d.cap,
				); err != nil {
					return err
				}
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	}
}
