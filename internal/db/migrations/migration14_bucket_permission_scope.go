package migrations

import "database/sql"

// migration14_v141_BucketPermissionScope adds the bucket tenant scope to bucket permissions.
func migration14_v141_BucketPermissionScope() Migration {
	return Migration{
		Version:     14,
		Description: "v1.4.1 - Scope bucket permissions by bucket tenant",
		Up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`
				ALTER TABLE bucket_permissions RENAME TO bucket_permissions_old
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`
				CREATE TABLE bucket_permissions (
					id TEXT PRIMARY KEY,
					bucket_name TEXT NOT NULL,
					bucket_tenant_id TEXT NOT NULL DEFAULT '',
					user_id TEXT,
					tenant_id TEXT,
					group_id TEXT REFERENCES groups(id) ON DELETE CASCADE,
					permission_level TEXT NOT NULL,
					granted_by TEXT NOT NULL,
					granted_at INTEGER NOT NULL,
					expires_at INTEGER,
					FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
					FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
				)
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`
				INSERT INTO bucket_permissions (
					id, bucket_name, bucket_tenant_id, user_id, tenant_id, group_id,
					permission_level, granted_by, granted_at, expires_at
				)
				SELECT
					id, bucket_name, '', user_id, tenant_id, group_id,
					permission_level, granted_by, granted_at, expires_at
				FROM bucket_permissions_old
			`); err != nil {
				return err
			}

			if _, err := tx.Exec(`DROP TABLE bucket_permissions_old`); err != nil {
				return err
			}

			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_bucket_permissions_bucket_scope ON bucket_permissions(bucket_name, bucket_tenant_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_bucket_permissions_user ON bucket_permissions(user_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_bucket_permissions_tenant ON bucket_permissions(tenant_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_bucket_permissions_group_id ON bucket_permissions(group_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_bucket_permissions_unique_user_scope ON bucket_permissions(bucket_name, bucket_tenant_id, user_id) WHERE user_id IS NOT NULL`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_bucket_permissions_unique_tenant_scope ON bucket_permissions(bucket_name, bucket_tenant_id, tenant_id) WHERE tenant_id IS NOT NULL`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_bucket_permissions_unique_group_scope ON bucket_permissions(bucket_name, bucket_tenant_id, group_id) WHERE group_id IS NOT NULL`); err != nil {
				return err
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	}
}
