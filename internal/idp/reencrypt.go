package idp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ReencryptSecrets rewrites, inside tx, the bind passwords and client secrets
// encrypted with from as encrypted with to, and names the ones neither
// decrypts. With from equal to to it only names them.
func ReencryptSecrets(ctx context.Context, tx *sql.Tx, from, to string) ([]string, error) {
	if from == "" || to == "" {
		return nil, fmt.Errorf("an encryption secret is required")
	}
	type stored struct{ id, name, config string }
	rows, err := tx.QueryContext(ctx, `SELECT id, name, config FROM identity_providers`)
	if err != nil {
		return nil, err
	}
	var all []stored
	for rows.Next() {
		var r stored
		if err := rows.Scan(&r.id, &r.name, &r.config); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var unreadable []string
	for _, r := range all {
		var config ProviderConfig
		if err := json.Unmarshal([]byte(r.config), &config); err != nil {
			return nil, fmt.Errorf("identity provider %s: %w", r.name, err)
		}
		changed := false
		rewrite := func(value *string, what string) error {
			if *value == "" {
				return nil
			}
			plain, err := Decrypt(*value, from)
			if err != nil {
				if _, err := Decrypt(*value, to); err != nil {
					unreadable = append(unreadable, fmt.Sprintf("identity provider %s (%s)", r.name, what))
				}
				return nil
			}
			if from == to {
				return nil
			}
			encrypted, err := Encrypt(plain, to)
			if err != nil {
				return err
			}
			*value, changed = encrypted, true
			return nil
		}
		if config.LDAP != nil {
			if err := rewrite(&config.LDAP.BindPassword, "LDAP bind password"); err != nil {
				return nil, err
			}
		}
		if config.OAuth2 != nil {
			if err := rewrite(&config.OAuth2.ClientSecret, "OAuth client secret"); err != nil {
				return nil, err
			}
		}
		if !changed {
			continue
		}
		data, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE identity_providers SET config = ? WHERE id = ?`, string(data), r.id); err != nil {
			return nil, err
		}
	}
	return unreadable, nil
}
